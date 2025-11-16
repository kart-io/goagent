# Changelog

All notable changes to GoAgent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
  - `create_release.sh` - Interactive release helper script
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

- Refactored from k8s-agent monolithic architecture
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

**Note**: This is the initial release (1.0.0) extracted from the k8s-agent project.
Historical development is documented in the [archive](docs/archive/) directory.

[1.0.0]: https://github.com/kart-io/goagent/releases/tag/v1.0.0
[Unreleased]: https://github.com/kart-io/goagent/compare/v1.0.0...HEAD
