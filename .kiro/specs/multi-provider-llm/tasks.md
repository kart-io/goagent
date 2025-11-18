# Task List: Multi-Provider LLM Support

**Version**: 1.0
**Date**: 2025-11-18
**Status**: Implementation Phase
**Related**: [Requirements](requirements.md) | [Design](design.md)

## Overview

This document provides an ordered, actionable task list for implementing three new LLM providers (Anthropic Claude, Cohere, Hugging Face) for the GoAgent framework. Tasks are organized by dependency and designed for incremental development with testable milestones.

## Implementation Order

The implementation follows this order:
1. **Foundation tasks** - Common utilities and updates
2. **Anthropic Claude** - Complete implementation (simplest API)
3. **Cohere** - Complete implementation (mid complexity)
4. **Hugging Face** - Complete implementation (most complex due to model loading)
5. **Integration & Documentation** - Examples and docs

---

## Phase 1: Foundation (Prerequisites)

### Task 1.1: Update Provider Constants ✅ **Priority: HIGH**

**File**: `llm/client.go`

**Action**: Add three new provider constants to the `Provider` type.

**Implementation**:
```go
const (
    // Existing providers
    ProviderOpenAI      Provider = "openai"
    ProviderGemini      Provider = "gemini"
    ProviderDeepSeek    Provider = "deepseek"
    ProviderOllama      Provider = "ollama"
    ProviderSiliconFlow Provider = "siliconflow"
    ProviderKimi        Provider = "kimi"
    ProviderCustom      Provider = "custom"

    // NEW: Add these three constants
    ProviderAnthropic   Provider = "anthropic"
    ProviderCohere      Provider = "cohere"
    ProviderHuggingFace Provider = "huggingface"
)
```

**Acceptance Criteria**:
- [ ] Constants added to `llm/client.go`
- [ ] No compilation errors
- [ ] `./verify_imports.sh` passes

**Estimated Time**: 5 minutes

---

### Task 1.2: Verify Error Helpers Exist ✅ **Priority: HIGH**

**File**: `errors/helpers.go`

**Action**: Confirm that all required error helper functions exist:
- `NewInvalidConfigError`
- `NewLLMRequestError`
- `NewLLMResponseError`
- `NewLLMRateLimitError`
- `NewLLMTimeoutError`

**Implementation**: Read `errors/helpers.go` and verify functions exist (already verified in design phase).

**Acceptance Criteria**:
- [ ] All five helper functions exist
- [ ] Function signatures match design document

**Estimated Time**: 5 minutes

---

### Task 1.3: Create Common Utilities (Optional) ✅ **Priority: LOW**

**File**: `llm/providers/utils.go` (NEW)

**Action**: Create optional utility functions for SSE parsing (used by all providers).

**Implementation**:
```go
package providers

import (
    "bufio"
    "strings"
)

// parseSSE parses a Server-Sent Events line
// Returns (eventType, data, isValid)
func parseSSE(line string) (string, string, bool) {
    line = strings.TrimSpace(line)

    if line == "" || strings.HasPrefix(line, ":") {
        return "", "", false // Comment or empty
    }

    if strings.HasPrefix(line, "data: ") {
        data := strings.TrimPrefix(line, "data: ")
        return "data", data, true
    }

    if strings.HasPrefix(line, "event: ") {
        event := strings.TrimPrefix(line, "event: ")
        return "event", event, true
    }

    return "", "", false
}

// parseRetryAfter parses Retry-After header (seconds or HTTP-date)
func parseRetryAfter(header string) int {
    if header == "" {
        return 60 // Default 60 seconds
    }

    // Try parsing as integer (seconds)
    if seconds, err := strconv.Atoi(header); err == nil {
        return seconds
    }

    // Try parsing as HTTP-date (RFC1123)
    if t, err := time.Parse(time.RFC1123, header); err == nil {
        return int(time.Until(t).Seconds())
    }

    return 60 // Fallback
}
```

**Acceptance Criteria**:
- [ ] `utils.go` created in `llm/providers/`
- [ ] Functions are internal (lowercase) or exported if needed by tests
- [ ] No external dependencies beyond stdlib

**Estimated Time**: 15 minutes

---

## Phase 2: Anthropic Claude Provider

### Task 2.1: Create Anthropic Data Structures ✅ **Priority: HIGH**

**File**: `llm/providers/anthropic.go` (NEW)

**Action**: Define all Anthropic-specific data structures.

**Implementation**:
```go
package providers

import (
    "context"
    "net/http"
    "time"

    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm"
)

// AnthropicProvider implements LLM interface for Anthropic Claude
type AnthropicProvider struct {
    config      *llm.Config
    httpClient  *http.Client
    apiKey      string
    baseURL     string
    model       string
    maxTokens   int
    temperature float64
}

// AnthropicRequest represents a request to Anthropic API
type AnthropicRequest struct {
    Model         string              `json:"model"`
    Messages      []AnthropicMessage  `json:"messages"`
    MaxTokens     int                 `json:"max_tokens"`
    Temperature   float64             `json:"temperature,omitempty"`
    TopP          float64             `json:"top_p,omitempty"`
    TopK          int                 `json:"top_k,omitempty"`
    Stream        bool                `json:"stream,omitempty"`
    StopSequences []string            `json:"stop_sequences,omitempty"`
    System        string              `json:"system,omitempty"`
}

type AnthropicMessage struct {
    Role    string `json:"role"`    // "user" or "assistant"
    Content string `json:"content"`
}

// AnthropicResponse represents a response from Anthropic API
type AnthropicResponse struct {
    ID           string              `json:"id"`
    Type         string              `json:"type"`
    Role         string              `json:"role"`
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

// For streaming
type AnthropicStreamEvent struct {
    Type         string              `json:"type"`
    Message      *AnthropicResponse  `json:"message,omitempty"`
    Index        int                 `json:"index,omitempty"`
    Delta        *AnthropicDelta     `json:"delta,omitempty"`
    ContentBlock *AnthropicContent   `json:"content_block,omitempty"`
    Usage        *AnthropicUsage     `json:"usage,omitempty"`
}

type AnthropicDelta struct {
    Type string `json:"type"` // "text_delta"
    Text string `json:"text"`
}
```

**Acceptance Criteria**:
- [ ] File created with all data structures
- [ ] Struct tags match Anthropic API spec
- [ ] No compilation errors
- [ ] `./verify_imports.sh` passes

**Estimated Time**: 20 minutes

---

### Task 2.2: Implement Anthropic Constructor ✅ **Priority: HIGH**

**File**: `llm/providers/anthropic.go`

**Action**: Implement `NewAnthropic` constructor with validation and defaults.

**Implementation**:
```go
import (
    "os"
    agentErrors "github.com/kart-io/goagent/errors"
)

// NewAnthropic creates a new Anthropic provider
func NewAnthropic(config *llm.Config) (*AnthropicProvider, error) {
    // 1. Get API key from config or env
    apiKey := config.APIKey
    if apiKey == "" {
        apiKey = os.Getenv("ANTHROPIC_API_KEY")
    }

    if apiKey == "" {
        return nil, agentErrors.NewInvalidConfigError("anthropic", "api_key", "API key must be provided via config or ANTHROPIC_API_KEY env var")
    }

    // 2. Set base URL with fallback
    baseURL := config.BaseURL
    if baseURL == "" {
        baseURL = os.Getenv("ANTHROPIC_BASE_URL")
    }
    if baseURL == "" {
        baseURL = "https://api.anthropic.com/v1"
    }

    // 3. Set model with fallback
    model := config.Model
    if model == "" {
        model = os.Getenv("ANTHROPIC_MODEL")
    }
    if model == "" {
        model = "claude-3-sonnet-20240229" // Default to balanced model
    }

    // 4. Set other parameters with defaults
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

    // 5. Create HTTP client with connection pooling
    httpClient := &http.Client{
        Timeout: timeout,
        Transport: &http.Transport{
            MaxIdleConns:        100,
            MaxIdleConnsPerHost: 10,
            IdleConnTimeout:     90 * time.Second,
        },
    }

    provider := &AnthropicProvider{
        config:      config,
        httpClient:  httpClient,
        apiKey:      apiKey,
        baseURL:     baseURL,
        model:       model,
        maxTokens:   maxTokens,
        temperature: temperature,
    }

    return provider, nil
}
```

**Acceptance Criteria**:
- [ ] Constructor validates API key (returns error if missing)
- [ ] Environment variables are checked as fallback
- [ ] Default values are set correctly
- [ ] HTTP client uses connection pooling
- [ ] Returns appropriate error using `errors` package

**Estimated Time**: 25 minutes

---

### Task 2.3: Implement Anthropic Complete Method ✅ **Priority: HIGH**

**File**: `llm/providers/anthropic.go`

**Action**: Implement the `Complete` method (implements `llm.Client` interface).

**Implementation**: See design document section "Implementation Patterns" > "Complete Method Pattern".

**Key Steps**:
1. Build `AnthropicRequest` from `llm.CompletionRequest`
2. Separate system message if present
3. Execute HTTP request
4. Handle errors with proper error codes
5. Convert response to `llm.CompletionResponse`

**Acceptance Criteria**:
- [ ] Implements `llm.Client` interface method
- [ ] Handles system messages correctly (separate parameter)
- [ ] Converts message formats properly
- [ ] Uses error helpers from `errors` package
- [ ] Returns `llm.CompletionResponse` with token usage
- [ ] Respects context cancellation

**Estimated Time**: 40 minutes

---

### Task 2.4: Implement Anthropic HTTP Execution ✅ **Priority: HIGH**

**File**: `llm/providers/anthropic.go`

**Action**: Implement low-level HTTP request execution with retry logic.

**Implementation**: See design document section "Implementation Patterns" > "HTTP Request Pattern".

**Methods to implement**:
- `execute(ctx, req)` - Single HTTP request
- `executeWithRetry(ctx, req)` - Retry wrapper with exponential backoff
- `handleHTTPError(resp, model)` - Map HTTP status to errors
- `buildRequest(req)` - Convert llm.CompletionRequest to AnthropicRequest
- `convertResponse(resp)` - Convert AnthropicResponse to llm.CompletionResponse

**Acceptance Criteria**:
- [ ] HTTP headers set correctly (x-api-key, anthropic-version, Content-Type)
- [ ] Exponential backoff on retryable errors (429, 500, 503)
- [ ] Max 3 retry attempts
- [ ] Context cancellation respected
- [ ] Proper error mapping for all HTTP status codes
- [ ] Token usage populated correctly

**Estimated Time**: 60 minutes

---

### Task 2.5: Implement Anthropic Streaming ✅ **Priority: MEDIUM**

**File**: `llm/providers/anthropic.go`

**Action**: Implement `Stream` method for streaming responses.

**Implementation**: See design document section "Implementation Patterns" > "Streaming Pattern".

**Key Steps**:
1. Build streaming request (set `Stream: true`)
2. Set `Accept: text/event-stream` header
3. Parse SSE format
4. Extract text from `content_block_delta` events
5. Handle channel cleanup on errors

**Acceptance Criteria**:
- [ ] Returns buffered channel (size 100)
- [ ] Parses SSE format correctly
- [ ] Handles `content_block_delta` events
- [ ] Closes channel on completion or error
- [ ] Respects context cancellation
- [ ] No goroutine leaks

**Estimated Time**: 50 minutes

---

### Task 2.6: Implement Anthropic Interface Methods ✅ **Priority: MEDIUM**

**File**: `llm/providers/anthropic.go`

**Action**: Implement remaining `llm.Client` interface methods.

**Implementation**:
```go
// Chat implements chat conversation (delegates to Complete)
func (p *AnthropicProvider) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
    return p.Complete(ctx, &llm.CompletionRequest{
        Messages: messages,
    })
}

// Provider returns the provider type
func (p *AnthropicProvider) Provider() llm.Provider {
    return llm.ProviderAnthropic
}

// IsAvailable checks if the provider is available
func (p *AnthropicProvider) IsAvailable() bool {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // Try a minimal completion
    _, err := p.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{{Role: "user", Content: "test"}},
    })

    return err == nil
}

// Optional: Additional helper methods
func (p *AnthropicProvider) ModelName() string {
    return p.model
}

func (p *AnthropicProvider) MaxTokens() int {
    return p.maxTokens
}
```

**Acceptance Criteria**:
- [ ] `Chat` method delegates to `Complete`
- [ ] `Provider` returns `llm.ProviderAnthropic`
- [ ] `IsAvailable` performs lightweight health check
- [ ] All methods compile and implement interface correctly

**Estimated Time**: 20 minutes

---

### Task 2.7: Write Anthropic Unit Tests ✅ **Priority: HIGH**

**File**: `llm/providers/anthropic_test.go` (NEW)

**Action**: Write comprehensive unit tests using mock HTTP server.

**Test Cases**:
1. `TestNewAnthropic` - Constructor validation
2. `TestAnthropicComplete` - Successful completion
3. `TestAnthropicChat` - Chat method
4. `TestAnthropicStream` - Streaming responses
5. `TestAnthropicErrorHandling` - All error paths
6. `TestAnthropicRetry` - Retry logic
7. `TestAnthropicContextCancellation` - Context cancellation

**Implementation**: See design document section "Testing Strategy" > "Unit Tests".

**Acceptance Criteria**:
- [ ] All test cases pass
- [ ] Coverage >= 80% for `anthropic.go`
- [ ] Tests use `httptest.NewServer` for mocking
- [ ] Error paths covered
- [ ] Run with `go test -race` (no race conditions)

**Estimated Time**: 90 minutes

---

### Task 2.8: Write Anthropic Integration Test (Optional) ✅ **Priority: LOW**

**File**: `llm/providers/anthropic_test.go`

**Action**: Write optional integration test with real API.

**Implementation**:
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
        Messages: []llm.Message{{Role: "user", Content: "Say 'test' and nothing else"}},
    })

    assert.NoError(t, err)
    assert.NotEmpty(t, resp.Content)
    assert.Greater(t, resp.TokensUsed, 0)
    assert.Contains(t, strings.ToLower(resp.Content), "test")
}
```

**Acceptance Criteria**:
- [ ] Test tagged with `// +build integration`
- [ ] Skips if API key not set
- [ ] Uses cheapest model (Haiku)
- [ ] Verifies basic functionality

**Estimated Time**: 20 minutes

---

## Phase 3: Cohere Provider

### Task 3.1: Create Cohere Data Structures ✅ **Priority: HIGH**

**File**: `llm/providers/cohere.go` (NEW)

**Action**: Define all Cohere-specific data structures.

**Implementation**: See design document section "Data Structures" > "2. Cohere".

**Acceptance Criteria**:
- [ ] All structs defined with correct JSON tags
- [ ] Matches Cohere API spec
- [ ] No compilation errors

**Estimated Time**: 20 minutes

---

### Task 3.2: Implement Cohere Constructor ✅ **Priority: HIGH**

**File**: `llm/providers/cohere.go`

**Action**: Implement `NewCohere` constructor.

**Implementation**: Follow Anthropic pattern with Cohere-specific defaults.

**Defaults**:
- Base URL: `https://api.cohere.ai/v1`
- Model: `command`
- Environment variable: `COHERE_API_KEY`

**Acceptance Criteria**:
- [ ] Validates API key
- [ ] Sets defaults correctly
- [ ] Handles environment variables

**Estimated Time**: 20 minutes

---

### Task 3.3: Implement Cohere Complete Method ✅ **Priority: HIGH**

**File**: `llm/providers/cohere.go`

**Action**: Implement the `Complete` method.

**Key Differences from Anthropic**:
- Uses `/chat` endpoint
- Message format: `{role: "USER"/"CHATBOT", message: "..."}`
- System messages go in separate field or chat history

**Acceptance Criteria**:
- [ ] Converts messages to Cohere format (USER/CHATBOT)
- [ ] Handles chat history correctly
- [ ] Returns token usage from `token_count` field

**Estimated Time**: 40 minutes

---

### Task 3.4: Implement Cohere HTTP Execution ✅ **Priority: HIGH**

**File**: `llm/providers/cohere.go`

**Action**: Implement HTTP execution with retry.

**Key Differences**:
- Authorization: `Bearer {api_key}` (not x-api-key)
- Endpoint: `/v1/chat`
- Response format slightly different

**Acceptance Criteria**:
- [ ] Uses Bearer token authentication
- [ ] Handles Cohere-specific error responses
- [ ] Retry logic implemented

**Estimated Time**: 50 minutes

---

### Task 3.5: Implement Cohere Streaming ✅ **Priority: MEDIUM**

**File**: `llm/providers/cohere.go`

**Action**: Implement streaming for Cohere.

**Key Differences**:
- SSE events have `event_type` field
- Look for `"event_type": "text-generation"`
- Text is in `text` field

**Acceptance Criteria**:
- [ ] Parses Cohere SSE format
- [ ] Extracts text from `text-generation` events
- [ ] Handles `stream-end` event

**Estimated Time**: 40 minutes

---

### Task 3.6: Implement Cohere Interface Methods ✅ **Priority: MEDIUM**

**File**: `llm/providers/cohere.go`

**Action**: Implement `Chat`, `Provider`, `IsAvailable`.

**Acceptance Criteria**:
- [ ] All interface methods implemented
- [ ] Returns `llm.ProviderCohere`

**Estimated Time**: 15 minutes

---

### Task 3.7: Write Cohere Unit Tests ✅ **Priority: HIGH**

**File**: `llm/providers/cohere_test.go` (NEW)

**Action**: Write comprehensive unit tests.

**Acceptance Criteria**:
- [ ] Coverage >= 80%
- [ ] All error paths tested
- [ ] Mock HTTP server used

**Estimated Time**: 80 minutes

---

### Task 3.8: Write Cohere Integration Test (Optional) ✅ **Priority: LOW**

**File**: `llm/providers/cohere_test.go`

**Action**: Optional integration test.

**Acceptance Criteria**:
- [ ] Tagged with `// +build integration`
- [ ] Uses `COHERE_API_KEY` env var

**Estimated Time**: 15 minutes

---

## Phase 4: Hugging Face Provider

### Task 4.1: Create Hugging Face Data Structures ✅ **Priority: HIGH**

**File**: `llm/providers/huggingface.go` (NEW)

**Action**: Define all HuggingFace-specific data structures.

**Implementation**: See design document section "Data Structures" > "3. Hugging Face".

**Acceptance Criteria**:
- [ ] All structs defined
- [ ] Handles both non-streaming and streaming responses

**Estimated Time**: 20 minutes

---

### Task 4.2: Implement Hugging Face Constructor ✅ **Priority: HIGH**

**File**: `llm/providers/huggingface.go`

**Action**: Implement `NewHuggingFace` constructor.

**Defaults**:
- Base URL: `https://api-inference.huggingface.co`
- Model: `meta-llama/Meta-Llama-3-8B-Instruct`
- Environment variable: `HUGGINGFACE_API_KEY`

**Acceptance Criteria**:
- [ ] Validates API key
- [ ] Supports custom endpoints for dedicated inference

**Estimated Time**: 20 minutes

---

### Task 4.3: Implement Hugging Face Complete Method ✅ **Priority: HIGH**

**File**: `llm/providers/huggingface.go`

**Action**: Implement the `Complete` method.

**Key Differences**:
- Endpoint: `/models/{model_id}`
- Simple input: `{"inputs": "text", "parameters": {...}}`
- May return 503 if model is loading (need retry)

**Acceptance Criteria**:
- [ ] Constructs proper input format
- [ ] Handles model loading (503 status)
- [ ] Retries on model loading

**Estimated Time**: 45 minutes

---

### Task 4.4: Implement Hugging Face HTTP Execution ✅ **Priority: HIGH**

**File**: `llm/providers/huggingface.go`

**Action**: Implement HTTP execution with model loading retry.

**Key Differences**:
- Authorization: `Bearer {api_key}`
- Endpoint includes model ID: `{baseURL}/models/{modelID}`
- 503 means model is loading (wait and retry)

**Acceptance Criteria**:
- [ ] Handles 503 with extended retry (up to 60s)
- [ ] Proper error messages for model not found

**Estimated Time**: 50 minutes

---

### Task 4.5: Implement Hugging Face Streaming ✅ **Priority: MEDIUM**

**File**: `llm/providers/huggingface.go`

**Action**: Implement streaming.

**Key Differences**:
- Streaming format: `{"token": {"text": "..."}}`
- Final event has `"details"` field

**Acceptance Criteria**:
- [ ] Parses token-by-token responses
- [ ] Handles final event with details

**Estimated Time**: 40 minutes

---

### Task 4.6: Implement Hugging Face Interface Methods ✅ **Priority: MEDIUM**

**File**: `llm/providers/huggingface.go`

**Action**: Implement interface methods.

**Acceptance Criteria**:
- [ ] All methods implemented
- [ ] Returns `llm.ProviderHuggingFace`

**Estimated Time**: 15 minutes

---

### Task 4.7: Write Hugging Face Unit Tests ✅ **Priority: HIGH**

**File**: `llm/providers/huggingface_test.go` (NEW)

**Action**: Write comprehensive tests.

**Acceptance Criteria**:
- [ ] Coverage >= 80%
- [ ] Tests model loading retry (503 handling)

**Estimated Time**: 90 minutes

---

### Task 4.8: Write Hugging Face Integration Test (Optional) ✅ **Priority: LOW**

**File**: `llm/providers/huggingface_test.go`

**Action**: Optional integration test.

**Acceptance Criteria**:
- [ ] Tagged with `// +build integration`
- [ ] Uses `HUGGINGFACE_API_KEY`

**Estimated Time**: 15 minutes

---

## Phase 5: Integration & Documentation

### Task 5.1: Create Example for Anthropic ✅ **Priority: MEDIUM**

**File**: `examples/llm/anthropic_example.go` (NEW)

**Action**: Create usage example for Claude.

**Implementation**:
```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    // Create Anthropic provider
    provider, err := providers.NewAnthropic(&llm.Config{
        APIKey:      os.Getenv("ANTHROPIC_API_KEY"),
        Model:       "claude-3-sonnet-20240229",
        MaxTokens:   1000,
        Temperature: 0.7,
    })
    if err != nil {
        log.Fatal(err)
    }

    // Test completion
    resp, err := provider.Complete(context.Background(), &llm.CompletionRequest{
        Messages: []llm.Message{
            {Role: "user", Content: "What is the capital of France?"},
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Response: %s\n", resp.Content)
    fmt.Printf("Tokens used: %d (prompt: %d, completion: %d)\n",
        resp.Usage.TotalTokens,
        resp.Usage.PromptTokens,
        resp.Usage.CompletionTokens)

    // Test streaming
    fmt.Println("\nStreaming response:")
    tokens, err := provider.Stream(context.Background(), "Count from 1 to 5")
    if err != nil {
        log.Fatal(err)
    }

    for token := range tokens {
        fmt.Print(token)
    }
    fmt.Println()
}
```

**Acceptance Criteria**:
- [ ] Example compiles and runs with valid API key
- [ ] Demonstrates both `Complete` and `Stream`
- [ ] Shows token usage tracking

**Estimated Time**: 25 minutes

---

### Task 5.2: Create Example for Cohere ✅ **Priority: MEDIUM**

**File**: `examples/llm/cohere_example.go` (NEW)

**Action**: Create usage example for Cohere.

**Implementation**: Similar to Anthropic example, adapted for Cohere.

**Acceptance Criteria**:
- [ ] Example compiles and runs
- [ ] Uses `COHERE_API_KEY`

**Estimated Time**: 20 minutes

---

### Task 5.3: Create Example for Hugging Face ✅ **Priority: MEDIUM**

**File**: `examples/llm/huggingface_example.go` (NEW)

**Action**: Create usage example for Hugging Face.

**Implementation**: Similar pattern, with note about model loading.

**Acceptance Criteria**:
- [ ] Example compiles and runs
- [ ] Uses `HUGGINGFACE_API_KEY`

**Estimated Time**: 20 minutes

---

### Task 5.4: Update LLM Providers Documentation ✅ **Priority: HIGH**

**File**: `docs/guides/LLM_PROVIDERS.md`

**Action**: Add documentation for three new providers.

**Sections to add**:
1. **Anthropic Claude**
   - Supported models
   - API key setup
   - Configuration example
   - Rate limits and pricing
2. **Cohere**
   - Supported models
   - API key setup
   - Configuration example
   - Unique features (RAG-optimized models)
3. **Hugging Face**
   - Model selection
   - API token setup
   - Custom endpoints
   - Model loading considerations

**Acceptance Criteria**:
- [ ] All three providers documented
- [ ] Code examples provided
- [ ] Environment variables documented
- [ ] Rate limits and pricing notes included

**Estimated Time**: 60 minutes

---

### Task 5.5: Update README.md ✅ **Priority: MEDIUM**

**File**: `README.md`

**Action**: Update main README to mention new providers.

**Changes**:
- Update "LLM Abstraction" bullet: "Support for multiple LLM providers (OpenAI, Gemini, DeepSeek, **Anthropic Claude, Cohere, Hugging Face**)"
- Add to Quick Start example: Mention that users can swap providers easily

**Acceptance Criteria**:
- [ ] New providers mentioned in features list
- [ ] Quick Start section remains clear

**Estimated Time**: 10 minutes

---

### Task 5.6: Run Final Verification ✅ **Priority: HIGH**

**Action**: Run all verification steps before marking complete.

**Commands**:
```bash
# 1. Format all code
make fmt

# 2. Run linter (MUST pass with 0 issues)
make lint

# 3. Verify import layering
./verify_imports.sh

# 4. Run all tests with race detection
go test -race -v ./llm/providers/

# 5. Check coverage
go test -coverprofile=coverage.out ./llm/providers/
go tool cover -func=coverage.out | grep total

# 6. Try building examples
go build ./examples/llm/anthropic_example.go
go build ./examples/llm/cohere_example.go
go build ./examples/llm/huggingface_example.go
```

**Acceptance Criteria**:
- [ ] `make lint` passes with 0 issues
- [ ] `./verify_imports.sh` passes with 0 violations
- [ ] All tests pass
- [ ] Coverage >= 80% for all new provider files
- [ ] Examples compile successfully
- [ ] No race conditions detected

**Estimated Time**: 30 minutes

---

### Task 5.7: Create Pull Request ✅ **Priority: HIGH**

**Action**: Create PR with all changes.

**PR Description Template**:
```markdown
## Summary

Adds support for three new LLM providers:
- **Anthropic Claude** (Opus, Sonnet, Haiku models)
- **Cohere** (Command, Command-R models)
- **Hugging Face** (Any model via Inference API)

## Changes

- Added `ProviderAnthropic`, `ProviderCohere`, `ProviderHuggingFace` constants
- Implemented three new providers in `llm/providers/`
- Added comprehensive unit tests (>80% coverage)
- Created usage examples in `examples/llm/`
- Updated documentation in `docs/guides/LLM_PROVIDERS.md`

## Testing

- [x] All unit tests pass
- [x] Coverage >= 80%
- [x] `make lint` passes (0 issues)
- [x] `./verify_imports.sh` passes (0 violations)
- [x] No race conditions detected
- [x] Examples compile and run

## Checklist

- [x] Code follows existing patterns (OpenAI, DeepSeek)
- [x] All errors use `errors` package helpers
- [x] Streaming implemented for all providers
- [x] Token usage tracked correctly
- [x] Environment variables supported
- [x] Documentation updated
- [x] Examples provided

## Breaking Changes

None - fully backward compatible.

## Related Issues

Resolves roadmap item: "Additional LLM providers (Anthropic Claude, Cohere, Hugging Face)"
```

**Acceptance Criteria**:
- [ ] PR created with clear description
- [ ] All commits follow conventional commit format
- [ ] CI/CD checks pass

**Estimated Time**: 20 minutes

---

## Task Summary

| Phase | Tasks | Estimated Time |
|-------|-------|----------------|
| **Phase 1: Foundation** | 3 tasks | 25 minutes |
| **Phase 2: Anthropic** | 8 tasks | 6 hours |
| **Phase 3: Cohere** | 8 tasks | 5.5 hours |
| **Phase 4: Hugging Face** | 8 tasks | 6 hours |
| **Phase 5: Integration** | 7 tasks | 3 hours |
| **Total** | **34 tasks** | **~20.5 hours** |

## Risk Mitigation

### Risk: API Changes During Development

- **Mitigation**: Check official API docs before implementation
- **Fallback**: Use versioned API endpoints where available

### Risk: Import Layer Violations

- **Mitigation**: Run `./verify_imports.sh` after each provider completion
- **Fallback**: Refactor if violations detected (extract types to Layer 1)

### Risk: Insufficient Test Coverage

- **Mitigation**: Write tests alongside implementation (TDD approach)
- **Fallback**: Add tests before moving to next provider

### Risk: Rate Limiting During Testing

- **Mitigation**: Use mock servers for unit tests
- **Fallback**: Mark integration tests as optional

---

## Success Criteria

Implementation is considered complete when:

1. ✅ All three providers implemented
2. ✅ All providers implement `llm.Client` interface
3. ✅ Unit tests pass with >80% coverage
4. ✅ `make lint` passes with 0 issues
5. ✅ `./verify_imports.sh` passes with 0 violations
6. ✅ Examples compile and demonstrate usage
7. ✅ Documentation updated
8. ✅ PR created and CI/CD passes

---

**Status**: Ready for implementation
**Next Step**: Begin Phase 1 (Foundation tasks)
**Estimated Completion**: 3-4 days with focused development
