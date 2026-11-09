package revision

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
	"syscall"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
)

// FileArchive 保留目录句柄，以只增不改的 JSON 对象存放全部资源。
// 目录必须由可信调用方拥有；文件系统须支持硬链接和目录同步。
type FileArchive struct {
	mu       sync.RWMutex
	root     *os.Root
	closed   bool
	closeErr error
}

var _ Archive = (*FileArchive)(nil)

// Open 创建或打开归档目录。后续操作始终相对于保留的目录句柄。
func Open(dir string) (*FileArchive, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &FileArchive{root: root}, nil
}

// Put 封存副本并原子发布；相同内容去重，已有损坏对象报错且绝不覆盖。
func (a *FileArchive) Put(ctx context.Context, def Definition, files map[string][]byte) (AgentRevision, error) {
	if err := ctx.Err(); err != nil {
		return AgentRevision{}, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.ready(ctx); err != nil {
		return AgentRevision{}, err
	}
	rev, err := Seal(def, files)
	if err != nil {
		return AgentRevision{}, err
	}
	if err = ctx.Err(); err != nil {
		return AgentRevision{}, err
	}
	if old, err := a.load(ctx, rev.ID); err == nil {
		if err := a.syncDirectory(); err != nil {
			return AgentRevision{}, err
		}
		return old, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return AgentRevision{}, err
	}
	data, err := json.Marshal(rev)
	if err != nil {
		return AgentRevision{}, err
	}
	if len(data) > constants.MaxRevisionArchiveBytes {
		return AgentRevision{}, invalid("归档超过字节上限")
	}
	// 随机名称配合 O_EXCL，临时文件同样只通过保留的根目录访问。
	name := ".revision-" + rand.Text() + ".tmp"
	f, err := a.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return AgentRevision{}, err
	}
	defer func() { _ = a.root.Remove(name) }()
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	writeErr = errors.Join(writeErr, f.Close())
	if writeErr != nil {
		return AgentRevision{}, writeErr
	}
	if err := ctx.Err(); err != nil {
		return AgentRevision{}, err
	}
	// Link 是跨句柄、跨进程的 create-if-absent；Rename 会覆盖目标，不能用于这里。
	if err = a.root.Link(name, rev.ID+".json"); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return AgentRevision{}, err
		}
		old, err := a.load(ctx, rev.ID)
		if err != nil {
			return AgentRevision{}, err
		}
		if err := a.syncDirectory(); err != nil {
			return AgentRevision{}, err
		}
		return old, nil
	}
	if err := a.root.Remove(name); err != nil {
		return AgentRevision{}, err
	}
	if err := a.syncDirectory(); err != nil {
		return AgentRevision{}, err
	}
	return rev, nil
}

// Load 校验大小、严格结构、规范字节和内容摘要，返回独立拥有的数据。
func (a *FileArchive) Load(ctx context.Context, id string) (AgentRevision, error) {
	if err := ctx.Err(); err != nil {
		return AgentRevision{}, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.ready(ctx); err != nil {
		return AgentRevision{}, err
	}
	return a.load(ctx, id)
}

func (a *FileArchive) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.closed || a.root == nil {
		return fs.ErrClosed
	}
	return nil
}

func (a *FileArchive) load(ctx context.Context, id string) (_ AgentRevision, err error) {
	if !validDigest(id) {
		return AgentRevision{}, invalid("修订标识必须是小写 SHA-256 摘要")
	}
	name := id + ".json"
	info, err := a.root.Lstat(name)
	if err != nil {
		return AgentRevision{}, err
	}
	if !info.Mode().IsRegular() {
		return AgentRevision{}, invalid("归档必须是普通文件")
	}
	if info.Size() > constants.MaxRevisionArchiveBytes {
		return AgentRevision{}, invalid("归档超过字节上限")
	}
	// 非阻塞打开可避免被替换为 FIFO 后挂起；打开后重新确认文件种类与大小。
	f, err := a.root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return AgentRevision{}, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	opened, err := f.Stat()
	if err != nil {
		return AgentRevision{}, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return AgentRevision{}, invalid("归档文件已改变")
	}
	if opened.Size() > constants.MaxRevisionArchiveBytes {
		return AgentRevision{}, invalid("归档超过字节上限")
	}
	data, err := io.ReadAll(io.LimitReader(f, constants.MaxRevisionArchiveBytes+1))
	if err != nil {
		return AgentRevision{}, err
	}
	if len(data) > constants.MaxRevisionArchiveBytes {
		return AgentRevision{}, invalid("归档超过字节上限")
	}
	if err := ctx.Err(); err != nil {
		return AgentRevision{}, err
	}
	if err := checkArchiveJSON(data); err != nil {
		return AgentRevision{}, err
	}
	var rev AgentRevision
	if err := json.Unmarshal(data, &rev, json.Strict); err != nil {
		return AgentRevision{}, invalid("归档结构无效：%v", err)
	}
	if rev.ID != id {
		return AgentRevision{}, invalid("归档标识与文件名不符")
	}
	sealed, err := Seal(rev.Definition, rev.Files)
	if err != nil {
		return AgentRevision{}, err
	}
	if sealed.ID != id {
		return AgentRevision{}, invalid("归档摘要与内容不符")
	}
	canonical, err := json.Marshal(sealed)
	if err != nil {
		return AgentRevision{}, err
	}
	if !bytes.Equal(data, canonical) {
		return AgentRevision{}, invalid("归档字节不是规范编码")
	}
	if err := ctx.Err(); err != nil {
		return AgentRevision{}, err
	}
	return sealed, nil
}

// 在结构解码与 base64 分配前，检查嵌套、定义大小和文件数量/大小。
func checkArchiveJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data), json.Strict)
	files, fileCount, fileKey := false, 0, true
	definitionStart := int64(-1)
	for {
		before := dec.InputOffset()
		tok, err := dec.ReadToken()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return invalid("归档 JSON 无效：%v", err)
		}
		depth := dec.StackDepth()
		if depth > constants.MaxRevisionJSONDepth+2 {
			return invalid("归档 JSON 嵌套超过上限")
		}
		// 只在顶层成员开始对象时取路径，避免长键下的数组每个元素重复复制祖先键名。
		pointer := ""
		if depth == 2 && tok.Kind() == '{' {
			pointer = string(dec.StackPointer())
			if pointer == "/definition" {
				definitionStart = before
			}
		}
		if definitionStart >= 0 {
			if dec.InputOffset()-definitionStart > constants.MaxRevisionDefinitionBytes+1 {
				return invalid("定义超过字节上限")
			}
			if depth == 1 && tok.Kind() == '}' {
				definitionStart = -1
			}
		}
		if pointer == "/files" && tok.Kind() == '{' {
			files = true
			continue
		}
		if files {
			if depth == 1 && tok.Kind() == '}' {
				files = false
				continue
			}
			if depth != 2 || tok.Kind() != '"' {
				return invalid("文件映射必须包含路径与 base64 字符串")
			}
			if fileKey {
				fileCount++
				if fileCount > constants.MaxRevisionFiles {
					return invalid("文件数量超过上限")
				}
			} else if len(tok.String()) > ((constants.MaxRevisionFileBytes+2)/3)*4 {
				return invalid("文件超过字节上限")
			}
			fileKey = !fileKey
		}
	}
}

func (a *FileArchive) syncDirectory() (err error) {
	dir, err := a.root.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("同步归档目录失败：%w", err)
	}
	return nil
}

// Close 等待正在进行的操作完成后释放目录句柄，可重复调用。
func (a *FileArchive) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.closed {
		a.closed = true
		if a.root != nil {
			a.closeErr = a.root.Close()
		}
	}
	return a.closeErr
}
