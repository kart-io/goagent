# Quick Reference: pkg/agent/ Organization Issues

## Files Requiring Immediate Rename (Duplicate Names)

### Cache.go (3 locations - conflicts)

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/cache/cache.go
  → Rename to: cache_base.go

/home/hellotalk/code/go/src/github.com/kart-io/goagent/performance/cache.go
  → Rename to: cache_pool.go

/home/hellotalk/code/go/src/github.com/kart-io/goagent/tools/cache.go
  → Rename to: tool_cache.go
```

### Executor.go (3 locations - conflicts)

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/tools/executor.go
  → Rename to: executor_tool.go
  → Contains: ToolExecutor struct

/home/hellotalk/code/go/src/github.com/kart-io/goagent/agents/executor.go
  → Rename to: executor_agent.go
  → Contains: AgentExecutor struct

/home/hellotalk/code/go/src/github.com/kart-io/goagent/mcp/toolbox/executor.go
  → Rename to: executor_standard.go
  → Contains: StandardExecutor struct
```

### Stream.go (3 locations - conflicts)

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/core/stream.go
  → Rename to: stream_core.go or streaming.go
  → Contains: StreamingAgent, StreamOutput, StreamWriter interfaces

/home/hellotalk/code/go/src/github.com/kart-io/goagent/llm/stream.go
  → Rename to: stream_client.go
  → Contains: StreamClient interface

/home/hellotalk/code/go/src/github.com/kart-io/goagent/stream/stream.go
  → Rename to: stream_base.go or stream_engine.go
  → Contains: main streaming implementation
```

### Client.go (2 locations)

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/llm/client.go
  → Keep as is (clear context)

/home/hellotalk/code/go/src/github.com/kart-io/goagent/distributed/client.go
  → Rename to: client_distributed.go
```

### Registry.go (2 locations)

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/distributed/registry.go
  → Rename to: registry_distributed.go

/home/hellotalk/code/go/src/github.com/kart-io/goagent/mcp/tools/registry.go
  → Rename to: registry_mcp.go
```

### Vector_store.go (2 locations)

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/retrieval/vector_store.go
  → Keep as is (primary location)

/home/hellotalk/code/go/src/github.com/kart-io/goagent/memory/vector_store.go
  → Rename to: vector_store_memory.go
```

### React.go (2 locations)

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/agents/react.go
  → Keep as is (agent implementation)

/home/hellotalk/code/go/src/github.com/kart-io/goagent/parsers/react.go
  → Rename to: parser_react.go
```

### Tracing.go (2 locations)

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/observability/tracing.go
  → Keep as is (primary location)

/home/hellotalk/code/go/src/github.com/kart-io/goagent/distributed/tracing.go
  → Rename to: tracing_distributed.go OR move to observability/
```

### Middleware.go (2 locations)

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/core/middleware.go
  → Keep as is (core middleware system)

/home/hellotalk/code/go/src/github.com/kart-io/goagent/stream/middleware/middleware.go
  → Rename to: stream_middleware.go (move to stream/ directory)
```

---

## Files That Need to Move (Wrong Package)

### Agent-like Classes in Tools Package (Should be in Agents)

```
MOVE FROM tools/ TO agents/ (or create agents/tool_agents/):

/home/hellotalk/code/go/src/github.com/kart-io/goagent/tools/cache_agent.go
/home/hellotalk/code/go/src/github.com/kart-io/goagent/tools/database_agent.go
/home/hellotalk/code/go/src/github.com/kart-io/goagent/tools/http_agent.go
/home/hellotalk/code/go/src/github.com/kart-io/goagent/tools/shell_agent.go

Reason: These are agents that use tools, not tools themselves.
        They belong in the agents/ package or a sub-package.
```

### ToolExecutor in Tools (Should be in Core/Orchestration)

```
MOVE FROM tools/executor.go TO:
  Option A: core/executor_tool.go (if tool-specific execution logic)
  Option B: New package: pkg/agent/orchestration/executor_tool.go

Reason: Tool execution orchestration is infrastructure, not a tool itself.
```

### Tracing in Distributed (Should be in Observability)

```
MOVE FROM distributed/tracing.go TO observability/tracing_distributed.go

Reason: All tracing/telemetry should be in observability package.
        distributed/tracing.go is a distributed variant.
```

### Example Files in Production Code

```
MOVE FROM core/example_agent.go TO:
  example/core/ or example/01_basic_agent/

Reason: Examples should be in example package, not mixed with production code.
```

---

## Package Structure Issues (Priority Order)

### CRITICAL: Core Package (11,092 lines across 28 files)

**Location**: `/home/hellotalk/code/go/src/github.com/kart-io/goagent/core/`

**Suggested Reorganization**:

```
Create sub-packages:
  core/abstractions/   (agent.go, runnable.go, chain.go, callback.go, errors.go)
  core/store/         (store.go, store_redis.go, store_postgres.go + tests)
  core/checkpoint/    (checkpointer.go, checkpointer_*.go + tests)
  core/middleware/    (middleware.go, middleware_advanced.go + tests)
  core/streaming/     (stream.go, runtime.go)

Action: Break core into logical sub-packages
Impact: Reduces complexity, improves organization
```

### HIGH: Tools Package (5,345 lines, mixed responsibilities)

**Location**: `/home/hellotalk/code/go/src/github.com/kart-io/goagent/tools/`

**Action Items**:

1. Move cache_agent.go, database_agent.go, http_agent.go, shell_agent.go to agents/
2. Extract concrete tool implementations to tools/concrete/ or use tool\_\*.go prefix
3. Rename executor.go to executor_tool.go
4. Consolidate builders (APIToolBuilder, ShellToolBuilder) into builder.go

```
After reorganization:
tools/
  ├── tool.go (interface + BaseTool)
  ├── builder.go (ToolBuilder interface + base)
  ├── toolkit.go (Toolkit interface)
  ├── cache.go (ToolCache interface)
  ├── graph.go (ToolGraph)
  ├── executor_tool.go (execution logic)
  ├── tool_api.go (APITool, APIToolBuilder)
  ├── tool_calculator.go (CalculatorTool, AdvancedCalculatorTool)
  ├── tool_function.go (FunctionTool, FunctionToolBuilder)
  ├── tool_shell.go (ShellTool, ShellToolBuilder)
  ├── tool_search.go (SearchTool, SearchEngine)
  └── tests/
      ├── executor_test.go
      ├── tools_test.go
      └── cache_test.go
```

### MEDIUM: Stream Package (8 files, sub-packages needed)

**Location**: `/home/hellotalk/code/go/src/github.com/kart-io/goagent/stream/`

**Action**: Flatten structure with clear naming

```
From:
  stream/
    ├── agents/
    ├── middleware/
    ├── tools/
    └── [main files]

To:
  stream/
    ├── stream.go (base)
    ├── buffer.go
    ├── multiplexer.go
    ├── reader.go
    ├── writer.go
    ├── middleware_stream.go (moved from middleware/)
    ├── agent_streaming_llm.go (from agents/)
    ├── agent_progress.go (from agents/)
    ├── agent_data_pipeline.go (from agents/)
    ├── tool_sse.go (from tools/)
    ├── tool_websocket.go (from tools/)
    └── tests/
```

---

## Missing Documentation

### Create These README Files

```
/home/hellotalk/code/go/src/github.com/kart-io/goagent/core/README.md
  - Explain core abstractions (Agent, Runnable, Chain, Callback)
  - Document Store and Checkpointer interfaces
  - Link to sub-package documentation

/home/hellotalk/code/go/src/github.com/kart-io/goagent/builder/README.md
  - Explain builder pattern usage
  - Document AgentBuilder class
  - Provide examples

/home/hellotalk/code/go/src/github.com/kart-io/goagent/middleware/README.md
  - Explain middleware chain
  - Document available middleware
  - Provide usage examples

/home/hellotalk/code/go/src/github.com/kart-io/goagent/memory/README.md
  - Explain memory management
  - Document interfaces
  - Provide usage examples

/home/hellotalk/code/go/src/github.com/kart-io/goagent/agents/README.md
  - Already exists, good!

/home/hellotalk/code/go/src/github.com/kart-io/goagent/multiagent/README.md
  - Explain multi-agent patterns
  - Document communicators (NATS vs Memory)
  - Provide examples

/home/hellotalk/code/go/src/github.com/kart-io/goagent/distributed/README.md
  - Explain distributed patterns
  - Document Coordinator and Registry
  - Explain when to use

/home/hellotalk/code/go/src/github.com/kart-io/goagent/observability/README.md
  - Explain telemetry setup
  - Document metrics and tracing
  - Provide integration examples

/home/hellotalk/code/go/src/github.com/kart-io/goagent/docs/PACKAGES.md
  - Create comprehensive package index
  - Explain dependencies between packages
  - Link to all README files

/home/hellotalk/code/go/src/github.com/kart-io/goagent/docs/API.md
  - Document all public interfaces
  - Show usage patterns
  - Provide code examples
```

---

## Duplicate Interfaces to Consolidate

### VectorStore Interface (2 definitions - INCOMPATIBLE)

```
retrieval/vector_store.go:
  type VectorStore interface {
    Add(ctx, docs...) error
    Delete(ctx, ids...) error
    Search(ctx, query, ...) error
  }

memory/manager.go:
  type VectorStore interface {
    Store(ctx, key, vector[], metadata) error
    ...
  }

ACTION: Choose one definition, consolidate into interfaces/ package
```

### Store Interface (3 definitions)

```
core/store.go: type Store interface { ... }
memory/manager.go: type ConversationStore interface { ... }
Implicit patterns in other packages

ACTION: Consolidate into single shared definition
```

---

## Summary of Immediate Actions

### Phase 1 (Rename files - no refactoring needed)

```
1. tools/executor.go → tools/executor_tool.go
2. agents/executor.go → agents/executor_agent.go (or rename class only)
3. performance/cache.go → performance/cache_pool.go
4. stream/stream.go → stream/stream_base.go
5. llm/stream.go → llm/stream_client.go
6. All other duplicate files per the list above
```

### Phase 2 (Move files - requires import updates)

```
1. tools/{cache_agent,database_agent,http_agent,shell_agent}.go → agents/
2. distributed/tracing.go → observability/tracing_distributed.go
3. core/example_agent.go → example/
4. stream/{middleware,agents,tools}/* → stream/ (flatten structure)
```

### Phase 3 (Refactor packages - complex work)

```
1. Break core into sub-packages (abstractions/, store/, checkpoint/, etc.)
2. Create interfaces/ for shared interface definitions
3. Reorganize tools/ with clear structure (concrete/, builder/, etc.)
```

### Phase 4 (Documentation)

```
1. Create docs/ folder structure
2. Add missing README.md files
3. Create comprehensive API documentation
4. Create package dependency diagram
```
