package builder

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// ConfigurableAgent 是具有完整配置的已构建 Agent
type ConfigurableAgent[S core.State] struct {
	llmClient       llm.Client
	tools           []interfaces.Tool
	systemPrompt    string
	state           S
	chain           *middleware.MiddlewareChain
	config          *AgentConfig
	callbacks       []core.Callback
	errorHandler    func(error) error
	metadata        map[string]interface{}
	memoryManager   interfaces.MemoryManager // 对话记忆管理
	inputSerializer InputSerializer
	retriever       interfaces.DocumentRetriever
	retrievalConfig RetrievalConfig
	mu              sync.RWMutex
	executionGate   chan struct{}
	// initialState 是构建时状态的编码；stateSession/stateSeq 是当前状态对应的会话与 turn/end 序号。
	// 进行中的轮次 stateSeq 为 -1，stateBase/stateBaseSeq 是它开始前的状态与对应序号，用于撤回失败轮次的修改。
	// 这些字段只在 executionGate 内读写。
	initialState json.RawMessage
	stateSession string
	stateSeq     int64
	stateBase    json.RawMessage
	stateBaseSeq int64
}

// Initialize 准备 Agent 执行
func (a *ConfigurableAgent[S]) Initialize(ctx context.Context) (err error) {
	defer recoverPanic("Agent 初始化", &err)
	if ctx == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "初始化 Agent 的 context 不能为空").WithComponent("builder").WithOperation("initialize")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 通知回调
	for _, cb := range a.callbacks {
		if err := ctx.Err(); err != nil {
			return err
		}
		callbackErr := cb.OnStart(ctx, a.metadata)
		if err := ctx.Err(); err != nil {
			return err
		}
		if callbackErr != nil {
			return callbackErr
		}
	}

	return nil
}

func (a *ConfigurableAgent[S]) execute(ctx context.Context, input interface{}, chain *middleware.MiddlewareChain) (*AgentOutput, error) {
	// 如果配置了超时则应用
	if a.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.config.Timeout)
		defer cancel()
	}

	// 创建请求
	request := &middleware.MiddlewareRequest{
		Input:     input,
		State:     a.state,
		Metadata:  make(map[string]interface{}),
		Headers:   make(map[string]string),
		Timestamp: time.Now(),
	}

	// 添加元数据
	for k, v := range a.metadata {
		request.Metadata[k] = v
	}

	// 通过中间件链执行；链内（中间件、模型、工具）的 panic 转为 *agentErrors.PanicError，
	// 与普通错误一样经过 errorHandler、OnError 与观测。
	response, err := func() (response *middleware.MiddlewareResponse, err error) {
		defer recoverPanic("Agent 执行", &err)
		return chain.Execute(ctx, request)
	}()
	if err != nil {
		return nil, a.handleError(ctx, err)
	}

	// 创建输出
	output := &AgentOutput{
		Result:     response.Output,
		State:      response.State,
		Metadata:   response.Metadata,
		Duration:   response.Duration,
		Timestamp:  time.Now(),
		TokenUsage: response.TokenUsage,
	}

	if err := a.validateOutput(ctx, output); err != nil {
		return nil, a.outputValidationError(ctx, err)
	}
	if err := a.notifyEnd(ctx, output); err != nil {
		return nil, err
	}
	if err := a.validateOutput(ctx, output); err != nil {
		return nil, a.outputValidationError(ctx, err)
	}
	return output, nil
}

// handleError 让错误依次经过 errorHandler 与各回调的 OnError。
// errorHandler 或 OnError 自身 panic 时直接返回 *PanicError，不再调用剩余回调，
// 也不把这个 panic 再交给 errorHandler 或 OnError，避免回调处理自身 panic 形成递归。
func (a *ConfigurableAgent[S]) handleError(ctx context.Context, err error) (handled error) {
	defer recoverPanic("Agent 错误处理回调", &handled)
	if a.errorHandler != nil {
		err = a.errorHandler(err)
	}
	for _, cb := range a.callbacks {
		if cbErr := cb.OnError(ctx, err); cbErr != nil {
			// 记录回调错误但不覆盖原始错误
			fmt.Fprintf(os.Stderr, "Callback OnError failed: %v\n", cbErr)
		}
	}
	return err
}

// notifyEnd 通知各回调的 OnEnd。OnEnd 自身 panic 时直接返回 *PanicError，
// 不再调用剩余回调，也不交给 errorHandler 或 OnError（同 handleError，避免回调递归）。
func (a *ConfigurableAgent[S]) notifyEnd(ctx context.Context, output *AgentOutput) (err error) {
	defer recoverPanic("Agent 完成回调", &err)
	for _, cb := range a.callbacks {
		if err := cb.OnEnd(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

// buildSystemPromptContent 构建完整的系统提示内容（包含输出格式指示）
func (a *ConfigurableAgent[S]) buildSystemPromptContent() string {
	if a.systemPrompt == "" && a.config.OutputFormat == OutputFormatDefault {
		return ""
	}

	var sb strings.Builder
	if a.systemPrompt != "" {
		sb.WriteString(a.systemPrompt)
	}

	// 追加输出格式指示
	var formatPrompt string
	if a.config.OutputFormat == OutputFormatCustom {
		formatPrompt = a.config.CustomOutputPrompt
	} else {
		formatPrompt = GetOutputFormatPrompt(a.config.OutputFormat)
	}

	if formatPrompt != "" {
		if sb.Len() > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(formatPrompt)
	}

	return sb.String()
}

// saveConversation 保存对话到记忆
func (a *ConfigurableAgent[S]) saveConversation(ctx context.Context, userInput, assistantResponse string) error {
	return writeMemory(ctx, a.memoryManager, a.config, userInput, assistantResponse)
}

// invokeToolCall 保留工具状态，供直接返回路径校验成功标志。
func (a *ConfigurableAgent[S]) invokeToolCall(ctx context.Context, call ToolCall) (*interfaces.ToolOutput, error) {
	// 查找工具
	for _, tool := range a.tools {
		if tool.Name() == call.Name {
			// 创建工具输入
			toolInput := &interfaces.ToolInput{
				CallID: call.ID,
				Args:   call.Input,
			}

			// 执行工具
			output, err := tool.Invoke(ctx, toolInput)
			if err != nil {
				return nil, err
			}
			if output == nil {
				return nil, agentErrors.Newf(agentErrors.CodeInvalidOutput, "tool %s returned nil output", call.Name).WithComponent("builder").WithOperation("invoke_tool_call")
			}
			if output.Error != "" {
				return nil, agentErrors.Newf(agentErrors.CodeToolExecution, "tool %s: %s", call.Name, output.Error).WithComponent("builder").WithOperation("invoke_tool_call")
			}
			return output, nil
		}
	}
	return nil, agentErrors.NewToolNotFoundError(call.Name)
}

// GetState 返回当前状态
func (a *ConfigurableAgent[S]) GetState() S {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// GetMetrics 返回 Agent 指标
func (a *ConfigurableAgent[S]) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})

	// 添加基本指标
	metrics["session_id"] = a.config.SessionID
	metrics["tools_count"] = len(a.tools)

	// 如果可用则添加状态大小
	if state, ok := any(a.state).(*core.AgentState); ok {
		metrics["state_size"] = state.Size()
	}

	return metrics
}

// Shutdown 优雅地关闭 Agent
func (a *ConfigurableAgent[S]) Shutdown(ctx context.Context) (err error) {
	defer recoverPanic("Agent 关闭", &err)
	// 通知回调
	for _, cb := range a.callbacks {
		if shutdown, ok := cb.(interface{ OnShutdown(context.Context) error }); ok {
			if err := shutdown.OnShutdown(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

// ToolCall 表示工具调用请求
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]interface{}
}

// AgentOutput 表示 Agent 执行结果
type AgentOutput struct {
	Result     interface{}
	State      core.State
	Metadata   map[string]interface{}
	Duration   time.Duration
	Timestamp  time.Time
	TokenUsage *interfaces.TokenUsage
}
