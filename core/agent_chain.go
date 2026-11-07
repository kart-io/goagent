package core

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

// ChainableAgent 顺序执行多个 Agent，并将前一节点的 Metadata 作为下一节点的 Context。
type ChainableAgent struct {
	*BaseAgent
	agents   []Agent
	agentsMu sync.RWMutex
}

// NewChainableAgent 创建拥有独立节点列表的链，节点实例仍由调用方共享。
func NewChainableAgent(name, description string, agents ...Agent) *ChainableAgent {
	capabilities := []string{"chaining"}
	for _, agent := range agents {
		capabilities = append(capabilities, agent.Capabilities()...)
	}
	return &ChainableAgent{
		BaseAgent: NewBaseAgent(name, description, capabilities),
		agents:    slices.Clone(agents),
	}
}

// runnable 将标准入口绑定到同一个链执行源，避免嵌入基类的静态方法分派。
func (c *ChainableAgent) runnable() *StreamRunnable[*AgentInput, *AgentOutput] {
	runner := NewStreamRunnable(StreamModeFinal, func(ctx context.Context, input *AgentInput, emit func(StreamChunk[*AgentOutput]) error) error {
		output, err := c.executeChain(ctx, input, false)
		if err != nil {
			return err
		}
		return emit(StreamChunk[*AgentOutput]{Data: output, Done: true})
	})
	runner.BaseRunnable = c.BaseRunnable
	return runner
}

// Invoke 顺序执行节点的普通入口，保留节点和链的标准回调。
func (c *ChainableAgent) Invoke(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	return c.runnable().Invoke(ctx, input)
}

// InvokeFast 显式选择快速路径，跳过链回调；支持快速入口的节点也会跳过其回调。
func (c *ChainableAgent) InvokeFast(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	return c.executeChain(ctx, input, true)
}

// Stream 产生整条链的最终结果块。
func (c *ChainableAgent) Stream(ctx context.Context, input *AgentInput) (<-chan StreamChunk[*AgentOutput], error) {
	return c.runnable().Stream(ctx, input)
}

// Batch 使用链本身的执行源和并发配置处理输入。
func (c *ChainableAgent) Batch(ctx context.Context, inputs []*AgentInput) ([]*AgentOutput, error) {
	return c.runnable().Batch(ctx, inputs)
}

// Pipe 将链的最终输出传递给后继 Runnable。
func (c *ChainableAgent) Pipe(next Runnable[*AgentOutput, any]) Runnable[*AgentInput, any] {
	return c.runnable().Pipe(next)
}

// RunGenerator 通过相同的普通入口产生单个最终结果。
func (c *ChainableAgent) RunGenerator(ctx context.Context, input *AgentInput) Generator[*AgentOutput] {
	return func(yield func(*AgentOutput, error) bool) {
		output, err := c.Invoke(ctx, input)
		yield(output, err)
	}
}

// WithConfig 返回保留完整节点列表、使用独立配置的链。
func (c *ChainableAgent) WithConfig(config RunnableConfig) Runnable[*AgentInput, *AgentOutput] {
	return c.clone(c.BaseRunnable.WithConfig(config))
}

// WithCallbacks 返回保留完整节点列表并追加回调的链。
func (c *ChainableAgent) WithCallbacks(callbacks ...Callback) Runnable[*AgentInput, *AgentOutput] {
	return c.clone(c.BaseRunnable.WithCallbacks(callbacks...))
}

func (c *ChainableAgent) clone(base *BaseRunnable[*AgentInput, *AgentOutput]) *ChainableAgent {
	// 只复制受保护的节点引用，为副本创建新锁，避免复制已使用的互斥锁。
	c.agentsMu.RLock()
	agents := slices.Clone(c.agents)
	c.agentsMu.RUnlock()
	return &ChainableAgent{
		BaseAgent: &BaseAgent{
			BaseRunnable: base.WithConfig(base.GetConfig()),
			name:         c.name,
			description:  c.description,
			capabilities: slices.Clone(c.capabilities),
		},
		agents: agents,
	}
}

func (c *ChainableAgent) executeChain(ctx context.Context, input *AgentInput, fast bool) (*AgentOutput, error) {
	c.agentsMu.RLock()
	agents := slices.Clone(c.agents)
	c.agentsMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return &AgentOutput{Status: "success", Message: "链中没有节点", Timestamp: time.Now()}, nil
	}
	if input == nil {
		return nil, fmt.Errorf("链输入不能为空")
	}
	current := input
	var output *AgentOutput
	for i, agent := range agents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var err error
		if fast {
			output, err = TryInvokeFast(ctx, agent, current)
		} else {
			output, err = agent.Invoke(ctx, current)
		}
		if err != nil {
			return nil, err
		}
		if output == nil {
			return nil, fmt.Errorf("链节点 %q 返回空输出", agent.Name())
		}
		if i < len(agents)-1 {
			options := current.Options
			options.AllowedTools = slices.Clone(options.AllowedTools)
			// 中间输入可能由节点或输出保留引用，不归还对象池，也不复制 Context 的锁。
			current = &AgentInput{
				Task:        current.Task,
				Instruction: current.Instruction,
				Options:     options,
				SessionID:   current.SessionID,
				Timestamp:   time.Now(),
				Context:     maps.Clone(output.Metadata),
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return output, nil
}
