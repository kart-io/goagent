# Requirements Document: Multi-Provider LLM Support

## Introduction

This feature adds support for three new LLM providers to the GoAgent framework: Anthropic Claude, Cohere, and Hugging Face. The implementation will follow the existing provider pattern established by OpenAI, Gemini, and DeepSeek providers, ensuring consistency and maintainability while respecting the strict 4-layer import architecture.

## Business Context

Adding multiple LLM providers enables:

- **Provider diversity**: Users can choose the best model for their specific use case
- **Cost optimization**: Different providers offer different pricing models
- **Capability variety**: Each provider has unique strengths (Claude for reasoning, Cohere for embeddings, HuggingFace for open-source models)
- **Redundancy**: Fallback options if one provider is unavailable
- **Compliance**: Some organizations may require specific providers for regulatory reasons

## Requirements

### Requirement 1: Anthropic Claude Provider Implementation

**User Story:** As a developer, I want to use Anthropic Claude models (Opus, Sonnet, Haiku) in my agents, so that I can leverage Claude's advanced reasoning capabilities.

#### Acceptance Criteria

1. WHEN a user creates a Claude provider with valid API key THEN the provider SHALL initialize successfully
2. WHEN a user sends a completion request THEN the provider SHALL return a response with content and token usage
3. WHEN a user requests streaming generation THEN the provider SHALL return a channel that streams tokens incrementally
4. WHEN a user makes a request with context cancellation THEN the provider SHALL respect the context and stop processing
5. IF the API key is missing THEN the provider SHALL return an InvalidConfigError with code "INVALID_CONFIG"
6. IF the API returns a rate limit error THEN the provider SHALL return an LLMRateLimitError with retry-after information
7. WHEN the provider completes a request THEN it SHALL track and return token usage (prompt tokens, completion tokens, total tokens)
8. WHEN multiple concurrent requests are made THEN the provider SHALL handle them safely without race conditions
9. IF network timeout occurs THEN the provider SHALL return an LLMTimeoutError with appropriate context
10. WHEN a request includes stop sequences THEN the provider SHALL pass them to the API correctly

**Models Supported:**

- `claude-3-opus-20240229` (most capable, best for complex tasks)
- `claude-3-sonnet-20240229` (balanced performance/cost)
- `claude-3-haiku-20240307` (fastest, most cost-effective)
- `claude-3-5-sonnet-20241022` (latest, enhanced capabilities)

**API Specifications:**

- Endpoint: `https://api.anthropic.com/v1/messages`
- Authentication: API key via `x-api-key` header
- Anthropic version header: `anthropic-version: 2023-06-01`
- Streaming: Server-sent events (SSE) format
- Rate limits: Vary by tier (handled via 429 responses)

### Requirement 2: Cohere Provider Implementation

**User Story:** As a developer, I want to use Cohere models in my agents, so that I can leverage Cohere's specialized capabilities for generation and embeddings.

#### Acceptance Criteria

1. WHEN a user creates a Cohere provider with valid API key THEN the provider SHALL initialize successfully
2. WHEN a user sends a completion request THEN the provider SHALL return a response with content and token usage
3. WHEN a user requests streaming generation THEN the provider SHALL return a channel that streams tokens incrementally
4. WHEN a user requests embeddings THEN the provider SHALL return vector embeddings for the input text
5. IF the API key is missing THEN the provider SHALL return an InvalidConfigError with code "INVALID_CONFIG"
6. IF the API returns an error THEN the provider SHALL wrap it with appropriate error code and context
7. WHEN the provider completes a request THEN it SHALL track and return token usage statistics
8. WHEN multiple concurrent requests are made THEN the provider SHALL handle them safely without race conditions
9. IF network issues occur THEN the provider SHALL return descriptive errors with context
10. WHEN chat history is provided THEN the provider SHALL convert messages to Cohere's chat format correctly

**Models Supported:**

- `command` (flagship model for complex tasks)
- `command-light` (faster, lower-cost variant)
- `command-nightly` (experimental features, latest improvements)
- `command-r` (RAG-optimized model)
- `command-r-plus` (enhanced RAG capabilities)

**API Specifications:**

- Endpoint: `https://api.cohere.ai/v1/chat` for chat completion
- Endpoint: `https://api.cohere.ai/v1/embed` for embeddings
- Authentication: Bearer token via `Authorization` header
- Streaming: Server-sent events (SSE) with `event:` and `data:` fields
- Rate limits: Based on API tier (handled via 429 responses)

### Requirement 3: Hugging Face Provider Implementation

**User Story:** As a developer, I want to use Hugging Face models via the Inference API, so that I can access open-source models and custom deployed models.

#### Acceptance Criteria

1. WHEN a user creates a Hugging Face provider with valid API key and model THEN the provider SHALL initialize successfully
2. WHEN a user sends a completion request THEN the provider SHALL return generated text
3. WHEN a user requests streaming generation THEN the provider SHALL return a channel that streams tokens incrementally
4. WHEN a user specifies a custom model ID THEN the provider SHALL use that model for inference
5. IF the API key is missing THEN the provider SHALL return an InvalidConfigError with code "INVALID_CONFIG"
6. IF the model is not available THEN the provider SHALL return an LLMResponseError with appropriate message
7. WHEN the provider completes a request THEN it SHALL return basic usage information
8. WHEN multiple concurrent requests are made THEN the provider SHALL handle them safely
9. IF the model is loading THEN the provider SHALL retry with exponential backoff (up to 3 attempts)
10. WHEN custom inference endpoints are used THEN the provider SHALL support custom base URLs

**Models Supported:**

- Any text generation model on Hugging Face Hub
- Default: `meta-llama/Meta-Llama-3-8B-Instruct`
- Support for custom deployed models via endpoint URL
- Popular models: `mistralai/Mixtral-8x7B-Instruct-v0.1`, `google/flan-t5-xxl`, etc.

**API Specifications:**

- Endpoint: `https://api-inference.huggingface.co/models/{model_id}`
- Authentication: Bearer token via `Authorization` header
- Streaming: Server-sent events (SSE) format
- Model loading: May return 503 while model loads (requires retry logic)
- Custom endpoints: Support for dedicated inference endpoints

### Requirement 4: Consistent Error Handling

**User Story:** As a developer, I want consistent error handling across all providers, so that I can write robust error handling code.

#### Acceptance Criteria

1. WHEN any provider encounters an error THEN it SHALL use the errors package from Layer 1
2. WHEN API requests fail THEN providers SHALL return NewLLMRequestError with provider and model context
3. WHEN API responses are invalid THEN providers SHALL return NewLLMResponseError with descriptive message
4. WHEN rate limits are hit THEN providers SHALL return NewLLMRateLimitError with retry-after information
5. WHEN timeouts occur THEN providers SHALL return NewLLMTimeoutError with timeout duration
6. WHEN configuration is invalid THEN providers SHALL return NewInvalidConfigError with specific config key
7. WHEN network errors occur THEN providers SHALL wrap them with appropriate context
8. IF context is canceled THEN providers SHALL return quickly and clean up resources
9. WHEN retries are needed THEN providers SHALL implement exponential backoff (max 3 attempts)
10. IF errors occur during streaming THEN providers SHALL send error through channel and close it

### Requirement 5: Configuration Management

**User Story:** As a developer, I want to configure providers via environment variables and code, so that I can easily switch providers without code changes.

#### Acceptance Criteria

1. WHEN a provider is created THEN it SHALL accept an llm.Config struct
2. WHEN API key is provided via config THEN the provider SHALL use it for authentication
3. WHEN base URL is customized THEN the provider SHALL use the custom endpoint
4. WHEN model is specified THEN the provider SHALL use that model, otherwise use defaults
5. WHEN temperature is set THEN the provider SHALL apply it to requests
6. WHEN max tokens is configured THEN the provider SHALL respect the limit
7. WHEN timeout is specified THEN the provider SHALL enforce request timeout
8. IF environment variables are set THEN they SHALL override default values
9. WHEN per-request parameters are provided THEN they SHALL override provider defaults
10. IF configuration is invalid THEN providers SHALL fail fast with descriptive errors

**Environment Variables:**

- `ANTHROPIC_API_KEY`: Claude API key
- `COHERE_API_KEY`: Cohere API key
- `HUGGINGFACE_API_KEY`: Hugging Face API token
- `ANTHROPIC_BASE_URL`: Custom Claude endpoint (optional)
- `COHERE_BASE_URL`: Custom Cohere endpoint (optional)
- `HUGGINGFACE_BASE_URL`: Custom HF endpoint (optional)
- `LLM_TIMEOUT`: Default timeout in seconds (default: 60)

### Requirement 6: Token Usage Tracking

**User Story:** As a developer, I want to track token usage for cost monitoring, so that I can manage API costs effectively.

#### Acceptance Criteria

1. WHEN a completion request succeeds THEN the response SHALL include token usage statistics
2. WHEN prompt tokens are counted THEN they SHALL be accurately reported
3. WHEN completion tokens are counted THEN they SHALL be accurately reported
4. WHEN total tokens are calculated THEN it SHALL equal prompt + completion tokens
5. IF the provider API returns token usage THEN it SHALL be mapped to interfaces.TokenUsage
6. IF the provider API does not return token usage THEN the provider SHALL estimate based on content length
7. WHEN streaming completes THEN cumulative token usage SHALL be calculable
8. WHEN embeddings are generated THEN token usage SHALL be tracked
9. WHEN multiple requests are made THEN each SHALL have independent token tracking
10. IF token limits are exceeded THEN the provider SHALL return an appropriate error

### Requirement 7: Streaming Support

**User Story:** As a developer, I want streaming responses for better user experience, so that I can display incremental results as they arrive.

#### Acceptance Criteria

1. WHEN streaming is requested THEN the provider SHALL return a channel of strings
2. WHEN the LLM generates tokens THEN they SHALL be sent to the channel incrementally
3. WHEN streaming completes THEN the channel SHALL be closed
4. IF an error occurs during streaming THEN the channel SHALL be closed after sending available data
5. WHEN context is canceled THEN streaming SHALL stop and channel SHALL be closed
6. WHEN multiple concurrent streams are active THEN each SHALL operate independently
7. IF network issues occur during streaming THEN the provider SHALL handle reconnection or fail gracefully
8. WHEN the stream ends normally THEN no error SHALL be returned
9. WHEN buffering is needed THEN channels SHALL have reasonable buffer sizes (100 tokens)
10. IF the stream is slow THEN the provider SHALL not block indefinitely

### Requirement 8: Thread Safety

**User Story:** As a developer, I want thread-safe providers, so that I can use them concurrently without issues.

#### Acceptance Criteria

1. WHEN multiple goroutines call Complete THEN no race conditions SHALL occur
2. WHEN multiple goroutines call Stream THEN each SHALL receive independent streams
3. WHEN configuration is read THEN it SHALL be safe from concurrent access
4. WHEN HTTP clients are used THEN they SHALL be shared safely
5. IF state needs to be modified THEN proper synchronization SHALL be used
6. WHEN providers are created THEN they SHALL be immediately safe for concurrent use
7. WHEN resources are cleaned up THEN no race conditions SHALL occur
8. IF connection pooling is used THEN it SHALL be thread-safe
9. WHEN errors occur THEN error handling SHALL be thread-safe
10. IF metrics are tracked THEN they SHALL be updated atomically

### Requirement 9: Architecture Compliance

**User Story:** As a maintainer, I want providers to follow Layer 2 import rules, so that the codebase remains well-structured.

#### Acceptance Criteria

1. WHEN providers are implemented THEN they SHALL reside in llm/providers/ (Layer 2)
2. WHEN providers import packages THEN they SHALL only import from Layer 1 (interfaces/, errors/, cache/, utils/)
3. WHEN providers implement interfaces THEN they SHALL use llm.Client from the llm package
4. IF providers need types THEN they SHALL use interfaces.TokenUsage from Layer 1
5. WHEN providers use errors THEN they SHALL use the errors package from Layer 1
6. WHEN verify_imports.sh runs THEN it SHALL pass with zero violations
7. IF new types are needed THEN they SHALL be added to Layer 1 or the llm package
8. WHEN providers are built THEN no circular dependencies SHALL exist
9. IF utilities are needed THEN they SHALL be from utils/ (Layer 1)
10. WHEN tests are written THEN they SHALL reside in *_test.go files (Layer 4)

### Requirement 10: Testing and Examples

**User Story:** As a developer, I want comprehensive tests and examples, so that I can understand and trust the provider implementations.

#### Acceptance Criteria

1. WHEN unit tests are written THEN they SHALL achieve at least 80% code coverage
2. WHEN HTTP responses are tested THEN mock HTTP servers SHALL be used
3. WHEN error conditions are tested THEN all error paths SHALL be covered
4. WHEN streaming is tested THEN channel behavior SHALL be validated
5. IF integration tests are written THEN they SHALL be optional and use real API keys from environment
6. WHEN examples are written THEN they SHALL demonstrate basic usage for each provider
7. WHEN examples are run THEN they SHALL work with valid API keys
8. IF benchmarks are written THEN they SHALL compare performance across providers
9. WHEN tests run THEN they SHALL pass in CI/CD without external dependencies
10. IF edge cases exist THEN they SHALL be covered by tests

## Non-Functional Requirements

### Performance

1. Provider initialization SHALL complete within 100ms
2. Completion requests SHALL have overhead less than 10ms (excluding network/API time)
3. Streaming SHALL have latency less than 50ms for first token
4. Concurrent requests SHALL scale linearly up to 100 goroutines
5. Memory allocation SHALL be minimal (no unnecessary copies)

### Reliability

1. Providers SHALL handle network errors gracefully with exponential backoff
2. Context cancellation SHALL be respected within 100ms
3. Resource cleanup SHALL occur even in error conditions
4. Streaming SHALL not leak goroutines on early termination
5. Providers SHALL be production-ready with proper error messages

### Maintainability

1. Code SHALL follow existing patterns from OpenAI and DeepSeek providers
2. Documentation SHALL include API reference and usage examples
3. Error messages SHALL be clear and actionable
4. Code SHALL pass golangci-lint with zero issues
5. Import layering SHALL be verified automatically

### Security

1. API keys SHALL NOT be logged or exposed in error messages
2. TLS connections SHALL be used for all API calls
3. Timeouts SHALL prevent indefinite hangs
4. Input validation SHALL prevent injection attacks
5. Secrets SHALL be configurable via environment variables

## Out of Scope

The following are explicitly out of scope for this feature:

1. **Tool calling support**: Function calling will be added in a future iteration
2. **Fine-tuning support**: Model fine-tuning is not part of this feature
3. **Provider auto-fallback**: Automatic failover between providers
4. **Response caching**: LLM response caching middleware (separate feature)
5. **Cost tracking dashboard**: UI for cost monitoring
6. **Batch processing**: Batch API support for supported providers
7. **Legacy model support**: Only current/stable models are supported
8. **Custom model training**: Integration with training APIs
9. **Multi-modal support**: Image/audio input (text-only for now)
10. **Provider-specific features**: Advanced features unique to one provider

## Success Criteria

This feature will be considered successful when:

1. All three providers (Claude, Cohere, Hugging Face) are implemented
2. All providers implement the llm.Client interface completely
3. All unit tests pass with >80% coverage
4. Integration tests work with real API keys (manual verification)
5. Examples run successfully and demonstrate key features
6. `make lint` passes with zero issues
7. `./verify_imports.sh` passes with zero violations
8. Documentation is complete and accurate
9. Code follows existing patterns and conventions
10. The feature is ready for production use

## Dependencies

### External Dependencies

- `github.com/anthropics/anthropic-sdk-go` (if available) or custom HTTP client
- Existing HTTP client libraries already in use
- Existing JSON parsing libraries
- Context and cancellation support from stdlib

### Internal Dependencies

- `interfaces/` package (Layer 1) - for interfaces.TokenUsage
- `errors/` package (Layer 1) - for error handling
- `llm/` package - for llm.Client interface and types
- Test utilities from existing test files

## Assumptions

1. Users have valid API keys for the providers they want to use
2. Network connectivity to provider APIs is available
3. Go 1.25.0+ is available (per project requirements)
4. The existing llm.Client interface is sufficient (no breaking changes needed)
5. Provider APIs remain stable (no major breaking changes during development)
6. The errors package provides all necessary error types
7. Existing middleware and builder patterns work with new providers
8. Test environment allows HTTP mocking
9. CI/CD environment has golangci-lint configured
10. Documentation follows existing patterns in docs/guides/

## Constraints

1. **Layer 2 import rules**: Must only import from Layer 1 packages
2. **No breaking changes**: Existing code must continue to work
3. **Performance**: Must not introduce significant overhead
4. **Thread safety**: All providers must be safe for concurrent use
5. **Error compatibility**: Must use existing error package
6. **Testing**: Must achieve 80%+ code coverage
7. **Linting**: Must pass all lint checks
8. **Documentation**: Must update LLM_PROVIDERS.md guide
9. **Consistency**: Must follow patterns from existing providers
10. **Production ready**: Must be reliable enough for production use

## Risks and Mitigations

### Risk 1: Provider API Changes

**Impact**: Medium - API changes could break implementations
**Probability**: Low - Provider APIs are generally stable
**Mitigation**: Use official SDKs where available, implement comprehensive error handling, add integration tests

### Risk 2: Import Layer Violations

**Impact**: High - Would require refactoring
**Probability**: Low - Will be caught by verify_imports.sh
**Mitigation**: Run import verification frequently during development, follow existing patterns closely

### Risk 3: Performance Degradation

**Impact**: Medium - Could affect user experience
**Probability**: Low - Following existing patterns should maintain performance
**Mitigation**: Run benchmarks, compare with existing providers, optimize hot paths

### Risk 4: Incomplete Error Handling

**Impact**: Medium - Could cause runtime panics
**Probability**: Medium - Many error paths to consider
**Mitigation**: Comprehensive unit tests, error path testing, code review

### Risk 5: Thread Safety Issues

**Impact**: High - Race conditions are hard to debug
**Probability**: Low - Using established patterns
**Mitigation**: Run tests with -race flag, follow existing provider patterns, code review

## Approval

This requirements document requires approval before proceeding to the design phase.

**Prepared by**: Claude Code
**Date**: 2025-11-18
**Version**: 1.0
