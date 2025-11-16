# Phase 2.4 File Renaming Map

## Date

2025-11-14

## Summary

Eliminated all problematic filename collisions in pkg/agent directory through systematic renaming.

## Files Renamed

### Critical Collisions Eliminated

#### 1. runtime.go → tool_runtime.go

- **Old**: `tools/runtime.go`
- **New**: `tools/tool_runtime.go`
- **Reason**: Collision with `core/execution/runtime.go`
- **Method**: `git mv`

#### 2. config.go → postgres_config.go

- **Old**: `store/postgres/config.go`
- **New**: `store/postgres/postgres_config.go`
- **Reason**: Collision with `store/redis/config.go`
- **Method**: `git mv`

#### 3. config.go → redis_config.go

- **Old**: `store/redis/config.go`
- **New**: `store/redis/redis_config.go`
- **Reason**: Collision with `store/postgres/config.go`
- **Method**: `git mv`

#### 4. vector_store_memory.go → retrieval_memory_store.go

- **Old**: `retrieval/vector_store_memory.go`
- **New**: `retrieval/retrieval_memory_store.go`
- **Reason**: Collision with `memory/vector_store_memory.go`
- **Method**: `git mv`

#### 5. vector_store_memory.go → memory_vector_store.go

- **Old**: `memory/vector_store_memory.go`
- **New**: `memory/memory_vector_store.go`
- **Reason**: Collision with `retrieval/vector_store_memory.go`
- **Method**: `git mv`

#### 6. advanced.go → tool_selector_advanced.go

- **Old**: `middleware/advanced.go`
- **New**: `middleware/tool_selector_advanced.go`
- **Reason**: Collision with `core/middleware/advanced.go`
- **Method**: `git mv`
- **Note**: Old middleware package still used by examples; will be cleaned in Phase 3.2

## Acceptable Duplicates (Different Contexts)

The following filename duplicates remain but are acceptable as they exist in different functional contexts:

### Interface vs Implementation Pattern

- `agent.go`: `interfaces/agent.go` (definition) vs `core/agent.go` (implementation)
- `memory.go`: `interfaces/memory.go` (definition) vs `store/memory/memory.go` (implementation)
- `store.go`: `interfaces/store.go` (definition) vs `store/store.go` (implementation)
- `tool.go`: `interfaces/tool.go` (definition) vs `tools/tool.go` (implementation) vs `mcp/core/tool.go` (MCP-specific)

### Different Backend Implementations

- `redis.go`: `core/checkpoint/redis.go` (checkpoint backend) vs `store/redis/redis.go` (store backend)

### Different Registries

- `registry.go`: `tools/registry.go` (tool registry) vs `mcp/toolbox/registry.go` (MCP registry)

### MCP Package Context

- `toolbox.go`: `mcp/core/toolbox.go` (core) vs `mcp/toolbox/toolbox.go` (implementation)

## Future Work

### Phase 3.2 - Example Reorganization

- 20 `main.go` files in example directories will be renamed to descriptive names
- Examples will be reorganized into basic/, advanced/, integration/ categories
- Each main.go will be renamed to `{purpose}_demo.go` or `{purpose}_main.go`

## Metrics

- **Renamed files**: 6 files
- **Acceptable duplicates**: 16 files (in 7 categories)
- **Problematic collisions remaining**: 0 (excluding main.go in examples)
- **Success rate**: 100% (all critical collisions eliminated)

## Verification

```bash
# Check for duplicates (excluding test files and main.go)
find . -name "*.go" -type f | sed 's|.*/||' | sort | uniq -d | grep -v "main.go" | grep -v "_test.go"

# Expected output: Only acceptable duplicates
# (agent.go, memory.go, store.go, tool.go, redis.go, registry.go, toolbox.go)
```

## Impact

- All files renamed using `git mv` to preserve history
- No code changes required (only filename changes)
- No import path changes (package names remain the same)
- Build verified: `make build` passes successfully

## Status

**Phase 2.4 COMPLETE** - All problematic filename collisions eliminated.
