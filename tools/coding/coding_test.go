package coding

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func newTestWorkspace(t *testing.T, opts ...Option) *Workspace {
	t.Helper()
	w, err := NewWorkspace(t.TempDir(), opts...)
	require.NoError(t, err)
	return w
}

func toolByName(t *testing.T, w *Workspace, name string) interfaces.Tool {
	t.Helper()
	for _, tool := range w.Tools() {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}

func call(t *testing.T, w *Workspace, name string, args map[string]any) (*interfaces.ToolOutput, error) {
	t.Helper()
	return toolByName(t, w, name).Invoke(context.Background(), &interfaces.ToolInput{Args: args})
}

func mustSucceed(t *testing.T, w *Workspace, name string, args map[string]any) map[string]any {
	t.Helper()
	out, err := call(t, w, name, args)
	require.NoError(t, err)
	_, rejected := tools.RejectedResult(out)
	require.False(t, rejected, "unexpected rejection: %+v", out.Result)
	require.True(t, out.Success, out.Error)
	return out.Result.(map[string]any)
}

func mustReject(t *testing.T, w *Workspace, name string, args map[string]any, contains string) {
	t.Helper()
	out, err := call(t, w, name, args)
	require.NoError(t, err)
	msg, ok := tools.RejectedResult(out)
	require.True(t, ok, "expected rejection, got %+v", out)
	require.Contains(t, msg, contains)
}

func writeFile(t *testing.T, w *Workspace, rel, content string) {
	t.Helper()
	p := filepath.Join(w.Root(), filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func readFile(t *testing.T, w *Workspace, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(w.Root(), filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(data)
}

func TestNewWorkspaceRejectsInvalidRoot(t *testing.T) {
	_, err := NewWorkspace(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)

	file := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, err = NewWorkspace(file)
	require.Error(t, err)

	_, err = NewWorkspace(t.TempDir(), WithExecutor(nil))
	require.Error(t, err)
}

func TestReadFile(t *testing.T) {
	w := newTestWorkspace(t)
	writeFile(t, w, "a.txt", "one\ntwo\nthree\nfour\n")

	res := mustSucceed(t, w, ToolReadFile, map[string]any{"path": "a.txt"})
	require.Equal(t, "     1\tone\n     2\ttwo\n     3\tthree\n     4\tfour\n", res["content"])
	require.Equal(t, 4, res["total_lines"])

	res = mustSucceed(t, w, ToolReadFile, map[string]any{"path": "a.txt", "offset": float64(2), "limit": float64(2)})
	require.Equal(t, "     2\ttwo\n     3\tthree\n", res["content"])
	require.Equal(t, 3, res["end_line"])

	writeFile(t, w, "empty.txt", "")
	res = mustSucceed(t, w, ToolReadFile, map[string]any{"path": "empty.txt"})
	require.Equal(t, "", res["content"])

	mustReject(t, w, ToolReadFile, map[string]any{"path": "a.txt", "offset": float64(9)}, "exceeds total lines")
	mustReject(t, w, ToolReadFile, map[string]any{"path": "missing.txt"}, "does not exist")
	writeFile(t, w, "bin", "a\x00b")
	mustReject(t, w, ToolReadFile, map[string]any{"path": "bin"}, "binary")

	_, err := call(t, w, ToolReadFile, map[string]any{"path": "a.txt", "offset": 1.5})
	require.Error(t, err)
}

func TestWriteFileRequiresFreshRead(t *testing.T) {
	w := newTestWorkspace(t)

	res := mustSucceed(t, w, ToolWriteFile, map[string]any{"path": "dir/new.go", "content": "package x\n"})
	require.Equal(t, true, res["created"])
	require.Equal(t, "package x\n", readFile(t, w, "dir/new.go"))
	// 自己写入的内容视为已读，可以继续覆盖。
	res = mustSucceed(t, w, ToolWriteFile, map[string]any{"path": "dir/new.go", "content": "package y\n"})
	require.Equal(t, false, res["created"])

	writeFile(t, w, "old.go", "old")
	mustReject(t, w, ToolWriteFile, map[string]any{"path": "old.go", "content": "new"}, "has not been read")

	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "old.go"})
	writeFile(t, w, "old.go", "changed by someone else")
	mustReject(t, w, ToolWriteFile, map[string]any{"path": "old.go", "content": "new"}, "changed since it was last read")
	require.Equal(t, "changed by someone else", readFile(t, w, "old.go"))

	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "old.go"})
	mustSucceed(t, w, ToolWriteFile, map[string]any{"path": "old.go", "content": "new"})
	require.Equal(t, "new", readFile(t, w, "old.go"))
}

func TestEditFile(t *testing.T) {
	w := newTestWorkspace(t)
	writeFile(t, w, "main.go", "a := 1\nb := 1\nc := 2\n")
	require.NoError(t, os.Chmod(filepath.Join(w.Root(), "main.go"), 0o755))

	mustReject(t, w, ToolEditFile, map[string]any{"path": "main.go", "old_string": "c := 2", "new_string": "c := 3"}, "has not been read")
	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "main.go"})

	mustReject(t, w, ToolEditFile, map[string]any{"path": "main.go", "old_string": "z", "new_string": "y"}, "not found")
	mustReject(t, w, ToolEditFile, map[string]any{"path": "main.go", "old_string": ":= 1", "new_string": ":= 9"}, "occurs 2 times")

	res := mustSucceed(t, w, ToolEditFile, map[string]any{"path": "main.go", "old_string": "c := 2", "new_string": "c := 3"})
	require.Equal(t, 1, res["replacements"])
	// 连续编辑无需重新读取：编辑后的内容已被记录。
	res = mustSucceed(t, w, ToolEditFile, map[string]any{"path": "main.go", "old_string": ":= 1", "new_string": ":= 9", "replace_all": true})
	require.Equal(t, 2, res["replacements"])
	require.Equal(t, "a := 9\nb := 9\nc := 3\n", readFile(t, w, "main.go"))

	info, err := os.Stat(filepath.Join(w.Root(), "main.go"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	_, err = call(t, w, ToolEditFile, map[string]any{"path": "main.go", "old_string": "a", "new_string": "a"})
	require.Error(t, err)
	mustReject(t, w, ToolEditFile, map[string]any{"path": "nope.go", "old_string": "a", "new_string": "b"}, "does not exist")
}

func TestGlob(t *testing.T) {
	w := newTestWorkspace(t)
	writeFile(t, w, "main.go", "")
	writeFile(t, w, "pkg/a/a.go", "")
	writeFile(t, w, "pkg/a/a_test.go", "")
	writeFile(t, w, "pkg/readme.md", "")
	writeFile(t, w, ".git/HEAD.go", "")
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(w.Root(), "main.go"), old, old))

	res := mustSucceed(t, w, ToolGlob, map[string]any{"pattern": "**/*.go"})
	files := res["files"].([]string)
	require.ElementsMatch(t, []string{"main.go", "pkg/a/a.go", "pkg/a/a_test.go"}, files)
	require.Equal(t, "main.go", files[len(files)-1], "oldest file last")

	res = mustSucceed(t, w, ToolGlob, map[string]any{"pattern": "*.go"})
	require.Equal(t, []string{"main.go"}, res["files"])

	res = mustSucceed(t, w, ToolGlob, map[string]any{"pattern": "a/*_test.go", "path": "pkg"})
	require.Equal(t, []string{"pkg/a/a_test.go"}, res["files"])

	res = mustSucceed(t, w, ToolGlob, map[string]any{"pattern": "**", "limit": float64(2)})
	require.Len(t, res["files"], 2)
	require.Equal(t, true, res["truncated"])

	_, err := call(t, w, ToolGlob, map[string]any{"pattern": "[a"})
	require.Error(t, err)
	_, err = call(t, w, ToolGlob, map[string]any{"pattern": "*", "path": "missing"})
	require.Error(t, err)
}

func TestGrep(t *testing.T) {
	w := newTestWorkspace(t)
	writeFile(t, w, "a.go", "package a\nfunc Hello() {}\n")
	writeFile(t, w, "sub/b.go", "package b\nfunc hello() {}\n")
	writeFile(t, w, "sub/c.txt", "func Hello\n")
	writeFile(t, w, "bin.go", "func Hello\x00")

	res := mustSucceed(t, w, ToolGrep, map[string]any{"pattern": `func Hello`, "include": "*.go"})
	require.Equal(t, []grepMatch{{Path: "a.go", Line: 2, Text: "func Hello() {}"}}, res["matches"])

	res = mustSucceed(t, w, ToolGrep, map[string]any{"pattern": `func hello`, "ignore_case": true, "include": "*.go"})
	require.Len(t, res["matches"], 2)

	res = mustSucceed(t, w, ToolGrep, map[string]any{"pattern": `func`, "include": "sub/*.txt"})
	require.Equal(t, []grepMatch{{Path: "sub/c.txt", Line: 1, Text: "func Hello"}}, res["matches"])

	res = mustSucceed(t, w, ToolGrep, map[string]any{"pattern": `func`, "path": "sub/b.go"})
	require.Len(t, res["matches"], 1)

	res = mustSucceed(t, w, ToolGrep, map[string]any{"pattern": `package|func`, "limit": float64(1)})
	require.Len(t, res["matches"], 1)
	require.Equal(t, true, res["truncated"])

	_, err := call(t, w, ToolGrep, map[string]any{"pattern": `(`})
	require.Error(t, err)
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.go", "a.go", true},
		{"**/*.go", "x/y/a.go", true},
		{"x/**", "x/y/z", true},
		{"x/**/z", "x/z", true},
		{"*.go", "x/a.go", false},
		{"x/*/z", "x/y/w/z", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, matchGlob(strings.Split(c.pattern, "/"), strings.Split(c.name, "/")), c)
	}
}

func TestRunCommand(t *testing.T) {
	w := newTestWorkspace(t, WithCommandTimeout(time.Second), WithMaxOutputBytes(100))
	writeFile(t, w, "sub/x.txt", "")

	res := mustSucceed(t, w, ToolRunCommand, map[string]any{"command": "echo hi; ls; exit 3", "work_dir": "sub"})
	require.Equal(t, 3, res["exit_code"])
	require.Equal(t, "hi\nx.txt\n", res["output"])
	require.Equal(t, false, res["timed_out"])

	res = mustSucceed(t, w, ToolRunCommand, map[string]any{"command": "echo before; sleep 5"})
	require.Equal(t, true, res["timed_out"])
	require.Equal(t, -1, res["exit_code"])
	require.Contains(t, res["output"], "before")

	res = mustSucceed(t, w, ToolRunCommand, map[string]any{"command": "head -c 500 /dev/zero | tr '\\0' a; echo END"})
	require.Equal(t, true, res["truncated"])
	require.Contains(t, res["output"], "省略")
	require.True(t, strings.HasSuffix(res["output"].(string), "END\n"))

	_, err := call(t, w, ToolRunCommand, map[string]any{"command": "pwd", "work_dir": "../"})
	require.Error(t, err)
	_, err = call(t, w, ToolRunCommand, map[string]any{"command": "pwd", "work_dir": "sub/x.txt"})
	require.Error(t, err)

	provider, ok := toolByName(t, w, ToolRunCommand).(tools.DispatchTimeoutProvider)
	require.True(t, ok)
	require.Greater(t, provider.DispatchTimeout(), time.Second)
}

func TestRunCommandParentCancel(t *testing.T) {
	w := newTestWorkspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := toolByName(t, w, ToolRunCommand).Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"command": "true"}})
	require.Error(t, err)
	require.False(t, out.Success)
}

func TestTruncateOutputKeepsValidUTF8(t *testing.T) {
	out, truncated := truncateOutput([]byte(strings.Repeat("中", 50)), 31)
	require.True(t, truncated)
	require.True(t, strings.Contains(out, "省略"))
	require.True(t, strings.ToValidUTF8(out, "") == out)
}

func TestGitTools(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	w := newTestWorkspace(t)
	t.Setenv("GIT_AUTHOR_NAME", "tester")
	t.Setenv("GIT_AUTHOR_EMAIL", "tester@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "tester")
	t.Setenv("GIT_COMMITTER_EMAIL", "tester@example.com")

	mustReject(t, w, ToolGitStatus, map[string]any{}, "exited with code")
	mustSucceed(t, w, ToolRunCommand, map[string]any{"command": "git init -q -b main"})

	writeFile(t, w, "a.go", "package a\n")
	writeFile(t, w, "中文.go", "package a\n")
	res := mustSucceed(t, w, ToolGitStatus, map[string]any{})
	require.Contains(t, res["status"], "?? a.go")
	require.Contains(t, res["status"], "中文.go")

	res = mustSucceed(t, w, ToolGitCommit, map[string]any{"message": "add a", "paths": []any{"a.go"}})
	require.Len(t, res["commit"], 40)
	mustSucceed(t, w, ToolGitCommit, map[string]any{"message": "add rest", "all": true})
	mustReject(t, w, ToolGitCommit, map[string]any{"message": "empty"}, "exited with code")

	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "a.go"})
	mustSucceed(t, w, ToolEditFile, map[string]any{"path": "a.go", "old_string": "package a", "new_string": "package b"})
	res = mustSucceed(t, w, ToolGitDiff, map[string]any{"paths": []any{"a.go"}})
	require.Contains(t, res["diff"], "+package b")
	res = mustSucceed(t, w, ToolGitDiff, map[string]any{"staged": true})
	require.Equal(t, "", res["diff"])

	res = mustSucceed(t, w, ToolGitLog, map[string]any{"max_count": float64(1)})
	require.Contains(t, res["log"], "tester add rest")
	require.NotContains(t, res["log"], "add a")

	_, err := call(t, w, ToolGitCommit, map[string]any{"message": "x", "all": true, "paths": []any{"a.go"}})
	require.Error(t, err)
	_, err = call(t, w, ToolGitDiff, map[string]any{"paths": []any{"../x"}})
	require.Error(t, err)
}

// 工具的 JSON Schema 必须能被 Dispatcher 编译，参数错误经 ReturnArgumentErrors 交还模型。
func TestToolsWorkWithDispatcher(t *testing.T) {
	w := newTestWorkspace(t)
	registry := tools.NewRegistry()
	for _, tool := range w.Tools() {
		require.NoError(t, registry.Register(tool))
	}
	d, err := tools.NewDispatcher(registry, tools.DispatchConfig{ReturnArgumentErrors: true})
	require.NoError(t, err)

	model := map[string]interfaces.Tool{}
	for _, tool := range d.Tools() {
		model[tool.Name()] = tool
	}
	require.Len(t, model, 12)

	ctx := context.Background()
	out, err := model[ToolWriteFile].Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"path": "x.go", "content": "package x\n"}})
	require.NoError(t, err)
	require.True(t, out.Success)

	out, err = model[ToolReadFile].Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"path": "../etc/passwd"}})
	require.NoError(t, err)
	msg, ok := tools.ArgumentErrorResult(out)
	require.True(t, ok)
	require.Contains(t, msg, "outside the workspace")

	out, err = model[ToolEditFile].Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"path": "x.go", "old_string": "nope", "new_string": "y"}})
	require.NoError(t, err)
	msg, ok = tools.RejectedResult(out)
	require.True(t, ok)
	require.Contains(t, msg, "not found")
}

// 设置 GOAGENT_CODING_DOCKER_IMAGE 为含 sh 的本地镜像时运行，如 alpine:3。容器执行器的细节在 env 包测试。
func TestDockerExecutorWorkspace(t *testing.T) {
	image := os.Getenv("GOAGENT_CODING_DOCKER_IMAGE")
	if image == "" {
		t.Skip("GOAGENT_CODING_DOCKER_IMAGE not set")
	}
	root := t.TempDir()
	executor := WithExecutor(env.DockerExecutor{Root: root, Image: image})
	// 容器启动可能就要数秒，正常命令留足时间，超时用例单独用短超时。
	w, err := NewWorkspace(root, executor, WithCommandTimeout(30*time.Second))
	require.NoError(t, err)
	writeFile(t, w, "sub/in.txt", "from host")

	res := mustSucceed(t, w, ToolRunCommand, map[string]any{"command": "pwd; cat in.txt; echo out > out.txt; ls /sys/class/net", "work_dir": "sub"})
	require.Equal(t, 0, res["exit_code"])
	require.Equal(t, "/workspace/sub\nfrom hostlo\n", res["output"])
	require.Equal(t, "out\n", readFile(t, w, "sub/out.txt"))

	w, err = NewWorkspace(root, executor, WithCommandTimeout(3*time.Second))
	require.NoError(t, err)
	res = mustSucceed(t, w, ToolRunCommand, map[string]any{"command": "sleep 60"})
	require.Equal(t, true, res["timed_out"])
	left, err := exec.Command("docker", "ps", "-aq", "--filter", "name=goagent-env-", "--filter", "volume="+root).Output()
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(string(left)), "timed out container must be removed")
}

// 命令把子进程放到后台后立即退出，子进程仍持有输出管道：应作为正常结果返回而非中止运行。
func TestRunCommandBackgroundChild(t *testing.T) {
	w := newTestWorkspace(t)
	res := mustSucceed(t, w, ToolRunCommand, map[string]any{"command": "sleep 5 & echo started"})
	require.Equal(t, 0, res["exit_code"])
	require.Equal(t, false, res["timed_out"])
	require.Contains(t, res["output"], "started")
}

func TestGitDirIsNotWritable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	w := newTestWorkspace(t)
	mustSucceed(t, w, ToolRunCommand, map[string]any{"command": "git init -q"})
	for _, p := range []string{".git/config", ".git/hooks/pre-commit", "sub/.git/config"} {
		_, err := call(t, w, ToolWriteFile, map[string]any{"path": p, "content": "x"})
		require.Error(t, err, p)
	}
	mustSucceed(t, w, ToolReadFile, map[string]any{"path": ".git/config"})
	_, err := call(t, w, ToolEditFile, map[string]any{"path": ".git/config", "old_string": "[core]", "new_string": "[core]\n\tfsmonitor = touch marker"})
	require.Error(t, err)

	// 即使仓库配置被其他途径改写，git 工具也不执行 fsmonitor。
	marker := filepath.Join(w.Root(), "marker")
	mustSucceed(t, w, ToolRunCommand, map[string]any{"command": "git config core.fsmonitor 'touch " + marker + "; false'"})
	mustSucceed(t, w, ToolGitStatus, map[string]any{})
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "fsmonitor must not run")
}

// 同一轮并发编辑同一文件时，两处改动都应保留。
func TestConcurrentEditsDoNotLoseUpdates(t *testing.T) {
	for range 20 {
		w := newTestWorkspace(t)
		writeFile(t, w, "f.txt", "AAA\nBBB\n")
		mustSucceed(t, w, ToolReadFile, map[string]any{"path": "f.txt"})
		edit := toolByName(t, w, ToolEditFile)
		errs := make(chan error, 2)
		for _, pair := range [][2]string{{"AAA", "aaa"}, {"BBB", "bbb"}} {
			go func() {
				out, err := edit.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"path": "f.txt", "old_string": pair[0], "new_string": pair[1]}})
				if err == nil && !out.Success {
					err = os.ErrInvalid
				}
				errs <- err
			}()
		}
		require.NoError(t, <-errs)
		require.NoError(t, <-errs)
		require.Equal(t, "aaa\nbbb\n", readFile(t, w, "f.txt"))
	}
}

func TestMatchGlobManyDoubleStars(t *testing.T) {
	pattern := strings.Split(strings.Repeat("**/", 12)+"x", "/")
	name := strings.Split(strings.Repeat("d/", 20)+"y", "/")
	start := time.Now()
	require.False(t, matchGlob(pattern, name))
	require.Less(t, time.Since(start), 100*time.Millisecond)
}

func TestGlobNormalizesPatterns(t *testing.T) {
	w := newTestWorkspace(t)
	writeFile(t, w, "src/a.go", "package a\n")
	for _, p := range []string{"./src/*.go", "./**/*.go", "src/*.go/"} {
		res := mustSucceed(t, w, ToolGlob, map[string]any{"pattern": p})
		require.Equal(t, []string{"src/a.go"}, res["files"], p)
	}
	res := mustSucceed(t, w, ToolGrep, map[string]any{"pattern": "package", "include": "./src/*.go"})
	require.Len(t, res["matches"], 1)
	// path 指向文件时，include 相对其所在目录匹配。
	res = mustSucceed(t, w, ToolGrep, map[string]any{"pattern": "package", "path": "src/a.go", "include": "**/a.go"})
	require.Len(t, res["matches"], 1)
}

func TestUnreadableFileIsRejected(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	w := newTestWorkspace(t)
	writeFile(t, w, "secret.txt", "x")
	require.NoError(t, os.Chmod(filepath.Join(w.Root(), "secret.txt"), 0))
	mustReject(t, w, ToolReadFile, map[string]any{"path": "secret.txt"}, "permission denied")
}
