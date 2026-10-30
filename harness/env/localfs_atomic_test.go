package env

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 已打开的读者继续看到完整旧版本，路径上的新读者看到完整新版本。
func TestLocalFSReplacesWithoutTruncatingReaders(t *testing.T) {
	l := newLocal(t)
	put(t, l, "file", "原始内容")
	f, err := os.Open(filepath.Join(l.Root(), "file"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	_, err = l.WriteFile(context.Background(), "file", []byte("新内容"), WriteIntent{})
	require.NoError(t, err)
	old, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "原始内容", string(old))
	require.Equal(t, "新内容", get(t, l, "file"))
	entries, err := os.ReadDir(l.Root())
	require.NoError(t, err)
	require.Len(t, entries, 1, "成功后清理临时文件")
}

// 不同实例共享条件写入协调；同一个版本或不存在的路径只能有一个竞争者成功。
func TestLocalFSConditionalWritesAcrossInstances(t *testing.T) {
	for _, create := range []bool{false, true} {
		t.Run(fmt.Sprintf("create=%t", create), func(t *testing.T) {
			l := newLocal(t)
			intent := WriteIntent{CreateIfAbsent: true}
			conflict := ErrExists
			if !create {
				put(t, l, "file", strings.Repeat("a", 1<<20))
				_, v, err := l.ReadFile(context.Background(), "file")
				require.NoError(t, err)
				intent = WriteIntent{IfVersion: v}
				conflict = ErrStale
			}
			const writers = 16
			start := make(chan struct{})
			type result struct {
				content string
				err     error
			}
			done := make(chan result, writers)
			for i := range writers {
				other, err := NewLocalFS(l.Root())
				require.NoError(t, err)
				go func() {
					<-start
					content := strings.Repeat(fmt.Sprintf("%02d", i), 1<<19)
					_, err := other.WriteFile(context.Background(), "file", []byte(content), intent)
					done <- result{content, err}
				}()
			}
			close(start)
			var winners []string
			for range writers {
				r := <-done
				if r.err == nil {
					winners = append(winners, r.content)
				} else {
					require.ErrorIs(t, r.err, conflict)
				}
			}
			require.Len(t, winners, 1)
			require.Equal(t, winners[0], get(t, l, "file"))
		})
	}
}

func TestLocalFSCancelledMutationLeavesFileUnchanged(t *testing.T) {
	l := newLocal(t)
	put(t, l, "file", "原始内容")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := l.WriteFile(ctx, "file", []byte("新内容"), WriteIntent{})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, l.Remove(ctx, "file"), context.Canceled)
	require.Equal(t, "原始内容", get(t, l, "file"))
}
