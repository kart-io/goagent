# GoAgent Reasoning Patterns Extension Implementation

## Overview

This document outlines the implementation of advanced reasoning patterns for the GoAgent framework, extending beyond the existing ReAct agent to support:

- **Chain-of-Thought (CoT)** ✅ - Linear step-by-step reasoning
- **Tree-of-Thought (ToT)** ✅ - Tree-based search with multiple reasoning paths
- **Graph-of-Thought (GoT)** - Graph-based reasoning with complex dependencies
- **Program-of-Thought (PoT)** - Code generation and execution for reasoning
- **Skeleton-of-Thought (SoT)** - Parallel reasoning with skeleton structure
- **Meta-CoT / Self-Ask** - Self-questioning and meta-reasoning

## Implementation Status

### ✅ Completed

1. **Interfaces** (`interfaces/reasoning.go`)
   - Core reasoning pattern interfaces
   - Data structures for thoughts, nodes, and reasoning steps
   - Strategy definitions for search algorithms

2. **Chain-of-Thought Agent** (`agents/cot/cot.go`)
   - Zero-shot and few-shot CoT
   - Step-by-step reasoning with justification
   - Tool integration support
   - Configurable prompting strategies

3. **Tree-of-Thought Agent** (`agents/tot/tot.go`)
   - Multiple search strategies (DFS, BFS, Beam Search, MCTS)
   - Thought generation and evaluation
   - Pruning and backtracking
   - Solution path extraction

### 🚧 To Be Implemented

4. **Graph-of-Thought Agent** (`agents/got/`)
5. **Program-of-Thought Agent** (`agents/pot/`)
6. **Skeleton-of-Thought Agent** (`agents/sot/`)
7. **Meta-CoT / Self-Ask Agent** (`agents/metacot/`)

## Architecture Design

### Layer Organization (Following GoAgent's 4-Layer Structure)

```
Layer 1: Foundation (interfaces/)
├── reasoning.go - Core interfaces and types

Layer 2: Business Logic (core/)
├── (uses existing core components)

Layer 3: Implementation (agents/)
├── cot/
│   ├── cot.go - Chain-of-Thought implementation
│   └── cot_test.go
├── tot/
│   ├── tot.go - Tree-of-Thought implementation
│   └── tot_test.go
├── got/
│   ├── got.go - Graph-of-Thought implementation
│   └── got_test.go
├── pot/
│   ├── pot.go - Program-of-Thought implementation
│   └── pot_test.go
├── sot/
│   ├── sot.go - Skeleton-of-Thought implementation
│   └── sot_test.go
└── metacot/
    ├── metacot.go - Meta-CoT/Self-Ask implementation
    └── metacot_test.go

Layer 4: Examples & Tests
└── examples/reasoning/
    ├── cot_example.go
    ├── tot_example.go
    ├── got_example.go
    ├── pot_example.go
    ├── sot_example.go
    └── metacot_example.go
```

## Integration with Builder Pattern

The reasoning agents will be integrated with the existing `AgentBuilder` through presets:

```go
// In builder/presets.go
func (b *AgentBuilder) WithChainOfThought(config ...cot.CoTConfig) *AgentBuilder
func (b *AgentBuilder) WithTreeOfThought(config ...tot.ToTConfig) *AgentBuilder
func (b *AgentBuilder) WithGraphOfThought(config ...got.GoTConfig) *AgentBuilder
func (b *AgentBuilder) WithProgramOfThought(config ...pot.PoTConfig) *AgentBuilder
func (b *AgentBuilder) WithSkeletonOfThought(config ...sot.SoTConfig) *AgentBuilder
func (b *AgentBuilder) WithMetaCoT(config ...metacot.MetaCoTConfig) *AgentBuilder
```

## Quick Implementation Guide for Remaining Patterns

### Graph-of-Thought (GoT)

```go
// Key features:
// - Directed acyclic graph structure
// - Multiple dependency relationships
// - Parallel branch exploration
// - Merge points for combining thoughts

type GoTAgent struct {
    graph *ThoughtGraph
    // Topological sorting
    // Cycle detection
    // Parallel execution of independent nodes
}
```

### Program-of-Thought (PoT)

```go
// Key features:
// - Generate executable code
// - Support multiple languages (Python, JavaScript)
// - Sandboxed execution
// - Result interpretation

type PoTAgent struct {
    codeGen CodeGenerator
    executor CodeExecutor
    // Language detection
    // Safety checks
    // Result parsing
}
```

### Skeleton-of-Thought (SoT)

```go
// Key features:
// - Parallel sub-problem decomposition
// - Skeleton point generation
// - Concurrent elaboration
// - Result aggregation

type SoTAgent struct {
    skeleton []SkeletonPoint
    // Parallel execution
    // Dependency resolution
    // Aggregation strategy
}
```

### Meta-CoT / Self-Ask

```go
// Key features:
// - Self-questioning mechanism
// - Follow-up question generation
// - Answer verification
// - Recursive reasoning

type MetaCoTAgent struct {
    questionGen QuestionGenerator
    answerVerifier AnswerVerifier
    // Self-critique
    // Question decomposition
    // Answer synthesis
}
```

## Usage Examples

### Chain-of-Thought

```go
agent := builder.NewAgentBuilder(llmClient).
    WithChainOfThought(cot.CoTConfig{
        ZeroShot: true,
        ShowStepNumbers: true,
    }).
    Build()

result, err := agent.Invoke(ctx, &AgentInput{
    Task: "Calculate the total cost if items are $15, $23, and $47 with 8% tax",
})
```

### Tree-of-Thought

```go
agent := builder.NewAgentBuilder(llmClient).
    WithTreeOfThought(tot.ToTConfig{
        MaxDepth: 5,
        BranchingFactor: 3,
        SearchStrategy: interfaces.StrategyBeamSearch,
        BeamWidth: 2,
    }).
    Build()

result, err := agent.Invoke(ctx, &AgentInput{
    Task: "Solve the 24 game with numbers 3, 3, 8, 8",
})
```

## Performance Considerations

1. **CoT**: Low overhead, single LLM call for zero-shot
2. **ToT**: Higher cost due to multiple branches (O(b^d) worst case)
3. **GoT**: Parallel execution can improve performance
4. **PoT**: Code execution adds latency
5. **SoT**: Parallel processing reduces overall time
6. **Meta-CoT**: Multiple rounds increase token usage

## Testing Strategy

Each agent should have:
1. Unit tests for core logic
2. Integration tests with mock LLM
3. Benchmark tests for performance
4. Example demonstrations

## Next Steps

1. Implement remaining agents (GoT, PoT, SoT, Meta-CoT)
2. Create builder presets
3. Add comprehensive tests
4. Create usage examples
5. Run import verification
6. Update documentation

## Key Benefits

- **Modularity**: Each reasoning pattern is independent
- **Composability**: Can combine patterns via middleware
- **Extensibility**: Easy to add new reasoning strategies
- **Performance**: Optimized for each pattern's characteristics
- **Compatibility**: Follows GoAgent's architecture principles