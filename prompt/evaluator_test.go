package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPromptEvaluator(t *testing.T) {
	evaluator := NewPromptEvaluator()

	require.NotNil(t, evaluator)

	// Verify default weights
	assert.Equal(t, 0.3, evaluator.weights.exactMatch)
	assert.Equal(t, 0.2, evaluator.weights.fuzzyMatch)
	assert.Equal(t, 0.2, evaluator.weights.semantic)
	assert.Equal(t, 0.1, evaluator.weights.format)
	assert.Equal(t, 0.1, evaluator.weights.completeness)
	assert.Equal(t, 0.1, evaluator.weights.relevance)
}

func TestPromptEvaluator_Evaluate(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		actual   string
		expected string
		minScore float64
		maxScore float64
	}{
		{
			name:     "exact match",
			actual:   "Hello World",
			expected: "Hello World",
			minScore: 0.9,
			maxScore: 1.0,
		},
		{
			name:     "case insensitive match",
			actual:   "hello world",
			expected: "HELLO WORLD",
			minScore: 0.8,
			maxScore: 1.0,
		},
		{
			name:     "similar text",
			actual:   "The quick brown fox",
			expected: "The quick brown fox jumps",
			minScore: 0.5,
			maxScore: 0.9,
		},
		{
			name:     "completely different",
			actual:   "Hello",
			expected: "Goodbye",
			minScore: 0.0,
			maxScore: 0.3,
		},
		{
			name:     "both empty",
			actual:   "",
			expected: "",
			minScore: 1.0,
			maxScore: 1.0,
		},
		{
			name:     "actual empty",
			actual:   "",
			expected: "test",
			minScore: 0.0,
			maxScore: 0.0,
		},
		{
			name:     "expected empty",
			actual:   "test",
			expected: "",
			minScore: 0.0,
			maxScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := evaluator.Evaluate(tt.actual, tt.expected)
			assert.GreaterOrEqual(t, score, tt.minScore, "score should be >= minScore")
			assert.LessOrEqual(t, score, tt.maxScore, "score should be <= maxScore")
		})
	}
}

func TestPromptEvaluator_EvaluateExactMatch(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		actual   string
		expected string
		score    float64
	}{
		{
			name:     "exact match",
			actual:   "Hello World",
			expected: "Hello World",
			score:    1.0,
		},
		{
			name:     "case insensitive",
			actual:   "hello world",
			expected: "HELLO WORLD",
			score:    0.95,
		},
		{
			name:     "whitespace normalized",
			actual:   "Hello  World",
			expected: "Hello   World",
			score:    0.9,
		},
		{
			name:     "different text",
			actual:   "Hello",
			expected: "Goodbye",
			score:    0.0,
		},
		{
			name:     "with leading/trailing spaces",
			actual:   "  Hello World  ",
			expected: "Hello World",
			score:    1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := evaluator.evaluateExactMatch(tt.actual, tt.expected)
			assert.Equal(t, tt.score, score)
		})
	}
}

func TestPromptEvaluator_EvaluateFuzzyMatch(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		actual   string
		expected string
		minScore float64
	}{
		{
			name:     "exact match",
			actual:   "test",
			expected: "test",
			minScore: 1.0,
		},
		{
			name:     "one char difference",
			actual:   "test",
			expected: "text",
			minScore: 0.7,
		},
		{
			name:     "similar strings",
			actual:   "kitten",
			expected: "sitting",
			minScore: 0.4,
		},
		{
			name:     "completely different",
			actual:   "abc",
			expected: "xyz",
			minScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := evaluator.evaluateFuzzyMatch(tt.actual, tt.expected)
			assert.GreaterOrEqual(t, score, tt.minScore)
			assert.LessOrEqual(t, score, 1.0)
		})
	}
}

func TestPromptEvaluator_EvaluateSemanticSimilarity(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		actual   string
		expected string
		minScore float64
	}{
		{
			name:     "identical text",
			actual:   "the quick brown fox",
			expected: "the quick brown fox",
			minScore: 1.0,
		},
		{
			name:     "similar words",
			actual:   "the quick brown fox jumps",
			expected: "the quick brown dog runs",
			minScore: 0.4,
		},
		{
			name:     "no common words",
			actual:   "hello world",
			expected: "goodbye universe",
			minScore: 0.0,
		},
		{
			name:     "empty strings",
			actual:   "",
			expected: "",
			minScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := evaluator.evaluateSemanticSimilarity(tt.actual, tt.expected)
			assert.GreaterOrEqual(t, score, tt.minScore)
			assert.LessOrEqual(t, score, 1.0)
		})
	}
}

func TestPromptEvaluator_EvaluateFormatCompliance(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		actual   string
		expected string
		minScore float64
	}{
		{
			name:     "both JSON",
			actual:   `{"key": "value"}`,
			expected: `{"key": "value"}`,
			minScore: 0.9,
		},
		{
			name:     "both markdown",
			actual:   "# Title\n## Subtitle",
			expected: "# Other\n## Header",
			minScore: 0.8,
		},
		{
			name:     "different formats",
			actual:   `{"key": "value"}`,
			expected: "plain text",
			minScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := evaluator.evaluateFormatCompliance(tt.actual, tt.expected)
			assert.GreaterOrEqual(t, score, tt.minScore)
			assert.LessOrEqual(t, score, 1.0)
		})
	}
}

func TestPromptEvaluator_EvaluateCompleteness(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		actual   string
		expected string
		minScore float64
	}{
		{
			name:     "equal length",
			actual:   "Hello World",
			expected: "Hello World",
			minScore: 1.0,
		},
		{
			name:     "actual longer",
			actual:   "Hello World Extended",
			expected: "Hello World",
			minScore: 0.5,
		},
		{
			name:     "actual shorter",
			actual:   "Hello",
			expected: "Hello World",
			minScore: 0.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := evaluator.evaluateCompleteness(tt.actual, tt.expected)
			assert.GreaterOrEqual(t, score, tt.minScore)
			assert.LessOrEqual(t, score, 1.0)
		})
	}
}

func TestPromptEvaluator_EvaluateRelevance(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		actual   string
		expected string
		minScore float64
	}{
		{
			name:     "high relevance",
			actual:   "machine learning algorithm neural network",
			expected: "deep learning neural network model",
			minScore: 0.3,
		},
		{
			name:     "low relevance",
			actual:   "cooking recipe ingredients",
			expected: "computer programming code",
			minScore: 0.0,
		},
		{
			name:     "empty strings",
			actual:   "",
			expected: "",
			minScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := evaluator.evaluateRelevance(tt.actual, tt.expected)
			assert.GreaterOrEqual(t, score, tt.minScore)
			assert.LessOrEqual(t, score, 1.0)
		})
	}
}

func TestPromptEvaluator_LevenshteinDistance(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		s1       string
		s2       string
		expected int
	}{
		{
			name:     "identical strings",
			s1:       "test",
			s2:       "test",
			expected: 0,
		},
		{
			name:     "one insertion",
			s1:       "test",
			s2:       "tests",
			expected: 1,
		},
		{
			name:     "one deletion",
			s1:       "test",
			s2:       "tes",
			expected: 1,
		},
		{
			name:     "one substitution",
			s1:       "test",
			s2:       "text",
			expected: 1,
		},
		{
			name:     "kitten to sitting",
			s1:       "kitten",
			s2:       "sitting",
			expected: 3,
		},
		{
			name:     "empty strings",
			s1:       "",
			s2:       "",
			expected: 0,
		},
		{
			name:     "one empty",
			s1:       "test",
			s2:       "",
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			distance := evaluator.levenshteinDistance(tt.s1, tt.s2)
			assert.Equal(t, tt.expected, distance)
		})
	}
}

func TestPromptEvaluator_ExtractWords(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "simple text",
			text:     "hello world",
			expected: []string{"hello", "world"},
		},
		{
			name:     "with punctuation",
			text:     "Hello, World! How are you?",
			expected: []string{"hello", "world", "how", "are", "you"},
		},
		{
			name:     "with numbers",
			text:     "test123 abc456",
			expected: []string{"test123", "abc456"},
		},
		{
			name:     "empty string",
			text:     "",
			expected: []string{},
		},
		{
			name:     "only punctuation",
			text:     "!@#$%",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words := evaluator.extractWords(tt.text)
			if len(tt.expected) == 0 {
				assert.Empty(t, words)
			} else {
				assert.Equal(t, tt.expected, words)
			}
		})
	}
}

func TestPromptEvaluator_IsJSON(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "valid JSON object",
			text:     `{"key": "value"}`,
			expected: true,
		},
		{
			name:     "valid JSON array",
			text:     `[1, 2, 3]`,
			expected: true,
		},
		{
			name:     "plain text",
			text:     "hello world",
			expected: false,
		},
		{
			name:     "empty string",
			text:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluator.isJSON(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPromptEvaluator_SetWeights(t *testing.T) {
	evaluator := NewPromptEvaluator()

	evaluator.SetWeights(0.4, 0.3, 0.1, 0.05, 0.1, 0.05)

	assert.Equal(t, 0.4, evaluator.weights.exactMatch)
	assert.Equal(t, 0.3, evaluator.weights.fuzzyMatch)
	assert.Equal(t, 0.1, evaluator.weights.semantic)
	assert.Equal(t, 0.05, evaluator.weights.format)
	assert.Equal(t, 0.1, evaluator.weights.completeness)
	assert.Equal(t, 0.05, evaluator.weights.relevance)
}

func TestPromptEvaluator_EvaluateWithMetrics(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name            string
		actual          string
		expected        string
		expectedMetrics []string
	}{
		{
			name:     "exact match",
			actual:   "Hello World",
			expected: "Hello World",
			expectedMetrics: []string{
				"exact_match",
				"fuzzy_match",
				"semantic_similarity",
				"format_compliance",
				"completeness",
				"relevance",
				"overall",
			},
		},
		{
			name:     "different text",
			actual:   "Hello",
			expected: "Goodbye",
			expectedMetrics: []string{
				"exact_match",
				"fuzzy_match",
				"semantic_similarity",
				"format_compliance",
				"completeness",
				"relevance",
				"overall",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := evaluator.EvaluateWithMetrics(tt.actual, tt.expected)

			require.NotNil(t, metrics)

			// Check all expected metrics are present
			for _, metric := range tt.expectedMetrics {
				assert.Contains(t, metrics, metric)
				assert.GreaterOrEqual(t, metrics[metric], 0.0)
				assert.LessOrEqual(t, metrics[metric], 1.0)
			}
		})
	}
}

func TestPromptEvaluator_NormalizeWhitespace(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "multiple spaces",
			input:    "hello    world",
			expected: "hello world",
		},
		{
			name:     "tabs and newlines",
			input:    "hello\t\nworld",
			expected: "hello world",
		},
		{
			name:     "leading and trailing spaces",
			input:    "  hello world  ",
			expected: "hello world",
		},
		{
			name:     "already normalized",
			input:    "hello world",
			expected: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluator.normalizeWhitespace(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPromptEvaluator_ExtractKeyPhrases(t *testing.T) {
	evaluator := NewPromptEvaluator()

	tests := []struct {
		name     string
		text     string
		minCount int
	}{
		{
			name:     "technical text",
			text:     "machine learning neural network deep learning algorithm",
			minCount: 2,
		},
		{
			name:     "simple text",
			text:     "hello world",
			minCount: 0,
		},
		{
			name:     "empty text",
			text:     "",
			minCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phrases := evaluator.extractKeyPhrases(tt.text)
			assert.GreaterOrEqual(t, len(phrases), tt.minCount)
		})
	}
}

// Benchmark tests
func BenchmarkPromptEvaluator_Evaluate(b *testing.B) {
	evaluator := NewPromptEvaluator()
	actual := "The quick brown fox jumps over the lazy dog"
	expected := "The quick brown fox jumps over the lazy cat"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evaluator.Evaluate(actual, expected)
	}
}

func BenchmarkPromptEvaluator_LevenshteinDistance(b *testing.B) {
	evaluator := NewPromptEvaluator()
	s1 := "kitten"
	s2 := "sitting"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evaluator.levenshteinDistance(s1, s2)
	}
}
