//go:build linux

package skills

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDiscoverListedDirectoryReplacedByFIFO(t *testing.T) {
	base := t.TempDir()
	name := filepath.Join(base, "demo")
	require.NoError(t, os.Mkdir(name, 0o700))
	root, err := os.OpenRoot(base)
	require.NoError(t, err)
	defer root.Close()
	listed, err := os.ReadDir(base)
	require.NoError(t, err)
	require.True(t, listed[0].IsDir())
	// Deterministically place the mutation between the IsDir check and the
	// retained-root acquisition used by Discover.
	require.NoError(t, os.Remove(name))
	require.NoError(t, syscall.Mkfifo(name, 0o600))
	done := make(chan error, 1)
	go func() {
		l, err := DiscoverOptions{}.resolve()
		require.NoError(t, err)
		total := l.totalBytes
		_, retained, err := discoverSkill(&rootSource{root: root}, listed[0].Name(), l, &total)
		if retained != nil {
			_ = retained.close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		writer, err := os.OpenFile(name, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_ = writer.Close()
		}
		<-done
		t.Fatal("retaining a replaced directory blocked waiting for FIFO writer")
	}
}
