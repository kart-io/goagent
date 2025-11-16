# GoAgent Architecture - Executive Summary

## What is GoAgent?

GoAgent is a production-ready AI agent framework for Go that enables building autonomous agents capable of reasoning, using tools, and making decisions. It's inspired by LangChain's architecture but built from scratch for Go with a strict layered architecture and enterprise-grade features.

## Key Concepts

### 1. Agents
Autonomous entities that:
- Take input and produce structured output
- Implement a reasoning pattern (e.g., ReAct)
- Use tools to gather information
- Maintain state and memory
- Support callbacks for monitoring

### 2. Runnable Pattern
Everything is composable:
- `Invoke(ctx, input)` - Single execution
- `Stream(ctx, input)` - Streaming output
- `Batch(ctx, inputs)` - Multiple inputs
- `Pipe(next)` - Chaining runnables

### 3. Tools
Functions agents can invoke:
- Shell commands, HTTP requests, searches
- Custom implementations
- Registered in registry
- Executed with parameters and results fed back to agent

### 4. Memory & State
Persistent storage:
- Session state (conversation history)
- Long-term memory (vector stores, databases)
- Case-based learning (similar problem lookup)
- Checkpointing for recovery

### 5. Middleware
Cross-cutting concerns:
- Caching, logging, rate limiting
- Validation, transformation
- Observability, monitoring
- Custom business logic

## Current Architecture

### Layer Structure
```
Layer 4: Examples & Tests
   ↓
Layer 3: Implementation (agents, tools, middleware, parsers)
   ↓
Layer 2: Business Logic (core, builder, llm, memory, planning)
   ↓
Layer 1: Foundation (interfaces, errors, utils)
```

### Main Components

**interfaces/** - Agent, Tool, MemoryManager interfaces
**core/** - BaseAgent, Runnable, Callback base implementations
**builder/** - Fluent API for agent construction
**agents/react/** - ReAct (Thought-Action-Observation) agent
**agents/executor/** - Tool execution focused agent
**planning/** - Task decomposition and planning
**middleware/** - Interceptors for cross-cutting concerns
**llm/** - LLM client abstraction (OpenAI, Gemini, DeepSeek)
**tools/** - Tool implementations (shell, http, search, etc.)
**memory/** - Conversation history and case memory
**parsers/** - Output parsing for agent patterns

## How ReAct Works (Current Default Pattern)

```
Input: "What is the capital of France?"
  ↓
[LLM] Generate Thought: "I need to search for capital of France"
  ↓
Parse: Action=Search, ActionInput={query: "capital of France"}
  ↓
[Tool] Execute Search → "The capital of France is Paris"
  ↓
Update Context (Scratchpad) with result
  ↓
[LLM] Generate Final Answer: "The capital of France is Paris"
  ↓
Return: Output with reasoning trace
```

**Key Features:**
- Introspective reasoning at each step
- Full visibility of tool outputs
- Explainable decision-making
- Handles complex, multi-step tasks

**Limitations:**
- Multiple LLM calls (slower for simple tasks)
- May hallucinate tool availability
- No forward planning

## Builder Pattern Usage

```go
// Simple agent
agent := builder.NewAgentBuilder(llmClient).
    WithSystemPrompt("You are helpful").
    WithTools(tool1, tool2).
    Build()

// Or with presets
agent := builder.ChatAgent(llmClient, "John")
agent := builder.RAGAgent(llmClient, retriever)
agent := builder.AnalysisAgent(llmClient, dataSource)
```

## Extension Points for New Reasoning Patterns

### Option 1: Middleware (Minimal)
Add a post-processing layer:
```
Agent Output → Reflection Middleware → Enhanced Output
```
- Least invasive
- Good for filtering/enhancement
- Example: Self-critique, validation

### Option 2: New Agent Type (Comprehensive)
Create alternative reasoning loop:
```
Input → Custom Agent Logic → Output with reasoning
```
- Full control
- Can customize everything
- Examples: Chain-of-Thought, Tree-of-Thought

### Option 3: Planning Strategy (Structured)
Custom decomposition logic:
```
Goal → Strategy.Apply(plan) → Refined Plan
```
- For hierarchical/forward planning
- Integrates with SmartPlanner
- Examples: Hierarchical planning, Few-shot learning

## Key Design Patterns

1. **Runnable** - Unified execution interface (Invoke/Stream/Batch)
2. **Builder** - Fluent API for complex construction
3. **Strategy** - Multiple planning/reasoning strategies
4. **Middleware** - Interceptor pattern for cross-cutting concerns
5. **Callback** - Event hooks for monitoring
6. **Adapter** - Multiple LLM provider support
7. **Composition** - Agents/chains composition

## Critical Rules

### Import Layering (Strict)
- Layer 3 imports only Layer 1+2
- Layer 2 imports only Layer 1
- Layer 1 has no GoAgent imports
- `tools/` cannot import `agents/`
- No circular dependencies

**Verify before committing:**
```bash
./verify_imports.sh
```

### Testing Standards
- Minimum 80% coverage
- Table-driven tests
- Mock external dependencies
- Integration tests for agents

### Code Quality
```bash
make fmt      # Format
make vet      # Go vet
make lint     # Linter
make test     # Tests
```

## Recommended Next Steps

### To Understand the Framework Better:
1. Read `/agents/react/react.go` - See full ReAct implementation
2. Read `/builder/builder.go` - See builder pattern
3. Study `/core/agent.go` - Base agent implementation
4. Check `/planning/planner.go` - Planning system

### To Add New Reasoning Patterns:
1. Identify complexity level (minimal/medium/high)
2. Choose extension point (middleware/agent/strategy)
3. Follow code organization template
4. Write tests (target 80%+ coverage)
5. Run verification commands
6. Create example in `/examples/`

### Quick Start for New Pattern:
```bash
# For middleware (simplest)
# Create /core/middleware/yourpattern_middleware.go
# 1. Implement Execute() method
# 2. Call req.Next() for original handler
# 3. Add custom logic after

# For agent (most common)
# Create /agents/yourpattern/yourpattern_agent.go
# 1. Embed BaseAgent
# 2. Implement Invoke() with custom logic
# 3. Return ReasoningSteps in output
# 4. Create parser if needed

# For planning strategy
# Create /planning/yourpattern_strategy.go
# 1. Implement PlanStrategy interface
# 2. Implement Apply() method
# 3. Register with SmartPlanner
```

## File Organization

```
goagent/
├── interfaces/          # Interfaces (Layer 1)
├── errors/              # Error types (Layer 1)
├── utils/               # Utilities (Layer 1)
├── core/                # Base implementations (Layer 2)
│   ├── agent.go         # BaseAgent
│   ├── runnable.go      # Runnable interface
│   ├── callback.go      # Callback system
│   ├── middleware/      # Middleware framework
│   └── execution/       # Execution runtime
├── builder/             # Agent builder (Layer 2)
├── llm/                 # LLM abstraction (Layer 2)
├── memory/              # Memory systems (Layer 2)
├── planning/            # Planning module (Layer 2)
├── agents/              # Agent implementations (Layer 3)
│   ├── react/           # ReAct agent
│   ├── executor/        # Executor agent
│   └── specialized/     # Domain-specific agents
├── tools/               # Tool implementations (Layer 3)
├── middleware/          # Middleware implementations (Layer 3)
├── parsers/             # Output parsers (Layer 3)
├── examples/            # Example code (Layer 4)
└── tests/               # Tests (Layer 4)
```

## Performance Characteristics

- Agent initialization: ~100 microseconds
- Single invoke: ~1-5ms (excluding LLM calls)
- Middleware overhead: <5%
- Parallel tool execution: Linear scaling to 100+ concurrent
- Cache hit rate: >90% (with LRU)
- Memory per agent: ~10-50MB (depending on state size)

## Enterprise Features

- Distributed execution across nodes (NATS)
- Redis/PostgreSQL backend support
- OpenTelemetry observability (traces, metrics)
- Checkpointing for fault tolerance
- Rate limiting and circuit breakers
- Multi-agent coordination

## Common Use Cases

1. **Customer Service** - ChatAgent with memory
2. **Data Analysis** - AnalysisAgent with tools
3. **Information Gathering** - ResearchAgent with search
4. **Workflow Automation** - WorkflowAgent with orchestration
5. **System Monitoring** - MonitoringAgent with alerts
6. **Document Q&A** - RAGAgent with retrieval

## Limitations & Trade-offs

**ReAct Pattern (current):**
- Fast for complex reasoning
- Slow for simple lookups (multiple LLM calls)
- Good interpretability
- Token expensive

**Planning Module:**
- Good for structured tasks
- Less flexible for open-ended problems
- Better for human review

**Memory/State:**
- Limited by storage (Redis/PG)
- Semantic search needs vector DB
- Trade-off: Speed vs. context richness

## Future Directions

**Likely additions:**
1. Chain-of-Thought agent (common request)
2. Tree-of-Thought exploration
3. Self-reflection middleware
4. Multi-agent debate
5. Better long-context handling

**Possible research areas:**
1. Neural-symbolic integration
2. Meta-learning (learn to plan)
3. Causal reasoning
4. Knowledge graphs
5. Continuous learning/adaptation

## Getting Help

- **Architecture questions:** See `ARCHITECTURE_ANALYSIS.md`
- **Adding patterns:** See `REASONING_PATTERNS_QUICK_REFERENCE.md`
- **Import layering:** Run `./verify_imports.sh`
- **Testing:** See `docs/development/TESTING_BEST_PRACTICES.md`
- **LLM integration:** See `docs/guides/LLM_PROVIDERS.md`
- **Examples:** Check `/examples/` directory

## Key Takeaway

GoAgent provides a **modular, extensible framework** for building AI agents. The strict 4-layer architecture ensures scalability and maintainability. New reasoning patterns can be added at multiple levels (middleware, agent, or strategy) depending on complexity. The framework balances flexibility with structure, allowing both simple extensions and complex customizations.

