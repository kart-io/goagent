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
	"sync"
)

// LocalFS 是宿主机目录上的 FileSystem。每次操作都重新解析路径并检查仍在工作区内，路径已被换成指向工作区外的
// 符号链接时返回 ErrOutsideWorkspace；解析与打开之间仍有被替换的窗口，不能防御并发篡改工作区的进程。
type LocalFS struct {
	root string
	// mu 让版本检查与写入在本进程内原子，外部进程的并发修改由版本比对发现。
	mu sync.Mutex
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
		if _, lerr := os.Lstat(cur); lerr == nil {
			return "", fmt.Errorf("%s is a dangling symlink: %w", cur, ErrOutsideWorkspace)
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
	l.mu.Lock()
	defer l.mu.Unlock()
	abs, _, err := l.abs(p)
	if err != nil {
		return "", err
	}
	perm := intent.Perm
	fi, err := os.Stat(abs)
	switch {
	case err == nil:
		if fi.IsDir() {
			return "", fmt.Errorf("%s is a directory", p)
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
	if err := os.WriteFile(abs, data, perm); err != nil {
		return "", err
	}
	// WriteFile 只在新建时应用权限。
	if fi != nil && fi.Mode().Perm() != perm {
		if err := os.Chmod(abs, perm); err != nil {
			return "", err
		}
	}
	return versionOf(data), nil
}

// Remove 实现 FileSystem。删除的是工作区内的条目本身：路径是符号链接时删除链接。
func (l *LocalFS) Remove(ctx context.Context, p string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
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
