package coding

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func modelTools(t *testing.T, w *Workspace, p *tools.Pipeline) map[string]interfaces.Tool {
	t.Helper()
	registry := tools.NewRegistry()
	for _, tool := range w.Tools() {
		require.NoError(t, registry.Register(tool))
	}
	d, err := tools.NewDispatcher(registry, tools.DispatchConfig{ReturnArgumentErrors: true, Pipeline: p})
	require.NoError(t, err)
	out := map[string]interfaces.Tool{}
	for _, tool := range d.Tools() {
		out[tool.Name()] = tool
	}
	return out
}

// 编码工具经 DefaultPolicy 与审批：编辑直接放行，命令需要批准，会话批准收窄为命令前缀。
func TestDefaultPolicyWithPipeline(t *testing.T) {
	w := newTestWorkspace(t)
	var asked atomic.Int32
	outcome := tools.ApprovalAllowedSession
	p := &tools.Pipeline{
		Pre: []tools.PreHook{DefaultPolicy().Hook()},
		Approver: tools.ApproverFunc(func(_ context.Context, req tools.ApprovalRequest) (tools.ApprovalResult, error) {
			asked.Add(1)
			return tools.ApprovalResult{Outcome: outcome, Feedback: "use make"}, nil
		}),
	}
	m := modelTools(t, w, p)
	ctx := tools.WithSessionID(context.Background(), "session")
	invoke := func(name string, args map[string]any) *interfaces.ToolOutput {
		out, err := m[name].Invoke(ctx, &interfaces.ToolInput{Args: args})
		require.NoError(t, err)
		return out
	}

	out := invoke(ToolWriteFile, map[string]any{"path": "a.txt", "content": "x"})
	require.True(t, out.Success)
	require.Zero(t, asked.Load())

	out = invoke(ToolRunCommand, map[string]any{"command": "cat a.txt"})
	require.True(t, out.Success)
	require.EqualValues(t, 1, asked.Load())
	out = invoke(ToolRunCommand, map[string]any{"command": "cat a.txt a.txt"})
	require.True(t, out.Success)
	require.EqualValues(t, 1, asked.Load(), "session grant covers the same command prefix")

	outcome = tools.ApprovalRejected
	out = invoke(ToolRunCommand, map[string]any{"command": "touch b.txt"})
	msg, ok := tools.RejectedResult(out)
	require.True(t, ok)
	require.Equal(t, "the user denied run_command: use make", msg)
	_, err := os.Stat(filepath.Join(w.Root(), "b.txt"))
	require.True(t, os.IsNotExist(err), "denied command must not run")
}

func TestLinterUndoesFailingEdits(t *testing.T) {
	w := newTestWorkspace(t, WithLinter(func(path string) []string {
		if filepath.Ext(path) != ".txt" {
			return nil
		}
		return []string{"sh", "-c", `! grep -q BAD "$0" || { echo "lint: BAD found in $0"; exit 1; }`, path}
	}))
	writeFile(t, w, "a.txt", "good\n")
	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "a.txt"})

	mustReject(t, w, ToolEditFile, map[string]any{"path": "a.txt", "old_string": "good", "new_string": "BAD"}, "lint: BAD found in a.txt")
	require.Equal(t, "good\n", readFile(t, w, "a.txt"), "failed edit is undone")
	// 撤销后文件内容与读取时一致，可以继续编辑。
	mustSucceed(t, w, ToolEditFile, map[string]any{"path": "a.txt", "old_string": "good", "new_string": "better"})
	require.Equal(t, "better\n", readFile(t, w, "a.txt"))

	mustReject(t, w, ToolWriteFile, map[string]any{"path": "new.txt", "content": "BAD"}, "linter failed")
	_, err := os.Stat(filepath.Join(w.Root(), "new.txt"))
	require.True(t, os.IsNotExist(err), "a rejected new file is removed")
	require.Equal(t, []env.Change{{Path: "a.txt"}}, w.Changes(), "a rejected new file leaves no change record")

	mustSucceed(t, w, ToolWriteFile, map[string]any{"path": "skip.go", "content": "BAD"})
}

func TestSpillHook(t *testing.T) {
	w := newTestWorkspace(t)
	big := strings.Repeat("中", 3000)
	p := &tools.Pipeline{Post: []tools.PostHook{SpillHook(w, 1000)}}
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(tools.NewBaseTool("fetch", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: map[string]any{"body": big}}, nil
	})))
	require.NoError(t, registry.Register(tools.NewBaseTool("small", "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: "ok"}, nil
	})))
	for _, tool := range w.Tools() {
		require.NoError(t, registry.Register(tool))
	}
	d, err := tools.NewDispatcher(registry, tools.DispatchConfig{Pipeline: p})
	require.NoError(t, err)

	out, err := d.Invoke(context.Background(), tools.ToolRequest{Name: "fetch", Input: &interfaces.ToolInput{CallID: "c/1", Args: map[string]any{}}})
	require.NoError(t, err)
	res := out.Result.(map[string]any)
	require.Equal(t, true, res["truncated"])
	require.Equal(t, ".goagent/spill/fetch-c_1.txt", res["spill_path"])
	require.Equal(t, 1500, len([]rune(res["preview"].(string))))
	require.Contains(t, readFile(t, w, ".goagent/spill/fetch-c_1.txt"), big)
	require.Empty(t, w.Changes(), "spill files are not workspace changes")

	// 溢出文件可用 read_file 读取；小结果不受影响。
	read, err := d.Invoke(context.Background(), tools.ToolRequest{Name: ToolReadFile, Input: &interfaces.ToolInput{Args: map[string]any{"path": res["spill_path"], "limit": float64(1)}}})
	require.NoError(t, err)
	require.Equal(t, "     1\t== body ==\n", read.Result.(map[string]any)["content"])
	out, err = d.Invoke(context.Background(), tools.ToolRequest{Name: "small", Input: &interfaces.ToolInput{Args: map[string]any{}}})
	require.NoError(t, err)
	require.Equal(t, "ok", out.Result)
}

// linter 起不来或自行改动文件后失败时，编辑都被撤销并交给模型，运行不中止。
func TestLinterUndoEdgeCases(t *testing.T) {
	w := newTestWorkspace(t, WithLinter(func(string) []string { return []string{"definitely-not-a-linter-xyz"} }))
	writeFile(t, w, "a.txt", "good\n")
	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "a.txt"})
	mustReject(t, w, ToolEditFile, map[string]any{"path": "a.txt", "old_string": "good", "new_string": "bad"}, "failed to run")
	require.Equal(t, "good\n", readFile(t, w, "a.txt"))

	w = newTestWorkspace(t, WithLinter(func(p string) []string {
		return []string{"sh", "-c", `echo fixed >> "$0"; exit 1`, p}
	}))
	writeFile(t, w, "a.txt", "good\n")
	mustSucceed(t, w, ToolReadFile, map[string]any{"path": "a.txt"})
	mustReject(t, w, ToolEditFile, map[string]any{"path": "a.txt", "old_string": "good", "new_string": "bad"}, "linter failed")
	require.Equal(t, "good\n", readFile(t, w, "a.txt"))
}
