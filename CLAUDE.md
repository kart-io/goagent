# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GoAgent is a comprehensive, production-ready AI agent framework for Go, inspired by LangChain. It provides agents, tools, memory, LLM abstraction, and orchestration capabilities with enterprise-grade features.

**Key Technologies:**
- Go 1.25.0+
- OpenTelemetry for observability
- Redis, PostgreSQL, NATS for distributed systems
- Multiple LLM providers (OpenAI, Gemini, DeepSeek)

## Development Commands

### Building and Testing

```bash
# Run all tests
make test
# or
go test ./...

# Run tests with race detection (default in Makefile)
go test -v -race -timeout 30s ./...

# Run short tests only
make test-short

# Run a single test
go test -v -run TestSpecificTest ./path/to/package

# Run integration tests
make test-integration

# Generate coverage report
make coverage

# View coverage in browser
make coverage-view
```

### Code Quality

```bash
# Format code
make fmt

# Run linter (CRITICAL: Must pass with 0 issues before committing)
make lint

# Run go vet
make vet

# Run all checks (fmt + vet + lint)
make check

# CRITICAL: Verify import layering compliance
./verify_imports.sh

# Strict mode (treat warnings as errors)
./verify_imports.sh --strict
```

**Important**: The linter catches common issues like:
- `ineffassign`: Ineffectual assignments
- `errcheck`: Unchecked error returns
- `staticcheck`: Various static analysis issues (e.g., deprecated functions, performance issues)
- `unused`: Unused variables, functions, or constants

See the "Lint Best Practices" section for detailed guidance on avoiding common lint issues.

### Building

```bash
# Build for current OS
make build

# Build for all platforms
make build-all

# Clean build artifacts
make clean
```

### Dependencies

```bash
# Download dependencies
make deps

# Update dependencies
make deps-update

# Tidy module dependencies
make mod-tidy
```

## Architecture: Strict 4-Layer Import System

**CRITICAL**: This project enforces a strict 4-layer architecture with automated verification. Violating import rules will cause CI failures.

### Layer 1: Foundation (No GoAgent imports)
```
interfaces/  - All public interface definitions
errors/      - Error types and helpers
cache/       - Basic caching utilities
utils/       - Utility functions
```

**Rule**: Layer 1 MUST NOT import from any other GoAgent packages (only stdlib and external deps).

### Layer 2: Business Logic (Import L1 only)
```
core/         - Base implementations (BaseAgent, BaseChain)
core/execution/ - Execution engine
core/state/   - State management
core/checkpoint/ - Checkpointing logic
core/middleware/ - Middleware framework
builder/      - Fluent API builders (AgentBuilder)
llm/          - LLM client implementations
memory/       - Memory management
store/        - Store implementations (memory, redis, postgres)
retrieval/    - Document retrieval
observability/ - Telemetry and monitoring
performance/  - Performance utilities
planning/     - Planning utilities
prompt/       - Prompt engineering
reflection/   - Reflection utilities
```

**Rule**: Layer 2 can import from Layer 1 only. Cross-imports within Layer 2 are allowed but must be carefully managed to avoid circular dependencies.

### Layer 3: Implementation (Import L1+L2 only)
```
agents/       - Agent implementations (executor, react, specialized)
tools/        - Tool definitions and implementations
middleware/   - Middleware implementations
parsers/      - Output parsers
stream/       - Stream processing
multiagent/   - Multi-agent orchestration
distributed/  - Distributed execution
mcp/          - Model Context Protocol
document/     - Document handling
toolkits/     - Tool collections
```

**Rules**:
- Layer 3 can import from Layer 1 and Layer 2
- Limited cross-imports within Layer 3 (e.g., agents can import tools)
- **tools/ MUST NOT import agents/, middleware/, or parsers/**
- **parsers/ MUST NOT import agents/, tools/, or middleware/**

### Layer 4: Examples & Tests (Import everything)
```
examples/     - Usage demonstrations
*_test.go     - Test files
```

**Rule**: Can import from all layers. No production code should import from Layer 4.

### Critical Import Restrictions

**NEVER DO THIS:**
```go
// ❌ Layer 1 importing from GoAgent
package interfaces
import "github.com/kart-io/goagent/core"

// ❌ Layer 2 importing from Layer 3
package core
import "github.com/kart-io/goagent/agents"

// ❌ tools importing agents
package tools
import "github.com/kart-io/goagent/agents"

// ❌ Circular dependency
package core
import "github.com/kart-io/goagent/builder"
// AND in builder:
import "github.com/kart-io/goagent/core"

// ❌ Production code importing examples
package agents
import "github.com/kart-io/goagent/examples"
```

**DO THIS INSTEAD:**
```go
// ✅ Layer 2 importing from Layer 1
package core
import (
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/errors"
)

// ✅ Layer 3 importing from Layer 1 and 2
package agents
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"  // Same layer is OK
)

// ✅ Tests can import anything
package agents_test
import (
    "github.com/kart-io/goagent/agents"
    "github.com/kart-io/goagent/tools"
)
```

### Verification Workflow

**ALWAYS run before committing:**
```bash
./verify_imports.sh
```

This script checks:
- Layer 1 has no GoAgent imports
- Layer 2 doesn't import Layer 3
- tools/ doesn't import agents/
- parsers/ doesn't import agents/ or middleware/
- No examples/ imports in production code
- No circular dependencies

## Core Architectural Concepts

### Agent Pattern
Agents are autonomous entities that can reason, use tools, and make decisions. They implement the `Agent` interface:

```go
type Agent interface {
    Runnable[*AgentInput, *AgentOutput]
    Name() string
    Description() string
    Capabilities() []string
}
```

### Builder Pattern
The fluent `AgentBuilder` is the primary way to construct agents:

```go
agent := builder.NewAgentBuilder(llmClient).
    WithSystemPrompt("You are a helpful assistant").
    WithTools(searchTool, calcTool).
    WithMemory(memoryManager).
    WithMiddleware(loggingMW, cacheMW).
    WithTimeout(30 * time.Second).
    Build()
```

### Runnable Pattern
Inspired by LangChain, `Runnable` provides a composable interface for all executable components:

```go
type Runnable[I, O any] interface {
    Invoke(ctx context.Context, input I, opts ...InvokeOption) (O, error)
    Stream(ctx context.Context, input I, opts ...StreamOption) (<-chan StreamEvent[O], error)
    Batch(ctx context.Context, inputs []I, opts ...InvokeOption) ([]O, error)
}
```

### State Management
State is managed through the `State` interface with thread-safe operations:
- `core/state/` - State implementations
- `core/checkpoint/` - Checkpointing for persistence
- Redis and PostgreSQL backends available

### Tool System
Tools are extensible functions that agents can invoke:

```go
type Tool interface {
    Name() string
    Description() string
    Execute(ctx context.Context, input map[string]interface{}) (interface{}, error)
}
```

Tools are registered in `tools/registry.go` and can execute in parallel.

### Middleware System
Middleware wraps agent execution to add cross-cutting concerns:

```go
type Middleware func(next Runnable[*AgentInput, *AgentOutput])
    Runnable[*AgentInput, *AgentOutput]
```

Common middleware: observability, caching, tool selection, rate limiting.

## Key Package Purposes

### core/
Base implementations and foundational logic. Contains:
- `BaseAgent` - Core agent implementation
- `execution/` - Execution runtime engine
- `state/` - State management
- `checkpoint/` - Checkpointing system
- `middleware/` - Middleware framework

### builder/
Fluent API for constructing complex agents. The primary public API for users.

### llm/
LLM client abstraction with multiple provider implementations:
- `llm/providers/openai.go` - OpenAI integration
- `llm/providers/gemini.go` - Google Gemini
- `llm/providers/deepseek.go` - DeepSeek

### agents/
Concrete agent implementations:
- `agents/executor/` - Tool execution agent
- `agents/react/` - ReAct reasoning agent
- `agents/specialized/` - Domain-specific agents

### tools/
Tool implementations organized by category:
- `tools/shell/` - Shell command execution
- `tools/http/` - HTTP requests
- `tools/search/` - Search operations
- `tools/practical/` - File ops, database queries, web scraping

### memory/
Conversation history and case-based reasoning:
- In-memory storage
- Vector store integration
- Short-term and long-term memory

### observability/
OpenTelemetry integration for distributed tracing, metrics, and logging.

### multiagent/
Multi-agent communication via NATS messaging with distributed coordination.

### distributed/
Distributed execution across multiple nodes with coordination and registry.

## Testing Requirements

### Coverage Standards
- **Minimum**: 80% coverage for all packages
- **New code**: Must include tests before PR merge
- **Critical paths**: Aim for >90% coverage

### Test Organization
- Table-driven tests for multiple cases
- Use `testify/assert` and `testify/require`
- Test files can import from any layer (Layer 4)
- Mock external dependencies (LLMs, databases, etc.)

### Running Tests
```bash
# Run all tests with race detection
go test -v -race ./...

# Test specific package
go test -v ./core

# Test with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run integration tests
go test -v -tags=integration ./...
```

## Code Style and Conventions

### Import Organization
```go
import (
    // Standard library first
    "context"
    "fmt"
    "time"

    // External dependencies
    "github.com/google/uuid"
    "github.com/stretchr/testify/assert"

    // Internal packages (respecting layer rules)
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/core"
)
```

### Documentation
- All exported types, functions, and constants MUST have doc comments
- Package-level documentation in `doc.go` or at package top
- Use examples in `examples/` directory

### Error Handling
Use the `errors/` package for consistent error handling:

```go
import "github.com/kart-io/goagent/errors"

return errors.Wrap(err, errors.CodeInternal, "execution failed")
```

### Naming Conventions
- Public: PascalCase (Agent, ToolExecutor)
- Private: camelCase (baseAgent, executeInternal)
- Interfaces: Name the capability (Agent, Runnable, Tool)
- Implementations: Descriptive (ExecutorAgent, ReactAgent)

### Lint Best Practices

**CRITICAL**: Always write lint-compliant code from the start to avoid rework. The project uses `golangci-lint` with strict rules.

#### Common Lint Issues and How to Avoid Them

**1. Ineffectual Assignment (ineffassign)**
```go
// ❌ BAD: Variable assigned but immediately reassigned
if err != nil {
    result = getDefault()  // This assignment is wasted
}
result = parseOutput(output)  // Result always reassigned

// ✅ GOOD: Only assign when needed
if err == nil {
    result = parseOutput(output)
} else {
    result = getDefault()
}
```

**2. Regexp in Loop (staticcheck SA6000)**
```go
// ❌ BAD: Compiling regex in every iteration
for _, line := range lines {
    if matched, _ := regexp.MatchString(`^\d+$`, line); matched {
        // Process line
    }
}

// ✅ GOOD: Pre-compile regex outside loop
digitPattern := regexp.MustCompile(`^\d+$`)
for _, line := range lines {
    if digitPattern.MatchString(line) {
        // Process line
    }
}
```

**3. Deprecated Functions (staticcheck SA1019)**
```go
// ❌ BAD: Using deprecated strings.Title
import "strings"
title := strings.Title(name)

// ✅ GOOD: Use golang.org/x/text/cases
import (
    "golang.org/x/text/cases"
    "golang.org/x/text/language"
)
title := cases.Title(language.English).String(name)
```

**4. Unused Variables and Return Values**
```go
// ❌ BAD: Ignoring error return values
data, _ := ioutil.ReadFile(path)

// ✅ GOOD: Always handle errors
data, err := ioutil.ReadFile(path)
if err != nil {
    return errors.Wrap(err, errors.CodeInternal, "failed to read file")
}

// ❌ BAD: Unused variable
func process() {
    result := calculate()  // Never used
    return
}

// ✅ GOOD: Remove unused variables or use them
func process() int {
    result := calculate()
    return result
}
```

**5. Error Checking (errcheck)**
```go
// ❌ BAD: Not checking error from important operations
file.Close()
rows.Close()

// ✅ GOOD: Defer with error check
defer func() {
    if err := file.Close(); err != nil {
        log.Error("failed to close file", err)
    }
}()
```

**6. Context Usage**
```go
// ❌ BAD: Not passing context through call chain
func fetchData() ([]byte, error) {
    resp, err := http.Get(url)
    // ...
}

// ✅ GOOD: Always accept and pass context
func fetchData(ctx context.Context) ([]byte, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    // ...
}
```

**7. String Formatting**
```go
// ❌ BAD: Using + for complex string building
msg := "Error: " + err.Error() + " at " + time.Now().String()

// ✅ GOOD: Use fmt.Sprintf or strings.Builder
msg := fmt.Sprintf("Error: %v at %v", err, time.Now())

// For multiple concatenations in a loop
var builder strings.Builder
for _, item := range items {
    builder.WriteString(item)
}
result := builder.String()
```

#### Pre-Generation Checklist

Before generating or modifying code, ensure:

1. **Variables are used**: Every declared variable must be used
2. **Errors are handled**: Never ignore error return values with `_`
3. **Regex is pre-compiled**: If using regex in loops, compile once outside
4. **No deprecated functions**: Check if stdlib functions are deprecated
5. **Context is propagated**: All I/O operations should accept `context.Context`
6. **Assignments are effective**: Variables shouldn't be assigned then immediately reassigned
7. **Imports are organized**: Stdlib → External → Internal (respecting layer rules)
8. **Comments for exports**: All exported items must have doc comments

#### Running Lint

```bash
# Always run before committing
make lint

# Fix auto-fixable issues
golangci-lint run --fix ./...

# Run specific linters
golangci-lint run --disable-all --enable=ineffassign,errcheck,staticcheck ./...
```

#### Integration with Development Workflow

When writing code:
1. Write lint-compliant code from the start
2. Run `make lint` after completing each feature
3. Fix issues immediately - don't accumulate lint debt
4. If lint fails in CI, it blocks the PR

Common workflow:
```bash
# After writing code
make fmt              # Format code
make lint             # Check for issues
./verify_imports.sh   # Verify import layering
make test             # Run tests
```

## Common Development Patterns

### Adding a New Tool
1. Create in `tools/[category]/` (Layer 3)
2. Implement `Tool` interface from `interfaces/`
3. Can import from `core/`, `interfaces/`, `cache/`
4. Register in `tools/registry.go`
5. Add tests in `*_test.go`
6. Verify imports: `./verify_imports.sh`

### Adding a New Agent
1. Create in `agents/[type]/` (Layer 3)
2. Embed or compose with `core.BaseAgent`
3. Import from `core/`, `interfaces/`, `tools/`, `memory/`, `llm/`
4. Add to builder presets if appropriate
5. Include comprehensive tests
6. Verify imports

### Adding a New LLM Provider
1. Create in `llm/providers/` (Layer 2)
2. Implement `llm.Client` interface
3. Import only from `interfaces/`, `errors/`
4. Add provider-specific configuration
5. Update `docs/guides/LLM_PROVIDERS.md`

### Refactoring Across Layers
If you need to move code between layers:
1. Identify the correct layer based on function
2. Move code to new location
3. Update imports in dependent packages
4. Add type aliases in old location for backward compatibility (if needed)
5. Run `./verify_imports.sh`
6. Update tests and documentation

## Common Pitfalls to Avoid

1. **Importing from wrong layer**: Always check layer rules before adding imports
2. **Circular dependencies**: Use interfaces in Layer 1 to break circles
3. **Skipping import verification**: Always run `./verify_imports.sh` before committing
4. **Low test coverage**: Ensure new code has 80%+ coverage
5. **Missing documentation**: All exported APIs must have doc comments
6. **Importing examples/ in production**: Examples are Layer 4 - never import in production code
7. **Writing lint-violating code**: Always follow lint best practices from the start (see "Lint Best Practices" section)
8. **Ignoring error returns**: Never use `_` to ignore errors from important operations
9. **Using deprecated functions**: Check for deprecation warnings before using stdlib functions
10. **Regex in loops**: Pre-compile regex patterns outside loops for performance

## Performance Considerations

From benchmarks:
- Builder construction: ~100μs/op
- Agent execution: ~1ms/op (excluding LLM calls)
- Middleware overhead: <5%
- Parallel tool execution: Linear scaling to 100+ concurrent calls
- Cache hit rate: >90% with LRU caching

Optimize for:
- Parallel tool execution when possible
- Proper context cancellation
- Efficient state updates
- Minimal middleware stack

## Documentation Structure

- `README.md` - Project overview and quick start
- `DOCUMENTATION_INDEX.md` - Complete documentation guide
- `docs/architecture/` - Architecture and design docs
- `docs/guides/` - User guides (quickstart, migration, LLM providers)
- `docs/development/` - Development guidelines and test practices
- `examples/` - Working code examples (basic, advanced, integration)

## External Dependencies

Key external packages:
- `github.com/sashabaranov/go-openai` - OpenAI client
- `cloud.google.com/go/vertexai` - Google Gemini
- `github.com/redis/go-redis/v9` - Redis client
- `gorm.io/gorm` - Database ORM
- `github.com/nats-io/nats.go` - NATS messaging
- `go.opentelemetry.io/otel` - Observability

## CI/CD Considerations

**CRITICAL**: All code must pass lint checks before being committed. Zero tolerance for lint errors.

Before pushing:
1. Run `make check` (fmt + vet + lint) - **MUST pass with 0 issues**
2. Run `go test ./...` - All tests must pass
3. Run `./verify_imports.sh` - No import layer violations
4. Ensure coverage meets minimum 80%

Recommended pre-commit workflow:
```bash
make fmt && make lint && ./verify_imports.sh && make test
```

CI will fail on:
- **Lint errors** (ineffassign, errcheck, staticcheck, etc.) - Zero tolerance
- Import layering violations
- Test failures
- Insufficient coverage (<80%)

**Pro Tip**: Set up a git pre-commit hook to run `make lint` automatically:
```bash
# .git/hooks/pre-commit
#!/bin/sh
make lint || {
    echo "❌ Lint failed. Fix issues before committing."
    exit 1
}
```

## Getting Help

- Architecture questions: See `docs/architecture/ARCHITECTURE.md`
- Import layering: See `docs/architecture/IMPORT_LAYERING.md`
- Testing: See `docs/development/TESTING_BEST_PRACTICES.md`
- Migration: See `docs/guides/MIGRATION_GUIDE.md`
- Examples: Check `examples/` directory
