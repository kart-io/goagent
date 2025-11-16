# Documentation Index

Complete guide to GoAgent documentation organized by category and expertise level.

## Getting Started

**New to GoAgent? Start here:**

1. **[README](README.md)** - Project overview, quick start, and key features
2. **[Quick Start Guide](docs/guides/quickstart.md)** - Get up and running in 5 minutes
3. **[Basic Examples](examples/basic/)** - Simple, working code examples

## Architecture & Design

**Understanding the system design:**

- **[Architecture Overview](docs/architecture/ARCHITECTURE.md)** - System architecture and package structure
- **[Import Layering](docs/architecture/IMPORT_LAYERING.md)** - 4-layer architecture and import rules
- **[Import Verification](docs/architecture/IMPORT_VERIFICATION.md)** - Tools and procedures for verifying import compliance

**Key Concepts:**
- Layer 1: Foundation (interfaces, errors, cache, utils)
- Layer 2: Business Logic (core, LLM, memory, storage)
- Layer 3: Implementation (agents, tools, middleware)
- Layer 4: Examples & Tests

## User Guides

**Practical guides for common tasks:**

### Core Functionality
- **[Quick Start](docs/guides/quickstart.md)** - Getting started with GoAgent
- **[LangChain Guide](docs/guides/langchain.md)** - LangChain-inspired features
- **[LangChain Summary](docs/guides/langchain-summary.md)** - Summary of LangChain integration
- **[LangChain Final](docs/guides/langchain-final.md)** - Complete LangChain feature set

### LLM Integration
- **[LLM Providers](docs/guides/LLM_PROVIDERS.md)** - Supported LLM providers (OpenAI, Gemini, DeepSeek)
- **[LLM Provider Consistency](docs/guides/LLM_PROVIDER_CONSISTENCY.md)** - Provider compatibility guide

### Migration & Deployment
- **[Migration Guide](docs/guides/MIGRATION_GUIDE.md)** - Upgrade between versions
- **[Migration Summary](docs/guides/MIGRATION_SUMMARY.md)** - Quick migration reference
- **[Production Deployment](docs/guides/PRODUCTION_DEPLOYMENT.md)** - Deploy GoAgent at scale

## Development

**For contributors and developers:**

- **[Testing Best Practices](docs/development/TESTING_BEST_PRACTICES.md)** - Writing effective tests
- **[Test Coverage Report](docs/development/TEST_COVERAGE_REPORT.md)** - Current test coverage and benchmarks
- **[Contributing Guidelines](CONTRIBUTING.md)** - How to contribute to GoAgent

**Code Quality:**
- Minimum 80% test coverage required
- Follow import layering rules (verify with `./verify_imports.sh`)
- All public APIs must have documentation
- Use `golangci-lint` for code quality checks

## Examples

**Learn by example:**

Located in `examples/`:

### Basic Examples
- **01-simple-agent** - Create and execute a basic agent
- **02-tools** - Use tools with agents
- **03-memory** - Implement memory management
- **04-builder** - Use the fluent builder API

### Advanced Examples
- **State Management** - Checkpointing and session persistence
- **Middleware** - Custom middleware implementation
- **Parallel Execution** - Concurrent tool execution
- **Streaming** - Real-time streaming responses

### Integration Examples
- **Observability** - OpenTelemetry tracing and metrics
- **Multi-agent** - Agent-to-agent communication
- **Production** - Production-ready configurations

**Run an example:**
```bash
go run examples/basic/01-simple-agent/main.go
```

## API Reference

**Detailed API documentation:**

Core interfaces and types:

- **Agent Interface** - Main agent abstraction
- **Tool Interface** - Tool system
- **Memory Manager** - Memory and conversation management
- **Builder API** - Fluent agent construction
- **Checkpointer** - State persistence
- **Observability** - Tracing and metrics

See package documentation with:
```bash
go doc github.com/kart-io/goagent/core
go doc github.com/kart-io/goagent/builder
```

## Analysis & Reports

**Historical documentation and analysis:**

### Code Analysis
Located in `docs/analysis/`:
- **[Code Structure](docs/analysis/code-structure.md)** - Codebase structure analysis
- **[Comprehensive Analysis](docs/analysis/comprehensive.md)** - Detailed code analysis
- **[Documents Index](docs/analysis/documents-index.md)** - Legacy documentation index

### Phase Reports
Located in `docs/archive/phase-reports/`:
- **Phase 1 Completion** - Initial architecture implementation
- **Phase 2 Completion** - Business logic implementation
- **Phase 3 Completion** - Advanced features and testing
- **Test Coverage Reports** - Detailed test coverage analysis

### Refactoring History
Located in `docs/archive/refactoring/`:
- Refactoring guides and completion reports
- Migration histories
- Task verification reports

### Roadmaps
Located in `docs/archive/roadmaps/`:
- **Improvement Roadmap Q1 2025** - Planned enhancements
- **Roadmap Checklists** - Feature implementation tracking
- **Roadmap Timeline** - Historical development timeline

## Quick Reference

### By Role

**Application Developer:**
1. [Quick Start](docs/guides/quickstart.md)
2. [Basic Examples](examples/basic/)
3. [LLM Providers](docs/guides/LLM_PROVIDERS.md)
4. [Production Deployment](docs/guides/PRODUCTION_DEPLOYMENT.md)

**Framework Contributor:**
1. [Architecture](docs/architecture/ARCHITECTURE.md)
2. [Import Layering](docs/architecture/IMPORT_LAYERING.md)
3. [Testing Best Practices](docs/development/TESTING_BEST_PRACTICES.md)
4. [Contributing Guidelines](CONTRIBUTING.md)

**System Architect:**
1. [Architecture Overview](docs/architecture/ARCHITECTURE.md)
2. [Import Layering](docs/architecture/IMPORT_LAYERING.md)
3. [Production Deployment](docs/guides/PRODUCTION_DEPLOYMENT.md)
4. [Test Coverage](docs/development/TEST_COVERAGE_REPORT.md)

### By Topic

**Agent Development:**
- [Quick Start](docs/guides/quickstart.md)
- [Builder API Examples](examples/basic/04-builder/)
- Agent interface documentation: `go doc github.com/kart-io/goagent/core.Agent`

**Tool Development:**
- [Tool Examples](examples/basic/02-tools/)
- [Parallel Execution](examples/advanced/)
- Tool interface: `go doc github.com/kart-io/goagent/tools`

**LLM Integration:**
- [LLM Providers](docs/guides/LLM_PROVIDERS.md)
- [Provider Consistency](docs/guides/LLM_PROVIDER_CONSISTENCY.md)
- LLM interface: `go doc github.com/kart-io/goagent/llm`

**State & Persistence:**
- [State Management Examples](examples/advanced/)
- Checkpointer: `go doc github.com/kart-io/goagent/core.Checkpointer`
- Store: `go doc github.com/kart-io/goagent/core.Store`

**Observability:**
- [Observability Examples](examples/observability/)
- [Production Deployment](docs/guides/PRODUCTION_DEPLOYMENT.md)
- OpenTelemetry: `go doc github.com/kart-io/goagent/observability`

**Multi-Agent Systems:**
- [Multi-agent Examples](examples/multiagent/)
- Communicator: `go doc github.com/kart-io/goagent/multiagent`

## Search Tips

### Find by Keywords

**Architecture:**
```bash
grep -r "architecture" docs/ --include="*.md"
```

**Performance:**
```bash
grep -r "benchmark\|performance" docs/ --include="*.md"
```

**Examples:**
```bash
find examples -name "*.go" | xargs grep -l "keyword"
```

**Tests:**
```bash
find . -name "*_test.go" | xargs grep -l "TestName"
```

## Document Conventions

- **README.md** - Always the starting point for any directory
- **UPPERCASE.md** - Important standalone documents (CONTRIBUTING, CHANGELOG)
- **Title-Case.md** - Specific guides and documentation
- **lowercase.md** - Supporting documentation

## Maintenance

This index is manually maintained. When adding new documentation:

1. Add entry to appropriate section above
2. Include brief description
3. Link to actual document
4. Update last modified date below

**Contributors:** Update this index when adding major documentation.

---

**Last Updated:** 2025-11-15
**Status:** Active
**Maintained By:** GoAgent Team
