package coding

import (
	"context"
	"os"
	"path/filepath"
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
