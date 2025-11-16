package builder

import (
	"time"

	"github.com/kart-io/goagent/agents/cot"
	"github.com/kart-io/goagent/agents/got"
	"github.com/kart-io/goagent/agents/metacot"
	"github.com/kart-io/goagent/agents/pot"
	"github.com/kart-io/goagent/agents/react"
	"github.com/kart-io/goagent/agents/sot"
	"github.com/kart-io/goagent/agents/tot"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
)

// ReasoningPresets provides fluent API methods for creating agents with different reasoning patterns
//
// These methods extend AgentBuilder to easily create agents with specific reasoning strategies:
// - Chain-of-Thought (CoT)
// - Tree-of-Thought (ToT)
// - Graph-of-Thought (GoT)
// - Program-of-Thought (PoT)
// - Skeleton-of-Thought (SoT)
// - Meta-CoT / Self-Ask

// WithChainOfThought creates a Chain-of-Thought agent
//
// Example:
//   agent := NewAgentBuilder(llm).
//     WithChainOfThought(cot.CoTConfig{
//       ZeroShot: true,
//       ShowStepNumbers: true,
//     }).
//     Build()
func (b *AgentBuilder[C, S]) WithChainOfThought(config ...cot.CoTConfig) *AgentBuilder[C, S] {
	cfg := cot.CoTConfig{
		Name:        "chain-of-thought",
		Description: "Agent that uses step-by-step reasoning",
		LLM:         b.llmClient,
		Tools:       b.tools,
		MaxSteps:    10,
		ZeroShot:    true,
	}

	// Merge with provided config if any
	if len(config) > 0 {
		provided := config[0]
		if provided.Name != "" {
			cfg.Name = provided.Name
		}
		if provided.Description != "" {
			cfg.Description = provided.Description
		}
		if provided.MaxSteps > 0 {
			cfg.MaxSteps = provided.MaxSteps
		}
		if provided.ShowStepNumbers {
			cfg.ShowStepNumbers = provided.ShowStepNumbers
		}
		if provided.RequireJustification {
			cfg.RequireJustification = provided.RequireJustification
		}
		if provided.FinalAnswerFormat != "" {
			cfg.FinalAnswerFormat = provided.FinalAnswerFormat
		}
		if provided.FewShot {
			cfg.FewShot = provided.FewShot
			cfg.ZeroShot = false
		}
		if len(provided.FewShotExamples) > 0 {
			cfg.FewShotExamples = provided.FewShotExamples
		}
	}

	// Store the agent configuration for Build()
	b.metadata["reasoning_pattern"] = "cot"
	b.metadata["cot_config"] = cfg

	return b
}

// WithTreeOfThought creates a Tree-of-Thought agent
//
// Example:
//   agent := NewAgentBuilder(llm).
//     WithTreeOfThought(tot.ToTConfig{
//       MaxDepth: 5,
//       BranchingFactor: 3,
//       SearchStrategy: interfaces.StrategyBeamSearch,
//     }).
//     Build()
func (b *AgentBuilder[C, S]) WithTreeOfThought(config ...tot.ToTConfig) *AgentBuilder[C, S] {
	cfg := tot.ToTConfig{
		Name:             "tree-of-thought",
		Description:      "Agent that explores multiple reasoning paths",
		LLM:              b.llmClient,
		Tools:            b.tools,
		MaxDepth:         5,
		BranchingFactor:  3,
		BeamWidth:        2,
		SearchStrategy:   interfaces.StrategyBeamSearch,
		EvaluationMethod: "llm",
		PruneThreshold:   0.3,
	}

	// Merge with provided config if any
	if len(config) > 0 {
		provided := config[0]
		if provided.Name != "" {
			cfg.Name = provided.Name
		}
		if provided.Description != "" {
			cfg.Description = provided.Description
		}
		if provided.MaxDepth > 0 {
			cfg.MaxDepth = provided.MaxDepth
		}
		if provided.BranchingFactor > 0 {
			cfg.BranchingFactor = provided.BranchingFactor
		}
		if provided.BeamWidth > 0 {
			cfg.BeamWidth = provided.BeamWidth
		}
		if provided.SearchStrategy != "" {
			cfg.SearchStrategy = provided.SearchStrategy
		}
		if provided.EvaluationMethod != "" {
			cfg.EvaluationMethod = provided.EvaluationMethod
		}
		if provided.PruneThreshold > 0 {
			cfg.PruneThreshold = provided.PruneThreshold
		}
	}

	// Store the agent configuration for Build()
	b.metadata["reasoning_pattern"] = "tot"
	b.metadata["tot_config"] = cfg

	return b
}

// WithReAct creates a ReAct agent (existing pattern)
//
// Example:
//   agent := NewAgentBuilder(llm).
//     WithReAct(react.ReActConfig{
//       MaxSteps: 10,
//       StopPattern: []string{"Final Answer:"},
//     }).
//     Build()
func (b *AgentBuilder[C, S]) WithReAct(config ...react.ReActConfig) *AgentBuilder[C, S] {
	cfg := react.ReActConfig{
		Name:        "react",
		Description: "Agent that uses Thought-Action-Observation loops",
		LLM:         b.llmClient,
		Tools:       b.tools,
		MaxSteps:    10,
	}

	// Merge with provided config if any
	if len(config) > 0 {
		provided := config[0]
		if provided.Name != "" {
			cfg.Name = provided.Name
		}
		if provided.Description != "" {
			cfg.Description = provided.Description
		}
		if provided.MaxSteps > 0 {
			cfg.MaxSteps = provided.MaxSteps
		}
		if len(provided.StopPattern) > 0 {
			cfg.StopPattern = provided.StopPattern
		}
		if provided.PromptPrefix != "" {
			cfg.PromptPrefix = provided.PromptPrefix
		}
		if provided.PromptSuffix != "" {
			cfg.PromptSuffix = provided.PromptSuffix
		}
		if provided.FormatInstr != "" {
			cfg.FormatInstr = provided.FormatInstr
		}
	}

	// Store the agent configuration for Build()
	b.metadata["reasoning_pattern"] = "react"
	b.metadata["react_config"] = cfg

	return b
}

// BuildReasoningAgent builds an agent based on the configured reasoning pattern
//
// This method is called internally by Build() when a reasoning pattern is configured
func (b *AgentBuilder[C, S]) BuildReasoningAgent() core.Agent {
	pattern, exists := b.metadata["reasoning_pattern"].(string)
	if !exists {
		// Default to ReAct if no pattern specified
		pattern = "react"
	}

	var agent core.Agent

	switch pattern {
	case "cot":
		if cfg, ok := b.metadata["cot_config"].(cot.CoTConfig); ok {
			cotAgent := cot.NewCoTAgent(cfg)
			// Apply callbacks and middlewares
			if len(b.callbacks) > 0 {
				cotAgent = cotAgent.WithCallbacks(b.callbacks...).(*cot.CoTAgent)
			}
			agent = cotAgent
		}

	case "tot":
		if cfg, ok := b.metadata["tot_config"].(tot.ToTConfig); ok {
			totAgent := tot.NewToTAgent(cfg)
			// Apply callbacks and middlewares
			if len(b.callbacks) > 0 {
				totAgent = totAgent.WithCallbacks(b.callbacks...).(*tot.ToTAgent)
			}
			agent = totAgent
		}

	case "react":
		if cfg, ok := b.metadata["react_config"].(react.ReActConfig); ok {
			reactAgent := react.NewReActAgent(cfg)
			// Apply callbacks and middlewares
			if len(b.callbacks) > 0 {
				reactAgent = reactAgent.WithCallbacks(b.callbacks...).(*react.ReActAgent)
			}
			agent = reactAgent
		}

	default:
		// Fallback to basic ReAct
		reactAgent := react.NewReActAgent(react.ReActConfig{
			Name:        "default-react",
			Description: "Default ReAct agent",
			LLM:         b.llmClient,
			Tools:       b.tools,
			MaxSteps:    10,
		})
		agent = reactAgent
	}

	// TODO: Apply middlewares if configured
	// Middleware integration needs to be implemented based on
	// the actual middleware application pattern in GoAgent

	return agent
}

// Preset configurations for quick setup

// WithZeroShotCoT creates a zero-shot Chain-of-Thought agent
//
// Example:
//   agent := NewAgentBuilder(llm).WithZeroShotCoT().Build()
func (b *AgentBuilder[C, S]) WithZeroShotCoT() *AgentBuilder[C, S] {
	return b.WithChainOfThought(cot.CoTConfig{
		ZeroShot:        true,
		ShowStepNumbers: true,
		RequireJustification: true,
	})
}

// WithFewShotCoT creates a few-shot Chain-of-Thought agent with examples
//
// Example:
//   agent := NewAgentBuilder(llm).
//     WithFewShotCoT(examples).
//     Build()
func (b *AgentBuilder[C, S]) WithFewShotCoT(examples []cot.CoTExample) *AgentBuilder[C, S] {
	return b.WithChainOfThought(cot.CoTConfig{
		FewShot:         true,
		FewShotExamples: examples,
		ShowStepNumbers: true,
	})
}

// WithBeamSearchToT creates a Tree-of-Thought agent with beam search
//
// Example:
//   agent := NewAgentBuilder(llm).
//     WithBeamSearchToT(beamWidth, maxDepth).
//     Build()
func (b *AgentBuilder[C, S]) WithBeamSearchToT(beamWidth, maxDepth int) *AgentBuilder[C, S] {
	return b.WithTreeOfThought(tot.ToTConfig{
		MaxDepth:       maxDepth,
		BeamWidth:      beamWidth,
		SearchStrategy: interfaces.StrategyBeamSearch,
	})
}

// WithMonteCarloToT creates a Tree-of-Thought agent with Monte Carlo Tree Search
//
// Example:
//   agent := NewAgentBuilder(llm).WithMonteCarloToT().Build()
func (b *AgentBuilder[C, S]) WithMonteCarloToT() *AgentBuilder[C, S] {
	return b.WithTreeOfThought(tot.ToTConfig{
		SearchStrategy:  interfaces.StrategyMonteCarlo,
		BranchingFactor: 4,
		MaxDepth:        6,
	})
}

// WithGraphOfThought creates a Graph-of-Thought agent
//
// Example:
//   agent := NewAgentBuilder(llm).
//     WithGraphOfThought(got.GoTConfig{
//       MaxNodes: 50,
//       ParallelExecution: true,
//     }).
//     Build()
func (b *AgentBuilder[C, S]) WithGraphOfThought(config ...got.GoTConfig) *AgentBuilder[C, S] {
	cfg := got.GoTConfig{
		Name:              "graph-of-thought",
		Description:       "Agent that uses graph-based reasoning",
		LLM:               b.llmClient,
		Tools:             b.tools,
		MaxNodes:          50,
		MaxEdgesPerNode:   5,
		ParallelExecution: true,
		MergeStrategy:     "weighted",
		CycleDetection:    true,
		PruneThreshold:    0.3,
	}

	// Merge with provided config if any
	if len(config) > 0 {
		provided := config[0]
		if provided.Name != "" {
			cfg.Name = provided.Name
		}
		if provided.Description != "" {
			cfg.Description = provided.Description
		}
		if provided.MaxNodes > 0 {
			cfg.MaxNodes = provided.MaxNodes
		}
		if provided.MaxEdgesPerNode > 0 {
			cfg.MaxEdgesPerNode = provided.MaxEdgesPerNode
		}
		if provided.MergeStrategy != "" {
			cfg.MergeStrategy = provided.MergeStrategy
		}
		cfg.ParallelExecution = provided.ParallelExecution
		cfg.CycleDetection = provided.CycleDetection
		if provided.PruneThreshold > 0 {
			cfg.PruneThreshold = provided.PruneThreshold
		}
	}

	b.metadata["reasoning_pattern"] = "got"
	b.metadata["got_config"] = cfg
	return b
}

// WithProgramOfThought creates a Program-of-Thought agent
//
// Example:
//   agent := NewAgentBuilder(llm).
//     WithProgramOfThought(pot.PoTConfig{
//       Language: "python",
//       SafeMode: true,
//     }).
//     Build()
func (b *AgentBuilder[C, S]) WithProgramOfThought(config ...pot.PoTConfig) *AgentBuilder[C, S] {
	cfg := pot.PoTConfig{
		Name:             "program-of-thought",
		Description:      "Agent that generates and executes code",
		LLM:              b.llmClient,
		Tools:            b.tools,
		Language:         "python",
		AllowedLanguages: []string{"python", "javascript"},
		MaxCodeLength:    2000,
		ExecutionTimeout: 10 * time.Second,
		SafeMode:         true,
		MaxIterations:    3,
	}

	// Merge with provided config if any
	if len(config) > 0 {
		provided := config[0]
		if provided.Name != "" {
			cfg.Name = provided.Name
		}
		if provided.Description != "" {
			cfg.Description = provided.Description
		}
		if provided.Language != "" {
			cfg.Language = provided.Language
		}
		if len(provided.AllowedLanguages) > 0 {
			cfg.AllowedLanguages = provided.AllowedLanguages
		}
		if provided.MaxCodeLength > 0 {
			cfg.MaxCodeLength = provided.MaxCodeLength
		}
		if provided.ExecutionTimeout > 0 {
			cfg.ExecutionTimeout = provided.ExecutionTimeout
		}
		cfg.SafeMode = provided.SafeMode
		if provided.MaxIterations > 0 {
			cfg.MaxIterations = provided.MaxIterations
		}
	}

	b.metadata["reasoning_pattern"] = "pot"
	b.metadata["pot_config"] = cfg
	return b
}

// WithSkeletonOfThought creates a Skeleton-of-Thought agent
//
// Example:
//   agent := NewAgentBuilder(llm).
//     WithSkeletonOfThought(sot.SoTConfig{
//       MaxConcurrency: 5,
//       AggregationStrategy: "hierarchical",
//     }).
//     Build()
func (b *AgentBuilder[C, S]) WithSkeletonOfThought(config ...sot.SoTConfig) *AgentBuilder[C, S] {
	cfg := sot.SoTConfig{
		Name:                "skeleton-of-thought",
		Description:         "Agent that uses parallel skeleton elaboration",
		LLM:                 b.llmClient,
		Tools:               b.tools,
		MaxSkeletonPoints:   10,
		MinSkeletonPoints:   3,
		AutoDecompose:       true,
		MaxConcurrency:      5,
		ElaborationTimeout:  30 * time.Second,
		AggregationStrategy: "sequential",
		DependencyAware:     true,
	}

	// Merge with provided config if any
	if len(config) > 0 {
		provided := config[0]
		if provided.Name != "" {
			cfg.Name = provided.Name
		}
		if provided.Description != "" {
			cfg.Description = provided.Description
		}
		if provided.MaxSkeletonPoints > 0 {
			cfg.MaxSkeletonPoints = provided.MaxSkeletonPoints
		}
		if provided.MinSkeletonPoints > 0 {
			cfg.MinSkeletonPoints = provided.MinSkeletonPoints
		}
		cfg.AutoDecompose = provided.AutoDecompose
		if provided.MaxConcurrency > 0 {
			cfg.MaxConcurrency = provided.MaxConcurrency
		}
		if provided.ElaborationTimeout > 0 {
			cfg.ElaborationTimeout = provided.ElaborationTimeout
		}
		if provided.AggregationStrategy != "" {
			cfg.AggregationStrategy = provided.AggregationStrategy
		}
		cfg.DependencyAware = provided.DependencyAware
	}

	b.metadata["reasoning_pattern"] = "sot"
	b.metadata["sot_config"] = cfg
	return b
}

// WithMetaCoT creates a Meta-CoT / Self-Ask agent
//
// Example:
//   agent := NewAgentBuilder(llm).
//     WithMetaCoT(metacot.MetaCoTConfig{
//       MaxQuestions: 5,
//       SelfCritique: true,
//     }).
//     Build()
func (b *AgentBuilder[C, S]) WithMetaCoT(config ...metacot.MetaCoTConfig) *AgentBuilder[C, S] {
	cfg := metacot.MetaCoTConfig{
		Name:                "meta-cot",
		Description:         "Agent that uses self-questioning and meta-reasoning",
		LLM:                 b.llmClient,
		Tools:               b.tools,
		MaxQuestions:        5,
		MaxDepth:            3,
		AutoDecompose:       true,
		RequireEvidence:     true,
		SelfCritique:        true,
		QuestionStrategy:    "focused",
		VerifyAnswers:       true,
		ConfidenceThreshold: 0.7,
	}

	// Merge with provided config if any
	if len(config) > 0 {
		provided := config[0]
		if provided.Name != "" {
			cfg.Name = provided.Name
		}
		if provided.Description != "" {
			cfg.Description = provided.Description
		}
		if provided.MaxQuestions > 0 {
			cfg.MaxQuestions = provided.MaxQuestions
		}
		if provided.MaxDepth > 0 {
			cfg.MaxDepth = provided.MaxDepth
		}
		cfg.AutoDecompose = provided.AutoDecompose
		cfg.RequireEvidence = provided.RequireEvidence
		cfg.SelfCritique = provided.SelfCritique
		if provided.QuestionStrategy != "" {
			cfg.QuestionStrategy = provided.QuestionStrategy
		}
		cfg.VerifyAnswers = provided.VerifyAnswers
		if provided.ConfidenceThreshold > 0 {
			cfg.ConfidenceThreshold = provided.ConfidenceThreshold
		}
	}

	b.metadata["reasoning_pattern"] = "metacot"
	b.metadata["metacot_config"] = cfg
	return b
}