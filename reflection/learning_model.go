package reflection

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// LearningModel manages and applies learned knowledge
type LearningModel struct {
	learnings     map[string]*LearningPoint
	categories    map[string][]*LearningPoint
	applications  map[string]int     // Track how often each learning is applied
	effectiveness map[string]float64 // Track effectiveness of each learning
	mu            sync.RWMutex
}

// NewLearningModel creates a new learning model
func NewLearningModel() *LearningModel {
	return &LearningModel{
		learnings:     make(map[string]*LearningPoint),
		categories:    make(map[string][]*LearningPoint),
		applications:  make(map[string]int),
		effectiveness: make(map[string]float64),
	}
}

// AddLearning adds a new learning to the model
func (m *LearningModel) AddLearning(learning LearningPoint) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.generateLearningID(learning)
	m.learnings[id] = &learning

	// Update category index
	if m.categories[learning.Category] == nil {
		m.categories[learning.Category] = []*LearningPoint{}
	}
	m.categories[learning.Category] = append(m.categories[learning.Category], &learning)

	// Initialize tracking
	m.applications[id] = 0
	m.effectiveness[id] = learning.Confidence
}

// UpdateWithLearnings updates the model with multiple learnings
func (m *LearningModel) UpdateWithLearnings(learnings []LearningPoint) {
	for _, learning := range learnings {
		m.AddLearning(learning)
	}
}

// GetRelevantLearnings retrieves learnings relevant to the given context
func (m *LearningModel) GetRelevantLearnings(context interface{}) []LearningPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var relevant []LearningPoint
	contextStr := fmt.Sprintf("%v", context)

	// Score each learning for relevance
	type scoredLearning struct {
		learning *LearningPoint
		score    float64
	}

	var scored []scoredLearning

	for id, learning := range m.learnings {
		score := m.calculateRelevanceScore(learning, contextStr)

		// Include effectiveness in scoring
		if effectiveness, ok := m.effectiveness[id]; ok {
			score *= effectiveness
		}

		if score > 0.3 { // Threshold for relevance
			scored = append(scored, scoredLearning{
				learning: learning,
				score:    score,
			})
		}
	}

	// Sort by score
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Return top relevant learnings
	maxLearnings := 5
	for i := 0; i < len(scored) && i < maxLearnings; i++ {
		relevant = append(relevant, *scored[i].learning)
		// Track application
		m.trackApplication(scored[i].learning)
	}

	return relevant
}

// GetLearningsByCategory retrieves learnings by category
func (m *LearningModel) GetLearningsByCategory(category string) []LearningPoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var learnings []LearningPoint
	if categoryLearnings, ok := m.categories[category]; ok {
		for _, learning := range categoryLearnings {
			learnings = append(learnings, *learning)
		}
	}

	return learnings
}

// UpdateEffectiveness updates the effectiveness of a learning based on outcomes
func (m *LearningModel) UpdateEffectiveness(learningID string, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.effectiveness[learningID]
	if !ok {
		current = 0.5
	}

	// Update effectiveness using exponential moving average
	alpha := 0.1 // Learning rate
	if success {
		m.effectiveness[learningID] = current + alpha*(1.0-current)
	} else {
		m.effectiveness[learningID] = current + alpha*(0.0-current)
	}
}

// GetStatistics returns statistics about the learning model
func (m *LearningModel) GetStatistics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalApplications := 0
	avgEffectiveness := 0.0

	for id := range m.learnings {
		totalApplications += m.applications[id]
		avgEffectiveness += m.effectiveness[id]
	}

	if len(m.learnings) > 0 {
		avgEffectiveness /= float64(len(m.learnings))
	}

	stats := map[string]interface{}{
		"total_learnings":       len(m.learnings),
		"total_categories":      len(m.categories),
		"total_applications":    totalApplications,
		"average_effectiveness": avgEffectiveness,
	}

	// Category distribution
	categoryDist := make(map[string]int)
	for category, learnings := range m.categories {
		categoryDist[category] = len(learnings)
	}
	stats["category_distribution"] = categoryDist

	return stats
}

// PruneLowEffectiveness removes learnings with consistently low effectiveness
func (m *LearningModel) PruneLowEffectiveness(threshold float64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	pruned := 0
	for id, effectiveness := range m.effectiveness {
		if effectiveness < threshold && m.applications[id] > 10 {
			// Only prune if it's been applied enough times to judge
			delete(m.learnings, id)
			delete(m.applications, id)
			delete(m.effectiveness, id)
			pruned++
		}
	}

	// Rebuild category index
	m.rebuildCategoryIndex()

	return pruned
}

// Helper methods

func (m *LearningModel) generateLearningID(learning LearningPoint) string {
	return fmt.Sprintf("%s_%s_%d", learning.Category, learning.Context, learning.Timestamp.Unix())
}

func (m *LearningModel) calculateRelevanceScore(learning *LearningPoint, context string) float64 {
	score := 0.0

	// Check context similarity (simplified)
	if learning.Context == context {
		score += 0.5
	}

	// Factor in applicability
	score += learning.Applicability * 0.3

	// Factor in confidence
	score += learning.Confidence * 0.2

	// Decay based on age
	age := time.Since(learning.Timestamp)
	agePenalty := age.Hours() / (24 * 30) // Decay over months
	if agePenalty > 1.0 {
		agePenalty = 1.0
	}
	score *= (1.0 - agePenalty*0.2) // Max 20% penalty for age

	return score
}

func (m *LearningModel) trackApplication(learning *LearningPoint) {
	id := m.generateLearningID(*learning)
	m.applications[id]++
}

func (m *LearningModel) rebuildCategoryIndex() {
	m.categories = make(map[string][]*LearningPoint)
	for _, learning := range m.learnings {
		if m.categories[learning.Category] == nil {
			m.categories[learning.Category] = []*LearningPoint{}
		}
		m.categories[learning.Category] = append(m.categories[learning.Category], learning)
	}
}

// ReflectionMetrics tracks metrics for reflection performance
type ReflectionMetrics struct {
	TotalReflections       int                      `json:"total_reflections"`
	AverageScore           float64                  `json:"average_score"`
	ImprovementRate        float64                  `json:"improvement_rate"`
	LearningsGenerated     int                      `json:"learnings_generated"`
	LearningsApplied       int                      `json:"learnings_applied"`
	SuccessfulApplications int                      `json:"successful_applications"`
	CategoryPerformance    map[string]float64       `json:"category_performance"`
	TimeMetrics            map[string]time.Duration `json:"time_metrics"`
	mu                     sync.RWMutex
}

// NewReflectionMetrics creates new reflection metrics
func NewReflectionMetrics() *ReflectionMetrics {
	return &ReflectionMetrics{
		CategoryPerformance: make(map[string]float64),
		TimeMetrics:         make(map[string]time.Duration),
	}
}

// RecordReflection records a reflection event
func (m *ReflectionMetrics) RecordReflection(result *ReflectionResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalReflections++

	// Update average score
	m.AverageScore = (m.AverageScore*float64(m.TotalReflections-1) + result.PerformanceScore) / float64(m.TotalReflections)

	// Count learnings
	m.LearningsGenerated += len(result.LearningPoints)

	// Update category performance
	for _, insight := range result.Insights {
		if currentScore, ok := m.CategoryPerformance[insight.Category]; ok {
			m.CategoryPerformance[insight.Category] = (currentScore + insight.Importance) / 2
		} else {
			m.CategoryPerformance[insight.Category] = insight.Importance
		}
	}
}

// RecordLearningApplication records when a learning is applied
func (m *ReflectionMetrics) RecordLearningApplication(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.LearningsApplied++
	if success {
		m.SuccessfulApplications++
	}
}

// GetImprovementRate calculates the improvement rate
func (m *ReflectionMetrics) GetImprovementRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.LearningsApplied == 0 {
		return 0
	}

	return float64(m.SuccessfulApplications) / float64(m.LearningsApplied)
}

// GetSummary returns a summary of reflection metrics
func (m *ReflectionMetrics) GetSummary() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"total_reflections":        m.TotalReflections,
		"average_score":            m.AverageScore,
		"improvement_rate":         m.GetImprovementRate(),
		"learnings_generated":      m.LearningsGenerated,
		"learnings_applied":        m.LearningsApplied,
		"application_success_rate": float64(m.SuccessfulApplications) / float64(max(m.LearningsApplied, 1)),
		"category_performance":     m.CategoryPerformance,
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
