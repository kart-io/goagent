package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/kart-io/goagent/constants"
	agenterrors "github.com/kart-io/goagent/errors"
)

type entry struct {
	skill Skill
	root  *os.Root
}

// Catalog 保存发现时的元数据和正文快照，并持有每个技能的目录句柄。
// 资源在 Load/ReadReference 时读取，可以变化；这不是整包的不可变快照。
// 所有方法可以并发使用，Close 与文件操作互斥。Catalog 不可复制。
type Catalog struct {
	mu       sync.RWMutex
	entries  map[string]entry
	order    []string
	closed   bool
	closeErr error
}

// Discover 扫描调用方已授权并打开的目录中的 <name>/SKILL.md。
// 来源顺序由调用方决定，同名时保留第一个；各目录内按名称排序。
// 不递归发现技能，不跟随技能目录本身的符号链接，无 SKILL.md 的目录被跳过。
// 错误的技能会使整个发现失败（包括被覆盖的技能），并关闭已获取的句柄。
// roots 仅被借用到函数返回，调用方仍须关闭它们。成功后 Catalog 持有独立的
// 子目录句柄，必须 Close；目录被重命名或原路径被替换不会重定向后续读取。
func Discover(ctx context.Context, roots ...*os.Root) (_ *Catalog, err error) {
	c := &Catalog{entries: make(map[string]entry)}
	defer func() {
		if err != nil {
			err = errors.Join(err, c.Close())
		}
	}()
	remaining := constants.SkillMaxEntries
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if root == nil {
			return nil, agenterrors.New(agenterrors.CodeInvalidConfig, "nil skill source root")
		}
		children, err := readDirectory(root, ".", &remaining)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !child.IsDir() {
				continue
			}
			s, dir, err := discoverSkill(root, child.Name())
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if _, exists := c.entries[s.Metadata.Name]; exists {
				if err := dir.Close(); err != nil {
					return nil, err
				}
				continue
			}
			c.entries[s.Metadata.Name] = entry{skill: s, root: dir}
			c.order = append(c.order, s.Metadata.Name)
			if len(c.order) > constants.SkillMaxCount {
				return nil, agenterrors.New(agenterrors.CodeInvalidInput, "skill count limit exceeded")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c, nil
}

func discoverSkill(parent *os.Root, name string) (s Skill, root *os.Root, err error) {
	// Keep the final dot: the named component must be opened as an intermediate
	// directory (O_DIRECTORY on Unix), even if a listed directory becomes a FIFO.
	// filepath.Join/Clean would remove this protection before OpenRoot sees it.
	root, err = parent.OpenRoot(name + string(os.PathSeparator) + ".")
	if err != nil {
		return Skill{}, nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, root.Close())
			root = nil
		}
	}()
	data, err := readFile(root, "SKILL.md", constants.SkillMaxDocumentBytes)
	if err != nil {
		return Skill{}, root, err
	}
	s, err = Parse(data)
	if err != nil {
		return Skill{}, root, fmt.Errorf("skill %s: %w", filepath.Join(parent.Name(), name, "SKILL.md"), err)
	}
	if s.Metadata.Name != name {
		return Skill{}, root, agenterrors.Newf(agenterrors.CodeInvalidInput, "skill name %q 与目录名 %q 不一致", s.Metadata.Name, name)
	}
	s.Metadata.Source = filepath.Join(parent.Name(), name, "SKILL.md")
	return s, root, nil
}

// List 返回独立的元数据副本，不读取文件、不隐藏或授权任何技能。
// Close 后仍可查看这份发现时的目录快照。
func (c *Catalog) List() []Metadata {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := make([]Metadata, 0, len(c.order))
	for _, name := range c.order {
		list = append(list, cloneMetadata(c.entries[name].skill.Metadata))
	}
	return list
}

// Load 返回缓存的正文与当前资源列表。目录遍历不跟随符号链接，列表中的
// 文件链接仍需经 ReadReference 的边界检查才能读取，不代表已获授权。
func (c *Catalog) Load(ctx context.Context, name string) (Skill, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, err := c.lookup(ctx, name)
	if err != nil {
		return Skill{}, err
	}
	s := e.skill
	s.Metadata = cloneMetadata(s.Metadata)
	s.Files = make([]string, 0)
	remaining := constants.SkillMaxEntries
	if err := resourceFiles(ctx, e.root, ".", 0, &remaining, &s.Files); err != nil {
		return Skill{}, err
	}
	slices.Sort(s.Files)
	return s, nil
}

// ReadReference 读取技能根目录内的普通文件，返回原始字节，不执行脚本。
// file 使用斜线相对路径（允许 . 和重复分隔符，无 ..、绝对路径或反斜线）；内部相对
// 符号链接允许，越界链接由 os.Root 拒绝。资源大小限制见 constants。
func (c *Catalog) ReadReference(ctx context.Context, name, file string) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, err := c.lookup(ctx, name)
	if err != nil {
		return nil, err
	}
	valid := file != "" && !path.IsAbs(file) && !strings.ContainsAny(file, "\\\x00") &&
		!strings.HasSuffix(file, "/") && !strings.HasSuffix(file, "/.")
	// Reject parent components before cleaning: normalization must not erase an
	// explicitly forbidden traversal. Benign dot/separator spellings remain
	// compatible with links in existing skill instructions.
	for _, component := range strings.Split(file, "/") {
		if component == ".." {
			valid = false
		}
	}
	clean := path.Clean(file)
	if !valid || clean == "." || !fs.ValidPath(clean) || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return nil, &fs.PathError{Op: "read skill reference", Path: file, Err: fs.ErrInvalid}
	}
	data, err := readFile(e.root, filepath.FromSlash(clean), constants.SkillMaxReferenceBytes)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Catalog) lookup(ctx context.Context, name string) (entry, error) {
	if err := ctx.Err(); err != nil {
		return entry{}, err
	}
	if c.closed {
		return entry{}, fs.ErrClosed
	}
	e, ok := c.entries[name]
	if !ok {
		return entry{}, fmt.Errorf("unknown skill %q: %w", name, fs.ErrNotExist)
	}
	return e, nil
}

// Close 释放所有保留的技能目录句柄，可重复调用。已关闭的 Catalog 不再读文件。
func (c *Catalog) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		for _, name := range c.order {
			c.closeErr = errors.Join(c.closeErr, c.entries[name].root.Close())
		}
	}
	return c.closeErr
}

func readFile(root *os.Root, name string, limit int) (_ []byte, err error) {
	// O_NONBLOCK prevents a FIFO substituted during discovery/reading from hanging
	// the caller before Stat can reject it. It does not affect regular-file reads.
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, agenterrors.New(agenterrors.CodeInvalidInput, "skill content must be a regular file")
	}
	if info.Size() > int64(limit) {
		return nil, agenterrors.New(agenterrors.CodeInvalidInput, "skill file byte limit exceeded")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, agenterrors.New(agenterrors.CodeInvalidInput, "skill file byte limit exceeded")
	}
	return data, nil
}

func readDirectory(root *os.Root, name string, remaining *int) (_ []fs.DirEntry, err error) {
	f, err := root.OpenFile(filepath.FromSlash(name), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	entries, err := f.ReadDir(*remaining + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > *remaining {
		return nil, agenterrors.New(agenterrors.CodeInvalidInput, "skill directory entry limit exceeded")
	}
	*remaining -= len(entries)
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func resourceFiles(ctx context.Context, root *os.Root, dir string, depth int, remaining *int, files *[]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > constants.SkillMaxResourceDepth {
		return agenterrors.New(agenterrors.CodeInvalidInput, "skill resource depth limit exceeded")
	}
	entries, err := readDirectory(root, dir, remaining)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := path.Join(dir, entry.Name())
		if entry.IsDir() {
			if err := resourceFiles(ctx, root, name, depth+1, remaining, files); err != nil {
				return err
			}
		} else if name != "SKILL.md" && (entry.Type().IsRegular() || entry.Type()&fs.ModeSymlink != 0) {
			*files = append(*files, name)
		}
	}
	return nil
}
