package errors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"
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

// Error() 文本会传播到日志、事件与模型输入，不得包含 Context 的键与值。
func TestAgentError_ErrorExcludesContext(t *testing.T) {
	err := Wrap(fmt.Errorf("底层错误"), CodeLLMRequest, "request failed").
		WithComponent("llm").
		WithOperation("call").
		WithContext("url", "https://api.example.com/v1?key=secret").
		WithContext("prompt", "用户输入")

	want := "[LLM_REQUEST] [llm] operation=call: request failed: 底层错误"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	for _, leaked := range []string{"secret", "用户输入", "url=", "prompt="} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("Error() 不应包含 %q: %s", leaked, err.Error())
		}
	}
	if GetContext(err)["prompt"] != "用户输入" {
		t.Errorf("GetContext 应保留 Context: %v", GetContext(err))
	}
}

func TestAgentError_LogValue(t *testing.T) {
	err := Wrap(fmt.Errorf("底层错误"), CodeToolExecution, "tool failed").
		WithComponent("tool").
		WithOperation("run").
		WithContext("tool_name", "search").
		WithContext("attempt", 2)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Error("失败", "err", err)

	var record struct {
		Err map[string]any `json:"err"`
	}
	if jerr := json.Unmarshal(buf.Bytes(), &record); jerr != nil {
		t.Fatalf("解析日志失败: %v, 原文: %s", jerr, buf.String())
	}
	want := map[string]any{
		"code":      "TOOL_EXECUTION",
		"component": "tool",
		"operation": "run",
		"message":   "tool failed",
		"context":   map[string]any{"tool_name": "search", "attempt": float64(2)},
		"cause":     "底层错误",
	}
	got, _ := json.Marshal(record.Err)
	wantJSON, _ := json.Marshal(want)
	if string(got) != string(wantJSON) {
		t.Errorf("LogValue = %s, want %s", got, wantJSON)
	}

	// 无 Context、Component、Operation、Cause 时只输出 code 与 message
	attrs := New(CodeInternal, "boom").LogValue().Group()
	if len(attrs) != 2 || attrs[0].Key != "code" || attrs[1].Key != "message" {
		t.Errorf("最小 LogValue = %v", attrs)
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://user:pass@api.example.com/v1/chat?key=secret#frag", "https://api.example.com/v1/chat"},
		{"https://api.example.com/v1?", "https://api.example.com/v1"},
		{"nats://token@nats.example.com:4222", "nats://nats.example.com:4222"},
		{"postgres://admin:pw@db:5432/app?sslmode=disable&password=x", "postgres://db:5432/app"},
		{"http://localhost:8080", "http://localhost:8080"},
		{"localhost:6379", "localhost:6379"},
		{"127.0.0.1:6379", "127.0.0.1:6379"},
		{"user:pw@10.0.0.1:6379", "10.0.0.1:6379"},
		{"://bad@host/path?token=1", "://host/path"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := RedactURL(tt.in); got != tt.want {
			t.Errorf("RedactURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConnectionErrorsRedactEndpoint(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	for _, err := range []*AgentError{
		NewStoreConnectionError("postgres", "postgres://admin:pw@db:5432/app?sslmode=disable", cause),
		NewDistributedConnectionError("https://u:p@node.example.com/api?token=t", cause),
	} {
		ep, _ := err.Context["endpoint"].(string)
		if strings.ContainsAny(ep, "@?") || strings.Contains(ep, "pw") || strings.Contains(ep, "token") {
			t.Errorf("endpoint 未脱敏: %q", ep)
		}
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
			want: "",
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

	if len(err.StackFrames()) == 0 {
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

func TestHelpers_DistributedErrors(t *testing.T) {
	t.Run("NewDistributedConnectionError", func(t *testing.T) {
		cause := fmt.Errorf("connection refused")
		err := NewDistributedConnectionError("http://localhost:8080", cause)

		if err.Code != CodeDistributedConnection {
			t.Errorf("Code = %v, want %v", err.Code, CodeDistributedConnection)
		}
		if err.Component != "distributed" {
			t.Errorf("Component = %v, want distributed", err.Component)
		}
		if err.Context["endpoint"] != "http://localhost:8080" {
			t.Errorf("Context[endpoint] = %v, want http://localhost:8080", err.Context["endpoint"])
		}
	})

	t.Run("NewDistributedSerializationError", func(t *testing.T) {
		cause := fmt.Errorf("invalid json")
		err := NewDistributedSerializationError("AgentInput", cause)

		if err.Code != CodeDistributedSerialization {
			t.Errorf("Code = %v, want %v", err.Code, CodeDistributedSerialization)
		}
		if err.Context["data_type"] != "AgentInput" {
			t.Errorf("Context[data_type] = %v, want AgentInput", err.Context["data_type"])
		}
	})

	t.Run("NewDistributedCoordinationError", func(t *testing.T) {
		cause := fmt.Errorf("leader election failed")
		err := NewDistributedCoordinationError("elect_leader", cause)

		if err.Code != CodeDistributedCoordination {
			t.Errorf("Code = %v, want %v", err.Code, CodeDistributedCoordination)
		}
		if err.Operation != "elect_leader" {
			t.Errorf("Operation = %v, want elect_leader", err.Operation)
		}
	})
}

func TestHelpers_RetrievalErrors(t *testing.T) {
	t.Run("NewRetrievalSearchError", func(t *testing.T) {
		cause := fmt.Errorf("search failed")
		err := NewRetrievalSearchError("test query", cause)

		if err.Code != CodeRetrievalSearch {
			t.Errorf("Code = %v, want %v", err.Code, CodeRetrievalSearch)
		}
		if err.Context["query"] != "test query" {
			t.Errorf("Context[query] = %v, want test query", err.Context["query"])
		}
	})

	t.Run("NewRetrievalEmbeddingError", func(t *testing.T) {
		cause := fmt.Errorf("embedding failed")
		longText := string(make([]byte, 200)) // text longer than 100 chars
		err := NewRetrievalEmbeddingError(longText, cause)

		if err.Code != CodeRetrievalEmbedding {
			t.Errorf("Code = %v, want %v", err.Code, CodeRetrievalEmbedding)
		}
		preview := err.Context["text_preview"].(string)
		if len(preview) > 103 { // 100 chars + "..."
			t.Errorf("text_preview should be truncated, got length %d", len(preview))
		}
	})

	t.Run("NewDocumentNotFoundError", func(t *testing.T) {
		err := NewDocumentNotFoundError("doc-123")

		if err.Code != CodeDocumentNotFound {
			t.Errorf("Code = %v, want %v", err.Code, CodeDocumentNotFound)
		}
		if err.Context["document_id"] != "doc-123" {
			t.Errorf("Context[document_id] = %v, want doc-123", err.Context["document_id"])
		}
	})

	t.Run("NewVectorDimMismatchError", func(t *testing.T) {
		err := NewVectorDimMismatchError(512, 768)

		if err.Code != CodeVectorDimMismatch {
			t.Errorf("Code = %v, want %v", err.Code, CodeVectorDimMismatch)
		}
		if err.Context["expected_dim"] != 512 {
			t.Errorf("Context[expected_dim] = %v, want 512", err.Context["expected_dim"])
		}
		if err.Context["actual_dim"] != 768 {
			t.Errorf("Context[actual_dim] = %v, want 768", err.Context["actual_dim"])
		}
	})
}

func TestHelpers_PlanningErrors(t *testing.T) {
	t.Run("NewPlanningError", func(t *testing.T) {
		cause := fmt.Errorf("planning failed")
		err := NewPlanningError("solve complex problem", cause)

		if err.Code != CodePlanningFailed {
			t.Errorf("Code = %v, want %v", err.Code, CodePlanningFailed)
		}
		if err.Context["goal"] != "solve complex problem" {
			t.Errorf("Context[goal] = %v, want solve complex problem", err.Context["goal"])
		}
	})

	t.Run("NewPlanValidationError", func(t *testing.T) {
		err := NewPlanValidationError("plan-123", "missing required step")

		if err.Code != CodePlanValidation {
			t.Errorf("Code = %v, want %v", err.Code, CodePlanValidation)
		}
		if err.Context["plan_id"] != "plan-123" {
			t.Errorf("Context[plan_id] = %v, want plan-123", err.Context["plan_id"])
		}
	})

	t.Run("NewPlanExecutionError", func(t *testing.T) {
		cause := fmt.Errorf("step failed")
		err := NewPlanExecutionError("plan-123", "step-5", cause)

		if err.Code != CodePlanExecutionFailed {
			t.Errorf("Code = %v, want %v", err.Code, CodePlanExecutionFailed)
		}
		if err.Context["plan_id"] != "plan-123" {
			t.Errorf("Context[plan_id] = %v, want plan-123", err.Context["plan_id"])
		}
		if err.Context["step_id"] != "step-5" {
			t.Errorf("Context[step_id] = %v, want step-5", err.Context["step_id"])
		}
	})

	t.Run("NewPlanNotFoundError", func(t *testing.T) {
		err := NewPlanNotFoundError("plan-123")

		if err.Code != CodePlanNotFound {
			t.Errorf("Code = %v, want %v", err.Code, CodePlanNotFound)
		}
		if err.Context["plan_id"] != "plan-123" {
			t.Errorf("Context[plan_id] = %v, want plan-123", err.Context["plan_id"])
		}
	})
}

func TestHelpers_ParserErrors(t *testing.T) {
	t.Run("NewParserError", func(t *testing.T) {
		cause := fmt.Errorf("parse failed")
		longContent := string(make([]byte, 300)) // content longer than 200 chars
		err := NewParserError("json", longContent, cause)

		if err.Code != CodeParserFailed {
			t.Errorf("Code = %v, want %v", err.Code, CodeParserFailed)
		}
		if err.Context["parser_type"] != "json" {
			t.Errorf("Context[parser_type] = %v, want json", err.Context["parser_type"])
		}
		preview := err.Context["content_preview"].(string)
		if len(preview) > 203 { // 200 chars + "..."
			t.Errorf("content_preview should be truncated, got length %d", len(preview))
		}
	})

	t.Run("NewParserInvalidJSONError", func(t *testing.T) {
		cause := fmt.Errorf("invalid json")
		err := NewParserInvalidJSONError(`{"invalid": json}`, cause)

		if err.Code != CodeParserInvalidJSON {
			t.Errorf("Code = %v, want %v", err.Code, CodeParserInvalidJSON)
		}
	})

	t.Run("NewParserMissingFieldError", func(t *testing.T) {
		err := NewParserMissingFieldError("action")

		if err.Code != CodeParserMissingField {
			t.Errorf("Code = %v, want %v", err.Code, CodeParserMissingField)
		}
		if err.Context["missing_field"] != "action" {
			t.Errorf("Context[missing_field] = %v, want action", err.Context["missing_field"])
		}
	})
}

func TestHelpers_MultiAgentErrors(t *testing.T) {
	t.Run("NewMultiAgentRegistrationError", func(t *testing.T) {
		cause := fmt.Errorf("registration failed")
		err := NewMultiAgentRegistrationError("agent-123", cause)

		if err.Code != CodeMultiAgentRegistration {
			t.Errorf("Code = %v, want %v", err.Code, CodeMultiAgentRegistration)
		}
		if err.Context["agent_id"] != "agent-123" {
			t.Errorf("Context[agent_id] = %v, want agent-123", err.Context["agent_id"])
		}
	})

	t.Run("NewMultiAgentConsensusError", func(t *testing.T) {
		votes := map[string]bool{
			"agent-1": true,
			"agent-2": false,
			"agent-3": true,
		}
		err := NewMultiAgentConsensusError(votes)

		if err.Code != CodeMultiAgentConsensus {
			t.Errorf("Code = %v, want %v", err.Code, CodeMultiAgentConsensus)
		}
		if err.Context["yes_votes"] != 2 {
			t.Errorf("Context[yes_votes] = %v, want 2", err.Context["yes_votes"])
		}
		if err.Context["no_votes"] != 1 {
			t.Errorf("Context[no_votes] = %v, want 1", err.Context["no_votes"])
		}
		if err.Context["total_votes"] != 3 {
			t.Errorf("Context[total_votes] = %v, want 3", err.Context["total_votes"])
		}
	})

	t.Run("NewMultiAgentMessageError", func(t *testing.T) {
		cause := fmt.Errorf("message send failed")
		err := NewMultiAgentMessageError("agent.task", cause)

		if err.Code != CodeMultiAgentMessage {
			t.Errorf("Code = %v, want %v", err.Code, CodeMultiAgentMessage)
		}
		if err.Context["topic"] != "agent.task" {
			t.Errorf("Context[topic] = %v, want agent.task", err.Context["topic"])
		}
	})
}

func TestHelpers_StoreErrors(t *testing.T) {
	t.Run("NewStoreConnectionError", func(t *testing.T) {
		cause := fmt.Errorf("connection failed")
		err := NewStoreConnectionError("redis", "localhost:6379", cause)

		if err.Code != CodeStoreConnection {
			t.Errorf("Code = %v, want %v", err.Code, CodeStoreConnection)
		}
		if err.Context["store_type"] != "redis" {
			t.Errorf("Context[store_type] = %v, want redis", err.Context["store_type"])
		}
		if err.Context["endpoint"] != "localhost:6379" {
			t.Errorf("Context[endpoint] = %v, want localhost:6379", err.Context["endpoint"])
		}
	})

	t.Run("NewStoreSerializationError", func(t *testing.T) {
		cause := fmt.Errorf("serialization failed")
		err := NewStoreSerializationError("session:123", cause)

		if err.Code != CodeStoreSerialization {
			t.Errorf("Code = %v, want %v", err.Code, CodeStoreSerialization)
		}
		if err.Context["key"] != "session:123" {
			t.Errorf("Context[key] = %v, want session:123", err.Context["key"])
		}
	})

	t.Run("NewStoreNotFoundError", func(t *testing.T) {
		namespace := []string{"memory", "session"}
		err := NewStoreNotFoundError(namespace, "key-123")

		if err.Code != CodeStoreNotFound {
			t.Errorf("Code = %v, want %v", err.Code, CodeStoreNotFound)
		}
		if err.Context["key"] != "key-123" {
			t.Errorf("Context[key] = %v, want key-123", err.Context["key"])
		}
	})
}

func TestHelpers_RouterErrors(t *testing.T) {
	t.Run("NewRouterNoMatchError", func(t *testing.T) {
		err := NewRouterNoMatchError("user.login", "/api/*")

		if err.Code != CodeRouterNoMatch {
			t.Errorf("Code = %v, want %v", err.Code, CodeRouterNoMatch)
		}
		if err.Context["topic"] != "user.login" {
			t.Errorf("Context[topic] = %v, want user.login", err.Context["topic"])
		}
		if err.Context["pattern"] != "/api/*" {
			t.Errorf("Context[pattern] = %v, want /api/*", err.Context["pattern"])
		}
	})

	t.Run("NewRouterFailedError", func(t *testing.T) {
		cause := fmt.Errorf("routing failed")
		err := NewRouterFailedError("semantic", cause)

		if err.Code != CodeRouterFailed {
			t.Errorf("Code = %v, want %v", err.Code, CodeRouterFailed)
		}
		if err.Context["router_type"] != "semantic" {
			t.Errorf("Context[router_type] = %v, want semantic", err.Context["router_type"])
		}
	})

	t.Run("NewRouterOverloadError", func(t *testing.T) {
		err := NewRouterOverloadError(100, 150)

		if err.Code != CodeRouterOverload {
			t.Errorf("Code = %v, want %v", err.Code, CodeRouterOverload)
		}
		if err.Context["capacity"] != 100 {
			t.Errorf("Context[capacity] = %v, want 100", err.Context["capacity"])
		}
		if err.Context["current"] != 150 {
			t.Errorf("Context[current] = %v, want 150", err.Context["current"])
		}
	})
}

func TestNilAgentErrorIsSafe(t *testing.T) {
	var cause error
	wrapped := Wrap(cause, CodeLLMRequest, "x").WithComponent("c").WithOperation("o").
		WithContext("k", 1).WithContextMap(map[string]interface{}{"a": 2}).MarkModelArgument()
	if wrapped != nil {
		t.Fatalf("Wrap(nil) 链式调用应保持 nil，得到 %v", wrapped)
	}
	if NewLLMRequestError("p", "m", nil) != nil {
		t.Error("cause 为 nil 的辅助构造函数应返回 nil 而不是 panic")
	}
	if ErrorWithRetry(nil, 1, 3) != nil || ErrorWithDuration(nil, 1) != nil {
		t.Error("nil 错误不应被包装")
	}
	var nilErr *AgentError
	if got := nilErr.Error(); got != "<nil>" {
		t.Errorf("nil.Error() = %q", got)
	}
	if got := fmt.Sprintf("%+v", nilErr); got != "<nil>" {
		t.Errorf("%%+v = %q", got)
	}
	if msg, ok := nilErr.ModelArgumentMessage(); msg != "" || ok {
		t.Error("nil.ModelArgumentMessage() 应为空")
	}
	if nilErr.StackFrames() != nil || nilErr.FormatStack() != "" || nilErr.Unwrap() != nil {
		t.Error("nil 的栈与 Unwrap 应为空")
	}
	if nilErr.Is(New(CodeLLMRequest, "x")) {
		t.Error("nil 不应匹配任何错误码")
	}
}

func TestGetCodeNil(t *testing.T) {
	if got := GetCode(nil); got != "" {
		t.Errorf("GetCode(nil) = %q, want empty", got)
	}
	if IsCode(nil, CodeInternal) {
		t.Error("IsCode(nil, CodeInternal) 应为 false")
	}
	if got := GetCode(fmt.Errorf("plain")); got != CodeInternal {
		t.Errorf("非 AgentError 应为 CodeInternal，得到 %q", got)
	}
}

func TestAnnotateDoesNotMutateInput(t *testing.T) {
	base := New(CodeToolExecution, "tool failed").WithContext("tool", "x")
	retried := ErrorWithRetry(base, 2, 3)
	timed := ErrorWithDuration(base, 10)
	if _, ok := base.Context["retry_attempt"]; ok {
		t.Error("ErrorWithRetry 不应修改原错误")
	}
	if _, ok := base.Context["duration_ms"]; ok {
		t.Error("ErrorWithDuration 不应修改原错误")
	}
	if retried.Context["tool"] != "x" || timed.Context["tool"] != "x" {
		t.Error("副本应保留原 Context")
	}
	if retried == base || retried.Code != CodeToolExecution {
		t.Error("顶层 *AgentError 应返回同错误码的副本")
	}

	wrapped := fmt.Errorf("外层: %w", New(CodeLLMTimeout, "timeout"))
	got := ErrorWithRetry(wrapped, 1, 3)
	if got.Code != CodeLLMTimeout {
		t.Errorf("被包装的 *AgentError 应沿用其错误码，得到 %s", got.Code)
	}
	if !errors.Is(got, wrapped) {
		t.Error("应保留原错误链")
	}
}

func TestPreviewKeepsValidUTF8(t *testing.T) {
	text := strings.Repeat("中", 150)
	got := NewRetrievalEmbeddingError(text, fmt.Errorf("x")).Context["text_preview"].(string)
	if !utf8.ValidString(got) || got != strings.Repeat("中", 100)+"..." {
		t.Errorf("预览应按字符截断为 100 个字符，得到 %d 个字符", utf8.RuneCountInString(got))
	}
	content := strings.Repeat("é", 250)
	got = NewParserInvalidJSONError(content, fmt.Errorf("x")).Context["content_preview"].(string)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 203 {
		t.Errorf("内容预览应为 200 个字符加省略号，得到 %d", utf8.RuneCountInString(got))
	}
	if short := NewParserError("json", "短", fmt.Errorf("x")).Context["content_preview"]; short != "短" {
		t.Errorf("短内容不截断，得到 %v", short)
	}
}

func TestStackFramesPointToCaller(t *testing.T) {
	frames := New(CodeInternal, "x").StackFrames()
	if len(frames) == 0 || !strings.HasSuffix(frames[0].Function, "TestStackFramesPointToCaller") {
		t.Fatalf("首帧应为调用 New 的函数，得到 %+v", frames)
	}
	frames = NewLLMRequestError("p", "m", fmt.Errorf("x")).StackFrames()
	if len(frames) < 2 || !strings.HasSuffix(frames[1].Function, "TestStackFramesPointToCaller") {
		t.Fatalf("辅助构造函数之后应为调用方，得到 %+v", frames)
	}
}

func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = New(CodeInternal, "x")
	}
}

func BenchmarkWrapWithContextError(b *testing.B) {
	b.ReportAllocs()
	cause := fmt.Errorf("cause")
	for i := 0; i < b.N; i++ {
		_ = NewLLMRequestError("openai", "gpt", cause).Error()
	}
}

type nilPtrErr struct{ msg string }

func (e *nilPtrErr) Error() string { return e.msg }

func TestErrorWithNilPointerCause(t *testing.T) {
	var cause *nilPtrErr
	got := Wrap(cause, CodeInternal, "m").Error()
	if !strings.HasPrefix(got, "[INTERNAL_ERROR]: m: ") {
		t.Errorf("Cause 为值是 nil 的指针时不应 panic，得到 %q", got)
	}
}
