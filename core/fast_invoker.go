package core

import "context"

// FastInvoker 定义快速调用接口
//
// FastInvoker 提供绕过回调和中间件的高性能执行路径，适用于：
//   - Chain 内部调用
//   - 嵌套 Agent 调用
//   - 高频循环场景
//   - 性能关键路径
//
// 性能收益：
//   - 延迟降低 4-6%
//   - 内存分配减少 5-8%
//   - 无回调遍历开销
//   - 无虚拟方法分派开销
//
// 注意事项：
//   - InvokeFast 不触发任何回调（OnStart/OnFinish/OnLLM*/OnTool*）
//   - 不执行中间件逻辑
//   - 适用于内部调用，外部入口应使用标准 Invoke 方法
//   - 调试时建议使用标准 Invoke 以获得完整追踪信息
//
// 使用示例：
//
//	// 检查 Agent 是否支持快速调用
//	if fastAgent, ok := agent.(FastInvoker); ok {
//	    output, err := fastAgent.InvokeFast(ctx, input)
//	} else {
//	    output, err := agent.Invoke(ctx, input)
//	}
//
// 参考文档：docs/guides/INVOKE_FAST_OPTIMIZATION.md
type FastInvoker interface {
	// InvokeFast 快速执行 Agent，跳过回调和中间件
	//
	// 参数：
	//   - ctx: 执行上下文（用于取消和超时控制）
	//   - input: Agent 输入
	//
	// 返回：
	//   - output: Agent 输出
	//   - error: 执行错误
	//
	// 性能特性：
	//   - 无回调触发
	//   - 无中间件执行
	//   - 最小化内存分配
	//   - 优化的执行路径
	InvokeFast(ctx context.Context, input *AgentInput) (*AgentOutput, error)
}

// TryInvokeFast 尝试使用快速调用，如果不支持则回退到标准 Invoke
//
// 这是一个便捷函数，用于在不确定 Agent 是否支持 FastInvoker 时使用
//
// 使用示例：
//
//	output, err := core.TryInvokeFast(ctx, agent, input)
//
// 等价于：
//
//	var output *core.AgentOutput
//	var err error
//	if fastAgent, ok := agent.(core.FastInvoker); ok {
//	    output, err = fastAgent.InvokeFast(ctx, input)
//	} else {
//	    output, err = agent.Invoke(ctx, input)
//	}
func TryInvokeFast(ctx context.Context, agent Agent, input *AgentInput) (*AgentOutput, error) {
	if fastAgent, ok := agent.(FastInvoker); ok {
		return fastAgent.InvokeFast(ctx, input)
	}
	return agent.Invoke(ctx, input)
}

// IsFastInvoker 检查 Agent 是否支持快速调用
//
// 使用示例：
//
//	if core.IsFastInvoker(agent) {
//	    // 使用快速调用路径
//	    output, err := agent.(core.FastInvoker).InvokeFast(ctx, input)
//	} else {
//	    // 使用标准路径
//	    output, err := agent.Invoke(ctx, input)
//	}
func IsFastInvoker(agent Agent) bool {
	_, ok := agent.(FastInvoker)
	return ok
}
