package coding

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kart-io/goagent/harness/env"
	"github.com/stretchr/testify/require"
)

func TestMultiEditIsAtomic(t *testing.T) {
	w := newTestWorkspace(t)
	writeFile(t, w, "a.go", "x := 1\ny := 2\n")
	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "a.go"})

	// 第二处找不到时第一处也不生效。
	mustReject(t, w, ToolMultiEdit, map[string]any{"path": "a.go", "edits": []any{
		map[string]any{"old_string": "x := 1", "new_string": "x := 10"},
		map[string]any{"old_string": "nope", "new_string": "z"},
	}}, "edits[1]: old_string not found")
	require.Equal(t, "x := 1\ny := 2\n", readFile(t, w, "a.go"))

	// 后一处作用于前一处的结果。
	res := mustSucceed(t, w, ToolMultiEdit, map[string]any{"path": "a.go", "edits": []any{
		map[string]any{"old_string": "x := 1", "new_string": "x := 10"},
		map[string]any{"old_string": "x := 10\ny", "new_string": "x := 10\nyy"},
		map[string]any{"old_string": ":=", "new_string": "=", "replace_all": true},
	}})
	require.Equal(t, 3, res["edits"])
	require.Equal(t, 4, res["replacements"])
	require.Equal(t, "x = 10\nyy = 2\n", readFile(t, w, "a.go"))

	_, err := call(t, w, ToolMultiEdit, map[string]any{"path": "a.go", "edits": []any{map[string]any{"old_string": "a", "new_string": "a"}}})
	require.Error(t, err)
	_, err = call(t, w, ToolMultiEdit, map[string]any{"path": "a.go", "edits": []any{}})
	require.Error(t, err)
}

func TestChangesAndRevert(t *testing.T) {
	w := newTestWorkspace(t)
	writeFile(t, w, "old.txt", "v1")
	require.NoError(t, os.Chmod(filepath.Join(w.Root(), "old.txt"), 0o600))
	writeFile(t, w, "keep.txt", "k1")

	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "old.txt"})
	mustSucceed(t, w, ToolEditFile, map[string]any{"path": "old.txt", "old_string": "v1", "new_string": "v2"})
	mustSucceed(t, w, ToolWriteFile, map[string]any{"path": "old.txt", "content": "v3"})
	mustSucceed(t, w, ToolWriteFile, map[string]any{"path": "dir/new.txt", "content": "n"})
	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "keep.txt"})
	mustSucceed(t, w, ToolWriteFile, map[string]any{"path": "keep.txt", "content": "k2"})
	require.Equal(t, []env.Change{{Path: "dir/new.txt", Created: true}, {Path: "keep.txt"}, {Path: "old.txt"}}, w.Changes())

	require.Error(t, w.Revert(context.Background(), "missing.txt"))
	require.NoError(t, os.Chmod(filepath.Join(w.Root(), "old.txt"), 0o644))
	require.NoError(t, w.Revert(context.Background(), "old.txt", "dir/new.txt"))
	require.Equal(t, "v1", readFile(t, w, "old.txt"), "restores content before the first change")
	info, err := os.Stat(filepath.Join(w.Root(), "old.txt"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	_, err = os.Stat(filepath.Join(w.Root(), "dir/new.txt"))
	require.True(t, os.IsNotExist(err))
	require.Equal(t, []env.Change{{Path: "keep.txt"}}, w.Changes())

	// 回滚后须重新读取才能修改。
	mustReject(t, w, ToolWriteFile, map[string]any{"path": "old.txt", "content": "x"}, "has not been read")

	require.NoError(t, w.Revert(context.Background()))
	require.Equal(t, "k1", readFile(t, w, "keep.txt"))
	require.Empty(t, w.Changes())
}

func TestTodoWrite(t *testing.T) {
	w := newTestWorkspace(t)
	res := mustSucceed(t, w, ToolTodoWrite, map[string]any{"todos": []any{
		map[string]any{"content": "跑测试", "status": "completed"},
		map[string]any{"content": "修 bug", "status": "in_progress"},
		map[string]any{"content": "提交", "status": "pending"},
	}})
	require.Equal(t, 3, res["total"])
	require.Equal(t, 1, res["completed"])
	todos := w.Todos()
	require.Equal(t, Todo{Content: "修 bug", Status: TodoInProgress}, todos[1])
	todos[0].Content = "changed"
	require.Equal(t, "跑测试", w.Todos()[0].Content, "Todos returns a copy")

	for _, bad := range [][]any{
		{map[string]any{"content": "a", "status": "in_progress"}, map[string]any{"content": "b", "status": "in_progress"}},
		{map[string]any{"content": "a", "status": "done"}},
		{map[string]any{"content": " ", "status": "pending"}},
	} {
		_, err := call(t, w, ToolTodoWrite, map[string]any{"todos": bad})
		require.Error(t, err)
	}
	require.Len(t, w.Todos(), 3, "invalid lists do not replace the current one")

	mustSucceed(t, w, ToolTodoWrite, map[string]any{"todos": []any{}})
	require.Empty(t, w.Todos())
}

func TestProjectInstructions(t *testing.T) {
	w := newTestWorkspace(t)
	got, err := w.ProjectInstructions(context.Background())
	require.NoError(t, err)
	require.Empty(t, got)

	writeFile(t, w, "CLAUDE.md", "用中文\n")
	writeFile(t, w, "AGENTS.md", "先跑测试\n")
	got, err = w.ProjectInstructions(context.Background())
	require.NoError(t, err)
	require.Equal(t, "<AGENTS.md>\n先跑测试\n</AGENTS.md>\n<CLAUDE.md>\n用中文\n</CLAUDE.md>\n", got)
}

func TestProjectInstructionsSkipsUnsafeFiles(t *testing.T) {
	w := newTestWorkspace(t)
	secret := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(secret, []byte("SECRET"), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(w.Root(), "AGENTS.md")))
	require.NoError(t, os.Mkdir(filepath.Join(w.Root(), "CLAUDE.md"), 0o755))
	got, err := w.ProjectInstructions(context.Background())
	require.NoError(t, err)
	require.Empty(t, got)
}
