package middleware

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// DynamicPromptMiddleware modifies prompts based on context and state
type DynamicPromptMiddleware struct {
	*BaseMiddleware
	promptModifier func(*MiddlewareRequest) string
}

// NewDynamicPromptMiddleware creates a middleware that modifies prompts dynamically
func NewDynamicPromptMiddleware(modifier func(*MiddlewareRequest) string) *DynamicPromptMiddleware {
	return &DynamicPromptMiddleware{
		BaseMiddleware: NewBaseMiddleware("dynamic-prompt"),
		promptModifier: modifier,
	}
}

// OnBefore modifies the prompt based on the current context
func (m *DynamicPromptMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	if m.promptModifier != nil {
		// Get the current prompt from input
		if prompt, ok := request.Input.(string); ok {
			// Add context from state if available
			contextInfo := ""
			if request.State != nil {
				if userName, ok := request.State.Get("user_name"); ok {
					contextInfo = fmt.Sprintf("\n[Context: User=%v]", userName)
				}
				if history, ok := request.State.Get("conversation_history"); ok {
					contextInfo += fmt.Sprintf("\n[Previous context: %v]", history)
				}
			}

			// Modify the prompt
			modifiedPrompt := m.promptModifier(request)
			if modifiedPrompt == "" {
				modifiedPrompt = prompt
			}
			request.Input = modifiedPrompt + contextInfo

			// Record modification in metadata
			if request.Metadata == nil {
				request.Metadata = make(map[string]interface{})
			}
			request.Metadata["original_prompt"] = prompt
			request.Metadata["modified_prompt"] = request.Input
		}
	}
	return request, nil
}

// ToolSelectorMiddleware filters and selects appropriate tools
type ToolSelectorMiddleware struct {
	*BaseMiddleware
	availableTools []string
	maxTools       int
	selector       func(query string, tools []string) []string
}

// NewToolSelectorMiddleware creates a middleware that selects tools based on the query
func NewToolSelectorMiddleware(tools []string, maxTools int) *ToolSelectorMiddleware {
	return &ToolSelectorMiddleware{
		BaseMiddleware: NewBaseMiddleware("tool-selector"),
		availableTools: tools,
		maxTools:       maxTools,
		selector:       defaultToolSelector,
	}
}

// WithSelector sets a custom tool selection function
func (m *ToolSelectorMiddleware) WithSelector(selector func(query string, tools []string) []string) *ToolSelectorMiddleware {
	m.selector = selector
	return m
}

// OnBefore selects appropriate tools for the request
func (m *ToolSelectorMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	// Extract query from input
	query := ""
	switch v := request.Input.(type) {
	case string:
		query = v
	case map[string]interface{}:
		if q, ok := v["query"].(string); ok {
			query = q
		}
	}

	// Select tools
	selectedTools := m.selector(query, m.availableTools)

	// Limit number of tools
	if len(selectedTools) > m.maxTools {
		selectedTools = selectedTools[:m.maxTools]
	}

	// Add selected tools to metadata
	if request.Metadata == nil {
		request.Metadata = make(map[string]interface{})
	}
	request.Metadata["selected_tools"] = selectedTools
	request.Metadata["tool_count"] = len(selectedTools)

	return request, nil
}

// defaultToolSelector is a simple tool selector based on keywords
func defaultToolSelector(query string, tools []string) []string {
	// Simple keyword-based selection (in real implementation, use NLP)
	selected := []string{}
	for _, tool := range tools {
		// Simplified logic - select tools that might be relevant
		if shouldSelectTool(query, tool) {
			selected = append(selected, tool)
		}
	}
	return selected
}

// shouldSelectTool determines if a tool should be selected for a query
func shouldSelectTool(query string, tool string) bool {
	// Simplified logic - in production, use more sophisticated matching
	keywordMap := map[string][]string{
		"calculator":   {"calculate", "math", "add", "subtract", "multiply", "divide"},
		"search":       {"search", "find", "look", "query"},
		"database":     {"database", "sql", "query", "table"},
		"file_reader":  {"read", "file", "open", "load"},
		"file_writer":  {"write", "save", "create", "store"},
		"web_scraper":  {"scrape", "web", "url", "fetch"},
		"translator":   {"translate", "language", "convert"},
		"code_runner":  {"run", "execute", "code", "script"},
		"image_reader": {"image", "picture", "photo", "visual"},
		"api_caller":   {"api", "endpoint", "request", "call"},
	}

	if keywords, ok := keywordMap[tool]; ok {
		for _, keyword := range keywords {
			// Simple contains check (case-insensitive would be better)
			if containsKeyword(query, keyword) {
				return true
			}
		}
	}

	return false
}

// containsKeyword checks if a query contains a keyword
func containsKeyword(query, keyword string) bool {
	// Simplified - in production, use proper NLP tokenization
	return len(query) > 0 && len(keyword) > 0
}

// RateLimiterMiddleware implements rate limiting
type RateLimiterMiddleware struct {
	*BaseMiddleware
	maxRequests   int
	windowSize    time.Duration
	requestCounts map[string]*rateLimitWindow
	mu            sync.RWMutex
}

// rateLimitWindow tracks requests in a time window
type rateLimitWindow struct {
	count       int
	windowStart time.Time
}

// NewRateLimiterMiddleware creates a rate limiting middleware
func NewRateLimiterMiddleware(maxRequests int, windowSize time.Duration) *RateLimiterMiddleware {
	return &RateLimiterMiddleware{
		BaseMiddleware: NewBaseMiddleware("rate-limiter"),
		maxRequests:    maxRequests,
		windowSize:     windowSize,
		requestCounts:  make(map[string]*rateLimitWindow),
	}
}

// OnBefore checks rate limits
func (m *RateLimiterMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	// Extract user/session ID for rate limiting
	userID := m.getUserID(request)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	window, exists := m.requestCounts[userID]

	if !exists || now.Sub(window.windowStart) > m.windowSize {
		// New window
		m.requestCounts[userID] = &rateLimitWindow{
			count:       1,
			windowStart: now,
		}
	} else {
		// Check rate limit
		if window.count >= m.maxRequests {
			return nil, agentErrors.New(agentErrors.CodeMiddlewareExecution,
				fmt.Sprintf("rate limit exceeded: %d requests in %v", m.maxRequests, m.windowSize)).
				WithComponent("rate-limiter").
				WithOperation("OnBefore").
				WithContext("user_id", userID).
				WithContext("max_requests", m.maxRequests).
				WithContext("window_size", m.windowSize)
		}
		window.count++
	}

	// Add rate limit info to metadata
	if request.Metadata == nil {
		request.Metadata = make(map[string]interface{})
	}
	request.Metadata["rate_limit_remaining"] = m.maxRequests - m.requestCounts[userID].count
	request.Metadata["rate_limit_reset"] = m.requestCounts[userID].windowStart.Add(m.windowSize)

	return request, nil
}

// getUserID extracts user ID from request
func (m *RateLimiterMiddleware) getUserID(request *MiddlewareRequest) string {
	// Try to get from state
	if request.State != nil {
		if userID, ok := request.State.Get("user_id"); ok {
			return fmt.Sprintf("%v", userID)
		}
	}

	// Try to get from metadata
	if request.Metadata != nil {
		if userID, ok := request.Metadata["user_id"]; ok {
			return fmt.Sprintf("%v", userID)
		}
	}

	// Default to a generic ID
	return "default"
}

// Reset clears all rate limit windows
func (m *RateLimiterMiddleware) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestCounts = make(map[string]*rateLimitWindow)
}

// AuthenticationMiddleware handles authentication
type AuthenticationMiddleware struct {
	*BaseMiddleware
	authFunc func(context.Context, *MiddlewareRequest) (bool, error)
}

// NewAuthenticationMiddleware creates an authentication middleware
func NewAuthenticationMiddleware(authFunc func(context.Context, *MiddlewareRequest) (bool, error)) *AuthenticationMiddleware {
	return &AuthenticationMiddleware{
		BaseMiddleware: NewBaseMiddleware("authentication"),
		authFunc:       authFunc,
	}
}

// OnBefore checks authentication
func (m *AuthenticationMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	if m.authFunc != nil {
		authenticated, err := m.authFunc(ctx, request)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeMiddlewareExecution, "authentication error").
				WithComponent("authentication").
				WithOperation("OnBefore")
		}
		if !authenticated {
			return nil, agentErrors.New(agentErrors.CodeMiddlewareExecution, "authentication failed: unauthorized").
				WithComponent("authentication").
				WithOperation("OnBefore")
		}

		// Mark as authenticated in metadata
		if request.Metadata == nil {
			request.Metadata = make(map[string]interface{})
		}
		request.Metadata["authenticated"] = true
	}
	return request, nil
}

// ValidationMiddleware validates requests
type ValidationMiddleware struct {
	*BaseMiddleware
	validators []func(*MiddlewareRequest) error
}

// NewValidationMiddleware creates a validation middleware
func NewValidationMiddleware(validators ...func(*MiddlewareRequest) error) *ValidationMiddleware {
	return &ValidationMiddleware{
		BaseMiddleware: NewBaseMiddleware("validation"),
		validators:     validators,
	}
}

// OnBefore validates the request
func (m *ValidationMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	for i, validator := range m.validators {
		if err := validator(request); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeMiddlewareExecution, "validation failed").
				WithComponent("validation").
				WithOperation("OnBefore").
				WithContext("validator_index", i)
		}
	}

	// Mark as validated
	if request.Metadata == nil {
		request.Metadata = make(map[string]interface{})
	}
	request.Metadata["validated"] = true

	return request, nil
}

// TransformMiddleware transforms input/output
type TransformMiddleware struct {
	*BaseMiddleware
	inputTransform  func(interface{}) (interface{}, error)
	outputTransform func(interface{}) (interface{}, error)
}

// NewTransformMiddleware creates a transform middleware
func NewTransformMiddleware(
	inputTransform func(interface{}) (interface{}, error),
	outputTransform func(interface{}) (interface{}, error),
) *TransformMiddleware {
	return &TransformMiddleware{
		BaseMiddleware:  NewBaseMiddleware("transform"),
		inputTransform:  inputTransform,
		outputTransform: outputTransform,
	}
}

// OnBefore transforms the input
func (m *TransformMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	if m.inputTransform != nil {
		transformed, err := m.inputTransform(request.Input)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeMiddlewareExecution, "input transform failed").
				WithComponent("transform").
				WithOperation("OnBefore")
		}
		request.Input = transformed
	}
	return request, nil
}

// OnAfter transforms the output
func (m *TransformMiddleware) OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error) {
	if m.outputTransform != nil && response.Error == nil {
		transformed, err := m.outputTransform(response.Output)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeMiddlewareExecution, "output transform failed").
				WithComponent("transform").
				WithOperation("OnAfter")
		}
		response.Output = transformed
	}
	return response, nil
}

// CircuitBreakerMiddleware implements circuit breaker pattern
type CircuitBreakerMiddleware struct {
	*BaseMiddleware
	maxFailures     int
	resetTimeout    time.Duration
	failureCount    int
	lastFailureTime time.Time
	state           string // "closed", "open", "half-open"
	mu              sync.RWMutex
}

// NewCircuitBreakerMiddleware creates a circuit breaker middleware
func NewCircuitBreakerMiddleware(maxFailures int, resetTimeout time.Duration) *CircuitBreakerMiddleware {
	return &CircuitBreakerMiddleware{
		BaseMiddleware: NewBaseMiddleware("circuit-breaker"),
		maxFailures:    maxFailures,
		resetTimeout:   resetTimeout,
		state:          "closed",
	}
}

// OnBefore checks if circuit is open
func (m *CircuitBreakerMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	m.mu.RLock()
	state := m.state
	lastFailure := m.lastFailureTime
	m.mu.RUnlock()

	// Check circuit state
	switch state {
	case "open":
		// Check if we should transition to half-open
		if time.Since(lastFailure) > m.resetTimeout {
			m.mu.Lock()
			m.state = "half-open"
			m.mu.Unlock()
		} else {
			return nil, agentErrors.New(agentErrors.CodeMiddlewareExecution, "circuit breaker is open").
				WithComponent("circuit-breaker").
				WithOperation("OnBefore").
				WithContext("state", state).
				WithContext("last_failure", lastFailure).
				WithContext("reset_timeout", m.resetTimeout)
		}
	case "half-open":
		// Allow request to proceed (testing)
	case "closed":
		// Normal operation
	}

	return request, nil
}

// OnError handles errors and updates circuit state
func (m *CircuitBreakerMiddleware) OnError(ctx context.Context, err error) error {
	if err != nil {
		m.mu.Lock()
		defer m.mu.Unlock()

		m.failureCount++
		m.lastFailureTime = time.Now()

		if m.state == "half-open" {
			// Failed in half-open state, go back to open
			m.state = "open"
			m.failureCount = m.maxFailures
		} else if m.failureCount >= m.maxFailures {
			// Too many failures, open the circuit
			m.state = "open"
		}
	}

	return err
}

// OnAfter handles successful responses
func (m *CircuitBreakerMiddleware) OnAfter(ctx context.Context, response *MiddlewareResponse) (*MiddlewareResponse, error) {
	if response.Error == nil {
		m.mu.Lock()
		if m.state == "half-open" {
			// Success in half-open state, close the circuit
			m.state = "closed"
			m.failureCount = 0
		}
		m.mu.Unlock()
	}

	return response, nil
}

// RandomDelayMiddleware adds random delay (for testing/simulation)
type RandomDelayMiddleware struct {
	*BaseMiddleware
	minDelay time.Duration
	maxDelay time.Duration
}

// NewRandomDelayMiddleware creates a random delay middleware
func NewRandomDelayMiddleware(minDelay, maxDelay time.Duration) *RandomDelayMiddleware {
	return &RandomDelayMiddleware{
		BaseMiddleware: NewBaseMiddleware("random-delay"),
		minDelay:       minDelay,
		maxDelay:       maxDelay,
	}
}

// OnBefore adds random delay
func (m *RandomDelayMiddleware) OnBefore(ctx context.Context, request *MiddlewareRequest) (*MiddlewareRequest, error) {
	delayRange := int64(m.maxDelay - m.minDelay)
	if delayRange > 0 {
		// Use crypto/rand for secure random number generation
		n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(delayRange))
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeMiddlewareExecution, "failed to generate random delay").
				WithComponent("random-delay").
				WithOperation("OnBefore").
				WithContext("min_delay", m.minDelay).
				WithContext("max_delay", m.maxDelay)
		}
		delay := m.minDelay + time.Duration(n.Int64())
		select {
		case <-time.After(delay):
			// Delay completed
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return request, nil
}
