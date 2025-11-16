package prompt

import (
	"math"
	"strings"
	"unicode"
)

// PromptEvaluator evaluates prompt outputs
type PromptEvaluator struct {
	// Evaluation metrics weights
	weights struct {
		exactMatch   float64
		fuzzyMatch   float64
		semantic     float64
		format       float64
		completeness float64
		relevance    float64
	}
}

// NewPromptEvaluator creates a new prompt evaluator
func NewPromptEvaluator() *PromptEvaluator {
	evaluator := &PromptEvaluator{}

	// Set default weights
	evaluator.weights.exactMatch = 0.3
	evaluator.weights.fuzzyMatch = 0.2
	evaluator.weights.semantic = 0.2
	evaluator.weights.format = 0.1
	evaluator.weights.completeness = 0.1
	evaluator.weights.relevance = 0.1

	return evaluator
}

// Evaluate compares actual output with expected output and returns a score
func (e *PromptEvaluator) Evaluate(actual, expected string) float64 {
	if actual == "" && expected == "" {
		return 1.0
	}

	if actual == "" || expected == "" {
		return 0.0
	}

	scores := make(map[string]float64)

	// Exact match
	scores["exact"] = e.evaluateExactMatch(actual, expected)

	// Fuzzy match
	scores["fuzzy"] = e.evaluateFuzzyMatch(actual, expected)

	// Semantic similarity (simplified)
	scores["semantic"] = e.evaluateSemanticSimilarity(actual, expected)

	// Format compliance
	scores["format"] = e.evaluateFormatCompliance(actual, expected)

	// Completeness
	scores["completeness"] = e.evaluateCompleteness(actual, expected)

	// Relevance
	scores["relevance"] = e.evaluateRelevance(actual, expected)

	// Calculate weighted score
	totalScore := 0.0
	totalScore += scores["exact"] * e.weights.exactMatch
	totalScore += scores["fuzzy"] * e.weights.fuzzyMatch
	totalScore += scores["semantic"] * e.weights.semantic
	totalScore += scores["format"] * e.weights.format
	totalScore += scores["completeness"] * e.weights.completeness
	totalScore += scores["relevance"] * e.weights.relevance

	return totalScore
}

// evaluateExactMatch checks for exact string match
func (e *PromptEvaluator) evaluateExactMatch(actual, expected string) float64 {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)

	if actual == expected {
		return 1.0
	}

	// Case-insensitive match
	if strings.EqualFold(actual, expected) {
		return 0.95
	}

	// Whitespace-normalized match
	if e.normalizeWhitespace(actual) == e.normalizeWhitespace(expected) {
		return 0.9
	}

	return 0.0
}

// evaluateFuzzyMatch performs fuzzy string matching
func (e *PromptEvaluator) evaluateFuzzyMatch(actual, expected string) float64 {
	// Levenshtein distance
	distance := e.levenshteinDistance(actual, expected)
	maxLen := math.Max(float64(len(actual)), float64(len(expected)))

	if maxLen == 0 {
		return 1.0
	}

	similarity := 1.0 - (float64(distance) / maxLen)
	if similarity < 0 {
		similarity = 0
	}

	return similarity
}

// evaluateSemanticSimilarity evaluates semantic similarity
func (e *PromptEvaluator) evaluateSemanticSimilarity(actual, expected string) float64 {
	// Simplified semantic similarity based on word overlap
	actualWords := e.extractWords(actual)
	expectedWords := e.extractWords(expected)

	if len(expectedWords) == 0 {
		return 0.0
	}

	// Calculate Jaccard similarity
	intersection := e.wordIntersection(actualWords, expectedWords)
	union := e.wordUnion(actualWords, expectedWords)

	if len(union) == 0 {
		return 0.0
	}

	jaccard := float64(len(intersection)) / float64(len(union))

	// Calculate overlap coefficient
	minLen := math.Min(float64(len(actualWords)), float64(len(expectedWords)))
	if minLen == 0 {
		return jaccard
	}

	overlap := float64(len(intersection)) / minLen

	// Combine metrics
	return (jaccard + overlap) / 2
}

// evaluateFormatCompliance checks if format matches expected
func (e *PromptEvaluator) evaluateFormatCompliance(actual, expected string) float64 {
	score := 1.0

	// Check line count similarity
	actualLines := strings.Split(actual, "\n")
	expectedLines := strings.Split(expected, "\n")

	lineRatio := float64(len(actualLines)) / float64(len(expectedLines))
	if lineRatio > 2 || lineRatio < 0.5 {
		score *= 0.8
	}

	// Check structure markers (bullets, numbers, etc.)
	actualMarkers := e.extractStructureMarkers(actual)
	expectedMarkers := e.extractStructureMarkers(expected)

	if len(expectedMarkers) > 0 {
		markerMatch := e.compareMarkers(actualMarkers, expectedMarkers)
		score *= markerMatch
	}

	// Check JSON/code format if present
	if e.isJSON(expected) {
		if !e.isJSON(actual) {
			score *= 0.5
		}
	}

	return score
}

// evaluateCompleteness checks if all expected elements are present
func (e *PromptEvaluator) evaluateCompleteness(actual, expected string) float64 {
	// Extract key phrases from expected
	expectedPhrases := e.extractKeyPhrases(expected)
	if len(expectedPhrases) == 0 {
		return 1.0
	}

	// Count how many are present in actual
	present := 0
	actualLower := strings.ToLower(actual)

	for _, phrase := range expectedPhrases {
		if strings.Contains(actualLower, strings.ToLower(phrase)) {
			present++
		}
	}

	return float64(present) / float64(len(expectedPhrases))
}

// evaluateRelevance checks relevance of actual to expected
func (e *PromptEvaluator) evaluateRelevance(actual, expected string) float64 {
	// TF-IDF inspired relevance scoring
	expectedTerms := e.extractTerms(expected)
	actualTerms := e.extractTerms(actual)

	if len(expectedTerms) == 0 {
		return 0.0
	}

	// Calculate term frequency overlap
	relevanceScore := 0.0
	for term, expectedFreq := range expectedTerms {
		if actualFreq, exists := actualTerms[term]; exists {
			// Normalize frequencies
			normalizedExpected := float64(expectedFreq) / float64(e.totalTerms(expectedTerms))
			normalizedActual := float64(actualFreq) / float64(e.totalTerms(actualTerms))

			// Calculate similarity for this term
			termScore := math.Min(normalizedExpected, normalizedActual) / math.Max(normalizedExpected, normalizedActual)
			relevanceScore += termScore
		}
	}

	// Normalize by number of expected terms
	return relevanceScore / float64(len(expectedTerms))
}

// Helper methods

func (e *PromptEvaluator) normalizeWhitespace(s string) string {
	// Replace multiple spaces with single space
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

func (e *PromptEvaluator) levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	// Create distance matrix
	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
	}

	// Initialize first column and row
	for i := 0; i <= len(s1); i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len(s2); j++ {
		matrix[0][j] = j
	}

	// Calculate distances
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}

			matrix[i][j] = e.min3(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

func (e *PromptEvaluator) min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func (e *PromptEvaluator) extractWords(text string) []string {
	var words []string
	currentWord := []rune{}

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			currentWord = append(currentWord, unicode.ToLower(r))
		} else if len(currentWord) > 0 {
			words = append(words, string(currentWord))
			currentWord = []rune{}
		}
	}

	if len(currentWord) > 0 {
		words = append(words, string(currentWord))
	}

	return words
}

func (e *PromptEvaluator) wordIntersection(words1, words2 []string) []string {
	set1 := make(map[string]bool)
	for _, w := range words1 {
		set1[w] = true
	}

	var intersection []string
	seen := make(map[string]bool)

	for _, w := range words2 {
		if set1[w] && !seen[w] {
			intersection = append(intersection, w)
			seen[w] = true
		}
	}

	return intersection
}

func (e *PromptEvaluator) wordUnion(words1, words2 []string) []string {
	union := make(map[string]bool)

	for _, w := range words1 {
		union[w] = true
	}
	for _, w := range words2 {
		union[w] = true
	}

	result := make([]string, 0, len(union))
	for w := range union {
		result = append(result, w)
	}

	return result
}

func (e *PromptEvaluator) extractStructureMarkers(text string) []string {
	var markers []string
	lines := strings.Split(text, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for bullet points
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") ||
			strings.HasPrefix(trimmed, "• ") {
			markers = append(markers, "bullet")
		}

		// Check for numbered lists
		if len(trimmed) > 2 && unicode.IsDigit(rune(trimmed[0])) &&
			(trimmed[1] == '.' || trimmed[1] == ')') {
			markers = append(markers, "number")
		}

		// Check for headers
		if strings.HasPrefix(trimmed, "#") {
			markers = append(markers, "header")
		}
	}

	return markers
}

func (e *PromptEvaluator) compareMarkers(actual, expected []string) float64 {
	if len(expected) == 0 {
		return 1.0
	}

	// Count marker types
	actualCounts := e.countMarkerTypes(actual)
	expectedCounts := e.countMarkerTypes(expected)

	totalScore := 0.0
	totalTypes := 0

	for markerType, expectedCount := range expectedCounts {
		actualCount := actualCounts[markerType]
		if expectedCount > 0 {
			ratio := float64(actualCount) / float64(expectedCount)
			if ratio > 1 {
				ratio = 1 / ratio // Penalize over-use
			}
			totalScore += ratio
			totalTypes++
		}
	}

	if totalTypes == 0 {
		return 1.0
	}

	return totalScore / float64(totalTypes)
}

func (e *PromptEvaluator) countMarkerTypes(markers []string) map[string]int {
	counts := make(map[string]int)
	for _, marker := range markers {
		counts[marker]++
	}
	return counts
}

func (e *PromptEvaluator) isJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	return (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"))
}

func (e *PromptEvaluator) extractKeyPhrases(text string) []string {
	// Simple key phrase extraction - words longer than 4 characters
	words := e.extractWords(text)
	phrases := make(map[string]bool)

	for _, word := range words {
		if len(word) > 4 {
			phrases[word] = true
		}
	}

	// Also extract bigrams
	for i := 0; i < len(words)-1; i++ {
		if len(words[i]) > 2 && len(words[i+1]) > 2 {
			bigram := words[i] + " " + words[i+1]
			phrases[bigram] = true
		}
	}

	result := make([]string, 0, len(phrases))
	for phrase := range phrases {
		result = append(result, phrase)
	}

	return result
}

func (e *PromptEvaluator) extractTerms(text string) map[string]int {
	terms := make(map[string]int)
	words := e.extractWords(text)

	for _, word := range words {
		if len(word) > 2 { // Skip very short words
			terms[word]++
		}
	}

	return terms
}

func (e *PromptEvaluator) totalTerms(terms map[string]int) int {
	total := 0
	for _, count := range terms {
		total += count
	}
	return total
}

// SetWeights allows customizing evaluation weights
func (e *PromptEvaluator) SetWeights(exactMatch, fuzzyMatch, semantic, format, completeness, relevance float64) {
	total := exactMatch + fuzzyMatch + semantic + format + completeness + relevance

	// Normalize weights to sum to 1
	e.weights.exactMatch = exactMatch / total
	e.weights.fuzzyMatch = fuzzyMatch / total
	e.weights.semantic = semantic / total
	e.weights.format = format / total
	e.weights.completeness = completeness / total
	e.weights.relevance = relevance / total
}

// EvaluateWithMetrics returns detailed evaluation metrics
func (e *PromptEvaluator) EvaluateWithMetrics(actual, expected string) map[string]float64 {
	metrics := make(map[string]float64)

	metrics["exact_match"] = e.evaluateExactMatch(actual, expected)
	metrics["fuzzy_match"] = e.evaluateFuzzyMatch(actual, expected)
	metrics["semantic_similarity"] = e.evaluateSemanticSimilarity(actual, expected)
	metrics["format_compliance"] = e.evaluateFormatCompliance(actual, expected)
	metrics["completeness"] = e.evaluateCompleteness(actual, expected)
	metrics["relevance"] = e.evaluateRelevance(actual, expected)

	// Calculate overall score
	metrics["overall"] = 0.0
	metrics["overall"] += metrics["exact_match"] * e.weights.exactMatch
	metrics["overall"] += metrics["fuzzy_match"] * e.weights.fuzzyMatch
	metrics["overall"] += metrics["semantic_similarity"] * e.weights.semantic
	metrics["overall"] += metrics["format_compliance"] * e.weights.format
	metrics["overall"] += metrics["completeness"] * e.weights.completeness
	metrics["overall"] += metrics["relevance"] * e.weights.relevance

	return metrics
}
