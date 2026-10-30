package env

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LocalFS 是宿主机目录上的 FileSystem。每次操作都重新解析路径并检查仍在工作区内，路径已被换成指向工作区外的
// 符号链接时返回 ErrOutsideWorkspace；解析与打开之间仍有被替换的窗口，不能防御并发篡改工作区的进程。
// 同进程所有 LocalFS 实例按真实绝对路径协调写入和删除（含重叠根目录），IfVersion 在此范围内原子执行。
// 直接 os 写入或其他进程不参与协调，版本比对只能发现检查前的内容变化；CreateIfAbsent 用硬链接防止覆盖。
// Unix 本地文件系统上，写入通过同步临时文件后 rename/link 发布；替换会更换 inode，不保留硬链接关系或扩展属性。
// 发布前失败保留旧文件；发布后目录同步失败会返回 error，但新内容已可见。
type LocalFS struct {
	root string
}

// 按真实绝对路径分片，让不同实例及重叠根目录中的同一路径共享锁，且不随访问路径数增长。
// 分片碰撞只会降低并行度；等待可取消。直接通过 os 或其他进程写入不参与该协调。
var localMutationLocks = func() [256]chan struct{} {
	var locks [256]chan struct{}
	for i := range locks {
		locks[i] = make(chan struct{}, 1)
	}
	return locks
}()

func lockLocalMutation(ctx context.Context, abs string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(abs))
	lock := localMutationLocks[sum[0]]
	select {
	case lock <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-lock
			return nil, err
		}
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// NewLocalFS 以已存在的目录 root 为工作区根目录。
func NewLocalFS(root string) (*LocalFS, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root %q is not a directory", root)
	}
	return &LocalFS{root: real}, nil
}

// Root 返回解析符号链接后的根目录。
func (l *LocalFS) Root() string { return l.root }

// Resolve 实现 FileSystem。已存在的部分会解析符号链接，返回的是真实路径对应的相对路径。
func (l *LocalFS) Resolve(p string) (string, error) {
	_, rel, err := l.abs(p)
	return rel, err
}

// abs 解析路径为工作区内的绝对路径与相对路径。
func (l *LocalFS) abs(p string) (abs, rel string, err error) {
	if p == "" {
		return "", "", errors.New("path is required")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(l.root, filepath.FromSlash(p))
	}
	real, err := evalExisting(filepath.Clean(p))
	if err != nil {
		return "", "", err
	}
	r, err := filepath.Rel(l.root, real)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", "", ErrOutsideWorkspace
	}
	return real, filepath.ToSlash(r), nil
}

// evalExisting 解析 p 中已存在的最长前缀的符号链接，再拼回不存在的部分。
func evalExisting(p string) (string, error) {
	cur, rest := p, []string(nil)
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(append([]string{real}, rest...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		// 悬空链接的目标无法确认是否在工作区内，写入时会跟随链接。
		if info, lerr := os.Lstat(cur); lerr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("%s is a dangling symlink: %w", cur, ErrOutsideWorkspace)
			}
			// 另一个写者刚创建了该条目，重新解析，不能误报为悬空链接。
			continue
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}

func versionOf(data []byte) Version {
	sum := sha256.Sum256(data)
	return Version(hex.EncodeToString(sum[:]))
}

func infoOf(rel string, fi fs.FileInfo) Info {
	return Info{Path: rel, Size: fi.Size(), Mode: fi.Mode(), ModTime: fi.ModTime()}
}

// Stat 实现 FileSystem。
func (l *LocalFS) Stat(ctx context.Context, p string) (Info, error) {
	abs, rel, err := l.abs(p)
	if err != nil {
		return Info{}, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return Info{}, err
	}
	return infoOf(rel, fi), nil
}

// ReadFile 实现 FileSystem。
func (l *LocalFS) ReadFile(ctx context.Context, p string) ([]byte, Version, error) {
	abs, _, err := l.abs(p)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	return data, versionOf(data), nil
}

// WriteFile 实现 FileSystem。
func (l *LocalFS) WriteFile(ctx context.Context, p string, data []byte, intent WriteIntent) (Version, error) {
	abs, _, err := l.abs(p)
	if err != nil {
		return "", err
	}
	unlock, err := lockLocalMutation(ctx, abs)
	if err != nil {
		return "", err
	}
	defer unlock()
	perm := intent.Perm
	fi, err := os.Stat(abs)
	switch {
	case err == nil:
		if fi.IsDir() {
			return "", fmt.Errorf("%s is a directory", p)
		}
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("%s is not a regular file", p)
		}
		if intent.CreateIfAbsent {
			return "", ErrExists
		}
		if intent.IfVersion != "" {
			current, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			if versionOf(current) != intent.IfVersion {
				return "", ErrStale
			}
		}
		if perm == 0 {
			perm = fi.Mode().Perm()
		}
	case errors.Is(err, fs.ErrNotExist):
		if intent.IfVersion != "" {
			return "", ErrStale
		}
		if perm == 0 {
			perm = 0o644
		}
	default:
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := publishLocalFile(ctx, abs, data, perm, intent.CreateIfAbsent); err != nil {
		return "", err
	}
	return versionOf(data), nil
}

// publishLocalFile 在同目录写完并同步临时文件后发布，读者不会看到被截断或只写了一部分的文件。
// 创建用硬链接原子地判定目标不存在，替换用 rename；发布前失败保留原文件。
func publishLocalFile(ctx context.Context, abs string, data []byte, perm fs.FileMode, create bool) error {
	f, err := os.CreateTemp(filepath.Dir(abs), ".goagent-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if create {
		if err := os.Link(name, abs); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return ErrExists
			}
			return err
		}
		if err := os.Remove(name); err != nil {
			return err
		}
	} else if err := os.Rename(name, abs); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(abs))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

// Remove 实现 FileSystem。删除的是工作区内的条目本身：路径是符号链接时删除链接。
func (l *LocalFS) Remove(ctx context.Context, p string) error {
	if p == "" {
		return errors.New("path is required")
	}
	// 只解析父目录：最后一段是链接时删除链接，而不是链接目标。
	full := p
	if !filepath.IsAbs(full) {
		full = filepath.Join(l.root, filepath.FromSlash(p))
	}
	full = filepath.Clean(full)
	parent, _, err := l.abs(filepath.Dir(full))
	if err != nil {
		return err
	}
	target := filepath.Join(parent, filepath.Base(full))
	if r, err := filepath.Rel(l.root, target); err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return ErrOutsideWorkspace
	}
	unlock, err := lockLocalMutation(ctx, target)
	if err != nil {
		return err
	}
	defer unlock()
	return os.Remove(target)
}

// Walk 实现 FileSystem。
func (l *LocalFS) Walk(ctx context.Context, p string, fn func(Info) error) error {
	base, _, err := l.abs(p)
	if err != nil {
		return err
	}
	return filepath.WalkDir(base, func(abs string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			// 无权限等不可读的条目跳过，不影响其余结果。
			if d != nil && d.IsDir() && abs != base {
				return fs.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		r, _ := filepath.Rel(l.root, abs)
		return fn(infoOf(filepath.ToSlash(r), fi))
	})
}
