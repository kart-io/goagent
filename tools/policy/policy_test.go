package policy

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/tools"
	"github.com/stretchr/testify/require"
)

func call(tool, command string) *tools.Call {
	args := map[string]any{}
	if command != "" {
		args["command"] = command
	}
	return &tools.Call{Tool: tool, Args: args}
}

func TestDecide(t *testing.T) {
	p := &Policy{
		Rules: []Rule{
			{Tool: "run_command", Command: "rm", Decision: Deny},
			{Tool: "run_command", Command: "go test", Decision: Allow},
			{Tool: "read_file", Decision: Allow},
			{Command: "go vet", Decision: Allow}, // 漏写 Tool 的规则不匹配任何工具
		},
		Default: Ask,
	}
	cases := []struct {
		tool, command string
		want          Decision
	}{
		{"read_file", "", Allow},
		{"write_file", "", Ask},
		{"run_command", "go test ./...", Allow},
		{"run_command", "  go   test -run X", Allow},
		{"run_command", "go testx", Ask},
		{"run_command", "go test ./... ; rm -rf /", Ask},
		{"run_command", "go test $(rm -rf /)", Ask},
		{"run_command", `go test -exec 'sh -c x'`, Ask},
		{"run_command", `go test \`, Ask},
		{"run_command", "rm -rf build", Deny},
		{"run_command", "go", Ask},
		{"run_command", "go vet ./...", Ask},
	}
	for _, c := range cases {
		require.Equal(t, c.want, p.Decide(call(c.tool, c.command)), c)
	}
	require.Equal(t, Allow, (&Policy{Rules: []Rule{{Tool: "*", Decision: Allow}}}).Decide(call("x", "")))
}

func TestHookAndGrants(t *testing.T) {
	p := &Policy{Default: Ask, SessionTools: []string{"git_commit"}, Rules: []Rule{{Tool: "rm_all", Decision: Deny}}}
	hook := p.Hook()
	ctx := context.Background()

	d, err := hook(ctx, call("rm_all", ""))
	require.NoError(t, err)
	require.Equal(t, tools.PreDeny, d.Kind)

	d, err = hook(ctx, call("run_command", "go test ./pkg"))
	require.NoError(t, err)
	require.Equal(t, tools.PreAsk, d.Kind)
	require.Equal(t, `本会话内允许 run_command 执行以 "go test" 开头的命令`, d.Grant.Describe())
	require.True(t, d.Grant.Matches(call("run_command", "go test ./other")))
	require.False(t, d.Grant.Matches(call("run_command", "go run main.go")))
	require.False(t, d.Grant.Matches(call("run_command", "go test ./... && rm -rf /")))
	require.False(t, d.Grant.Matches(call("other_tool", "go test")))

	// 第二个词不是子命令时只取首词。
	d, _ = hook(ctx, call("run_command", "make -j4"))
	require.Equal(t, []string{"make"}, d.Grant.(CommandGrant).Prefix)
	// 含 shell 控制符的命令无法收窄，只能单次批准。
	d, _ = hook(ctx, call("run_command", "go test | tee out"))
	require.Nil(t, d.Grant)

	d, _ = hook(ctx, call("git_commit", ""))
	require.Equal(t, ToolGrant{Tool: "git_commit"}, d.Grant)
	require.Equal(t, "本会话内允许 git_commit 的全部调用", d.Grant.Describe())
	d, _ = hook(ctx, call("mcp_send_mail", ""))
	require.Nil(t, d.Grant, "tools not listed in SessionTools can only be approved once")
}
