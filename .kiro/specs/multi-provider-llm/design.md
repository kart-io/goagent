# Design Document: Multi-Provider LLM Support

**Version**: 1.0
**Date**: 2025-11-18
**Status**: Design Phase
**Related**: [Requirements](requirements.md)

## Table of Contents

1. [Design Overview](#design-overview)
2. [Architecture Design](#architecture-design)
3. [API Design](#api-design)
4. [Data Structures](#data-structures)
5. [Error Handling Strategy](#error-handling-strategy)
6. [Implementation Patterns](#implementation-patterns)
7. [Integration Points](#integration-points)
8. [Configuration Management](#configuration-management)
9. [Testing Strategy](#testing-strategy)
10. [Deployment Considerations](#deployment-considerations)

---

## Design Overview

### Goals

This design adds three new LLM providers to the GoAgent framework following the established patterns from OpenAI and DeepSeek implementations. The design prioritizes:

1. **Consistency**: All providers implement the same `llm.Client` interface
2. **Simplicity**: Minimal dependencies, clear error handling
3. **Performance**: Streaming support, connection pooling, efficient token tracking
4. **Maintainability**: Clear separation of concerns, comprehensive tests
5. **Layer Compliance**: Strict adherence to Layer 2 import rules

### Design Principles

1. **Follow existing patterns**: OpenAI provider serves as the primary reference
2. **Use standard HTTP clients**: No provider-specific SDKs unless necessary
3. **Error-first design**: All error paths must be handled explicitly
4. **Thread-safe by default**: Use shared `http.Client` with connection pooling
5. **Context propagation**: All operations respect `context.Context` cancellation

---

## Architecture Design

### Layer 2 Placement

All three providers will reside in `llm/providers/` (Layer 2):

```
llm/
├── client.go                    # Client interface definition
├── providers/
│   ├── openai.go                # Existing
│   ├── deepseek.go              # Existing
│   ├── gemini.go                # Existing
│   ├── anthropic.go             # NEW - Anthropic Claude
│   ├── cohere.go                # NEW - Cohere
│   ├── huggingface.go           # NEW - Hugging Face
│   ├── providers_test.go        # Existing tests
│   ├── anthropic_test.go        # NEW - Claude tests
│   ├── cohere_test.go           # NEW - Cohere tests
│   └── huggingface_test.go      # NEW - HF tests
```

### Import Restrictions (Critical)

**Allowed imports (Layer 1)**:
- `github.com/kart-io/goagent/interfaces` - For `TokenUsage` type
- `github.com/kart-io/goagent/errors` - For error handling
- `github.com/kart-io/goagent/cache` - If caching is needed
- `github.com/kart-io/goagent/utils` - For utility functions

**Allowed internal imports**:
- `github.com/kart-io/goagent/llm` - For `Client` interface, `Config`, `Message`, etc.

**Forbidden imports**:
- `github.com/kart-io/goagent/core` ❌
- `github.com/kart-io/goagent/agents` ❌
- `github.com/kart-io/goagent/builder` ❌
- `github.com/kart-io/goagent/tools` ❌

### Provider Constants

Update `llm/client.go` to add new provider constants:

```go
const (
    // Existing
    ProviderOpenAI      Provider = "openai"
    ProviderGemini      Provider = "gemini"
    ProviderDeepSeek    Provider = "deepseek"
    ProviderOllama      Provider = "ollama"
    ProviderSiliconFlow Provider = "siliconflow"
    ProviderKimi        Provider = "kimi"
    ProviderCustom      Provider = "custom"

    // New providers
    ProviderAnthropic   Provider = "anthropic"
    ProviderCohere      Provider = "cohere"
    ProviderHuggingFace Provider = "huggingface"
)
```

---

## API Design

### Provider Interface Implementation

All providers implement the existing `llm.Client` interface:

```go
type Client interface {
    // Complete generates text completion
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

    // Chat conducts a conversation
    Chat(ctx context.Context, messages []Message) (*CompletionResponse, error)

    // Provider returns the provider type
    Provider() Provider

    // IsAvailable checks if LLM is available
    IsAvailable() bool
}
```

**Note**: The existing interface does NOT include streaming methods. Streaming will be implemented as additional methods on the provider structs, following the OpenAI pattern.

### Constructor Signatures

Each provider follows the same constructor pattern:

```go
// Anthropic Claude
func NewAnthropic(config *llm.Config) (*AnthropicProvider, error)

// Cohere
func NewCohere(config *llm.Config) (*CohereProvider, error)

// Hugging Face
func NewHuggingFace(config *llm.Config) (*HuggingFaceProvider, error)
```

### Provider-Specific Methods

Following the OpenAI pattern, each provider can have additional methods:

```go
// AnthropicProvider
type AnthropicProvider struct {
    config      *llm.Config
    httpClient  *http.Client
    apiKey      string
    baseURL     string
    model       string
    maxTokens   int
    temperature float64
}

// Additional methods (not part of Client interface)
func (p *AnthropicProvider) Stream(ctx context.Context, prompt string) (<-chan string, error)
func (p *AnthropicProvider) ModelName() string
func (p *AnthropicProvider) MaxTokens() int
```

---

## Data Structures

### 1. Anthropic Claude

#### Request Structure

```go
type AnthropicRequest struct {
    Model       string              `json:"model"`
    Messages    []AnthropicMessage  `json:"messages"`
    MaxTokens   int                 `json:"max_tokens"`
    Temperature float64             `json:"temperature,omitempty"`
    TopP        float64             `json:"top_p,omitempty"`
    TopK        int                 `json:"top_k,omitempty"`
    Stream      bool                `json:"stream,omitempty"`
    StopSequences []string          `json:"stop_sequences,omitempty"`
}

type AnthropicMessage struct {
    Role    string `json:"role"`    // "user" or "assistant"
    Content string `json:"content"`
}

// Note: Claude uses system parameter separately, not in messages
```

#### Response Structure

```go
type AnthropicResponse struct {
    ID           string              `json:"id"`
    Type         string              `json:"type"`           // "message"
    Role         string              `json:"role"`           // "assistant"
    Content      []AnthropicContent  `json:"content"`
    Model        string              `json:"model"`
    StopReason   string              `json:"stop_reason"`
    StopSequence string              `json:"stop_sequence,omitempty"`
    Usage        AnthropicUsage      `json:"usage"`
}

type AnthropicContent struct {
    Type string `json:"type"` // "text"
    Text string `json:"text"`
}

type AnthropicUsage struct {
    InputTokens  int `json:"input_tokens"`
    OutputTokens int `json:"output_tokens"`
}
```

#### Stream Event Structure

```go
type AnthropicStreamEvent struct {
    Type         string             `json:"type"` // "message_start", "content_block_delta", "message_stop"
    Message      *AnthropicResponse `json:"message,omitempty"`
    Index        int                `json:"index,omitempty"`
    Delta        *AnthropicDelta    `json:"delta,omitempty"`
    ContentBlock *AnthropicContent  `json:"content_block,omitempty"`
    Usage        *AnthropicUsage    `json:"usage,omitempty"`
}

type AnthropicDelta struct {
    Type string `json:"type"` // "text_delta"
    Text string `json:"text"`
}
```

### 2. Cohere

#### Request Structure

```go
type CohereRequest struct {
    Model             string           `json:"model"`
    Message           string           `json:"message"`
    ChatHistory       []CohereMessage  `json:"chat_history,omitempty"`
    Temperature       float64          `json:"temperature,omitempty"`
    MaxTokens         int              `json:"max_tokens,omitempty"`
    P                 float64          `json:"p,omitempty"` // Top-p
    K                 int              `json:"k,omitempty"` // Top-k
    Stream            bool             `json:"stream,omitempty"`
    StopSequences     []string         `json:"stop_sequences,omitempty"`
    PresencePenalty   float64          `json:"presence_penalty,omitempty"`
    FrequencyPenalty  float64          `json:"frequency_penalty,omitempty"`
}

type CohereMessage struct {
    Role    string `json:"role"`    // "USER", "CHATBOT", "SYSTEM"
    Message string `json:"message"`
}
```

#### Response Structure

```go
type CohereResponse struct {
    ResponseID   string        `json:"response_id"`
    Text         string        `json:"text"`
    GenerationID string        `json:"generation_id"`
    FinishReason string        `json:"finish_reason"`
    TokenCount   CohereTokens  `json:"token_count"`
    ChatHistory  []CohereMessage `json:"chat_history,omitempty"`
}

type CohereTokens struct {
    PromptTokens     int `json:"prompt_tokens"`
    ResponseTokens   int `json:"response_tokens"`
    TotalTokens      int `json:"total_tokens"`
    BilledTokens     int `json:"billed_tokens,omitempty"`
}
```

#### Stream Event Structure

```go
type CohereStreamEvent struct {
    EventType    string `json:"event_type"` // "stream-start", "text-generation", "stream-end"
    Text         string `json:"text,omitempty"`
    FinishReason string `json:"finish_reason,omitempty"`
    Response     *CohereResponse `json:"response,omitempty"`
}
```

### 3. Hugging Face

#### Request Structure

```go
type HuggingFaceRequest struct {
    Inputs     string                     `json:"inputs"`
    Parameters HuggingFaceParameters      `json:"parameters,omitempty"`
    Options    HuggingFaceOptions         `json:"options,omitempty"`
}

type HuggingFaceParameters struct {
    Temperature      float64  `json:"temperature,omitempty"`
    MaxNewTokens     int      `json:"max_new_tokens,omitempty"`
    TopP             float64  `json:"top_p,omitempty"`
    TopK             int      `json:"top_k,omitempty"`
    RepetitionPenalty float64 `json:"repetition_penalty,omitempty"`
    StopSequences    []string `json:"stop_sequences,omitempty"`
    ReturnFullText   bool     `json:"return_full_text,omitempty"`
}

type HuggingFaceOptions struct {
    UseCache     bool `json:"use_cache"`
    WaitForModel bool `json:"wait_for_model"`
}
```

#### Response Structure

```go
type HuggingFaceResponse struct {
    GeneratedText string                  `json:"generated_text"`
    Details       *HuggingFaceDetails     `json:"details,omitempty"`
}

type HuggingFaceDetails struct {
    FinishReason   string `json:"finish_reason"`
    GeneratedTokens int   `json:"generated_tokens"`
    Seed           int64  `json:"seed,omitempty"`
}

// For streaming
type HuggingFaceStreamResponse struct {
    Token         HuggingFaceToken  `json:"token"`
    GeneratedText string            `json:"generated_text,omitempty"`
    Details       *HuggingFaceDetails `json:"details,omitempty"`
}

type HuggingFaceToken struct {
    ID      int     `json:"id"`
    Text    string  `json:"text"`
    LogProb float64 `json:"logprob"`
    Special bool    `json:"special"`
}
```

---

## Error Handling Strategy

### Error Types (from `errors/` package)

All providers use existing error helper functions:

1. **Configuration Errors**:
   ```go
   errors.NewInvalidConfigError("anthropic", "api_key", "API key is required")
   ```

2. **Request Errors**:
   ```go
   errors.NewLLMRequestError("anthropic", "claude-3-opus", err)
   ```

3. **Response Errors**:
   ```go
   errors.NewLLMResponseError("anthropic", "claude-3-opus", "no content in response")
   ```

4. **Rate Limit Errors**:
   ```go
   errors.NewLLMRateLimitError("anthropic", "claude-3-opus", 60)
   ```

5. **Timeout Errors**:
   ```go
   errors.NewLLMTimeoutError("anthropic", "claude-3-opus", 60)
   ```

### HTTP Error Code Mapping

Each provider should map HTTP status codes to appropriate errors:

```go
func (p *AnthropicProvider) handleHTTPError(resp *http.Response, model string) error {
    switch resp.StatusCode {
    case 400:
        return errors.NewInvalidInputError("anthropic", "request", "bad request")
    case 401:
        return errors.NewInvalidConfigError("anthropic", "api_key", "invalid API key")
    case 403:
        return errors.NewInvalidConfigError("anthropic", "api_key", "API key lacks permissions")
    case 404:
        return errors.NewLLMResponseError("anthropic", model, "model not found")
    case 429:
        retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
        return errors.NewLLMRateLimitError("anthropic", model, retryAfter)
    case 500, 502, 503, 504:
        return errors.NewLLMRequestError("anthropic", model, fmt.Errorf("server error: %d", resp.StatusCode))
    default:
        return errors.NewLLMRequestError("anthropic", model, fmt.Errorf("unexpected status: %d", resp.StatusCode))
    }
}
```

### Retry Strategy

Implement exponential backoff for retryable errors (rate limits, timeouts, server errors):

```go
func (p *AnthropicProvider) executeWithRetry(ctx context.Context, req *AnthropicRequest) (*AnthropicResponse, error) {
    maxAttempts := 3
    baseDelay := 1 * time.Second

    for attempt := 1; attempt <= maxAttempts; attempt++ {
        resp, err := p.execute(ctx, req)
        if err == nil {
            return resp, nil
        }

        // Check if error is retryable
        if !isRetryable(err) {
            return nil, err
        }

        // Last attempt failed
        if attempt == maxAttempts {
            return nil, errors.ErrorWithRetry(err, attempt, maxAttempts)
        }

        // Exponential backoff with jitter
        delay := baseDelay * time.Duration(1<<uint(attempt-1))
        jitter := time.Duration(rand.Int63n(int64(delay) / 2))

        select {
        case <-ctx.Done():
            return nil, errors.NewContextCanceledError("llm_request")
        case <-time.After(delay + jitter):
            // Continue to next attempt
        }
    }

    return nil, errors.NewInternalError("anthropic", "execute_with_retry", fmt.Errorf("max retries exceeded"))
}
```

---

## Implementation Patterns

### 1. Provider Struct Pattern

Follow the DeepSeek pattern (simpler than OpenAI):

```go
type AnthropicProvider struct {
    config      *llm.Config
    httpClient  *http.Client
    apiKey      string
    baseURL     string
    model       string
    maxTokens   int
    temperature float64
}
```

### 2. Constructor Pattern

```go
func NewAnthropic(config *llm.Config) (*AnthropicProvider, error) {
    // 1. Validate configuration
    if config.APIKey == "" {
        return nil, errors.NewInvalidConfigError("anthropic", "api_key", "Anthropic API key is required")
    }

    // 2. Set defaults
    baseURL := config.BaseURL
    if baseURL == "" {
        baseURL = "https://api.anthropic.com/v1"
    }

    model := config.Model
    if model == "" {
        model = "claude-3-sonnet-20240229"
    }

    maxTokens := config.MaxTokens
    if maxTokens == 0 {
        maxTokens = 2000
    }

    temperature := config.Temperature
    if temperature == 0 {
        temperature = 0.7
    }

    timeout := time.Duration(config.Timeout) * time.Second
    if timeout == 0 {
        timeout = 60 * time.Second
    }

    // 3. Create provider
    provider := &AnthropicProvider{
        config:      config,
        httpClient:  &http.Client{Timeout: timeout},
        apiKey:      config.APIKey,
        baseURL:     baseURL,
        model:       model,
        maxTokens:   maxTokens,
        temperature: temperature,
    }

    return provider, nil
}
```

### 3. Complete Method Pattern

```go
func (p *AnthropicProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
    // 1. Build provider-specific request
    anthropicReq := p.buildRequest(req)

    // 2. Execute request with retry
    resp, err := p.executeWithRetry(ctx, anthropicReq)
    if err != nil {
        return nil, err
    }

    // 3. Convert to standard response
    return p.convertResponse(resp), nil
}
```

### 4. HTTP Request Pattern

```go
func (p *AnthropicProvider) execute(ctx context.Context, req *AnthropicRequest) (*AnthropicResponse, error) {
    // 1. Serialize request
    body, err := json.Marshal(req)
    if err != nil {
        return nil, errors.Wrap(err, errors.CodeInternal, "failed to marshal request")
    }

    // 2. Create HTTP request
    httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(body))
    if err != nil {
        return nil, errors.Wrap(err, errors.CodeInternal, "failed to create request")
    }

    // 3. Set headers
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("x-api-key", p.apiKey)
    httpReq.Header.Set("anthropic-version", "2023-06-01")

    // 4. Execute request
    httpResp, err := p.httpClient.Do(httpReq)
    if err != nil {
        return nil, errors.NewLLMRequestError("anthropic", p.model, err)
    }
    defer httpResp.Body.Close()

    // 5. Check status code
    if httpResp.StatusCode != http.StatusOK {
        return nil, p.handleHTTPError(httpResp, p.model)
    }

    // 6. Deserialize response
    var resp AnthropicResponse
    if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
        return nil, errors.NewLLMResponseError("anthropic", p.model, "failed to decode response")
    }

    return &resp, nil
}
```

### 5. Streaming Pattern

```go
func (p *AnthropicProvider) Stream(ctx context.Context, prompt string) (<-chan string, error) {
    tokens := make(chan string, 100)

    // Build streaming request
    req := &AnthropicRequest{
        Model:     p.model,
        Messages:  []AnthropicMessage{{Role: "user", Content: prompt}},
        MaxTokens: p.maxTokens,
        Temperature: p.temperature,
        Stream:    true,
    }

    // Create HTTP request
    body, _ := json.Marshal(req)
    httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(body))
    if err != nil {
        return nil, errors.NewLLMRequestError("anthropic", p.model, err)
    }

    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("x-api-key", p.apiKey)
    httpReq.Header.Set("anthropic-version", "2023-06-01")
    httpReq.Header.Set("Accept", "text/event-stream")

    // Execute request
    httpResp, err := p.httpClient.Do(httpReq)
    if err != nil {
        return nil, errors.NewLLMRequestError("anthropic", p.model, err)
    }

    if httpResp.StatusCode != http.StatusOK {
        httpResp.Body.Close()
        return nil, p.handleHTTPError(httpResp, p.model)
    }

    // Start goroutine to read stream
    go func() {
        defer close(tokens)
        defer httpResp.Body.Close()

        scanner := bufio.NewScanner(httpResp.Body)
        for scanner.Scan() {
            line := scanner.Text()

            // Parse SSE format: "data: {...}"
            if !strings.HasPrefix(line, "data: ") {
                continue
            }

            data := strings.TrimPrefix(line, "data: ")
            if data == "[DONE]" {
                return
            }

            var event AnthropicStreamEvent
            if err := json.Unmarshal([]byte(data), &event); err != nil {
                continue
            }

            // Extract text from content_block_delta events
            if event.Type == "content_block_delta" && event.Delta != nil {
                tokens <- event.Delta.Text
            }
        }

        if err := scanner.Err(); err != nil {
            // Log error but don't crash stream
            fmt.Printf("Stream error: %v\n", err)
        }
    }()

    return tokens, nil
}
```

### 6. Token Usage Conversion

```go
func (p *AnthropicProvider) convertResponse(resp *AnthropicResponse) *llm.CompletionResponse {
    // Extract text content
    var content string
    if len(resp.Content) > 0 {
        content = resp.Content[0].Text
    }

    return &llm.CompletionResponse{
        Content:      content,
        Model:        resp.Model,
        TokensUsed:   resp.Usage.InputTokens + resp.Usage.OutputTokens,
        FinishReason: resp.StopReason,
        Provider:     string(llm.ProviderAnthropic),
        Usage: &interfaces.TokenUsage{
            PromptTokens:     resp.Usage.InputTokens,
            CompletionTokens: resp.Usage.OutputTokens,
            TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
        },
    }
}
```

---

## Integration Points

### 1. Builder Integration

No changes needed to `builder/` package. Users can create providers directly:

```go
// Create Anthropic provider
llmClient, err := providers.NewAnthropic(&llm.Config{
    APIKey:      os.Getenv("ANTHROPIC_API_KEY"),
    Model:       "claude-3-opus-20240229",
    MaxTokens:   4000,
    Temperature: 0.7,
    Timeout:     60,
})

// Use with builder
agent, err := builder.NewAgentBuilder(llmClient).
    WithSystemPrompt("You are a helpful assistant").
    Build()
```

### 2. Environment Variable Integration

Add environment variable loading in constructor:

```go
func NewAnthropic(config *llm.Config) (*AnthropicProvider, error) {
    // Allow env var override
    apiKey := config.APIKey
    if apiKey == "" {
        apiKey = os.Getenv("ANTHROPIC_API_KEY")
    }

    if apiKey == "" {
        return nil, errors.NewInvalidConfigError("anthropic", "api_key", "API key must be provided via config or ANTHROPIC_API_KEY env var")
    }

    // ... rest of constructor
}
```

### 3. Examples Integration

Create example files in `examples/llm/`:

```
examples/
└── llm/
    ├── openai_example.go        # Existing
    ├── anthropic_example.go     # NEW
    ├── cohere_example.go        # NEW
    └── huggingface_example.go   # NEW
```

---

## Configuration Management

### Environment Variables

Each provider supports the following environment variables:

**Anthropic Claude**:
- `ANTHROPIC_API_KEY` - API key (required)
- `ANTHROPIC_BASE_URL` - Custom endpoint (optional, default: `https://api.anthropic.com/v1`)
- `ANTHROPIC_MODEL` - Default model (optional, default: `claude-3-sonnet-20240229`)

**Cohere**:
- `COHERE_API_KEY` - API key (required)
- `COHERE_BASE_URL` - Custom endpoint (optional, default: `https://api.cohere.ai/v1`)
- `COHERE_MODEL` - Default model (optional, default: `command`)

**Hugging Face**:
- `HUGGINGFACE_API_KEY` - API token (required)
- `HUGGINGFACE_BASE_URL` - Custom endpoint (optional, default: `https://api-inference.huggingface.co`)
- `HUGGINGFACE_MODEL` - Default model (optional, default: `meta-llama/Meta-Llama-3-8B-Instruct`)

### Config Priority

1. Explicit config parameter (highest priority)
2. Environment variable
3. Default value (lowest priority)

```go
// Example priority implementation
model := config.Model
if model == "" {
    model = os.Getenv("ANTHROPIC_MODEL")
}
if model == "" {
    model = "claude-3-sonnet-20240229" // default
}
```

---

## Testing Strategy

### 1. Unit Tests

Use mock HTTP servers (`httptest`) to test:

```go
func TestAnthropicComplete(t *testing.T) {
    // Create mock server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verify headers
        assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
        assert.NotEmpty(t, r.Header.Get("x-api-key"))

        // Return mock response
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(AnthropicResponse{
            ID:   "msg_123",
            Type: "message",
            Role: "assistant",
            Content: []AnthropicContent{{Type: "text", Text: "Hello!"}},
            Model: "claude-3-sonnet-20240229",
            Usage: AnthropicUsage{InputTokens: 10, OutputTokens: 5},
        })
    }))
    defer server.Close()

    // Create provider with mock server
    provider, err := NewAnthropic(&llm.Config{
        APIKey:  "test-key",
        BaseURL: server.URL,
        Model:   "claude-3-sonnet-20240229",
    })
    require.NoError(t, err)

    // Test Complete
    resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
        Messages: []llm.Message{{Role: "user", Content: "Hi"}},
    })

    assert.NoError(t, err)
    assert.Equal(t, "Hello!", resp.Content)
    assert.Equal(t, 15, resp.TokensUsed)
}
```

### 2. Error Path Tests

Test all error conditions:

```go
func TestAnthropicErrorHandling(t *testing.T) {
    tests := []struct {
        name           string
        statusCode     int
        expectedError  errors.ErrorCode
    }{
        {"unauthorized", 401, errors.CodeInvalidConfig},
        {"rate_limit", 429, errors.CodeLLMRateLimit},
        {"server_error", 500, errors.CodeLLMRequest},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(tt.statusCode)
            }))
            defer server.Close()

            provider, _ := NewAnthropic(&llm.Config{APIKey: "test", BaseURL: server.URL})
            _, err := provider.Complete(context.Background(), &llm.CompletionRequest{
                Messages: []llm.Message{{Role: "user", Content: "test"}},
            })

            assert.Error(t, err)
            assert.True(t, errors.IsCode(err, tt.expectedError))
        })
    }
}
```

### 3. Streaming Tests

Test streaming behavior:

```go
func TestAnthropicStreaming(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        w.WriteHeader(http.StatusOK)

        flusher := w.(http.Flusher)

        // Send stream events
        events := []string{
            "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\"}}\n\n",
            "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
            "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n",
            "data: {\"type\":\"message_stop\"}\n\n",
        }

        for _, event := range events {
            fmt.Fprint(w, event)
            flusher.Flush()
            time.Sleep(10 * time.Millisecond)
        }
    }))
    defer server.Close()

    provider, _ := NewAnthropic(&llm.Config{APIKey: "test", BaseURL: server.URL})

    tokens, err := provider.Stream(context.Background(), "test")
    require.NoError(t, err)

    var result []string
    for token := range tokens {
        result = append(result, token)
    }

    assert.Equal(t, []string{"Hello", " world"}, result)
}
```

### 4. Integration Tests (Optional)

Tag integration tests to run only with real API keys:

```go
// +build integration

func TestAnthropicIntegration(t *testing.T) {
    apiKey := os.Getenv("ANTHROPIC_API_KEY")
    if apiKey == "" {
        t.Skip("ANTHROPIC_API_KEY not set")
    }

    provider, err := NewAnthropic(&llm.Config{
        APIKey: apiKey,
        Model:  "claude-3-haiku-20240307", // Use cheapest model
    })
    require.NoError(t, err)

    resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
        Messages: []llm.Message{{Role: "user", Content: "Say 'test'"}},
    })

    assert.NoError(t, err)
    assert.NotEmpty(t, resp.Content)
    assert.Greater(t, resp.TokensUsed, 0)
}
```

---

## Deployment Considerations

### 1. API Key Security

- **Never log API keys**: Ensure error messages don't expose keys
- **Use environment variables**: Don't hardcode keys
- **Rotate keys regularly**: Support key rotation without downtime

### 2. Rate Limiting

Each provider has different rate limits:

| Provider       | Rate Limit (Free) | Rate Limit (Paid) |
|----------------|-------------------|-------------------|
| Anthropic      | 50 req/min        | 1000+ req/min     |
| Cohere         | 100 req/min       | 10000+ req/min    |
| Hugging Face   | 1000 req/day      | Custom limits     |

Implement exponential backoff to handle 429 responses gracefully.

### 3. Timeout Configuration

Recommended timeout values:

- **Default**: 60 seconds
- **Streaming**: 120 seconds (longer for continuous streams)
- **Fast models**: 30 seconds (e.g., Claude Haiku)

### 4. Connection Pooling

Use shared `http.Client` with proper transport settings:

```go
httpClient := &http.Client{
    Timeout: timeout,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     90 * time.Second,
    },
}
```

### 5. Monitoring

Track key metrics:

- Request latency (p50, p95, p99)
- Error rate by error code
- Token usage per provider
- Rate limit hits

---

## Approval

This design document is ready for review and approval before implementation begins.

**Prepared by**: Claude Code
**Date**: 2025-11-18
**Version**: 1.0

**Next Steps**: Upon approval, generate implementation task list.
