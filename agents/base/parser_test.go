package base

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultParser_ContainsReasoningWords(t *testing.T) {
	parser := NewDefaultParser()

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "英文 - 包含 because",
			text:     "This is true because of the evidence",
			expected: true,
		},
		{
			name:     "英文 - 包含 therefore",
			text:     "Therefore, we conclude that",
			expected: true,
		},
		{
			name:     "英文 - 包含 thus",
			text:     "Thus the result is valid",
			expected: true,
		},
		{
			name:     "中文 - 包含 因为",
			text:     "因为这个原因，我们需要考虑",
			expected: true,
		},
		{
			name:     "中文 - 包含 所以",
			text:     "所以我们可以得出结论",
			expected: true,
		},
		{
			name:     "中文 - 包含 首先",
			text:     "首先我们要分析问题",
			expected: true,
		},
		{
			name:     "无推理词汇",
			text:     "Hello world",
			expected: false,
		},
		{
			name:     "空字符串",
			text:     "",
			expected: false,
		},
		{
			name:     "混合语言 - 包含推理词汇",
			text:     "We should consider this 因此 we need to analyze",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.ContainsReasoningWords(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultParser_CountReasoningWords(t *testing.T) {
	parser := NewDefaultParser()

	tests := []struct {
		name     string
		text     string
		minCount int // 最少包含的推理词汇数
	}{
		{
			name:     "多个英文推理词汇",
			text:     "Because of this, therefore we can conclude that if we analyze",
			minCount: 2,
		},
		{
			name:     "多个中文推理词汇",
			text:     "因为这个原因，所以我们可以首先分析然后得出结论",
			minCount: 2,
		},
		{
			name:     "无推理词汇",
			text:     "Hello world",
			minCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := parser.CountReasoningWords(tt.text)
			assert.GreaterOrEqual(t, count, tt.minCount)
		})
	}
}

func TestDefaultParser_GetReasoningWords(t *testing.T) {
	parser := NewDefaultParser()

	words := parser.GetReasoningWords()

	// 验证返回的词汇列表不为空
	assert.NotEmpty(t, words)

	// 验证包含一些关键的英文推理词汇
	assert.Contains(t, words, "because")
	assert.Contains(t, words, "therefore")

	// 验证包含一些关键的中文推理词汇
	assert.Contains(t, words, "因为")
	assert.Contains(t, words, "所以")
}

func TestDefaultParser_CustomReasoningWords(t *testing.T) {
	// 测试外部重写推理词汇
	parser := NewDefaultParser()
	config := parser.GetConfig()

	// 添加自定义推理词汇
	if keywords, ok := config.Languages["en"]; ok {
		keywords.ReasoningWords = append(keywords.ReasoningWords, "custom_reasoning_word")
	}

	// 验证自定义词汇生效
	assert.True(t, parser.ContainsReasoningWords("This has custom_reasoning_word in it"))
}

func TestDefaultParser_ParseSteps(t *testing.T) {
	parser := NewDefaultParser()

	tests := []struct {
		name     string
		response string
		minSteps int
	}{
		{
			name: "英文步骤",
			response: `Step 1: First we analyze the problem
Step 2: Then we find a solution
Step 3: Finally we implement it`,
			minSteps: 3,
		},
		{
			name: "中文步骤",
			response: `步骤1：首先分析问题
步骤2：然后找到解决方案
步骤3：最后实现它`,
			minSteps: 3,
		},
		{
			name: "数字列表格式",
			response: `1. First step
2. Second step
3. Third step`,
			minSteps: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := parser.ParseSteps(tt.response)
			assert.GreaterOrEqual(t, len(steps), tt.minSteps)
		})
	}
}

func TestDefaultParser_AreThoughtsRelated(t *testing.T) {
	parser := NewDefaultParser()

	tests := []struct {
		name     string
		thought1 string
		thought2 string
		expected bool
	}{
		{
			name:     "相关 - 都包含 therefore",
			thought1: "Therefore, we conclude",
			thought2: "Therefore, the result is",
			expected: true,
		},
		{
			name:     "相关 - 都包含 analysis",
			thought1: "Analysis shows",
			thought2: "Analysis indicates",
			expected: true,
		},
		{
			name:     "不相关 - 无共同关键词",
			thought1: "Random thought",
			thought2: "Another unrelated idea",
			expected: false,
		},
		{
			name:     "中文相关 - 都包含 因此",
			thought1: "因此我们得出结论",
			thought2: "因此这个方案可行",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			related := parser.AreThoughtsRelated(tt.thought1, tt.thought2)
			assert.Equal(t, tt.expected, related)
		})
	}
}

// TestIntToString 测试 intToString 辅助函数
func TestIntToString(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected string
	}{
		{
			name:     "正数",
			input:    42,
			expected: "42",
		},
		{
			name:     "零",
			input:    0,
			expected: "0",
		},
		{
			name:     "负数",
			input:    -123,
			expected: "-123",
		},
		{
			name:     "大数",
			input:    999999,
			expected: "999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := intToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStoi 测试 Stoi 字符串转整数函数
func TestStoi(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "正数",
			input:    "42",
			expected: 42,
		},
		{
			name:     "零",
			input:    "0",
			expected: 0,
		},
		{
			name:     "大数",
			input:    "999999",
			expected: 999999,
		},
		{
			name:     "带前缀文本",
			input:    "abc123",
			expected: 123,
		},
		{
			name:     "空字符串",
			input:    "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Stoi(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGenerateQuestionID 测试问题 ID 生成
func TestGenerateQuestionID(t *testing.T) {
	tests := []struct {
		name     string
		parentID string
		index    int
		expected string
	}{
		{
			name:     "基本生成",
			parentID: "q1",
			index:    0,
			expected: "q1_fq_0",
		},
		{
			name:     "非零索引",
			parentID: "parent",
			index:    5,
			expected: "parent_fq_5",
		},
		{
			name:     "空父 ID",
			parentID: "",
			index:    1,
			expected: "_fq_1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateQuestionID(tt.parentID, tt.index)
			assert.Equal(t, tt.expected, result)
		})
	}
}
