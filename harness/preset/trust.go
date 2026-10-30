package preset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TrustStore 记录宿主对项目扩权项的授权。授权按"规范化后的项目根目录 + 扩权项摘要"记录，
// 扩权项或其配置变化后摘要随之变化，需要重新授权。
type TrustStore interface {
	IsTrusted(root, digest string) (bool, error)
	Grant(root, digest string) error
}

// TrustRequiredError 表示 preset 含有未授权的扩权项。宿主向用户展示 Items，同意后调用
// TrustStore.Grant(Root, Digest) 再重新 Build。
type TrustRequiredError struct {
	Preset, Root, Digest string
	Items                []Expansion
}

func (e *TrustRequiredError) Error() string {
	lines := make([]string, len(e.Items))
	for i, item := range e.Items {
		lines[i] = "  - " + item.String()
	}
	return fmt.Sprintf("preset %s 含有需要授权的扩权项（项目 %s）：\n%s", e.Preset, e.Root, strings.Join(lines, "\n"))
}

// expansionDigest 覆盖每个扩权项及其所在组件的完整配置，配置任何变化都使授权失效。
func expansionDigest(root string, p *Resolved) (string, error) {
	type item struct {
		Component, Layer, Path, Reason string
		Config                         any
	}
	items := make([]item, 0, len(p.Expansions))
	for _, e := range p.Expansions {
		var cfg any
		for _, r := range p.Rows {
			if r.ID == e.Component {
				var err error
				if cfg, err = rawJSON(r); err != nil {
					return "", err
				}
			}
		}
		items = append(items, item{e.Component, e.Origin.Layer, e.Origin.Path, e.Reason, cfg})
	}
	data, err := json.Marshal(struct {
		Root, Preset string
		Items        []item
	}{root, p.ID, items})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// MemoryTrustStore 是进程内的 TrustStore，适用于测试与一次性运行。
type MemoryTrustStore struct {
	mu sync.Mutex
	m  map[string]string
}

// IsTrusted 实现 TrustStore。
func (s *MemoryTrustStore) IsTrusted(root, digest string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[root] == digest, nil
}

// Grant 实现 TrustStore，每个项目只保留最近一次授权。
func (s *MemoryTrustStore) Grant(root, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[root] = digest
	return nil
}

// FileTrustStore 把授权保存在 JSON 文件中（如 ~/.goagent/trust.json），每个项目只保留最近一次授权。
type FileTrustStore struct {
	Path string
	mu   sync.Mutex
}

func (s *FileTrustStore) load() (map[string]string, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", s.Path, err)
	}
	return m, nil
}

// IsTrusted 实现 TrustStore。
func (s *FileTrustStore) IsTrusted(root, digest string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	return err == nil && m[root] == digest, err
}

// Grant 实现 TrustStore，经临时文件改名写入。
func (s *FileTrustStore) Grant(root, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	m[root] = digest
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".trust-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}
