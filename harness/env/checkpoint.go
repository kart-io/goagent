package env

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"sync"
)

// Change 是经 Checkpointed 改动过的一个文件。
type Change struct {
	Path string
	// Created 表示文件由本次会话新建，回滚时删除。
	Created bool
}

type original struct {
	existed bool
	content []byte
	perm    fs.FileMode
}

// Checkpointed 在每个文件首次被写入或删除前保存原始状态，可列出改动并回滚。
// 只记录经它写入的改动；命令对文件的修改不在记录内。
type Checkpointed struct {
	FileSystem
	mu        sync.Mutex
	originals map[string]original
}

// NewCheckpointed 包装 base。
func NewCheckpointed(base FileSystem) *Checkpointed {
	return &Checkpointed{FileSystem: base, originals: map[string]original{}}
}

// snapshot 读取文件当前状态，调用方须持有 mu。
func (c *Checkpointed) snapshot(ctx context.Context, rel string) (original, bool, error) {
	if _, ok := c.originals[rel]; ok {
		return original{}, false, nil
	}
	info, err := c.Stat(ctx, rel)
	if errors.Is(err, fs.ErrNotExist) {
		return original{}, true, nil
	}
	if err != nil {
		return original{}, false, err
	}
	if info.IsDir() {
		return original{}, false, fmt.Errorf("%s is a directory", rel)
	}
	data, _, err := c.ReadFile(ctx, rel)
	if err != nil {
		return original{}, false, err
	}
	return original{existed: true, content: data, perm: info.Mode.Perm()}, true, nil
}

// WriteFile 写入成功后记录首次修改前的状态；写入失败不留记录。
func (c *Checkpointed) WriteFile(ctx context.Context, p string, data []byte, intent WriteIntent) (Version, error) {
	rel, err := c.Resolve(p)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	o, record, err := c.snapshot(ctx, rel)
	if err != nil {
		return "", err
	}
	v, err := c.FileSystem.WriteFile(ctx, rel, data, intent)
	if err != nil {
		return "", err
	}
	if record {
		c.originals[rel] = o
	}
	return v, nil
}

// Remove 删除成功后记录原始状态；文件原本不存在时删除即复原，记录随之清除。
// 被删除的符号链接回滚时恢复为普通文件。
func (c *Checkpointed) Remove(ctx context.Context, p string) error {
	rel, err := entryPath(c, p)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	o, record, err := c.snapshot(ctx, rel)
	if err != nil {
		return err
	}
	if err := c.FileSystem.Remove(ctx, rel); err != nil {
		return err
	}
	if record {
		c.originals[rel] = o
	}
	if !c.originals[rel].existed {
		delete(c.originals, rel)
	}
	return nil
}

// Changes 返回改动过的文件，按路径排序。
func (c *Checkpointed) Changes() []Change {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Change, 0, len(c.originals))
	for rel, o := range c.originals {
		out = append(out, Change{Path: rel, Created: !o.existed})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Revert 把文件恢复到首次修改前的状态：原有文件写回原内容与权限位，新建的文件被删除；不传 paths 时回滚全部。
// 路径解析与越界检查由底层 FileSystem 负责。单个文件失败不影响其余文件，失败的文件保留在 Changes 中。
// 回滚绕过外层的 Observed，之后修改这些文件须重新读取。
func (c *Checkpointed) Revert(ctx context.Context, paths ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var errs []error
	targets := map[string]bool{}
	if len(paths) == 0 {
		for rel := range c.originals {
			targets[rel] = true
		}
	}
	for _, p := range paths {
		rel, err := c.Resolve(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		if _, ok := c.originals[rel]; !ok {
			errs = append(errs, fmt.Errorf("%s has no recorded changes", rel))
			continue
		}
		targets[rel] = true
	}
	for rel := range targets {
		o := c.originals[rel]
		var err error
		if o.existed {
			_, err = c.FileSystem.WriteFile(ctx, rel, o.content, WriteIntent{Perm: o.perm})
		} else if err = c.FileSystem.Remove(ctx, rel); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("revert %s: %w", rel, err))
			continue
		}
		delete(c.originals, rel)
	}
	return errors.Join(errs...)
}
