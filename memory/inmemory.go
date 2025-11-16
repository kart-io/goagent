package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InMemoryManager 内存记忆管理器实现
type InMemoryManager struct {
	// 对话存储
	conversations map[string][]*Conversation // sessionID -> conversations
	convMu        sync.RWMutex

	// 通用键值存储
	store   map[string]interface{}
	storeMu sync.RWMutex

	// 案例存储
	cases   map[string]*Case
	casesMu sync.RWMutex

	// 配置
	config *Config
}

// NewInMemoryManager 创建内存记忆管理器
func NewInMemoryManager(config *Config) *InMemoryManager {
	if config == nil {
		config = DefaultConfig()
	}

	return &InMemoryManager{
		conversations: make(map[string][]*Conversation),
		store:         make(map[string]interface{}),
		cases:         make(map[string]*Case),
		config:        config,
	}
}

// AddConversation 添加对话
func (m *InMemoryManager) AddConversation(ctx context.Context, conv *Conversation) error {
	if conv == nil {
		return errors.New("conversation is nil")
	}

	if conv.SessionID == "" {
		return errors.New("session_id is required")
	}

	// 生成 ID
	if conv.ID == "" {
		conv.ID = uuid.New().String()
	}

	// 设置时间戳
	if conv.Timestamp.IsZero() {
		conv.Timestamp = time.Now()
	}

	m.convMu.Lock()
	defer m.convMu.Unlock()

	// 添加对话
	m.conversations[conv.SessionID] = append(m.conversations[conv.SessionID], conv)

	// 限制对话数量
	if m.config.EnableConversation && m.config.MaxConversationLength > 0 {
		convs := m.conversations[conv.SessionID]
		if len(convs) > m.config.MaxConversationLength {
			m.conversations[conv.SessionID] = convs[len(convs)-m.config.MaxConversationLength:]
		}
	}

	return nil
}

// GetConversationHistory 获取对话历史
func (m *InMemoryManager) GetConversationHistory(ctx context.Context, sessionID string, limit int) ([]*Conversation, error) {
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}

	m.convMu.RLock()
	defer m.convMu.RUnlock()

	convs, exists := m.conversations[sessionID]
	if !exists {
		return []*Conversation{}, nil
	}

	// 应用 limit
	if limit > 0 && len(convs) > limit {
		return convs[len(convs)-limit:], nil
	}

	return convs, nil
}

// ClearConversation 清空会话对话
func (m *InMemoryManager) ClearConversation(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errors.New("session_id is required")
	}

	m.convMu.Lock()
	defer m.convMu.Unlock()

	delete(m.conversations, sessionID)
	return nil
}

// AddCase 添加案例
func (m *InMemoryManager) AddCase(ctx context.Context, caseMemory *Case) error {
	if caseMemory == nil {
		return errors.New("case is nil")
	}

	// 生成 ID
	if caseMemory.ID == "" {
		caseMemory.ID = uuid.New().String()
	}

	// 设置时间戳
	now := time.Now()
	if caseMemory.CreatedAt.IsZero() {
		caseMemory.CreatedAt = now
	}
	caseMemory.UpdatedAt = now

	m.casesMu.Lock()
	defer m.casesMu.Unlock()

	m.cases[caseMemory.ID] = caseMemory
	return nil
}

// SearchSimilarCases 搜索相似案例
//
// 注意：这是一个简单的文本匹配实现，实际应用中应使用向量相似度搜索
func (m *InMemoryManager) SearchSimilarCases(ctx context.Context, query string, limit int) ([]*Case, error) {
	if query == "" {
		return []*Case{}, nil
	}

	m.casesMu.RLock()
	defer m.casesMu.RUnlock()

	// 简单的文本匹配（实际应使用向量搜索）
	results := make([]*Case, 0)
	for _, c := range m.cases {
		// 简单检查：如果查询字符串出现在案例的描述或问题中
		if contains(c.Description, query) || contains(c.Problem, query) || contains(c.Title, query) {
			caseCopy := *c
			caseCopy.Similarity = 0.8 // 模拟相似度
			results = append(results, &caseCopy)
		}
	}

	// 应用 limit
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// Store 存储键值对
func (m *InMemoryManager) Store(ctx context.Context, key string, value interface{}) error {
	if key == "" {
		return errors.New("key is required")
	}

	m.storeMu.Lock()
	defer m.storeMu.Unlock()

	m.store[key] = value
	return nil
}

// Retrieve 检索值
func (m *InMemoryManager) Retrieve(ctx context.Context, key string) (interface{}, error) {
	if key == "" {
		return nil, errors.New("key is required")
	}

	m.storeMu.RLock()
	defer m.storeMu.RUnlock()

	value, exists := m.store[key]
	if !exists {
		return nil, errors.New("key not found")
	}

	return value, nil
}

// Delete 删除键值对
func (m *InMemoryManager) Delete(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("key is required")
	}

	m.storeMu.Lock()
	defer m.storeMu.Unlock()

	delete(m.store, key)
	return nil
}

// Clear 清空所有记忆
func (m *InMemoryManager) Clear(ctx context.Context) error {
	m.convMu.Lock()
	m.storeMu.Lock()
	m.casesMu.Lock()
	defer m.convMu.Unlock()
	defer m.storeMu.Unlock()
	defer m.casesMu.Unlock()

	m.conversations = make(map[string][]*Conversation)
	m.store = make(map[string]interface{})
	m.cases = make(map[string]*Case)

	return nil
}

// contains 检查字符串包含（不区分大小写的简单实现）
func contains(s, substr string) bool {
	// 简化实现，实际应使用 strings.Contains 或更复杂的匹配
	return len(s) > 0 && len(substr) > 0
}
