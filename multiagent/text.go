package multiagent

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Turn 是文本接力中的一轮：执行该轮的 Step 名与其输出。
type Turn struct {
	Agent  string
	Output string
}

// Conversation 是文本接力的状态：原始任务与按顺序追加的各轮输出。
type Conversation struct {
	Input string
	Turns []Turn
}

type textConfig struct {
	render    func(Conversation) any
	lastTurns int
	renderSet bool
	lastSet   bool
}

// TextOption 配置 TextStep 的输入格式。
type TextOption func(*textConfig)

// WithRender 自定义 Agent 输入。render 必须是 Conversation 的纯函数：恢复时输出不同会使模型请求意图不一致；
// 修改 render 等同于修改 Agent 行为，须提升图版本。
func WithRender(render func(Conversation) any) TextOption {
	return func(c *textConfig) { c.render, c.renderSet = render, true }
}

// WithLastTurns 使用固定窗口：输入为 Input 加最近 k 轮。
func WithLastTurns(k int) TextOption {
	return func(c *textConfig) { c.lastTurns, c.lastSet = k, true }
}

// TextStep 把 Agent 接成文本接力的一步：默认输入为 Input 加每一轮的“[Agent]: Output”，按顺序逐行拼接、不截断；
// 输出取 AgentOutput.Result（字符串原样，其他类型按 JSON 编码），追加为 Turn{Agent: name}，空回答也记录。
func TextStep(name string, agent AgentFactory, opts ...TextOption) (Step[Conversation], error) {
	var cfg textConfig
	for _, opt := range opts {
		if opt == nil {
			return Step[Conversation]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "TextStep %q 收到 nil 选项", name).WithComponent("multiagent").WithOperation("text_step")
		}
		opt(&cfg)
	}
	switch {
	case cfg.renderSet && cfg.lastSet:
		return Step[Conversation]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "TextStep %q 不能同时使用 WithRender 与 WithLastTurns", name).WithComponent("multiagent").WithOperation("text_step")
	case cfg.renderSet && cfg.render == nil:
		return Step[Conversation]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "TextStep %q 的 WithRender 需要非 nil 函数", name).WithComponent("multiagent").WithOperation("text_step")
	case cfg.lastSet && cfg.lastTurns <= 0:
		return Step[Conversation]{}, agentErrors.Newf(agentErrors.CodeInvalidConfig, "TextStep %q 的 WithLastTurns 需要大于 0 的轮数", name).WithComponent("multiagent").WithOperation("text_step")
	}
	render := cfg.render
	if render == nil {
		k := cfg.lastTurns
		render = func(c Conversation) any { return renderTurns(c, k) }
	}
	in := func(c Conversation) (any, error) { return render(c), nil }
	out := func(c Conversation, o *builder.AgentOutput) (Conversation, error) {
		text, err := resultText(o.Result)
		if err != nil {
			return c, err
		}
		// Clip 保证追加时分配新数组，不改写其他状态共享的底层数组。
		c.Turns = append(slices.Clip(c.Turns), Turn{Agent: name, Output: text})
		return c, nil
	}
	return AgentStep(name, agent, in, out)
}

// renderTurns 是默认输入格式；k > 0 时只保留最近 k 轮。
func renderTurns(c Conversation, k int) string {
	turns := c.Turns
	if k > 0 && len(turns) > k {
		turns = turns[len(turns)-k:]
	}
	var b strings.Builder
	b.WriteString(c.Input)
	for _, t := range turns {
		b.WriteString("\n[")
		b.WriteString(t.Agent)
		b.WriteString("]: ")
		b.WriteString(t.Output)
	}
	return b.String()
}

// resultText 把 Agent 结果转成文本：字符串原样，其他类型按 JSON 编码。
func resultText(result any) (string, error) {
	if s, ok := result.(string); ok {
		return s, nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("编码 Agent 结果: %w", err)
	}
	return string(data), nil
}

// CloneConversation 复制 Turns，供 ParallelConfig.Clone 使用；浅拷贝会让并行分支写同一底层数组。
func CloneConversation(c Conversation) Conversation {
	return Conversation{Input: c.Input, Turns: slices.Clone(c.Turns)}
}

// MergeTurns 按分支登记顺序，追加各分支在 base 之后新增的 Turns。分支的 Turns 短于 base 或前缀不同时返回错误。
func MergeTurns(_ context.Context, base Conversation, branches []Conversation) (Conversation, error) {
	out := CloneConversation(base)
	n := len(base.Turns)
	for i, b := range branches {
		if len(b.Turns) < n || !slices.Equal(b.Turns[:n], base.Turns) {
			return Conversation{}, agentErrors.Newf(agentErrors.CodeInvalidOutput, "第 %d 个分支的记录不以合并前的记录为前缀", i+1).WithComponent("multiagent").WithOperation("merge_turns")
		}
		out.Turns = append(out.Turns, b.Turns[n:]...)
	}
	return out, nil
}

// TextNext 从成员 Step 推导名称，返回供 RouterConfig.Next 使用的路由：取最后一轮输出的第一行并去掉首尾空白，
// 必须恰好等于某个成员名或 Finish；记录为空、首行为空或不匹配时返回包装 ErrInvalidRoute 的错误，不做模糊匹配。
func TextNext(members ...Step[Conversation]) (func(context.Context, Conversation) (string, error), error) {
	if len(members) == 0 {
		return nil, agentErrors.NewInvalidConfigError("multiagent", "members", "TextNext 需要至少一个成员")
	}
	names := make(map[string]bool, len(members))
	for _, m := range members {
		n := m.Name
		if n == "" || strings.TrimSpace(n) != n || strings.ContainsAny(n, "\r\n") || n == Finish || names[n] {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "TextNext 的成员名 %q 必须非空、单行、不重复且不等于 %s", n, Finish).WithComponent("multiagent").WithOperation("text_next")
		}
		names[n] = true
	}
	return func(_ context.Context, c Conversation) (string, error) {
		if len(c.Turns) == 0 {
			return "", fmt.Errorf("%w：记录为空", agentErrors.ErrInvalidRoute)
		}
		line, _, _ := strings.Cut(c.Turns[len(c.Turns)-1].Output, "\n")
		line = strings.TrimSpace(line)
		if line == "" {
			return "", fmt.Errorf("%w：最后一轮输出的首行为空", agentErrors.ErrInvalidRoute)
		}
		if line != Finish && !names[line] {
			return "", fmt.Errorf("%w：%q 不是成员名或 %s", agentErrors.ErrInvalidRoute, line, Finish)
		}
		return line, nil
	}, nil
}
