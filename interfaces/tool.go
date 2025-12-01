package interfaces

import "context"

// Tool 表示智能体可以调用的可执行工具
//
// 所有工具实现都应实现此接口
//
// 实现位置：
//   - tools.BaseTool - 基础实现，包含通用功能
//   - tools/compute/* - 计算工具
//   - tools/http/* - HTTP 请求工具
//   - tools/search/* - 搜索工具
//   - tools/shell/* - Shell 执行工具
//   - tools/practical/* - 实用工具
//
// 参见：tools/tool.go 的基础实现
type Tool interface {
	// Name 返回工具标识符
	//
	// 名称应在工具注册表中唯一，并遵循命名约定
	// （小写字母，下划线分隔）
	Name() string

	// Description 返回工具的功能描述
	//
	// 此描述供 LLM 理解何时以及如何使用工具
	// 应当清晰简洁
	Description() string

	// Invoke 使用给定输入执行工具
	//
	// 工具应处理输入参数并返回结果
	// 如果执行失败则返回错误
	Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)

	// ArgsSchema 返回工具的输入模式（JSON Schema 格式）
	//
	// 此模式定义工具接受的参数结构
	// LLM 使用它生成有效的工具调用
	//
	// 示例模式：
	//   {
	//     "type": "object",
	//     "properties": {
	//       "query": {"type": "string", "description": "搜索查询"}
	//     },
	//     "required": ["query"]
	//   }
	ArgsSchema() string
}

// ValidatableTool 是工具可以实现的可选接口
// 用于提供自定义输入验证逻辑
//
// 如果工具实现了此接口，验证器将在执行工具之前调用 Validate
// 允许进行比单独使用 JSON schema 更复杂的验证
//
// 示例实现：
//
//	func (t *MyTool) Validate(ctx context.Context, input *ToolInput) error {
//	    // 自定义验证逻辑
//	    if val, ok := input.Args["amount"].(float64); ok && val < 0 {
//	        return fmt.Errorf("amount must be non-negative")
//	    }
//	    return nil
//	}
type ValidatableTool interface {
	Tool

	// Validate 在执行前验证工具输入
	//
	// 如果输入无效则返回错误，错误消息应清楚描述输入的问题
	Validate(ctx context.Context, input *ToolInput) error
}

// ToolExecutor 表示可以执行工具的组件
//
// 此接口由需要运行工具的组件实现
// 例如智能体和工作流引擎
//
// 实现位置：
//   - agents/executor/executor_agent.go - 执行器智能体实现
//   - tools/registry.go - 具有执行能力的工具注册表
type ToolExecutor interface {
	// ExecuteTool 按名称使用给定参数执行工具
	//
	// 执行器负责：
	//   - 按名称查找工具
	//   - 验证参数是否符合工具模式
	//   - 执行工具
	//   - 处理错误和超时
	ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (*ToolResult, error)

	// ListTools 返回所有可用工具
	//
	// 这对于需要发现可用工具的智能体很有用
	ListTools() []Tool
}

// ToolInput 表示工具执行输入
//
// 此结构包含执行工具所需的所有信息
// 包括参数和用于追踪和调试的元数据
type ToolInput struct {
	// Args 包含工具的输入参数
	//
	// Args 的结构应与工具的 ArgsSchema 匹配
	Args map[string]interface{} `json:"args"`

	// Context 是执行上下文（不序列化）
	//
	// 用于取消、超时和传递请求范围的值
	Context context.Context `json:"-"`

	// CallerID 标识调用工具的身份
	//
	// 可选，用于授权和审计
	CallerID string `json:"caller_id,omitempty"`

	// TraceID 用于分布式追踪
	//
	// 可选，帮助跨系统跟踪工具执行
	TraceID string `json:"trace_id,omitempty"`
}

// ToolOutput 表示工具执行输出
//
// 此结构包含工具执行的结果
// 以及状态信息和元数据
type ToolOutput struct {
	// Result 包含工具的输出数据
	//
	// 类型取决于具体工具，常见类型：
	//   - string: 文本输出
	//   - map[string]interface{}: 结构化数据
	//   - []byte: 二进制数据
	Result interface{} `json:"result"`

	// Success 指示工具是否成功执行
	//
	// 如果工具无错误完成则为 true，否则为 false
	Success bool `json:"success"`

	// Error 包含错误消息（当 Success 为 false 时）
	//
	// 当 Success 为 true 时为空字符串
	Error string `json:"error,omitempty"`

	// Metadata 包含关于执行的额外信息
	//
	// 可选，可能包括：
	//   - execution_time: 工具运行时长
	//   - retries: 重试次数
	//   - cost: API 调用成本（对于外部服务）
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ToolResult 表示 ToolExecutor 执行工具的结果
//
// 与 ToolOutput 不同，它包含执行器跟踪的额外信息
// 例如执行的工具名称
type ToolResult struct {
	// ToolName 是被执行工具的名称
	ToolName string `json:"tool_name"`

	// Output 包含工具的执行输出
	Output *ToolOutput `json:"output"`

	// ExecutionTime 是工具执行时长（毫秒）
	//
	// 可选，用于性能监控
	ExecutionTime int64 `json:"execution_time,omitempty"`
}

// ToolCall 表示工具调用的记录
//
// 用于日志记录、审计和调试
// 它捕获工具调用的完整上下文
type ToolCall struct {
	// ID 是此工具调用的唯一标识符
	//
	// 用于在日志和追踪中关联调用
	ID string `json:"id"`

	// ToolName 是被调用工具的名称
	ToolName string `json:"tool_name"`

	// Args 是传递给工具的参数
	Args map[string]interface{} `json:"args"`

	// Result 是工具的输出
	//
	// 如果调用仍在进行中可能为 nil
	Result *ToolOutput `json:"result,omitempty"`

	// Error 包含调用失败时的错误信息
	//
	// 调用成功时为空字符串
	Error string `json:"error,omitempty"`

	// StartTime 是工具调用开始的时间（Unix 时间戳）
	StartTime int64 `json:"start_time"`

	// EndTime 是工具调用完成的时间（Unix 时间戳）
	//
	// 如果调用仍在进行中则为零
	EndTime int64 `json:"end_time,omitempty"`

	// Metadata 包含关于调用的额外上下文
	//
	// 可能包括调用者信息、追踪 ID 等
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
