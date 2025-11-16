package errors

import (
	"context"
	"fmt"
)

// Agent Execution Errors

// NewAgentExecutionError creates an error for agent execution failures
func NewAgentExecutionError(agentName, operation string, cause error) *AgentError {
	return Wrap(cause, CodeAgentExecution, fmt.Sprintf("agent execution failed")).
		WithComponent("agent").
		WithOperation(operation).
		WithContext("agent_name", agentName)
}

// NewAgentValidationError creates an error for agent input validation failures
func NewAgentValidationError(agentName, reason string) *AgentError {
	return New(CodeAgentValidation, fmt.Sprintf("agent validation failed: %s", reason)).
		WithComponent("agent").
		WithOperation("validation").
		WithContext("agent_name", agentName)
}

// NewAgentNotFoundError creates an error when an agent is not found
func NewAgentNotFoundError(agentName string) *AgentError {
	return New(CodeAgentNotFound, fmt.Sprintf("agent not found: %s", agentName)).
		WithComponent("agent").
		WithOperation("lookup").
		WithContext("agent_name", agentName)
}

// NewAgentInitializationError creates an error for agent initialization failures
func NewAgentInitializationError(agentName string, cause error) *AgentError {
	return Wrap(cause, CodeAgentInitialization, "agent initialization failed").
		WithComponent("agent").
		WithOperation("initialize").
		WithContext("agent_name", agentName)
}

// Tool Errors

// NewToolExecutionError creates an error for tool execution failures
func NewToolExecutionError(toolName, operation string, cause error) *AgentError {
	return Wrap(cause, CodeToolExecution, "tool execution failed").
		WithComponent("tool").
		WithOperation(operation).
		WithContext("tool_name", toolName)
}

// NewToolNotFoundError creates an error when a tool is not found
func NewToolNotFoundError(toolName string) *AgentError {
	return New(CodeToolNotFound, fmt.Sprintf("tool not found: %s", toolName)).
		WithComponent("tool").
		WithOperation("lookup").
		WithContext("tool_name", toolName)
}

// NewToolValidationError creates an error for tool input validation failures
func NewToolValidationError(toolName, reason string) *AgentError {
	return New(CodeToolValidation, fmt.Sprintf("tool validation failed: %s", reason)).
		WithComponent("tool").
		WithOperation("validation").
		WithContext("tool_name", toolName)
}

// NewToolTimeoutError creates an error when a tool execution times out
func NewToolTimeoutError(toolName string, timeoutSeconds int) *AgentError {
	return New(CodeToolTimeout, fmt.Sprintf("tool execution timed out after %d seconds", timeoutSeconds)).
		WithComponent("tool").
		WithOperation("execute").
		WithContext("tool_name", toolName).
		WithContext("timeout_seconds", timeoutSeconds)
}

// NewToolRetryExhaustedError creates an error when tool retry attempts are exhausted
func NewToolRetryExhaustedError(toolName string, attempts int, lastError error) *AgentError {
	return Wrap(lastError, CodeToolRetryExhausted, fmt.Sprintf("tool retry exhausted after %d attempts", attempts)).
		WithComponent("tool").
		WithOperation("execute_with_retry").
		WithContext("tool_name", toolName).
		WithContext("attempts", attempts)
}

// Middleware Errors

// NewMiddlewareExecutionError creates an error for middleware execution failures
func NewMiddlewareExecutionError(middlewareName, phase string, cause error) *AgentError {
	return Wrap(cause, CodeMiddlewareExecution, "middleware execution failed").
		WithComponent("middleware").
		WithOperation(phase).
		WithContext("middleware_name", middlewareName).
		WithContext("phase", phase)
}

// NewMiddlewareChainError creates an error for middleware chain failures
func NewMiddlewareChainError(position int, cause error) *AgentError {
	return Wrap(cause, CodeMiddlewareChain, "middleware chain execution failed").
		WithComponent("middleware_chain").
		WithOperation("execute").
		WithContext("position", position)
}

// NewMiddlewareValidationError creates an error for middleware validation failures
func NewMiddlewareValidationError(middlewareName, reason string) *AgentError {
	return New(CodeMiddlewareValidation, fmt.Sprintf("middleware validation failed: %s", reason)).
		WithComponent("middleware").
		WithOperation("validation").
		WithContext("middleware_name", middlewareName)
}

// State Management Errors

// NewStateLoadError creates an error for state loading failures
func NewStateLoadError(sessionID string, cause error) *AgentError {
	return Wrap(cause, CodeStateLoad, "failed to load state").
		WithComponent("state").
		WithOperation("load").
		WithContext("session_id", sessionID)
}

// NewStateSaveError creates an error for state saving failures
func NewStateSaveError(sessionID string, cause error) *AgentError {
	return Wrap(cause, CodeStateSave, "failed to save state").
		WithComponent("state").
		WithOperation("save").
		WithContext("session_id", sessionID)
}

// NewStateValidationError creates an error for state validation failures
func NewStateValidationError(reason string) *AgentError {
	return New(CodeStateValidation, fmt.Sprintf("state validation failed: %s", reason)).
		WithComponent("state").
		WithOperation("validation")
}

// NewStateCheckpointError creates an error for checkpoint operations
func NewStateCheckpointError(sessionID string, operation string, cause error) *AgentError {
	return Wrap(cause, CodeStateCheckpoint, fmt.Sprintf("checkpoint %s failed", operation)).
		WithComponent("checkpoint").
		WithOperation(operation).
		WithContext("session_id", sessionID)
}

// Stream Processing Errors

// NewStreamReadError creates an error for stream reading failures
func NewStreamReadError(cause error) *AgentError {
	return Wrap(cause, CodeStreamRead, "stream read failed").
		WithComponent("stream").
		WithOperation("read")
}

// NewStreamWriteError creates an error for stream writing failures
func NewStreamWriteError(cause error) *AgentError {
	return Wrap(cause, CodeStreamWrite, "stream write failed").
		WithComponent("stream").
		WithOperation("write")
}

// NewStreamTimeoutError creates an error when stream operations time out
func NewStreamTimeoutError(operation string, timeoutSeconds int) *AgentError {
	return New(CodeStreamTimeout, fmt.Sprintf("stream %s timed out after %d seconds", operation, timeoutSeconds)).
		WithComponent("stream").
		WithOperation(operation).
		WithContext("timeout_seconds", timeoutSeconds)
}

// NewStreamClosedError creates an error when operating on a closed stream
func NewStreamClosedError(operation string) *AgentError {
	return New(CodeStreamClosed, fmt.Sprintf("stream is closed, cannot %s", operation)).
		WithComponent("stream").
		WithOperation(operation)
}

// LLM Errors

// NewLLMRequestError creates an error for LLM request failures
func NewLLMRequestError(provider, model string, cause error) *AgentError {
	return Wrap(cause, CodeLLMRequest, "LLM request failed").
		WithComponent("llm").
		WithOperation("request").
		WithContext("provider", provider).
		WithContext("model", model)
}

// NewLLMResponseError creates an error for LLM response parsing failures
func NewLLMResponseError(provider, model, reason string) *AgentError {
	return New(CodeLLMResponse, fmt.Sprintf("LLM response error: %s", reason)).
		WithComponent("llm").
		WithOperation("parse_response").
		WithContext("provider", provider).
		WithContext("model", model)
}

// NewLLMTimeoutError creates an error when LLM request times out
func NewLLMTimeoutError(provider, model string, timeoutSeconds int) *AgentError {
	return New(CodeLLMTimeout, fmt.Sprintf("LLM request timed out after %d seconds", timeoutSeconds)).
		WithComponent("llm").
		WithOperation("request").
		WithContext("provider", provider).
		WithContext("model", model).
		WithContext("timeout_seconds", timeoutSeconds)
}

// NewLLMRateLimitError creates an error when LLM rate limit is hit
func NewLLMRateLimitError(provider, model string, retryAfterSeconds int) *AgentError {
	return New(CodeLLMRateLimit, "LLM rate limit exceeded").
		WithComponent("llm").
		WithOperation("request").
		WithContext("provider", provider).
		WithContext("model", model).
		WithContext("retry_after_seconds", retryAfterSeconds)
}

// Context Errors

// NewContextCanceledError creates an error when context is canceled
func NewContextCanceledError(operation string) *AgentError {
	return Wrap(context.Canceled, CodeContextCanceled, "operation canceled").
		WithOperation(operation)
}

// NewContextTimeoutError creates an error when context times out
func NewContextTimeoutError(operation string, timeoutSeconds int) *AgentError {
	return Wrap(context.DeadlineExceeded, CodeContextTimeout, fmt.Sprintf("operation timed out after %d seconds", timeoutSeconds)).
		WithOperation(operation).
		WithContext("timeout_seconds", timeoutSeconds)
}

// General Errors

// NewInvalidInputError creates an error for invalid input
func NewInvalidInputError(component, parameter, reason string) *AgentError {
	return New(CodeInvalidInput, fmt.Sprintf("invalid input: %s", reason)).
		WithComponent(component).
		WithOperation("validate_input").
		WithContext("parameter", parameter)
}

// NewInvalidConfigError creates an error for invalid configuration
func NewInvalidConfigError(component, configKey, reason string) *AgentError {
	return New(CodeInvalidConfig, fmt.Sprintf("invalid configuration: %s", reason)).
		WithComponent(component).
		WithOperation("validate_config").
		WithContext("config_key", configKey)
}

// NewNotImplementedError creates an error for unimplemented features
func NewNotImplementedError(component, feature string) *AgentError {
	return New(CodeNotImplemented, fmt.Sprintf("feature not implemented: %s", feature)).
		WithComponent(component).
		WithContext("feature", feature)
}

// NewInternalError creates an error for internal failures
func NewInternalError(component, operation string, cause error) *AgentError {
	return Wrap(cause, CodeInternal, "internal error occurred").
		WithComponent(component).
		WithOperation(operation)
}

// ErrorWithRetry wraps an error with retry information
func ErrorWithRetry(err error, attempt, maxAttempts int) *AgentError {
	if err == nil {
		return nil
	}

	if agentErr, ok := err.(*AgentError); ok {
		return agentErr.
			WithContext("retry_attempt", attempt).
			WithContext("max_attempts", maxAttempts)
	}

	return Wrap(err, CodeInternal, "operation failed").
		WithContext("retry_attempt", attempt).
		WithContext("max_attempts", maxAttempts)
}

// ErrorWithDuration wraps an error with duration information
func ErrorWithDuration(err error, durationMs int64) *AgentError {
	if err == nil {
		return nil
	}

	if agentErr, ok := err.(*AgentError); ok {
		return agentErr.WithContext("duration_ms", durationMs)
	}

	return Wrap(err, CodeInternal, "operation failed").
		WithContext("duration_ms", durationMs)
}
