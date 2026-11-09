//go:build linux

package skills_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/skills"
)

func TestCatalogRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	base := t.TempDir()
	dir := writeSkill(t, base, "demo", "demo", "body")
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "fifo"), 0600))
	c := discover(t, openRoot(t, base))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.ReadReference(ctx, "demo", "fifo"); done <- err }()
	select {
	case err := <-done:
		require.ErrorContains(t, err, "regular file")
	case <-ctx.Done():
		// Release a regressed blocking reader before cleanup closes the catalog.
		f, err := os.OpenFile(filepath.Join(dir, "fifo"), os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_ = f.Close()
		}
		t.Fatal("reference read waited for a FIFO writer")
	}
	require.NoError(t, os.Remove(filepath.Join(dir, "SKILL.md")))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "SKILL.md"), 0600))
	_, err := skills.Discover(context.Background(), openRoot(t, base))
	require.ErrorContains(t, err, "regular file")
}

func TestDiscoveryFailureDoesNotLeakHandles(t *testing.T) {
	base := t.TempDir()
	writeSkill(t, base, "a-valid", "valid", "body")
	broken := writeSkill(t, base, "z-invalid", "valid", "body")
	require.NoError(t, os.WriteFile(filepath.Join(broken, "SKILL.md"), []byte("invalid"), 0600))
	root := openRoot(t, base)
	before, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	for range 10 {
		_, err := skills.Discover(context.Background(), root)
		require.Error(t, err)
	}
	after, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	require.LessOrEqual(t, len(after), len(before), "failed discovery must release all child handles")
}
