// Package reflection provides self-evaluation and improvement capabilities for agents
package reflection

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/utils/json"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

// ReflectionType represents the type of reflection
type ReflectionType string

const (
	ReflectionTypeSelfEvaluation     ReflectionType = "self_evaluation"
	ReflectionTypePerformanceReview  ReflectionType = "performance_review"
	ReflectionTypeStrategyAnalysis   ReflectionType = "strategy_analysis"
	ReflectionTypeLearningExtraction ReflectionType = "learning_extraction"
	ReflectionTypeErrorAnalysis      ReflectionType = "error_analysis"
)

// ReflectionResult represents the result of a reflection process
type ReflectionResult struct {
	ID               string                 `json:"id"`
	Type             ReflectionType         `json:"type"`
	Subject          string                 `json:"subject"`
	Timestamp        time.Time              `json:"timestamp"`
	Evaluation       *Evaluation            `json:"evaluation"`
	Insights         []Insight              `json:"insights"`
	Improvements     []Improvement          `json:"improvements"`
	LearningPoints   []LearningPoint        `json:"learning_points"`
	PerformanceScore float64                `json:"performance_score"`
	Confidence       float64                `json:"confidence"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// Evaluation represents an evaluation of performance
type Evaluation struct {
	Strengths     []string               `json:"strengths"`
	Weaknesses    []string               `json:"weaknesses"`
	Opportunities []string               `json:"opportunities"`
	Threats       []string               `json:"threats"`
	Score         float64                `json:"score"` // 0-1
	Details       map[string]interface{} `json:"details,omitempty"`
}

// Insight represents a key insight from reflection
type Insight struct {
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Importance  float64   `json:"importance"` // 0-1
	Evidence    []string  `json:"evidence,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// Improvement represents a suggested improvement
type Improvement struct {
	Description   string                 `json:"description"`
	Priority      int                    `json:"priority"` // 1-5
	Impact        float64                `json:"impact"`   // Expected impact 0-1
	Effort        float64                `json:"effort"`   // Required effort 0-1
	ActionItems   []string               `json:"action_items"`
	Prerequisites []string               `json:"prerequisites,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// LearningPoint represents something learned from experience
type LearningPoint struct {
	Lesson        string    `json:"lesson"`
	Context       string    `json:"context"`
	Category      string    `json:"category"`
	Applicability float64   `json:"applicability"` // How widely applicable 0-1
	Confidence    float64   `json:"confidence"`    // Confidence in the learning 0-1
	Examples      []string  `json:"examples,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// ReflectiveAgent interface for agents with reflection capabilities
type ReflectiveAgent interface {
	core.Agent

	// Reflect performs self-reflection
	Reflect(ctx context.Context, subject interface{}) (*ReflectionResult, error)

	// EvaluatePerformance evaluates recent performance
	EvaluatePerformance(ctx context.Context, metrics PerformanceMetrics) (*Evaluation, error)

	// ExtractLearnings extracts learnings from experiences
	ExtractLearnings(ctx context.Context, experiences []Experience) ([]LearningPoint, error)

	// GenerateImprovements generates improvement suggestions
	GenerateImprovements(ctx context.Context, evaluation *Evaluation) ([]Improvement, error)

	// ApplyLearnings applies learnings to improve behavior
	ApplyLearnings(ctx context.Context, learnings []LearningPoint) error
}

// PerformanceMetrics contains performance metrics for evaluation
type PerformanceMetrics struct {
	SuccessRate     float64                `json:"success_rate"`
	AverageTime     time.Duration          `json:"average_time"`
	ErrorRate       float64                `json:"error_rate"`
	TotalExecutions int                    `json:"total_executions"`
	ResourceUsage   map[string]float64     `json:"resource_usage"`
	CustomMetrics   map[string]interface{} `json:"custom_metrics,omitempty"`
}

// Experience represents an experience to learn from
type Experience struct {
	ID          string                 `json:"id"`
	Description string                 `json:"description"`
	Input       interface{}            `json:"input"`
	Output      interface{}            `json:"output"`
	Success     bool                   `json:"success"`
	Duration    time.Duration          `json:"duration"`
	Error       string                 `json:"error,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
}

// SelfReflectiveAgent implements reflection capabilities
type SelfReflectiveAgent struct {
	*core.BaseAgent
	llmClient         llm.Client
	memory            memory.EnhancedMemory
	reflectionHistory []*ReflectionResult
	learningModel     *LearningModel
	mu                sync.RWMutex

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup

	// Configuration
	reflectionInterval time.Duration
	minExperiences     int
	learningThreshold  float64
}

// NewSelfReflectiveAgent creates a new self-reflective agent
func NewSelfReflectiveAgent(llmClient llm.Client, mem memory.EnhancedMemory, opts ...ReflectionOption) *SelfReflectiveAgent {
	ctx, cancel := context.WithCancel(context.Background())
	agent := &SelfReflectiveAgent{
		BaseAgent:          core.NewBaseAgent("self_reflective", "Agent with self-reflection capabilities", []string{"reflection", "learning", "self-improvement"}),
		llmClient:          llmClient,
		memory:             mem,
		reflectionHistory:  make([]*ReflectionResult, 0),
		learningModel:      NewLearningModel(),
		ctx:                ctx,
		cancel:             cancel,
		done:               make(chan struct{}),
		reflectionInterval: 1 * time.Hour,
		minExperiences:     10,
		learningThreshold:  0.7,
	}

	for _, opt := range opts {
		opt(agent)
	}

	// Start background reflection with proper lifecycle management
	agent.wg.Add(1)
	go agent.backgroundReflection()

	return agent
}

// ReflectionOption configures the reflective agent
type ReflectionOption func(*SelfReflectiveAgent)

// WithReflectionInterval sets the reflection interval
func WithReflectionInterval(interval time.Duration) ReflectionOption {
	return func(a *SelfReflectiveAgent) {
		a.reflectionInterval = interval
	}
}

// WithLearningThreshold sets the learning threshold
func WithLearningThreshold(threshold float64) ReflectionOption {
	return func(a *SelfReflectiveAgent) {
		a.learningThreshold = threshold
	}
}

// Shutdown gracefully shuts down the agent
func (a *SelfReflectiveAgent) Shutdown(ctx context.Context) error {
	// Signal shutdown
	a.cancel()
	close(a.done)

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Execute implements the Agent interface with reflection
func (a *SelfReflectiveAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	startTime := time.Now()

	// Record the experience
	experience := &Experience{
		ID:        fmt.Sprintf("exp_%d", time.Now().Unix()),
		Input:     input.Context,
		Timestamp: startTime,
		Context:   input.Context,
	}

	// Execute the actual task
	output, err := a.executeTask(ctx, input)

	// Complete the experience record
	experience.Duration = time.Since(startTime)
	experience.Success = err == nil
	if err != nil {
		experience.Error = err.Error()
	} else if output != nil {
		experience.Output = output.Result
	}

	// Store experience in memory
	a.storeExperience(ctx, experience)

	// Trigger reflection if enough experiences
	if a.shouldReflect() {
		go a.performReflection(ctx)
	}

	return output, err
}

// Reflect performs self-reflection on a subject
func (a *SelfReflectiveAgent) Reflect(ctx context.Context, subject interface{}) (*ReflectionResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Prepare reflection prompt
	prompt := a.buildReflectionPrompt(subject)

	// Use LLM for reflection
	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	}
	response, err := a.llmClient.Complete(ctx, req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "reflection failed").
			WithComponent("reflective_agent").
			WithOperation("reflect")
	}

	// Parse reflection result
	result := a.parseReflectionResult(response.Content, subject)

	// Store in history
	a.reflectionHistory = append(a.reflectionHistory, result)

	// Store in memory
	_ = a.memory.StoreTyped(ctx, result.ID, result, memory.MemoryTypeSemantic, memory.StoreOptions{
		Tags: []string{"reflection", string(result.Type)},
	})

	return result, nil
}

// EvaluatePerformance evaluates recent performance
func (a *SelfReflectiveAgent) EvaluatePerformance(ctx context.Context, metrics PerformanceMetrics) (*Evaluation, error) {
	// Build evaluation prompt
	prompt := fmt.Sprintf(`Evaluate the following performance metrics:

Success Rate: %.2f%%
Average Time: %s
Error Rate: %.2f%%
Total Executions: %d

Please provide:
1. Strengths (what's working well)
2. Weaknesses (areas needing improvement)
3. Opportunities (potential improvements)
4. Threats (risks or concerns)
5. Overall score (0-1)`,
		metrics.SuccessRate*100,
		metrics.AverageTime,
		metrics.ErrorRate*100,
		metrics.TotalExecutions,
	)

	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	}
	response, err := a.llmClient.Complete(ctx, req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "evaluation failed").
			WithComponent("reflective_agent").
			WithOperation("evaluate_performance")
	}

	// Parse evaluation
	evaluation := a.parseEvaluation(response.Content, metrics)

	return evaluation, nil
}

// ExtractLearnings extracts learnings from experiences
func (a *SelfReflectiveAgent) ExtractLearnings(ctx context.Context, experiences []Experience) ([]LearningPoint, error) {
	if len(experiences) < a.minExperiences {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "insufficient experiences for learning extraction").
			WithComponent("reflective_agent").
			WithOperation("extract_learnings").
			WithContext("experiences_count", len(experiences)).
			WithContext("min_required", a.minExperiences)
	}

	// Analyze patterns in experiences
	patterns := a.analyzeExperiencePatterns(experiences)

	// Extract learnings using LLM
	prompt := a.buildLearningExtractionPrompt(experiences, patterns)

	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	}
	response, err := a.llmClient.Complete(ctx, req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "learning extraction failed").
			WithComponent("reflective_agent").
			WithOperation("extract_learnings")
	}

	// Parse learnings
	learnings := a.parseLearnings(response.Content, experiences)

	// Update learning model
	a.learningModel.UpdateWithLearnings(learnings)

	// Store learnings in memory
	for _, learning := range learnings {
		_ = a.memory.StoreTyped(ctx,
			fmt.Sprintf("learning_%d", time.Now().UnixNano()),
			learning,
			memory.MemoryTypeSemantic,
			memory.StoreOptions{
				Tags: []string{"learning", learning.Category},
			},
		)
	}

	return learnings, nil
}

// GenerateImprovements generates improvement suggestions
func (a *SelfReflectiveAgent) GenerateImprovements(ctx context.Context, evaluation *Evaluation) ([]Improvement, error) {
	// Build improvement generation prompt
	prompt := fmt.Sprintf(`Based on the following evaluation, suggest improvements:

Strengths: %v
Weaknesses: %v
Opportunities: %v
Threats: %v

Generate specific, actionable improvements with:
1. Clear description
2. Priority (1-5)
3. Expected impact (0-1)
4. Required effort (0-1)
5. Action items`,
		evaluation.Strengths,
		evaluation.Weaknesses,
		evaluation.Opportunities,
		evaluation.Threats,
	)

	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	}
	response, err := a.llmClient.Complete(ctx, req)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "improvement generation failed").
			WithComponent("reflective_agent").
			WithOperation("generate_improvements")
	}

	// Parse improvements
	improvements := a.parseImprovements(response.Content, evaluation)

	// Prioritize improvements
	a.prioritizeImprovements(improvements)

	return improvements, nil
}

// ApplyLearnings applies learnings to improve behavior
func (a *SelfReflectiveAgent) ApplyLearnings(ctx context.Context, learnings []LearningPoint) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Update learning model
	for _, learning := range learnings {
		if learning.Confidence >= a.learningThreshold {
			a.learningModel.AddLearning(learning)
		}
	}

	// Store learnings in memory for future reference
	for _, learning := range learnings {
		key := fmt.Sprintf("applied_learning_%d", time.Now().UnixNano())
		_ = a.memory.StoreTyped(ctx, key, learning, memory.MemoryTypeProcedural, memory.StoreOptions{
			Tags: []string{"applied_learning", learning.Category},
		})
	}

	return nil
}

// Helper methods

func (a *SelfReflectiveAgent) executeTask(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// This would contain the actual task execution logic
	// For demonstration, we'll simulate task execution

	// Check if we can apply previous learnings
	relevantLearnings := a.learningModel.GetRelevantLearnings(input.Context)

	// Adjust strategy based on learnings
	if len(relevantLearnings) > 0 {
		if input.Context == nil {
			input.Context = make(map[string]interface{})
		}
		input.Context["learnings_applied"] = len(relevantLearnings)
	}

	// Simulate task execution
	output := &core.AgentOutput{
		Result: map[string]interface{}{
			"result":            "Task completed",
			"learnings_applied": len(relevantLearnings),
		},
		Status: "success",
		Metadata: map[string]interface{}{
			"reflection_enabled": true,
		},
	}

	return output, nil
}

func (a *SelfReflectiveAgent) storeExperience(ctx context.Context, experience *Experience) {
	// Store in memory
	_ = a.memory.StoreTyped(ctx, experience.ID, experience, memory.MemoryTypeEpisodic, memory.StoreOptions{
		Tags: []string{"experience"},
	})
}

func (a *SelfReflectiveAgent) shouldReflect() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Check if we have enough experiences
	experiences, _ := a.memory.GetByType(context.Background(), memory.MemoryTypeEpisodic, 100)
	return len(experiences) >= a.minExperiences
}

func (a *SelfReflectiveAgent) performReflection(ctx context.Context) {
	// Get recent experiences
	experiences, _ := a.memory.GetByType(ctx, memory.MemoryTypeEpisodic, 50)

	// Convert to Experience objects
	var exps []Experience
	for _, memEntry := range experiences {
		if exp, ok := memEntry.Content.(Experience); ok {
			exps = append(exps, exp)
		}
	}

	// Extract learnings
	learnings, err := a.ExtractLearnings(ctx, exps)
	if err == nil && len(learnings) > 0 {
		// Apply learnings
		_ = a.ApplyLearnings(ctx, learnings)
	}

	// Perform self-reflection
	reflection, err := a.Reflect(ctx, exps)
	if err == nil && reflection.PerformanceScore < 0.8 {
		// Generate improvements if performance is below threshold
		improvements, _ := a.GenerateImprovements(ctx, reflection.Evaluation)
		reflection.Improvements = improvements
	}
}

func (a *SelfReflectiveAgent) backgroundReflection() {
	defer a.wg.Done()
	ticker := time.NewTicker(a.reflectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return // Clean shutdown
		case <-ticker.C:
			a.performReflection(a.ctx)
		}
	}
}

func (a *SelfReflectiveAgent) buildReflectionPrompt(subject interface{}) string {
	data, _ := json.MarshalIndent(subject, "", "  ")
	return fmt.Sprintf(`Perform self-reflection on the following subject:

%s

Provide:
1. Overall evaluation (strengths, weaknesses, opportunities, threats)
2. Key insights discovered
3. Suggested improvements
4. Learning points for future
5. Performance score (0-1)
6. Confidence level (0-1)`, string(data))
}

func (a *SelfReflectiveAgent) parseReflectionResult(content string, subject interface{}) *ReflectionResult {
	// In production, this would parse structured LLM output
	result := &ReflectionResult{
		ID:        fmt.Sprintf("reflection_%d", time.Now().Unix()),
		Type:      ReflectionTypeSelfEvaluation,
		Subject:   fmt.Sprintf("%v", subject),
		Timestamp: time.Now(),
		Evaluation: &Evaluation{
			Strengths:  []string{"Consistent performance", "Good error handling"},
			Weaknesses: []string{"Could be faster", "Memory usage high"},
			Score:      0.75,
		},
		Insights: []Insight{
			{
				Description: "Pattern recognition improves with more examples",
				Category:    "learning",
				Importance:  0.8,
				Timestamp:   time.Now(),
			},
		},
		PerformanceScore: 0.75,
		Confidence:       0.85,
		Metadata:         make(map[string]interface{}),
	}

	return result
}

func (a *SelfReflectiveAgent) parseEvaluation(content string, metrics PerformanceMetrics) *Evaluation {
	// Simplified parsing - in production would parse LLM output
	eval := &Evaluation{
		Strengths:  []string{"High success rate", "Consistent performance"},
		Weaknesses: []string{"Response time could be improved"},
		Score:      metrics.SuccessRate,
		Details:    make(map[string]interface{}),
	}

	if metrics.ErrorRate > 0.1 {
		eval.Threats = append(eval.Threats, "High error rate needs attention")
	}

	return eval
}

func (a *SelfReflectiveAgent) analyzeExperiencePatterns(experiences []Experience) map[string]interface{} {
	patterns := make(map[string]interface{})

	// Analyze success patterns
	successCount := 0
	totalDuration := time.Duration(0)

	for _, exp := range experiences {
		if exp.Success {
			successCount++
		}
		totalDuration += exp.Duration
	}

	patterns["success_rate"] = float64(successCount) / float64(len(experiences))
	patterns["average_duration"] = totalDuration / time.Duration(len(experiences))

	return patterns
}

func (a *SelfReflectiveAgent) buildLearningExtractionPrompt(experiences []Experience, patterns map[string]interface{}) string {
	return fmt.Sprintf(`Analyze the following experiences and patterns to extract learnings:

Number of experiences: %d
Success rate: %.2f%%
Average duration: %v

Extract key learnings that can be applied to future tasks.
Focus on patterns, successful strategies, and common failure modes.`,
		len(experiences),
		patterns["success_rate"].(float64)*100,
		patterns["average_duration"],
	)
}

func (a *SelfReflectiveAgent) parseLearnings(content string, experiences []Experience) []LearningPoint {
	// Simplified parsing - in production would parse structured LLM output
	learnings := []LearningPoint{
		{
			Lesson:        "Early validation prevents downstream errors",
			Context:       "Task execution",
			Category:      "validation",
			Applicability: 0.9,
			Confidence:    0.85,
			Timestamp:     time.Now(),
		},
		{
			Lesson:        "Caching frequently accessed data improves performance",
			Context:       "Performance optimization",
			Category:      "optimization",
			Applicability: 0.8,
			Confidence:    0.9,
			Timestamp:     time.Now(),
		},
	}

	return learnings
}

func (a *SelfReflectiveAgent) parseImprovements(content string, evaluation *Evaluation) []Improvement {
	// Simplified parsing - in production would parse structured LLM output
	improvements := []Improvement{
		{
			Description: "Implement result caching for repeated queries",
			Priority:    2,
			Impact:      0.7,
			Effort:      0.3,
			ActionItems: []string{
				"Design cache key strategy",
				"Implement cache layer",
				"Add cache invalidation logic",
			},
		},
		{
			Description: "Add parallel processing for independent tasks",
			Priority:    1,
			Impact:      0.8,
			Effort:      0.5,
			ActionItems: []string{
				"Identify parallelizable tasks",
				"Implement concurrent execution",
				"Add synchronization mechanisms",
			},
		},
	}

	return improvements
}

func (a *SelfReflectiveAgent) prioritizeImprovements(improvements []Improvement) {
	// Sort by priority and impact/effort ratio
	for i := range improvements {
		if improvements[i].Effort > 0 {
			// Calculate ROI (Return on Investment)
			roi := improvements[i].Impact / improvements[i].Effort
			improvements[i].Metadata = map[string]interface{}{
				"roi": roi,
			}
		}
	}
}
