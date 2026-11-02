// Package txnlog 用提交日志实现目录内多个文件的原子更新：提交文件 rename 到位即提交，之后崩溃也会被前滚。
package txnlog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const commitExt = ".commit"

// AfterCommit 仅供测试：提交文件落盘后、应用前调用，用于模拟进程在此处崩溃。
var AfterCommit func()

// BeforeCommit 仅供测试：提交文件 rename 之前调用，用于模拟提交点之前的崩溃。
var BeforeCommit func()

// Log 是 parent 目录的提交日志。调用方须在读写目标文件前调用 Recover，否则可能读到已提交但未应用的旧内容。
type Log struct {
	parent string
	dir    string
}

// New 不做任何文件操作；parent 须在首次 Commit 前存在。
func New(parent string) *Log {
	return &Log{parent: parent, dir: filepath.Join(parent, ".txn")}
}

func (l *Log) lock(ctx context.Context) (func() error, error) {
	lock := flock.New(filepath.Join(l.parent, ".txn.lock"))
	ok, err := lock.TryLockContext(ctx, 5*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("提交日志锁不可用")
	}
	return lock.Close, nil
}

// pending 不加锁检查是否有文件，常见的空目录路径只有一次系统调用。
func (l *Log) pending() ([]os.DirEntry, error) {
	entries, err := os.ReadDir(l.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}

// Recover 前滚崩溃留下的提交。apply 须幂等：提交可能已部分或全部应用。
func (l *Log) Recover(ctx context.Context, apply func([]byte) error) (err error) {
	entries, err := l.pending()
	if err != nil || len(entries) == 0 {
		return err
	}
	unlock, err := l.lock(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	return l.recoverLocked(apply)
}

func (l *Log) recoverLocked(apply func([]byte) error) error {
	entries, err := l.pending()
	if err != nil {
		return err
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, entry := range entries {
		path := filepath.Join(l.dir, entry.Name())
		if !strings.HasSuffix(entry.Name(), commitExt) {
			// 提交点之前崩溃留下的临时文件，没有生效。
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := apply(payload); err != nil {
			return fmt.Errorf("前滚提交 %s: %w", entry.Name(), err)
		}
		if err := l.remove(path); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) remove(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDir(l.dir)
}

// Commit 持久写入 payload 后调用 apply。提交点之后 apply 失败时提交日志保留，后续 Recover 继续前滚，
// 错误表示结果尚未全部可见，而不是没有生效。
func (l *Log) Commit(ctx context.Context, payload []byte, apply func([]byte) error) (err error) {
	unlock, err := l.lock(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if err := l.recoverLocked(apply); err != nil {
		return err
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	if err := syncDir(l.parent); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(l.dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if BeforeCommit != nil {
		BeforeCommit()
	}
	path := name + commitExt
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if err := syncDir(l.dir); err != nil {
		return err
	}
	if AfterCommit != nil {
		AfterCommit()
	}
	if err := apply(payload); err != nil {
		return fmt.Errorf("已提交但尚未应用完成: %w", err)
	}
	return l.remove(path)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
