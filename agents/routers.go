package agents

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
)

// LLMRouter uses an LLM to route tasks to agents
type LLMRouter struct {
	llm              llm.Client
	capabilities     map[string][]string
	performanceCache map[string]float64
	mu               sync.RWMutex
}

// NewLLMRouter creates a new LLM-based router
func NewLLMRouter(llmClient llm.Client) *LLMRouter {
	return &LLMRouter{
		llm:              llmClient,
		capabilities:     make(map[string][]string),
		performanceCache: make(map[string]float64),
	}
}

// Route uses LLM to decide which agent should handle the task
func (r *LLMRouter) Route(ctx context.Context, task Task, agents map[string]core.Agent) (string, error) {
	// Build agent descriptions
	agentDescriptions := r.buildAgentDescriptions(agents)

	// Create routing prompt
	prompt := fmt.Sprintf(`
		Task: %s
		Description: %s
		Type: %s
		Priority: %d

		Available Agents:
		%s

		Which agent is best suited for this task? Return only the agent name.
	`, task.ID, task.Description, task.Type, task.Priority, agentDescriptions)

	// Call LLM
	response, err := r.llm.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.SystemMessage("You are an expert at routing tasks to specialized agents."),
			llm.UserMessage(prompt),
		},
		Temperature: 0.3, // Lower temperature for more consistent routing
		MaxTokens:   50,
	})
	if err != nil {
		return "", fmt.Errorf("LLM routing failed: %w", err)
	}

	// Parse agent name from response
	agentName := strings.TrimSpace(response.Content)

	// Validate agent exists
	if _, exists := agents[agentName]; !exists {
		// Fallback to first available agent
		for name := range agents {
			return name, nil
		}
		return "", fmt.Errorf("no suitable agent found")
	}

	return agentName, nil
}

// buildAgentDescriptions creates descriptions of available agents
func (r *LLMRouter) buildAgentDescriptions(agents map[string]core.Agent) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	descriptions := []string{}
	for name := range agents {
		caps := r.capabilities[name]
		perf := r.performanceCache[name]

		desc := fmt.Sprintf("- %s", name)
		if len(caps) > 0 {
			desc += fmt.Sprintf(" (capabilities: %s)", strings.Join(caps, ", "))
		}
		if perf > 0 {
			desc += fmt.Sprintf(" [performance: %.2f]", perf)
		}

		descriptions = append(descriptions, desc)
	}

	return strings.Join(descriptions, "\n")
}

// GetCapabilities returns the capabilities of an agent
func (r *LLMRouter) GetCapabilities(agentName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilities[agentName]
}

// UpdateRouting updates the performance score for an agent
func (r *LLMRouter) UpdateRouting(agentName string, performance float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Use exponential moving average
	alpha := 0.3
	if current, exists := r.performanceCache[agentName]; exists {
		r.performanceCache[agentName] = alpha*performance + (1-alpha)*current
	} else {
		r.performanceCache[agentName] = performance
	}
}

// SetCapabilities sets the capabilities for an agent
func (r *LLMRouter) SetCapabilities(agentName string, capabilities []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities[agentName] = capabilities
}

// RuleBasedRouter uses predefined rules to route tasks
type RuleBasedRouter struct {
	rules        []RoutingRule
	capabilities map[string][]string
	performance  map[string]float64
	mu           sync.RWMutex
}

// RoutingRule defines a routing rule
type RoutingRule struct {
	Condition func(Task) bool
	AgentName string
	Priority  int
}

// NewRuleBasedRouter creates a new rule-based router
func NewRuleBasedRouter() *RuleBasedRouter {
	return &RuleBasedRouter{
		rules:        []RoutingRule{},
		capabilities: make(map[string][]string),
		performance:  make(map[string]float64),
	}
}

// AddRule adds a routing rule
func (r *RuleBasedRouter) AddRule(rule RoutingRule) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rules = append(r.rules, rule)

	// Sort rules by priority
	for i := len(r.rules) - 1; i > 0; i-- {
		if r.rules[i].Priority > r.rules[i-1].Priority {
			r.rules[i], r.rules[i-1] = r.rules[i-1], r.rules[i]
		} else {
			break
		}
	}
}

// Route uses rules to determine the appropriate agent
func (r *RuleBasedRouter) Route(ctx context.Context, task Task, agents map[string]core.Agent) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check each rule in priority order
	for _, rule := range r.rules {
		if rule.Condition(task) {
			// Check if agent exists
			if _, exists := agents[rule.AgentName]; exists {
				return rule.AgentName, nil
			}
		}
	}

	// Default to first available agent
	for name := range agents {
		return name, nil
	}

	return "", fmt.Errorf("no agents available")
}

// GetCapabilities returns the capabilities of an agent
func (r *RuleBasedRouter) GetCapabilities(agentName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilities[agentName]
}

// UpdateRouting updates the performance score for an agent
func (r *RuleBasedRouter) UpdateRouting(agentName string, performance float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.performance[agentName] = performance
}

// RoundRobinRouter distributes tasks evenly across agents
type RoundRobinRouter struct {
	counter      uint64
	capabilities map[string][]string
	performance  map[string]float64
	mu           sync.RWMutex
}

// NewRoundRobinRouter creates a new round-robin router
func NewRoundRobinRouter() *RoundRobinRouter {
	return &RoundRobinRouter{
		capabilities: make(map[string][]string),
		performance:  make(map[string]float64),
	}
}

// Route selects the next agent in round-robin fashion
func (r *RoundRobinRouter) Route(ctx context.Context, task Task, agents map[string]core.Agent) (string, error) {
	if len(agents) == 0 {
		return "", fmt.Errorf("no agents available")
	}

	// Get agent names
	agentNames := make([]string, 0, len(agents))
	for name := range agents {
		agentNames = append(agentNames, name)
	}

	// Select next agent
	index := atomic.AddUint64(&r.counter, 1) % uint64(len(agentNames))
	return agentNames[index], nil
}

// GetCapabilities returns the capabilities of an agent
func (r *RoundRobinRouter) GetCapabilities(agentName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilities[agentName]
}

// UpdateRouting updates the performance score for an agent
func (r *RoundRobinRouter) UpdateRouting(agentName string, performance float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.performance[agentName] = performance
}

// CapabilityRouter routes based on agent capabilities
type CapabilityRouter struct {
	capabilities map[string][]string
	performance  map[string]float64
	taskMatchers map[string]func(Task) float64 // Returns match score
	mu           sync.RWMutex
}

// NewCapabilityRouter creates a new capability-based router
func NewCapabilityRouter() *CapabilityRouter {
	return &CapabilityRouter{
		capabilities: make(map[string][]string),
		performance:  make(map[string]float64),
		taskMatchers: make(map[string]func(Task) float64),
	}
}

// RegisterAgent registers an agent with its capabilities and matcher
func (r *CapabilityRouter) RegisterAgent(name string, capabilities []string, matcher func(Task) float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.capabilities[name] = capabilities
	r.taskMatchers[name] = matcher
}

// Route selects the agent with the best capability match
func (r *CapabilityRouter) Route(ctx context.Context, task Task, agents map[string]core.Agent) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	bestAgent := ""
	bestScore := 0.0

	// Find the agent with the highest match score
	for name := range agents {
		if matcher, exists := r.taskMatchers[name]; exists {
			score := matcher(task)

			// Apply performance modifier
			if perf, exists := r.performance[name]; exists {
				score *= (0.7 + 0.3*perf) // Performance affects 30% of score
			}

			if score > bestScore {
				bestScore = score
				bestAgent = name
			}
		}
	}

	if bestAgent != "" {
		return bestAgent, nil
	}

	// Fallback to random selection
	for name := range agents {
		return name, nil
	}

	return "", fmt.Errorf("no agents available")
}

// GetCapabilities returns the capabilities of an agent
func (r *CapabilityRouter) GetCapabilities(agentName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilities[agentName]
}

// UpdateRouting updates the performance score for an agent
func (r *CapabilityRouter) UpdateRouting(agentName string, performance float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.performance[agentName] = performance
}

// LoadBalancingRouter distributes tasks based on current load
type LoadBalancingRouter struct {
	activeTaskCount  map[string]int32
	maxTasksPerAgent int32
	capabilities     map[string][]string
	performance      map[string]float64
	mu               sync.RWMutex
}

// NewLoadBalancingRouter creates a new load-balancing router
func NewLoadBalancingRouter(maxTasksPerAgent int32) *LoadBalancingRouter {
	return &LoadBalancingRouter{
		activeTaskCount:  make(map[string]int32),
		maxTasksPerAgent: maxTasksPerAgent,
		capabilities:     make(map[string][]string),
		performance:      make(map[string]float64),
	}
}

// Route selects the agent with the lowest current load
func (r *LoadBalancingRouter) Route(ctx context.Context, task Task, agents map[string]core.Agent) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	leastLoadedAgent := ""
	minLoad := r.maxTasksPerAgent + 1

	for name := range agents {
		load := r.activeTaskCount[name]
		if load < minLoad && load < r.maxTasksPerAgent {
			minLoad = load
			leastLoadedAgent = name
		}
	}

	if leastLoadedAgent != "" {
		count := r.activeTaskCount[leastLoadedAgent]
		r.activeTaskCount[leastLoadedAgent] = count + 1
		return leastLoadedAgent, nil
	}

	// All agents at max capacity
	return "", fmt.Errorf("all agents at maximum capacity")
}

// ReleaseTask decrements the task count for an agent
func (r *LoadBalancingRouter) ReleaseTask(agentName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if count, exists := r.activeTaskCount[agentName]; exists && count > 0 {
		r.activeTaskCount[agentName] = count - 1
	}
}

// GetCapabilities returns the capabilities of an agent
func (r *LoadBalancingRouter) GetCapabilities(agentName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilities[agentName]
}

// UpdateRouting updates the performance score for an agent
func (r *LoadBalancingRouter) UpdateRouting(agentName string, performance float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.performance[agentName] = performance
}

// GetLoad returns the current load for an agent
func (r *LoadBalancingRouter) GetLoad(agentName string) int32 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeTaskCount[agentName]
}

// HybridRouter combines multiple routing strategies
type HybridRouter struct {
	strategies []struct {
		router AgentRouter
		weight float64
	}
	fallback     AgentRouter
	capabilities map[string][]string
	mu           sync.RWMutex
}

// NewHybridRouter creates a new hybrid router
func NewHybridRouter(fallback AgentRouter) *HybridRouter {
	return &HybridRouter{
		strategies: []struct {
			router AgentRouter
			weight float64
		}{},
		fallback:     fallback,
		capabilities: make(map[string][]string),
	}
}

// AddStrategy adds a routing strategy with a weight
func (h *HybridRouter) AddStrategy(router AgentRouter, weight float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.strategies = append(h.strategies, struct {
		router AgentRouter
		weight float64
	}{router: router, weight: weight})
}

// Route uses weighted voting from multiple strategies
func (h *HybridRouter) Route(ctx context.Context, task Task, agents map[string]core.Agent) (string, error) {
	if len(h.strategies) == 0 {
		return h.fallback.Route(ctx, task, agents)
	}

	// Collect votes from each strategy
	votes := make(map[string]float64)

	for _, strategy := range h.strategies {
		agentName, err := strategy.router.Route(ctx, task, agents)
		if err == nil {
			votes[agentName] += strategy.weight
		}
	}

	// Find the agent with the most votes
	var bestAgent string
	maxVotes := 0.0

	for agent, voteCount := range votes {
		if voteCount > maxVotes {
			maxVotes = voteCount
			bestAgent = agent
		}
	}

	if bestAgent != "" {
		return bestAgent, nil
	}

	// Fallback if no consensus
	return h.fallback.Route(ctx, task, agents)
}

// GetCapabilities returns the capabilities of an agent
func (h *HybridRouter) GetCapabilities(agentName string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.capabilities[agentName]
}

// UpdateRouting updates routing information across all strategies
func (h *HybridRouter) UpdateRouting(agentName string, performance float64) {
	for _, strategy := range h.strategies {
		strategy.router.UpdateRouting(agentName, performance)
	}
	h.fallback.UpdateRouting(agentName, performance)
}

// RandomRouter randomly selects an agent
type RandomRouter struct {
	capabilities map[string][]string
	performance  map[string]float64
	mu           sync.RWMutex
}

// NewRandomRouter creates a new random router
func NewRandomRouter() *RandomRouter {
	return &RandomRouter{
		capabilities: make(map[string][]string),
		performance:  make(map[string]float64),
	}
}

// Route randomly selects an agent
func (r *RandomRouter) Route(ctx context.Context, task Task, agents map[string]core.Agent) (string, error) {
	if len(agents) == 0 {
		return "", fmt.Errorf("no agents available")
	}

	// Convert to slice for random selection
	agentNames := make([]string, 0, len(agents))
	for name := range agents {
		agentNames = append(agentNames, name)
	}

	// Random selection using crypto/rand for security
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(agentNames))))
	if err != nil {
		return "", fmt.Errorf("failed to generate random number: %w", err)
	}
	return agentNames[n.Int64()], nil
}

// GetCapabilities returns the capabilities of an agent
func (r *RandomRouter) GetCapabilities(agentName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilities[agentName]
}

// UpdateRouting updates the performance score for an agent
func (r *RandomRouter) UpdateRouting(agentName string, performance float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.performance[agentName] = performance
}
