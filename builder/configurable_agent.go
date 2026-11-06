package builder

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/execution"
	"github.com/kart-io/goagent/core/middleware"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// ConfigurableAgent 是具有完整配置的已构建 Agent
type ConfigurableAgent[C any, S core.State] struct {
	llmClient       llm.Client
	tools           []interfaces.Tool
	systemPrompt    string
	runtime         *execution.Runtime[C, S]
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
}

// Initialize 准备 Agent 执行
func (a *ConfigurableAgent[C, S]) Initialize(ctx context.Context) (err error) {
	defer recoverPanic("Agent 初始化", &err)
	if ctx == nil {
		return fmt.Errorf("初始化 Agent 的 context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 如果存在则加载先前状态
	if a.runtime.Checkpointer != nil {
		exists, err := a.runtime.Checkpointer.Exists(ctx, a.config.SessionID)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return fmt.Errorf("checkpoint exists: %w", err)
		}
		if exists {
			if err := ctx.Err(); err != nil {
				return err
			}
			state, err := a.runtime.Checkpointer.Load(ctx, a.config.SessionID)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if err != nil {
				return fmt.Errorf("checkpoint load: %w", err)
			}
			loaded, ok := state.(S)
			if !ok || isNilState(loaded) {
				return fmt.Errorf("checkpoint state type mismatch: got %T", state)
			}
			a.runtime.State = loaded
		}
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

func (a *ConfigurableAgent[C, S]) execute(ctx context.Context, input interface{}, chain *middleware.MiddlewareChain) (*AgentOutput, error) {
	// 如果配置了超时则应用
	if a.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.config.Timeout)
		defer cancel()
	}

	// 创建请求
	request := &middleware.MiddlewareRequest{
		Input:     input,
		State:     a.runtime.State,
		Runtime:   a.runtime,
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
func (a *ConfigurableAgent[C, S]) handleError(ctx context.Context, err error) (handled error) {
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
func (a *ConfigurableAgent[C, S]) notifyEnd(ctx context.Context, output *AgentOutput) (err error) {
	defer recoverPanic("Agent 完成回调", &err)
	for _, cb := range a.callbacks {
		if err := cb.OnEnd(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

// buildSystemPromptContent 构建完整的系统提示内容（包含输出格式指示）
func (a *ConfigurableAgent[C, S]) buildSystemPromptContent() string {
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
func (a *ConfigurableAgent[C, S]) saveConversation(ctx context.Context, userInput, assistantResponse string) error {
	return writeMemory(ctx, a.memoryManager, a.config, userInput, assistantResponse)
}

// isNilState 拒绝接口内包装的空指针，避免替换成不可用状态。
func isNilState(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// invokeToolCall 保留工具状态，供直接返回路径校验成功标志。
func (a *ConfigurableAgent[C, S]) invokeToolCall(ctx context.Context, call ToolCall) (*interfaces.ToolOutput, error) {
	// 查找工具
	for _, tool := range a.tools {
		if tool.Name() == call.Name {
			// 创建工具输入
			toolInput := &interfaces.ToolInput{
				CallID:  call.ID,
				Args:    call.Input,
				Context: ctx,
			}

			// 执行工具
			output, err := tool.Invoke(ctx, toolInput)
			if err != nil {
				return nil, err
			}
			if output == nil {
				return nil, fmt.Errorf("tool %s returned nil output", call.Name)
			}
			if output.Error != "" {
				return nil, fmt.Errorf("tool %s: %s", call.Name, output.Error)
			}
			return output, nil
		}
	}
	return nil, agentErrors.NewToolNotFoundError(call.Name)
}

// GetState 返回当前状态
func (a *ConfigurableAgent[C, S]) GetState() S {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.runtime.State
}

// GetMetrics 返回 Agent 指标
func (a *ConfigurableAgent[C, S]) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})

	// 添加基本指标
	metrics["session_id"] = a.config.SessionID
	metrics["tools_count"] = len(a.tools)

	// 如果可用则添加状态大小
	if state, ok := any(a.runtime.State).(*core.AgentState); ok {
		metrics["state_size"] = state.Size()
	}

	return metrics
}

// Shutdown 优雅地关闭 Agent
func (a *ConfigurableAgent[C, S]) Shutdown(ctx context.Context) (err error) {
	defer recoverPanic("Agent 关闭", &err)
	// 保存最终状态
	if a.runtime.Checkpointer != nil {
		if err := a.runtime.SaveState(ctx); err != nil {
			return agentErrors.NewStateCheckpointError(a.config.SessionID, "save_final", err)
		}
	}

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
