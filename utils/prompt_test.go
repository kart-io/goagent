package utils

import (
	"strings"
	"testing"
)

// TestPromptBuilder_WithSystemPrompt 测试设置系统提示词
func TestPromptBuilder_WithSystemPrompt(t *testing.T) {
	builder := NewPromptBuilder()
	prompt := "You are a helpful assistant"

	result := builder.WithSystemPrompt(prompt).Build()

	if !strings.Contains(result, prompt) {
		t.Errorf("Expected result to contain system prompt, got: %s", result)
	}
}

// TestPromptBuilder_WithContext 测试添加上下文信息
func TestPromptBuilder_WithContext(t *testing.T) {
	builder := NewPromptBuilder()
	ctx := "User is working on a Go project"

	result := builder.WithContext(ctx).Build()

	if !strings.Contains(result, "## Context") {
		t.Errorf("Expected result to contain Context section")
	}
	if !strings.Contains(result, ctx) {
		t.Errorf("Expected result to contain context, got: %s", result)
	}
}

func TestPromptBuilder_WithContexts(t *testing.T) {
	builder := NewPromptBuilder()
	contexts := []string{"Context 1", "Context 2", "Context 3"}

	result := builder.WithContexts(contexts).Build()

	for _, ctx := range contexts {
		if !strings.Contains(result, ctx) {
			t.Errorf("Expected result to contain context: %s", ctx)
		}
	}
}

func TestPromptBuilder_WithExample(t *testing.T) {
	builder := NewPromptBuilder()
	input := "What is 2+2?"
	output := "4"

	result := builder.WithExample(input, output).Build()

	if !strings.Contains(result, "## Examples") {
		t.Errorf("Expected result to contain Examples section")
	}
	if !strings.Contains(result, input) || !strings.Contains(result, output) {
		t.Errorf("Expected result to contain example input and output")
	}
}

func TestPromptBuilder_WithTask(t *testing.T) {
	builder := NewPromptBuilder()
	task := "Analyze the following code"

	result := builder.WithTask(task).Build()

	if !strings.Contains(result, "## Task") {
		t.Errorf("Expected result to contain Task section")
	}
	if !strings.Contains(result, task) {
		t.Errorf("Expected result to contain task, got: %s", result)
	}
}

func TestPromptBuilder_WithConstraint(t *testing.T) {
	builder := NewPromptBuilder()
	constraint := "Keep response under 100 words"

	result := builder.WithConstraint(constraint).Build()

	if !strings.Contains(result, "## Constraints") {
		t.Errorf("Expected result to contain Constraints section")
	}
	if !strings.Contains(result, constraint) {
		t.Errorf("Expected result to contain constraint")
	}
}

func TestPromptBuilder_WithConstraints(t *testing.T) {
	builder := NewPromptBuilder()
	constraints := []string{"Constraint 1", "Constraint 2"}

	result := builder.WithConstraints(constraints).Build()

	for _, c := range constraints {
		if !strings.Contains(result, c) {
			t.Errorf("Expected result to contain constraint: %s", c)
		}
	}
}

func TestPromptBuilder_WithOutputFormat(t *testing.T) {
	builder := NewPromptBuilder()
	format := "Respond in JSON format"

	result := builder.WithOutputFormat(format).Build()

	if !strings.Contains(result, "## Output Format") {
		t.Errorf("Expected result to contain Output Format section")
	}
	if !strings.Contains(result, format) {
		t.Errorf("Expected result to contain format")
	}
}

func TestPromptBuilder_FullBuild(t *testing.T) {
	builder := NewPromptBuilder()

	result := builder.
		WithSystemPrompt("You are an expert").
		WithContext("Working on Go").
		WithExample("Input", "Output").
		WithConstraint("Be concise").
		WithOutputFormat("JSON").
		WithTask("Analyze code").
		Build()

	sections := []string{
		"You are an expert",
		"## Context",
		"## Examples",
		"## Constraints",
		"## Output Format",
		"## Task",
	}

	for _, section := range sections {
		if !strings.Contains(result, section) {
			t.Errorf("Expected result to contain: %s", section)
		}
	}
}

func TestPromptBuilder_BuildWithTemplate(t *testing.T) {
	builder := NewPromptBuilder()
	template := "Hello {{name}}, your role is {{role}}"
	vars := map[string]string{
		"name": "John",
		"role": "developer",
	}

	result := builder.BuildWithTemplate(template, vars)

	if !strings.Contains(result, "Hello John") {
		t.Errorf("Expected result to contain replaced name")
	}
	if !strings.Contains(result, "developer") {
		t.Errorf("Expected result to contain replaced role")
	}
	if strings.Contains(result, "{{") {
		t.Errorf("Expected all placeholders to be replaced")
	}
}

func TestPromptBuilder_Reset(t *testing.T) {
	builder := NewPromptBuilder()

	builder.
		WithSystemPrompt("System").
		WithContext("Context").
		WithTask("Task")

	builder.Reset()
	result := builder.Build()

	if strings.Contains(result, "System") || strings.Contains(result, "Context") || strings.Contains(result, "Task") {
		t.Errorf("Expected builder to be reset, got: %s", result)
	}
}

func TestPromptBuilder_EmptyContext(t *testing.T) {
	builder := NewPromptBuilder()
	result := builder.WithContext("").Build()

	if strings.Contains(result, "## Context") {
		t.Errorf("Expected no Context section for empty context")
	}
}

func TestPromptBuilder_EmptyConstraint(t *testing.T) {
	builder := NewPromptBuilder()
	result := builder.WithConstraint("").Build()

	if strings.Contains(result, "## Constraints") {
		t.Errorf("Expected no Constraints section for empty constraint")
	}
}

func TestCommonPrompts_RootCauseAnalysis(t *testing.T) {
	template := CommonPrompts.RootCauseAnalysis

	if !strings.Contains(template, "root cause") {
		t.Errorf("Expected root cause analysis template to mention root cause")
	}
	if !strings.Contains(template, "{{context}}") {
		t.Errorf("Expected template to have context placeholder")
	}
}

func TestCommonPrompts_ProblemSummary(t *testing.T) {
	template := CommonPrompts.ProblemSummary

	if !strings.Contains(template, "{{problem}}") {
		t.Errorf("Expected template to have problem placeholder")
	}
}

func TestCommonPrompts_RecommendationGeneration(t *testing.T) {
	template := CommonPrompts.RecommendationGeneration

	if !strings.Contains(template, "recommendations") {
		t.Errorf("Expected recommendation template to mention recommendations")
	}
	if !strings.Contains(template, "{{root_cause}}") {
		t.Errorf("Expected template to have root_cause placeholder")
	}
}
