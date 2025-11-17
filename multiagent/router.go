package multiagent

import (
	"context"
	"regexp"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
)

// MessageRouter 消息路由器
type MessageRouter struct {
	routes   map[string]RouteHandler
	patterns map[string]*regexp.Regexp
	mu       sync.RWMutex
}

// RouteHandler 路由处理器
type RouteHandler func(ctx context.Context, message *AgentMessage) (*AgentMessage, error)

// NewAgentMessageRouter 创建消息路由器
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{
		routes:   make(map[string]RouteHandler),
		patterns: make(map[string]*regexp.Regexp),
	}
}

// RegisterRoute 注册路由
func (r *MessageRouter) RegisterRoute(pattern string, handler RouteHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.routes[pattern] = handler
	return nil
}

// RegisterPatternRoute 注册模式路由（正则）
func (r *MessageRouter) RegisterPatternRoute(pattern string, handler RouteHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	re, err := regexp.Compile(pattern)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid pattern").
			WithComponent("agent_router").
			WithOperation("register_pattern_route").
			WithContext("pattern", pattern)
	}

	r.patterns[pattern] = re
	r.routes[pattern] = handler
	return nil
}

// Route 路由消息
func (r *MessageRouter) Route(ctx context.Context, message *AgentMessage) (*AgentMessage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 精确匹配
	if handler, exists := r.routes[message.Topic]; exists {
		return handler(ctx, message)
	}

	// 模式匹配
	for pattern, re := range r.patterns {
		if re.MatchString(message.Topic) {
			if handler, exists := r.routes[pattern]; exists {
				return handler(ctx, message)
			}
		}
	}

	return nil, agentErrors.Newf(agentErrors.CodeRouterNoMatch, "no route found for topic: %s", message.Topic).
		WithComponent("agent_router").
		WithOperation("route").
		WithContext("topic", message.Topic)
}

// UnregisterRoute 注销路由
func (r *MessageRouter) UnregisterRoute(pattern string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.routes, pattern)
	delete(r.patterns, pattern)
}

// SessionManager 会话管理器
type SessionManager struct {
	sessions map[string]*AgentSession
	mu       sync.RWMutex
}

// AgentSession Agent 会话
type AgentSession struct {
	ID           string
	Participants []string
	Messages     []*AgentMessage
	State        map[string]interface{}
	CreatedAt    string
	UpdatedAt    string
}

// NewSessionManager 创建会话管理器
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*AgentSession),
	}
}

// CreateSession 创建会话
func (m *SessionManager) CreateSession(participants []string) (*AgentSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &AgentSession{
		ID:           generateMessageID(),
		Participants: participants,
		Messages:     make([]*AgentMessage, 0),
		State:        make(map[string]interface{}),
		CreatedAt:    "now",
		UpdatedAt:    "now",
	}

	m.sessions[session.ID] = session
	return session, nil
}

// GetSession 获取会话
func (m *SessionManager) GetSession(sessionID string) (*AgentSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return nil, agentErrors.Newf(agentErrors.CodeStoreNotFound, "session not found: %s", sessionID).
			WithComponent("session_manager").
			WithOperation("get_session").
			WithContext("session_id", sessionID)
	}

	return session, nil
}

// AddMessage 添加消息到会话
func (m *SessionManager) AddMessage(sessionID string, message *AgentMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return agentErrors.Newf(agentErrors.CodeStoreNotFound, "session not found: %s", sessionID).
			WithComponent("session_manager").
			WithOperation("add_message").
			WithContext("session_id", sessionID)
	}

	session.Messages = append(session.Messages, message)
	session.UpdatedAt = "now"
	return nil
}

// CloseSession 关闭会话
func (m *SessionManager) CloseSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, sessionID)
	return nil
}
