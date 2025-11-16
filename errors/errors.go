package errors

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// ErrorCode defines different error categories for agent operations
type ErrorCode string

const (
	// Agent execution errors
	CodeAgentExecution      ErrorCode = "AGENT_EXECUTION"
	CodeAgentValidation     ErrorCode = "AGENT_VALIDATION"
	CodeAgentNotFound       ErrorCode = "AGENT_NOT_FOUND"
	CodeAgentInitialization ErrorCode = "AGENT_INITIALIZATION"

	// Tool errors
	CodeToolExecution      ErrorCode = "TOOL_EXECUTION"
	CodeToolNotFound       ErrorCode = "TOOL_NOT_FOUND"
	CodeToolValidation     ErrorCode = "TOOL_VALIDATION"
	CodeToolTimeout        ErrorCode = "TOOL_TIMEOUT"
	CodeToolRetryExhausted ErrorCode = "TOOL_RETRY_EXHAUSTED"

	// Middleware errors
	CodeMiddlewareExecution  ErrorCode = "MIDDLEWARE_EXECUTION"
	CodeMiddlewareChain      ErrorCode = "MIDDLEWARE_CHAIN"
	CodeMiddlewareValidation ErrorCode = "MIDDLEWARE_VALIDATION"

	// State management errors
	CodeStateLoad       ErrorCode = "STATE_LOAD"
	CodeStateSave       ErrorCode = "STATE_SAVE"
	CodeStateValidation ErrorCode = "STATE_VALIDATION"
	CodeStateCheckpoint ErrorCode = "STATE_CHECKPOINT"

	// Stream processing errors
	CodeStreamRead    ErrorCode = "STREAM_READ"
	CodeStreamWrite   ErrorCode = "STREAM_WRITE"
	CodeStreamTimeout ErrorCode = "STREAM_TIMEOUT"
	CodeStreamClosed  ErrorCode = "STREAM_CLOSED"

	// LLM errors
	CodeLLMRequest   ErrorCode = "LLM_REQUEST"
	CodeLLMResponse  ErrorCode = "LLM_RESPONSE"
	CodeLLMTimeout   ErrorCode = "LLM_TIMEOUT"
	CodeLLMRateLimit ErrorCode = "LLM_RATE_LIMIT"

	// Context errors
	CodeContextCanceled ErrorCode = "CONTEXT_CANCELED"
	CodeContextTimeout  ErrorCode = "CONTEXT_TIMEOUT"

	// General errors
	CodeInvalidInput   ErrorCode = "INVALID_INPUT"
	CodeInvalidConfig  ErrorCode = "INVALID_CONFIG"
	CodeNotImplemented ErrorCode = "NOT_IMPLEMENTED"
	CodeInternal       ErrorCode = "INTERNAL_ERROR"
)

// AgentError is the structured error type for all agent operations
//
// It provides:
// - Error codes for categorization
// - Context preservation through the error chain
// - Stack trace information for debugging
// - Structured metadata for logging and monitoring
// - Operation-specific context
type AgentError struct {
	// Code categorizes the error type
	Code ErrorCode

	// Message is the human-readable error message
	Message string

	// Operation identifies what was being attempted
	Operation string

	// Component identifies which component raised the error
	Component string

	// Context provides structured metadata about the error
	Context map[string]interface{}

	// Cause is the underlying error (for error chain)
	Cause error

	// Stack contains the stack trace where the error was created
	Stack []StackFrame
}

// StackFrame represents a single frame in a stack trace
type StackFrame struct {
	File     string
	Line     int
	Function string
}

// Error implements the error interface
func (e *AgentError) Error() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("[%s]", e.Code))

	if e.Component != "" {
		sb.WriteString(fmt.Sprintf(" [%s]", e.Component))
	}

	if e.Operation != "" {
		sb.WriteString(fmt.Sprintf(" operation=%s", e.Operation))
	}

	sb.WriteString(": ")
	sb.WriteString(e.Message)

	if len(e.Context) > 0 {
		sb.WriteString(" (")
		first := true
		for k, v := range e.Context {
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s=%v", k, v))
			first = false
		}
		sb.WriteString(")")
	}

	if e.Cause != nil {
		sb.WriteString(fmt.Sprintf(": %v", e.Cause))
	}

	return sb.String()
}

// Unwrap returns the underlying cause for error chain support
func (e *AgentError) Unwrap() error {
	return e.Cause
}

// Is supports error comparison with errors.Is
func (e *AgentError) Is(target error) bool {
	t, ok := target.(*AgentError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// New creates a new AgentError with the given code and message
func New(code ErrorCode, message string) *AgentError {
	return &AgentError{
		Code:    code,
		Message: message,
		Context: make(map[string]interface{}),
		Stack:   captureStack(2),
	}
}

// Newf creates a new AgentError with formatted message
func Newf(code ErrorCode, format string, args ...interface{}) *AgentError {
	return &AgentError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Context: make(map[string]interface{}),
		Stack:   captureStack(2),
	}
}

// Wrap wraps an existing error with context
func Wrap(err error, code ErrorCode, message string) *AgentError {
	if err == nil {
		return nil
	}

	return &AgentError{
		Code:    code,
		Message: message,
		Context: make(map[string]interface{}),
		Cause:   err,
		Stack:   captureStack(2),
	}
}

// Wrapf wraps an existing error with formatted message
func Wrapf(err error, code ErrorCode, format string, args ...interface{}) *AgentError {
	if err == nil {
		return nil
	}

	return &AgentError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Context: make(map[string]interface{}),
		Cause:   err,
		Stack:   captureStack(2),
	}
}

// WithOperation sets the operation context
func (e *AgentError) WithOperation(operation string) *AgentError {
	e.Operation = operation
	return e
}

// WithComponent sets the component context
func (e *AgentError) WithComponent(component string) *AgentError {
	e.Component = component
	return e
}

// WithContext adds a single key-value pair to the context
func (e *AgentError) WithContext(key string, value interface{}) *AgentError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithContextMap adds multiple key-value pairs to the context
func (e *AgentError) WithContextMap(ctx map[string]interface{}) *AgentError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	for k, v := range ctx {
		e.Context[k] = v
	}
	return e
}

// GetCode extracts the error code from any error
func GetCode(err error) ErrorCode {
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return agentErr.Code
	}
	return CodeInternal
}

// GetOperation extracts the operation from any error
func GetOperation(err error) string {
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return agentErr.Operation
	}
	return ""
}

// GetComponent extracts the component from any error
func GetComponent(err error) string {
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return agentErr.Component
	}
	return ""
}

// GetContext extracts the context from any error
func GetContext(err error) map[string]interface{} {
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return agentErr.Context
	}
	return nil
}

// IsCode checks if an error has the specified code
func IsCode(err error, code ErrorCode) bool {
	return GetCode(err) == code
}

// IsAgentError checks if an error is an AgentError
func IsAgentError(err error) bool {
	var agentErr *AgentError
	return errors.As(err, &agentErr)
}

// captureStack captures the current stack trace
func captureStack(skip int) []StackFrame {
	const maxDepth = 32
	pcs := make([]uintptr, maxDepth)
	n := runtime.Callers(skip+1, pcs)

	frames := make([]StackFrame, 0, n)
	callersFrames := runtime.CallersFrames(pcs[:n])

	for {
		frame, more := callersFrames.Next()
		frames = append(frames, StackFrame{
			File:     frame.File,
			Line:     frame.Line,
			Function: frame.Function,
		})

		if !more {
			break
		}
	}

	return frames
}

// FormatStack formats the stack trace for logging
func (e *AgentError) FormatStack() string {
	if len(e.Stack) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Stack trace:\n")
	for _, frame := range e.Stack {
		sb.WriteString(fmt.Sprintf("  %s:%d %s\n", frame.File, frame.Line, frame.Function))
	}
	return sb.String()
}

// ErrorChain returns all errors in the error chain
func ErrorChain(err error) []error {
	var chain []error
	for err != nil {
		chain = append(chain, err)
		err = errors.Unwrap(err)
	}
	return chain
}

// RootCause returns the root cause of the error chain
func RootCause(err error) error {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}
