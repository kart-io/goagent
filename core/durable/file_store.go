package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
