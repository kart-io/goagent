package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// FileStore uses OS-backed per-run locks, fsync and same-directory atomic rename.
// Use a local filesystem with reliable file locks/rename semantics. It is not a
// distributed lease service; network filesystem guarantees must be verified.
type FileStore struct{ dir string }

func NewFileStore(dir string) (*FileStore, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	return &FileStore{dir: path}, nil
}
func (s *FileStore) path(id string) string { return filepath.Join(s.dir, digest(id)+".json") }
func (s *FileStore) Lock(ctx context.Context, id string) (func() error, error) {
	lock := flock.New(s.path(id) + ".lock")
	ok, err := lock.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("run lock unavailable")
	}
	return lock.Close, nil
}
func (s *FileStore) Load(ctx context.Context, id string) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, err
	}
	var snapshot Snapshot
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("corrupt checkpoint: %w", err)
	}
	return &snapshot, nil
}
func (s *FileStore) Save(ctx context.Context, id string, snapshot *Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.dir, ".checkpoint-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(name, s.path(id)); err != nil {
		return err
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

// fileScope 是 FileStore 的作用域：数据位于 scopes/<摘要>/，闸门文件 scopes/<摘要>.gate 位于目录之外。
// 写入方在闸门上持共享锁，RemoveScope 持排他锁写入墓碑后删除目录，因此两者在本机所有进程间互斥；
// 墓碑（非空闸门）在删除后保留，使迟到的写入被拒绝而不是重建目录，直到 PruneRemoved 将其回收。
type fileScope struct {
	files *FileStore
	gate  string
}

const (
	scopesDir = "scopes"
	gateExt   = ".gate"
)

var scopeTombstone = []byte("removed\n")

// afterTombstone 仅供测试：墓碑落盘后、删除目录前调用，用于模拟进程在此处崩溃。
var afterTombstone func()

func (s *FileStore) scope(name string) *fileScope {
	root := filepath.Join(s.dir, scopesDir)
	return &fileScope{files: &FileStore{dir: filepath.Join(root, digest(name))}, gate: filepath.Join(root, digest(name)+gateExt)}
}

// Scope 实现 Scoped。目录在首次写入时创建，Load 不存在的记录不产生任何文件；已删除作用域的 Load 返回 ErrScopeRemoved。
func (s *FileStore) Scope(name string) Store { return s.scope(name) }

// RemoveScope 实现 Scoped：等待进行中的写入结束，落盘墓碑，再删除作用域目录。
// 中途崩溃时墓碑已生效，重复调用可清理剩余数据。每个被删除的作用域保留一个墓碑文件（8 字节），由 PruneRemoved 回收。
func (s *FileStore) RemoveScope(ctx context.Context, name string) (err error) {
	scope := s.scope(name)
	gate, err := lockGate(ctx, scope.gate, false, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, gate.Close()) }()
	removed, err := scopeRemoved(gate)
	if err == nil && !removed {
		err = writeTombstone(scope.gate)
	}
	if err != nil {
		return err
	}
	if afterTombstone != nil {
		afterTombstone()
	}
	return os.RemoveAll(scope.files.dir)
}

// PruneRemoved 实现 Scoped：回收墓碑写入时间（闸门文件 mtime）早于 now-olderThan 的已删除作用域，
// 在闸门排他锁内删除残留目录和墓碑，与写入互斥。保证边界：阈值内的迟到写入仍被拒绝；
// 墓碑回收后同名作用域的迟到写入会成功并留下残留，olderThan 须大于最长的重试或恢复窗口。
func (s *FileStore) PruneRemoved(ctx context.Context, olderThan time.Duration) error {
	if olderThan < 0 {
		return fmt.Errorf("墓碑保留时长不能为负：%v", olderThan)
	}
	root := filepath.Join(s.dir, scopesDir)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-olderThan)
	var errs []error
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		name, ok := strings.CutSuffix(entry.Name(), gateExt)
		if !ok || entry.IsDir() {
			continue
		}
		// 预筛避免为未过期的闸门加锁；文件已被并发回收时跳过，加锁后再以持锁视图复核。
		if info, err := entry.Info(); err != nil || !expired(info, cutoff) {
			continue
		}
		errs = append(errs, pruneScope(ctx, filepath.Join(root, name), filepath.Join(root, entry.Name()), cutoff))
	}
	return errors.Join(errs...)
}

func expired(info os.FileInfo, cutoff time.Time) bool {
	return info.Size() > 0 && info.ModTime().Before(cutoff)
}

func pruneScope(ctx context.Context, dir, path string, cutoff time.Time) (err error) {
	gate, err := lockGate(ctx, path, false, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, gate.Close()) }()
	info, err := gate.Stat()
	if err != nil || !expired(info, cutoff) {
		return err
	}
	if err = os.RemoveAll(dir); err != nil {
		return err
	}
	return os.Remove(path)
}

// lockGate 在闸门上加锁并确认锁住的仍是路径上的当前文件：PruneRemoved 会删除闸门，
// 锁在已删除旧文件上的进程与新文件的持有者互不排斥，此时换新文件重试。create 为 false 时闸门不存在返回 os.ErrNotExist。
func lockGate(ctx context.Context, path string, shared, create bool) (*flock.Flock, error) {
	flag := os.O_RDONLY
	if create {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		flag |= os.O_CREATE
	}
	for {
		gate := flock.New(path, flock.SetFlag(flag))
		try := gate.TryLockContext
		if shared {
			try = gate.TryRLockContext
		}
		ok, err := try(ctx, 20*time.Millisecond)
		if err == nil && !ok {
			err = fmt.Errorf("scope lock unavailable")
		}
		if err != nil {
			return nil, err
		}
		held, err := gate.Stat()
		var current os.FileInfo
		if err == nil {
			current, err = os.Stat(path)
		}
		if err == nil && os.SameFile(held, current) {
			return gate, nil
		}
		if err = errors.Join(ignoreNotExist(err), gate.Close()); err != nil {
			return nil, err
		}
		if !create {
			return nil, os.ErrNotExist
		}
	}
}

func ignoreNotExist(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func scopeRemoved(gate *flock.Flock) (bool, error) {
	info, err := gate.Stat()
	if err != nil {
		return false, err
	}
	return info.Size() > 0, nil
}

func writeTombstone(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(scopeTombstone); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

// enter 在闸门上持共享锁并确认作用域未删除，返回的函数释放该锁。
func (s *fileScope) enter(ctx context.Context) (func() error, error) {
	gate, err := lockGate(ctx, s.gate, true, true)
	if err != nil {
		return nil, err
	}
	removed, err := scopeRemoved(gate)
	if err == nil && removed {
		err = ErrScopeRemoved
	}
	if err == nil {
		err = os.MkdirAll(s.files.dir, 0700)
	}
	if err != nil {
		return nil, errors.Join(err, gate.Close())
	}
	return gate.Close, nil
}

// Lock 在整个持锁期间同时持有闸门共享锁，RemoveScope 会等待其释放。
func (s *fileScope) Lock(ctx context.Context, id string) (func() error, error) {
	leave, err := s.enter(ctx)
	if err != nil {
		return nil, err
	}
	unlock, err := s.files.Lock(ctx, id)
	if err != nil {
		return nil, errors.Join(err, leave())
	}
	return func() error { return errors.Join(unlock(), leave()) }, nil
}

// Load 不加锁：Save 以原子 rename 写入，读到的总是完整快照。读前检查墓碑，使 RemoveScope 中途崩溃残留的数据不可读；
// 读后复核，使读取期间完成的 RemoveScope 同样生效。只 Stat 闸门，不产生任何文件。
func (s *fileScope) Load(ctx context.Context, id string) (*Snapshot, error) {
	if err := s.checkRemoved(); err != nil {
		return nil, err
	}
	snapshot, err := s.files.Load(ctx, id)
	if removed := s.checkRemoved(); removed != nil {
		return nil, removed
	}
	return snapshot, err
}

// checkRemoved 不加锁读取闸门：非空即墓碑，返回 ErrScopeRemoved；闸门不存在视为未删除。
func (s *fileScope) checkRemoved() error {
	info, err := os.Stat(s.gate)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return err
	case info.Size() > 0:
		return ErrScopeRemoved
	}
	return nil
}

func (s *fileScope) Save(ctx context.Context, id string, snapshot *Snapshot) (err error) {
	leave, err := s.enter(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, leave()) }()
	return s.files.Save(ctx, id, snapshot)
}
