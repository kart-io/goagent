# Migration Guide: GoAgent Refactoring

## Overview

This guide helps you migrate from the old pkg/agent package structure to the new refactored structure introduced in **v0.10.0**. The refactoring maintains full backward compatibility, so your existing code will continue to work without modification.

**Important**: While old import paths still work, we recommend migrating to the new structure for long-term maintainability. Type aliases will be removed in **v1.0.0**.

## Quick Reference Table

### Interface Imports

| Old Import                                         | New Import                                            | Deprecated In | Removed In |
| -------------------------------------------------- | ----------------------------------------------------- | ------------- | ---------- |
| `github.com/kart-io/goagent/core.Agent`            | `github.com/kart-io/goagent/interfaces.Agent`         | v0.10.0       | v1.0.0     |
| `github.com/kart-io/goagent/core.Runnable`         | `github.com/kart-io/goagent/interfaces.Runnable`      | v0.10.0       | v1.0.0     |
| `github.com/kart-io/goagent/retrieval.VectorStore` | `github.com/kart-io/goagent/interfaces.VectorStore`   | v0.10.0       | v1.0.0     |
| `github.com/kart-io/goagent/retrieval.Document`    | `github.com/kart-io/goagent/interfaces.Document`      | v0.10.0       | v1.0.0     |
| `github.com/kart-io/goagent/memory.Manager`        | `github.com/kart-io/goagent/interfaces.MemoryManager` | v0.10.0       | v1.0.0     |
| `github.com/kart-io/goagent/memory.Conversation`   | `github.com/kart-io/goagent/interfaces.Conversation`  | v0.10.0       | v1.0.0     |
| `github.com/kart-io/goagent/memory.Case`           | `github.com/kart-io/goagent/interfaces.Case`          | v0.10.0       | v1.0.0     |
| `github.com/kart-io/goagent/core.Checkpointer`     | `github.com/kart-io/goagent/interfaces.Checkpointer`  | v0.10.0       | v1.0.0     |
| `github.com/kart-io/goagent/tools.Tool`            | `github.com/kart-io/goagent/interfaces.Tool`          | v0.10.0       | v1.0.0     |
| `github.com/kart-io/goagent/core.Store`            | `github.com/kart-io/goagent/interfaces.Store`         | v0.10.0       | v1.0.0     |

### Core Sub-Package Imports

| Old Import                                      | New Import                                   | Deprecated In | Removed In |
| ----------------------------------------------- | -------------------------------------------- | ------------- | ---------- |
| `github.com/kart-io/goagent/core` (state types) | `github.com/kart-io/goagent/core/state`      | v0.11.0       | v1.0.0     |
| `github.com/kart-io/goagent/core.Checkpointer*` | `github.com/kart-io/goagent/core/checkpoint` | v0.11.0       | v1.0.0     |
| `github.com/kart-io/goagent/core.Runtime`       | `github.com/kart-io/goagent/core/execution`  | v0.11.0       | v1.0.0     |
| `github.com/kart-io/goagent/core.Middleware`    | `github.com/kart-io/goagent/core/middleware` | v0.11.0       | v1.0.0     |

### File Renames (No Import Changes)

| Old File                           | New File                               | Change Type            |
| ---------------------------------- | -------------------------------------- | ---------------------- |
| `tools/runtime.go`                 | `tools/tool_runtime.go`                | Renamed (same package) |
| `store/postgres/config.go`         | `store/postgres/postgres_config.go`    | Renamed (same package) |
| `store/redis/config.go`            | `store/redis/redis_config.go`          | Renamed (same package) |
| `middleware/advanced.go`           | `middleware/tool_selector_advanced.go` | Renamed (same package) |
| `memory/vector_store_memory.go`    | `memory/memory_vector_store.go`        | Renamed (same package) |
| `retrieval/vector_store_memory.go` | `retrieval/retrieval_memory_store.go`  | Renamed (same package) |

## Why This Change?

### Before Refactoring (Problems)

1. **Interface Duplication**: VectorStore defined in 2+ incompatible locations
2. **Core Package Bloat**: 9,465 lines in single package violating Single Responsibility
3. **Filename Collisions**: 12 duplicate filenames causing confusion
4. **Documentation Chaos**: 26 Markdown files in root directory
5. **Misplaced Components**: Agent implementation in tools package
6. **Test Coverage Gaps**: Multiple packages at 0% coverage

### After Refactoring (Benefits)

1. **Single Source of Truth**: All interfaces in canonical `interfaces/` package
2. **Focused Packages**: Core split into 4 sub-packages, each under 2,500 lines
3. **Unique Filenames**: 100% unique filenames across codebase
4. **Organized Documentation**: 4-category structure (92% root reduction)
5. **Clear Boundaries**: Proper agent/tool separation
6. **Comprehensive Tests**: +3,786 lines of tests, 8 bugs fixed

## Migration Strategies

### Strategy 1: No Migration (Stay on Old Paths)

**Best for**: Projects that need zero changes, short-term compatibility

**Approach**: Do nothing - your existing code continues to work

**Example**:

```go
// This still works in v0.10.0 - v0.14.0
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/retrieval"
)

func MyFunction(agent core.Agent, store retrieval.VectorStore) {
    // Your existing code works without changes
}
```

**Warning**: This code will break in **v1.0.0** when type aliases are removed.

### Strategy 2: Gradual Migration (Recommended)

**Best for**: Most projects, production systems

**Approach**: Migrate interfaces first, then core sub-packages

**Timeline**: 1-2 weeks per phase

#### Phase 1: Migrate Interface Imports

**Scope**: Update all interface imports to `interfaces/` package

**Impact**: Low risk, high benefit

**Steps**:

1. **Identify Interface Usage**:

```bash
# Find all interface imports
grep -r "github.com/kart-io/goagent/core.Agent" .
grep -r "github.com/kart-io/goagent/retrieval.VectorStore" .
grep -r "github.com/kart-io/goagent/memory.Manager" .
```

2. **Update Imports**:

```go
// Before
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/retrieval"
    "github.com/kart-io/goagent/memory"
)

func MyAgent(agent core.Agent, store retrieval.VectorStore, mem memory.Manager) {
    // ...
}

// After
import (
    "github.com/kart-io/goagent/interfaces"
)

func MyAgent(agent interfaces.Agent, store interfaces.VectorStore, mem interfaces.MemoryManager) {
    // Same code, just updated type names
}
```

3. **Run Tests**:

```bash
go test ./...
```

4. **Commit**:

```bash
git add -A
git commit -m "refactor: migrate to canonical interfaces package"
```

#### Phase 2: Migrate Core Sub-Packages

**Scope**: Update core sub-package imports (state, checkpoint, execution, middleware)

**Impact**: Medium risk, high long-term benefit

**Steps**:

1. **Identify Sub-Package Usage**:

```bash
# Find state-related imports
grep -r "core.State" .
grep -r "core.NewState" .

# Find checkpoint imports
grep -r "core.Checkpointer" .
grep -r "core.NewMemoryCheckpointer" .

# Find execution imports
grep -r "core.Runtime" .
grep -r "core.NewRuntime" .

# Find middleware imports
grep -r "core.Middleware" .
```

2. **Update Imports**:

```go
// Before
import "github.com/kart-io/goagent/core"

func InitializeAgent() {
    state := core.NewState()
    checkpointer := core.NewMemoryCheckpointer()
    runtime := core.NewRuntime()
}

// After
import (
    "github.com/kart-io/goagent/core/state"
    "github.com/kart-io/goagent/core/checkpoint"
    "github.com/kart-io/goagent/core/execution"
)

func InitializeAgent() {
    s := state.NewState()
    checkpointer := checkpoint.NewMemoryCheckpointer()
    runtime := execution.NewRuntime()
}
```

3. **Run Tests**:

```bash
go test ./...
```

4. **Commit**:

```bash
git add -A
git commit -m "refactor: migrate to core sub-packages"
```

### Strategy 3: Complete Migration (All at Once)

**Best for**: New projects, small codebases, during major refactoring

**Approach**: Update all imports in one go

**Timeline**: 1-2 days

**Steps**:

1. **Create Migration Script**:

```bash
#!/bin/bash
# migrate-imports.sh

# Migrate interface imports
find . -name "*.go" -exec sed -i \
  's|github.com/kart-io/goagent/core.Agent|github.com/kart-io/goagent/interfaces.Agent|g' {} \;

find . -name "*.go" -exec sed -i \
  's|github.com/kart-io/goagent/retrieval.VectorStore|github.com/kart-io/goagent/interfaces.VectorStore|g' {} \;

find . -name "*.go" -exec sed -i \
  's|github.com/kart-io/goagent/memory.Manager|github.com/kart-io/goagent/interfaces.MemoryManager|g' {} \;

# Add more as needed...

# Run goimports to clean up
goimports -w .
```

2. **Execute Migration**:

```bash
chmod +x migrate-imports.sh
./migrate-imports.sh
```

3. **Verify**:

```bash
go build ./...
go test ./...
```

4. **Manual Review**:

```bash
git diff
```

5. **Commit**:

```bash
git add -A
git commit -m "refactor: complete migration to new package structure"
```

## Code Migration Examples

### Example 1: Simple Agent

**Before**:

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/retrieval"
    "github.com/kart-io/goagent/memory"
)

func CreateAgent() core.Agent {
    vectorStore := retrieval.NewMemoryVectorStore()
    memoryMgr := memory.NewDefaultManager(vectorStore)

    agent := core.NewBaseAgent("my-agent")
    agent.SetMemory(memoryMgr)

    return agent
}

func ExecuteAgent(agent core.Agent, input *core.Input) (*core.Output, error) {
    return agent.Invoke(context.Background(), input)
}
```

**After** (Recommended):

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/retrieval"
    "github.com/kart-io/goagent/memory"
)

func CreateAgent() interfaces.Agent {
    vectorStore := retrieval.NewMemoryVectorStore()  // Returns interfaces.VectorStore
    memoryMgr := memory.NewDefaultManager(vectorStore)  // Returns interfaces.MemoryManager

    agent := core.NewBaseAgent("my-agent")  // Returns interfaces.Agent
    agent.SetMemory(memoryMgr)

    return agent
}

func ExecuteAgent(agent interfaces.Agent, input *interfaces.Input) (*interfaces.Output, error) {
    return agent.Invoke(context.Background(), input)
}
```

### Example 2: VectorStore Implementation

**Before**:

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/retrieval"
)

type MyCustomVectorStore struct {
    // ... fields
}

// Before: implements retrieval.VectorStore
func (s *MyCustomVectorStore) SimilaritySearch(ctx context.Context, query string, topK int) ([]*retrieval.Document, error) {
    // ... implementation
}

func (s *MyCustomVectorStore) AddDocuments(ctx context.Context, docs []*retrieval.Document) error {
    // ... implementation
}

func NewMyVectorStore() retrieval.VectorStore {
    return &MyCustomVectorStore{}
}
```

**After** (Recommended):

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/interfaces"
)

type MyCustomVectorStore struct {
    // ... fields (unchanged)
}

// Now implements interfaces.VectorStore (canonical definition)
func (s *MyCustomVectorStore) SimilaritySearch(ctx context.Context, query string, topK int) ([]*interfaces.Document, error) {
    // ... implementation (unchanged)
}

func (s *MyCustomVectorStore) AddDocuments(ctx context.Context, docs []*interfaces.Document) error {
    // ... implementation (unchanged)
}

func NewMyVectorStore() interfaces.VectorStore {
    return &MyCustomVectorStore{}
}

// Ensure compilation-time type checking
var _ interfaces.VectorStore = (*MyCustomVectorStore)(nil)
```

### Example 3: Checkpoint Usage

**Before**:

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/core"
)

func SaveAgentState(agent core.Agent) error {
    checkpointer := core.NewRedisCheckpointer(redisConfig)

    checkpoint := &core.Checkpoint{
        ID:       "checkpoint-1",
        ThreadID: "thread-1",
        State:    agent.GetState(),
    }

    return checkpointer.SaveCheckpoint(context.Background(), checkpoint)
}
```

**After** (Recommended):

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/core/checkpoint"
)

func SaveAgentState(agent interfaces.Agent) error {
    checkpointer := checkpoint.NewRedisCheckpointer(redisConfig)

    cp := &interfaces.Checkpoint{
        ID:       "checkpoint-1",
        ThreadID: "thread-1",
        State:    agent.GetState(),
    }

    return checkpointer.SaveCheckpoint(context.Background(), cp)
}
```

### Example 4: Runtime Execution

**Before**:

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/core"
)

func ExecuteWithRuntime(agent core.Agent, input *core.Input) (*core.Output, error) {
    runtime := core.NewRuntime(
        core.WithCheckpointer(core.NewMemoryCheckpointer()),
        core.WithMiddleware(core.LoggingMiddleware()),
    )

    return runtime.Execute(context.Background(), agent, input)
}
```

**After** (Recommended):

```go
package main

import (
    "context"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/core/execution"
    "github.com/kart-io/goagent/core/checkpoint"
    "github.com/kart-io/goagent/core/middleware"
)

func ExecuteWithRuntime(agent interfaces.Agent, input *interfaces.Input) (*interfaces.Output, error) {
    runtime := execution.NewRuntime(
        execution.WithCheckpointer(checkpoint.NewMemoryCheckpointer()),
        execution.WithMiddleware(middleware.LoggingMiddleware()),
    )

    return runtime.Execute(context.Background(), agent, input)
}
```

## Compatibility Matrix

| Version | Old Imports        | New Imports | Type Aliases | Breaking Changes          |
| ------- | ------------------ | ----------- | ------------ | ------------------------- |
| v0.9.x  | Works              | N/A         | N/A          | None                      |
| v0.10.x | Works (deprecated) | Works       | Active       | None                      |
| v0.11.x | Works (deprecated) | Works       | Active       | None                      |
| v0.12.x | Works (deprecated) | Works       | Active       | None                      |
| v0.13.x | Works (deprecated) | Works       | Active       | None                      |
| v0.14.x | Works (deprecated) | Works       | Active       | None                      |
| v1.0.0  | REMOVED            | Works       | Removed      | Yes (planned, documented) |

**Deprecation Period**: Minimum 4 minor versions (v0.10.0 → v1.0.0)

**Recommendation**: Migrate during v0.10.x - v0.14.x to avoid breaking changes in v1.0.0

## Testing Your Migration

### Step 1: Verify Compilation

```bash
# Ensure all code compiles
go build ./...
```

### Step 2: Run Unit Tests

```bash
# Run all tests
go test ./...

# Run with race detection
go test -race ./...

# Run with coverage
go test -cover ./...
```

### Step 3: Check Imports

```bash
# Find any remaining old imports (should be empty after migration)
grep -r "core.Agent" .
grep -r "retrieval.VectorStore" .
grep -r "memory.Manager" .
```

### Step 4: Lint Checks

```bash
# Run linting
golangci-lint run

# Check for deprecated usage (if linter supports)
golangci-lint run --enable=staticcheck
```

### Step 5: Integration Tests

```bash
# Run integration tests
go test -tags=integration ./...
```

## Common Migration Issues

### Issue 1: Import Cycle Errors

**Problem**: Creating import cycle after migrating imports

**Cause**: Circular dependencies between packages

**Solution**: Use interfaces to break cycles

```go
// Bad: Creates cycle
import (
    "core"
    "tools"
)

type MyAgent struct {
    tools []tools.Tool  // tools imports core, core imports tools = cycle!
}

// Good: Use interfaces
import (
    "interfaces"
)

type MyAgent struct {
    tools []interfaces.Tool  // No cycle!
}
```

### Issue 2: Type Assertion Failures

**Problem**: Type assertions fail after migration

**Cause**: Using concrete types instead of interfaces

**Solution**: Update type assertions

```go
// Bad
store := retrieval.NewMemoryVectorStore()
concreteStore := store.(*retrieval.MemoryVectorStore)  // Fails if type changed

// Good
store := retrieval.NewMemoryVectorStore()
// Use interface methods instead of asserting to concrete type
docs, _ := store.SimilaritySearch(ctx, query, 10)
```

### Issue 3: Duplicate Import Aliases

**Problem**: Import alias conflicts

**Cause**: Both old and new imports in same file

**Solution**: Remove old imports, use new imports exclusively

```go
// Bad
import (
    oldcore "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/core"
)

// Good
import (
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/core"
)
```

### Issue 4: Test Failures

**Problem**: Tests fail after migration

**Cause**: Test mocks or helpers using old types

**Solution**: Update test helpers to use new interfaces

```go
// Before
type MockAgent struct {
    mock.Mock
}

func (m *MockAgent) Invoke(ctx context.Context, input *core.Input) (*core.Output, error) {
    // ...
}

// After
type MockAgent struct {
    mock.Mock
}

func (m *MockAgent) Invoke(ctx context.Context, input *interfaces.Input) (*interfaces.Output, error) {
    // ...
}

// Ensure mock implements interface
var _ interfaces.Agent = (*MockAgent)(nil)
```

## FAQ

### Q: Do I need to update my code immediately?

**A**: No. Type aliases provide full backward compatibility. Your existing code will continue to work without modification until v1.0.0. However, we recommend migrating during the v0.10.x - v0.14.x period.

### Q: Will my tests break?

**A**: No. All existing tests should pass without modification. If tests use type assertions to concrete types, you may need to update them, but this is rare.

### Q: When will old imports stop working?

**A**: Old imports will be removed in **v1.0.0** (minimum 4 minor versions away). You have at least 4 releases to migrate.

### Q: How do I know which imports to update?

**A**: Use the Quick Reference Table at the top of this guide. Generally:

- Interfaces → `interfaces/` package
- State management → `core/state/`
- Checkpointing → `core/checkpoint/`
- Runtime → `core/execution/`
- Middleware → `core/middleware/`

### Q: What if I use a custom implementation?

**A**: Ensure your custom implementation implements the canonical interface from `interfaces/` package. Add a compile-time check:

```go
var _ interfaces.VectorStore = (*MyCustomStore)(nil)
```

### Q: Can I mix old and new imports?

**A**: Yes, but not recommended. Type aliases ensure old and new types are compatible, but mixing creates confusion and technical debt.

### Q: What about third-party packages using old imports?

**A**: Third-party packages will continue to work via type aliases. However, encourage third-party maintainers to migrate for long-term compatibility.

### Q: How do I report migration issues?

**A**: Create an issue on GitHub: https://github.com/kart-io/k8s-agent/issues

Include:

- Your current version
- Old import path
- New import path
- Error message
- Code snippet

## Automated Migration Tools

### Future Tools (Planned for v0.11.0)

We plan to provide automated migration tools:

1. **Import Rewriter**: Automatically update import paths
2. **Type Migrator**: Convert type references
3. **Test Updater**: Update test mocks and helpers
4. **Compatibility Checker**: Verify migration completeness

**Track Progress**: Watch https://github.com/kart-io/k8s-agent/milestones/v0.11.0

## Getting Help

### Documentation

- **Architecture**: `ARCHITECTURE.md` - New package structure
- **Requirements**: `.kiro/specs/pkg-agent-refactoring/requirements.md`
- **Design**: `.kiro/specs/pkg-agent-refactoring/design.md`
- **Project Summary**: `PROJECT_REFACTORING_COMPLETE.md`

### Community

- **GitHub Issues**: https://github.com/kart-io/k8s-agent/issues
- **Discussions**: https://github.com/kart-io/k8s-agent/discussions
- **Slack**: #k8s-agent channel

### Examples

- **Basic Examples**: `examples/basic/` - Simple migration examples
- **Advanced Examples**: `examples/advanced/` - Complex migration scenarios
- **Integration Examples**: `examples/integration/` - Full-system migrations

## Summary

**Key Takeaways**:

1. **No Urgency**: Old imports work until v1.0.0 (at least 4 versions away)
2. **Recommended Migration**: Update to new imports during v0.10.x - v0.14.x
3. **Zero Breakage**: Type aliases ensure full backward compatibility
4. **Gradual Approach**: Migrate interfaces first, then sub-packages
5. **Comprehensive Testing**: Run full test suite after migration
6. **Community Support**: Help available via GitHub issues and discussions

**Next Steps**:

1. Review this migration guide
2. Choose migration strategy (gradual recommended)
3. Update imports in development branch
4. Run comprehensive tests
5. Deploy to staging environment
6. Monitor for issues
7. Deploy to production

**Timeline Recommendation**:

- **v0.10.x - v0.11.x**: Migrate interface imports
- **v0.11.x - v0.12.x**: Migrate core sub-package imports
- **v0.12.x - v0.14.x**: Complete migration, remove all old imports
- **v1.0.0**: Old imports removed, migration mandatory

Good luck with your migration! If you encounter any issues, please reach out to the community.

---

**Document Version**: 1.0
**Last Updated**: November 14, 2025
**Applies To**: v0.10.0 and later
**Next Review**: v0.11.0 release
