package errors

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestAgentError_Error(t *testing.T) {
	tests := []struct {
		name    string
		err     *AgentError
		wantMsg string
	}{
		{
			name:    "simple error",
			err:     New(CodeAgentExecution, "execution failed"),
			wantMsg: "[AGENT_EXECUTION]: execution failed",
		},
		{
			name: "error with component",
			err: New(CodeAgentExecution, "execution failed").
				WithComponent("test-agent"),
			wantMsg: "[AGENT_EXECUTION] [test-agent]: execution failed",
		},
		{
			name: "error with operation",
			err: New(CodeAgentExecution, "execution failed").
				WithOperation("run"),
			wantMsg: "[AGENT_EXECUTION] operation=run: execution failed",
		},
		{
			name: "error with context",
			err: New(CodeAgentExecution, "execution failed").
				WithContext("agent_name", "test").
				WithContext("attempt", 1),
			wantMsg: "[AGENT_EXECUTION]: execution failed",
		},
		{
			name:    "wrapped error",
			err:     Wrap(fmt.Errorf("underlying error"), CodeAgentExecution, "execution failed"),
			wantMsg: "[AGENT_EXECUTION]: execution failed: underlying error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if !contains(got, tt.wantMsg) {
				t.Errorf("Error() = %v, want to contain %v", got, tt.wantMsg)
			}
		})
	}
}

func TestAgentError_Unwrap(t *testing.T) {
	underlying := fmt.Errorf("underlying error")
	err := Wrap(underlying, CodeAgentExecution, "execution failed")

	if unwrapped := errors.Unwrap(err); unwrapped != underlying {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, underlying)
	}
}

func TestAgentError_Is(t *testing.T) {
	baseErr := New(CodeAgentExecution, "base error")
	wrappedErr := Wrap(baseErr, CodeToolExecution, "wrapped error")

	if !errors.Is(wrappedErr, baseErr) {
		t.Error("errors.Is() should recognize base error in chain")
	}

	differentErr := New(CodeToolExecution, "different error")
	if !errors.Is(wrappedErr, differentErr) {
		t.Error("errors.Is() should match by code")
	}
}

func TestAgentError_WithChaining(t *testing.T) {
	err := New(CodeAgentExecution, "execution failed").
		WithComponent("test-agent").
		WithOperation("run").
		WithContext("attempt", 1).
		WithContext("max_attempts", 3)

	if err.Component != "test-agent" {
		t.Errorf("Component = %v, want test-agent", err.Component)
	}
	if err.Operation != "run" {
		t.Errorf("Operation = %v, want run", err.Operation)
	}
	if err.Context["attempt"] != 1 {
		t.Errorf("Context[attempt] = %v, want 1", err.Context["attempt"])
	}
}

func TestGetCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorCode
	}{
		{
			name: "agent error",
			err:  New(CodeAgentExecution, "test"),
			want: CodeAgentExecution,
		},
		{
			name: "wrapped agent error",
			err:  Wrap(New(CodeToolExecution, "test"), CodeAgentExecution, "wrapped"),
			want: CodeAgentExecution,
		},
		{
			name: "standard error",
			err:  fmt.Errorf("standard error"),
			want: CodeInternal,
		},
		{
			name: "nil error",
			err:  nil,
			want: CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetCode(tt.err); got != tt.want {
				t.Errorf("GetCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCode(t *testing.T) {
	err := New(CodeAgentExecution, "test")

	if !IsCode(err, CodeAgentExecution) {
		t.Error("IsCode() should return true for matching code")
	}

	if IsCode(err, CodeToolExecution) {
		t.Error("IsCode() should return false for different code")
	}
}

func TestHelpers_AgentErrors(t *testing.T) {
	t.Run("NewAgentExecutionError", func(t *testing.T) {
		cause := fmt.Errorf("underlying error")
		err := NewAgentExecutionError("test-agent", "run", cause)

		if err.Code != CodeAgentExecution {
			t.Errorf("Code = %v, want %v", err.Code, CodeAgentExecution)
		}
		if err.Component != "agent" {
			t.Errorf("Component = %v, want agent", err.Component)
		}
		if err.Operation != "run" {
			t.Errorf("Operation = %v, want run", err.Operation)
		}
		if err.Context["agent_name"] != "test-agent" {
			t.Errorf("Context[agent_name] = %v, want test-agent", err.Context["agent_name"])
		}
		if !errors.Is(err, cause) {
			t.Error("should wrap underlying cause")
		}
	})

	t.Run("NewAgentValidationError", func(t *testing.T) {
		err := NewAgentValidationError("test-agent", "invalid input")

		if err.Code != CodeAgentValidation {
			t.Errorf("Code = %v, want %v", err.Code, CodeAgentValidation)
		}
	})

	t.Run("NewAgentNotFoundError", func(t *testing.T) {
		err := NewAgentNotFoundError("test-agent")

		if err.Code != CodeAgentNotFound {
			t.Errorf("Code = %v, want %v", err.Code, CodeAgentNotFound)
		}
	})
}

func TestHelpers_ToolErrors(t *testing.T) {
	t.Run("NewToolExecutionError", func(t *testing.T) {
		cause := fmt.Errorf("tool failed")
		err := NewToolExecutionError("test-tool", "execute", cause)

		if err.Code != CodeToolExecution {
			t.Errorf("Code = %v, want %v", err.Code, CodeToolExecution)
		}
		if err.Component != "tool" {
			t.Errorf("Component = %v, want tool", err.Component)
		}
	})

	t.Run("NewToolTimeoutError", func(t *testing.T) {
		err := NewToolTimeoutError("test-tool", 30)

		if err.Code != CodeToolTimeout {
			t.Errorf("Code = %v, want %v", err.Code, CodeToolTimeout)
		}
		if err.Context["timeout_seconds"] != 30 {
			t.Errorf("Context[timeout_seconds] = %v, want 30", err.Context["timeout_seconds"])
		}
	})

	t.Run("NewToolRetryExhaustedError", func(t *testing.T) {
		lastErr := fmt.Errorf("last attempt failed")
		err := NewToolRetryExhaustedError("test-tool", 3, lastErr)

		if err.Code != CodeToolRetryExhausted {
			t.Errorf("Code = %v, want %v", err.Code, CodeToolRetryExhausted)
		}
		if err.Context["attempts"] != 3 {
			t.Errorf("Context[attempts] = %v, want 3", err.Context["attempts"])
		}
	})
}

func TestHelpers_MiddlewareErrors(t *testing.T) {
	t.Run("NewMiddlewareExecutionError", func(t *testing.T) {
		cause := fmt.Errorf("middleware failed")
		err := NewMiddlewareExecutionError("test-middleware", "before", cause)

		if err.Code != CodeMiddlewareExecution {
			t.Errorf("Code = %v, want %v", err.Code, CodeMiddlewareExecution)
		}
		if err.Context["phase"] != "before" {
			t.Errorf("Context[phase] = %v, want before", err.Context["phase"])
		}
	})

	t.Run("NewMiddlewareChainError", func(t *testing.T) {
		cause := fmt.Errorf("chain failed")
		err := NewMiddlewareChainError(2, cause)

		if err.Code != CodeMiddlewareChain {
			t.Errorf("Code = %v, want %v", err.Code, CodeMiddlewareChain)
		}
		if err.Context["position"] != 2 {
			t.Errorf("Context[position] = %v, want 2", err.Context["position"])
		}
	})
}

func TestHelpers_StateErrors(t *testing.T) {
	t.Run("NewStateLoadError", func(t *testing.T) {
		cause := fmt.Errorf("load failed")
		err := NewStateLoadError("session-123", cause)

		if err.Code != CodeStateLoad {
			t.Errorf("Code = %v, want %v", err.Code, CodeStateLoad)
		}
		if err.Context["session_id"] != "session-123" {
			t.Errorf("Context[session_id] = %v, want session-123", err.Context["session_id"])
		}
	})

	t.Run("NewStateSaveError", func(t *testing.T) {
		cause := fmt.Errorf("save failed")
		err := NewStateSaveError("session-123", cause)

		if err.Code != CodeStateSave {
			t.Errorf("Code = %v, want %v", err.Code, CodeStateSave)
		}
	})
}

func TestHelpers_StreamErrors(t *testing.T) {
	t.Run("NewStreamReadError", func(t *testing.T) {
		cause := fmt.Errorf("read failed")
		err := NewStreamReadError(cause)

		if err.Code != CodeStreamRead {
			t.Errorf("Code = %v, want %v", err.Code, CodeStreamRead)
		}
	})

	t.Run("NewStreamClosedError", func(t *testing.T) {
		err := NewStreamClosedError("read")

		if err.Code != CodeStreamClosed {
			t.Errorf("Code = %v, want %v", err.Code, CodeStreamClosed)
		}
	})
}

func TestHelpers_LLMErrors(t *testing.T) {
	t.Run("NewLLMRequestError", func(t *testing.T) {
		cause := fmt.Errorf("request failed")
		err := NewLLMRequestError("openai", "gpt-4", cause)

		if err.Code != CodeLLMRequest {
			t.Errorf("Code = %v, want %v", err.Code, CodeLLMRequest)
		}
		if err.Context["provider"] != "openai" {
			t.Errorf("Context[provider] = %v, want openai", err.Context["provider"])
		}
		if err.Context["model"] != "gpt-4" {
			t.Errorf("Context[model] = %v, want gpt-4", err.Context["model"])
		}
	})

	t.Run("NewLLMRateLimitError", func(t *testing.T) {
		err := NewLLMRateLimitError("openai", "gpt-4", 60)

		if err.Code != CodeLLMRateLimit {
			t.Errorf("Code = %v, want %v", err.Code, CodeLLMRateLimit)
		}
		if err.Context["retry_after_seconds"] != 60 {
			t.Errorf("Context[retry_after_seconds] = %v, want 60", err.Context["retry_after_seconds"])
		}
	})
}

func TestHelpers_ContextErrors(t *testing.T) {
	t.Run("NewContextCanceledError", func(t *testing.T) {
		err := NewContextCanceledError("run_agent")

		if err.Code != CodeContextCanceled {
			t.Errorf("Code = %v, want %v", err.Code, CodeContextCanceled)
		}
		if !errors.Is(err, context.Canceled) {
			t.Error("should wrap context.Canceled")
		}
	})

	t.Run("NewContextTimeoutError", func(t *testing.T) {
		err := NewContextTimeoutError("run_agent", 30)

		if err.Code != CodeContextTimeout {
			t.Errorf("Code = %v, want %v", err.Code, CodeContextTimeout)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Error("should wrap context.DeadlineExceeded")
		}
	})
}

func TestErrorChain(t *testing.T) {
	err1 := fmt.Errorf("base error")
	err2 := Wrap(err1, CodeToolExecution, "tool failed")
	err3 := Wrap(err2, CodeAgentExecution, "agent failed")

	chain := ErrorChain(err3)

	if len(chain) != 3 {
		t.Errorf("ErrorChain() length = %v, want 3", len(chain))
	}
}

func TestRootCause(t *testing.T) {
	base := fmt.Errorf("base error")
	err1 := Wrap(base, CodeToolExecution, "tool failed")
	err2 := Wrap(err1, CodeAgentExecution, "agent failed")

	root := RootCause(err2)

	if root != base {
		t.Errorf("RootCause() = %v, want %v", root, base)
	}
}

func TestErrorWithRetry(t *testing.T) {
	baseErr := New(CodeToolExecution, "tool failed")
	err := ErrorWithRetry(baseErr, 2, 3)

	if err.Context["retry_attempt"] != 2 {
		t.Errorf("Context[retry_attempt] = %v, want 2", err.Context["retry_attempt"])
	}
	if err.Context["max_attempts"] != 3 {
		t.Errorf("Context[max_attempts] = %v, want 3", err.Context["max_attempts"])
	}
}

func TestErrorWithDuration(t *testing.T) {
	baseErr := New(CodeAgentExecution, "agent failed")
	err := ErrorWithDuration(baseErr, 1500)

	if err.Context["duration_ms"] != int64(1500) {
		t.Errorf("Context[duration_ms] = %v, want 1500", err.Context["duration_ms"])
	}
}

func TestStackTrace(t *testing.T) {
	err := New(CodeAgentExecution, "test error")

	if len(err.Stack) == 0 {
		t.Error("Stack should not be empty")
	}

	stackStr := err.FormatStack()
	if stackStr == "" {
		t.Error("FormatStack() should not be empty")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
