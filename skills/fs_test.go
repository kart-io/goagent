package skills_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/skills"
)

func skillFile(name, description, body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("---\nname: " + name + "\ndescription: " + description + "\n---\n" + body)}
}

func TestDiscoverFSMatchesDiscover(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dir := writeSkill(t, base, "demo", "first", "instructions")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "references"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "references", "guide.md"), []byte("guide"), 0o600))
	writeSkill(t, base, "another", "second", "other")
	root := openRoot(t, base)

	want := discover(t, root)
	got, err := skills.DiscoverFS(ctx, root.FS(), root.Name(), skills.DiscoverOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, got.Close()) })

	require.Equal(t, want.List(), got.List())
	for _, m := range want.List() {
		ws, err := want.Load(ctx, m.Name)
		require.NoError(t, err)
		gs, err := got.Load(ctx, m.Name)
		require.NoError(t, err)
		require.Equal(t, ws, gs)
	}
	data, err := got.ReadReference(ctx, "demo", "references/guide.md")
	require.NoError(t, err)
	require.Equal(t, "guide", string(data))
}

func TestDiscoverFSRejectsPathEscape(t *testing.T) {
	ctx := context.Background()
	base, outside := t.TempDir(), t.TempDir()
	dir := writeSkill(t, base, "demo", "demo", "body")
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "link")))

	for name, fsys := range map[string]fs.FS{
		"os.Root.FS": openRoot(t, base).FS(),
		"MapFS": fstest.MapFS{
			"demo/SKILL.md": skillFile("demo", "demo", "body"),
			"secret":        {Data: []byte("secret")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := discoverFS(t, fsys, "src")
			for _, file := range []string{"../secret", "../../secret", "/etc/passwd", "a/../../secret", `..\secret`} {
				_, err := c.ReadReference(ctx, "demo", file)
				require.Error(t, err, file)
			}
		})
	}
	// os.Root.FS enforces the root boundary for symlinks itself.
	c := discoverFS(t, openRoot(t, base).FS(), "src")
	_, err := c.ReadReference(ctx, "demo", "link")
	require.Error(t, err)
}

func discoverFS(t *testing.T, fsys fs.FS, source string) *skills.Catalog {
	t.Helper()
	c, err := skills.DiscoverFS(context.Background(), fsys, source, skills.DiscoverOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}

func TestDiscoverFSEmbedStyleSource(t *testing.T) {
	fsys := fstest.MapFS{
		"b/SKILL.md":        skillFile("b", "bee", "bbb"),
		"a/SKILL.md":        skillFile("a", "ay", "aaa"),
		"a/refs/x.md":       {Data: []byte("x")},
		"notes.txt":         {Data: []byte("ignored")},
		"empty/keep":        {Data: []byte("no skill")},
		"nested/c/SKILL.md": skillFile("c", "see", "ccc"),
	}
	c := discoverFS(t, fsys, "embedded")
	list := c.List()
	require.Len(t, list, 2)
	require.Equal(t, "a", list[0].Name)
	require.Equal(t, "embedded/a/SKILL.md", list[0].Source)
	s, err := c.Load(context.Background(), "a")
	require.NoError(t, err)
	require.Equal(t, []string{"refs/x.md"}, s.Files)
}

func TestDiscoverOptionsOverrideAndUnlimited(t *testing.T) {
	ctx := context.Background()
	big := strings.Repeat("x", constants.SkillMaxDocumentBytes+1)
	fsys := fstest.MapFS{"demo/SKILL.md": skillFile("demo", "demo", big)}

	_, err := skills.DiscoverFS(ctx, fsys, "s", skills.DiscoverOptions{})
	require.ErrorContains(t, err, "byte limit", "zero keeps the default")
	c, err := skills.DiscoverFS(ctx, fsys, "s", skills.DiscoverOptions{MaxFileBytes: skills.Unlimited})
	require.NoError(t, err)
	require.NoError(t, c.Close())
	c, err = skills.DiscoverFS(ctx, fsys, "s", skills.DiscoverOptions{MaxFileBytes: constants.SkillMaxDocumentBytes + 1024})
	require.NoError(t, err)
	require.NoError(t, c.Close())

	small := fstest.MapFS{"demo/SKILL.md": skillFile("demo", "demo", "0123456789")}
	_, err = skills.DiscoverFS(ctx, small, "s", skills.DiscoverOptions{MaxFileBytes: 20})
	require.ErrorContains(t, err, "byte limit", "caller cap tighter than the default")

	t.Run("description", func(t *testing.T) {
		long := fstest.MapFS{"demo/SKILL.md": skillFile("demo", strings.Repeat("d", 1025), "b")}
		_, err := skills.DiscoverFS(ctx, long, "s", skills.DiscoverOptions{})
		require.Error(t, err)
		for _, opts := range []skills.DiscoverOptions{{MaxDescription: 2000}, {MaxDescription: skills.Unlimited}} {
			c, err := skills.DiscoverFS(ctx, long, "s", opts)
			require.NoError(t, err)
			require.NoError(t, c.Close())
		}
		_, err = skills.DiscoverFS(ctx, small, "s", skills.DiscoverOptions{MaxDescription: 3})
		require.Error(t, err)
	})

	t.Run("total bytes", func(t *testing.T) {
		two := fstest.MapFS{
			"a/SKILL.md": skillFile("a", "a", strings.Repeat("x", 100)),
			"b/SKILL.md": skillFile("b", "b", strings.Repeat("x", 100)),
		}
		_, err := skills.DiscoverFS(ctx, two, "s", skills.DiscoverOptions{MaxTotalBytes: 250})
		require.ErrorContains(t, err, "byte limit")
		c, err := skills.DiscoverFS(ctx, two, "s", skills.DiscoverOptions{MaxTotalBytes: 1000})
		require.NoError(t, err)
		require.NoError(t, c.Close())
	})

	t.Run("files and depth", func(t *testing.T) {
		tree := fstest.MapFS{
			"demo/SKILL.md": skillFile("demo", "demo", "b"),
			"demo/a/b/c/f":  {Data: []byte("f")},
			"demo/g":        {Data: []byte("g")},
		}
		few := discoverFSWith(t, tree, skills.DiscoverOptions{MaxFiles: 2})
		_, err := few.Load(ctx, "demo")
		require.ErrorContains(t, err, "entry limit")
		_, err = skills.DiscoverFS(ctx, fstest.MapFS{
			"a/SKILL.md": skillFile("a", "a", "b"), "b/SKILL.md": skillFile("b", "b", "b"), "c/SKILL.md": skillFile("c", "c", "b"),
		}, "s", skills.DiscoverOptions{MaxFiles: 2})
		require.ErrorContains(t, err, "entry limit")
		c, err := skills.DiscoverFS(ctx, tree, "s", skills.DiscoverOptions{MaxDepth: 1})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, c.Close()) })
		_, err = c.Load(ctx, "demo")
		require.ErrorContains(t, err, "depth limit")
		deep := discoverFSWith(t, tree, skills.DiscoverOptions{MaxDepth: skills.Unlimited, MaxFiles: skills.Unlimited})
		s, err := deep.Load(ctx, "demo")
		require.NoError(t, err)
		require.Equal(t, []string{"a/b/c/f", "g"}, s.Files)
	})

	t.Run("invalid negative", func(t *testing.T) {
		for _, opts := range []skills.DiscoverOptions{
			{MaxFileBytes: -2}, {MaxTotalBytes: -2}, {MaxFiles: -2}, {MaxDescription: -2}, {MaxDepth: -2},
		} {
			_, err := skills.DiscoverFS(ctx, small, "s", opts)
			require.Error(t, err, "%+v", opts)
		}
	})

	t.Run("DiscoverWithOptions", func(t *testing.T) {
		base := t.TempDir()
		writeSkill(t, base, "demo", "demo", big)
		_, err := skills.DiscoverWithOptions(ctx, skills.DiscoverOptions{}, openRoot(t, base))
		require.ErrorContains(t, err, "byte limit")
		c, err := skills.DiscoverWithOptions(ctx, skills.DiscoverOptions{MaxFileBytes: skills.Unlimited}, openRoot(t, base))
		require.NoError(t, err)
		require.NoError(t, c.Close())
	})
}

func discoverFSWith(t *testing.T, fsys fs.FS, opts skills.DiscoverOptions) *skills.Catalog {
	t.Helper()
	c, err := skills.DiscoverFS(context.Background(), fsys, "s", opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}

func TestCatalogSelect(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	for _, n := range []string{"alpha", "beta", "gamma"} {
		dir := writeSkill(t, base, n, n+" skill", n+" body")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ref"), []byte(n), 0o600))
	}
	parent := discover(t, openRoot(t, base))

	_, err := parent.Select("alpha", "missing")
	require.ErrorIs(t, err, fs.ErrNotExist)

	sub, err := parent.Select("gamma", "alpha")
	require.NoError(t, err)
	names := []string{}
	for _, m := range sub.List() {
		names = append(names, m.Name)
	}
	require.Equal(t, []string{"alpha", "gamma"}, names)
	s, err := sub.Load(ctx, "alpha")
	require.NoError(t, err)
	require.Equal(t, "alpha body", s.Body)
	data, err := sub.ReadReference(ctx, "gamma", "ref")
	require.NoError(t, err)
	require.Equal(t, "gamma", string(data))
	_, err = sub.Load(ctx, "beta")
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = sub.ReadReference(ctx, "beta", "ref")
	require.ErrorIs(t, err, fs.ErrNotExist)

	empty, err := parent.Select()
	require.NoError(t, err)
	require.Empty(t, empty.List())

	require.NoError(t, sub.Close())
	_, err = parent.Load(ctx, "beta")
	require.NoError(t, err, "closing a subset must not close the parent")
	_, err = sub.Load(ctx, "alpha")
	require.NoError(t, err, "subset Close is a no-op")

	subOfSub, err := sub.Select("alpha")
	require.NoError(t, err)
	_, err = subOfSub.Select("gamma")
	require.ErrorIs(t, err, fs.ErrNotExist)

	require.NoError(t, parent.Close())
	_, err = sub.Load(ctx, "alpha")
	require.ErrorIs(t, err, fs.ErrClosed)
	_, err = subOfSub.ReadReference(ctx, "alpha", "ref")
	require.ErrorIs(t, err, fs.ErrClosed)
	_, err = parent.Select("alpha")
	require.ErrorIs(t, err, fs.ErrClosed)
}

func TestCatalogPromptSkipsHiddenAndSorts(t *testing.T) {
	fsys := fstest.MapFS{
		"zeta/SKILL.md":  skillFile("zeta", "last one", "z"),
		"mid/SKILL.md":   {Data: []byte("---\nname: mid\ndescription: hidden\ndisable-model-invocation: true\n---\nm")},
		"alpha/SKILL.md": skillFile("alpha", "first one", "a"),
	}
	c := discoverFS(t, fsys, "s")
	require.Equal(t, "alpha: first one\nzeta: last one", c.Prompt())

	sub, err := c.Select("zeta", "mid")
	require.NoError(t, err)
	require.Equal(t, "zeta: last one", sub.Prompt())

	onlyHidden, err := c.Select("mid")
	require.NoError(t, err)
	require.Empty(t, onlyHidden.Prompt())
	empty, err := c.Select()
	require.NoError(t, err)
	require.Empty(t, empty.Prompt())
}
