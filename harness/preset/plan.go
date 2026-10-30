package preset

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/coding"
	"github.com/kart-io/goagent/tools/policy"
	"github.com/kart-io/goagent/tools/spill"
)

// ComponentPlanMode 是计划模式组件名。
const ComponentPlanMode = "plan-mode"

// 计划模式的工具。
const (
	ToolEnterPlanMode = "enter_plan_mode"
	ToolExitPlanMode  = "exit_plan_mode"
)

// PlanModeConfig 是 plan-mode 组件的配置。计划模式中只能调用只读工具；模型调用 exit_plan_mode 提交计划，
// 经审批人批准后退出计划模式（approval: never 时无法退出）。切换模式只改变每步附加的运行上下文，
// 不增删工具，system 前缀保持稳定。
//
// 模式按会话记录：每步开始时从对话中最近一次成功的 enter_plan_mode / exit_plan_mode 结果恢复，
// 对话中没有时取 initial。只作用于原生工具流。
type PlanModeConfig struct {
	// Initial 表示会话开始时处于计划模式，默认 true。
	Initial *bool `yaml:"initial"`
	// Allow 是计划模式中可以调用的工具。默认为只读的编码工具、todo_write、read_spill 与 load_skill。
	Allow []string `yaml:"allow"`
}

var defaultPlanAllow = []string{
	coding.ToolReadFile, coding.ToolGlob, coding.ToolGrep, coding.ToolGitStatus, coding.ToolGitDiff, coding.ToolGitLog,
	coding.ToolTodoWrite, spill.ToolReadSpill, ToolLoadSkill,
}

func init() {
	Register(Spec[PlanModeConfig]{
		Name: ComponentPlanMode, Version: "1", Singleton: true,
		Validate: func(c *PlanModeConfig) error {
			if c.Initial == nil {
				on := true
				c.Initial = &on
			}
			if c.Allow == nil {
				c.Allow = defaultPlanAllow
			}
			return nil
		},
		Build: buildPlanMode,
	})
}

func buildPlanMode(_ context.Context, a *Assembler, c *PlanModeConfig) error {
	p := &planMode{initial: *c.Initial, allow: c.Allow, active: map[string]bool{}}
	a.AddTools(p.enterTool(), p.exitTool())
	// 进入计划模式只会收紧权限，无需审批；退出总要审批，项目层的 allow 越不过 ask。
	a.AddRules([]policy.Rule{{Tool: ToolEnterPlanMode, Decision: policy.Allow}, {Tool: ToolExitPlanMode, Decision: policy.Ask}}, nil, false)
	a.AddPreHook(p.pre)
	a.AddPreStep(p.restore)
	a.AddPromptContext(builder.PromptContext{Name: ComponentPlanMode, Text: p.context})
	return nil
}

type planMode struct {
	initial bool
	allow   []string

	mu     sync.Mutex
	active map[string]bool
}

func (p *planMode) isActive(session string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if on, ok := p.active[session]; ok {
		return on
	}
	return p.initial
}

func (p *planMode) set(session string, on bool) {
	p.mu.Lock()
	p.active[session] = on
	p.mu.Unlock()
}

// planResult 是 enter/exit 工具成功时的结果，plan_mode 字段用于从对话中恢复模式。
type planResult struct {
	PlanMode string `json:"plan_mode"`
	Message  string `json:"message"`
}

const (
	planEntered = "entered"
	planExited  = "exited"
)

// restore 从对话中最近一次成功的切换结果恢复模式。持久运行恢复时，被回放的步骤不执行 PreStep，
// 之后的第一步仍从完整对话恢复；对话中没有切换结果（如只带文本的记忆历史）时保留进程内的状态。
func (p *planMode) restore(ctx context.Context, step *builder.Step) (builder.PreStepResult, error) {
	if mode, ok := lastPlanSwitch(step.Messages); ok {
		p.set(tools.SessionIDFrom(ctx), mode == planEntered)
	}
	return builder.PreStepResult{}, nil
}

func lastPlanSwitch(messages []llm.Message) (string, bool) {
	calls := map[string]string{}
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == ToolEnterPlanMode || tc.Function.Name == ToolExitPlanMode {
				calls[tc.ID] = tc.Function.Name
			}
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != "tool" || calls[m.ToolCallID] == "" {
			continue
		}
		var r planResult
		if json.Unmarshal([]byte(m.Content), &r) == nil && (r.PlanMode == planEntered || r.PlanMode == planExited) {
			return r.PlanMode, true
		}
	}
	return "", false
}

func (p *planMode) pre(_ context.Context, call *tools.Call) (tools.PreDecision, error) {
	active := p.isActive(call.SessionID)
	switch {
	case call.Tool == ToolEnterPlanMode:
		return tools.PreDecision{}, nil
	case call.Tool == ToolExitPlanMode && !active:
		return tools.PreDecision{Kind: tools.PreDeny, Feedback: "not in plan mode"}, nil
	case call.Tool == ToolExitPlanMode || !active || slices.Contains(p.allow, call.Tool):
		return tools.PreDecision{}, nil
	}
	return tools.PreDecision{Kind: tools.PreDeny, Feedback: fmt.Sprintf(
		"plan mode is active: %s is not allowed. Investigate with read-only tools, then call %s with your plan and wait for approval",
		call.Tool, ToolExitPlanMode)}, nil
}

func (p *planMode) context(scope builder.PromptScope) string {
	if !p.isActive(scope.SessionID) {
		return ""
	}
	return fmt.Sprintf("当前处于计划模式：只调查和规划，不修改文件、不执行有副作用的命令（可用工具：%s）。"+
		"计划完成后调用 %s 提交计划，获批后才能执行。", strings.Join(p.allow, "、"), ToolExitPlanMode)
}

func (p *planMode) enterTool() interfaces.Tool {
	return tools.NewBaseTool(ToolEnterPlanMode,
		"进入计划模式：之后只能调用只读工具调查和规划，直到用 "+ToolExitPlanMode+" 提交的计划获批。任务复杂、需要先和用户对齐方案时使用。",
		`{"type":"object","properties":{},"additionalProperties":false}`,
		func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			p.set(tools.SessionIDFrom(ctx), true)
			return &interfaces.ToolOutput{Success: true, Result: planResult{PlanMode: planEntered, Message: "已进入计划模式"}}, nil
		})
}

func (p *planMode) exitTool() interfaces.Tool {
	return tools.NewBaseTool(ToolExitPlanMode,
		"提交计划并请求退出计划模式。plan 是完整的实施计划（Markdown），审批人批准后才能开始修改。",
		`{"type":"object","properties":{"plan":{"type":"string","description":"完整的实施计划（Markdown）"}},"required":["plan"],"additionalProperties":false}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			if plan, _ := input.Args["plan"].(string); strings.TrimSpace(plan) == "" {
				return tools.Rejected("plan must not be empty", nil), nil
			}
			p.set(tools.SessionIDFrom(ctx), false)
			return &interfaces.ToolOutput{Success: true, Result: planResult{PlanMode: planExited, Message: "计划已获批准，已退出计划模式，可以开始执行"}}, nil
		})
}
