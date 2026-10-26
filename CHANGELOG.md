# Changelog

All notable changes to GoAgent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- 批量评测增加按预先声明任务契约选择直返/推理的模式，，原始答案标签不参与路由。

- 新增固定种子的 12 类批量评测、逐条结果持久化、默认与直返分组统计，以及千级执行边界和 SDK 协议回归；支持导入脱敏业务题集。

- Builder 新增显式 `WithReturnDirectTools`，单个终态工具成功后直接结束原生工具循环，省去模型总结请求；多工具保持常规循环，保留真实用量和完成处理。

- 新增可选 `llm.ToolStreamClient` 与 OpenAI 原生工具流，Builder 自动复用工具对话轮次直接回答，消除额外 READY/答案请求；去除流式请求中的重复系统提示，保留原生消息与工具调用 ID。

- 新增 `core.NewStreamRunnable` 与 Builder `AsRunnable()`，同步、流式、Batch 和 Pipe 绑定同一事件执行源，提供流式能力声明与类型化来源。
- 新增 `evaluation/agentbench`，提供请求预算、实际 SDK 协议检查、批量评测和历史失败记录。

- Builder retrieval integration via `interfaces.DocumentRetriever` and `WithRetriever`, with Unicode-safe evidence limits and numbered source metadata.
- OpenAI `CompleteStream` and `ChatStream` with structured deltas, terminal usage, and asynchronous error propagation.
- Real streaming agent support for structured and token-only clients, including upstream cancellation on reader close.

### Changed

- Builder 原生工具循环在已有工具结果且只剩最后一次请求时停止提供工具，将额度用于最终回答，避免无预算收尾的重复调用；应用评测提示区分原样返回和证据推理。

- 原生工具循环中的字符串结果直接传入工具正文，避免额外 JSON 引号/转义干扰模型原样输出；结构化结果继续使用 JSON。

- OpenAI 工具请求采用 `tools/tool_calls`，保留旧 `function_call` 响应兼容；支持并发工具分片合并、用量传递和流中错误。仅支持旧 `functions` 请求的服务需要升级端点。
- 自反思任务必须显式提供 `WithTaskExecutor`，不再返回模拟成功结果；各执行入口委派给同一真实执行器。

- `RAGAgent` now uses supplied retrievers or vector stores. Unsupported non-nil inputs fail validation; nil preserves the prompt-only preset.
- `ConfigureForRAG` no longer caches answers implicitly or rewrites the question as context. Applications should configure caching explicitly when evidence freshness and session isolation are accounted for.
- ReAct `Stream` now emits intermediate reasoning/tool snapshots and one terminal chunk. `Invoke`, `InvokeFast`, `Stream`, and `RunGenerator` share the execution loop; consumers stopping early must cancel their context.

### Deprecated

- `interfaces.Agent/Runnable` 为旧消息协议，新增编排统一使用 `core.Runnable[I,O]`；现有 API 保留迁移路径。
- 旧 `CrossEncoderReranker` 名称改为准确的 `LexicalOverlapReranker`，不代表实际交叉编码器推理。

⚠️ **Cache API Deprecation** - 简化缓存实现，减少过度设计

以下缓存函数已标记为 **Deprecated**，将在 v2.0.0 中移除：

- `cache.NewInMemoryCache()` - 请改用 `cache.NewSimpleCache()`
  - 理由：实现过于复杂，包含不必要的特性（maxSize、cleanupInterval 参数）
  - 迁移：将 `NewInMemoryCache(100, 5*time.Minute, 1*time.Minute)` 改为 `NewSimpleCache(5*time.Minute)`

- `cache.NewLRUCache()` - 请改用 `cache.NewSimpleCache()`
  - 理由：LRU 驱逐逻辑在实际场景中很少使用，SimpleCache 基于 TTL 的方式更简单有效
  - 迁移：将 `NewLRUCache(100, 5*time.Minute, 1*time.Minute)` 改为 `NewSimpleCache(5*time.Minute)`

- `cache.NewMultiTierCache()` - 请改用 `cache.NewSimpleCache()`
  - 理由：多级缓存在单进程应用中过于复杂，实际使用场景有限
  - 迁移：单进程应用使用 `NewSimpleCache()` 即可，分布式场景建议使用 Redis + 本地缓存方案

**向后兼容性**：所有 deprecated 函数在 v1.x 版本中仍然可用，不会影响现有代码。

**迁移指南**：
- 完整的迁移指南请参考 [`docs/guides/CACHING_GUIDE.md`](docs/guides/CACHING_GUIDE.md)
- 扫描工具：`go run tools/migrate-cache.go scan ./...`
- 自动迁移：`go run tools/migrate-cache.go replace ./...`

**相关变更**：
- 已将 `tools/middleware/caching.go` 从 `NewLRUCache` 迁移到 `NewSimpleCache` [commit: d7bd117]
- 已更新示例代码 `examples/integration/langchain-inspired/langchain_demo.go` 使用 `SimpleCache`
- 新增文档 `docs/guides/CACHING_GUIDE.md` - 详细的缓存使用指南
- 更新文档 `docs/guides/TOOL_MIDDLEWARE.md` - 推荐使用 `SimpleCache`

### Performance

- **utils/parser.go**: Pre-compile regular expressions to improve performance by 60-87%
  - Added 13 package-level pre-compiled regex variables
  - Optimized `RemoveMarkdown()` to avoid compiling 8 regexes on every call (85% faster)
  - Optimized `ExtractJSON()`, `ExtractList()`, `ExtractAllCodeBlocks()` methods (50-95% faster)
  - Added regex cache for dynamic patterns in `ExtractCodeBlock()`, `ExtractKeyValue()`, `ExtractSection()`
  - Reduced memory allocations by ~40-50%
  - Added comprehensive benchmark tests in `utils/parser_bench_test.go` (20 benchmarks)
  - Excellent concurrent performance: 2.5-9.6x speedup on 28-core CPU
  - Eliminated all staticcheck SA6000 warnings
  - See `/tmp/goagent-regex-analysis/performance_report.md` for detailed analysis

### Added - Advanced Reasoning Patterns

#### New Agent Types
- **Chain-of-Thought (CoT)** - Step-by-step reasoning with justification
  - Zero-shot and few-shot modes
  - Configurable reasoning depth
  - Self-verification support
  - Location: `agents/cot/`

- **Tree-of-Thought (ToT)** - Multi-path reasoning exploration
  - Multiple search strategies: DFS, BFS, Beam Search, MCTS
  - Configurable branching factor and depth
  - Thought evaluation and pruning
  - Parallel path exploration
  - Location: `agents/tot/`

- **Graph-of-Thought (GoT)** - DAG-based complex reasoning
  - Directed Acyclic Graph structure
  - Parallel node execution with topological sorting
  - Cycle detection
  - Multiple merge strategies (vote, weighted, LLM)
  - Location: `agents/got/`

- **Program-of-Thought (PoT)** - Code generation and execution
  - Multi-language support (Python, JavaScript, Go)
  - Safe mode with sandboxing
  - Code validation
  - Execution result capture
  - Location: `agents/pot/`

- **Skeleton-of-Thought (SoT)** - Fast long-form content generation
  - Skeleton generation and parallel elaboration
  - Dependency-aware scheduling
  - Multiple aggregation strategies (sequential, hierarchical, weighted)
  - Timeout control
  - Location: `agents/sot/`

- **Meta-CoT / Self-Ask** - Deep analysis with self-refinement
  - Automatic question decomposition
  - Recursive sub-question solving
  - Self-critique and improvement
  - Confidence estimation
  - Evidence collection and verification
  - Location: `agents/metacot/`

#### Builder API Extensions
- `WithChainOfThought()` - Create CoT agents
- `WithZeroShotCoT()` - Zero-shot CoT preset
- `WithFewShotCoT(examples)` - Few-shot CoT with examples
- `WithTreeOfThought()` - Create ToT agents
- `WithDFSToT()`, `WithBFSToT()` - Search strategy presets
- `WithBeamSearchToT(width, depth)` - Beam search configuration
- `WithMCTSToT(iterations)` - MCTS configuration
- `WithGraphOfThought()` - Create GoT agents
- `WithProgramOfThought()` - Create PoT agents
- `WithSkeletonOfThought()` - Create SoT agents
- `WithMetaCoT()` - Create Meta-CoT agents
- Location: `builder/reasoning_presets.go`

### Infrastructure

#### CI/CD
- **GitHub Actions Workflows**:
  - `ci.yml` - Continuous integration (test, lint, build)
  - `release.yml` - Automated releases on tag push
  - `pr.yml` - Pull request validation with coverage reporting
  - `nightly.yml` - Nightly builds and benchmarks
- **Automated Release Process**:
  - Multi-platform binary builds (Linux, macOS, Windows)
  - SHA256 checksum generation
  - Automatic GitHub Release creation
  - pkg.go.dev publication
- **Release Management**:
  - `scripts/create_release.sh` - Interactive release helper script
  - `.github/RELEASE.md` - Comprehensive release documentation
  - Pre-release support (alpha, beta, rc)

### Documentation

- **Comprehensive Agent Documentation** (`agents/README.md`):
  - Quick comparison table for all reasoning patterns
  - Decision tree for pattern selection
  - Detailed usage examples for each pattern
  - Configuration options reference
  - Performance comparison
  - Best practices guide
  - Testing instructions
- **Release Management Guide** (`.github/RELEASE.md`):
  - Semantic versioning guidelines
  - Step-by-step release process
  - Emergency hotfix procedures
  - Version compatibility matrix
  - Troubleshooting guide

### Changed

- **Import Layer Compliance**: Fixed Layer 1 import violations in `interfaces/` package
- **Test Suite**: Added comprehensive test coverage for all new reasoning patterns
- **Mock Implementations**: Improved test mocks for better interface compliance

### Fixed

- Import layering violations in `interfaces/memory.go` and `interfaces/tool.go`
- Test compilation errors in reasoning agent test files
- Mock setup issues in test suites

## [1.0.0] - 2025-11-15

### Added - Core Framework

#### Phase 1: Foundation
- **State Management** - Thread-safe state management with `core/state.go`
- **Runtime & Context** - Runtime environment and context propagation
- **Store System** - Long-term storage with hierarchical namespaces
  - InMemoryStore implementation
  - RedisStore for distributed systems
  - PostgresStore for persistent storage
- **Checkpointer** - Session persistence and recovery
  - InMemoryCheckpointer
  - RedisCheckpointer for distributed checkpointing
  - DistributedCheckpointer with high availability

#### Phase 2: Middleware & Business Logic
- **Middleware Framework** - Extensible middleware architecture
- **Advanced Middleware**:
  - DynamicPromptMiddleware - Dynamic prompt enhancement
  - ToolSelectorMiddleware - Intelligent tool selection
  - RateLimiterMiddleware - Rate limiting protection
  - AuthenticationMiddleware - Identity verification
  - ValidationMiddleware - Input validation
  - TransformMiddleware - Data transformation
  - CircuitBreakerMiddleware - Circuit breaker pattern
  - CacheMiddleware - Response caching
- **LLM Abstraction** - Multi-provider LLM support
  - OpenAI integration
  - Google Gemini integration
  - DeepSeek integration
- **Memory Management** - Conversation and case-based memory

#### Phase 3: Advanced Features
- **Agent Builder** - Fluent API for agent construction
- **Pre-configured Agent Templates**:
  - QuickAgent - Simple agent creation
  - RAGAgent - Retrieval-augmented generation
  - ChatAgent - Conversational agents
  - AnalysisAgent - Data analysis (low temperature, high precision)
  - WorkflowAgent - Workflow orchestration
  - MonitoringAgent - System monitoring
  - ResearchAgent - Research and information gathering
- **Vector Database** - Memory-based vector storage and RAG retrieval
- **Tool System**:
  - Parallel tool execution with worker pool
  - Tool dependency graph with topological sorting
  - LRU cache with TTL support
  - Tool registry
- **Stream Processing**:
  - Stream manager with buffering
  - Stream multiplexing
  - Rate limiting for streams
  - Stream transformations

#### Enterprise Features
- **OpenTelemetry Integration**:
  - Distributed tracing with W3C Trace Context
  - Metrics collection
  - Agent-specific tracer API
  - HTTP and NATS carrier propagation
- **Multi-Agent Communication**:
  - MemoryCommunicator for local communication
  - NATSCommunicator for distributed systems
  - Message routing with pattern matching
  - Session management
- **Observability Middleware** - Integration with tracing and metrics

### Architecture

- **4-Layer Architecture**:
  - Layer 1: Foundation (interfaces, errors, cache, utils)
  - Layer 2: Business Logic (core, LLM, memory, storage)
  - Layer 3: Implementation (agents, tools, middleware)
  - Layer 4: Examples and Tests
- **Import Layering** - Strict import rules to prevent circular dependencies
- **Verification Tools** - Automated import layering verification script

### Documentation

- **User Guides**:
  - Quick Start Guide
  - LangChain Integration Guide
  - LLM Provider Documentation
  - Migration Guide
  - Production Deployment Guide
- **Architecture Documentation**:
  - Architecture Overview
  - Import Layering Rules
  - Import Verification Guide
- **Development Documentation**:
  - Testing Best Practices
  - Test Coverage Reports
  - Contributing Guidelines
- **Examples**:
  - Basic usage examples
  - Advanced patterns
  - Integration examples
  - Streaming examples
  - Observability examples
  - Multi-agent examples

### Testing

- **Test Coverage**: >80% overall coverage
- **Test Suites**:
  - Unit tests for all core components
  - Integration tests for complex workflows
  - Benchmark tests for performance
- **Testing Tools**:
  - Mock implementations for testing
  - Test helpers and utilities

### Performance

- Builder construction: ~100μs/op
- Agent execution: ~1ms/op (excluding LLM calls)
- Middleware overhead: <5%
- Parallel tool execution: Linear scaling to 100+ concurrent
- Cache hit rate: >90% with LRU
- OpenTelemetry overhead: <2% at 10% sampling
- NATS messaging: <1ms latency, 1000+ msg/s throughput

### Changed

- Refactored from goagent monolithic architecture
- Extracted pkg/agent as standalone framework
- Reorganized documentation structure
- Consolidated import layering rules

### Fixed

- Memory leaks in checkpointer implementations
- Race conditions in state management
- Import circular dependency issues
- Tool execution timeout handling

### Security

- Added authentication middleware
- Implemented rate limiting
- Added input validation middleware
- Secure context propagation in distributed tracing

## [Unreleased]

### Planned Features

- Additional LLM providers (Anthropic Claude, Cohere, Hugging Face)
- Production vector database integration (Qdrant, Milvus, Pinecone)
- Graphical workflow designer
- Enhanced monitoring dashboard
- Agent versioning and A/B testing
- Performance optimizations (connection pooling, batch processing)

---

## Version History

### Version Numbering

GoAgent follows [Semantic Versioning](https://semver.org/):

- **MAJOR** version for incompatible API changes
- **MINOR** version for new functionality in a backward compatible manner
- **PATCH** version for backward compatible bug fixes

### Release Process

1. Update CHANGELOG.md with changes
2. Update version in code
3. Create git tag (e.g., v1.0.0)
4. Push tag to GitHub
5. Create GitHub release with notes
6. Update documentation

### Migration Guides

For breaking changes, see:
- [Migration Guide](docs/guides/MIGRATION_GUIDE.md) - Detailed upgrade instructions
- [Migration Summary](docs/guides/MIGRATION_SUMMARY.md) - Quick reference

---

**Note**: This is the initial release (1.0.0) extracted from the goagent project.
Historical development is documented in the [archive](docs/archive/) directory.

[1.0.0]: https://github.com/kart-io/goagent/releases/tag/v1.0.0
[Unreleased]: https://github.com/kart-io/goagent/compare/v1.0.0...HEAD
