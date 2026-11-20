package skills

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"

	agenterrors "github.com/kart-io/goagent/errors"
)

// DiscoverFS is Discover for a generic fs.FS whose top-level directories are
// skills (<name>/SKILL.md), such as an embed.FS or os.Root.FS(). source labels
// the origin in Metadata.Source ("<source>/<name>/SKILL.md").
//
// Whether symbolic links are followed is decided by fsys, not by this
// package: embed.FS has none, os.Root.FS() confines them to the root, while
// os.DirFS follows them without any boundary. The caller is responsible for
// passing an fs.FS whose behavior it trusts. The returned Catalog holds no
// handles beyond fsys itself, which the caller keeps ownership of; Close
// only marks the Catalog closed.
func DiscoverFS(ctx context.Context, fsys fs.FS, source string, opts DiscoverOptions) (*Catalog, error) {
	if fsys == nil {
		return nil, agenterrors.New(agenterrors.CodeInvalidConfig, "nil skill source fs")
	}
	return discoverSources(ctx, opts, &fsSource{fsys: fsys, dir: ".", label: source})
}

// fsSource reads one directory of an fs.FS; dir is "." for the top level.
type fsSource struct {
	fsys  fs.FS
	dir   string
	label string
}

func (s *fsSource) sourcePath(skill string) string { return path.Join(s.label, skill, "SKILL.md") }

func (s *fsSource) resolve(name string) (string, error) {
	full := path.Join(s.dir, name)
	if !fs.ValidPath(full) || (s.dir != "." && full != s.dir && !strings.HasPrefix(full, s.dir+"/")) {
		return "", &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return full, nil
}

func (s *fsSource) readDir(name string, remaining *int) (_ []fs.DirEntry, err error) {
	full, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	f, err := s.fsys.Open(full)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	dir, ok := f.(fs.ReadDirFile)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: full, Err: errors.New("not a directory")}
	}
	entries, err := dir.ReadDir(readDirCount(*remaining))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return takeEntries(entries, remaining)
}

func (s *fsSource) readFile(name string, limit int64) (_ []byte, err error) {
	full, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	f, err := s.fsys.Open(full)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return readLimited(f, info, limit)
}

func (s *fsSource) openChild(name string) (source, error) {
	full, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	info, err := fs.Stat(s.fsys, full)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &fs.PathError{Op: "open", Path: full, Err: errors.New("not a directory")}
	}
	return &fsSource{fsys: s.fsys, dir: full, label: s.label}, nil
}

func (s *fsSource) close() error { return nil }
