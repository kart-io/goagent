# GoAgent Reasoning Patterns - Quick Reference Guide

## Overview

This guide shows you how to extend GoAgent with new reasoning patterns. The framework provides multiple extension points, from minimal (middleware) to comprehensive (new agent types).

---

## Pattern 1: Reflection/Self-Critique (Minimal - Middleware)

Add self-reflection after agent decisions.

**Files to create:**
- `/core/middleware/reflection.go`
- `/core/middleware/reflection_test.go`

**Integration:** Add to builder with `WithMiddleware()`

**Complexity:** Low | **Reuse:** High

```go
package middleware

type ReflectionMiddleware struct {
    llm llm.Client
}

func (m *ReflectionMiddleware) Execute(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
    // Execute main handler
    resp, err := req.Next(ctx, req)
    if err != nil {
        return nil, err
    }
    
    // Ask LLM to critique the response
    reflection, _ := m.llm.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{{
            Role: "user",
            Content: fmt.Sprintf("Critique this response for accuracy: %v", resp.Output),
        }},
    })
    
    resp.Metadata["reflection"] = reflection.Content
    return resp, nil
}
```

---

## Pattern 2: Chain-of-Thought (CoT) (Medium - Agent + Parser)

Generate step-by-step reasoning before answering.

**Files to create:**
- `/agents/cot/cot_agent.go`
- `/agents/cot/cot_agent_test.go`
- `/parsers/parser_cot.go`

**Integration:** Use like ReActAgent through builder

**Complexity:** Medium | **Reuse:** Medium

**Key Differences from ReAct:**
- No tool execution in thinking phase
- Generates complete reasoning chain upfront
- Then produces final answer
- Lower latency (fewer LLM calls)

```go
package cot

type ChainOfThoughtAgent struct {
    *core.BaseAgent
    llm            llm.Client
    tools          []tools.Tool
    thinkingPrompt string
    maxTokens      int
}

func (c *ChainOfThoughtAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    // Phase 1: Generate complete thinking chain
    thinkingPrompt := fmt.Sprintf(`Let's think about this step by step:

Question: %s

Reasoning:`, input.Task)
    
    thinkingResp, _ := c.llm.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{{Role: "user", Content: thinkingPrompt}},
        MaxTokens: c.maxTokens,
    })
    
    // Phase 2: Parse thinking into steps
    steps := c.parseThinking(thinkingResp.Content)
    output := &core.AgentOutput{
        ReasoningSteps: steps,
        ToolCalls:      []core.ToolCall{},
        Metadata:       make(map[string]interface{}),
    }
    
    // Phase 3: Execute tools if needed (based on reasoning)
    // ...extract tool calls from thinking and execute...
    
    return output, nil
}

func (c *ChainOfThoughtAgent) parseThinking(thinking string) []core.ReasoningStep {
    // Parse lines with numbered reasoning steps
    var steps []core.ReasoningStep
    lines := strings.Split(thinking, "\n")
    for i, line := range lines {
        if strings.TrimSpace(line) != "" {
            steps = append(steps, core.ReasoningStep{
                Step:        i + 1,
                Action:      "Thinking",
                Description: line,
                Success:     true,
            })
        }
    }
    return steps
}
```

---

## Pattern 3: Tree-of-Thought (ToT) (High - Advanced Agent)

Explore multiple reasoning paths in parallel.

**Files to create:**
- `/agents/tot/tot_agent.go`
- `/agents/tot/tot_agent_test.go`

**Integration:** Standalone agent type

**Complexity:** High | **Reuse:** Low

**Key Features:**
- Generates multiple reasoning branches
- Evaluates branch quality
- Prunes weak branches
- Combines results

```go
package tot

type TreeOfThoughtAgent struct {
    *core.BaseAgent
    llm              llm.Client
    tools            []tools.Tool
    branchingFactor  int // How many branches to explore
    maxDepth         int // Maximum tree depth
}

type ThoughtNode struct {
    Content     string
    Children    []*ThoughtNode
    Value       float64 // Evaluation score
    Complete    bool
}

func (t *TreeOfThoughtAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    // Build thought tree
    root := &ThoughtNode{Content: input.Task}
    
    // Recursively expand tree
    t.expandNode(ctx, root, 0)
    
    // Evaluate and select best path
    bestPath := t.selectBestPath(root)
    
    // Convert path to reasoning steps
    steps := t.pathToSteps(bestPath)
    
    return &core.AgentOutput{
        ReasoningSteps: steps,
        Result:         bestPath[len(bestPath)-1].Content,
    }, nil
}

func (t *TreeOfThoughtAgent) expandNode(ctx context.Context, node *ThoughtNode, depth int) error {
    if depth >= t.maxDepth {
        return nil
    }
    
    // Generate multiple branches
    for i := 0; i < t.branchingFactor; i++ {
        childContent, _ := t.llm.Complete(ctx, &llm.CompletionRequest{
            Messages: []llm.Message{{
                Role:    "user",
                Content: fmt.Sprintf("Continue thinking about: %s (branch %d)", node.Content, i),
            }},
        })
        
        child := &ThoughtNode{Content: childContent.Content}
        node.Children = append(node.Children, child)
        
        // Recursive expansion
        t.expandNode(ctx, child, depth+1)
    }
    
    return nil
}
```

---

## Pattern 4: Multi-Agent Debate (Medium - High)

Multiple agents argue different viewpoints, reach consensus.

**Files to create:**
- `/agents/debate/debate_agent.go`
- `/agents/debate/debate_agent_test.go`

**Integration:** Composite agent type

**Complexity:** Medium-High | **Reuse:** Medium

```go
package debate

type DebateAgent struct {
    *core.BaseAgent
    agents         []core.Agent // Multiple agents with different perspectives
    maxRounds      int
    evaluatorLLM   llm.Client
}

func (d *DebateAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
    // Round-robin debate
    positions := make([]string, len(d.agents))
    
    for round := 0; round < d.maxRounds; round++ {
        for i, agent := range d.agents {
            // Each agent refines their position
            output, _ := agent.Invoke(ctx, input)
            positions[i] = output.Result.(string)
            
            // Provide other positions as context
            input.Context["other_positions"] = positions
        }
    }
    
    // Evaluate and combine positions
    consensus := d.evaluateAndCombine(ctx, positions)
    
    return &core.AgentOutput{
        Result: consensus,
    }, nil
}
```

---

## Pattern 5: Hierarchical Planning (Planning-based)

Multi-level goal decomposition using planning module.

**Files to create:**
- `/planning/hierarchical_strategy.go` (extends existing)
- `/planning/hierarchical_strategy_test.go`

**Integration:** Register strategy with SmartPlanner

**Complexity:** Medium | **Reuse:** High

```go
package planning

type HierarchicalPlanningStrategy struct {
    llm llm.Client
}

func (h *HierarchicalPlanningStrategy) Apply(ctx context.Context, plan *Plan, constraints PlanConstraints) (*Plan, error) {
    // Decompose top-level goal into sub-goals
    subGoals := h.decomposeGoal(ctx, plan.Goal)
    
    var allSteps []*Step
    for _, subGoal := range subGoals {
        // Create sub-plan for each sub-goal
        subPlan, _ := h.createSubPlan(ctx, subGoal)
        allSteps = append(allSteps, subPlan.Steps...)
    }
    
    plan.Steps = allSteps
    return plan, nil
}
```

---

## Pattern 6: Few-Shot Learning (Strategy + Memory)

Learn from examples, apply to new problems.

**Files to create:**
- `/planning/few_shot_strategy.go`
- `/planning/few_shot_strategy_test.go`

**Integration:** Register strategy with SmartPlanner

**Complexity:** Medium | **Reuse:** High

```go
package planning

type FewShotStrategy struct {
    llm    llm.Client
    memory interfaces.MemoryManager
    numExamples int
}

func (f *FewShotStrategy) Apply(ctx context.Context, plan *Plan, constraints PlanConstraints) (*Plan, error) {
    // Retrieve similar past problems
    cases, _ := f.memory.SearchSimilarCases(ctx, plan.Goal, f.numExamples)
    
    // Build few-shot prompt with examples
    examplePrompt := f.buildExamplePrompt(cases)
    
    // Generate plan with examples
    prompt := fmt.Sprintf("%s\n\nNow plan for: %s", examplePrompt, plan.Goal)
    resp, _ := f.llm.Complete(ctx, &llm.CompletionRequest{
        Messages: []llm.Message{{Role: "user", Content: prompt}},
    })
    
    // Parse and return plan
    return f.parsePlan(resp.Content), nil
}
```

---

## Implementation Checklist

### For Any New Pattern:

- [ ] **Layer Compliance Check**
  - [ ] Code in correct layer (agents/ for Layer 3)
  - [ ] Only imports Layer 1+2 if in Layer 3
  - [ ] Run `./verify_imports.sh`

- [ ] **Core Components**
  - [ ] Implement main logic (Agent/Middleware/Strategy)
  - [ ] Create output parser if custom format
  - [ ] Implement Invoke() with reasoning trace
  - [ ] Return ReasoningSteps in output

- [ ] **Configuration**
  - [ ] Create Config struct
  - [ ] Implement NewXXX() factory
  - [ ] Support builder pattern integration

- [ ] **Testing**
  - [ ] Unit tests for core logic
  - [ ] Integration tests with LLM
  - [ ] Test with different tools
  - [ ] Aim for 80%+ coverage

- [ ] **Documentation**
  - [ ] Add doc comments
  - [ ] Create example in `/examples/`
  - [ ] Update README if major feature

- [ ] **CI/CD**
  - [ ] Run `make test`
  - [ ] Run `make lint`
  - [ ] Run `make check`
  - [ ] Verify imports

---

## Quick Decision Tree

```
Do you want to...

├─ Add a simple post-processing feature?
│  └─ Use Middleware (Reflection, Validation, etc.)
│
├─ Add alternative reasoning logic?
│  ├─ Similar to ReAct but different flow?
│  │  └─ Create new Agent + Parser
│  │
│  └─ Based on planning/decomposition?
│     └─ Create new Strategy
│
├─ Combine multiple agents?
│  └─ Create composite Agent (Debate, Ensemble)
│
└─ Completely different paradigm?
   └─ Custom Agent + Custom Parser + Custom Strategy
```

---

## Code Organization Template

```
/agents/yourpattern/
├─ yourpattern_agent.go      # Main agent implementation
├─ yourpattern_agent_test.go # Unit tests
├─ config.go (optional)      # Configuration structures
└─ README.md (optional)      # Pattern-specific docs

/parsers/
├─ parser_yourpattern.go     # Output parser
└─ parser_yourpattern_test.go

/planning/
├─ yourpattern_strategy.go   # Planning strategy
└─ yourpattern_strategy_test.go

/core/middleware/
├─ yourpattern_middleware.go # Middleware impl
└─ yourpattern_middleware_test.go

/examples/
└─ yourpattern_example.go    # Usage example
```

---

## Common Pitfalls

1. **Not capturing reasoning steps**
   - Always populate ReasoningSteps
   - Include Action, Description, Success

2. **Ignoring context/scratchpad**
   - For iterative patterns, maintain state
   - Feed previous results to LLM

3. **Circular imports**
   - Check import layering
   - Use interfaces from Layer 1
   - Run verify_imports.sh before commit

4. **Not testing with real LLM**
   - Mock for unit tests
   - Integration tests with real provider

5. **Hardcoded assumptions**
   - Make prompt configurable
   - Support different tool sets
   - Allow parameter tuning

---

## Performance Tips

1. **Minimize LLM calls**
   - Batch when possible
   - Use caching middleware
   - Reuse contexts

2. **Parallel execution**
   - Use goroutines for independent branches
   - Leverage Batch() for multiple inputs

3. **Memory efficiency**
   - Don't store full LLM responses unnecessarily
   - Use streaming for large outputs

4. **Timeout configuration**
   - Set reasonable maxSteps defaults
   - Use builder config for overrides

---

## Testing Examples

```go
func TestChainOfThoughtAgent(t *testing.T) {
    // Mock LLM
    mockLLM := &MockLLMClient{
        response: "Step 1: Understand...\nStep 2: Plan...",
    }
    
    agent := cot.NewChainOfThoughtAgent(cot.Config{
        LLM: mockLLM,
    })
    
    output, err := agent.Invoke(context.Background(), &core.AgentInput{
        Task: "Solve math problem",
    })
    
    assert.NoError(t, err)
    assert.Equal(t, "success", output.Status)
    assert.NotEmpty(t, output.ReasoningSteps)
}
```

---

## Resources

- Full architecture: `ARCHITECTURE_ANALYSIS.md`
- Layer compliance: `docs/architecture/IMPORT_LAYERING.md`
- Testing guide: `docs/development/TESTING_BEST_PRACTICES.md`
- ReAct example: `/agents/react/react.go`
- Planning example: `/planning/planner.go`

