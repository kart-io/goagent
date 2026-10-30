package env

import (
	"context"
	"errors"
	"io/fs"
	"sync"
)

// Observed 在 FileSystem 上实施"先读后改"：修改已存在的文件前必须经 ReadFile 读过，
// 写入时以读到的版本做条件写入，读取后被外部改动则返回 ErrStale；新建文件以 CreateIfAbsent 写入。
// 自己写入的内容视为已读。
type Observed struct {
	FileSystem
	mu   sync.Mutex
	seen map[string]Version
}

// NewObserved 包装 base。
func NewObserved(base FileSystem) *Observed {
	return &Observed{FileSystem: base, seen: map[string]Version{}}
}

// ReadFile 读取并记录版本。
func (o *Observed) ReadFile(ctx context.Context, p string) ([]byte, Version, error) {
	rel, err := o.Resolve(p)
	if err != nil {
		return nil, "", err
	}
	data, v, err := o.FileSystem.ReadFile(ctx, rel)
	if err != nil {
		return nil, "", err
	}
	o.mu.Lock()
	o.seen[rel] = v
	o.mu.Unlock()
	return data, v, nil
}

// Peek 读取但不记录，用于编辑前取得当前内容；随后的 WriteFile 仍以读过的版本做条件写入。
func (o *Observed) Peek(ctx context.Context, p string) ([]byte, Version, error) {
	return o.FileSystem.ReadFile(ctx, p)
}

// WriteFile 按观察记录约束写入。intent 中的条件被观察记录取代，Perm 保留。
func (o *Observed) WriteFile(ctx context.Context, p string, data []byte, intent WriteIntent) (Version, error) {
	rel, err := o.Resolve(p)
	if err != nil {
		return "", err
	}
	_, err = o.Stat(ctx, rel)
	switch {
	case err == nil:
		o.mu.Lock()
		v, ok := o.seen[rel]
		o.mu.Unlock()
		if !ok {
			return "", ErrNotObserved
		}
		intent = WriteIntent{IfVersion: v, Perm: intent.Perm}
	case errors.Is(err, fs.ErrNotExist):
		intent = WriteIntent{CreateIfAbsent: true, Perm: intent.Perm}
	default:
		return "", err
	}
	v, err := o.FileSystem.WriteFile(ctx, rel, data, intent)
	if err != nil {
		return "", err
	}
	o.mu.Lock()
	o.seen[rel] = v
	o.mu.Unlock()
	return v, nil
}

// Remove 删除并清除观察记录。
func (o *Observed) Remove(ctx context.Context, p string) error {
	rel, err := entryPath(o, p)
	if err != nil {
		return err
	}
	if err := o.FileSystem.Remove(ctx, rel); err != nil {
		return err
	}
	o.Forget(rel)
	return nil
}

// Forget 清除路径的观察记录，之后修改须重新读取。
func (o *Observed) Forget(paths ...string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, p := range paths {
		delete(o.seen, p)
	}
}
