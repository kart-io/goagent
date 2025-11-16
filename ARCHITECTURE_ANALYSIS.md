# GoAgent Framework Architecture Analysis

## Executive Summary

GoAgent is a comprehensive, production-ready AI agent framework for Go. It implements a **Thought-Action-Observation (ReAct) pattern** with support for multiple reasoning strategies, tool integration, memory management, and distributed execution. The framework is built on a strict 4-layer architecture with clear separation of concerns.

---

## 1. Core Architecture Overview

### 1.1 Design Principles

**Layered Architecture (4 Layers)**
```
Layer 4: Examples & Tests (can import all layers)
   ↓
Layer 3: Implementation (agents/, tools/, middleware/, parsers/, etc.)
   ↓
Layer 2: Business Logic (core/, builder/, llm/, memory/, store/, planning/)
   ↓
Layer 1: Foundation (interfaces/, errors/, cache/, utils/)
```

**Key Design Patterns**
- **Runnable Pattern**: Unified execution interface inspired by LangChain
- **Builder Pattern**: Fluent API for agent construction
- **Callback Pattern**: Monitoring and debugging hooks
- **Middleware Pattern**: Cross-cutting concerns (caching, validation, rate limiting)
- **Strategy Pattern**: Multiple planning and reasoning strategies

### 1.2 Key Concepts

**Agents**: Autonomous entities that reason, use tools, and make decisions
**Runnables**: Composable components with Invoke/Stream/Batch operations
**Tools**: Extensible functions agents can invoke for information gathering
**Memory**: Persistent state and conversation history management
**Middleware**: Interceptors for adding features like caching, logging, validation

---

## 2. Agent Architecture

### 2.1 Agent Interface Hierarchy

```
┌─────────────────────────────────────────────────────┐
│ interfaces.Agent (Primary Interface)                 │
├─────────────────────────────────────────────────────┤
│ - Runnable (Invoke, Stream, Batch, Pipe)             │
│ - Name() string                                       │
│ - Description() string                                │
│ - Plan(ctx, input) (*Plan, error)                     │
└──────────────────┬──────────────────────────────────┘
                   │
        ┌──────────┼──────────┬──────────┐
        ▼          ▼          ▼          ▼
   ReactAgent  ExecutorAgent SpecialAgent BaseAgent
   (reasoning)  (tool-focused) (domain)   (base impl)
```

### 2.2 Agent Input/Output

**Agent Input Structure**:
```go
type AgentInput struct {
    Task          string                 // Task description
    Instruction   string                 // Specific instructions
    Context       map[string]interface{} // Contextual data
    Options       AgentOptions           // Execution options
    SessionID     string                 // Session identifier
    Timestamp     time.Time              // Timestamp
}
```

**Agent Output Structure**:
```go
type AgentOutput struct {
    Result         interface{}            // Execution result
    Status         string                 // "success", "failed", "partial"
    Message        string                 // Status message
    ReasoningSteps []ReasoningStep        // Thought/Action/Observation steps
    ToolCalls      []ToolCall             // Tool invocation records
    Latency        time.Duration          // Execution time
    Metadata       map[string]interface{} // Additional metadata
}
```

---

## 3. ReAct Agent Implementation

### 3.1 ReAct Pattern (Reasoning + Acting)

The ReActAgent implements the Thought-Action-Observation loop:

```
Loop until Final Answer:
  1. Thought:      Analyze current situation
  2. Action:       Decide which tool to use
  3. Action Input: Provide parameters to tool
  4. Observation:  Execute tool and observe results
  5. Repeat with new context
```

### 3.2 ReActAgent Structure

**Location**: `/agents/react/react.go`

**Key Components**:
```go
type ReActAgent struct {
    *BaseAgent               // Base implementation
    llm          llm.Client  // LLM for reasoning
    tools        []tools.Tool // Available tools
    toolsByName  map[string]tools.Tool
    parser       *parsers.ReActOutputParser
    maxSteps     int         // Max loop iterations
    stopPattern  []string    // Stop patterns ("Final Answer:")
    promptPrefix string      // System prompt
    promptSuffix string      // Task prompt
    formatInstr  string      // Output format instructions
}
```

### 3.3 ReAct Execution Flow

```
Invoke(ctx, input)
  │
  ├─→ buildPrompt(input)
  │     - Format tools list
  │     - Insert task description
  │     - Add format instructions
  │
  ├─→ For each step (up to maxSteps):
  │   │
  │   ├─→ Call LLM with current prompt + scratchpad
  │   │
  │   ├─→ Parse LLM output (Thought/Action/Action Input)
  │   │
  │   ├─→ Check for Final Answer
  │   │     └─→ If found: exit loop
  │   │
  │   ├─→ Execute tool
  │   │     └─→ Get observation result
  │   │
  │   └─→ Update scratchpad with new step
  │
  └─→ Return output with reasoning trace
```

### 3.4 ReAct Parser

**Location**: `/parsers/parser_react.go`

Parses LLM output using regex patterns:
```
Thought: <thought content>
Action: <tool name>
Action Input: <JSON parameters>
Observation: <tool result>
...
Final Answer: <final answer>
```

**Key Regex Patterns**:
- `Thought:` - LLM's reasoning
- `Action:` - Tool to invoke
- `Action Input:` - Tool parameters (JSON or string)
- `Final Answer:` - Conclusion (stops loop)

---

## 4. BaseAgent & Runnable Pattern

### 4.1 Runnable Interface

All executable components implement the Runnable pattern:

```go
type Runnable[I, O any] interface {
    Invoke(ctx context.Context, input I) (O, error)
    Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error)
    Batch(ctx context.Context, inputs []I) ([]O, error)
    Pipe(next Runnable[O, any]) Runnable[I, any]
    WithCallbacks(callbacks ...Callback) Runnable[I, O]
    WithConfig(config RunnableConfig) Runnable[I, O]
}
```

### 4.2 BaseAgent Implementation

**Location**: `/core/agent.go`

```go
type BaseAgent struct {
    *BaseRunnable[*AgentInput, *AgentOutput]
    name         string
    description  string
    capabilities []string
}
```

**Methods**:
- `Invoke()` - Execute single input
- `Stream()` - Streaming execution
- `Batch()` - Execute multiple inputs
- `Pipe()` - Chain to another Runnable
- `WithCallbacks()` - Add monitoring hooks
- `WithConfig()` - Configure execution

---

## 5. Builder Pattern Implementation

### 5.1 AgentBuilder Design

**Location**: `/builder/builder.go`

The fluent API for agent construction:

```go
agent := NewAgentBuilder[C, S](llmClient).
    WithTools(tool1, tool2).
    WithSystemPrompt("You are helpful").
    WithState(initialState).
    WithMiddleware(loggingMW, cacheMW).
    WithCallbacks(debugCallback).
    WithConfig(config).
    Build()
```

### 5.2 Configuration Presets

The builder includes specialized factory methods:

1. **QuickAgent**: Minimal configuration
2. **RAGAgent**: Retrieval-augmented generation
3. **ChatAgent**: Conversational agent
4. **AnalysisAgent**: Data analysis focus
5. **WorkflowAgent**: Multi-step orchestration
6. **MonitoringAgent**: Continuous monitoring
7. **ResearchAgent**: Information gathering

### 5.3 AgentConfig

```go
type AgentConfig struct {
    MaxIterations    int           // Reasoning loop limit
    Timeout          time.Duration // Execution timeout
    EnableStreaming  bool          // Stream responses
    EnableAutoSave   bool          // Auto-save state
    SaveInterval     time.Duration // Save frequency
    MaxTokens        int           // LLM response limit
    Temperature      float64       // LLM sampling temperature
    SessionID        string        // Checkpoint key
    Verbose          bool          // Detailed logging
}
```

---

## 6. Middleware System

### 6.1 Middleware Architecture

**Location**: `/core/middleware/middleware.go`

```go
type Middleware func(next Runnable) Runnable

type MiddlewareChain struct {
    middlewares []Middleware
    handler     Handler // Final handler
}
```

### 6.2 Built-in Middleware

1. **CacheMiddleware** - LRU caching for repeated queries
2. **DynamicPromptMiddleware** - Prompt engineering
3. **RateLimiterMiddleware** - Token/request rate limiting
4. **ValidationMiddleware** - Input validation
5. **TimingMiddleware** - Performance metrics
6. **TransformMiddleware** - Input/output transformation
7. **LoggingMiddleware** - Execution logging
8. **CircuitBreakerMiddleware** - Fault tolerance

### 6.3 Middleware Usage Pattern

```go
builder.WithMiddleware(
    middleware.NewCacheMiddleware(5*time.Minute),
    middleware.NewLoggingMiddleware(logger),
    middleware.NewRateLimiterMiddleware(100, time.Minute),
)
```

---

## 7. Planning Module

### 7.1 Planning Architecture

**Location**: `/planning/`

```
Planner Interface
  ├─ CreatePlan(goal, constraints) - Generate execution plan
  ├─ RefinePlan(plan, feedback) - Improve plan
  ├─ DecomposePlan(plan, step) - Break down step
  ├─ OptimizePlan(plan) - Efficiency optimization
  └─ ValidatePlan(plan) - Feasibility check
```

### 7.2 Plan Structure

```go
type Plan struct {
    ID           string
    Goal         string
    Strategy     string
    Steps        []*Step // Ordered execution steps
    Dependencies map[string][]string // Step relationships
    Status       PlanStatus // draft/ready/executing/completed
    Metrics      *PlanMetrics
}

type Step struct {
    ID                string // Unique identifier
    Name              string
    Description       string
    Type              StepType // analysis/decision/action/validation
    Agent             string // Which agent executes this
    Parameters        map[string]interface{}
    Expected          *ExpectedOutcome
    Priority          int
    EstimatedDuration time.Duration
    Status            StepStatus
    Result            *StepResult
}
```

### 7.3 Planning Strategies

**Decomposition Strategy**:
- Breaks complex goals into simpler steps
- Example: Analysis → Preparation → Execution → Verification

**Backward Chaining Strategy**:
- Works backwards from goal
- Identifies prerequisite steps
- Best for constrained problems (MaxSteps < 5)

**Hierarchical Strategy**:
- Multi-level goal decomposition
- Complex problems with multiple sub-goals

**SmartPlanner Features**:
- LLM-based plan generation
- Memory-based similar case retrieval
- Automatic validation and refinement
- Custom strategy registration

---

## 8. Callback System

### 8.1 Callback Interface

**Location**: `/core/callback.go`

```go
type Callback interface {
    // Generic
    OnStart(ctx context.Context, input interface{}) error
    OnEnd(ctx context.Context, output interface{}) error
    OnError(ctx context.Context, err error) error
    
    // LLM callbacks
    OnLLMStart(ctx context.Context, prompts []string, model string) error
    OnLLMEnd(ctx context.Context, output string, tokenUsage int) error
    OnLLMError(ctx context.Context, err error) error
    
    // Chain callbacks
    OnChainStart(ctx context.Context, name string, input interface{}) error
    OnChainEnd(ctx context.Context, name string, output interface{}) error
    OnChainError(ctx context.Context, name string, err error) error
    
    // Tool callbacks
    OnToolStart(ctx context.Context, name string, input interface{}) error
    OnToolEnd(ctx context.Context, name string, output interface{}) error
    OnToolError(ctx context.Context, name string, err error) error
    
    // Agent callbacks
    OnAgentAction(ctx context.Context, action *AgentAction) error
    OnAgentFinish(ctx context.Context, output interface{}) error
}
```

### 8.2 Callback Usage

```go
type DebugCallback struct {
    *BaseCallback
}

func (c *DebugCallback) OnLLMEnd(ctx, output, tokenUsage) error {
    log.Printf("LLM returned: %s (tokens: %d)", output, tokenUsage)
    return nil
}

agent = builder.WithCallbacks(debugCallback).Build()
```

---

## 9. Tool System

### 9.1 Tool Interface

**Location**: `/interfaces/tool.go`

```go
type Tool interface {
    Name() string
    Description() string
    Execute(ctx context.Context, input map[string]interface{}) (interface{}, error)
    Invoke(ctx context.Context, input *ToolInput) (*ToolOutput, error)
}

type ToolInput struct {
    Args    map[string]interface{}
    Context context.Context
}

type ToolOutput struct {
    Result interface{}
    Error  error
}
```

### 9.2 Tool Categories

1. **Shell Tools** - Shell command execution
2. **HTTP Tools** - REST API calls
3. **Search Tools** - Web search integration
4. **Compute Tools** - Mathematical calculations
5. **Practical Tools** - File ops, DB queries, web scraping
6. **Custom Tools** - User-defined functionality

### 9.3 Tool Execution in ReAct

```go
// During ReAct loop:
observation, err := r.executeTool(ctx, action, actionInput)

// Tool is:
// 1. Found in toolsByName map
// 2. Executed with parameters
// 3. Result added to scratchpad
// 4. Scratchpad fed back to LLM
```

---

## 10. Memory Management

### 10.1 Memory Architecture

**Location**: `/memory/` and `/store/`

```go
type MemoryManager interface {
    SaveContext(ctx, sessionID, input, output) error
    LoadHistory(ctx, sessionID) ([]Message, error)
    SearchSimilarCases(ctx, query, topK) ([]*Case, error)
    Clear(ctx, sessionID) error
}
```

### 10.2 Memory Types

1. **Short-term Memory** - Current conversation/session
2. **Long-term Memory** - Persistent storage (Redis/PostgreSQL)
3. **Vector Memory** - Semantic similarity search
4. **Case-based Memory** - Past solutions for similar problems

### 10.3 State Management

```go
type AgentState map[string]interface{}

state := core.NewAgentState()
state.Set("user_id", 123)
state.Set("conversation_history", messages)
```

---

## 11. How Reasoning Patterns Work

### 11.1 Current: ReAct Pattern

**Characteristics**:
- Sequential Thought-Action-Observation loops
- Introspective reasoning at each step
- Full tool output visibility to LLM
- Suitable for: Tool-heavy tasks, complex reasoning

**Advantages**:
- Clear reasoning trace
- Explainable decision-making
- Good for debugging

**Limitations**:
- Slow for simple tasks (multiple LLM calls)
- May hallucinate tool availability
- No forward planning

### 11.2 Existing Framework Support

The framework already supports multiple patterns:

1. **Planning Module** - Forward planning with goal decomposition
2. **ExecutorAgent** - Direct tool execution without explicit reasoning
3. **Specialized Agents** - Domain-specific implementations
4. **Middleware-based Reasoning** - Custom logic in middleware

### 11.3 Extension Points for New Patterns

**Key Integration Areas**:

1. **Parser Layer** (`/parsers/`)
   - Create new parser for output format
   - Example: `parser_chain_of_thought.go` for CoT
   - Implement structured output parsing

2. **Agent Layer** (`/agents/`)
   - Create new agent type
   - Embed BaseAgent or composition
   - Override Invoke() method
   - Implement custom reasoning loop

3. **Middleware Layer** (`/core/middleware/`)
   - Add reasoning-specific middleware
   - Example: ReflectionMiddleware for self-critique
   - Can wrap or enhance existing agents

4. **Strategy Layer** (`/planning/strategies.go`)
   - Register new PlanStrategy
   - Implement different decomposition logic
   - Compose with SmartPlanner

---

## 12. Extension Guide: Adding New Reasoning Patterns

### 12.1 Minimal New Pattern (Middleware Approach)

```go
// In /core/middleware/reflection.go
type ReflectionMiddleware struct {
    llm llm.Client
}

func (m *ReflectionMiddleware) Execute(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
    // Execute main handler
    resp, err := req.Next(ctx, req)
    if err != nil {
        return nil, err
    }
    
    // Ask LLM to reflect on output
    reflectionPrompt := fmt.Sprintf(
        "Review this response and check for errors:\n%v", 
        resp.Output)
    
    reflection, err := m.llm.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{{Role: "user", Content: reflectionPrompt}},
    })
    
    resp.Metadata["reflection"] = reflection.Content
    return resp, nil
}

// Usage:
builder.WithMiddleware(
    middleware.NewReflectionMiddleware(llmClient),
)
```

### 12.2 Full New Pattern (Agent Approach)

```go
// In /agents/cot/cot_agent.go - Chain of Thought Agent
type ChainOfThoughtAgent struct {
    *core.BaseAgent
    llm   llm.Client
    tools []tools.Tool
    
    // New fields
    thinkingPrompt string
    maxReflections int
}

func (c *ChainOfThoughtAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    output := &core.AgentOutput{
        ReasoningSteps: []core.ReasoningStep{},
        ToolCalls:      []core.ToolCall{},
        Metadata:       make(map[string]interface{}),
    }
    
    // 1. Generate initial thinking chain
    thinkingOutput, err := c.generateThinking(ctx, input.Task)
    
    // 2. Extract intermediate conclusions
    conclusions := c.parseThinking(thinkingOutput)
    
    // 3. Validate each conclusion
    for i, conclusion := range conclusions {
        output.ReasoningSteps = append(output.ReasoningSteps, core.ReasoningStep{
            Step:        i + 1,
            Action:      "Thinking",
            Description: conclusion,
            Success:     true,
        })
    }
    
    // 4. Execute tools if needed
    // ...
    
    return output, nil
}

func (c *ChainOfThoughtAgent) generateThinking(ctx context.Context, task string) (string, error) {
    prompt := fmt.Sprintf(c.thinkingPrompt, task)
    resp, err := c.llm.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{{Role: "user", Content: prompt}},
    })
    return resp.Content, err
}
```

### 12.3 Custom Parser for New Pattern

```go
// In /parsers/parser_cot.go
type CoTOutput struct {
    Thinking      []string               `json:"thinking"` // Reasoning steps
    Conclusion    string                 `json:"conclusion"`
    ToolUsage     []string               `json:"tool_usage,omitempty"`
    Confidence    float64                `json:"confidence"`
}

type CoTOutputParser struct {
    *BaseOutputParser[*CoTOutput]
    thinkingPattern *regexp.Regexp
}

func (p *CoTOutputParser) Parse(ctx context.Context, text string) (*CoTOutput, error) {
    // Parse structured thinking output
    result := &CoTOutput{Thinking: []string{}}
    
    // Extract thinking steps (e.g., "Let me think about this...")
    matches := p.thinkingPattern.FindAllString(text, -1)
    result.Thinking = matches
    
    // Extract final conclusion
    // ...
    
    return result, nil
}
```

### 12.4 Integration with Planning Module

```go
// In /planning/strategies.go
type ReflectiveStrategy struct {
    llm llm.Client
}

func (s *ReflectiveStrategy) Apply(ctx context.Context, plan *Plan, constraints PlanConstraints) (*Plan, error) {
    // Each step includes self-reflection
    for _, step := range plan.Steps {
        // After planning, ask LLM to reflect
        reflectionPrompt := fmt.Sprintf("Reflect on this step: %s", step.Description)
        
        reflection, err := s.llm.Complete(ctx, &llm.CompletionRequest{
            Messages: []llm.Message{{Role: "user", Content: reflectionPrompt}},
        })
        
        if step.Metadata == nil {
            step.Metadata = make(map[string]interface{})
        }
        step.Metadata["reflection"] = reflection.Content
    }
    
    return plan, nil
}

// Register:
planner.RegisterStrategy("reflective", &ReflectiveStrategy{llm: llmClient})
```

---

## 13. Key Files and Their Purposes

| Directory | Key Files | Purpose |
|-----------|-----------|---------|
| `/interfaces/` | `agent.go`, `tool.go` | Interface definitions |
| `/core/` | `agent.go`, `runnable.go`, `callback.go` | Base implementations |
| `/core/middleware/` | `middleware.go`, `advanced.go` | Middleware framework |
| `/builder/` | `builder.go` | Agent construction API |
| `/agents/react/` | `react.go`, `parser_react.go` | ReAct agent implementation |
| `/agents/executor/` | `executor_agent.go` | Tool execution agent |
| `/planning/` | `planner.go`, `strategies.go`, `executor.go` | Planning system |
| `/parsers/` | `parser_react.go`, `output_parser.go` | Output parsing |
| `/llm/` | `providers/*.go` | LLM client implementations |
| `/tools/` | `tool.go`, `registry.go` | Tool implementations |
| `/memory/` | `manager.go` | Memory/history management |
| `/observability/` | Tracing, metrics, logging | Monitoring and debugging |

---

## 14. Execution Flow Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│ User Code                                                        │
│ agent.Invoke(ctx, input)                                        │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
        ┌────────────────────────────────────┐
        │ Callback.OnStart()                 │
        └────────┬───────────────────────────┘
                 │
                 ▼
        ┌────────────────────────────────────┐
        │ buildPrompt()                      │
        │ - Format tools                     │
        │ - Add task description             │
        └────────┬───────────────────────────┘
                 │
        ┌────────▼──────────────────────────────────┐
        │ ReAct Loop (until Final Answer or maxSteps)
        │                                            │
        │  ┌─────────────────────────────────────┐ │
        │  │ Call LLM with prompt + scratchpad  │ │
        │  │ Callback.OnLLMStart/End()          │ │
        │  └────────┬────────────────────────────┘ │
        │           │                               │
        │           ▼                               │
        │  ┌─────────────────────────────────────┐ │
        │  │ Parse output (Thought/Action/...)  │ │
        │  │ Check for Final Answer              │ │
        │  └────────┬────────────────────────────┘ │
        │           │                               │
        │           ├─ Final Answer? → Exit Loop   │
        │           │                               │
        │           ▼                               │
        │  ┌─────────────────────────────────────┐ │
        │  │ Execute Tool                        │ │
        │  │ Callback.OnToolStart/End()          │ │
        │  └────────┬────────────────────────────┘ │
        │           │                               │
        │           ▼                               │
        │  ┌─────────────────────────────────────┐ │
        │  │ Update Scratchpad with observation │ │
        │  └────────┬────────────────────────────┘ │
        │           │                               │
        │           └──► Loop continues            │
        └────────┬──────────────────────────────────┘
                 │
                 ▼
        ┌────────────────────────────────────────┐
        │ Build Output with reasoning trace      │
        │ Callback.OnAgentFinish()               │
        └────────┬───────────────────────────────┘
                 │
                 ▼
        ┌────────────────────────────────────────┐
        │ Return AgentOutput                     │
        │ - Result                               │
        │ - ReasoningSteps (trace)               │
        │ - ToolCalls (execution record)         │
        │ - Metadata                             │
        └────────────────────────────────────────┘
```

---

## 15. Design Patterns Used

1. **Runnable Pattern** - LangChain-inspired execution interface
2. **Builder Pattern** - Fluent API for complex object construction
3. **Callback Pattern** - Monitoring and debugging hooks
4. **Middleware Pattern** - Cross-cutting concerns
5. **Strategy Pattern** - Multiple planning strategies
6. **Adapter Pattern** - Multiple LLM provider adapters
7. **Composition Pattern** - Agents/chains composition
8. **Decorator Pattern** - Middleware wrapping
9. **Template Method** - BaseAgent base behavior

---

## 16. Recommended Extensions

### High Priority (Common Use Cases)
1. **Chain-of-Thought (CoT)** - Step-by-step reasoning
2. **Self-Reflection** - Agent critiques own output
3. **Tree-of-Thought (ToT)** - Explores multiple reasoning paths
4. **Hierarchical Planning** - Multi-level goal decomposition
5. **Few-Shot Learning** - Example-based reasoning

### Medium Priority (Advanced Features)
1. **Ensemble Agents** - Multiple agents voting
2. **Debate Protocol** - Agents argue positions
3. **Knowledge Graphs** - Structured knowledge integration
4. **Causal Reasoning** - Cause-effect understanding
5. **Constraint Satisfaction** - Explicit constraint handling

### Research/Experimental
1. **Neural-Symbolic** - Combining neural + symbolic
2. **Meta-Learning** - Learning to learn
3. **Multi-Modal** - Image/text/audio reasoning
4. **Collaborative** - Agent-to-agent communication
5. **Continuous Learning** - Online adaptation

---

## 17. Key Takeaways for Extension

**To add a new reasoning pattern:**

1. **Minimal Approach**: Use Middleware to enhance existing agents
   - Least invasive
   - Reuses existing infrastructure
   - Good for filtering/enhancement

2. **Full Approach**: Create new Agent type
   - Complete control over reasoning
   - Can customize everything
   - Best for fundamentally different patterns

3. **Hybrid Approach**: New Agent + Custom Parser + Strategy
   - Comprehensive integration
   - Reuses builder and planning infrastructure
   - Enables full feature set

**Key Extension Points**:
- `/parsers/` for output parsing
- `/agents/` for new agent types
- `/core/middleware/` for interceptors
- `/planning/strategies.go` for planning logic
- `/llm/providers/` for new LLM integrations

**Follow Layer Rules**:
- Layer 3 imports Layer 1+2 only
- tools/ cannot import agents/
- No circular dependencies
- Always run `verify_imports.sh`

