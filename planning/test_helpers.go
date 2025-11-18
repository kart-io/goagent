package planning

import (
	"context"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	loggercore "github.com/kart-io/logger/core"
)

// Compile-time interface verification
var (
	_ llm.Client                = (*MockLLMClient)(nil)
	_ interfaces.MemoryManager  = (*MockMemoryManager)(nil)
	_ loggercore.Logger         = (*MockLogger)(nil)
	_ core.Agent                = (*MockAgent)(nil)
)

// MockLLMClient is a complete mock LLM client for testing
type MockLLMClient struct {
	CompleteFn    func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error)
	ChatFn        func(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error)
	ProviderFn    func() llm.Provider
	IsAvailableFn func() bool
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.CompleteFn != nil {
		return m.CompleteFn(ctx, req)
	}
	return &llm.CompletionResponse{
		Content: "Strategy: decomposition\n\nStep 1: Analyze\nStep 2: Execute\nStep 3: Validate",
		Usage: &interfaces.TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}, nil
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	if m.ChatFn != nil {
		return m.ChatFn(ctx, messages)
	}
	return &llm.CompletionResponse{
		Content: "Chat response",
	}, nil
}

func (m *MockLLMClient) Provider() llm.Provider {
	if m.ProviderFn != nil {
		return m.ProviderFn()
	}
	return llm.ProviderOpenAI
}

func (m *MockLLMClient) IsAvailable() bool {
	if m.IsAvailableFn != nil {
		return m.IsAvailableFn()
	}
	return true
}

// MockMemoryManager is a complete mock memory manager for testing
type MockMemoryManager struct {
	AddConversationFn        func(ctx context.Context, conv *interfaces.Conversation) error
	GetConversationHistoryFn func(ctx context.Context, sessionID string, limit int) ([]*interfaces.Conversation, error)
	ClearConversationFn      func(ctx context.Context, sessionID string) error
	AddCaseFn                func(ctx context.Context, caseMemory *interfaces.Case) error
	SearchSimilarCasesFn     func(ctx context.Context, query string, limit int) ([]*interfaces.Case, error)
	GetCaseFn                func(ctx context.Context, caseID string) (*interfaces.Case, error)
	UpdateCaseFn             func(ctx context.Context, caseMemory *interfaces.Case) error
	DeleteCaseFn             func(ctx context.Context, caseID string) error
	StoreFn                  func(ctx context.Context, key string, value interface{}) error
	RetrieveFn               func(ctx context.Context, key string) (interface{}, error)
	DeleteFn                 func(ctx context.Context, key string) error
}

func (m *MockMemoryManager) AddConversation(ctx context.Context, conv *interfaces.Conversation) error {
	if m.AddConversationFn != nil {
		return m.AddConversationFn(ctx, conv)
	}
	return nil
}

func (m *MockMemoryManager) GetConversationHistory(ctx context.Context, sessionID string, limit int) ([]*interfaces.Conversation, error) {
	if m.GetConversationHistoryFn != nil {
		return m.GetConversationHistoryFn(ctx, sessionID, limit)
	}
	return []*interfaces.Conversation{}, nil
}

func (m *MockMemoryManager) ClearConversation(ctx context.Context, sessionID string) error {
	if m.ClearConversationFn != nil {
		return m.ClearConversationFn(ctx, sessionID)
	}
	return nil
}

func (m *MockMemoryManager) AddCase(ctx context.Context, caseMemory *interfaces.Case) error {
	if m.AddCaseFn != nil {
		return m.AddCaseFn(ctx, caseMemory)
	}
	return nil
}

func (m *MockMemoryManager) SearchSimilarCases(ctx context.Context, query string, limit int) ([]*interfaces.Case, error) {
	if m.SearchSimilarCasesFn != nil {
		return m.SearchSimilarCasesFn(ctx, query, limit)
	}
	// Return empty slice to avoid nil Metrics in production code bug
	return []*interfaces.Case{}, nil
}

func (m *MockMemoryManager) GetCase(ctx context.Context, caseID string) (*interfaces.Case, error) {
	if m.GetCaseFn != nil {
		return m.GetCaseFn(ctx, caseID)
	}
	return nil, agentErrors.New(agentErrors.CodeInternal, "case not found")
}

func (m *MockMemoryManager) UpdateCase(ctx context.Context, caseMemory *interfaces.Case) error {
	if m.UpdateCaseFn != nil {
		return m.UpdateCaseFn(ctx, caseMemory)
	}
	return nil
}

func (m *MockMemoryManager) DeleteCase(ctx context.Context, caseID string) error {
	if m.DeleteCaseFn != nil {
		return m.DeleteCaseFn(ctx, caseID)
	}
	return nil
}

// Clear removes all memory (implements MemoryManager interface)
func (m *MockMemoryManager) Clear(ctx context.Context) error {
	// Clear all conversation history
	if err := m.ClearConversation(ctx, ""); err != nil {
		return err
	}
	// Could also clear all cases, storage, etc. if needed
	return nil
}

// Store persists key-value data
func (m *MockMemoryManager) Store(ctx context.Context, key string, value interface{}) error {
	if m.StoreFn != nil {
		return m.StoreFn(ctx, key, value)
	}
	return nil
}

// Retrieve fetches stored data by key
func (m *MockMemoryManager) Retrieve(ctx context.Context, key string) (interface{}, error) {
	if m.RetrieveFn != nil {
		return m.RetrieveFn(ctx, key)
	}
	return nil, agentErrors.New(agentErrors.CodeInternal, "key not found")
}

// Delete removes stored data by key
func (m *MockMemoryManager) Delete(ctx context.Context, key string) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, key)
	}
	return nil
}

// MockLogger is a complete mock logger for testing
type MockLogger struct{}

func (m *MockLogger) Debug(args ...interface{})                       {}
func (m *MockLogger) Info(args ...interface{})                        {}
func (m *MockLogger) Warn(args ...interface{})                        {}
func (m *MockLogger) Error(args ...interface{})                       {}
func (m *MockLogger) Fatal(args ...interface{})                       {}
func (m *MockLogger) Debugf(template string, args ...interface{})     {}
func (m *MockLogger) Infof(template string, args ...interface{})      {}
func (m *MockLogger) Warnf(template string, args ...interface{})      {}
func (m *MockLogger) Errorf(template string, args ...interface{})     {}
func (m *MockLogger) Fatalf(template string, args ...interface{})     {}
func (m *MockLogger) Debugw(msg string, keysAndValues ...interface{}) {}
func (m *MockLogger) Infow(msg string, keysAndValues ...interface{})  {}
func (m *MockLogger) Warnw(msg string, keysAndValues ...interface{})  {}
func (m *MockLogger) Errorw(msg string, keysAndValues ...interface{}) {}
func (m *MockLogger) Fatalw(msg string, keysAndValues ...interface{}) {}
func (m *MockLogger) With(keyValues ...interface{}) loggercore.Logger { return m }
func (m *MockLogger) WithCtx(ctx context.Context, keyValues ...interface{}) loggercore.Logger {
	return m
}
func (m *MockLogger) WithCallerSkip(skip int) loggercore.Logger { return m }
func (m *MockLogger) SetLevel(level loggercore.Level)           {}
func (m *MockLogger) Flush() error                              { return nil }

// MockAgent is a complete mock agent for testing
type MockAgent struct {
	name         string
	description  string
	capabilities []string
	invokeFn     func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error)
	streamFn     func(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error)
	batchFn      func(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error)
}

func NewMockAgent(name string) *MockAgent {
	return &MockAgent{
		name:         name,
		description:  "Mock agent for testing",
		capabilities: []string{"test"},
	}
}

func (m *MockAgent) Name() string           { return m.name }
func (m *MockAgent) Description() string    { return m.description }
func (m *MockAgent) Capabilities() []string { return m.capabilities }

func (m *MockAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, input)
	}
	return &core.AgentOutput{
		Result: "success",
		Status: "success",
		Metadata: map[string]interface{}{
			"test": true,
		},
	}, nil
}

func (m *MockAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, input)
	}
	ch := make(chan core.StreamChunk[*core.AgentOutput])
	close(ch)
	return ch, nil
}

func (m *MockAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	if m.batchFn != nil {
		return m.batchFn(ctx, inputs)
	}
	outputs := make([]*core.AgentOutput, len(inputs))
	for i := range inputs {
		outputs[i] = &core.AgentOutput{Result: "success", Status: "success"}
	}
	return outputs, nil
}

func (m *MockAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return nil
}

func (m *MockAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return m
}

func (m *MockAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return m
}

// SetInvokeFn sets a custom invoke function for testing
func (m *MockAgent) SetInvokeFn(fn func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error)) {
	m.invokeFn = fn
}
