package practical

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// APICallerTool makes HTTP API calls with authentication and retry logic
type APICallerTool struct {
	httpClient     *http.Client
	defaultHeaders map[string]string
	maxRetries     int
	rateLimiter    *RateLimiter
	responseCache  *ResponseCache
}

// NewAPICallerTool creates a new API caller tool
func NewAPICallerTool() *APICallerTool {
	return &APICallerTool{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		defaultHeaders: map[string]string{
			"User-Agent": "AgentFramework/1.0",
		},
		maxRetries:    3,
		rateLimiter:   NewRateLimiter(100, time.Minute),
		responseCache: NewResponseCache(100, 5*time.Minute),
	}
}

// Name returns the tool name
func (t *APICallerTool) Name() string {
	return "api_caller"
}

// Description returns the tool description
func (t *APICallerTool) Description() string {
	return "Makes HTTP API calls with support for various authentication methods, retries, and rate limiting"
}

// ArgsSchema returns the arguments schema as a JSON string
func (t *APICallerTool) ArgsSchema() string {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The API endpoint URL",
			},
			"method": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
				"default":     "GET",
				"description": "HTTP method",
			},
			"headers": map[string]interface{}{
				"type":        "object",
				"description": "Additional HTTP headers",
			},
			"params": map[string]interface{}{
				"type":        "object",
				"description": "URL query parameters",
			},
			"body": map[string]interface{}{
				"type":        []interface{}{"object", "string", "null"},
				"description": "Request body (will be JSON encoded if object)",
			},
			"auth": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"basic", "bearer", "api_key", "oauth2"},
						"description": "Authentication type",
					},
					"credentials": map[string]interface{}{
						"type":        "object",
						"description": "Authentication credentials",
					},
				},
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Request timeout in seconds",
				"default":     30,
			},
			"retry": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"max_attempts": map[string]interface{}{
						"type":    "integer",
						"default": 3,
					},
					"backoff": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"constant", "linear", "exponential"},
						"default":     "exponential",
						"description": "Retry backoff strategy",
					},
				},
			},
			"cache": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to cache GET responses",
				"default":     false,
			},
			"follow_redirects": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to follow HTTP redirects",
				"default":     true,
			},
		},
		"required": []string{"url"},
	}

	schemaJSON, _ := json.Marshal(schema)
	return string(schemaJSON)
}

// OutputSchema returns the output schema

// Execute makes the API call
func (t *APICallerTool) Execute(ctx context.Context, input *tools.ToolInput) (*interfaces.ToolOutput, error) {
	params, err := t.parseAPIInput(input.Args)
	if err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	// Check rate limit
	if !t.rateLimiter.Allow() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	// Check cache for GET requests
	if params.Method == "GET" && params.Cache {
		cacheKey := t.getCacheKey(params)
		if cached := t.responseCache.Get(cacheKey); cached != nil {
			result := cached.(map[string]interface{})
			result["cached"] = true
			return &interfaces.ToolOutput{
				Result: result,
			}, nil
		}
	}

	// Execute with retry
	var response map[string]interface{}
	var lastErr error
	attempts := 0

	for i := 0; i < params.Retry.MaxAttempts; i++ {
		attempts++
		response, lastErr = t.executeRequest(ctx, params)
		if lastErr == nil {
			break
		}

		// Check if error is retryable
		if !t.isRetryableError(lastErr) {
			break
		}

		// Apply backoff
		backoff := t.calculateBackoff(i, params.Retry.Backoff)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			// Continue to next attempt
		}
	}

	if lastErr != nil {
		return &interfaces.ToolOutput{
			Result: map[string]interface{}{
				"error":    lastErr.Error(),
				"attempts": attempts,
			},
			Error: lastErr.Error(),
		}, lastErr
	}

	response["attempts"] = attempts
	response["cached"] = false

	// Cache successful GET responses
	if params.Method == "GET" && params.Cache {
		cacheKey := t.getCacheKey(params)
		t.responseCache.Set(cacheKey, response)
	}

	return &interfaces.ToolOutput{
		Result: response,
	}, nil
}

// Implement Runnable interface
func (t *APICallerTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return t.Execute(ctx, input)
}

func (t *APICallerTool) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan agentcore.StreamChunk[*interfaces.ToolOutput], error) {
	ch := make(chan agentcore.StreamChunk[*interfaces.ToolOutput])
	go func() {
		defer close(ch)
		output, err := t.Execute(ctx, input)
		if err != nil {
			ch <- agentcore.StreamChunk[*interfaces.ToolOutput]{Error: err}
		} else {
			ch <- agentcore.StreamChunk[*interfaces.ToolOutput]{Data: output}
		}
	}()
	return ch, nil
}

func (t *APICallerTool) Batch(ctx context.Context, inputs []*tools.ToolInput) ([]*interfaces.ToolOutput, error) {
	outputs := make([]*interfaces.ToolOutput, len(inputs))
	for i, input := range inputs {
		output, err := t.Execute(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

func (t *APICallerTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*tools.ToolInput, any] {
	return nil
}

func (t *APICallerTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*tools.ToolInput, *interfaces.ToolOutput] {
	return t
}

func (t *APICallerTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// executeRequest executes a single HTTP request
func (t *APICallerTool) executeRequest(ctx context.Context, params *apiParams) (map[string]interface{}, error) {
	startTime := time.Now()

	// Build URL with query parameters
	fullURL := params.URL
	if len(params.Params) > 0 {
		values := url.Values{}
		for k, v := range params.Params {
			values.Set(k, fmt.Sprint(v))
		}
		fullURL = fmt.Sprintf("%s?%s", params.URL, values.Encode())
	}

	// Prepare request body
	var bodyReader io.Reader
	if params.Body != nil {
		switch v := params.Body.(type) {
		case string:
			bodyReader = strings.NewReader(v)
		case map[string]interface{}:
			data, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal body: %w", err)
			}
			bodyReader = bytes.NewReader(data)
		}
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, params.Method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	for k, v := range t.defaultHeaders {
		req.Header.Set(k, v)
	}
	for k, v := range params.Headers {
		req.Header.Set(k, v)
	}

	// Set authentication
	if err := t.setAuthentication(req, params.Auth); err != nil {
		return nil, fmt.Errorf("authentication error: %w", err)
	}

	// Set content type for JSON body
	if bodyReader != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Configure redirect policy
	client := t.httpClient
	if !params.FollowRedirects {
		client = &http.Client{
			Timeout:   t.httpClient.Timeout,
			Transport: t.httpClient.Transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	result := map[string]interface{}{
		"status_code": resp.StatusCode,
		"headers":     t.headersToMap(resp.Header),
		"latency_ms":  int(time.Since(startTime).Milliseconds()),
	}

	// Try to parse as JSON
	var jsonBody interface{}
	if err := json.Unmarshal(bodyBytes, &jsonBody); err == nil {
		result["body"] = jsonBody
	} else {
		// Return as string if not JSON
		result["body"] = string(bodyBytes)
	}

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return result, nil
}

// setAuthentication sets authentication headers
func (t *APICallerTool) setAuthentication(req *http.Request, auth *authConfig) error {
	if auth == nil {
		return nil
	}

	switch auth.Type {
	case "bearer":
		token, ok := auth.Credentials["token"].(string)
		if !ok {
			return fmt.Errorf("bearer auth requires 'token' credential")
		}
		req.Header.Set("Authorization", "Bearer "+token)

	case "basic":
		username, _ := auth.Credentials["username"].(string)
		password, _ := auth.Credentials["password"].(string)
		req.SetBasicAuth(username, password)

	case "api_key":
		key, ok := auth.Credentials["key"].(string)
		if !ok {
			return fmt.Errorf("api_key auth requires 'key' credential")
		}
		location, _ := auth.Credentials["location"].(string)
		name, _ := auth.Credentials["name"].(string)

		if location == "query" {
			// Add to URL query parameters
			u, _ := url.Parse(req.URL.String())
			q := u.Query()
			q.Set(name, key)
			u.RawQuery = q.Encode()
			req.URL = u
		} else {
			// Default to header
			if name == "" {
				name = "X-API-Key"
			}
			req.Header.Set(name, key)
		}

	case "oauth2":
		token, ok := auth.Credentials["access_token"].(string)
		if !ok {
			return fmt.Errorf("oauth2 auth requires 'access_token' credential")
		}
		req.Header.Set("Authorization", "Bearer "+token)

	default:
		return fmt.Errorf("unsupported auth type: %s", auth.Type)
	}

	return nil
}

// headersToMap converts http.Header to map
func (t *APICallerTool) headersToMap(headers http.Header) map[string]string {
	result := make(map[string]string)
	for k, v := range headers {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result
}

// getCacheKey generates a cache key for the request
func (t *APICallerTool) getCacheKey(params *apiParams) string {
	key := params.URL
	if len(params.Params) > 0 {
		data, _ := json.Marshal(params.Params)
		key += string(data)
	}
	return key
}

// isRetryableError checks if an error is retryable
func (t *APICallerTool) isRetryableError(err error) bool {
	// Check for specific HTTP status codes in error message
	errStr := err.Error()
	retryableCodes := []string{"429", "500", "502", "503", "504"}
	for _, code := range retryableCodes {
		if strings.Contains(errStr, "HTTP "+code) {
			return true
		}
	}

	// Check for network errors
	networkErrors := []string{"timeout", "connection refused", "connection reset"}
	for _, netErr := range networkErrors {
		if strings.Contains(strings.ToLower(errStr), netErr) {
			return true
		}
	}

	return false
}

// calculateBackoff calculates retry backoff duration
func (t *APICallerTool) calculateBackoff(attempt int, strategy string) time.Duration {
	base := time.Second

	switch strategy {
	case "constant":
		return base
	case "linear":
		return base * time.Duration(attempt+1)
	case "exponential":
		// Cap attempt to prevent overflow (2^31 is safe for uint)
		safeAttempt := attempt
		if safeAttempt > 31 {
			safeAttempt = 31
		}
		if safeAttempt < 0 {
			safeAttempt = 0
		}
		return base * (1 << uint(safeAttempt))
	default:
		return base
	}
}

// parseAPIInput parses the tool input
func (t *APICallerTool) parseAPIInput(input interface{}) (*apiParams, error) {
	var params apiParams

	switch v := input.(type) {
	case string:
		// Simple GET request
		params.URL = v
		params.Method = "GET"
	case map[string]interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &params); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported input type: %T", input)
	}

	// Set defaults
	if params.Method == "" {
		params.Method = "GET"
	}
	if params.Timeout == 0 {
		params.Timeout = 30
	}
	if params.Retry.MaxAttempts == 0 {
		params.Retry.MaxAttempts = t.maxRetries
	}
	if params.Retry.Backoff == "" {
		params.Retry.Backoff = "exponential"
	}
	if params.Headers == nil {
		params.Headers = make(map[string]string)
	}
	if params.Params == nil {
		params.Params = make(map[string]interface{})
	}

	// Default to follow redirects
	params.FollowRedirects = true

	return &params, nil
}

type apiParams struct {
	URL             string                 `json:"url"`
	Method          string                 `json:"method"`
	Headers         map[string]string      `json:"headers"`
	Params          map[string]interface{} `json:"params"`
	Body            interface{}            `json:"body"`
	Auth            *authConfig            `json:"auth"`
	Timeout         int                    `json:"timeout"`
	Retry           retryConfig            `json:"retry"`
	Cache           bool                   `json:"cache"`
	FollowRedirects bool                   `json:"follow_redirects"`
}

type authConfig struct {
	Type        string                 `json:"type"`
	Credentials map[string]interface{} `json:"credentials"`
}

type retryConfig struct {
	MaxAttempts int    `json:"max_attempts"`
	Backoff     string `json:"backoff"`
}

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	tokens   int
	max      int
	interval time.Duration
	lastFill time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(max int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:   max,
		max:      max,
		interval: interval,
		lastFill: time.Now(),
	}
}

// Allow checks if a request is allowed
func (r *RateLimiter) Allow() bool {
	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(r.lastFill)
	tokensToAdd := int(elapsed / r.interval * time.Duration(r.max))

	if tokensToAdd > 0 {
		r.tokens = min(r.max, r.tokens+tokensToAdd)
		r.lastFill = now
	}

	if r.tokens > 0 {
		r.tokens--
		return true
	}

	return false
}

// ResponseCache implements a simple LRU cache
type ResponseCache struct {
	entries  map[string]*cacheEntry
	maxSize  int
	ttl      time.Duration
	eviction []string
}

type cacheEntry struct {
	value     interface{}
	timestamp time.Time
}

// NewResponseCache creates a new response cache
func NewResponseCache(maxSize int, ttl time.Duration) *ResponseCache {
	return &ResponseCache{
		entries:  make(map[string]*cacheEntry),
		maxSize:  maxSize,
		ttl:      ttl,
		eviction: make([]string, 0, maxSize),
	}
}

// Get retrieves a cached value
func (c *ResponseCache) Get(key string) interface{} {
	entry, exists := c.entries[key]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Since(entry.timestamp) > c.ttl {
		delete(c.entries, key)
		return nil
	}

	return entry.value
}

// Set stores a value in cache
func (c *ResponseCache) Set(key string, value interface{}) {
	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize && c.entries[key] == nil {
		oldest := c.eviction[0]
		delete(c.entries, oldest)
		c.eviction = c.eviction[1:]
	}

	c.entries[key] = &cacheEntry{
		value:     value,
		timestamp: time.Now(),
	}

	// Track for eviction
	if c.entries[key] != nil {
		c.eviction = append(c.eviction, key)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// APICallerRuntimeTool extends APICallerTool with runtime support
type APICallerRuntimeTool struct {
	*APICallerTool
}

// NewAPICallerRuntimeTool creates a runtime-aware API caller
func NewAPICallerRuntimeTool() *APICallerRuntimeTool {
	return &APICallerRuntimeTool{
		APICallerTool: NewAPICallerTool(),
	}
}

// ExecuteWithRuntime executes with runtime support
func (t *APICallerRuntimeTool) ExecuteWithRuntime(ctx context.Context, input *interfaces.ToolInput, runtime *tools.ToolRuntime) (*interfaces.ToolOutput, error) {
	// Stream status
	if runtime != nil && runtime.StreamWriter != nil {
		runtime.StreamWriter(map[string]interface{}{
			"status": "calling_api",
			"tool":   t.Name(),
		})
	}

	// Get stored API keys from runtime
	if runtime != nil {
		params, _ := t.parseAPIInput(input.Args)
		if params != nil && params.Auth != nil && params.Auth.Type == "api_key" {
			// Try to get API key from runtime state
			if key, err := runtime.GetState("api_key_" + params.URL); err == nil {
				params.Auth.Credentials["key"] = key
			}
		}
	}

	// Execute the API call
	result, err := t.Execute(ctx, input)

	// Store successful results in runtime
	if err == nil && runtime != nil {
		params, _ := t.parseAPIInput(input.Args)
		if params != nil {
			// Store last successful response
			runtime.PutToStore([]string{"api_responses"}, params.URL, result)
		}
	}

	// Stream completion
	if runtime != nil && runtime.StreamWriter != nil {
		runtime.StreamWriter(map[string]interface{}{
			"status": "completed",
			"tool":   t.Name(),
			"error":  err,
		})
	}

	return result, err
}
