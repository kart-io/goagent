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

// InputSerializer 定义输入序列化接口
type InputSerializer interface {
	Serialize(input interface{}) (string, error)
}

// DefaultInputSerializer 默认输入序列化器（使用 fmt.Sprintf）
type DefaultInputSerializer struct{}

func (s *DefaultInputSerializer) Serialize(input interface{}) (string, error) {
	return fmt.Sprintf("%v", input), nil
}

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
//   - 输入序列化
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
	memoryManager   interfaces.MemoryManager
	retriever       interfaces.DocumentRetriever
	retrievalConfig RetrievalConfig

	// 配置
	config *AgentConfig

	// 回调
	callbacks []core.Callback

	// 错误处理
	errorHandler func(error) error

	// 输入序列化
	inputSerializer InputSerializer

	// 元数据
	metadata map[string]interface{}
}

// NewAgentBuilder 创建一个新的 Agent 构建器
func NewAgentBuilder[C any, S core.State](llmClient llm.Client) *AgentBuilder[C, S] {
	return &AgentBuilder[C, S]{
		llmClient:       llmClient,
		tools:           []interfaces.Tool{},
		middlewares:     []middleware.Middleware{},
		callbacks:       []core.Callback{},
		config:          DefaultAgentConfig(),
		inputSerializer: &DefaultInputSerializer{},
		metadata:        make(map[string]interface{}),
	}
}

// Build 构建最终的 Agent
func (b *AgentBuilder[C, S]) Build() (*ConfigurableAgent[C, S], error) {
	return b.BuildContext(context.Background())
}

// BuildContext 使用宿主上下文构建并初始化 Agent。
// 取消会阻止后续初始化步骤；忽略上下文的宿主调用仍需自行返回。
func (b *AgentBuilder[C, S]) BuildContext(ctx context.Context) (*ConfigurableAgent[C, S], error) {
	if ctx == nil {
		return nil, fmt.Errorf("构建 Agent 的 context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("构建 Agent 已取消：%w", err)
	}
	// 验证必需组件
	if b.llmClient == nil {
		return nil, agentErrors.NewInvalidConfigError("builder", "llm_client", "LLM client is required")
	}
	if b.config.MemoryFailurePolicy != MemoryFailureBestEffort && b.config.MemoryFailurePolicy != MemoryFailureStrict {
		return nil, agentErrors.NewInvalidConfigError("builder", "memory_failure_policy", "unknown memory failure policy")
	}
	if b.retriever != nil && isNilRetriever(b.retriever) {
		return nil, agentErrors.NewInvalidConfigError("builder", "retriever", "retriever must not be a typed nil")
	}
	if b.retrievalConfig.MaxDocuments < 0 || b.retrievalConfig.MaxContextRunes < 0 {
		return nil, agentErrors.NewInvalidConfigError("builder", "retrieval", "retrieval limits must not be negative")
	}
	if len(b.config.ReturnDirectTools) > 0 {
		if _, ok := b.llmClient.(llm.ToolStreamClient); !ok {
			return nil, fmt.Errorf("ReturnDirectTools 需要 llm.ToolStreamClient")
		}
		for _, name := range b.config.ReturnDirectTools {
			matches := 0
			for _, tool := range b.tools {
				if tool != nil && tool.Name() == name {
					matches++
				}
			}
			if strings.TrimSpace(name) == "" || matches != 1 {
				return nil, fmt.Errorf("直接返回工具 %q 必须唯一注册", name)
			}
		}
	}

	if len(b.config.Offloads) > 0 {
		if _, ok := b.llmClient.(llm.ToolStreamClient); !ok || len(b.tools) == 0 {
			return nil, fmt.Errorf("WithOffload requires tools and llm.ToolStreamClient")
		}
		if err := b.validateOffloads(); err != nil {
			return nil, err
		}
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
		executionGate:   make(chan struct{}, 1),
		llmClient:       b.llmClient,
		tools:           b.tools,
		systemPrompt:    b.systemPrompt,
		runtime:         runtime,
		chain:           chain,
		config:          b.config,
		callbacks:       b.callbacks,
		errorHandler:    b.errorHandler,
		metadata:        b.metadata,
		memoryManager:   b.memoryManager,
		retriever:       b.retriever,
		retrievalConfig: b.retrievalConfig,
	}

	// 如果需要则初始化
	if err := agent.Initialize(ctx); err != nil {
		return nil, agentErrors.NewAgentInitializationError("configurable_agent", err)
	}

	if err := ctx.Err(); err != nil {
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

// WithInputSerializer 设置输入序列化器
func (b *AgentBuilder[C, S]) WithInputSerializer(serializer InputSerializer) *AgentBuilder[C, S] {
	b.inputSerializer = serializer
	return b
}

// createHandler 创建主执行处理器
func (b *AgentBuilder[C, S]) createHandler(runtime *execution.Runtime[C, S]) middleware.Handler {
	return func(ctx context.Context, request *middleware.MiddlewareRequest) (*middleware.MiddlewareResponse, error) {
		// 提取输入
		inputStr, err := b.inputSerializer.Serialize(request.Input)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "failed to serialize input")
		}

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

		history, err := readMemory(ctx, b.memoryManager, b.config)
		if err != nil {
			return nil, err
		}
		for _, conv := range history {
			messages = append(messages, llm.Message{Role: conv.Role, Content: conv.Content})
		}

		if b.retriever != nil {
			evidence, metadata, err := retrieveContext(ctx, b.retriever, b.retrievalConfig, inputStr)
			if err != nil {
				return nil, err
			}
			messages = append(messages, llm.SystemMessage(retrievalInstruction), llm.UserMessage(evidence))
			if request.Metadata == nil {
				request.Metadata = make(map[string]interface{})
			}
			for key, value := range metadata {
				request.Metadata[key] = value
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

		if err := writeMemory(ctx, b.memoryManager, b.config, inputStr, response.Content); err != nil {
			return nil, err
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
