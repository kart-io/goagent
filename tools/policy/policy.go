// Package policy 提供按工具名与命令前缀裁决的权限规则，作为 tools.Pipeline 的调用前钩子使用，
// 对任何工具生效（编码工具、MCP 工具、HTTP 工具）。
package policy

import (
	"context"
	"fmt"
	"path"
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
	// 放行规则要求首词完全相同，含 shell 控制符、引号或反斜杠的命令不匹配放行规则。
	// 拒绝与询问规则比较首词的文件名，/usr/bin/git push 匹配 "git push"；命令含上述字符而无法判断时，
	// 该规则至少裁决为 Ask，交给审批人。
	// 放行规则等于放行该命令的全部参数（go test -exec 可执行任意程序，测试代码本身也由模型编写）；
	// 拒绝规则仍可被 env rm、sh -c、脚本文件等写法绕过，不能依赖。需要隔离时配合容器执行器。
	Command  string
	Decision Decision
}

// Policy 在所有匹配的规则中取最严格的裁决（Deny > Ask > Allow），与规则顺序无关；都不匹配时用 Default。
// 因此追加的规则只能收紧已有规则的结果，不能越过已有的 Deny 或 Ask。
type Policy struct {
	Rules   []Rule
	Default Decision
	// CommandArg 是命令前缀匹配读取的参数名，默认 "command"。
	CommandArg string
	// ArgsArg 是参数数组的参数名，默认 "args"。argv 形式的工具（如 shell 工具）把子命令放在这里，
	// 匹配时接在 CommandArg 分词之后；数组元素按原样作为一个词，不再分词。
	ArgsArg string
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

func (p *Policy) argsArg() string {
	if p.ArgsArg != "" {
		return p.ArgsArg
	}
	return "args"
}

// commandLine 取调用的命令词序列；unsafe 表示命令串含 shell 控制符、引号或反斜杠，无法可靠分词。
func commandLine(args map[string]any, commandArg, argsArg string) (words []string, unsafe, ok bool) {
	command, ok := args[commandArg].(string)
	if !ok {
		return nil, false, false
	}
	words = strings.Fields(command)
	switch v := args[argsArg].(type) {
	case []string:
		words = append(words, v...)
	case []any:
		for _, a := range v {
			words = append(words, fmt.Sprint(a))
		}
	}
	return words, unsafeCommand(command), true
}

// Decide 返回调用的裁决。
func (p *Policy) Decide(call *tools.Call) Decision {
	words, unsafe, hasCommand := commandLine(call.Args, p.commandArg(), p.argsArg())
	decision, matched := Allow, false
	for _, r := range p.Rules {
		if r.Tool != "*" && r.Tool != call.Tool {
			continue
		}
		d := r.Decision
		if r.Command != "" {
			switch {
			case !hasCommand:
				continue
			case d == Allow:
				if unsafe || !wordsHavePrefix(words, r.Command) {
					continue
				}
			case unsafe:
				d = Ask
			case !restrictedPrefix(words, r.Command):
				continue
			}
		}
		if !matched || strictness(d) > strictness(decision) {
			decision, matched = d, true
		}
	}
	if !matched {
		return p.Default
	}
	return decision
}

func strictness(d Decision) int {
	switch d {
	case Deny:
		return 2
	case Ask:
		return 1
	}
	return 0
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
	if words, unsafe, ok := commandLine(call.Args, p.commandArg(), p.argsArg()); ok {
		prefix := commandScope(words, unsafe)
		if prefix == nil {
			return nil
		}
		return CommandGrant{Tool: call.Tool, Arg: p.commandArg(), ArgsArg: p.argsArg(), Prefix: prefix}
	}
	if slices.Contains(p.SessionTools, call.Tool) {
		return ToolGrant{Tool: call.Tool}
	}
	return nil
}

// commandScope 取命令首词，第二个词是纯字母子命令时一并取（go test、git status、npm run）。
// 含 shell 控制符的命令无法安全收窄，返回 nil。
func commandScope(words []string, unsafe bool) []string {
	if unsafe || len(words) == 0 {
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
	Tool    string
	Arg     string
	ArgsArg string
	Prefix  []string
}

// Describe 实现 tools.SessionGrant。
func (g CommandGrant) Describe() string {
	return fmt.Sprintf("本会话内允许 %s 执行以 %q 开头的命令", g.Tool, strings.Join(g.Prefix, " "))
}

// Matches 实现 tools.SessionGrant。
func (g CommandGrant) Matches(call *tools.Call) bool {
	words, unsafe, ok := commandLine(call.Args, g.Arg, g.ArgsArg)
	return ok && !unsafe && call.Tool == g.Tool && wordsHavePrefix(words, strings.Join(g.Prefix, " "))
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

// restrictedPrefix 供拒绝与询问规则使用：首词取文件名，带路径调用同一程序也算匹配。
func restrictedPrefix(words []string, prefix string) bool {
	if len(words) > 0 {
		words = append([]string{path.Base(words[0])}, words[1:]...)
	}
	return wordsHavePrefix(words, prefix)
}

func wordsHavePrefix(words []string, prefix string) bool {
	want := strings.Fields(prefix)
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
