package preset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/coding"
)

// ComponentClaudeCodeHooks 是 Claude Code 命令钩子的兼容组件名。
const ComponentClaudeCodeHooks = "claude-code-hooks"

// 支持的钩子事件；其余事件依赖 Agent 循环的扩展点，尚未支持。
const (
	hookPreToolUse  = "PreToolUse"
	hookPostToolUse = "PostToolUse"
)

// ClaudeCodeHooksConfig 是 claude-code-hooks 组件的配置，钩子格式与 Claude Code 的 settings.json 相同：
// 钩子命令经 sh -c 在宿主机工作区根目录执行，stdin 为调用信息的 JSON，退出码 2 表示阻止（stderr 为原因），
// stdout 可返回 JSON 裁决。多个钩子的裁决按 deny > ask > allow 合并，allow 不越过其他权限规则。
type ClaudeCodeHooksConfig struct {
	// Path 是工作区内的 settings 文件（如 .claude/settings.json），读取其中的 hooks；文件中不支持的事件与类型被忽略。
	// 文件属于工作区内容，其中的命令无论本行来自哪一层都需要授权（或列入 limits.hostProcesses）。
	Path string `yaml:"path"`
	// Hooks 是内联的钩子定义，只能使用支持的事件与 command 类型。
	Hooks map[string][]HookMatcher `yaml:"hooks"`
	// ToolNames 把工具名映射为钩子看到的名称，默认把编码工具映射为 Claude Code 的工具名（run_command → Bash 等）。
	ToolNames map[string]string `yaml:"toolNames"`
	// Timeout 是单个钩子的默认超时，默认 60s；钩子自己的 timeout（秒）优先。
	Timeout time.Duration `yaml:"timeout"`
}

// HookMatcher 按工具名选择钩子。Matcher 为空或 * 时匹配全部，否则作为完整匹配的正则表达式（如 Edit|Write）。
type HookMatcher struct {
	Matcher string        `yaml:"matcher" json:"matcher"`
	Hooks   []HookCommand `yaml:"hooks" json:"hooks"`
}

// HookCommand 是一个命令钩子。Timeout 以秒计。
type HookCommand struct {
	Type    string `yaml:"type" json:"type"`
	Command string `yaml:"command" json:"command"`
	Timeout int    `yaml:"timeout" json:"timeout"`
}

var defaultHookToolNames = map[string]string{
	coding.ToolRunCommand: "Bash", coding.ToolReadFile: "Read", coding.ToolWriteFile: "Write", coding.ToolEditFile: "Edit",
	coding.ToolMultiEdit: "MultiEdit", coding.ToolGlob: "Glob", coding.ToolGrep: "Grep",
}

func init() {
	Register(Spec[ClaudeCodeHooksConfig]{
		Name: ComponentClaudeCodeHooks, Version: "1", Singleton: true,
		Validate: func(c *ClaudeCodeHooksConfig) error {
			if c.Path == "" && len(c.Hooks) == 0 {
				return errors.New("需要 path 或 hooks")
			}
			for event, matchers := range c.Hooks {
				if event != hookPreToolUse && event != hookPostToolUse {
					return fmt.Errorf("不支持的钩子事件 %s，可选 %s、%s", event, hookPreToolUse, hookPostToolUse)
				}
				if err := validateMatchers(matchers, true); err != nil {
					return err
				}
			}
			if c.Timeout == 0 {
				c.Timeout = 60 * time.Second
			}
			if c.Timeout < 0 {
				return errors.New("timeout 不能为负")
			}
			return nil
		},
		Needs: func(c *ClaudeCodeHooksConfig) Needs { return Needs{HostProcesses: hookCommands(c.Hooks)} },
		Resolve: func(root string, c *ClaudeCodeHooksConfig) (Needs, error) {
			if c.Path == "" {
				return Needs{}, nil
			}
			loaded, err := loadHookFile(root, c.Path)
			if err != nil {
				return Needs{}, err
			}
			merged := make(map[string][]HookMatcher, len(c.Hooks)+len(loaded))
			maps.Copy(merged, c.Hooks)
			for event, matchers := range loaded {
				merged[event] = append(slices.Clone(merged[event]), matchers...)
			}
			c.Hooks = merged
			return Needs{HostProcesses: hookCommands(loaded)}, nil
		},
		Build: buildHooks,
	})
}

func validateMatchers(matchers []HookMatcher, strict bool) error {
	for _, m := range matchers {
		if _, err := matcherRegexp(m.Matcher); err != nil {
			return fmt.Errorf("matcher %q: %w", m.Matcher, err)
		}
		for _, h := range m.Hooks {
			if strict && h.Type != "command" {
				return fmt.Errorf("不支持的钩子类型 %q，只支持 command", h.Type)
			}
			if h.Type == "command" && strings.TrimSpace(h.Command) == "" {
				return errors.New("钩子缺少 command")
			}
			if h.Timeout < 0 {
				return errors.New("钩子 timeout 不能为负")
			}
		}
	}
	return nil
}

func matcherRegexp(m string) (*regexp.Regexp, error) {
	if m == "" || m == "*" {
		return nil, nil
	}
	return regexp.Compile("^(?:" + m + ")$")
}

func hookCommands(hooks map[string][]HookMatcher) []string {
	var out []string
	for _, event := range slices.Sorted(maps.Keys(hooks)) {
		for _, m := range hooks[event] {
			for _, h := range m.Hooks {
				if h.Type == "command" {
					out = append(out, h.Command)
				}
			}
		}
	}
	return out
}

// loadHookFile 读取工作区内 settings 文件的 hooks，只保留支持的事件与 command 类型。
func loadHookFile(root, path string) (map[string][]HookMatcher, error) {
	if root == "" {
		return nil, errors.New("读取钩子文件需要 Host.Root")
	}
	local, err := env.NewLocalFS(root)
	if err != nil {
		return nil, err
	}
	data, _, err := local.ReadFile(context.Background(), path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("钩子文件 %s: %w", path, err)
	}
	var file struct {
		Hooks map[string][]HookMatcher `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("钩子文件 %s: %w", path, err)
	}
	out := map[string][]HookMatcher{}
	for _, event := range []string{hookPreToolUse, hookPostToolUse} {
		for _, m := range file.Hooks[event] {
			m.Hooks = slices.DeleteFunc(slices.Clone(m.Hooks), func(h HookCommand) bool { return h.Type != "command" })
			if len(m.Hooks) > 0 {
				out[event] = append(out[event], m)
			}
		}
	}
	for event, matchers := range out {
		if err := validateMatchers(matchers, false); err != nil {
			return nil, fmt.Errorf("钩子文件 %s 的 %s: %w", path, event, err)
		}
	}
	return out, nil
}

type hookRunner struct {
	root      string
	timeout   time.Duration
	toolNames map[string]string
	matchers  map[string][]compiledMatcher
}

type compiledMatcher struct {
	re    *regexp.Regexp
	hooks []HookCommand
}

func buildHooks(_ context.Context, a *Assembler, c *ClaudeCodeHooksConfig) error {
	r := &hookRunner{root: a.Root(), timeout: c.Timeout, toolNames: c.ToolNames, matchers: map[string][]compiledMatcher{}}
	if r.toolNames == nil {
		r.toolNames = defaultHookToolNames
	}
	for event, matchers := range c.Hooks {
		for _, m := range matchers {
			re, err := matcherRegexp(m.Matcher)
			if err != nil {
				return err
			}
			r.matchers[event] = append(r.matchers[event], compiledMatcher{re: re, hooks: m.Hooks})
		}
	}
	if len(r.matchers[hookPreToolUse]) > 0 {
		a.AddPreHook(r.pre)
	}
	if len(r.matchers[hookPostToolUse]) > 0 {
		a.AddPostHook(r.post)
	}
	a.SetInfo("hooks", strings.Join(hookCommands(c.Hooks), "\n"))
	return nil
}

// hookName 返回钩子看到的工具名。
func (r *hookRunner) hookName(tool string) string {
	if name, ok := r.toolNames[tool]; ok {
		return name
	}
	return tool
}

func (r *hookRunner) selected(event string, call *tools.Call) []HookCommand {
	var out []HookCommand
	for _, m := range r.matchers[event] {
		if m.re == nil || m.re.MatchString(r.hookName(call.Tool)) || m.re.MatchString(call.Tool) {
			out = append(out, m.hooks...)
		}
	}
	return out
}

// input 构造钩子的 stdin。文件类工具同时提供 Claude Code 使用的 file_path 字段。
func (r *hookRunner) input(event string, call *tools.Call, response any) ([]byte, error) {
	toolInput := maps.Clone(call.Args)
	if p, ok := toolInput["path"]; ok && r.hookName(call.Tool) != call.Tool {
		toolInput["file_path"] = p
	}
	payload := map[string]any{
		"session_id": call.SessionID, "transcript_path": "", "cwd": r.root,
		"hook_event_name": event, "tool_name": r.hookName(call.Tool), "tool_input": toolInput,
	}
	if event == hookPostToolUse {
		payload["tool_response"] = response
	}
	return json.Marshal(payload)
}

type hookResult struct {
	exitCode       int
	stdout, stderr []byte
}

// run 执行一个钩子。命令未能启动或超时返回 error；钩子失败不阻止调用（与 Claude Code 一致，只有退出码 2 阻止）。
func (r *hookRunner) run(ctx context.Context, h HookCommand, input []byte) (hookResult, error) {
	timeout := r.timeout
	if h.Timeout > 0 {
		timeout = time.Duration(h.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", h.Command)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+r.root)
	cmd.Stdin = bytes.NewReader(input)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := hookResult{stdout: stdout.Bytes(), stderr: stderr.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && ctx.Err() == nil:
		res.exitCode = exitErr.ExitCode()
	default:
		return res, err
	}
	return res, nil
}

// hookOutput 是钩子 stdout 中的 JSON 字段（Claude Code 协议）。
type hookOutput struct {
	Decision           string `json:"decision"`
	Reason             string `json:"reason"`
	HookSpecificOutput struct {
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
		AdditionalContext        string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func parseHookOutput(stdout []byte) (hookOutput, bool) {
	var out hookOutput
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &out) != nil {
		return hookOutput{}, false
	}
	return out, true
}

func (r *hookRunner) pre(ctx context.Context, call *tools.Call) (tools.PreDecision, error) {
	hooks := r.selected(hookPreToolUse, call)
	if len(hooks) == 0 {
		return tools.PreDecision{}, nil
	}
	input, err := r.input(hookPreToolUse, call, nil)
	if err != nil {
		return tools.PreDecision{}, err
	}
	var denied, asked []string
	for _, h := range hooks {
		res, err := r.run(ctx, h, input)
		if err != nil {
			if ctx.Err() != nil {
				return tools.PreDecision{}, ctx.Err()
			}
			continue
		}
		reason := strings.TrimSpace(string(res.stderr))
		if res.exitCode == 2 {
			denied = append(denied, nonEmptyReason(reason, "blocked by hook"))
			continue
		}
		out, ok := parseHookOutput(res.stdout)
		if res.exitCode != 0 || !ok {
			continue
		}
		decision, why := out.HookSpecificOutput.PermissionDecision, out.HookSpecificOutput.PermissionDecisionReason
		if decision == "" {
			switch out.Decision {
			case "block":
				decision = "deny"
			case "approve":
				decision = "allow"
			}
			why = out.Reason
		}
		switch decision {
		case "deny":
			denied = append(denied, nonEmptyReason(why, "blocked by hook"))
		case "ask":
			asked = append(asked, nonEmptyReason(why, "hook requires approval"))
		}
	}
	switch {
	case len(denied) > 0:
		return tools.PreDecision{Kind: tools.PreDeny, Feedback: strings.Join(denied, "; ")}, nil
	case len(asked) > 0:
		return tools.PreDecision{Kind: tools.PreAsk, Feedback: strings.Join(asked, "; ")}, nil
	}
	return tools.PreDecision{}, nil
}

func (r *hookRunner) post(ctx context.Context, call *tools.Call, out *interfaces.ToolOutput) (tools.PostDecision, error) {
	hooks := r.selected(hookPostToolUse, call)
	if len(hooks) == 0 {
		return tools.PostDecision{}, nil
	}
	input, err := r.input(hookPostToolUse, call, out.Result)
	if err != nil {
		return tools.PostDecision{}, err
	}
	var notes []string
	for _, h := range hooks {
		res, err := r.run(ctx, h, input)
		if err != nil {
			if ctx.Err() != nil {
				return tools.PostDecision{}, ctx.Err()
			}
			continue
		}
		// 工具已经执行，阻止只能把原因交给模型。
		if res.exitCode == 2 {
			notes = append(notes, nonEmptyReason(strings.TrimSpace(string(res.stderr)), "a hook flagged this result"))
			continue
		}
		parsed, ok := parseHookOutput(res.stdout)
		if res.exitCode != 0 || !ok {
			continue
		}
		if parsed.Decision == "block" {
			notes = append(notes, nonEmptyReason(parsed.Reason, "a hook flagged this result"))
		}
		if ctx := parsed.HookSpecificOutput.AdditionalContext; ctx != "" {
			notes = append(notes, ctx)
		}
	}
	return tools.PostDecision{AdditionalContext: notes}, nil
}

func nonEmptyReason(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
