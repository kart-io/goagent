package skills_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/skills"
)

func writeSkill(t *testing.T, root, name, description, body string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+description+"\nmetadata: {version: '1'}\n---\n"+body), 0600))
	return dir
}

func openRoot(t *testing.T, path string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func discover(t *testing.T, roots ...*os.Root) *skills.Catalog {
	t.Helper()
	c, err := skills.Discover(context.Background(), roots...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}

func TestCatalogPrecedenceSnapshotsAndOwnership(t *testing.T) {
	ctx := context.Background()
	first, second := t.TempDir(), t.TempDir()
	dir := writeSkill(t, first, "demo", "first", "instructions")
	writeSkill(t, second, "demo", "second", "shadowed")
	writeSkill(t, second, "another", "another", "other")
	require.NoError(t, os.MkdirAll(filepath.Join(first, "empty"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(first, "README.md"), []byte("ignored"), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "references"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "references", "guide.md"), []byte("initial"), 0600))
	r1, r2 := openRoot(t, first), openRoot(t, second)
	c := discover(t, r1, r2)
	require.NoError(t, r1.Close())
	require.NoError(t, r2.Close())
	list := c.List()
	require.Len(t, list, 2)
	require.Equal(t, "demo", list[0].Name)
	require.Equal(t, "first", list[0].Description)
	require.Equal(t, filepath.Join(dir, "SKILL.md"), list[0].Source)
	list[0].Name = "changed"
	list[0].Attributes["version"] = "999"
	s, err := c.Load(ctx, "demo")
	require.NoError(t, err)
	require.Equal(t, "1", s.Metadata.Version)
	require.Equal(t, "1", s.Metadata.Attributes["version"])
	require.Equal(t, "instructions", s.Body)
	require.Equal(t, []string{"references/guide.md"}, s.Files)
	s.Metadata.Attributes["version"] = "also changed"
	require.Equal(t, "1", c.List()[0].Attributes["version"])
	writeSkill(t, first, "demo", "new description", "new instructions")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "references", "guide.md"), []byte("updated"), 0600))
	s, err = c.Load(ctx, "demo")
	require.NoError(t, err)
	require.Equal(t, "instructions", s.Body, "document is cached")
	data, err := c.ReadReference(ctx, "demo", "references/guide.md")
	require.NoError(t, err)
	require.Equal(t, "updated", string(data), "resources are read on demand")
	for _, file := range []string{"./references/guide.md", "././references/guide.md", "references/./guide.md", "references//guide.md"} {
		data, err := c.ReadReference(ctx, "demo", file)
		require.NoError(t, err)
		require.Equal(t, "updated", string(data))
	}
	_, err = c.Load(ctx, "missing")
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.NoError(t, c.Close())
	require.NoError(t, c.Close())
	_, err = c.Load(ctx, "demo")
	require.ErrorIs(t, err, fs.ErrClosed)
	_, err = c.ReadReference(ctx, "demo", "references/guide.md")
	require.ErrorIs(t, err, fs.ErrClosed)
	require.Len(t, c.List(), 2, "metadata remains inspectable after close")
}

func TestCatalogConfinesReferencesAndRetainsDirectory(t *testing.T) {
	ctx := context.Background()
	base, outside := t.TempDir(), t.TempDir()
	dir := writeSkill(t, base, "demo", "demo", "read reference.txt")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reference.txt"), []byte("inside"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0600))
	require.NoError(t, os.Symlink("reference.txt", filepath.Join(dir, "safe-link")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "bad-link")))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "bad-dir")))
	c := discover(t, openRoot(t, base))
	data, err := c.ReadReference(ctx, "demo", "safe-link")
	require.NoError(t, err)
	require.Equal(t, "inside", string(data))
	for _, file := range []string{"", ".", "./", "./../demo/reference.txt", ".//etc/passwd", "../demo/reference.txt", "references/../reference.txt", "bad-link", "bad-dir/secret.txt", filepath.Join(outside, "secret.txt"), `..\secret.txt`, "reference.txt\x00"} {
		_, err := c.ReadReference(ctx, "demo", file)
		require.Error(t, err, file)
	}
	s, err := c.Load(ctx, "demo")
	require.NoError(t, err)
	require.NotContains(t, s.Files, "bad-dir/secret.txt", "listing must not follow directory symlinks")
	require.NoError(t, os.Rename(dir, dir+"-old"))
	require.NoError(t, os.Symlink(outside, dir))
	data, err = c.ReadReference(ctx, "demo", "reference.txt")
	require.NoError(t, err)
	require.Equal(t, "inside", string(data))
}

func TestDiscoverRejectsInvalidMetadataAndSkipsDirectorySymlinks(t *testing.T) {
	ctx := context.Background()
	base, outside := t.TempDir(), t.TempDir()
	out := writeSkill(t, outside, "outside", "outside", "secret")
	require.NoError(t, os.Symlink(out, filepath.Join(base, "outside")))
	c := discover(t, openRoot(t, base))
	require.Empty(t, c.List())
	dir := writeSkill(t, base, "demo", "demo", "good")
	require.NoError(t, os.Remove(filepath.Join(dir, "SKILL.md")))
	require.NoError(t, os.Symlink(filepath.Join(out, "SKILL.md"), filepath.Join(dir, "SKILL.md")))
	_, err := skills.Discover(ctx, openRoot(t, base))
	require.Error(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "SKILL.md")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: mismatch\ndescription: useful\n---\n"), 0600))
	_, err = skills.Discover(ctx, openRoot(t, base))
	require.ErrorContains(t, err, "不一致")
	writeSkill(t, base, "demo", "demo", "valid")
	broken := writeSkill(t, outside, "demo", "demo", "body")
	require.NoError(t, os.WriteFile(filepath.Join(broken, "SKILL.md"), []byte("invalid"), 0600))
	r := openRoot(t, base)
	_, err = skills.Discover(ctx, r, openRoot(t, outside))
	require.Error(t, err, "invalid shadowed skills must be reported")
	_, err = r.Stat("demo")
	require.NoError(t, err, "failure must not close borrowed roots")
}

func TestCatalogContextAndConcurrentClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := skills.Discover(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = skills.Discover(context.Background(), nil)
	require.Error(t, err)
	base := t.TempDir()
	dir := writeSkill(t, base, "demo", "demo", "body")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ref"), []byte("data"), 0600))
	c := discover(t, openRoot(t, base))
	_, err = c.Load(ctx, "demo")
	require.ErrorIs(t, err, context.Canceled)
	_, err = c.ReadReference(ctx, "demo", "ref")
	require.ErrorIs(t, err, context.Canceled)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 10 {
				_, err := c.Load(context.Background(), "demo")
				if err != nil && !errors.Is(err, fs.ErrClosed) {
					t.Error(err)
				}
				_, err = c.ReadReference(context.Background(), "demo", "ref")
				if err != nil && !errors.Is(err, fs.ErrClosed) {
					t.Error(err)
				}
				list := c.List()
				list[0].Attributes["version"] = "caller copy"
			}
		})
	}
	require.NoError(t, c.Close())
	wg.Wait()
}

func TestCatalogFileAndTraversalLimits(t *testing.T) {
	base := t.TempDir()
	dir := writeSkill(t, base, "demo", "demo", "body")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big"), []byte(strings.Repeat("x", constants.SkillMaxReferenceBytes+1)), 0600))
	c := discover(t, openRoot(t, base))
	_, err := c.ReadReference(context.Background(), "demo", "big")
	require.ErrorContains(t, err, "byte limit")
	_, err = c.ReadReference(context.Background(), "demo", "SKILL.md/no-file")
	require.Error(t, err)
	nested := filepath.Join(dir, strings.Repeat("a/", constants.SkillMaxResourceDepth+1))
	require.NoError(t, os.MkdirAll(nested, 0700))
	_, err = c.ReadReference(context.Background(), "demo", "a")
	require.ErrorContains(t, err, "regular file")
	_, err = c.Load(context.Background(), "demo")
	require.ErrorContains(t, err, "depth limit")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(strings.Repeat("x", constants.SkillMaxDocumentBytes+1)), 0600))
	_, err = skills.Discover(context.Background(), openRoot(t, base))
	require.ErrorContains(t, err, "byte limit")
}
