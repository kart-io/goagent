package utils

import (
	"fmt"
	"strings"
)

// PromptBuilder 提供 Prompt 构建工具
type PromptBuilder struct {
	systemPrompt string
	context      []string
	examples     []Example
	task         string
	constraints  []string
	format       string
}

// Example 示例定义
type Example struct {
	Input  string
	Output string
}

// NewPromptBuilder 创建 Prompt 构建器
func NewPromptBuilder() *PromptBuilder {
	return &PromptBuilder{
		context:     make([]string, 0),
		examples:    make([]Example, 0),
		constraints: make([]string, 0),
	}
}

// WithSystemPrompt 设置系统提示
func (b *PromptBuilder) WithSystemPrompt(prompt string) *PromptBuilder {
	b.systemPrompt = prompt
	return b
}

// WithContext 添加上下文信息
func (b *PromptBuilder) WithContext(ctx string) *PromptBuilder {
	if ctx != "" {
		b.context = append(b.context, ctx)
	}
	return b
}

// WithContexts 添加多个上下文信息
func (b *PromptBuilder) WithContexts(contexts []string) *PromptBuilder {
	for _, ctx := range contexts {
		b.WithContext(ctx)
	}
	return b
}

// WithExample 添加示例
func (b *PromptBuilder) WithExample(input, output string) *PromptBuilder {
	b.examples = append(b.examples, Example{
		Input:  input,
		Output: output,
	})
	return b
}

// WithTask 设置任务描述
func (b *PromptBuilder) WithTask(task string) *PromptBuilder {
	b.task = task
	return b
}

// WithConstraint 添加约束条件
func (b *PromptBuilder) WithConstraint(constraint string) *PromptBuilder {
	if constraint != "" {
		b.constraints = append(b.constraints, constraint)
	}
	return b
}

// WithConstraints 添加多个约束条件
func (b *PromptBuilder) WithConstraints(constraints []string) *PromptBuilder {
	for _, c := range constraints {
		b.WithConstraint(c)
	}
	return b
}

// WithOutputFormat 设置输出格式要求
func (b *PromptBuilder) WithOutputFormat(format string) *PromptBuilder {
	b.format = format
	return b
}

// Build 构建最终的 Prompt
func (b *PromptBuilder) Build() string {
	var parts []string

	// 系统提示
	if b.systemPrompt != "" {
		parts = append(parts, b.systemPrompt)
	}

	// 上下文信息
	if len(b.context) > 0 {
		parts = append(parts, "\n## Context")
		for i, ctx := range b.context {
			parts = append(parts, fmt.Sprintf("%d. %s", i+1, ctx))
		}
	}

	// 示例
	if len(b.examples) > 0 {
		parts = append(parts, "\n## Examples")
		for i, ex := range b.examples {
			parts = append(parts, fmt.Sprintf("\nExample %d:", i+1))
			parts = append(parts, fmt.Sprintf("Input: %s", ex.Input))
			parts = append(parts, fmt.Sprintf("Output: %s", ex.Output))
		}
	}

	// 约束条件
	if len(b.constraints) > 0 {
		parts = append(parts, "\n## Constraints")
		for i, c := range b.constraints {
			parts = append(parts, fmt.Sprintf("%d. %s", i+1, c))
		}
	}

	// 输出格式
	if b.format != "" {
		parts = append(parts, "\n## Output Format")
		parts = append(parts, b.format)
	}

	// 任务
	if b.task != "" {
		parts = append(parts, "\n## Task")
		parts = append(parts, b.task)
	}

	return strings.Join(parts, "\n")
}

// BuildWithTemplate 使用模板构建 Prompt
func (b *PromptBuilder) BuildWithTemplate(template string, vars map[string]string) string {
	result := template
	for key, value := range vars {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// Reset 重置构建器
func (b *PromptBuilder) Reset() *PromptBuilder {
	b.systemPrompt = ""
	b.context = make([]string, 0)
	b.examples = make([]Example, 0)
	b.task = ""
	b.constraints = make([]string, 0)
	b.format = ""
	return b
}

// CommonPrompts 提供常用的 Prompt 模板
var CommonPrompts = struct {
	// RootCauseAnalysis 根因分析模板
	RootCauseAnalysis string

	// ProblemSummary 问题摘要模板
	ProblemSummary string

	// RecommendationGeneration 建议生成模板
	RecommendationGeneration string
}{
	RootCauseAnalysis: `You are an expert system administrator analyzing system failures.

## Context
{{context}}

## Task
Analyze the above information and identify the root cause of the failure.

## Output Format
Provide your analysis in the following JSON format:
{
  "root_cause": "Brief description of the root cause",
  "confidence": 0.0-1.0,
  "reasoning": "Detailed explanation of your analysis",
  "contributing_factors": ["factor1", "factor2", ...]
}`,

	ProblemSummary: `Summarize the following problem in a clear and concise manner.

## Problem Details
{{problem}}

## Requirements
1. Keep it under 100 words
2. Focus on the key issues
3. Use technical but accessible language`,

	RecommendationGeneration: `Based on the following root cause analysis, generate actionable recommendations.

## Root Cause
{{root_cause}}

## Context
{{context}}

## Requirements
1. Provide at least 3 recommendations
2. Prioritize by impact and feasibility
3. Include specific steps where possible`,
}
