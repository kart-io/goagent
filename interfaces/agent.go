package interfaces

import "context"

// Agent 表示可以处理输入并产生输出的自主智能体
//
// 所有智能体实现（react、executor、specialized）都应实现此接口
// 这是规范定义，其他所有引用都应使用类型别名
//
// Agent 接口扩展了 Runnable 并添加了智能体特定的方法用于：
//   - 识别智能体（Name、Description、Capabilities）
//   - 生成执行计划
//
// 实现：
//   - core.BaseAgent - 基础实现，支持 Runnable
//   - agents/executor.ExecutorAgent - 工具执行智能体
//   - agents/react.ReactAgent - ReAct 推理智能体
//   - agents/specialized.SpecializedAgent - 领域特定智能体
type Agent interface {
	Runnable

	// Name 返回智能体的标识符
	Name() string

	// Description 返回智能体的功能描述
	Description() string

	// Capabilities 返回智能体的能力列表
	// 描述智能体可以执行的任务或操作
	// 示例：["search", "analyze", "summarize"]
	Capabilities() []string

	// Plan 为给定输入生成执行计划
	// 这是可选的，如果智能体不支持规划可能返回 nil
	Plan(ctx context.Context, input *Input) (*Plan, error)
}

// Runnable 表示可以使用输入调用并产生输出的任何组件
//
// 这是由智能体、链和工具实现的基础接口
// 它提供核心执行能力，包括：
//   - 同步执行（Invoke）
//   - 流式执行（Stream）
//
// # Runnable 接口通过标准执行模式启用组件的组合和链接
//
// 实现：
//   - core.BaseRunnable - 基础实现，支持回调
//   - core.BaseAgent - 智能体特定实现
//   - core.Chain - Runnable 链
type Runnable interface {
	// Invoke 使用给定输入执行 runnable
	// 这是用于同步处理的主要执行方法
	//
	// 参数：
	//   - ctx: 用于取消和超时控制的上下文
	//   - input: runnable 的输入数据
	//
	// 返回：
	//   - output: 执行结果
	//   - error: 执行失败时的错误
	Invoke(ctx context.Context, input *Input) (*Output, error)

	// Stream 使用流式输出支持执行
	// 允许在输出可用时进行处理
	//
	// 参数：
	//   - ctx: 用于取消和超时控制的上下文
	//   - input: runnable 的输入数据
	//
	// 返回：
	//   - chan: 流式数据块通道
	//   - error: 流设置失败时的错误
	Stream(ctx context.Context, input *Input) (<-chan *StreamChunk, error)
}

// Input 表示 runnable 的标准化输入
//
// Input 提供了一个灵活的结构来向 runnable 传递数据：
//   - Messages: 用于基于 LLM 处理的对话历史
//   - State: 持久状态数据
//   - Config: 运行时配置选项
type Input struct {
	// Messages 是对话历史或输入消息
	Messages []Message `json:"messages"`

	// State 是持久状态数据
	// 可以存储任意键值对用于状态管理
	State State `json:"state"`

	// Config 包含运行时配置选项
	// 可以包括模型设置、工具设置等
	Config map[string]interface{} `json:"config,omitempty"`
}

// Output 表示 runnable 的标准化输出
//
// Output 提供了一个结构化的方式来返回结果：
//   - Messages: 生成或修改的消息
//   - State: 更新的状态数据
//   - Metadata: 关于执行的额外信息
type Output struct {
	// Messages 是生成或修改的消息
	Messages []Message `json:"messages"`

	// State 是更新的状态数据
	State State `json:"state"`

	// Metadata 包含关于执行的额外信息
	// 可以包括时序、模型信息、工具调用等
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Message 表示对话中的单条消息
//
// 消息用于：
//   - 聊天历史
//   - LLM 交互
//   - 智能体通信
//
// Role 字段指示消息发送者：
//   - "user": 用户输入
//   - "assistant": AI/智能体响应
//   - "system": 系统指令
//   - "function": 函数/工具输出
type Message struct {
	// Role 标识消息发送者（user、assistant、system、function）
	Role string `json:"role"`

	// Content 是消息的文本内容
	Content string `json:"content"`

	// Name 是发送者的可选名称（用于函数/工具消息）
	Name string `json:"name,omitempty"`
}

// StreamChunk 表示流式输出的数据块
//
// StreamChunk 允许增量处理输出：
//   - Content: 部分或完整输出
//   - Metadata: 上下文信息
//   - Done: 指示流完成的标志
type StreamChunk struct {
	// Content 是数据块内容（可以是部分输出）
	Content string `json:"content"`

	// Metadata 包含关于数据块的上下文信息
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Done 指示这是否是最后一个数据块
	Done bool `json:"done"`
}

// Plan 表示智能体的执行计划
//
// Plan 描述智能体为完成任务将采取的步骤：
//   - Steps: 有序的动作列表
//   - Metadata: 规划元数据（置信度、推理等）
//
// 计划由 Agent.Plan() 生成，用于：
//   - 透明性（显示智能体将要做什么）
//   - 验证（检查计划是否可接受）
//   - 优化（在执行前修改计划）
type Plan struct {
	// Steps 是要执行的有序动作列表
	Steps []Step `json:"steps"`

	// Metadata 包含规划元数据（置信度、推理等）
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Step 表示执行计划中的单个步骤
//
// 每个步骤描述：
//   - Action: 要做什么
//   - Input: 动作的参数
//   - ToolName: 要调用的工具（如果适用）
type Step struct {
	// Action 是要执行的动作（例如 "search"、"analyze"、"format"）
	Action string `json:"action"`

	// Input 包含动作的参数
	Input map[string]interface{} `json:"input"`

	// ToolName 是要调用的工具（如果这是工具调用步骤）
	ToolName string `json:"tool_name,omitempty"`
}

// State 表示可持久化的智能体状态
//
// State 是灵活的键值存储，用于：
//   - 智能体记忆
//   - 对话上下文
//   - 中间结果
//   - 配置数据
//
// 使用检查点功能时，State 在调用之间保持不变
type State map[string]interface{}
