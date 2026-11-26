package builder

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// InputSerializer defines how input is serialized.
type InputSerializer interface {
	Serialize(input interface{}) (string, error)
}

// DefaultInputSerializer is the default input serializer (uses fmt.Sprintf).
type DefaultInputSerializer struct{}

func (s *DefaultInputSerializer) Serialize(input interface{}) (string, error) {
	return fmt.Sprintf("%v", input), nil
}

// AgentBuilder provides a fluent API for building agents.
//
// Inspired by LangChain's create_agent function, it integrates:
//   - LLM client configuration
//   - Tool registration
//   - State management
//   - Runtime context
//   - Middleware stack
//   - System prompt
//   - Conversation memory management
//   - Input serialization
type AgentBuilder[S core.State] struct {
	// 核心组件
	llmClient     llm.Client
	dispatcher    *tools.Dispatcher
	dispatcherSet bool
	tools         []interfaces.Tool
	systemPrompt  string

	state S

	// Phase 2 组件
	middlewares []middleware.Middleware

	// 对话记忆管理（支持多轮对话）
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

// NewAgentBuilder creates a new agent builder.
func NewAgentBuilder[S core.State](llmClient llm.Client) *AgentBuilder[S] {
	return &AgentBuilder[S]{
		llmClient:       llmClient,
		tools:           []interfaces.Tool{},
		middlewares:     []middleware.Middleware{},
		callbacks:       []core.Callback{},
		config:          DefaultAgentConfig(),
		inputSerializer: &DefaultInputSerializer{},
		metadata:        make(map[string]interface{}),
	}
}

// Build builds the final agent.
func (b *AgentBuilder[S]) Build() (*ConfigurableAgent[S], error) {
	return b.BuildContext(context.Background())
}

// BuildContext builds and initializes the agent using the host context.
// Cancellation prevents subsequent initialization steps; host calls that ignore
// the context must still return on their own.
func (b *AgentBuilder[S]) BuildContext(ctx context.Context) (*ConfigurableAgent[S], error) {
	if ctx == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "构建 Agent 的 context 不能为空").WithComponent("builder").WithOperation("build_context")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("构建 Agent 已取消：%w", err)
	}
	toolset := b.tools
	if b.dispatcherSet {
		if b.dispatcher == nil {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "WithDispatcher需要非空派发器").WithComponent("builder").WithOperation("build_context")
		}
		if len(toolset) > 0 {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "WithDispatcher不能与WithTools混用").WithComponent("builder").WithOperation("build_context")
		}
		toolset = b.dispatcher.Tools()
	}
	for _, validate := range b.config.OutputValidators {
		if validate == nil {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "最终输出验收器不能为nil").WithComponent("builder").WithOperation("build_context")
		}
	}
	// 验证必需组件
	if b.llmClient == nil {
		return nil, agentErrors.NewInvalidConfigError("builder", "llm_client", "LLM client is required")
	}
	if _, ok := b.llmClient.(llm.ToolStreamClient); !ok && len(toolset) > 0 {
		return nil, agentErrors.NewInvalidConfigError("builder", "llm_client", "configured tools require llm.ToolStreamClient")
	}
	if err := b.validateLoopCapabilities(toolset); err != nil {
		return nil, err
	}
	if b.inputSerializer == nil {
		return nil, agentErrors.NewInvalidConfigError("builder", "input_serializer", "input serializer must not be nil")
	}
	if b.retriever != nil && isNilRetriever(b.retriever) {
		return nil, agentErrors.NewInvalidConfigError("builder", "retriever", "retriever must not be a typed nil")
	}
	if b.retrievalConfig.Filter != nil {
		if _, ok := b.retriever.(interfaces.RequestRetriever); !ok {
			return nil, agentErrors.NewInvalidConfigError("builder", "retrieval", "retrieval filter requires an interfaces.RequestRetriever")
		}
	}
	if err := b.config.validateLimits(); err != nil {
		return nil, err
	}
	if b.retrievalConfig.MaxDocuments < 0 || b.retrievalConfig.MaxContextRunes < 0 {
		return nil, agentErrors.NewInvalidConfigError("builder", "retrieval", "retrieval limits must not be negative")
	}
	if len(b.config.ReturnDirectTools) > 0 {
		if _, ok := b.llmClient.(llm.ToolStreamClient); !ok {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "ReturnDirectTools 需要 llm.ToolStreamClient").WithComponent("builder").WithOperation("build_context")
		}
		for _, name := range b.config.ReturnDirectTools {
			matches := 0
			for _, tool := range toolset {
				if tool != nil && tool.Name() == name {
					matches++
				}
			}
			if strings.TrimSpace(name) == "" || matches != 1 {
				return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "直接返回工具 %q 必须唯一注册", name).WithComponent("builder").WithOperation("build_context")
			}
		}
	}

	if len(b.config.Offloads) > 0 {
		if _, ok := b.llmClient.(llm.ToolStreamClient); !ok || len(toolset) == 0 {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "WithOffload requires tools and llm.ToolStreamClient").WithComponent("builder").WithOperation("build_context")
		}
		if err := b.validateOffloads(toolset); err != nil {
			return nil, err
		}
	}
	if err := b.validateLongTermMemory(); err != nil {
		return nil, err
	}
	if err := b.validateModelCache(); err != nil {
		return nil, err
	}
	if err := b.validateImages(); err != nil {
		return nil, err
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

	var initialState json.RawMessage
	if b.config.SessionState {
		var err error
		if initialState, err = encodeSessionState(b.config.SessionLog, b.state); err != nil {
			return nil, err
		}
	}

	// 每轮执行通过 WithHandler 替换处理器，这里只承载中间件。
	chain := middleware.NewMiddlewareChain(nil)

	// 如果 verbose,添加默认中间件
	if b.config.Verbose {
		chain.Use(middleware.NewLoggingMiddleware(verboseLog(b.config.Logger)))
		chain.Use(middleware.NewTimingMiddleware())
	}

	// 添加用户指定的中间件
	chain.Use(b.middlewares...)

	// 创建 Agent
	agent := &ConfigurableAgent[S]{
		executionGate:   make(chan struct{}, 1),
		initialState:    initialState,
		llmClient:       b.llmClient,
		tools:           slices.Clone(toolset),
		systemPrompt:    b.systemPrompt,
		state:           b.state,
		chain:           chain,
		config:          snapshotAgentConfig(b.config),
		callbacks:       slices.Clone(b.callbacks),
		errorHandler:    b.errorHandler,
		metadata:        maps.Clone(b.metadata),
		inputSerializer: b.inputSerializer,
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

// WithInputSerializer sets the input serializer.
func (b *AgentBuilder[S]) WithInputSerializer(serializer InputSerializer) *AgentBuilder[S] {
	b.inputSerializer = serializer
	return b
}

// ==============================
// 简化 API（推荐使用）
// ==============================

// SimpleAgentBuilder is a simplified type alias for AgentBuilder.
//
// It uses the most common combination of type parameters:
//   - Context: any (generic context)
//   - State: *core.AgentState (standard state implementation)
//
// This alias removes generic complexity in more than 95% of use cases. To
// customize Context or State, use the original NewAgentBuilder[S] function.
//
// Example:
//
//	builder := builder.NewSimpleBuilder(llmClient)
//	agent, err := builder.
//	WithSystemPrompt("You are an assistant").
//	Build()
type SimpleAgentBuilder = AgentBuilder[*core.AgentState]

// SimpleAgent is a simplified type alias for ConfigurableAgent.
//
// It uses the most common combination of type parameters and matches
// SimpleAgentBuilder.
type SimpleAgent = ConfigurableAgent[*core.AgentState]

// NewSimpleBuilder creates a simplified agent builder.
//
// This is the recommended way to create a builder and suits most use cases.
// Compared with NewAgentBuilder[*core.AgentState](client), it does not require
// explicit type parameters and is more concise.
//
// Parameters:
//   - llmClient: the LLM client instance
//
// Returns:
//   - *SimpleAgentBuilder: the simplified builder instance
//
// Example:
//
//	client := providers.NewOpenAIClient(apiKey)
//	builder := builder.NewSimpleBuilder(client)
//	agent, err := builder.
//	WithSystemPrompt("You are an assistant").
//	WithTools(calculatorTool, searchTool).
//	Build()
func NewSimpleBuilder(llmClient llm.Client) *SimpleAgentBuilder {
	return NewAgentBuilder[*core.AgentState](llmClient)
}
