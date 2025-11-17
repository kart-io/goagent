// Package prompt provides prompt engineering and management capabilities
package prompt

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// PromptType defines the type of prompt
type PromptType string

const (
	PromptTypeSystem         PromptType = "system"
	PromptTypeUser           PromptType = "user"
	PromptTypeAssistant      PromptType = "assistant"
	PromptTypeInstruction    PromptType = "instruction"
	PromptTypeContext        PromptType = "context"
	PromptTypeExample        PromptType = "example"
	PromptTypeConstraint     PromptType = "constraint"
	PromptTypeChainOfThought PromptType = "chain_of_thought"
)

// PromptStrategy defines the prompting strategy
type PromptStrategy string

const (
	StrategyZeroShot       PromptStrategy = "zero_shot"
	StrategyFewShot        PromptStrategy = "few_shot"
	StrategyChainOfThought PromptStrategy = "chain_of_thought"
	StrategyTreeOfThoughts PromptStrategy = "tree_of_thoughts"
	StrategyReAct          PromptStrategy = "react"
	StrategyRolePlay       PromptStrategy = "role_play"
	StrategyInstructional  PromptStrategy = "instructional"
	StrategyConversational PromptStrategy = "conversational"
)

// Prompt represents a structured prompt
type Prompt struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Type         PromptType             `json:"type"`
	Strategy     PromptStrategy         `json:"strategy"`
	Template     string                 `json:"template"`
	Variables    map[string]interface{} `json:"variables,omitempty"`
	Examples     []Example              `json:"examples,omitempty"`
	Constraints  []string               `json:"constraints,omitempty"`
	Context      string                 `json:"context,omitempty"`
	SystemPrompt string                 `json:"system_prompt,omitempty"`
	Version      string                 `json:"version"`
	Tags         []string               `json:"tags,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// Example represents an example for few-shot learning
type Example struct {
	Input     string                 `json:"input"`
	Output    string                 `json:"output"`
	Reasoning string                 `json:"reasoning,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// PromptChain represents a chain of prompts
type PromptChain struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Steps       []PromptStep  `json:"steps"`
	Strategy    ChainStrategy `json:"strategy"`
	CreatedAt   time.Time     `json:"created_at"`
}

// PromptStep represents a step in a prompt chain
type PromptStep struct {
	ID           string                 `json:"id"`
	PromptID     string                 `json:"prompt_id"`
	Order        int                    `json:"order"`
	Condition    string                 `json:"condition,omitempty"`
	InputMapping map[string]string      `json:"input_mapping,omitempty"`
	OutputKey    string                 `json:"output_key"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ChainStrategy defines how prompts are chained
type ChainStrategy string

const (
	ChainStrategySequential  ChainStrategy = "sequential"
	ChainStrategyConditional ChainStrategy = "conditional"
	ChainStrategyIterative   ChainStrategy = "iterative"
	ChainStrategyParallel    ChainStrategy = "parallel"
)

// PromptManager manages prompts and their execution
type PromptManager interface {
	// CreatePrompt creates a new prompt
	CreatePrompt(prompt *Prompt) error

	// GetPrompt retrieves a prompt by ID
	GetPrompt(id string) (*Prompt, error)

	// UpdatePrompt updates an existing prompt
	UpdatePrompt(prompt *Prompt) error

	// DeletePrompt deletes a prompt
	DeletePrompt(id string) error

	// ListPrompts lists all prompts
	ListPrompts(filter PromptFilter) ([]*Prompt, error)

	// ExecutePrompt executes a prompt with variables
	ExecutePrompt(ctx context.Context, promptID string, variables map[string]interface{}) (string, error)

	// CreateChain creates a prompt chain
	CreateChain(chain *PromptChain) error

	// ExecuteChain executes a prompt chain
	ExecuteChain(ctx context.Context, chainID string, input map[string]interface{}) (map[string]interface{}, error)

	// OptimizePrompt optimizes a prompt based on feedback
	OptimizePrompt(promptID string, feedback []Feedback) (*Prompt, error)

	// TestPrompt tests a prompt with test cases
	TestPrompt(promptID string, testCases []TestCase) (*TestResult, error)
}

// PromptFilter filters prompts
type PromptFilter struct {
	Type     PromptType
	Strategy PromptStrategy
	Tags     []string
	Since    time.Time
}

// Feedback represents feedback on prompt performance
type Feedback struct {
	PromptID  string    `json:"prompt_id"`
	Input     string    `json:"input"`
	Output    string    `json:"output"`
	Expected  string    `json:"expected,omitempty"`
	Score     float64   `json:"score"`
	Comments  string    `json:"comments,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TestCase represents a test case for prompt validation
type TestCase struct {
	ID          string                 `json:"id"`
	Input       map[string]interface{} `json:"input"`
	Expected    string                 `json:"expected"`
	Description string                 `json:"description,omitempty"`
}

// TestResult represents the result of prompt testing
type TestResult struct {
	PromptID    string        `json:"prompt_id"`
	TotalCases  int           `json:"total_cases"`
	PassedCases int           `json:"passed_cases"`
	FailedCases int           `json:"failed_cases"`
	SuccessRate float64       `json:"success_rate"`
	Details     []TestDetail  `json:"details"`
	Duration    time.Duration `json:"duration"`
}

// TestDetail contains details of a test case execution
type TestDetail struct {
	TestCaseID string  `json:"test_case_id"`
	Passed     bool    `json:"passed"`
	Actual     string  `json:"actual"`
	Expected   string  `json:"expected"`
	Score      float64 `json:"score,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// DefaultPromptManager is the default implementation of PromptManager
type DefaultPromptManager struct {
	prompts   map[string]*Prompt
	chains    map[string]*PromptChain
	templates map[string]*template.Template
	optimizer *PromptOptimizer
	evaluator *PromptEvaluator
	mu        sync.RWMutex
}

// NewPromptManager creates a new prompt manager
func NewPromptManager() *DefaultPromptManager {
	return &DefaultPromptManager{
		prompts:   make(map[string]*Prompt),
		chains:    make(map[string]*PromptChain),
		templates: make(map[string]*template.Template),
		optimizer: NewPromptOptimizer(),
		evaluator: NewPromptEvaluator(),
	}
}

// CreatePrompt creates a new prompt
func (m *DefaultPromptManager) CreatePrompt(prompt *Prompt) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.prompts[prompt.ID]; exists {
		return agentErrors.New(agentErrors.CodeInvalidConfig, "prompt already exists").
			WithComponent("prompt_manager").
			WithOperation("CreatePrompt").
			WithContext("prompt_id", prompt.ID)
	}

	// Parse and validate template
	tmpl, err := template.New(prompt.ID).Parse(prompt.Template)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid template").
			WithComponent("prompt_manager").
			WithOperation("CreatePrompt").
			WithContext("prompt_id", prompt.ID)
	}

	prompt.CreatedAt = time.Now()
	prompt.UpdatedAt = time.Now()

	m.prompts[prompt.ID] = prompt
	m.templates[prompt.ID] = tmpl

	return nil
}

// GetPrompt retrieves a prompt by ID
func (m *DefaultPromptManager) GetPrompt(id string) (*Prompt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prompt, exists := m.prompts[id]
	if !exists {
		return nil, agentErrors.New(agentErrors.CodeAgentNotFound, "prompt not found").
			WithComponent("prompt_manager").
			WithOperation("GetPrompt").
			WithContext("prompt_id", id)
	}

	return prompt, nil
}

// UpdatePrompt updates an existing prompt
func (m *DefaultPromptManager) UpdatePrompt(prompt *Prompt) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.prompts[prompt.ID]; !exists {
		return agentErrors.New(agentErrors.CodeAgentNotFound, "prompt not found").
			WithComponent("prompt_manager").
			WithOperation("UpdatePrompt").
			WithContext("prompt_id", prompt.ID)
	}

	// Re-parse template
	tmpl, err := template.New(prompt.ID).Parse(prompt.Template)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid template").
			WithComponent("prompt_manager").
			WithOperation("UpdatePrompt").
			WithContext("prompt_id", prompt.ID)
	}

	prompt.UpdatedAt = time.Now()
	m.prompts[prompt.ID] = prompt
	m.templates[prompt.ID] = tmpl

	return nil
}

// DeletePrompt deletes a prompt
func (m *DefaultPromptManager) DeletePrompt(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.prompts[id]; !exists {
		return agentErrors.New(agentErrors.CodeAgentNotFound, "prompt not found").
			WithComponent("prompt_manager").
			WithOperation("DeletePrompt").
			WithContext("prompt_id", id)
	}

	delete(m.prompts, id)
	delete(m.templates, id)

	return nil
}

// ListPrompts lists prompts based on filter
func (m *DefaultPromptManager) ListPrompts(filter PromptFilter) ([]*Prompt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]*Prompt, 0, len(m.prompts))

	for _, prompt := range m.prompts {
		// Apply filters
		if filter.Type != "" && prompt.Type != filter.Type {
			continue
		}

		if filter.Strategy != "" && prompt.Strategy != filter.Strategy {
			continue
		}

		if !filter.Since.IsZero() && prompt.UpdatedAt.Before(filter.Since) {
			continue
		}

		if len(filter.Tags) > 0 && !hasAnyTag(prompt.Tags, filter.Tags) {
			continue
		}

		results = append(results, prompt)
	}

	return results, nil
}

// ExecutePrompt executes a prompt with variables
func (m *DefaultPromptManager) ExecutePrompt(ctx context.Context, promptID string, variables map[string]interface{}) (string, error) {
	m.mu.RLock()
	prompt, exists := m.prompts[promptID]
	tmpl, hasTemplate := m.templates[promptID]
	m.mu.RUnlock()

	if !exists {
		return "", agentErrors.New(agentErrors.CodeAgentNotFound, "prompt not found").
			WithComponent("prompt_manager").
			WithOperation("ExecutePrompt").
			WithContext("prompt_id", promptID)
	}

	if !hasTemplate {
		return "", agentErrors.New(agentErrors.CodeAgentNotFound, "template not found for prompt").
			WithComponent("prompt_manager").
			WithOperation("ExecutePrompt").
			WithContext("prompt_id", promptID)
	}

	// Merge default variables with provided ones
	finalVars := make(map[string]interface{})
	for k, v := range prompt.Variables {
		finalVars[k] = v
	}
	for k, v := range variables {
		finalVars[k] = v
	}

	// Build the complete prompt
	var result strings.Builder

	// Add system prompt if present
	if prompt.SystemPrompt != "" {
		result.WriteString(fmt.Sprintf("System: %s\n\n", prompt.SystemPrompt))
	}

	// Add context if present
	if prompt.Context != "" {
		result.WriteString(fmt.Sprintf("Context: %s\n\n", prompt.Context))
	}

	// Add constraints
	if len(prompt.Constraints) > 0 {
		result.WriteString("Constraints:\n")
		for _, constraint := range prompt.Constraints {
			result.WriteString(fmt.Sprintf("- %s\n", constraint))
		}
		result.WriteString("\n")
	}

	// Add examples for few-shot
	if prompt.Strategy == StrategyFewShot && len(prompt.Examples) > 0 {
		result.WriteString("Examples:\n")
		for i, example := range prompt.Examples {
			result.WriteString(fmt.Sprintf("Example %d:\n", i+1))
			result.WriteString(fmt.Sprintf("Input: %s\n", example.Input))
			result.WriteString(fmt.Sprintf("Output: %s\n", example.Output))
			if example.Reasoning != "" {
				result.WriteString(fmt.Sprintf("Reasoning: %s\n", example.Reasoning))
			}
			result.WriteString("\n")
		}
	}

	// Execute template with variables
	var templateOutput strings.Builder
	if err := tmpl.Execute(&templateOutput, finalVars); err != nil {
		return "", agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "failed to execute template").
			WithComponent("prompt_manager").
			WithOperation("ExecutePrompt").
			WithContext("prompt_id", promptID)
	}

	result.WriteString(templateOutput.String())

	// Add chain of thought prompting if specified
	if prompt.Strategy == StrategyChainOfThought {
		result.WriteString("\n\nLet's think step by step:\n")
	}

	return result.String(), nil
}

// CreateChain creates a prompt chain
func (m *DefaultPromptManager) CreateChain(chain *PromptChain) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.chains[chain.ID]; exists {
		return agentErrors.New(agentErrors.CodeInvalidConfig, "chain already exists").
			WithComponent("prompt_manager").
			WithOperation("CreateChain").
			WithContext("chain_id", chain.ID)
	}

	// Validate that all referenced prompts exist
	for _, step := range chain.Steps {
		if _, exists := m.prompts[step.PromptID]; !exists {
			return agentErrors.New(agentErrors.CodeAgentNotFound, "prompt not found in step").
				WithComponent("prompt_manager").
				WithOperation("CreateChain").
				WithContext("chain_id", chain.ID).
				WithContext("step_id", step.ID).
				WithContext("prompt_id", step.PromptID)
		}
	}

	chain.CreatedAt = time.Now()
	m.chains[chain.ID] = chain

	return nil
}

// ExecuteChain executes a prompt chain
func (m *DefaultPromptManager) ExecuteChain(ctx context.Context, chainID string, input map[string]interface{}) (map[string]interface{}, error) {
	m.mu.RLock()
	chain, exists := m.chains[chainID]
	m.mu.RUnlock()

	if !exists {
		return nil, agentErrors.New(agentErrors.CodeAgentNotFound, "chain not found").
			WithComponent("prompt_manager").
			WithOperation("ExecuteChain").
			WithContext("chain_id", chainID)
	}

	results := make(map[string]interface{})
	currentInput := input

	for _, step := range chain.Steps {
		// Map inputs
		stepInput := make(map[string]interface{})
		if step.InputMapping != nil {
			for key, source := range step.InputMapping {
				if val, ok := currentInput[source]; ok {
					stepInput[key] = val
				} else if val, ok := results[source]; ok {
					stepInput[key] = val
				}
			}
		} else {
			stepInput = currentInput
		}

		// Execute prompt
		output, err := m.ExecutePrompt(ctx, step.PromptID, stepInput)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInternal, "failed to execute step").
				WithComponent("prompt_manager").
				WithOperation("ExecuteChain").
				WithContext("chain_id", chainID).
				WithContext("step_id", step.ID).
				WithContext("prompt_id", step.PromptID)
		}

		// Store result
		results[step.OutputKey] = output

		// Update current input for next step
		currentInput[step.OutputKey] = output
	}

	return results, nil
}

// OptimizePrompt optimizes a prompt based on feedback
func (m *DefaultPromptManager) OptimizePrompt(promptID string, feedback []Feedback) (*Prompt, error) {
	m.mu.RLock()
	prompt, exists := m.prompts[promptID]
	m.mu.RUnlock()

	if !exists {
		return nil, agentErrors.New(agentErrors.CodeAgentNotFound, "prompt not found").
			WithComponent("prompt_manager").
			WithOperation("OptimizePrompt").
			WithContext("prompt_id", promptID)
	}

	// Use optimizer to improve prompt
	optimizedPrompt := m.optimizer.Optimize(prompt, feedback)

	// Update the prompt
	if err := m.UpdatePrompt(optimizedPrompt); err != nil {
		return nil, err
	}

	return optimizedPrompt, nil
}

// TestPrompt tests a prompt with test cases
func (m *DefaultPromptManager) TestPrompt(promptID string, testCases []TestCase) (*TestResult, error) {
	m.mu.RLock()
	_, exists := m.prompts[promptID]
	m.mu.RUnlock()

	if !exists {
		return nil, agentErrors.New(agentErrors.CodeAgentNotFound, "prompt not found").
			WithComponent("prompt_manager").
			WithOperation("TestPrompt").
			WithContext("prompt_id", promptID)
	}

	startTime := time.Now()
	result := &TestResult{
		PromptID:   promptID,
		TotalCases: len(testCases),
		Details:    make([]TestDetail, 0, len(testCases)),
	}

	for _, testCase := range testCases {
		// Execute prompt with test input
		output, err := m.ExecutePrompt(context.Background(), promptID, testCase.Input)

		detail := TestDetail{
			TestCaseID: testCase.ID,
			Expected:   testCase.Expected,
			Actual:     output,
		}

		if err != nil {
			detail.Error = err.Error()
			detail.Passed = false
			result.FailedCases++
		} else {
			// Evaluate output
			score := m.evaluator.Evaluate(output, testCase.Expected)
			detail.Score = score
			detail.Passed = score >= 0.8 // 80% threshold

			if detail.Passed {
				result.PassedCases++
			} else {
				result.FailedCases++
			}
		}

		result.Details = append(result.Details, detail)
	}

	result.Duration = time.Since(startTime)
	result.SuccessRate = float64(result.PassedCases) / float64(result.TotalCases)

	return result, nil
}

// Helper functions

func hasAnyTag(promptTags, filterTags []string) bool {
	for _, filterTag := range filterTags {
		for _, promptTag := range promptTags {
			if promptTag == filterTag {
				return true
			}
		}
	}
	return false
}
