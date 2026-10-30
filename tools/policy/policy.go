// Package policy 提供按工具名与命令前缀裁决的权限规则，作为 tools.Pipeline 的调用前钩子使用，
// 对任何工具生效（编码工具、MCP 工具、HTTP 工具）。
package policy

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/kart-io/goagent/tools"
)

// Decision 是规则的裁决。零值为 Ask。
type Decision int

const (
	// Ask 交给审批人确认。
	Ask Decision = iota
	// Allow 直接执行。
	Allow
	// Deny 不执行，以 tools.Rejected 告知模型。
	Deny
)

// Rule 是一条权限规则。
type Rule struct {
	// Tool 是工具名，"*" 匹配所有工具，空串不匹配任何工具。
	Tool string
	// Command 只对带命令参数（Policy.CommandArg）的调用生效：命令按空白分词后以这些词开头才匹配，如 "go test"。
	// 含 shell 控制符、引号或反斜杠的命令不匹配任何带 Command 的规则，按后续规则或 Default 裁决。
	// 放行规则等于放行该命令的全部参数（go test -exec 可执行任意程序，测试代码本身也由模型编写）；
	// 拒绝规则可被 /bin/rm、env rm、sh -c 等写法绕过，不能依赖。需要隔离时配合容器执行器。
	Command  string
	Decision Decision
}

// Policy 按顺序取第一条匹配的规则，都不匹配时用 Default。
type Policy struct {
	Rules   []Rule
	Default Decision
	// CommandArg 是命令前缀匹配读取的参数名，默认 "command"。
	CommandArg string
	// SessionTools 列出审批时可以整工具"本会话允许"的工具。带命令参数的调用总是收窄为命令前缀，
	// 不在列表中的其他工具只能单次批准。
	SessionTools []string
}

func (p *Policy) commandArg() string {
	if p.CommandArg != "" {
		return p.CommandArg
	}
	return "command"
}

// Decide 返回调用的裁决。
func (p *Policy) Decide(call *tools.Call) Decision {
	command, hasCommand := call.Args[p.commandArg()].(string)
	for _, r := range p.Rules {
		if r.Tool != "*" && r.Tool != call.Tool {
			continue
		}
		if r.Command != "" && (!hasCommand || !commandHasPrefix(command, r.Command)) {
			continue
		}
		return r.Decision
	}
	return p.Default
}

// Hook 把策略适配为 tools.PreHook。
func (p *Policy) Hook() tools.PreHook {
	return func(_ context.Context, call *tools.Call) (tools.PreDecision, error) {
		switch p.Decide(call) {
		case Allow:
			return tools.PreDecision{Kind: tools.PreAllow}, nil
		case Deny:
			return tools.PreDecision{Kind: tools.PreDeny, Feedback: call.Tool + " is denied by policy"}, nil
		}
		return tools.PreDecision{Kind: tools.PreAsk, Grant: p.grantFor(call)}, nil
	}
}

// grantFor 拟定"本会话允许"的范围：命令调用收窄为命令前缀，列入 SessionTools 的工具可整工具放行。
func (p *Policy) grantFor(call *tools.Call) tools.SessionGrant {
	if command, ok := call.Args[p.commandArg()].(string); ok {
		prefix := commandScope(command)
		if prefix == nil {
			return nil
		}
		return CommandGrant{Tool: call.Tool, Arg: p.commandArg(), Prefix: prefix}
	}
	if slices.Contains(p.SessionTools, call.Tool) {
		return ToolGrant{Tool: call.Tool}
	}
	return nil
}

// commandScope 取命令首词，第二个词是纯字母子命令时一并取（go test、git status、npm run）。
// 含 shell 控制符的命令无法安全收窄，返回 nil。
func commandScope(command string) []string {
	if unsafeCommand(command) {
		return nil
	}
	words := strings.Fields(command)
	if len(words) == 0 {
		return nil
	}
	if len(words) > 1 && isSubcommand(words[1]) {
		return words[:2]
	}
	return words[:1]
}

func isSubcommand(w string) bool {
	for _, r := range w {
		if !unicode.IsLetter(r) && r != '-' {
			return false
		}
	}
	return w != "" && w[0] != '-'
}

// CommandGrant 放行同一工具中以 Prefix 开头、且不含 shell 控制符的命令。
type CommandGrant struct {
	Tool   string
	Arg    string
	Prefix []string
}

// Describe 实现 tools.SessionGrant。
func (g CommandGrant) Describe() string {
	return fmt.Sprintf("本会话内允许 %s 执行以 %q 开头的命令", g.Tool, strings.Join(g.Prefix, " "))
}

// Matches 实现 tools.SessionGrant。
func (g CommandGrant) Matches(call *tools.Call) bool {
	command, ok := call.Args[g.Arg].(string)
	return ok && call.Tool == g.Tool && commandHasPrefix(command, strings.Join(g.Prefix, " "))
}

// ToolGrant 放行同一工具的全部调用。
type ToolGrant struct{ Tool string }

// Describe 实现 tools.SessionGrant。
func (g ToolGrant) Describe() string { return "本会话内允许 " + g.Tool + " 的全部调用" }

// Matches 实现 tools.SessionGrant。
func (g ToolGrant) Matches(call *tools.Call) bool { return call.Tool == g.Tool }

func unsafeCommand(command string) bool {
	return strings.ContainsAny(command, ";&|`$()<>\n\r'\"\\")
}

func commandHasPrefix(command, prefix string) bool {
	if unsafeCommand(command) {
		return false
	}
	words, want := strings.Fields(command), strings.Fields(prefix)
	if len(want) == 0 || len(words) < len(want) {
		return false
	}
	for i, w := range want {
		if words[i] != w {
			return false
		}
	}
	return true
}
