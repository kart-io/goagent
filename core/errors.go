package core

import "errors"

// 错误定义
var (
	// Agent 相关错误
	ErrAgentNotFound        = errors.New("agent not found")
	ErrAgentAlreadyExists   = errors.New("agent already exists")
	ErrAgentExecutionFailed = errors.New("agent execution failed")
	ErrInvalidAgentInput    = errors.New("invalid agent input")
	ErrNotImplemented       = errors.New("method not implemented")

	// Chain 相关错误
	ErrChainNotFound       = errors.New("chain not found")
	ErrChainAlreadyExists  = errors.New("chain already exists")
	ErrChainProcessFailed  = errors.New("chain process failed")
	ErrInvalidChainInput   = errors.New("invalid chain input")
	ErrStepExecutionFailed = errors.New("step execution failed")

	// Orchestrator 相关错误
	ErrOrchestratorNotReady       = errors.New("orchestrator not ready")
	ErrInvalidOrchestratorRequest = errors.New("invalid orchestrator request")
	ErrExecutionFailed            = errors.New("execution failed")
	ErrExecutionTimeout           = errors.New("execution timeout")

	// Tool 相关错误
	ErrToolNotFound        = errors.New("tool not found")
	ErrToolAlreadyExists   = errors.New("tool already exists")
	ErrToolExecutionFailed = errors.New("tool execution failed")
	ErrInvalidToolInput    = errors.New("invalid tool input")

	// Memory 相关错误
	ErrMemoryStoreFailed    = errors.New("memory store failed")
	ErrMemoryRetrieveFailed = errors.New("memory retrieve failed")
	ErrMemoryNotFound       = errors.New("memory not found")

	// LLM 相关错误
	ErrLLMNotAvailable    = errors.New("LLM not available")
	ErrLLMRequestFailed   = errors.New("LLM request failed")
	ErrLLMResponseInvalid = errors.New("LLM response invalid")
)
