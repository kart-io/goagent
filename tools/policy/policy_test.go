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

// 匹配的规则中最严格者生效，与顺序无关；Default 只在没有规则匹配时使用。
func TestDecideStrictestWins(t *testing.T) {
	allowTest := Rule{Tool: "run_command", Command: "go test", Decision: Allow}
	askAll := Rule{Tool: "run_command", Decision: Ask}
	denyAll := Rule{Tool: "*", Decision: Deny}
	for _, rules := range [][]Rule{{allowTest, askAll}, {askAll, allowTest}} {
		require.Equal(t, Ask, (&Policy{Rules: rules, Default: Allow}).Decide(call("run_command", "go test ./...")))
	}
	for _, rules := range [][]Rule{{allowTest, denyAll}, {denyAll, allowTest}} {
		require.Equal(t, Deny, (&Policy{Rules: rules}).Decide(call("run_command", "go test ./...")))
	}
	p := &Policy{Rules: []Rule{allowTest}, Default: Ask}
	require.Equal(t, Allow, p.Decide(call("run_command", "go test ./...")))
	require.Equal(t, Ask, p.Decide(call("run_command", "make")))
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

// 无法可靠解析的命令不能绕过带 Command 的拒绝或询问规则。
func TestDecideRestrictiveRulesFailClosed(t *testing.T) {
	p := &Policy{
		Rules: []Rule{
			{Tool: "run_command", Decision: Allow},
			{Tool: "run_command", Command: "git push", Decision: Deny},
			{Tool: "run_command", Command: "npm publish", Decision: Ask},
		},
		Default: Allow,
	}
	cases := []struct {
		command string
		want    Decision
	}{
		{"git push origin main", Deny},
		{"/usr/bin/git push", Deny},
		{"./bin/git push --force", Deny},
		{"git push && true", Ask},
		{"true; git push", Ask},
		{"npm publish", Ask},
		{"/usr/local/bin/npm publish", Ask},
		{"go test ./... | tee out", Ask},
		{"git status", Allow},
		{"echo hi", Allow},
	}
	for _, c := range cases {
		require.Equal(t, c.want, p.Decide(call("run_command", c.command)), c.command)
	}
	// 其他工具的限制规则不影响本工具。
	require.Equal(t, Allow, p.Decide(call("other", "a; b")))
}

// 放行规则仍按原样精确匹配首词，带路径的命令不会因此被放行。
func TestDecideAllowRulesStayExact(t *testing.T) {
	p := &Policy{Rules: []Rule{{Tool: "run_command", Command: "go test", Decision: Allow}}, Default: Ask}
	require.Equal(t, Ask, p.Decide(call("run_command", "/tmp/evil/go test")))
	require.Equal(t, Allow, p.Decide(call("run_command", "go test ./...")))
}

// argv 形式的工具（如 shell 工具）把子命令放在参数数组里，规则与会话授权须按"命令 + 参数"整体匹配。
func TestDecideArgvCommands(t *testing.T) {
	argv := func(command string, args ...any) *tools.Call {
		return &tools.Call{Tool: "shell", Args: map[string]any{"command": command, "args": args}}
	}
	p := &Policy{
		Rules: []Rule{
			{Tool: "shell", Command: "git status", Decision: Allow},
			{Tool: "shell", Command: "git push", Decision: Deny},
		},
		Default: Ask,
	}
	cases := []struct {
		call *tools.Call
		want Decision
	}{
		{argv("git", "push", "origin", "main"), Deny},
		{argv("/usr/bin/git", "push"), Deny},
		{argv("git", "status", "-s"), Allow},
		{argv("git", "status --porcelain"), Ask},
		{argv("git"), Ask},
		{argv("git", "commit", "-m", "msg with spaces"), Ask},
		{&tools.Call{Tool: "shell", Args: map[string]any{"command": "git", "args": []string{"push"}}}, Deny},
	}
	for _, c := range cases {
		require.Equal(t, c.want, p.Decide(c.call), c.call.Args)
	}

	d, err := p.Hook()(context.Background(), argv("git", "log", "-1"))
	require.NoError(t, err)
	require.Equal(t, []string{"git", "log"}, d.Grant.(CommandGrant).Prefix)
	require.True(t, d.Grant.Matches(argv("git", "log", "--oneline")))
	require.False(t, d.Grant.Matches(argv("git", "push")), "授权不能从子命令扩大到整个程序")
}
