# Import Layering Architecture

## Overview

This document defines the import layering architecture for GoAgent to ensure maintainability, prevent circular dependencies, and enforce clear architectural boundaries.

## 4-Layer Architecture

```
LAYER 1: Foundational (No GoAgent imports)
├─ interfaces/ ─────── All public interfaces
├─ errors/ ─────────── Error types and helpers
├─ cache/ ──────────── Basic caching
└─ utils/ ──────────── Utility functions

LAYER 2: Business Logic (Import L1 only)
├─ core/ ────────────── Base implementations
├─ builder/ ─────────── Fluent API builders
├─ llm/ ─────────────── LLM implementations
├─ memory/ ──────────── Memory management
├─ store/ ──────────── Storage layer
└─ [retrieval, observability, performance, etc.]

LAYER 3: Implementation (Import L1+L2 only)
├─ agents/ ──────────── Agent implementations
├─ tools/ ──────────── Tool implementations
├─ middleware/ ──────── Middleware impls
├─ parsers/ ─────────── Output parsers
├─ stream/ ─────────── Stream processing
└─ [multiagent, distributed, mcp, etc.]

LAYER 4: Examples & Tests (Import everything)
├─ examples/ ───────── Usage examples
└─ *_test.go ───────── Test files
```

## Essential Rules

### Rule 1: Layer 1 Independence
```
interfaces/, errors/, cache/, utils/
MUST NOT import from any other GoAgent packages
```

### Rule 2: No Upward Imports
```
Layer 3 packages (agents, tools, middleware, parsers)
MUST NOT import from examples/ or Layer 4
```

### Rule 3: Core Protection
```
Layer 2 (core, builder)
MUST NOT import from Layer 3
```

### Rule 4: Tool Isolation
```
tools/
MUST NOT import from agents/, middleware/, or parsers/
```

### Rule 5: No Circular Dependencies
```
If A imports B, then B MUST NOT import A (transitively)
```

## Import Allowance Matrix

```
                      | Can Import From
Package/Layer         | L1  | L2  | L3  | L4
─────────────────────┼─────┼─────┼─────┼──────
Layer 1 (interfaces)  | -   | ✗   | ✗   | ✗
Layer 2 (core)        | ✓   | ✓*  | ✗   | ✗
Layer 3 (agents)      | ✓   | ✓   | ✓*  | ✗
Layer 4 (examples)    | ✓   | ✓   | ✓   | ✓
─────────────────────┴─────┴─────┴─────┴──────

Legend:
✓   = Allowed (unrestricted)
✓*  = Allowed (with restrictions/documentation)
✗   = Not allowed
-   = Not applicable
```

## Layer Definitions

### Layer 1: Foundation

**Packages:** `interfaces/`, `errors/`, `cache/`, `utils/`

**Purpose:** Provide foundational types and utilities with zero dependencies on other pkg/agent packages.

**Rules:**
- MUST NOT import from any other pkg/agent packages
- Can import stdlib and external packages only
- Provides interfaces, error types, and basic utilities

**Examples:**
```go
// Good: Layer 1 package (interfaces/agent.go)
package interfaces

import (
    "context"
)

type Agent interface {
    Execute(ctx context.Context, input *AgentInput) (*AgentOutput, error)
    Name() string
}

// Bad: Layer 1 importing from pkg/agent
package interfaces

import (
    "github.com/kart-io/goagent/core"  // ✗ VIOLATION
)
```

### Layer 2: Business Logic

**Packages:** `core/`, `builder/`, `llm/`, `memory/`, `store/`, `retrieval/`, `observability/`, `performance/`, `planning/`, `prompt/`, `reflection/`

**Purpose:** Implement core business logic and provide infrastructure for Layer 3.

**Rules:**
- Import ONLY from Layer 1
- Cross-imports within Layer 2 are allowed (carefully managed)
- MUST NOT import from Layer 3
- Export interfaces and implementations to Layer 3

**Examples:**
```go
// Good: Layer 2 package (core/agent.go)
package core

import (
    "github.com/kart-io/goagent/interfaces"  // ✓ Layer 1
    "github.com/kart-io/goagent/errors"      // ✓ Layer 1
    "github.com/kart-io/goagent/cache"       // ✓ Layer 1
)

type BaseAgent struct {
    // ...
}

// Bad: Layer 2 importing from Layer 3
package core

import (
    "github.com/kart-io/goagent/agents"  // ✗ VIOLATION
)
```

### Layer 3: Implementation

**Packages:** `agents/`, `tools/`, `middleware/`, `parsers/`, `stream/`, `multiagent/`, `distributed/`, `mcp/`, `document/`, `toolkits/`

**Purpose:** Provide specific implementations of agents, tools, and middleware.

**Rules:**
- Import from Layer 1 and Layer 2
- Limited cross-imports within Layer 3 (documented exceptions)
- MUST NOT import from Layer 4 (examples)
- MUST NOT create upward dependencies

**Examples:**
```go
// Good: Layer 3 package (agents/executor/executor_agent.go)
package executor

import (
    "github.com/kart-io/goagent/core"        // ✓ Layer 2
    "github.com/kart-io/goagent/interfaces"  // ✓ Layer 1
    "github.com/kart-io/goagent/tools"       // ✓ Same layer
)

// Bad: Layer 3 importing from Layer 4
package agents

import (
    "github.com/kart-io/goagent/examples"  // ✗ VIOLATION
)

// Bad: tools importing agents
package tools

import (
    "github.com/kart-io/goagent/agents"  // ✗ VIOLATION
)
```

### Layer 4: Examples and Tests

**Packages:** `examples/`, `*_test.go` files

**Purpose:** Demonstrate usage and test functionality.

**Rules:**
- Can import from ALL layers
- Nothing imports from examples (one-way dependency)
- Test files can cross-import for testing purposes

**Examples:**
```go
// Good: Example file
package main

import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/agents"
    "github.com/kart-io/goagent/tools"
)

func main() {
    // Demonstrate usage
}
```

## Detailed Dependency Map

### Layer 1: Foundation

```
interfaces/
├── imports: stdlib + external only
├── exports: Agent, Tool, Memory, Store, LLM, Middleware interfaces
└── no circular dependencies

errors/
├── imports: stdlib + external only
├── exports: Error types, helpers
└── independent

cache/
├── imports: stdlib + external only
├── exports: Cache implementations
└── independent

utils/
├── imports: stdlib + external only
├── exports: Utility functions
└── independent
```

### Layer 2: Business Logic

**core/**
```
core/
├── agent.go
│   └── imports: interfaces/, errors/, cache/
├── execution/
│   └── imports: interfaces/, errors/
├── state/
│   └── imports: interfaces/, errors/
├── checkpoint/
│   └── imports: interfaces/, errors/
├── middleware/
│   └── imports: interfaces/, errors/
└── NO imports from: agents/, tools/, middleware/, parsers/, builder/
```

**builder/**
```
builder/
├── builder.go
│   ├── imports: core/, llm/, store/, tools/, interfaces/, errors/
│   ├── imports: core/execution, core/middleware
│   └── cross-imports: memory/ (for type access)
└── NO imports from: agents/, middleware/, examples/
```

**llm/**
```
llm/
├── client.go
│   └── imports: interfaces/, errors/
├── providers/
│   └── imports: llm/ (parent), interfaces/, errors/
└── NO imports from: agents/, tools/, builder/
```

**memory/**
```
memory/
├── manager.go
│   └── imports: interfaces/, errors/
└── NO imports from: agents/, tools/, builder/
```

**store/**
```
store/
├── store.go
│   └── imports: interfaces/, errors/
├── memory/
│   └── imports: store/ (parent), interfaces/
├── redis/
│   └── imports: store/ (parent), interfaces/
├── postgres/
│   └── imports: store/ (parent), interfaces/
└── NO imports from: agents/, tools/, middleware/
```

### Layer 3: Implementation

**agents/**
```
agents/
├── executor/executor_agent.go
│   ├── imports: core/, interfaces/, tools/
│   └── imports: memory/, llm/ (Layer 2)
├── react/react_agent.go
│   ├── imports: core/, interfaces/
│   └── imports: parsers/ (same layer)
├── specialized/
│   ├── imports: core/, interfaces/
│   └── imports: tools/ (same layer)
└── NO imports from: builder/, middleware/, examples/
```

**tools/**
```
tools/
├── tool.go (interface only)
│   └── imports: interfaces/, errors/
├── registry.go
│   └── imports: tools.Tool (local), sync
├── shell/shell_tool.go
│   ├── imports: interfaces/, errors/
│   └── imports: core/ (for types)
├── http/http_tool.go
│   ├── imports: interfaces/, errors/
│   └── imports: cache/ (Layer 1)
└── NO imports from: agents/, middleware/, parsers/
```

**middleware/**
```
middleware/
├── observability.go
│   ├── imports: core/, core/middleware/
│   └── imports: observability/ (Layer 2)
├── tool_selector.go
│   ├── imports: core/, core/middleware/
│   └── imports: tools/ (same layer)
└── NO imports from: agents/, builder/, examples/
```

**parsers/**
```
parsers/
├── output_parser.go
│   └── imports: interfaces/, errors/
├── parser_react.go
│   └── imports: interfaces/, errors/, core/
└── NO imports from: agents/, tools/, middleware/
```

## Common Refactoring Scenarios

### Scenario 1: Moving Code Between Layers

**Problem:** You have code in Layer 3 that belongs in Layer 2.

**Solution:**

1. Create/update Layer 2 package (e.g., `planning/`)
2. Move code to Layer 2
3. Update Layer 3 imports to use Layer 2
4. Keep type aliases in Layer 3 for backward compatibility if needed

```go
// Before: In agents/planning.go (Layer 3)
package agents
func PlanExecution() { /* ... */ }

// After: In planning/executor.go (Layer 2)
package planning
func ExecutionPlan() { /* ... */ }

// Backward compat: In agents/planning.go (Layer 3)
package agents
import "github.com/kart-io/goagent/planning"
var PlanExecution = planning.ExecutionPlan
```

### Scenario 2: Circular Dependency

**Problem:** Package A imports B, B imports A.

**Solution:** Extract common interface to Layer 1.

```go
// Problem:
// core/agent.go imports builder/
// builder/builder.go imports core/

// Solution: Define interface in Layer 1
// interfaces/builder.go - New interface
type Builder interface {
    Build(ctx context.Context) (Agent, error)
}

// Now both can depend on interfaces without circular imports
```

### Scenario 3: Tool Needs Feature from Middleware

**Problem:** `tools/my_tool.go` needs functionality from `middleware/`.

**Solution:** Extract to Layer 2 if it's general purpose.

```go
// Problem:
// tools/my_tool.go wants middleware functionality

// Solution:
// 1. If it's general: Create performance/ or observability/ in Layer 2
// 2. Move code to Layer 2
// 3. tools/ imports from Layer 2

// Before:
import "github.com/kart-io/goagent/middleware"

// After:
import "github.com/kart-io/goagent/observability"
```

### Scenario 4: Adding a New Tool

1. Put in `tools/` (Layer 3)
2. Import from: `core/`, `interfaces/`, `cache/`
3. Can use: `tools/registry.go` for registration
4. Run verification

### Scenario 5: Adding LLM Provider

1. Put in `llm/providers/` (Layer 2)
2. Import from: `interfaces/`, `errors/`, parent `llm/`
3. Exported through: `llm.Client` interface
4. Used in: Layer 3 agents

## Good vs Bad Import Patterns

### Good Patterns

```go
// Layer 1 → No pkg/agent imports
package interfaces
import "context"

// Layer 2 → Import Layer 1
package core
import "github.com/kart-io/goagent/interfaces"

// Layer 3 → Import Layer 1 & 2
package agents
import (
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/core"
)

// Examples → Import everything
package main
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/agents"
)
```

### Bad Patterns

```go
// ✗ Layer 1 importing from pkg/agent
package interfaces
import "github.com/kart-io/goagent/core"

// ✗ Layer 2 importing from Layer 3
package core
import "github.com/kart-io/goagent/agents"

// ✗ Layer 3 importing from examples
package agents
import "github.com/kart-io/goagent/examples"

// ✗ tools importing agents
package tools
import "github.com/kart-io/goagent/agents"

// ✗ Circular dependency
package core
import "github.com/kart-io/goagent/builder"
// AND
package builder
import "github.com/kart-io/goagent/core"
```

## How to Use

### For Architects/Tech Leads

1. Review the 4-layer architecture overview
2. Review the layer definitions and rules
3. Review the import dependency matrix
4. Use verification procedures for audits

### For Developers Adding New Code

1. Determine the package's layer based on its function
2. Check the specific package import rules for that layer
3. Review good vs bad import patterns
4. Test with verification script

### For Code Reviewers

1. Use the Import Audit Checklist
2. Run verification script on PRs
3. Reference layer rules when asking for changes
4. Check specific package rules

## See Also

- [Import Verification Guide](./IMPORT_VERIFICATION.md) - Verification procedures and tools
- [README.md](../../README.md) - Package usage guide
- [MIGRATION_GUIDE.md](../guides/MIGRATION_GUIDE.md) - Migration procedures
- [interfaces/](../../interfaces/) - All interface definitions
- `examples/` - Usage examples

---

**Version:** 1.0
**Last Updated:** 2025-11-15
**Status:** Production Ready
