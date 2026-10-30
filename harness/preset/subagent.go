package preset

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// ComponentSubagent 是子 Agent 组件名。
const ComponentSubagent = "subagent"

// SubagentConfig 是 subagent 组件的配置：提供一个工具，把任务交给另一个 preset 装配的子 Agent，返回其最终答复。
// 子 Agent 与父 Agent 一同装配：它的每次调用同样要经过父 Agent 的调用前钩子与守卫，沙箱不宽于父 Agent，
// 父 Agent 的 approval: never 同样适用，因此权限是父 Agent、目标 preset 与宿主上限的交集。
type SubagentConfig struct {
	// Preset 是子 Agent 的 preset id。
	Preset string `yaml:"preset"`
	// ToolName 默认 agent_<preset>，preset id 中的其他字符替换为下划线。
	ToolName    string `yaml:"toolName"`
	Description string `yaml:"description"`
	// ToolFilter 在初始化之前限定子 Agent 的工具，被完全过滤掉的组件不初始化。
	ToolFilter ToolFilter `yaml:"toolFilter"`
	// MaxDepth 是允许的嵌套深度，默认 1：子 Agent 自己不能再委派。
	MaxDepth *int `yaml:"maxDepth"`
	// Timeout 是一次委派的上限，默认 10m。
	Timeout time.Duration `yaml:"timeout"`
}

func init() {
	Register(Spec[SubagentConfig]{
		Name: ComponentSubagent, Version: "1",
		Validate: func(c *SubagentConfig) error {
			if c.Preset == "" {
				return errors.New("需要 preset")
			}
			if c.ToolName == "" {
				c.ToolName = "agent_" + strings.Map(func(r rune) rune {
					if r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
						return r
					}
					return '_'
				}, c.Preset)
			}
			if !toolNamePattern.MatchString(c.ToolName) {
				return fmt.Errorf("toolName %q 不合法，须匹配 %s", c.ToolName, toolNamePattern)
			}
			if c.Description == "" {
				c.Description = "把任务交给子 Agent（preset " + c.Preset + "）独立完成，返回其最终答复。task 须包含完成任务所需的全部上下文。"
			}
			if c.MaxDepth == nil {
				one := 1
				c.MaxDepth = &one
			}
			if *c.MaxDepth < 0 {
				return errors.New("maxDepth 不能为负")
			}
			if c.Timeout == 0 {
				c.Timeout = 10 * time.Minute
			}
			if c.Timeout < 0 {
				return errors.New("timeout 不能为负")
			}
			return nil
		},
		References: func(c *SubagentConfig) []string { return []string{c.Preset} },
		Build:      buildSubagent,
	})
}

func buildSubagent(ctx context.Context, a *Assembler, c *SubagentConfig) error {
	if a.host.RunAgent == nil {
		return errors.New("子 Agent 需要 Host.RunAgent")
	}
	depth := a.Depth() + 1
	if depth > *c.MaxDepth {
		return fmt.Errorf("子 Agent 嵌套深度 %d 超过 maxDepth %d", depth, *c.MaxDepth)
	}
	target, ok := a.preset.set.Get(c.Preset)
	if !ok {
		return fmt.Errorf("preset %s 不存在", c.Preset)
	}
	tool := &subagentTool{name: c.ToolName, description: c.Description, timeout: c.Timeout, run: a.host.RunAgent}
	a.AddTools(tool)
	slot := a.cur
	parentSandbox := Sandbox("")
	a.AfterPublish(func(parent *tools.Pipeline) error {
		if a.workspace != nil {
			parentSandbox = a.workspaceInfo.Sandbox
		}
		filter := c.ToolFilter
		child, err := build(ctx, target, a.host, buildOptions{
			depth:  depth,
			parent: &parentScope{pre: parent.Pre, guards: parent.Guards, sandbox: parentSandbox, denyApproval: a.denyApproval},
			filter: &filter,
		})
		if err != nil {
			return fmt.Errorf("子 Agent %s: %w", c.Preset, err)
		}
		a.closers = append(a.closers, child.Close)
		tool.child = child
		if a.contribs[slot].info == nil {
			a.contribs[slot].info = map[string]string{}
		}
		a.contribs[slot].info["manifest"] = child.Manifest.Digest()
		return nil
	})
	return nil
}

type subagentTool struct {
	name, description string
	timeout           time.Duration
	run               func(context.Context, *Assembly, string) (string, error)
	child             *Assembly
}

func (t *subagentTool) Name() string                   { return t.name }
func (t *subagentTool) Description() string            { return t.description }
func (t *subagentTool) DispatchTimeout() time.Duration { return t.timeout }
func (t *subagentTool) ArgsSchema() string {
	return `{"type":"object","properties":{"task":{"type":"string","description":"交给子 Agent 的任务，须包含完成任务所需的全部上下文"}},"required":["task"],"additionalProperties":false}`
}

// Invoke 运行子 Agent。父 Agent 的日志已记录整次委派，子 Agent 不再写入日志：恢复时中断的委派按父调用的策略整体重跑。
func (t *subagentTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	task, _ := input.Args["task"].(string)
	if strings.TrimSpace(task) == "" {
		return tools.Rejected("task must not be empty", nil), nil
	}
	answer, err := t.run(durable.WithJournal(ctx, nil), t.child, task)
	if err != nil {
		return nil, fmt.Errorf("子 Agent %s: %w", t.name, err)
	}
	return &interfaces.ToolOutput{Success: true, Result: map[string]any{"answer": answer}}, nil
}

// BuilderRunner 返回用 builder 运行子 Agent 的 Host.RunAgent：client 为模型，maxIterations 为子 Agent 的最大轮数。
func BuilderRunner(client llm.Client, maxIterations int) func(context.Context, *Assembly, string) (string, error) {
	return func(ctx context.Context, asm *Assembly, task string) (string, error) {
		agent, err := Configure(builder.NewSimpleBuilder(client), asm).WithMaxIterations(maxIterations).Build()
		if err != nil {
			return "", err
		}
		out, err := agent.AsRunnable().Invoke(ctx, task)
		if err != nil {
			return "", err
		}
		return fmt.Sprint(out.Result), nil
	}
}
