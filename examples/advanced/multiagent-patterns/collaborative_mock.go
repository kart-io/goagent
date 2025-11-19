package main

import (
	"context"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/multiagent"
)

// CollaborativeMockAgent 是一个支持 CollaborativeAgent 接口的模拟Agent
type CollaborativeMockAgent struct {
	name         string
	description  string
	capabilities []string
	role         multiagent.Role
	invokeFn     func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error)
}

// NewCollaborativeMockAgent 创建一个新的协作模拟Agent
func NewCollaborativeMockAgent(name, description string, role multiagent.Role) *CollaborativeMockAgent {
	return &CollaborativeMockAgent{
		name:         name,
		description:  description,
		capabilities: []string{},
		role:         role,
	}
}

// SetInvokeFn 设置自定义的invoke函数
func (a *CollaborativeMockAgent) SetInvokeFn(fn func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error)) {
	a.invokeFn = fn
}

// Name 返回Agent名称
func (a *CollaborativeMockAgent) Name() string {
	return a.name
}

// Description 返回Agent描述
func (a *CollaborativeMockAgent) Description() string {
	return a.description
}

// Capabilities 返回Agent能力列表
func (a *CollaborativeMockAgent) Capabilities() []string {
	return a.capabilities
}

// GetRole 返回Agent角色
func (a *CollaborativeMockAgent) GetRole() multiagent.Role {
	return a.role
}

// SetRole 设置Agent角色
func (a *CollaborativeMockAgent) SetRole(role multiagent.Role) {
	a.role = role
}

// Invoke 执行Agent逻辑
func (a *CollaborativeMockAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	if a.invokeFn != nil {
		return a.invokeFn(ctx, input)
	}
	return &core.AgentOutput{
		Result: "mock response from " + a.name,
		Status: "success",
	}, nil
}

// Stream 流式执行（未实现）
func (a *CollaborativeMockAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	ch := make(chan core.StreamChunk[*core.AgentOutput])
	close(ch)
	return ch, nil
}

// Batch 批量执行（未实现）
func (a *CollaborativeMockAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	outputs := make([]*core.AgentOutput, len(inputs))
	for i, input := range inputs {
		output, err := a.Invoke(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

// Pipe 管道组合（未实现）
func (a *CollaborativeMockAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return nil
}

// WithCallbacks 添加回调
func (a *CollaborativeMockAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return a
}

// WithConfig 配置
func (a *CollaborativeMockAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return a
}

// ReceiveMessage 接收消息
func (a *CollaborativeMockAgent) ReceiveMessage(ctx context.Context, message multiagent.Message) error {
	// 简化实现
	return nil
}

// SendMessage 发送消息
func (a *CollaborativeMockAgent) SendMessage(ctx context.Context, message multiagent.Message) error {
	// 简化实现
	return nil
}

// Collaborate 参与协作任务
func (a *CollaborativeMockAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	// 将任务输入转换为字符串
	var taskInput string
	if str, ok := task.Input.(string); ok {
		taskInput = str
	} else {
		taskInput = "collaborative task"
	}

	// 调用Invoke处理任务
	output, err := a.Invoke(ctx, &core.AgentInput{Task: taskInput})
	if err != nil {
		return nil, err
	}

	// 返回Assignment
	return &multiagent.Assignment{
		AgentID: a.name,
		Role:    a.role,
		Subtask: task.Input,
		Status:  multiagent.TaskStatusCompleted,
		Result:  output.Result,
	}, nil
}

// Vote 投票
func (a *CollaborativeMockAgent) Vote(ctx context.Context, proposal interface{}) (bool, error) {
	// 简化实现：总是投赞成票
	return true, nil
}
