package builder

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/execution"
	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/memory"
)

// AgentBuilder 提供用于构建 Agent 的 fluent API
//
// 受 LangChain 的 create_agent 函数启发,它集成了:
//   - LLM 客户端配置
//   - 工具注册
//   - 状态管理
//   - 运行时上下文
//   - Store 和 Checkpointer
//   - 中间件栈
//   - 系统提示词
//   - 对话记忆管理
type AgentBuilder[C any, S core.State] struct {
	// 核心组件
	llmClient    llm.Client
	tools        []interfaces.Tool
	systemPrompt string

	// Phase 1 组件
	state        S
	store        store.Store
	checkpointer checkpoint.Checkpointer
	context      C

	// Phase 2 组件
	middlewares []middleware.Middleware

	// 对话记忆管理（支持多轮对话）
	memoryManager interfaces.MemoryManager

	// 配置
	config *AgentConfig

	// 回调
	callbacks []core.Callback

	// 错误处理
	errorHandler func(error) error

	// 元数据
	metadata map[string]interface{}
}

// NewAgentBuilder 创建一个新的 Agent 构建器
func NewAgentBuilder[C any, S core.State](llmClient llm.Client) *AgentBuilder[C, S] {
	return &AgentBuilder[C, S]{
		llmClient:   llmClient,
		tools:       []interfaces.Tool{},
		middlewares: []middleware.Middleware{},
		callbacks:   []core.Callback{},
		config:      DefaultAgentConfig(),
		metadata:    make(map[string]interface{}),
	}
}

// Build 构建最终的 Agent
func (b *AgentBuilder[C, S]) Build() (*ConfigurableAgent[C, S], error) {
	// 验证必需组件
	if b.llmClient == nil {
		return nil, agentErrors.NewInvalidConfigError("builder", "llm_client", "LLM client is required")
	}

	// 如果未提供则设置默认值
	var zero S
	if reflect.DeepEqual(b.state, zero) {
		// 尝试创建默认状态,如果 S 是 *core.AgentState
		if _, ok := any(zero).(*core.AgentState); ok {
			b.state = any(core.NewAgentState()).(S)
		} else {
			return nil, agentErrors.NewInvalidConfigError("builder", "state", "state is required")
		}
	}

	if b.store == nil {
		b.store = memory.New()
	}

	if b.checkpointer == nil {
		b.checkpointer = checkpoint.NewInMemorySaver()
	}

	// 创建运行时
	runtime := execution.NewRuntime(
		b.context,
		b.state,
		b.store,
		b.checkpointer,
		b.config.SessionID,
	)

	// 构建中间件链
	handler := b.createHandler(runtime)
	chain := middleware.NewMiddlewareChain(handler)

	// 如果 verbose,添加默认中间件
	if b.config.Verbose {
		chain.Use(middleware.NewLoggingMiddleware(nil))
		chain.Use(middleware.NewTimingMiddleware())
	}

	// 添加用户指定的中间件
	chain.Use(b.middlewares...)

	// 创建 Agent
	agent := &ConfigurableAgent[C, S]{
		llmClient:     b.llmClient,
		tools:         b.tools,
		systemPrompt:  b.systemPrompt,
		runtime:       runtime,
		chain:         chain,
		config:        b.config,
		callbacks:     b.callbacks,
		errorHandler:  b.errorHandler,
		metadata:      b.metadata,
		memoryManager: b.memoryManager,
	}

	// 如果需要则初始化
	if err := agent.Initialize(context.Background()); err != nil {
		return nil, agentErrors.NewAgentInitializationError("configurable_agent", err)
	}

	return agent, nil
}

// buildSystemPrompt 构建完整的系统提示内容（包含输出格式指示）
func (b *AgentBuilder[C, S]) buildSystemPrompt() string {
	if b.systemPrompt == "" && b.config.OutputFormat == OutputFormatDefault {
		return ""
	}

	var sb strings.Builder
	if b.systemPrompt != "" {
		sb.WriteString(b.systemPrompt)
	}

	// 追加输出格式指示
	var formatPrompt string
	if b.config.OutputFormat == OutputFormatCustom {
		formatPrompt = b.config.CustomOutputPrompt
	} else {
		formatPrompt = GetOutputFormatPrompt(b.config.OutputFormat)
	}

	if formatPrompt != "" {
		if sb.Len() > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(formatPrompt)
	}

	return sb.String()
}

// createHandler 创建主执行处理器
func (b *AgentBuilder[C, S]) createHandler(runtime *execution.Runtime[C, S]) middleware.Handler {
	return func(ctx context.Context, request *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
		// 提取输入
		inputStr := fmt.Sprintf("%v", request.Input)

		// 构建消息列表
		messages := make([]llm.Message, 0)

		// 构建系统提示（包含输出格式指示）
		systemContent := b.buildSystemPrompt()
		if systemContent != "" {
			messages = append(messages, llm.Message{
				Role:    "system",
				Content: systemContent,
			})
		}

		// 如果配置了 MemoryManager，加载历史对话
		if b.memoryManager != nil && b.config.SessionID != "" {
			history, err := b.memoryManager.GetConversationHistory(ctx, b.config.SessionID, b.config.MaxConversationHistory)
			if err == nil && len(history) > 0 {
				for _, conv := range history {
					messages = append(messages, llm.Message{
						Role:    conv.Role,
						Content: conv.Content,
					})
				}
			}
		}

		// 添加当前用户输入
		messages = append(messages, llm.Message{
			Role:    "user",
			Content: inputStr,
		})

		// 创建 LLM 请求
		llmReq := &llm.CompletionRequest{
			Messages:    messages,
			MaxTokens:   b.config.MaxTokens,
			Temperature: b.config.Temperature,
		}

		// 调用 LLM
		response, err := b.llmClient.Complete(ctx, llmReq)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeLLMRequest, "LLM completion error")
		}

		// 如果配置了 MemoryManager，保存对话到记忆
		if b.memoryManager != nil && b.config.SessionID != "" {
			// 保存用户输入
			if err := b.memoryManager.AddConversation(ctx, &interfaces.Conversation{
				SessionID: b.config.SessionID,
				Role:      "user",
				Content:   inputStr,
			}); err != nil {
				// 记录错误但不中断请求，对话记忆不是关键路径
				fmt.Fprintf(os.Stderr, "Failed to save user conversation: %v\n", err)
			}
			// 保存 AI 响应
			if err := b.memoryManager.AddConversation(ctx, &interfaces.Conversation{
				SessionID: b.config.SessionID,
				Role:      "assistant",
				Content:   response.Content,
			}); err != nil {
				// 记录错误但不中断请求
				fmt.Fprintf(os.Stderr, "Failed to save assistant conversation: %v\n", err)
			}
		}

		// 触发 OnLLMEnd 回调
		if len(b.callbacks) > 0 {
			for _, cb := range b.callbacks {
				if err := cb.OnLLMEnd(ctx, response.Content, response.TokensUsed); err != nil {
					// 记录错误但不失败请求
					fmt.Fprintf(os.Stderr, "Callback OnLLMEnd error: %v\n", err)
				}
			}
		}

		// 如果需要则更新状态
		if request.State != nil {
			request.State.Set("last_response", response.Content)
			request.State.Set("last_timestamp", time.Now())
		}

		// 如果启用自动保存则保存检查点
		if b.config.EnableAutoSave && runtime.Checkpointer != nil {
			if err := runtime.SaveState(ctx); err != nil {
				// 记录错误但不失败请求
				// 状态保存很重要但对响应不是关键的
				fmt.Fprintf(os.Stderr, "Failed to auto-save state: %v\n", err)
			}
		}

		// 创建响应
		return &middleware.MiddlewareResponse{
			Output:     response.Content,
			State:      request.State,
			Metadata:   request.Metadata,
			TokenUsage: response.Usage,
		}, nil
	}
}

// ==============================
// 简化 API（推荐使用）
// ==============================

// SimpleAgentBuilder 是 AgentBuilder 的简化版本类型别名
//
// 使用最常见的类型参数组合：
//   - Context: any (通用上下文)
//   - State: *core.AgentState (标准状态实现)
//
// 这个类型别名消除了 95% 以上使用场景中的泛型复杂度。
// 如果需要自定义 Context 或 State，请使用原始的 NewAgentBuilder[C, S] 函数。
//
// 使用示例：
//
//	builder := builder.NewSimpleBuilder(llmClient)
//	agent, err := builder.
//	WithSystemPrompt("你是一个助手").
//	Build()
type SimpleAgentBuilder = AgentBuilder[any, *core.AgentState]

// SimpleAgent 是 ConfigurableAgent 的简化版本类型别名
//
// 使用最常见的类型参数组合，与 SimpleAgentBuilder 匹配。
type SimpleAgent = ConfigurableAgent[any, *core.AgentState]

// NewSimpleBuilder 创建一个简化的 Agent 构建器
//
// 这是推荐的构建器创建方式，适用于大多数使用场景。
// 相比 NewAgentBuilder[any, *core.AgentState](client)，
// 此函数无需显式指定泛型参数，使用更简洁。
//
// 参数：
//   - llmClient: LLM 客户端实例
//
// 返回：
//   - *SimpleAgentBuilder: 简化的构建器实例
//
// 使用示例：
//
//	client := providers.NewOpenAIClient(apiKey)
//	builder := builder.NewSimpleBuilder(client)
//	agent, err := builder.
//	WithSystemPrompt("你是一个助手").
//	WithTools(calculatorTool, searchTool).
//	Build()
func NewSimpleBuilder(llmClient llm.Client) *SimpleAgentBuilder {
	return NewAgentBuilder[any, *core.AgentState](llmClient)
}
