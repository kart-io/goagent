# GoAgent Architecture and Import Layering Rules

## Overview

GoAgent is a comprehensive agent framework with strict layering rules to ensure maintainability, testability, and clear dependency management. This document defines the import boundaries and dependency rules for all packages within this module.

## 4-Layer Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│ LAYER 4: Examples and Tests                                          │
│ (examples/, docs/, test files)                                       │
│ ├─ Imports from: All other layers (for testing/demonstration)       │
│ └─ Imports to:   None (leaf layer)                                  │
└──────────────────┬──────────────────────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────────────────────┐
│ LAYER 3: Implementation Layer                                        │
│ (agents/, middleware/, stream/, tools/, parsers/, etc.)             │
│ ├─ Imports from: Core + Interfaces (ONLY)                           │
│ ├─ Imports to:   None (no upward dependencies)                      │
│ └─ Cross-imports: Limited and controlled                            │
└──────────────────┬──────────────────────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────────────────────┐
│ LAYER 2: Business Logic and Builders                                │
│ (builder/, core/, llm/, memory/, store/, retrieval/)                │
│ ├─ Imports from: Interfaces, errors                                 │
│ ├─ Imports to:   Layer 3 can import from here                       │
│ └─ Purpose:      Core abstractions and main components              │
└──────────────────┬──────────────────────────────────────────────────┘
                   │
┌──────────────────▼──────────────────────────────────────────────────┐
│ LAYER 1: Foundational Layer                                         │
│ (interfaces/, errors/, utils/, cache/)                              │
│ ├─ Imports from: Only stdlib and external packages                  │
│ ├─ Imports to:   All other layers (one-way dependency)              │
│ └─ Purpose:      Base definitions and error types                   │
└─────────────────────────────────────────────────────────────────────┘
```

## Layer Definitions

### Layer 1: Foundational (Core Abstractions)

**Packages:**

- `interfaces/` - All public interface definitions
- `errors/` - Error types and helpers
- `cache/` - Basic caching utilities
- `utils/` - Utility functions (minimal logic)

**Strict Rules:**

1. **NO imports from any other GoAgent packages** (except stdlib and external)
2. These packages define the contracts used everywhere
3. No dependencies on implementation details
4. Version 1 of interfaces should remain stable

**Allowed Imports:**

```go
// ✅ GOOD - Only stdlib and external packages
import (
    "context"
    "github.com/kart-io/logger"
)

// ❌ BAD - Never import from other GoAgent packages
import "github.com/kart-io/goagent/core"
```

**Key Interfaces:**

```
interfaces/
├── agent.go          # Agent, Runnable
├── tool.go           # Tool
├── memory.go         # MemoryManager, Conversation, Case
├── store.go          # Store, Checkpointer, State
├── llm.go            # LLM Client
├── middleware.go     # Middleware
├── checkpoint.go     # Checkpoint definitions
└── execution.go      # Execution tracking
```

### Layer 2: Business Logic and Builders

**Packages:**

- `core/` - Base implementations (BaseAgent, BaseChain, BaseOrchestrator)
- `core/execution/` - Execution engine
- `core/state/` - State management
- `core/checkpoint/` - Checkpointing logic
- `core/middleware/` - Middleware framework
- `builder/` - Fluent API builders (AgentBuilder)
- `llm/` - LLM client implementations
- `memory/` - Memory management implementations
- `store/` - Store implementations (memory, redis, postgres)
- `retrieval/` - Document retrieval
- `observability/` - Telemetry and monitoring
- `performance/` - Performance utilities
- `planning/` - Planning utilities
- `prompt/` - Prompt engineering utilities
- `reflection/` - Reflection utilities

**Import Rules:**

1. **CAN import from Layer 1** (interfaces, errors, utils, cache)
2. **CANNOT import from Layer 3** (agents, middleware, tools, parsers, etc.)
3. **Cross-imports within Layer 2 are allowed** but must be carefully managed
4. **Must not create circular dependencies**

**Allowed Imports:**

```go
package core

import (
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/errors"
    "github.com/kart-io/goagent/cache"
)

// ❌ Never import Layer 3
import "github.com/kart-io/goagent/agents"
```

**Core Implementations:**

```
core/
├── agent.go          # Base Agent implementation
├── execution/        # Execution engine
├── state/            # State management
├── checkpoint/       # Checkpointing
├── middleware/       # Base Middleware
└── callback.go       # Callbacks

builder/
├── builder.go        # AgentBuilder main
└── phases/           # Builder phases

store/
├── memory/           # In-memory store
├── redis/            # Redis store
├── postgres/         # Postgres store
└── factory/          # Store factory

llm/
├── client.go         # Client interface
└── providers/        # Provider implementations
```

### Layer 3: Implementation Layer

**Packages:**

- `agents/` - Specific agent implementations (executor, react, specialized, etc.)
- `agents/executor/` - Executor agent
- `agents/react/` - ReAct reasoning agent
- `agents/specialized/` - Domain-specific agents
- `tools/` - Tool definitions and implementations
- `tools/shell/` - Shell tool
- `tools/http/` - HTTP tools
- `tools/search/` - Search tools
- `tools/practical/` - Practical tools
- `middleware/` - Middleware implementations
- `stream/` - Stream processing
- `parsers/` - Output parsers
- `multiagent/` - Multi-agent orchestration
- `distributed/` - Distributed execution
- `mcp/` - Model Context Protocol
- `toolkits/` - Tool collections
- `document/` - Document handling

**Import Rules:**

1. **CAN import from Layer 1 and Layer 2** (interfaces, errors, core, builder, etc.)
2. **CANNOT import from Layer 4** (examples, test files)
3. **Limited cross-imports within Layer 3** - documented below
4. **Must follow dependency direction** - no circular imports

**Allowed Imports:**

```go
package agents

import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"
)

// ❌ Never import upward to examples
import "github.com/kart-io/goagent/examples"
```

**Implementation Examples:**

```
agents/
├── executor/         # Tool execution agent
├── react/            # ReAct reasoning
└── specialized/      # Domain-specific

tools/
├── shell/            # Shell command execution
├── http/             # HTTP requests
├── search/           # Search operations
└── practical/        # File ops, DB, etc.

middleware/
├── observability.go
├── tool_selector.go
└── cache_middleware.go

parsers/
├── output_parser.go
└── parser_react.go
```

### Layer 4: Examples and Tests

**Packages:**

- `examples/` - Demonstration code
- `examples/basic/` - Basic usage patterns
- `examples/advanced/` - Advanced patterns
- `examples/integration/` - Integration examples
- `docs/` - Documentation
- `document/examples/` - Document examples

**Import Rules:**

1. **CAN import from all layers** (for demonstration purposes)
2. **Test files** can import from any package
3. **Must be self-contained** - no other code should import from here
4. **Purpose:** Teaching and verification only

**Allowed Imports:**

```go
package examples

import (
    // All imports allowed - this is for demonstration
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/agents"
    "github.com/kart-io/goagent/tools"
    "github.com/kart-io/goagent/builder"
)
```

## Import Dependency Matrix

This matrix shows which packages can import from which:

```
                      | Layer 1 | Layer 2 | Layer 3 | Layer 4 |
                      | (Core)  | (Logic) | (Impl)  | (Tests) |
──────────────────────┼─────────┼─────────┼─────────┼─────────┤
Layer 1 (interfaces)  |    -    |    X    |    X    |    X    |
Layer 2 (core, llm)   |    X    |    X*   |    X    |    X    |
Layer 3 (agents)      |    X    |    X    |    X*   |    X    |
Layer 4 (examples)    |    X    |    X    |    X    |    X    |
──────────────────────┴─────────┴─────────┴─────────┴─────────┘

Legend:
  -   = No self-import needed
  X   = Can import from this layer
  X*  = Can import, but limited/controlled cross-imports
```

## Cross-Layer Dependency Rules

### Between Layer 2 (Business Logic)

**Allowed Pattern:**

- `core/` → `core/execution`, `core/state`, `core/checkpoint`, `core/middleware`
- `builder/` → `core/`, `llm/`, `store/`, `memory/`
- `llm/` → `llm/providers/`
- `store/` → `store/adapters`, `store/factory`, `store/memory`, `store/redis`, `store/postgres`

**Forbidden Pattern:**

```go
// ❌ NEVER - Creates circular dependency
// In core/agent.go
import "github.com/kart-io/goagent/builder"

// And in builder/builder.go
import "github.com/kart-io/goagent/core"
```

### Between Layer 3 (Implementation)

**Allowed Pattern:**

```go
// agents/executor can import:
import (
    "github.com/kart-io/goagent/core"      // ✅
    "github.com/kart-io/goagent/interfaces" // ✅
    "github.com/kart-io/goagent/tools"     // ✅ (same layer)
)

// agents/react can import:
import (
    "github.com/kart-io/goagent/core"      // ✅
    "github.com/kart-io/goagent/parsers"   // ✅ (same layer)
)
```

**Forbidden Pattern:**

```go
// ❌ tools cannot import agents (except in tests)
import "github.com/kart-io/goagent/agents"

// ❌ parsers cannot import tools (except in tests)
import "github.com/kart-io/goagent/tools"
```

### Special Cases

#### mcp/ Package

- Located in Layer 3, but is semi-independent
- `mcp/` can import from `core/`, `interfaces/`, and `tools/`
- `mcp/` is generally NOT imported by other Layer 3 packages
- Used as alternative agent framework alongside agents/

#### document/ Package

- Can import `store/`, `llm/`, `core/`
- Primarily used for document processing
- Can be imported by agents/ and tools/

## Specific Package Import Rules

### core/

**Can import:**

```go
import (
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/errors"
    "github.com/kart-io/goagent/cache"
    // Subpackages
    "github.com/kart-io/goagent/core/execution"
    "github.com/kart-io/goagent/core/state"
    "github.com/kart-io/goagent/core/middleware"
    "github.com/kart-io/goagent/core/checkpoint"
)
```

**Cannot import:**

```go
// ❌ NEVER
import "github.com/kart-io/goagent/agents"
import "github.com/kart-io/goagent/tools"
import "github.com/kart-io/goagent/builder"
```

### builder/

**Can import:**

```go
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/core/execution"
    "github.com/kart-io/goagent/core/middleware"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/store"
    "github.com/kart-io/goagent/tools"
)
```

**Cannot import:**

```go
// ❌ NEVER
import "github.com/kart-io/goagent/agents"
import "github.com/kart-io/goagent/middleware"
```

### agents/

**Can import:**

```go
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"
    "github.com/kart-io/goagent/parsers"     // Similar layer
    "github.com/kart-io/goagent/memory"
    "github.com/kart-io/goagent/llm"
)
```

**Cannot import:**

```go
// ❌ NEVER
import "github.com/kart-io/goagent/builder"
import "github.com/kart-io/goagent/examples"
```

### tools/

**Can import:**

```go
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/cache"
)
```

**Cannot import:**

```go
// ❌ NEVER
import "github.com/kart-io/goagent/agents"
import "github.com/kart-io/goagent/parsers"
import "github.com/kart-io/goagent/middleware"
```

### middleware/

**Can import:**

```go
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/core/middleware"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/observability"
)
```

**Cannot import:**

```go
// ❌ NEVER
import "github.com/kart-io/goagent/agents"
import "github.com/kart-io/goagent/builder"
```

### parsers/

**Can import:**

```go
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
)
```

**Cannot import:**

```go
// ❌ NEVER
import "github.com/kart-io/goagent/tools"
import "github.com/kart-io/goagent/agents"
import "github.com/kart-io/goagent/middleware"
```

### llm/, memory/, store/, retrieval/

**Can import:**

```go
import (
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/errors"
    // Subpackages of own module
    "github.com/kart-io/goagent/store/memory"
)
```

**Cannot import:**

```go
// ❌ NEVER
import "github.com/kart-io/goagent/agents"
import "github.com/kart-io/goagent/tools"
import "github.com/kart-io/goagent/middleware"
```

## Dependency Visualization

```
┌─────────────────────────────────────────────────────────────────┐
│ interfaces/  ◄─── All public API contracts                      │
│ errors/      ◄─── Error types and helpers                       │
│ utils/       ◄─── Utility functions                             │
│ cache/       ◄─── Basic caching                                 │
└─────────────────────────────────────────────────────────────────┘
         ▲
         │ All packages depend on Layer 1
         │
┌────────┴─────────────────────────────────────────────────────────┐
│                      Layer 2: Business Logic                      │
├──────────────────────────────────────────────────────────────────┤
│                                                                    │
│  core/ ──► core/execution, core/state, core/checkpoint            │
│   ▲                                                               │
│   │                                                               │
│ builder/ ──────────────────►  llm/, memory/, store/               │
│   ▲                            ▲                                  │
│   │                            │                                 │
└───┼────────────────────────────┼──────────────────────────────────┘
    │                            │
    │ Imported by Layer 3        │ Imported by Layer 3
    │                            │
┌───┴────────────────────────────┴──────────────────────────────────┐
│                     Layer 3: Implementation                        │
├──────────────────────────────────────────────────────────────────┤
│                                                                    │
│ agents/ ──► tools/, parsers/, middleware/                         │
│                                                                   │
│ middleware/ ──► observability/                                    │
│                                                                   │
│ document/, distributed/, mcp/, multiagent/                        │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

## Good vs Bad Import Patterns

### ✅ GOOD PATTERNS

**Pattern 1: Core Implementation**

```go
// In core/agent.go
package core

import (
    "context"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/errors"
)

type BaseAgent struct {
    // Uses interfaces
}

func (a *BaseAgent) Execute(ctx context.Context, input *interfaces.Input) (*interfaces.Output, error) {
    // Implementation
    return nil, errors.Wrap(err, errors.CodeInternal, "execution failed")
}
```

**Pattern 2: Agent Implementation**

```go
// In agents/executor/executor_agent.go
package executor

import (
    "context"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"
)

type ExecutorAgent struct {
    *core.BaseAgent
    tools []tools.Tool
}

func (a *ExecutorAgent) Execute(ctx context.Context, input *interfaces.Input) (*interfaces.Output, error) {
    // Uses tools
    return nil, nil
}
```

**Pattern 3: Builder Pattern**

```go
// In builder/builder.go
package builder

import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/store"
    "github.com/kart-io/goagent/tools"
)

type AgentBuilder struct {
    llmClient llm.Client
    tools     []tools.Tool
}

func (b *AgentBuilder) WithLLM(client llm.Client) *AgentBuilder {
    b.llmClient = client
    return b
}
```

**Pattern 4: Type Aliases for Compatibility**

```go
// In memory/manager.go - For backward compatibility
package memory

import "github.com/kart-io/goagent/interfaces"

// Manager provides backward compatibility with interfaces.MemoryManager
type Manager = interfaces.MemoryManager
```

**Pattern 5: Test File with Full Access**

```go
// In agents/executor/executor_agent_test.go
package executor

import (
    "testing"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/agents"  // ✅ OK in tests
    "github.com/kart-io/goagent/tools"
)

func TestExecutorAgent(t *testing.T) {
    // Test implementation
}
```

### ❌ BAD PATTERNS

**Pattern 1: Circular Imports**

```go
// ❌ NEVER - Creates circular dependency

// In core/agent.go
package core
import "github.com/kart-io/goagent/builder"

// In builder/builder.go
package builder
import "github.com/kart-io/goagent/core"
```

**Pattern 2: Importing from Examples**

```go
// ❌ NEVER - Examples are for demonstration only

// In agents/some_agent.go
package agents
import "github.com/kart-io/goagent/examples"

type SomeAgent struct {
    example examples.MyExample  // ❌ BAD
}
```

**Pattern 3: Upward Imports (Implementation importing Builders)**

```go
// ❌ NEVER - Violates layering

// In tools/my_tool.go
package tools
import "github.com/kart-io/goagent/builder"  // ❌ BAD

type MyTool struct {
    builder *builder.AgentBuilder  // ❌ BAD
}
```

**Pattern 4: Cross-Implementation Imports Without Layer 2 Intermediary**

```go
// ❌ SHOULD BE AVOIDED - Consider if you need a Layer 2 abstraction

// In agents/agent.go
package agents
import "github.com/kart-io/goagent/tools"  // ✅ OK same layer

// But in tools/my_tool.go
package tools
import "github.com/kart-io/goagent/parsers"  // ⚠️ Be careful
```

**Pattern 5: Implementation Importing Upward Implementation**

```go
// ❌ NEVER - Tools should not import middleware or agents

// In tools/my_tool.go
package tools
import "github.com/kart-io/goagent/middleware"  // ❌ BAD
import "github.com/kart-io/goagent/agents"     // ❌ BAD
```

## Verifying Compliance

### Using go mod graph

Check for circular dependencies:

```bash
go mod graph | grep -E "goagent.*goagent"
```

### Using goimports

Auto-fix imports to canonical form:

```bash
goimports -w ./
```

### Manual audit

For a specific package, check all imports:

```bash
grep -r "^import" /path/to/package/*.go
```

### Linting rule

Add this to `.golangci.yml` to enforce import boundaries:

```yaml
depguard:
  list-type: blacklist
  include-test: false
  packages:
    - github.com/kart-io/goagent/examples:
        deny:
          - github.com/kart-io/goagent
```

## Migration Paths

### Adding New Functionality

1. **Define interface in Layer 1** (`interfaces/`)
2. **Implement in Layer 2** (core, llm, store, etc.)
3. **Use in Layer 3** (agents, tools, middleware)
4. **Document in examples** (Layer 4)

### Refactoring Old Code

1. **Identify problematic imports** (violating layering)
2. **Create Layer 2 abstraction** if missing
3. **Update implementation imports** to use Layer 2
4. **Test thoroughly**
5. **Update documentation**

### Deprecating Packages

1. **Define replacement interfaces** in Layer 1
2. **Create type aliases** in old package pointing to new
3. **Add deprecation comments**
4. **Update documentation**
5. **Plan removal timeline**

## Enforcement Strategy

### Pre-commit Hooks

Add a check to prevent committing bad imports:

```bash
#!/bin/bash
# Check for disallowed imports
grep -r "import.*agents" tools/ && exit 1
grep -r "import.*examples" ./*/[^e]*.go && exit 1
```

### CI/CD Pipeline

Automated checks in GitHub Actions:

```yaml
- name: Check import layering
  run: |
    make check-import-layering
```

### Code Review Guidelines

1. Verify imports follow Layer rules
2. Check for circular dependencies
3. Ensure new packages are placed at correct layer
4. Approve only if import structure is sound

## Future Improvements

### Import Analysis Tools

- Develop custom `importlint` tool for GoAgent
- Generate import compliance reports
- Visualize dependency graphs

### Documentation

- Keep this document updated with architecture changes
- Add diagrams to visualize relationships
- Maintain changelog of import rule changes

### Testing

- Add tests to verify import boundaries
- Use go test `-run TestImports` for validation
- Measure import cycle metrics

## Quick Reference

| Need             | Do This                            | Don't Do This                        |
| ---------------- | ---------------------------------- | ------------------------------------ |
| Define interface | Add to `interfaces/`               | Add to `core/`                       |
| Implement logic  | Use Layer 2 package                | Use Layer 3                          |
| Create agent     | Import from `core/`, `interfaces/` | Import from `agents/`, `examples/`   |
| Add tool         | Import from `core/`, `interfaces/` | Import from `agents/`, `middleware/` |
| Build agent      | Use `builder.AgentBuilder`         | Instantiate in Layer 3               |
| Test agent       | Can import anything                | Cannot depend on examples            |
| Example code     | Can import anything                | Never import in production code      |

## See Also

- [MIGRATION_GUIDE.md](../../docs/guides/MIGRATION_GUIDE.md) - Migration guide
- [README.md](../../README.md) - GoAgent usage guide
- [interfaces/](../../interfaces/) - All interface definitions
