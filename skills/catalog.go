package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
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

// Unlimited disables a DiscoverOptions limit. Zero keeps the package default;
// any other negative value is rejected.
const Unlimited = -1

// DiscoverOptions overrides the abuse limits applied while discovering and
// reading skills. The zero value of each field keeps the package default
// (see the constants package), Unlimited removes the limit, and any other
// negative value is an error.
type DiscoverOptions struct {
	// MaxFileBytes caps one SKILL.md and one reference file. The defaults are
	// constants.SkillMaxDocumentBytes and constants.SkillMaxReferenceBytes.
	MaxFileBytes int64
	// MaxTotalBytes caps the SKILL.md bytes read in one discovery. There is no
	// default cap.
	MaxTotalBytes int64
	// MaxFiles caps directory entries listed per discovery and per Load
	// (default constants.SkillMaxEntries).
	MaxFiles int
	// MaxDescription caps the description length in characters (default 1024).
	MaxDescription int
	// MaxDepth caps resource directory nesting (default constants.SkillMaxResourceDepth).
	MaxDepth int
}

type limits struct {
	docBytes, refBytes, totalBytes int64
	files, description, depth      int
}

func resolveLimit[T int | int64](name string, v, def T, unlimited T) (T, error) {
	switch {
	case v == 0:
		return def, nil
	case v == Unlimited:
		return unlimited, nil
	case v < 0:
		return 0, agenterrors.Newf(agenterrors.CodeInvalidConfig, "invalid DiscoverOptions.%s %d", name, v)
	}
	return v, nil
}

func (o DiscoverOptions) resolve() (l limits, err error) {
	const noBytes, noCount = math.MaxInt64, math.MaxInt
	docDefault, refDefault := int64(constants.SkillMaxDocumentBytes), int64(constants.SkillMaxReferenceBytes)
	if o.MaxFileBytes != 0 {
		docDefault, refDefault = 0, 0
	}
	file, err := resolveLimit("MaxFileBytes", o.MaxFileBytes, 0, noBytes)
	if err != nil {
		return limits{}, err
	}
	l.docBytes, l.refBytes = max(file, docDefault), max(file, refDefault)
	for _, f := range []func() error{
		func() (err error) {
			l.totalBytes, err = resolveLimit("MaxTotalBytes", o.MaxTotalBytes, noBytes, noBytes)
			return
		},
		func() (err error) {
			l.files, err = resolveLimit("MaxFiles", o.MaxFiles, constants.SkillMaxEntries, noCount)
			return
		},
		func() (err error) {
			l.description, err = resolveLimit("MaxDescription", o.MaxDescription, maxDescriptionChars, noCount)
			return
		},
		func() (err error) {
			l.depth, err = resolveLimit("MaxDepth", o.MaxDepth, constants.SkillMaxResourceDepth, noCount)
			return
		},
	} {
		if err := f(); err != nil {
			return limits{}, err
		}
	}
	return l, nil
}

type entry struct {
	skill Skill
	src   source
}

// state is shared by a Catalog and the subsets derived from it by Select, so
// that Close on the owner excludes file operations on every subset.
type state struct {
	mu       sync.RWMutex
	closed   bool
	closeErr error
}

// Catalog 保存发现时的元数据和正文快照，并持有每个技能的目录句柄。
// 资源在 Load/ReadReference 时读取，可以变化；这不是整包的不可变快照。
// 所有方法可以并发使用，Close 与文件操作互斥。Catalog 不可复制。
type Catalog struct {
	st      *state
	entries map[string]entry
	order   []string
	owner   bool
	limits  limits
}

// Discover 扫描调用方已授权并打开的目录中的 <name>/SKILL.md。
// 来源顺序由调用方决定，同名时保留第一个；各目录内按名称排序。
// 不递归发现技能，不跟随技能目录本身的符号链接，无 SKILL.md 的目录被跳过。
// 错误的技能会使整个发现失败（包括被覆盖的技能），并关闭已获取的句柄。
// roots 仅被借用到函数返回，调用方仍须关闭它们。成功后 Catalog 持有独立的
// 子目录句柄，必须 Close；目录被重命名或原路径被替换不会重定向后续读取。
func Discover(ctx context.Context, roots ...*os.Root) (*Catalog, error) {
	return DiscoverWithOptions(ctx, DiscoverOptions{}, roots...)
}

// DiscoverWithOptions is Discover with caller-supplied limits. A zero
// DiscoverOptions behaves exactly like Discover.
func DiscoverWithOptions(ctx context.Context, opts DiscoverOptions, roots ...*os.Root) (*Catalog, error) {
	srcs := make([]source, len(roots))
	for i, root := range roots {
		if root == nil {
			return nil, agenterrors.New(agenterrors.CodeInvalidConfig, "nil skill source root")
		}
		srcs[i] = &rootSource{root: root}
	}
	return discoverSources(ctx, opts, srcs...)
}

func discoverSources(ctx context.Context, opts DiscoverOptions, srcs ...source) (_ *Catalog, err error) {
	l, err := opts.resolve()
	if err != nil {
		return nil, err
	}
	c := &Catalog{st: &state{}, entries: make(map[string]entry), owner: true, limits: l}
	defer func() {
		if err != nil {
			err = errors.Join(err, c.Close())
		}
	}()
	remaining, totalLeft := l.files, l.totalBytes
	for _, parent := range srcs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		children, err := parent.readDir(".", &remaining)
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
			s, dir, err := discoverSkill(parent, child.Name(), l, &totalLeft)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if _, exists := c.entries[s.Metadata.Name]; exists {
				if err := dir.close(); err != nil {
					return nil, err
				}
				continue
			}
			c.entries[s.Metadata.Name] = entry{skill: s, src: dir}
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

func discoverSkill(parent source, name string, l limits, totalLeft *int64) (s Skill, src source, err error) {
	src, err = parent.openChild(name)
	if err != nil {
		return Skill{}, nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, src.close())
			src = nil
		}
	}()
	data, err := src.readFile("SKILL.md", min(l.docBytes, *totalLeft))
	if err != nil {
		return Skill{}, src, err
	}
	*totalLeft -= int64(len(data))
	s, err = parse(data, l.docBytes, l.description)
	if err != nil {
		return Skill{}, src, fmt.Errorf("skill %s: %w", parent.sourcePath(name), err)
	}
	if s.Metadata.Name != name {
		return Skill{}, src, agenterrors.Newf(agenterrors.CodeInvalidInput, "skill name %q 与目录名 %q 不一致", s.Metadata.Name, name)
	}
	s.Metadata.Source = parent.sourcePath(name)
	return s, src, nil
}

// Select returns a Catalog containing only the named skills, in the parent's
// order; an unknown name is an error wrapping fs.ErrNotExist. Load and
// ReadReference on the result cannot reach any other skill.
//
// The result shares the parent's underlying handles. Only the parent
// releases them: Close on the result is a no-op, and once the parent is
// closed the result fails with fs.ErrClosed like the parent does.
func (c *Catalog) Select(names ...string) (*Catalog, error) {
	c.st.mu.RLock()
	defer c.st.mu.RUnlock()
	if c.st.closed {
		return nil, fs.ErrClosed
	}
	want := make(map[string]bool, len(names))
	for _, name := range names {
		if _, ok := c.entries[name]; !ok {
			return nil, fmt.Errorf("unknown skill %q: %w", name, fs.ErrNotExist)
		}
		want[name] = true
	}
	sub := &Catalog{st: c.st, entries: make(map[string]entry, len(want)), limits: c.limits}
	for _, name := range c.order {
		if want[name] {
			sub.entries[name] = c.entries[name]
			sub.order = append(sub.order, name)
		}
	}
	return sub, nil
}

// List 返回独立的元数据副本，不读取文件、不隐藏或授权任何技能。
// Close 后仍可查看这份发现时的目录快照。
func (c *Catalog) List() []Metadata {
	c.st.mu.RLock()
	defer c.st.mu.RUnlock()
	list := make([]Metadata, 0, len(c.order))
	for _, name := range c.order {
		list = append(list, cloneMetadata(c.entries[name].skill.Metadata))
	}
	return list
}

// Load 返回缓存的正文与当前资源列表。目录遍历不跟随符号链接，列表中的
// 文件链接仍需经 ReadReference 的边界检查才能读取，不代表已获授权。
func (c *Catalog) Load(ctx context.Context, name string) (Skill, error) {
	c.st.mu.RLock()
	defer c.st.mu.RUnlock()
	e, err := c.lookup(ctx, name)
	if err != nil {
		return Skill{}, err
	}
	s := e.skill
	s.Metadata = cloneMetadata(s.Metadata)
	s.Files = make([]string, 0)
	remaining := c.limits.files
	if err := resourceFiles(ctx, e.src, c.limits, ".", 0, &remaining, &s.Files); err != nil {
		return Skill{}, err
	}
	slices.Sort(s.Files)
	return s, nil
}

// ReadReference 读取技能根目录内的普通文件，返回原始字节，不执行脚本。
// file 使用斜线相对路径（允许 . 和重复分隔符，无 ..、绝对路径或反斜线）；内部相对
// 符号链接允许，越界链接由 os.Root 拒绝。资源大小限制见 constants。
func (c *Catalog) ReadReference(ctx context.Context, name, file string) ([]byte, error) {
	c.st.mu.RLock()
	defer c.st.mu.RUnlock()
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
	data, err := e.src.readFile(clean, c.limits.refBytes)
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
	if c.st.closed {
		return entry{}, fs.ErrClosed
	}
	e, ok := c.entries[name]
	if !ok {
		return entry{}, fmt.Errorf("unknown skill %q: %w", name, fs.ErrNotExist)
	}
	return e, nil
}

// Close 释放所有保留的技能目录句柄，可重复调用。已关闭的 Catalog 不再读文件。
// Select 返回的子目录不拥有句柄，其 Close 为空操作。
func (c *Catalog) Close() error {
	if !c.owner {
		return nil
	}
	c.st.mu.Lock()
	defer c.st.mu.Unlock()
	if !c.st.closed {
		c.st.closed = true
		for _, name := range c.order {
			c.st.closeErr = errors.Join(c.st.closeErr, c.entries[name].src.close())
		}
	}
	return c.st.closeErr
}

// source reads one skill directory (or a directory of skills) through a
// confined handle. Names are slash-separated and relative to it.
type source interface {
	readDir(name string, remaining *int) ([]fs.DirEntry, error)
	readFile(name string, limit int64) ([]byte, error)
	openChild(name string) (source, error)
	sourcePath(skill string) string
	close() error
}

// rootSource is a source backed by an os.Root.
type rootSource struct{ root *os.Root }

func (r *rootSource) sourcePath(skill string) string {
	return filepath.Join(r.root.Name(), skill, "SKILL.md")
}

func (r *rootSource) close() error { return r.root.Close() }

func (r *rootSource) openChild(name string) (source, error) {
	// Keep the final dot: the named component must be opened as an intermediate
	// directory (O_DIRECTORY on Unix), even if a listed directory becomes a FIFO.
	// filepath.Join/Clean would remove this protection before OpenRoot sees it.
	child, err := r.root.OpenRoot(name + string(os.PathSeparator) + ".")
	if err != nil {
		return nil, err
	}
	return &rootSource{root: child}, nil
}

func (r *rootSource) readFile(name string, limit int64) (_ []byte, err error) {
	// O_NONBLOCK prevents a FIFO substituted during discovery/reading from hanging
	// the caller before Stat can reject it. It does not affect regular-file reads.
	f, err := r.root.OpenFile(filepath.FromSlash(name), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return readLimited(f, info, limit)
}

func (r *rootSource) readDir(name string, remaining *int) (_ []fs.DirEntry, err error) {
	f, err := r.root.OpenFile(filepath.FromSlash(name), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	entries, err := f.ReadDir(readDirCount(*remaining))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return takeEntries(entries, remaining)
}

func readLimited(f io.Reader, info fs.FileInfo, limit int64) ([]byte, error) {
	if !info.Mode().IsRegular() {
		return nil, agenterrors.New(agenterrors.CodeInvalidInput, "skill content must be a regular file")
	}
	if info.Size() > limit {
		return nil, agenterrors.New(agenterrors.CodeInvalidInput, "skill file byte limit exceeded")
	}
	read := limit
	if read < math.MaxInt64 {
		read++
	}
	data, err := io.ReadAll(io.LimitReader(f, read))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, agenterrors.New(agenterrors.CodeInvalidInput, "skill file byte limit exceeded")
	}
	return data, nil
}

// readDirCount is the ReadDir batch size that detects one entry over budget.
func readDirCount(remaining int) int {
	if remaining == math.MaxInt {
		return -1
	}
	return remaining + 1
}

func takeEntries(entries []fs.DirEntry, remaining *int) ([]fs.DirEntry, error) {
	if len(entries) > *remaining {
		return nil, agenterrors.New(agenterrors.CodeInvalidInput, "skill directory entry limit exceeded")
	}
	*remaining -= len(entries)
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func resourceFiles(ctx context.Context, src source, l limits, dir string, depth int, remaining *int, files *[]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > l.depth {
		return agenterrors.New(agenterrors.CodeInvalidInput, "skill resource depth limit exceeded")
	}
	entries, err := src.readDir(dir, remaining)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := path.Join(dir, entry.Name())
		if entry.IsDir() {
			if err := resourceFiles(ctx, src, l, name, depth+1, remaining, files); err != nil {
				return err
			}
		} else if name != "SKILL.md" && (entry.Type().IsRegular() || entry.Type()&fs.ModeSymlink != 0) {
			*files = append(*files, name)
		}
	}
	return nil
}
