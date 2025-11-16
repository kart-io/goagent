package interfaces

import (
	"context"
	"testing"
	"time"
)

// TestConversationStructure verifies Conversation struct is properly defined
func TestConversationStructure(t *testing.T) {
	conv := &Conversation{
		ID:        "conv-123",
		SessionID: "session-456",
		Role:      "user",
		Content:   "Hello, how are you?",
		Timestamp: time.Now(),
		Metadata:  map[string]interface{}{"model": "gpt-4"},
	}

	if conv.ID != "conv-123" {
		t.Errorf("Expected ID 'conv-123', got '%s'", conv.ID)
	}
	if conv.Role != "user" {
		t.Errorf("Expected Role 'user', got '%s'", conv.Role)
	}
	if conv.SessionID != "session-456" {
		t.Errorf("Expected SessionID 'session-456', got '%s'", conv.SessionID)
	}
	if conv.Content != "Hello, how are you?" {
		t.Errorf("Expected Content 'Hello, how are you?', got '%s'", conv.Content)
	}
	if conv.Metadata["model"] != "gpt-4" {
		t.Errorf("Expected Metadata model 'gpt-4', got '%v'", conv.Metadata["model"])
	}
}

// TestCaseStructure verifies Case struct is properly defined
func TestCaseStructure(t *testing.T) {
	now := time.Now()
	c := &Case{
		ID:          "case-123",
		Title:       "Database Timeout",
		Description: "Connection timeout when accessing database",
		Problem:     "App cannot connect to database",
		Solution:    "Increase connection timeout to 30s",
		Category:    "infrastructure",
		Tags:        []string{"database", "timeout", "postgres"},
		Embedding:   []float64{0.1, 0.2, 0.3},
		Similarity:  0.85,
		CreatedAt:   now,
		UpdatedAt:   now,
		Metadata:    map[string]interface{}{"source": "production"},
	}

	if c.ID != "case-123" {
		t.Errorf("Expected ID 'case-123', got '%s'", c.ID)
	}
	if c.Title != "Database Timeout" {
		t.Errorf("Expected Title 'Database Timeout', got '%s'", c.Title)
	}
	if c.Category != "infrastructure" {
		t.Errorf("Expected Category 'infrastructure', got '%s'", c.Category)
	}
	if len(c.Tags) != 3 {
		t.Errorf("Expected 3 tags, got %d", len(c.Tags))
	}
	if len(c.Embedding) != 3 {
		t.Errorf("Expected 3 embedding dimensions, got %d", len(c.Embedding))
	}
	if c.Similarity != 0.85 {
		t.Errorf("Expected Similarity 0.85, got %f", c.Similarity)
	}
}

// mockMemoryManager is a minimal test implementation of MemoryManager
type mockMemoryManager struct {
	conversations map[string][]*Conversation
	cases         []*Case
	storage       map[string]interface{}
}

func newMockMemoryManager() *mockMemoryManager {
	return &mockMemoryManager{
		conversations: make(map[string][]*Conversation),
		cases:         make([]*Case, 0),
		storage:       make(map[string]interface{}),
	}
}

func (m *mockMemoryManager) AddConversation(ctx context.Context, conv *Conversation) error {
	if m.conversations[conv.SessionID] == nil {
		m.conversations[conv.SessionID] = make([]*Conversation, 0)
	}
	m.conversations[conv.SessionID] = append(m.conversations[conv.SessionID], conv)
	return nil
}

func (m *mockMemoryManager) GetConversationHistory(ctx context.Context, sessionID string, limit int) ([]*Conversation, error) {
	convs := m.conversations[sessionID]
	if limit > 0 && len(convs) > limit {
		return convs[:limit], nil
	}
	return convs, nil
}

func (m *mockMemoryManager) ClearConversation(ctx context.Context, sessionID string) error {
	delete(m.conversations, sessionID)
	return nil
}

func (m *mockMemoryManager) AddCase(ctx context.Context, caseMemory *Case) error {
	m.cases = append(m.cases, caseMemory)
	return nil
}

func (m *mockMemoryManager) SearchSimilarCases(ctx context.Context, query string, limit int) ([]*Case, error) {
	// Simple mock: return all cases with similarity based on query length
	result := make([]*Case, 0)
	for _, c := range m.cases {
		caseCopy := *c
		caseCopy.Similarity = 0.5
		result = append(result, &caseCopy)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (m *mockMemoryManager) Store(ctx context.Context, key string, value interface{}) error {
	m.storage[key] = value
	return nil
}

func (m *mockMemoryManager) Retrieve(ctx context.Context, key string) (interface{}, error) {
	return m.storage[key], nil
}

func (m *mockMemoryManager) Delete(ctx context.Context, key string) error {
	delete(m.storage, key)
	return nil
}

func (m *mockMemoryManager) Clear(ctx context.Context) error {
	m.conversations = make(map[string][]*Conversation)
	m.cases = make([]*Case, 0)
	m.storage = make(map[string]interface{})
	return nil
}

// Ensure mockMemoryManager implements MemoryManager interface
var _ MemoryManager = (*mockMemoryManager)(nil)

// TestMemoryManagerInterface verifies the MemoryManager interface works correctly
func TestMemoryManagerInterface(t *testing.T) {
	ctx := context.Background()
	manager := newMockMemoryManager()

	// Test conversation methods
	conv := &Conversation{
		ID:        "conv-1",
		SessionID: "session-1",
		Role:      "user",
		Content:   "Test message",
		Timestamp: time.Now(),
	}

	err := manager.AddConversation(ctx, conv)
	if err != nil {
		t.Fatalf("Failed to add conversation: %v", err)
	}

	history, err := manager.GetConversationHistory(ctx, "session-1", 10)
	if err != nil {
		t.Fatalf("Failed to get conversation history: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("Expected 1 conversation, got %d", len(history))
	}

	err = manager.ClearConversation(ctx, "session-1")
	if err != nil {
		t.Fatalf("Failed to clear conversation: %v", err)
	}

	history, err = manager.GetConversationHistory(ctx, "session-1", 10)
	if err != nil {
		t.Fatalf("Failed to get conversation history after clear: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("Expected 0 conversations after clear, got %d", len(history))
	}

	// Test case methods
	caseData := &Case{
		ID:       "case-1",
		Title:    "Test Case",
		Problem:  "Test problem",
		Solution: "Test solution",
	}

	err = manager.AddCase(ctx, caseData)
	if err != nil {
		t.Fatalf("Failed to add case: %v", err)
	}

	cases, err := manager.SearchSimilarCases(ctx, "test", 5)
	if err != nil {
		t.Fatalf("Failed to search cases: %v", err)
	}
	if len(cases) != 1 {
		t.Errorf("Expected 1 case, got %d", len(cases))
	}

	// Test storage methods
	testData := map[string]interface{}{"key": "value"}
	err = manager.Store(ctx, "test-key", testData)
	if err != nil {
		t.Fatalf("Failed to store data: %v", err)
	}

	retrieved, err := manager.Retrieve(ctx, "test-key")
	if err != nil {
		t.Fatalf("Failed to retrieve data: %v", err)
	}
	if retrieved == nil {
		t.Error("Expected non-nil retrieved data")
	}

	err = manager.Delete(ctx, "test-key")
	if err != nil {
		t.Fatalf("Failed to delete data: %v", err)
	}

	// Test clear
	err = manager.Clear(ctx)
	if err != nil {
		t.Fatalf("Failed to clear all memory: %v", err)
	}
}

// TestConversationMemoryInterface verifies the ConversationMemory interface
func TestConversationMemoryInterface(t *testing.T) {
	// This test just verifies the interface is defined correctly
	// Actual implementation testing will be done in memory package

	var _ ConversationMemory = (*mockConversationMemory)(nil)
}

// mockConversationMemory is a minimal test implementation
type mockConversationMemory struct {
	conversations map[string][]*Conversation
}

func (m *mockConversationMemory) Add(ctx context.Context, conv *Conversation) error {
	if m.conversations == nil {
		m.conversations = make(map[string][]*Conversation)
	}
	if m.conversations[conv.SessionID] == nil {
		m.conversations[conv.SessionID] = make([]*Conversation, 0)
	}
	m.conversations[conv.SessionID] = append(m.conversations[conv.SessionID], conv)
	return nil
}

func (m *mockConversationMemory) Get(ctx context.Context, sessionID string, limit int) ([]*Conversation, error) {
	convs := m.conversations[sessionID]
	if limit > 0 && len(convs) > limit {
		return convs[:limit], nil
	}
	return convs, nil
}

func (m *mockConversationMemory) Clear(ctx context.Context, sessionID string) error {
	delete(m.conversations, sessionID)
	return nil
}

func (m *mockConversationMemory) Count(ctx context.Context, sessionID string) (int, error) {
	return len(m.conversations[sessionID]), nil
}
