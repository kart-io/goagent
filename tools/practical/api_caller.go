package practical

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// APICallerTool makes HTTP API calls with authentication and retry logic
type APICallerTool struct {
	client         *resty.Client
	defaultHeaders map[string]string
	maxRetries     int
	rateLimiter    *RateLimiter
	responseCache  *ResponseCache
}

// NewAPICallerTool creates a new API caller tool
func NewAPICallerTool() *APICallerTool {
	client := resty.New().
		SetTimeout(30 * time.Second).
		SetHeader("User-Agent", "AgentFramework/1.0").
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(3 * time.Second).
		AddRetryCondition(func(r *resty.Response, err error) bool {
			// Retry on network errors or 5xx status codes
			if err != nil {
				return true
			}
			return r.StatusCode() >= 500
		})

	return &APICallerTool{
		client: client,
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
				"enum":        []string{interfaces.MethodGet, interfaces.MethodPost, interfaces.MethodPut, interfaces.MethodDelete, interfaces.MethodPatch},
				"default":     interfaces.MethodGet,
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
func (t *APICallerTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	params, err := t.parseAPIInput(input.Args)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid input").
			WithComponent("api_caller_tool").
			WithOperation("execute")
	}

	// Check rate limit
	if !t.rateLimiter.Allow() {
		return nil, agentErrors.New(agentErrors.CodeToolExecution, "rate limit exceeded").
			WithComponent("api_caller_tool").
			WithOperation("execute").
			WithContext("url", params.URL)
	}

	// Check cache for GET requests
	if params.Method == interfaces.MethodGet && params.Cache {
		cacheKey := t.getCacheKey(params)
		if cached := t.responseCache.Get(cacheKey); cached != nil {
			result := cached.(map[string]interface{})
			result["cached"] = true
			return &interfaces.ToolOutput{
				Result: result,
			}, nil
		}
	}

	// Execute request (retries are handled by resty)
	response, err := t.executeRequest(ctx, params)

	if err != nil {
		attempts := 1
		if response != nil {
			if a, ok := response["attempts"].(int); ok {
				attempts = a
			}
		}
		return &interfaces.ToolOutput{
			Result: map[string]interface{}{
				"error":    err.Error(),
				"attempts": attempts,
			},
			Error: err.Error(),
		}, err
	}

	response["cached"] = false

	// Cache successful GET responses
	if params.Method == interfaces.MethodGet && params.Cache {
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

func (t *APICallerTool) Batch(ctx context.Context, inputs []*interfaces.ToolInput) ([]*interfaces.ToolOutput, error) {
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

func (t *APICallerTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	return nil
}

func (t *APICallerTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
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
	queryParams := make(map[string]string)
	for k, v := range params.Params {
		queryParams[k] = fmt.Sprint(v)
	}

	// Track retry attempts using AfterResponse hook
	attemptCount := 0

	// Execute request
	var resp *resty.Response
	var err error

	// Handle redirect policy and retry configuration
	client := t.client
	if !params.FollowRedirects || params.Retry.MaxAttempts != t.maxRetries {
		// Create a new client with custom settings
		client = resty.New().
			SetTimeout(t.client.GetClient().Timeout)

		if !params.FollowRedirects {
			client.SetRedirectPolicy(resty.NoRedirectPolicy())
		}

		// Configure retries based on params
		client.SetRetryCount(params.Retry.MaxAttempts - 1) // -1 because resty counts retries, not total attempts

		// Set backoff strategy
		switch params.Retry.Backoff {
		case "constant":
			client.SetRetryWaitTime(1 * time.Second).
				SetRetryMaxWaitTime(1 * time.Second)
		case "linear":
			client.SetRetryWaitTime(1 * time.Second).
				SetRetryMaxWaitTime(time.Duration(params.Retry.MaxAttempts) * time.Second)
		case "exponential":
			client.SetRetryWaitTime(1 * time.Second).
				SetRetryMaxWaitTime(8 * time.Second)
		}

		client.AddRetryCondition(func(r *resty.Response, e error) bool {
			if e != nil {
				return true
			}
			return r.StatusCode() >= 500
		})
	}

	// Add hook to track attempts
	client.OnAfterResponse(func(c *resty.Client, r *resty.Response) error {
		attemptCount++
		return nil
	})

	// Create request
	req := client.R().
		SetContext(ctx).
		SetQueryParams(queryParams).
		SetHeaders(params.Headers)

	// Set default headers
	for k, v := range t.defaultHeaders {
		if _, exists := params.Headers[k]; !exists {
			req.SetHeader(k, v)
		}
	}

	// Set authentication
	if err := t.setAuthenticationResty(req, params.Auth); err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "authentication error").
			WithComponent("api_caller_tool").
			WithOperation("executeRequest").
			WithContext("url", params.URL).
			WithContext("auth_type", params.Auth.Type)
	}

	// Set body if provided
	if params.Body != nil {
		req.SetBody(params.Body)
		if req.Header.Get(interfaces.HeaderContentType) == "" {
			req.SetHeader(interfaces.HeaderContentType, interfaces.ContentTypeJSON)
		}
	}

	switch params.Method {
	case interfaces.MethodGet:
		resp, err = req.Get(fullURL)
	case interfaces.MethodPost:
		resp, err = req.Post(fullURL)
	case interfaces.MethodPut:
		resp, err = req.Put(fullURL)
	case interfaces.MethodDelete:
		resp, err = req.Delete(fullURL)
	case interfaces.MethodPatch:
		resp, err = req.Patch(fullURL)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported HTTP method").
			WithComponent("api_caller_tool").
			WithOperation("executeRequest").
			WithContext("method", params.Method)
	}

	// If attemptCount is still 0 (hook didn't fire), set it to 1
	if attemptCount == 0 {
		attemptCount = 1
	}

	if err != nil {
		// Return result with attempt count even on error
		return map[string]interface{}{
			"attempts": attemptCount,
		}, err
	}

	// Parse response
	result := map[string]interface{}{
		"status_code": resp.StatusCode(),
		"headers":     t.headersToMapResty(resp.Header()),
		"latency_ms":  int(time.Since(startTime).Milliseconds()),
		"attempts":    attemptCount,
	}

	// Try to parse as JSON
	var jsonBody interface{}
	if err := json.Unmarshal(resp.Body(), &jsonBody); err == nil {
		result["body"] = jsonBody
	} else {
		// Return as string if not JSON
		result["body"] = resp.String()
	}

	// Check for HTTP errors
	if resp.StatusCode() >= 400 {
		return result, agentErrors.New(agentErrors.CodeToolExecution, "HTTP request failed").
			WithComponent("api_caller_tool").
			WithOperation("executeRequest").
			WithContext("url", params.URL).
			WithContext("method", params.Method).
			WithContext("status_code", resp.StatusCode()).
			WithContext("status", resp.Status())
	}

	return result, nil
}

// setAuthenticationResty sets authentication for resty requests
func (t *APICallerTool) setAuthenticationResty(req *resty.Request, auth *authConfig) error {
	if auth == nil {
		return nil
	}

	switch auth.Type {
	case "bearer":
		token, ok := auth.Credentials["token"].(string)
		if !ok {
			return agentErrors.New(agentErrors.CodeInvalidInput, "bearer auth requires 'token' credential").
				WithComponent("api_caller_tool").
				WithOperation("setAuthenticationResty")
		}
		req.SetAuthToken(token)

	case "basic":
		username, _ := auth.Credentials["username"].(string)
		password, _ := auth.Credentials["password"].(string)
		req.SetBasicAuth(username, password)

	case "api_key":
		key, ok := auth.Credentials["key"].(string)
		if !ok {
			return agentErrors.New(agentErrors.CodeInvalidInput, "api_key auth requires 'key' credential").
				WithComponent("api_caller_tool").
				WithOperation("setAuthenticationResty")
		}
		location, _ := auth.Credentials["location"].(string)
		name, _ := auth.Credentials["name"].(string)

		if location == "query" {
			// Add to URL query parameters
			if name == "" {
				name = "api_key"
			}
			req.SetQueryParam(name, key)
		} else {
			// Default to header
			if name == "" {
				name = "X-API-Key"
			}
			req.SetHeader(name, key)
		}

	case "oauth2":
		token, ok := auth.Credentials["access_token"].(string)
		if !ok {
			return agentErrors.New(agentErrors.CodeInvalidInput, "oauth2 auth requires 'access_token' credential").
				WithComponent("api_caller_tool").
				WithOperation("setAuthenticationResty")
		}
		req.SetAuthToken(token)

	default:
		return agentErrors.New(agentErrors.CodeInvalidInput, "unsupported auth type").
			WithComponent("api_caller_tool").
			WithOperation("setAuthenticationResty").
			WithContext("auth_type", auth.Type)
	}

	return nil
}

// headersToMapResty converts http.Header to map for resty
func (t *APICallerTool) headersToMapResty(headers map[string][]string) map[string]string {
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

// parseAPIInput parses the tool input
func (t *APICallerTool) parseAPIInput(input interface{}) (*apiParams, error) {
	var params apiParams

	switch v := input.(type) {
	case string:
		// Simple GET request
		params.URL = v
		params.Method = interfaces.MethodGet
	case map[string]interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &params); err != nil {
			return nil, err
		}
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported input type").
			WithComponent("api_caller_tool").
			WithOperation("parseAPIInput").
			WithContext("input_type", v)
	}

	// Set defaults
	if params.Method == "" {
		params.Method = interfaces.MethodGet
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
		if err := runtime.StreamWriter(map[string]interface{}{
			"status": "calling_api",
			"tool":   t.Name(),
		}); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to stream status").
				WithComponent("api_caller_tool").
				WithOperation("executeWithRuntime")
		}
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
			if err := runtime.PutToStore([]string{"api_responses"}, params.URL, result); err != nil {
				return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to put to store").
					WithComponent("api_caller_tool").
					WithOperation("executeWithRuntime").
					WithContext("url", params.URL)
			}
		}
	}

	// Stream completion
	if runtime != nil && runtime.StreamWriter != nil {
		if err := runtime.StreamWriter(map[string]interface{}{
			"status": "completed",
			"tool":   t.Name(),
			"error":  err,
		}); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to stream completion").
				WithComponent("api_caller_tool").
				WithOperation("executeWithRuntime")
		}
	}

	return result, err
}
