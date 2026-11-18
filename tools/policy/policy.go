// Package policy provides permission rules decided by tool name and command
// prefix, used as a pre-call hook of tools.Pipeline. They apply to any tool
// (coding tools, MCP tools, HTTP tools).
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

// Decision is a rule's verdict. The zero value is Ask.
type Decision int

const (
	// Ask hands the call to the approver for confirmation.
	Ask Decision = iota
	// Allow executes the call directly.
	Allow
	// Deny does not execute the call and informs the model via tools.Rejected.
	Deny
)

// Rule is a permission rule.
type Rule struct {
	// Tool is the tool name; "*" matches all tools and the empty string
	// matches none.
	Tool string
	// Command only applies to calls with a command argument
	// (Policy.CommandArg): it matches when the command, split on whitespace,
	// starts with these words, such as "go test". Allow rules require an
	// identical first word, and commands containing shell control characters,
	// quotes or backslashes do not match allow rules. Deny and ask rules
	// compare the file name of the first word, so /usr/bin/git push matches
	// "git push"; when a command contains the characters above and cannot be
	// judged, the rule decides at least Ask and hands it to the approver.
	// An allow rule allows all arguments of that command (go test -exec can
	// run any program, and the test code itself is written by the model); deny
	// rules can still be bypassed with env rm, sh -c, script files and the
	// like, and cannot be relied on. Use a container executor when isolation is
	// needed.
	Command  string
	Decision Decision
}

// Policy takes the strictest verdict among all matching rules (Deny > Ask >
// Allow), regardless of rule order, and uses Default when none match. Rules
// added later can therefore only tighten the result of existing rules and
// cannot override an existing Deny or Ask.
type Policy struct {
	Rules   []Rule
	Default Decision
	// CommandArg is the argument name read for command prefix matching,
	// "command" by default.
	CommandArg string
	// ArgsArg is the argument name of the argument array, "args" by default.
	// argv-style tools (such as the shell tool) put subcommands here; when
	// matching, they follow the words split from CommandArg, and each array
	// element is taken as one word without further splitting.
	ArgsArg string
	// SessionTools lists the tools that may be allowed as a whole tool "for
	// this session" during approval. Calls with a command argument are always
	// narrowed to a command prefix; other tools not in the list can only be
	// approved once.
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

// Decide returns the verdict for the call.
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

// Hook adapts the policy to a tools.PreHook.
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

// CommandGrant allows commands in the same tool that start with Prefix and
// contain no shell control characters.
type CommandGrant struct {
	Tool    string
	Arg     string
	ArgsArg string
	Prefix  []string
}

// Describe implements tools.SessionGrant.
func (g CommandGrant) Describe() string {
	return fmt.Sprintf("本会话内允许 %s 执行以 %q 开头的命令", g.Tool, strings.Join(g.Prefix, " "))
}

// Matches implements tools.SessionGrant.
func (g CommandGrant) Matches(call *tools.Call) bool {
	words, unsafe, ok := commandLine(call.Args, g.Arg, g.ArgsArg)
	return ok && !unsafe && call.Tool == g.Tool && wordsHavePrefix(words, strings.Join(g.Prefix, " "))
}

// ToolGrant allows all calls of the same tool.
type ToolGrant struct{ Tool string }

// Describe implements tools.SessionGrant.
func (g ToolGrant) Describe() string { return "本会话内允许 " + g.Tool + " 的全部调用" }

// Matches implements tools.SessionGrant.
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
