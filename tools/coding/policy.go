package coding

import "github.com/kart-io/goagent/tools/policy"

// DefaultPolicy 放行只读、检索、待办和工作区内的文件编辑（可经 Workspace.Revert 回滚），
// run_command 与 git_commit 需要审批；run_command 的会话批准收窄为命令前缀，git_commit 可整工具批准。
// 用作 tools.Pipeline 的调用前钩子：Pipeline{Pre: []tools.PreHook{coding.DefaultPolicy().Hook()}}。
func DefaultPolicy() *policy.Policy {
	rules := make([]policy.Rule, 0, 10)
	for _, name := range []string{ToolReadFile, ToolGlob, ToolGrep, ToolGitStatus, ToolGitDiff, ToolGitLog, ToolTodoWrite, ToolWriteFile, ToolEditFile, ToolMultiEdit} {
		rules = append(rules, policy.Rule{Tool: name, Decision: policy.Allow})
	}
	return &policy.Policy{Rules: rules, Default: policy.Ask, SessionTools: []string{ToolGitCommit}}
}
