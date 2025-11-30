package builder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestWithChainOfThought 测试 Chain-of-Thought 推理预设
func TestWithChainOfThought(t *testing.T) {
	mockLLM := NewMockLLMClient("Step 1: Analyze problem\nStep 2: Find solution\nAnswer: 42")

	t.Run("default_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithChainOfThought()

		assert.NotNil(t, builder)
		assert.Equal(t, "cot", builder.metadata["reasoning_pattern"])

		cfg, ok := builder.metadata["cot_config"].(cot.CoTConfig)
		require.True(t, ok)
		assert.Equal(t, "chain-of-thought", cfg.Name)
		assert.True(t, cfg.ZeroShot)
		assert.Equal(t, 10, cfg.MaxSteps)
	})

	t.Run("custom_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithChainOfThought(cot.CoTConfig{
				Name:                 "custom-cot",
				MaxSteps:             5,
				ShowStepNumbers:      true,
				RequireJustification: true,
				FinalAnswerFormat:    "JSON",
			})

		assert.NotNil(t, builder)
		cfg, ok := builder.metadata["cot_config"].(cot.CoTConfig)
		require.True(t, ok)
		assert.Equal(t, "custom-cot", cfg.Name)
		assert.Equal(t, 5, cfg.MaxSteps)
		assert.True(t, cfg.ShowStepNumbers)
		assert.True(t, cfg.RequireJustification)
		assert.Equal(t, "JSON", cfg.FinalAnswerFormat)
	})

	t.Run("few_shot_mode", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithChainOfThought(cot.CoTConfig{
				FewShot: true,
			})

		cfg, ok := builder.metadata["cot_config"].(cot.CoTConfig)
		require.True(t, ok)
		assert.True(t, cfg.FewShot)
		assert.False(t, cfg.ZeroShot)
	})
}

// TestWithTreeOfThought 测试 Tree-of-Thought 推理预设
func TestWithTreeOfThought(t *testing.T) {
	mockLLM := NewMockLLMClient("Exploring multiple paths...")

	t.Run("default_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithTreeOfThought()

		assert.NotNil(t, builder)
		assert.Equal(t, "tot", builder.metadata["reasoning_pattern"])

		cfg, ok := builder.metadata["tot_config"].(tot.ToTConfig)
		require.True(t, ok)
		assert.Equal(t, "tree-of-thought", cfg.Name)
		assert.Equal(t, 5, cfg.MaxDepth)
		assert.Equal(t, 3, cfg.BranchingFactor)
	})

	t.Run("beam_search", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithTreeOfThought(tot.ToTConfig{
				MaxDepth:        7,
				BranchingFactor: 4,
				SearchStrategy:  interfaces.StrategyBeamSearch,
				BeamWidth:       2,
			})

		cfg, ok := builder.metadata["tot_config"].(tot.ToTConfig)
		require.True(t, ok)
		assert.Equal(t, 7, cfg.MaxDepth)
		assert.Equal(t, 4, cfg.BranchingFactor)
		assert.Equal(t, interfaces.StrategyBeamSearch, cfg.SearchStrategy)
		assert.Equal(t, 2, cfg.BeamWidth)
	})

	t.Run("monte_carlo_search", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithTreeOfThought(tot.ToTConfig{
				SearchStrategy: interfaces.StrategyMonteCarlo,
			})

		cfg, ok := builder.metadata["tot_config"].(tot.ToTConfig)
		require.True(t, ok)
		assert.Equal(t, interfaces.StrategyMonteCarlo, cfg.SearchStrategy)
	})
}

// TestWithReAct 测试 ReAct 推理预设
func TestWithReAct(t *testing.T) {
	mockLLM := NewMockLLMClient(
		"Thought: I need to search for information",
		"Action: search",
		"Observation: Found relevant data",
		"Final Answer: The result is 42",
	)

	t.Run("default_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithReAct()

		assert.NotNil(t, builder)
		assert.Equal(t, "react", builder.metadata["reasoning_pattern"])

		cfg, ok := builder.metadata["react_config"].(react.ReActConfig)
		require.True(t, ok)
		assert.Equal(t, "react", cfg.Name)
		assert.Equal(t, 10, cfg.MaxSteps)
	})

	t.Run("custom_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithReAct(react.ReActConfig{
				Name:     "custom-react",
				MaxSteps: 15,
			})

		cfg, ok := builder.metadata["react_config"].(react.ReActConfig)
		require.True(t, ok)
		assert.Equal(t, "custom-react", cfg.Name)
		assert.Equal(t, 15, cfg.MaxSteps)
	})
}

// TestBuildReasoningAgent 测试构建推理 Agent
func TestBuildReasoningAgent(t *testing.T) {
	mockLLM := NewMockLLMClient("Reasoning response")

	t.Run("build_cot_agent", func(t *testing.T) {
		agent := NewAgentBuilder[any, core.State](mockLLM).
			WithChainOfThought().
			BuildReasoningAgent()

		assert.NotNil(t, agent)
	})

	t.Run("build_tot_agent", func(t *testing.T) {
		agent := NewAgentBuilder[any, core.State](mockLLM).
			WithTreeOfThought().
			BuildReasoningAgent()

		assert.NotNil(t, agent)
	})

	t.Run("build_react_agent", func(t *testing.T) {
		agent := NewAgentBuilder[any, core.State](mockLLM).
			WithReAct().
			BuildReasoningAgent()

		assert.NotNil(t, agent)
	})

	t.Run("no_reasoning_pattern", func(t *testing.T) {
		// BuildReasoningAgent defaults to ReAct if no pattern specified
		agent := NewAgentBuilder[any, core.State](mockLLM).
			BuildReasoningAgent()

		assert.NotNil(t, agent)
	})
}

// TestWithZeroShotCoT 测试 Zero-Shot CoT
func TestWithZeroShotCoT(t *testing.T) {
	mockLLM := NewMockLLMClient("Let's think step by step...")

	builder := NewAgentBuilder[any, core.State](mockLLM).
		WithZeroShotCoT()

	assert.NotNil(t, builder)
	cfg, ok := builder.metadata["cot_config"].(cot.CoTConfig)
	require.True(t, ok)
	assert.True(t, cfg.ZeroShot)
	assert.False(t, cfg.FewShot)
	assert.Equal(t, "chain-of-thought", cfg.Name)
	assert.True(t, cfg.ShowStepNumbers)
	assert.True(t, cfg.RequireJustification)
}

// TestWithFewShotCoT 测试 Few-Shot CoT
func TestWithFewShotCoT(t *testing.T) {
	mockLLM := NewMockLLMClient("Following examples...")

	examples := []cot.CoTExample{
		{
			Question: "What is 2+2?",
			Steps:    []string{"Add 2 and 2", "The result is 4"},
			Answer:   "4",
		},
		{
			Question: "What is 3*3?",
			Steps:    []string{"Multiply 3 by 3", "The result is 9"},
			Answer:   "9",
		},
	}

	builder := NewAgentBuilder[any, core.State](mockLLM).
		WithFewShotCoT(examples)

	assert.NotNil(t, builder)
	cfg, ok := builder.metadata["cot_config"].(cot.CoTConfig)
	require.True(t, ok)
	assert.True(t, cfg.FewShot)
	assert.False(t, cfg.ZeroShot)
	assert.Equal(t, "chain-of-thought", cfg.Name)
	assert.Len(t, cfg.FewShotExamples, 2)
	assert.True(t, cfg.ShowStepNumbers)
}

// TestWithBeamSearchToT 测试 Beam Search ToT
func TestWithBeamSearchToT(t *testing.T) {
	mockLLM := NewMockLLMClient("Beam search in progress...")

	builder := NewAgentBuilder[any, core.State](mockLLM).
		WithBeamSearchToT(3, 5)

	assert.NotNil(t, builder)
	cfg, ok := builder.metadata["tot_config"].(tot.ToTConfig)
	require.True(t, ok)
	assert.Equal(t, "tree-of-thought", cfg.Name)
	assert.Equal(t, interfaces.StrategyBeamSearch, cfg.SearchStrategy)
	assert.Equal(t, 3, cfg.BeamWidth)
	assert.Equal(t, 5, cfg.MaxDepth)
}

// TestWithMonteCarloToT 测试 Monte Carlo ToT
func TestWithMonteCarloToT(t *testing.T) {
	mockLLM := NewMockLLMClient("Monte Carlo simulation...")

	t.Run("default_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithMonteCarloToT()

		cfg, ok := builder.metadata["tot_config"].(tot.ToTConfig)
		require.True(t, ok)
		assert.Equal(t, "tree-of-thought", cfg.Name)
		assert.Equal(t, interfaces.StrategyMonteCarlo, cfg.SearchStrategy)
		assert.Equal(t, 4, cfg.BranchingFactor)
		assert.Equal(t, 6, cfg.MaxDepth)
	})
}

// TestWithGraphOfThought 测试 Graph-of-Thought
func TestWithGraphOfThought(t *testing.T) {
	mockLLM := NewMockLLMClient("Building knowledge graph...")

	t.Run("default_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithGraphOfThought()

		assert.NotNil(t, builder)
		assert.Equal(t, "got", builder.metadata["reasoning_pattern"])

		cfg, ok := builder.metadata["got_config"].(got.GoTConfig)
		require.True(t, ok)
		assert.Equal(t, "graph-of-thought", cfg.Name)
		assert.Equal(t, 50, cfg.MaxNodes)
	})

	t.Run("custom_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithGraphOfThought(got.GoTConfig{
				Name:              "custom-got",
				MaxNodes:          20,
				MaxEdgesPerNode:   5,
				CycleDetection:    true,
				ParallelExecution: true,
			})

		cfg, ok := builder.metadata["got_config"].(got.GoTConfig)
		require.True(t, ok)
		assert.Equal(t, "custom-got", cfg.Name)
		assert.Equal(t, 20, cfg.MaxNodes)
		assert.Equal(t, 5, cfg.MaxEdgesPerNode)
		assert.True(t, cfg.CycleDetection)
		assert.True(t, cfg.ParallelExecution)
	})
}

// TestWithProgramOfThought 测试 Program-of-Thought
func TestWithProgramOfThought(t *testing.T) {
	mockLLM := NewMockLLMClient("Generating executable code...")

	t.Run("default_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithProgramOfThought()

		assert.NotNil(t, builder)
		assert.Equal(t, "pot", builder.metadata["reasoning_pattern"])

		cfg, ok := builder.metadata["pot_config"].(pot.PoTConfig)
		require.True(t, ok)
		assert.Equal(t, "program-of-thought", cfg.Name)
		assert.Equal(t, "python", cfg.Language)
	})

	t.Run("custom_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithProgramOfThought(pot.PoTConfig{
				Name:             "custom-pot",
				Language:         "javascript",
				ExecutionTimeout: 5 * time.Second,
				SafeMode:         true,
			})

		cfg, ok := builder.metadata["pot_config"].(pot.PoTConfig)
		require.True(t, ok)
		assert.Equal(t, "custom-pot", cfg.Name)
		assert.Equal(t, "javascript", cfg.Language)
		assert.Equal(t, 5*time.Second, cfg.ExecutionTimeout)
		assert.True(t, cfg.SafeMode)
	})
}

// TestWithSkeletonOfThought 测试 Skeleton-of-Thought
func TestWithSkeletonOfThought(t *testing.T) {
	mockLLM := NewMockLLMClient("Creating outline skeleton...")

	t.Run("default_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithSkeletonOfThought()

		assert.NotNil(t, builder)
		assert.Equal(t, "sot", builder.metadata["reasoning_pattern"])

		cfg, ok := builder.metadata["sot_config"].(sot.SoTConfig)
		require.True(t, ok)
		assert.Equal(t, "skeleton-of-thought", cfg.Name)
		assert.Equal(t, 10, cfg.MaxSkeletonPoints)
	})

	t.Run("custom_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithSkeletonOfThought(sot.SoTConfig{
				Name:              "custom-sot",
				MaxSkeletonPoints: 7,
				AutoDecompose:     true,
				MaxConcurrency:    3,
			})

		cfg, ok := builder.metadata["sot_config"].(sot.SoTConfig)
		require.True(t, ok)
		assert.Equal(t, "custom-sot", cfg.Name)
		assert.Equal(t, 7, cfg.MaxSkeletonPoints)
		assert.True(t, cfg.AutoDecompose)
		assert.Equal(t, 3, cfg.MaxConcurrency)
	})
}

// TestWithMetaCoT 测试 Meta-CoT / Self-Ask
func TestWithMetaCoT(t *testing.T) {
	mockLLM := NewMockLLMClient("Engaging meta-cognition...")

	t.Run("default_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithMetaCoT()

		assert.NotNil(t, builder)
		assert.Equal(t, "metacot", builder.metadata["reasoning_pattern"])

		cfg, ok := builder.metadata["metacot_config"].(metacot.MetaCoTConfig)
		require.True(t, ok)
		assert.Equal(t, "meta-cot", cfg.Name)
		assert.Equal(t, 3, cfg.MaxDepth)
	})

	t.Run("custom_config", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithMetaCoT(metacot.MetaCoTConfig{
				Name:         "custom-metacot",
				MaxDepth:     5,
				SelfCritique: true,
			})

		cfg, ok := builder.metadata["metacot_config"].(metacot.MetaCoTConfig)
		require.True(t, ok)
		assert.Equal(t, "custom-metacot", cfg.Name)
		assert.Equal(t, 5, cfg.MaxDepth)
		assert.True(t, cfg.SelfCritique)
	})
}

// TestReasoningPresetsIntegration 测试推理预设集成
func TestReasoningPresetsIntegration(t *testing.T) {
	mockLLM := NewMockLLMClient("Integrated reasoning response")

	t.Run("chain_multiple_presets", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithSystemPrompt("You are a reasoning agent").
			WithMaxIterations(15).
			WithTimeout(60 * time.Second).
			WithChainOfThought()

		assert.NotNil(t, builder)
		assert.Equal(t, "cot", builder.metadata["reasoning_pattern"])
		assert.Equal(t, 15, builder.config.MaxIterations)
		assert.Equal(t, 60*time.Second, builder.config.Timeout)
	})

	t.Run("override_reasoning_pattern", func(t *testing.T) {
		builder := NewAgentBuilder[any, core.State](mockLLM).
			WithChainOfThought().
			WithTreeOfThought()

		// 后设置的应该覆盖前面的
		assert.Equal(t, "tot", builder.metadata["reasoning_pattern"])
		_, hasCoT := builder.metadata["cot_config"]
		_, hasToT := builder.metadata["tot_config"]
		assert.True(t, hasCoT)
		assert.True(t, hasToT)
	})
}

// TestReasoningPresetsBuildFlow 测试推理预设构建流程
func TestReasoningPresetsBuildFlow(t *testing.T) {
	mockLLM := NewMockLLMClient("Build flow test")

	t.Run("successful_build_with_reasoning", func(t *testing.T) {
		agent := NewAgentBuilder[any, core.State](mockLLM).
			WithChainOfThought(cot.CoTConfig{
				Name:     "test-cot",
				MaxSteps: 5,
			}).
			BuildReasoningAgent()

		assert.NotNil(t, agent)
	})

	t.Run("build_without_reasoning_pattern_defaults_to_react", func(t *testing.T) {
		// BuildReasoningAgent defaults to ReAct if no pattern specified
		agent := NewAgentBuilder[any, core.State](mockLLM).
			WithSystemPrompt("Test").
			BuildReasoningAgent()

		assert.NotNil(t, agent)
	})
}
