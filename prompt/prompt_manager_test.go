package prompt

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPromptManager(t *testing.T) {
	manager := NewPromptManager()
	require.NotNil(t, manager)
	assert.NotNil(t, manager.prompts)
	assert.NotNil(t, manager.chains)
	assert.NotNil(t, manager.templates)
	assert.NotNil(t, manager.optimizer)
	assert.NotNil(t, manager.evaluator)
}

func TestDefaultPromptManager_CreatePrompt(t *testing.T) {
	manager := NewPromptManager()

	tests := []struct {
		name    string
		prompt  *Prompt
		wantErr bool
		errMsg  string
	}{
		{
			name: "create valid prompt",
			prompt: &Prompt{
				ID:       "prompt-1",
				Name:     "Test Prompt",
				Type:     PromptTypeUser,
				Strategy: StrategyZeroShot,
				Template: "Hello {{.name}}",
			},
			wantErr: false,
		},
		{
			name: "create prompt with variables",
			prompt: &Prompt{
				ID:       "prompt-2",
				Name:     "Var Prompt",
				Type:     PromptTypeSystem,
				Strategy: StrategyFewShot,
				Template: "User: {{.user}}, Task: {{.task}}",
				Variables: map[string]interface{}{
					"user": "default",
					"task": "analysis",
				},
			},
			wantErr: false,
		},
		{
			name: "create prompt with examples",
			prompt: &Prompt{
				ID:       "prompt-3",
				Name:     "Example Prompt",
				Type:     PromptTypeInstruction,
				Strategy: StrategyFewShot,
				Template: "Classify: {{.input}}",
				Examples: []Example{
					{Input: "good", Output: "positive"},
					{Input: "bad", Output: "negative"},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate prompt ID",
			prompt: &Prompt{
				ID:       "prompt-1",
				Name:     "Duplicate",
				Type:     PromptTypeUser,
				Strategy: StrategyZeroShot,
				Template: "Test",
			},
			wantErr: true,
			errMsg:  "prompt already exists",
		},
		{
			name: "invalid template syntax",
			prompt: &Prompt{
				ID:       "prompt-invalid",
				Name:     "Invalid Template",
				Type:     PromptTypeUser,
				Strategy: StrategyZeroShot,
				Template: "Hello {{.name",
			},
			wantErr: true,
			errMsg:  "invalid template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.CreatePrompt(tt.prompt)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.False(t, tt.prompt.CreatedAt.IsZero())
				assert.False(t, tt.prompt.UpdatedAt.IsZero())
			}
		})
	}
}

func TestDefaultPromptManager_GetPrompt(t *testing.T) {
	manager := NewPromptManager()

	prompt := &Prompt{
		ID:       "test-prompt",
		Name:     "Test",
		Type:     PromptTypeUser,
		Strategy: StrategyZeroShot,
		Template: "Hello",
	}
	err := manager.CreatePrompt(prompt)
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "get existing prompt",
			id:      "test-prompt",
			wantErr: false,
		},
		{
			name:    "get non-existent prompt",
			id:      "non-existent",
			wantErr: true,
			errMsg:  "prompt not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := manager.GetPrompt(tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.id, result.ID)
			}
		})
	}
}

func TestDefaultPromptManager_UpdatePrompt(t *testing.T) {
	manager := NewPromptManager()

	prompt := &Prompt{
		ID:       "update-test",
		Name:     "Original",
		Type:     PromptTypeUser,
		Strategy: StrategyZeroShot,
		Template: "Original {{.text}}",
	}
	err := manager.CreatePrompt(prompt)
	require.NoError(t, err)

	createdAt := prompt.CreatedAt
	time.Sleep(10 * time.Millisecond)

	tests := []struct {
		name    string
		prompt  *Prompt
		wantErr bool
		errMsg  string
	}{
		{
			name: "update existing prompt",
			prompt: &Prompt{
				ID:       "update-test",
				Name:     "Updated",
				Type:     PromptTypeSystem,
				Strategy: StrategyChainOfThought,
				Template: "Updated {{.text}}",
			},
			wantErr: false,
		},
		{
			name: "update non-existent prompt",
			prompt: &Prompt{
				ID:       "non-existent",
				Name:     "Test",
				Type:     PromptTypeUser,
				Strategy: StrategyZeroShot,
				Template: "Test",
			},
			wantErr: true,
			errMsg:  "prompt not found",
		},
		{
			name: "update with invalid template",
			prompt: &Prompt{
				ID:       "update-test",
				Name:     "Invalid",
				Type:     PromptTypeUser,
				Strategy: StrategyZeroShot,
				Template: "Invalid {{.text",
			},
			wantErr: true,
			errMsg:  "invalid template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.UpdatePrompt(tt.prompt)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.True(t, tt.prompt.UpdatedAt.After(createdAt))

				retrieved, err := manager.GetPrompt(tt.prompt.ID)
				require.NoError(t, err)
				assert.Equal(t, tt.prompt.Name, retrieved.Name)
			}
		})
	}
}

func TestDefaultPromptManager_DeletePrompt(t *testing.T) {
	manager := NewPromptManager()

	prompt := &Prompt{
		ID:       "delete-test",
		Name:     "To Delete",
		Type:     PromptTypeUser,
		Strategy: StrategyZeroShot,
		Template: "Delete me",
	}
	err := manager.CreatePrompt(prompt)
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "delete existing prompt",
			id:      "delete-test",
			wantErr: false,
		},
		{
			name:    "delete non-existent prompt",
			id:      "non-existent",
			wantErr: true,
			errMsg:  "prompt not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.DeletePrompt(tt.id)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)

				_, err := manager.GetPrompt(tt.id)
				assert.Error(t, err)
			}
		})
	}
}

func TestDefaultPromptManager_ListPrompts(t *testing.T) {
	manager := NewPromptManager()

	now := time.Now()

	prompts := []*Prompt{
		{
			ID:        "prompt-1",
			Name:      "User Prompt",
			Type:      PromptTypeUser,
			Strategy:  StrategyZeroShot,
			Template:  "Test 1",
			Tags:      []string{"tag1", "tag2"},
			UpdatedAt: now,
		},
		{
			ID:        "prompt-2",
			Name:      "System Prompt",
			Type:      PromptTypeSystem,
			Strategy:  StrategyFewShot,
			Template:  "Test 2",
			Tags:      []string{"tag2", "tag3"},
			UpdatedAt: now.Add(-1 * time.Hour),
		},
		{
			ID:        "prompt-3",
			Name:      "Chain Prompt",
			Type:      PromptTypeInstruction,
			Strategy:  StrategyChainOfThought,
			Template:  "Test 3",
			Tags:      []string{"tag1"},
			UpdatedAt: now.Add(-2 * time.Hour),
		},
	}

	for _, p := range prompts {
		err := manager.CreatePrompt(p)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		filter        PromptFilter
		expectedCount int
		checkIDs      []string
	}{
		{
			name:          "list all prompts",
			filter:        PromptFilter{},
			expectedCount: 3,
		},
		{
			name: "filter by type",
			filter: PromptFilter{
				Type: PromptTypeUser,
			},
			expectedCount: 1,
			checkIDs:      []string{"prompt-1"},
		},
		{
			name: "filter by strategy",
			filter: PromptFilter{
				Strategy: StrategyFewShot,
			},
			expectedCount: 1,
			checkIDs:      []string{"prompt-2"},
		},
		{
			name: "filter by tags",
			filter: PromptFilter{
				Tags: []string{"tag1"},
			},
			expectedCount: 2,
		},
		{
			name: "filter by since",
			filter: PromptFilter{
				Since: now.Add(-90 * time.Minute),
			},
			expectedCount: 3, // All prompts created recently
		},
		{
			name: "multiple filters",
			filter: PromptFilter{
				Type: PromptTypeSystem,
				Tags: []string{"tag2"},
			},
			expectedCount: 1,
			checkIDs:      []string{"prompt-2"},
		},
		{
			name: "no matches",
			filter: PromptFilter{
				Type: PromptTypeAssistant,
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := manager.ListPrompts(tt.filter)
			require.NoError(t, err)
			assert.Len(t, results, tt.expectedCount)

			if len(tt.checkIDs) > 0 {
				ids := make([]string, len(results))
				for i, r := range results {
					ids[i] = r.ID
				}
				for _, id := range tt.checkIDs {
					assert.Contains(t, ids, id)
				}
			}
		})
	}
}

func TestDefaultPromptManager_ExecutePrompt(t *testing.T) {
	manager := NewPromptManager()
	ctx := context.Background()

	tests := []struct {
		name        string
		prompt      *Prompt
		variables   map[string]interface{}
		wantErr     bool
		errMsg      string
		checkOutput func(t *testing.T, output string)
	}{
		{
			name: "execute simple template",
			prompt: &Prompt{
				ID:       "simple",
				Name:     "Simple",
				Type:     PromptTypeUser,
				Strategy: StrategyZeroShot,
				Template: "Hello {{.name}}!",
			},
			variables: map[string]interface{}{"name": "World"},
			wantErr:   false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Hello World!")
			},
		},
		{
			name: "execute with system prompt",
			prompt: &Prompt{
				ID:           "with-system",
				Name:         "With System",
				Type:         PromptTypeUser,
				Strategy:     StrategyZeroShot,
				Template:     "Query: {{.query}}",
				SystemPrompt: "You are a helpful assistant",
			},
			variables: map[string]interface{}{"query": "help"},
			wantErr:   false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "System: You are a helpful assistant")
				assert.Contains(t, output, "Query: help")
			},
		},
		{
			name: "execute with context",
			prompt: &Prompt{
				ID:       "with-context",
				Name:     "With Context",
				Type:     PromptTypeUser,
				Strategy: StrategyZeroShot,
				Template: "Question: {{.question}}",
				Context:  "This is background information",
			},
			variables: map[string]interface{}{"question": "What?"},
			wantErr:   false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Context: This is background information")
				assert.Contains(t, output, "Question: What?")
			},
		},
		{
			name: "execute with constraints",
			prompt: &Prompt{
				ID:          "with-constraints",
				Name:        "With Constraints",
				Type:        PromptTypeUser,
				Strategy:    StrategyZeroShot,
				Template:    "Task: {{.task}}",
				Constraints: []string{"Be concise", "Use examples"},
			},
			variables: map[string]interface{}{"task": "explain"},
			wantErr:   false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Constraints:")
				assert.Contains(t, output, "- Be concise")
				assert.Contains(t, output, "- Use examples")
			},
		},
		{
			name: "execute few-shot with examples",
			prompt: &Prompt{
				ID:       "few-shot",
				Name:     "Few Shot",
				Type:     PromptTypeUser,
				Strategy: StrategyFewShot,
				Template: "Classify: {{.input}}",
				Examples: []Example{
					{Input: "great", Output: "positive", Reasoning: "positive word"},
					{Input: "terrible", Output: "negative"},
				},
			},
			variables: map[string]interface{}{"input": "good"},
			wantErr:   false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Examples:")
				assert.Contains(t, output, "Example 1:")
				assert.Contains(t, output, "Input: great")
				assert.Contains(t, output, "Output: positive")
				assert.Contains(t, output, "Reasoning: positive word")
			},
		},
		{
			name: "execute chain of thought",
			prompt: &Prompt{
				ID:       "cot",
				Name:     "CoT",
				Type:     PromptTypeUser,
				Strategy: StrategyChainOfThought,
				Template: "Solve: {{.problem}}",
			},
			variables: map[string]interface{}{"problem": "2+2"},
			wantErr:   false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Let's think step by step:")
			},
		},
		{
			name: "execute with default variables",
			prompt: &Prompt{
				ID:       "default-vars",
				Name:     "Default Vars",
				Type:     PromptTypeUser,
				Strategy: StrategyZeroShot,
				Template: "User: {{.user}}, Action: {{.action}}",
				Variables: map[string]interface{}{
					"user":   "default_user",
					"action": "default_action",
				},
			},
			variables: map[string]interface{}{"action": "custom_action"},
			wantErr:   false,
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "User: default_user")
				assert.Contains(t, output, "Action: custom_action")
			},
		},
		{
			name:      "execute non-existent prompt",
			variables: map[string]interface{}{},
			wantErr:   true,
			errMsg:    "prompt not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var promptID string
			if tt.prompt != nil {
				err := manager.CreatePrompt(tt.prompt)
				require.NoError(t, err)
				promptID = tt.prompt.ID
			} else {
				promptID = "non-existent"
			}

			output, err := manager.ExecutePrompt(ctx, promptID, tt.variables)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				if tt.checkOutput != nil {
					tt.checkOutput(t, output)
				}
			}
		})
	}
}

func TestHasAnyTag(t *testing.T) {
	tests := []struct {
		name       string
		promptTags []string
		filterTags []string
		expected   bool
	}{
		{
			name:       "has matching tag",
			promptTags: []string{"tag1", "tag2", "tag3"},
			filterTags: []string{"tag2"},
			expected:   true,
		},
		{
			name:       "has multiple matching tags",
			promptTags: []string{"tag1", "tag2", "tag3"},
			filterTags: []string{"tag2", "tag3"},
			expected:   true,
		},
		{
			name:       "no matching tags",
			promptTags: []string{"tag1", "tag2"},
			filterTags: []string{"tag3", "tag4"},
			expected:   false,
		},
		{
			name:       "empty prompt tags",
			promptTags: []string{},
			filterTags: []string{"tag1"},
			expected:   false,
		},
		{
			name:       "empty filter tags",
			promptTags: []string{"tag1"},
			filterTags: []string{},
			expected:   false,
		},
		{
			name:       "both empty",
			promptTags: []string{},
			filterTags: []string{},
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAnyTag(tt.promptTags, tt.filterTags)
			assert.Equal(t, tt.expected, result)
		})
	}
}
