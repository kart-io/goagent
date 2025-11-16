package prompt

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// PromptOptimizer optimizes prompts based on feedback and performance
type PromptOptimizer struct {
	evaluator        *PromptEvaluator
	learningRate     float64
	minConfidence    float64
	maxIterations    int
	improvementRules map[string]OptimizationRule
}

// OptimizationRule defines a rule for optimizing prompts
type OptimizationRule struct {
	ID          string
	Name        string
	Description string
	Condition   func(*Prompt, []Feedback) bool
	Action      func(*Prompt, []Feedback) *Prompt
	Priority    int
}

// NewPromptOptimizer creates a new prompt optimizer
func NewPromptOptimizer() *PromptOptimizer {
	optimizer := &PromptOptimizer{
		evaluator:        NewPromptEvaluator(),
		learningRate:     0.1,
		minConfidence:    0.7,
		maxIterations:    10,
		improvementRules: make(map[string]OptimizationRule),
	}

	// Register default optimization rules
	optimizer.registerDefaultRules()

	return optimizer
}

// Optimize improves a prompt based on feedback
func (o *PromptOptimizer) Optimize(prompt *Prompt, feedback []Feedback) *Prompt {
	if len(feedback) == 0 {
		return prompt
	}

	// Create a copy of the prompt for optimization
	optimized := o.copyPrompt(prompt)

	// Apply optimization rules
	rules := o.getSortedRules()
	for _, rule := range rules {
		if rule.Condition(optimized, feedback) {
			optimized = rule.Action(optimized, feedback)
		}
	}

	// Analyze feedback patterns
	patterns := o.analyzeFeedbackPatterns(feedback)

	// Apply pattern-based optimizations
	optimized = o.applyPatternOptimizations(optimized, patterns)

	// Update based on performance metrics
	optimized = o.updateFromMetrics(optimized, feedback)

	// Refine examples if few-shot
	if optimized.Strategy == StrategyFewShot {
		optimized = o.refineExamples(optimized, feedback)
	}

	// Optimize constraints
	optimized = o.optimizeConstraints(optimized, feedback)

	// Update version
	optimized.Version = o.incrementVersion(optimized.Version)
	optimized.UpdatedAt = time.Now()

	return optimized
}

// OptimizeIterative performs iterative optimization
func (o *PromptOptimizer) OptimizeIterative(ctx context.Context, prompt *Prompt, testCases []TestCase, executor func(*Prompt) (string, error)) (*Prompt, error) {
	current := o.copyPrompt(prompt)
	bestScore := 0.0

	for i := 0; i < o.maxIterations; i++ {
		// Test current prompt
		testResult := o.testPrompt(ctx, current, testCases, executor)

		if testResult.SuccessRate > bestScore {
			bestScore = testResult.SuccessRate
		}

		// Check if optimization goal is met
		if testResult.SuccessRate >= o.minConfidence {
			return current, nil
		}

		// Generate feedback from test results
		feedback := o.generateFeedbackFromTests(testResult)

		// Optimize based on feedback
		current = o.Optimize(current, feedback)

		// Check context cancellation
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		default:
		}
	}

	return current, nil
}

// registerDefaultRules registers default optimization rules
func (o *PromptOptimizer) registerDefaultRules() {
	// Rule: Add more examples for low accuracy
	o.improvementRules["add_examples"] = OptimizationRule{
		ID:          "add_examples",
		Name:        "Add Examples",
		Description: "Add more examples when accuracy is low",
		Priority:    10,
		Condition: func(p *Prompt, feedback []Feedback) bool {
			avgScore := o.calculateAverageScore(feedback)
			return avgScore < 0.6 && len(p.Examples) < 5
		},
		Action: func(p *Prompt, feedback []Feedback) *Prompt {
			// Add high-scoring examples from feedback
			for _, f := range feedback {
				if f.Score > 0.8 && len(p.Examples) < 5 {
					p.Examples = append(p.Examples, Example{
						Input:  f.Input,
						Output: f.Output,
					})
				}
			}
			p.Strategy = StrategyFewShot
			return p
		},
	}

	// Rule: Simplify overly complex prompts
	o.improvementRules["simplify"] = OptimizationRule{
		ID:          "simplify",
		Name:        "Simplify Prompt",
		Description: "Simplify when prompt is too complex",
		Priority:    8,
		Condition: func(p *Prompt, feedback []Feedback) bool {
			return len(p.Template) > 1000 || len(p.Constraints) > 10
		},
		Action: func(p *Prompt, feedback []Feedback) *Prompt {
			// Remove redundant constraints
			p.Constraints = o.deduplicateConstraints(p.Constraints)

			// Simplify template
			if len(p.Template) > 1000 {
				p.Template = o.simplifyTemplate(p.Template)
			}

			return p
		},
	}

	// Rule: Add chain-of-thought for complex reasoning
	o.improvementRules["add_cot"] = OptimizationRule{
		ID:          "add_cot",
		Name:        "Add Chain of Thought",
		Description: "Add CoT prompting for complex tasks",
		Priority:    9,
		Condition: func(p *Prompt, feedback []Feedback) bool {
			// Check if feedback indicates reasoning issues
			for _, f := range feedback {
				if strings.Contains(strings.ToLower(f.Comments), "reasoning") ||
					strings.Contains(strings.ToLower(f.Comments), "logic") {
					return true
				}
			}
			return false
		},
		Action: func(p *Prompt, feedback []Feedback) *Prompt {
			p.Strategy = StrategyChainOfThought
			if !strings.Contains(p.Template, "step by step") {
				p.Template += "\n\nLet's think about this step by step:"
			}
			return p
		},
	}

	// Rule: Improve clarity
	o.improvementRules["improve_clarity"] = OptimizationRule{
		ID:          "improve_clarity",
		Name:        "Improve Clarity",
		Description: "Make instructions clearer",
		Priority:    7,
		Condition: func(p *Prompt, feedback []Feedback) bool {
			for _, f := range feedback {
				if strings.Contains(strings.ToLower(f.Comments), "unclear") ||
					strings.Contains(strings.ToLower(f.Comments), "ambiguous") {
					return true
				}
			}
			return false
		},
		Action: func(p *Prompt, feedback []Feedback) *Prompt {
			// Add clarifying constraints
			p.Constraints = append(p.Constraints,
				"Be specific and precise in your response",
				"If any part is unclear, state your assumptions",
			)

			// Update template for clarity
			p.Template = o.clarifyTemplate(p.Template)

			return p
		},
	}

	// Rule: Add role-playing for specific domains
	o.improvementRules["add_role"] = OptimizationRule{
		ID:          "add_role",
		Name:        "Add Role Playing",
		Description: "Add role for domain-specific tasks",
		Priority:    6,
		Condition: func(p *Prompt, feedback []Feedback) bool {
			return p.SystemPrompt == "" && o.requiresExpertise(feedback)
		},
		Action: func(p *Prompt, feedback []Feedback) *Prompt {
			p.Strategy = StrategyRolePlay
			domain := o.identifyDomain(feedback)
			p.SystemPrompt = fmt.Sprintf("You are an expert %s. ", domain)
			return p
		},
	}
}

// analyzeFeedbackPatterns analyzes patterns in feedback
func (o *PromptOptimizer) analyzeFeedbackPatterns(feedback []Feedback) map[string]interface{} {
	patterns := make(map[string]interface{})

	// Score distribution
	scores := make([]float64, 0, len(feedback))
	for _, f := range feedback {
		scores = append(scores, f.Score)
	}
	patterns["avg_score"] = o.calculateAverage(scores)
	patterns["std_dev"] = o.calculateStdDev(scores)
	patterns["min_score"] = o.findMin(scores)
	patterns["max_score"] = o.findMax(scores)

	// Common failure patterns
	failureKeywords := o.extractFailureKeywords(feedback)
	patterns["failure_keywords"] = failureKeywords

	// Success patterns
	successPatterns := o.extractSuccessPatterns(feedback)
	patterns["success_patterns"] = successPatterns

	// Response time analysis
	var responseTimes []time.Duration
	for i := 1; i < len(feedback); i++ {
		responseTimes = append(responseTimes,
			feedback[i].Timestamp.Sub(feedback[i-1].Timestamp))
	}
	if len(responseTimes) > 0 {
		avgTime := time.Duration(0)
		for _, t := range responseTimes {
			avgTime += t
		}
		patterns["avg_response_time"] = avgTime / time.Duration(len(responseTimes))
	}

	return patterns
}

// applyPatternOptimizations applies optimizations based on patterns
func (o *PromptOptimizer) applyPatternOptimizations(prompt *Prompt, patterns map[string]interface{}) *Prompt {
	avgScore, _ := patterns["avg_score"].(float64)

	// Low average score: make prompt more explicit
	if avgScore < 0.5 {
		prompt.Template = "Please carefully follow these instructions:\n\n" + prompt.Template
		prompt.Constraints = append([]string{"Double-check your response for accuracy"}, prompt.Constraints...)
	}

	// High variance: add consistency instructions
	if stdDev, ok := patterns["std_dev"].(float64); ok && stdDev > 0.3 {
		prompt.Constraints = append(prompt.Constraints,
			"Maintain consistency in your responses",
			"Follow the same format for all outputs",
		)
	}

	// Specific failure keywords: address them
	if keywords, ok := patterns["failure_keywords"].([]string); ok {
		for _, keyword := range keywords {
			prompt = o.addressFailureKeyword(prompt, keyword)
		}
	}

	return prompt
}

// Helper methods

func (o *PromptOptimizer) copyPrompt(prompt *Prompt) *Prompt {
	copied := *prompt

	// Deep copy slices and maps
	copied.Variables = make(map[string]interface{})
	for k, v := range prompt.Variables {
		copied.Variables[k] = v
	}

	copied.Examples = make([]Example, len(prompt.Examples))
	copy(copied.Examples, prompt.Examples)

	copied.Constraints = make([]string, len(prompt.Constraints))
	copy(copied.Constraints, prompt.Constraints)

	copied.Tags = make([]string, len(prompt.Tags))
	copy(copied.Tags, prompt.Tags)

	copied.Metadata = make(map[string]interface{})
	for k, v := range prompt.Metadata {
		copied.Metadata[k] = v
	}

	return &copied
}

func (o *PromptOptimizer) getSortedRules() []OptimizationRule {
	rules := make([]OptimizationRule, 0, len(o.improvementRules))
	for _, rule := range o.improvementRules {
		rules = append(rules, rule)
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority > rules[j].Priority
	})

	return rules
}

func (o *PromptOptimizer) calculateAverageScore(feedback []Feedback) float64 {
	if len(feedback) == 0 {
		return 0
	}

	sum := 0.0
	for _, f := range feedback {
		sum += f.Score
	}

	return sum / float64(len(feedback))
}

func (o *PromptOptimizer) calculateAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}

	return sum / float64(len(values))
}

func (o *PromptOptimizer) calculateStdDev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}

	avg := o.calculateAverage(values)
	sumSquares := 0.0

	for _, v := range values {
		diff := v - avg
		sumSquares += diff * diff
	}

	return math.Sqrt(sumSquares / float64(len(values)-1))
}

func (o *PromptOptimizer) findMin(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}

	return min
}

func (o *PromptOptimizer) findMax(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}

	return max
}

func (o *PromptOptimizer) extractFailureKeywords(feedback []Feedback) []string {
	keywordCount := make(map[string]int)

	for _, f := range feedback {
		if f.Score < 0.5 {
			words := strings.Fields(strings.ToLower(f.Comments))
			for _, word := range words {
				keywordCount[word]++
			}
		}
	}

	// Extract top keywords
	var keywords []string
	for word, count := range keywordCount {
		if count >= 2 {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

func (o *PromptOptimizer) extractSuccessPatterns(feedback []Feedback) []string {
	var patterns []string

	for _, f := range feedback {
		if f.Score > 0.8 && f.Expected != "" {
			// Extract successful patterns
			if strings.Contains(f.Output, f.Expected) {
				patterns = append(patterns, f.Expected)
			}
		}
	}

	return patterns
}

func (o *PromptOptimizer) updateFromMetrics(prompt *Prompt, feedback []Feedback) *Prompt {
	avgScore := o.calculateAverageScore(feedback)

	// Update metadata with optimization metrics
	if prompt.Metadata == nil {
		prompt.Metadata = make(map[string]interface{})
	}

	prompt.Metadata["last_optimization"] = time.Now()
	prompt.Metadata["optimization_score"] = avgScore
	prompt.Metadata["feedback_count"] = len(feedback)

	return prompt
}

func (o *PromptOptimizer) refineExamples(prompt *Prompt, feedback []Feedback) *Prompt {
	// Score existing examples
	exampleScores := make(map[int]float64)
	for i, example := range prompt.Examples {
		score := o.scoreExample(example, feedback)
		exampleScores[i] = score
	}

	// Keep only high-scoring examples
	var refinedExamples []Example
	for i, example := range prompt.Examples {
		if score, ok := exampleScores[i]; ok && score >= 0.7 {
			refinedExamples = append(refinedExamples, example)
		}
	}

	// Add new high-quality examples from feedback
	for _, f := range feedback {
		if f.Score > 0.9 && len(refinedExamples) < 5 {
			refinedExamples = append(refinedExamples, Example{
				Input:     f.Input,
				Output:    f.Output,
				Reasoning: f.Comments,
			})
		}
	}

	prompt.Examples = refinedExamples
	return prompt
}

func (o *PromptOptimizer) optimizeConstraints(prompt *Prompt, feedback []Feedback) *Prompt {
	// Remove duplicate constraints
	prompt.Constraints = o.deduplicateConstraints(prompt.Constraints)

	// Add constraints based on common issues
	for _, f := range feedback {
		if f.Score < 0.5 {
			if strings.Contains(f.Comments, "format") {
				prompt.Constraints = append(prompt.Constraints,
					"Follow the specified output format exactly")
			}
			if strings.Contains(f.Comments, "incomplete") {
				prompt.Constraints = append(prompt.Constraints,
					"Ensure your response is complete and addresses all aspects")
			}
		}
	}

	// Limit number of constraints
	if len(prompt.Constraints) > 10 {
		// Keep most important constraints
		prompt.Constraints = prompt.Constraints[:10]
	}

	return prompt
}

func (o *PromptOptimizer) deduplicateConstraints(constraints []string) []string {
	seen := make(map[string]bool)
	var unique []string

	for _, c := range constraints {
		normalized := strings.ToLower(strings.TrimSpace(c))
		if !seen[normalized] {
			seen[normalized] = true
			unique = append(unique, c)
		}
	}

	return unique
}

func (o *PromptOptimizer) simplifyTemplate(template string) string {
	// Remove excessive whitespace
	template = strings.TrimSpace(template)

	// Replace multiple newlines with double newline
	for strings.Contains(template, "\n\n\n") {
		template = strings.ReplaceAll(template, "\n\n\n", "\n\n")
	}

	// Simplify overly complex sentences
	sentences := strings.Split(template, ". ")
	var simplified []string

	for _, sentence := range sentences {
		if len(sentence) > 100 {
			// Break down long sentences
			parts := strings.Split(sentence, ", ")
			if len(parts) > 3 {
				simplified = append(simplified, parts[0]+". "+
					strings.Join(parts[1:], ", "))
			} else {
				simplified = append(simplified, sentence)
			}
		} else {
			simplified = append(simplified, sentence)
		}
	}

	return strings.Join(simplified, ". ")
}

func (o *PromptOptimizer) clarifyTemplate(template string) string {
	clarifications := map[string]string{
		"it":    "the specified item",
		"this":  "the current context",
		"that":  "the mentioned item",
		"they":  "the items",
		"these": "the listed items",
		"those": "the referenced items",
	}

	result := template
	for vague, clear := range clarifications {
		result = strings.ReplaceAll(result, " "+vague+" ", " "+clear+" ")
	}

	return result
}

func (o *PromptOptimizer) requiresExpertise(feedback []Feedback) bool {
	expertiseKeywords := []string{"technical", "domain", "expert", "specialized", "professional"}

	for _, f := range feedback {
		comment := strings.ToLower(f.Comments)
		for _, keyword := range expertiseKeywords {
			if strings.Contains(comment, keyword) {
				return true
			}
		}
	}

	return false
}

func (o *PromptOptimizer) identifyDomain(feedback []Feedback) string {
	domains := map[string][]string{
		"software engineer": {"code", "programming", "software", "debug", "algorithm"},
		"data scientist":    {"data", "analysis", "statistics", "machine learning", "model"},
		"teacher":           {"explain", "teach", "learn", "understand", "education"},
		"writer":            {"write", "content", "article", "story", "narrative"},
		"analyst":           {"analyze", "evaluate", "assess", "review", "examine"},
	}

	domainScores := make(map[string]int)

	for _, f := range feedback {
		text := strings.ToLower(f.Input + " " + f.Comments)
		for domain, keywords := range domains {
			for _, keyword := range keywords {
				if strings.Contains(text, keyword) {
					domainScores[domain]++
				}
			}
		}
	}

	// Find domain with highest score
	maxScore := 0
	selectedDomain := "assistant"

	for domain, score := range domainScores {
		if score > maxScore {
			maxScore = score
			selectedDomain = domain
		}
	}

	return selectedDomain
}

func (o *PromptOptimizer) addressFailureKeyword(prompt *Prompt, keyword string) *Prompt {
	switch keyword {
	case "wrong", "incorrect", "error":
		prompt.Constraints = append(prompt.Constraints,
			"Verify accuracy before responding")
	case "incomplete", "missing":
		prompt.Constraints = append(prompt.Constraints,
			"Provide comprehensive and complete responses")
	case "unclear", "confusing":
		prompt.Constraints = append(prompt.Constraints,
			"Use clear and unambiguous language")
	case "format", "structure":
		prompt.Constraints = append(prompt.Constraints,
			"Follow the specified format precisely")
	}

	return prompt
}

func (o *PromptOptimizer) incrementVersion(version string) string {
	if version == "" {
		return "1.0.0"
	}

	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return version + ".1"
	}

	// Increment patch version
	patch := 0
	fmt.Sscanf(parts[2], "%d", &patch)
	parts[2] = fmt.Sprintf("%d", patch+1)

	return strings.Join(parts, ".")
}

func (o *PromptOptimizer) scoreExample(example Example, feedback []Feedback) float64 {
	totalScore := 0.0
	count := 0

	for _, f := range feedback {
		similarity := o.calculateSimilarity(example.Input, f.Input)
		if similarity > 0.7 {
			totalScore += f.Score * similarity
			count++
		}
	}

	if count == 0 {
		return 0.5 // Default score
	}

	return totalScore / float64(count)
}

func (o *PromptOptimizer) calculateSimilarity(str1, str2 string) float64 {
	// Simple word overlap similarity
	words1 := strings.Fields(strings.ToLower(str1))
	words2 := strings.Fields(strings.ToLower(str2))

	if len(words1) == 0 || len(words2) == 0 {
		return 0
	}

	wordSet := make(map[string]bool)
	for _, w := range words1 {
		wordSet[w] = true
	}

	overlap := 0
	for _, w := range words2 {
		if wordSet[w] {
			overlap++
		}
	}

	return float64(overlap) / float64(len(words2))
}

func (o *PromptOptimizer) testPrompt(ctx context.Context, prompt *Prompt, testCases []TestCase, executor func(*Prompt) (string, error)) *TestResult {
	result := &TestResult{
		PromptID:   prompt.ID,
		TotalCases: len(testCases),
		Details:    make([]TestDetail, 0, len(testCases)),
	}

	for _, testCase := range testCases {
		// Set variables for the test case
		prompt.Variables = testCase.Input

		// Execute prompt
		output, err := executor(prompt)

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
			score := o.evaluator.Evaluate(output, testCase.Expected)
			detail.Score = score
			detail.Passed = score >= 0.8

			if detail.Passed {
				result.PassedCases++
			} else {
				result.FailedCases++
			}
		}

		result.Details = append(result.Details, detail)
	}

	result.SuccessRate = float64(result.PassedCases) / float64(result.TotalCases)

	return result
}

func (o *PromptOptimizer) generateFeedbackFromTests(testResult *TestResult) []Feedback {
	feedback := make([]Feedback, 0, len(testResult.Details))

	for _, detail := range testResult.Details {
		f := Feedback{
			PromptID:  testResult.PromptID,
			Input:     detail.TestCaseID,
			Output:    detail.Actual,
			Expected:  detail.Expected,
			Score:     detail.Score,
			Timestamp: time.Now(),
		}

		if !detail.Passed {
			if detail.Error != "" {
				f.Comments = fmt.Sprintf("Error: %s", detail.Error)
			} else {
				f.Comments = fmt.Sprintf("Output did not match expected (score: %.2f)", detail.Score)
			}
		}

		feedback = append(feedback, f)
	}

	return feedback
}
