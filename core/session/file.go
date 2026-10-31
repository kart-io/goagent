package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/kart-io/goagent/core/durable"
)

const fileVersion = "goagent-session-v1"

// fileHeader 是会话文件的第一行。
type fileHeader struct {
	Session string `json:"session"`
	Version string `json:"version"`
}

// FileStore 把每个会话存为一个 JSONL 文件：第一行是文件头，之后每行是一次 Append 的全部事件（JSON 数组），
// 因此一次 Append 要么整行可读、要么整行不存在。末尾不完整的一行（写入中崩溃）在读取时忽略，下次 Append 前截掉。
// 追加在会话级 flock 内完成并 fsync；Read 不加锁。同 durable.FileStore，须使用锁与 rename 语义可靠的本地文件系统。
type FileStore struct {
	dir string

	mu    sync.Mutex
	cache map[string]readPos
}

// readPos 记录上次读到的文件末尾，使"读取上次之后的事件"只读新增部分。
type readPos struct {
	offset int64
	seq    int64
}

// NewFileStore 在 dir 下保存会话，目录不存在时创建。
func NewFileStore(dir string) (*FileStore, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, err
	}
	return &FileStore{dir: path, cache: map[string]readPos{}}, nil
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (s *FileStore) path(id string) string { return filepath.Join(s.dir, digest(id)+".jsonl") }

func lockFile(ctx context.Context, path string) (func() error, error) {
	lock := flock.New(path)
	ok, err := lock.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %w", durable.ErrLockUnavailable, ctx.Err())
		}
		return nil, err
	}
	if !ok {
		return nil, durable.ErrLockUnavailable
	}
	return lock.Close, nil
}

// Lock 实现 Store。
func (s *FileStore) Lock(ctx context.Context, id, name string) (func() error, error) {
	return lockFile(ctx, filepath.Join(s.dir, digest(id)+"."+digest(name)+".lock"))
}

// scan 读取文件中 start 偏移之后的完整行。返回事件、最后一个完整行的结束偏移，以及文件头（start 为 0 时）。
func scan(r io.Reader, start int64, id string) (events []Event, end int64, err error) {
	br := bufio.NewReaderSize(r, 64*1024)
	end = start
	for {
		line, err := br.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			// 没有换行的末尾是写入中崩溃留下的不完整行。
			return events, end, nil
		}
		if err != nil {
			return nil, 0, err
		}
		if end == 0 {
			var h fileHeader
			if json.Unmarshal(line, &h) != nil || h.Version != fileVersion || h.Session != id {
				return nil, 0, fmt.Errorf("会话文件头无效")
			}
		} else {
			var batch []Event
			if err := json.Unmarshal(line, &batch); err != nil {
				return nil, 0, fmt.Errorf("会话文件在偏移 %d 处损坏: %w", end, err)
			}
			events = append(events, batch...)
		}
		end += int64(len(line))
	}
}

// load 读取会话中序号大于 after 的事件，返回最后序号与文件中完整内容的结束偏移。
func (s *FileStore) load(id string, after int64) (events []Event, last, end int64, err error) {
	f, err := os.Open(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, 0, err
	}
	defer func() { _ = f.Close() }()
	s.mu.Lock()
	pos, cached := s.cache[id]
	s.mu.Unlock()
	start := int64(0)
	if cached && pos.seq <= after {
		if info, err := f.Stat(); err == nil && info.Size() >= pos.offset && validBoundary(f, pos.offset) {
			start = pos.offset
		}
	}
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return nil, 0, 0, err
		}
	}
	all, end, err := scan(f, start, id)
	if err != nil {
		return nil, 0, 0, err
	}
	last = pos.seq
	if start == 0 {
		last = 0
	}
	if len(all) > 0 {
		last = all[len(all)-1].Seq
	}
	s.mu.Lock()
	s.cache[id] = readPos{offset: end, seq: last}
	s.mu.Unlock()
	n := sort.Search(len(all), func(i int) bool { return all[i].Seq > after })
	return all[n:], last, end, nil
}

// validBoundary 报告 offset 处是否紧跟在一个换行之后（缓存的位置仍是完整行的边界）。
func validBoundary(f *os.File, offset int64) bool {
	if offset == 0 {
		return true
	}
	b := make([]byte, 1)
	_, err := f.ReadAt(b, offset-1)
	return err == nil && b[0] == '\n'
}

// Read 实现 Store。
func (s *FileStore) Read(ctx context.Context, id string, after int64) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	events, _, _, err := s.load(id, after)
	return events, err
}

// Append 实现 Store。
func (s *FileStore) Append(ctx context.Context, id string, expected int64, events ...Event) (last int64, err error) {
	if err := validID(id); err != nil {
		return 0, err
	}
	unlock, err := lockFile(ctx, s.path(id)+".lock")
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	_, cur, end, err := s.load(id, 1<<62)
	created := errors.Is(err, ErrNotFound)
	if err != nil && !created {
		return 0, err
	}
	if cur != expected {
		return 0, fmt.Errorf("%w：期望序号 %d，当前 %d", ErrConflict, expected, cur)
	}
	if len(events) == 0 {
		return cur, nil
	}
	var buf bytes.Buffer
	if created {
		header, _ := json.Marshal(fileHeader{Session: id, Version: fileVersion})
		buf.Write(append(header, '\n'))
	}
	batch, err := json.Marshal(assign(events, expected))
	if err != nil {
		return 0, err
	}
	buf.Write(append(batch, '\n'))
	f, err := os.OpenFile(s.path(id), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	// 截掉末尾不完整的行，或上次失败的写入留下的残余。
	if err := f.Truncate(end); err != nil {
		return 0, err
	}
	if _, err := f.WriteAt(buf.Bytes(), end); err != nil {
		_ = f.Truncate(end)
		return 0, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Truncate(end)
		return 0, err
	}
	if created {
		if err := syncDir(s.dir); err != nil {
			return 0, err
		}
	}
	last = expected + int64(len(events))
	s.mu.Lock()
	s.cache[id] = readPos{offset: end + int64(buf.Len()), seq: last}
	s.mu.Unlock()
	return last, nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// List 实现 Store。
func (s *FileStore) List(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		f, err := os.Open(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, err
		}
		line, err := bufio.NewReader(f).ReadBytes('\n')
		_ = f.Close()
		var h fileHeader
		if err != nil || json.Unmarshal(line, &h) != nil || h.Version != fileVersion {
			continue
		}
		ids = append(ids, h.Session)
	}
	sort.Strings(ids)
	return ids, nil
}

// Fork 实现 Store：在 dst 的会话锁内写临时文件后 rename，dst 要么完整出现、要么不存在。
func (s *FileStore) Fork(ctx context.Context, src, dst string, upTo int64) (err error) {
	if err := validID(dst); err != nil {
		return err
	}
	unlock, err := lockFile(ctx, s.path(dst)+".lock")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if _, err := os.Stat(s.path(dst)); err == nil {
		return ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	events, err := s.Read(ctx, src, 0)
	if err != nil {
		return err
	}
	events = forkRange(events, upTo)
	var buf bytes.Buffer
	header, _ := json.Marshal(fileHeader{Session: dst, Version: fileVersion})
	buf.Write(append(header, '\n'))
	if len(events) > 0 {
		batch, err := json.Marshal(events)
		if err != nil {
			return err
		}
		buf.Write(append(batch, '\n'))
	}
	tmp, err := os.CreateTemp(s.dir, ".fork-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
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
	if err := os.Rename(name, s.path(dst)); err != nil {
		return err
	}
	return syncDir(s.dir)
}
