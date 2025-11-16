package multiagent

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
)

// BaseCollaborativeAgent provides a base implementation of CollaborativeAgent
type BaseCollaborativeAgent struct {
	*core.BaseAgent
	role         Role
	messageBox   chan Message
	outbox       chan Message
	system       *MultiAgentSystem
	capabilities []string
	state        map[string]interface{}
	mu           sync.RWMutex
}

// NewBaseCollaborativeAgent creates a new base collaborative agent
func NewBaseCollaborativeAgent(id, description string, role Role, system *MultiAgentSystem) *BaseCollaborativeAgent {
	return &BaseCollaborativeAgent{
		BaseAgent:    core.NewBaseAgent(id, description, []string{}),
		role:         role,
		messageBox:   make(chan Message, 100),
		outbox:       make(chan Message, 100),
		system:       system,
		capabilities: []string{},
		state:        make(map[string]interface{}),
	}
}

// GetRole returns the agent's role
func (a *BaseCollaborativeAgent) GetRole() Role {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.role
}

// SetRole sets the agent's role
func (a *BaseCollaborativeAgent) SetRole(role Role) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.role = role
}

// ReceiveMessage handles incoming messages
func (a *BaseCollaborativeAgent) ReceiveMessage(ctx context.Context, message Message) error {
	select {
	case a.messageBox <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout receiving message")
	}
}

// SendMessage sends a message to another agent
func (a *BaseCollaborativeAgent) SendMessage(ctx context.Context, message Message) error {
	if a.system != nil {
		return a.system.SendMessage(message)
	}
	return fmt.Errorf("agent not connected to system")
}

// Collaborate participates in a collaborative task
func (a *BaseCollaborativeAgent) Collaborate(ctx context.Context, task *CollaborativeTask) (*Assignment, error) {
	assignment := &Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Subtask:   task.Input,
		Status:    TaskStatusExecuting,
		StartTime: time.Now(),
	}

	// Simulate work based on role
	result, err := a.executeRoleBasedTask(ctx, task)
	if err != nil {
		assignment.Status = TaskStatusFailed
		return assignment, err
	}

	assignment.Result = result
	assignment.Status = TaskStatusCompleted
	assignment.EndTime = time.Now()

	return assignment, nil
}

// Vote participates in consensus decision making
func (a *BaseCollaborativeAgent) Vote(ctx context.Context, proposal interface{}) (bool, error) {
	// Simple voting logic - can be overridden
	// Randomly vote yes or no for demonstration - using crypto/rand for security
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(1000))
	if err != nil {
		return false, fmt.Errorf("failed to generate random vote: %w", err)
	}
	return float64(n.Int64())/1000.0 > 0.3, nil
}

// Execute implements the Agent interface
func (a *BaseCollaborativeAgent) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	// Convert to collaborative task if needed
	task := &CollaborativeTask{
		ID:          fmt.Sprintf("task_%d", time.Now().Unix()),
		Name:        "Agent Execution",
		Description: "Direct agent execution",
		Type:        CollaborationTypeSequential,
		Input:       input.Context,
		Status:      TaskStatusExecuting,
		Assignments: make(map[string]Assignment),
	}

	assignment, err := a.Collaborate(ctx, task)
	if err != nil {
		return nil, err
	}

	return &core.AgentOutput{
		Result: assignment.Result,
		Status: "success",
		Metadata: map[string]interface{}{
			"role":     a.role,
			"agent_id": a.Name(),
		},
	}, nil
}

// executeRoleBasedTask executes task based on agent's role
func (a *BaseCollaborativeAgent) executeRoleBasedTask(ctx context.Context, task *CollaborativeTask) (interface{}, error) {
	switch a.role {
	case RoleLeader:
		return a.executeLeaderTask(ctx, task)
	case RoleWorker:
		return a.executeWorkerTask(ctx, task)
	case RoleCoordinator:
		return a.executeCoordinatorTask(ctx, task)
	case RoleSpecialist:
		return a.executeSpecialistTask(ctx, task)
	case RoleValidator:
		return a.executeValidatorTask(ctx, task)
	case RoleObserver:
		return a.executeObserverTask(ctx, task)
	default:
		return a.executeDefaultTask(ctx, task)
	}
}

func (a *BaseCollaborativeAgent) executeLeaderTask(ctx context.Context, task *CollaborativeTask) (interface{}, error) {
	// Leader creates plan and delegates
	plan := map[string]interface{}{
		"strategy": "divide_and_conquer",
		"phases":   []string{"analyze", "execute", "validate"},
		"assignments": map[string]string{
			"worker_1": "data_processing",
			"worker_2": "computation",
			"worker_3": "aggregation",
		},
	}
	return plan, nil
}

func (a *BaseCollaborativeAgent) executeWorkerTask(ctx context.Context, task *CollaborativeTask) (interface{}, error) {
	// Worker executes assigned work
	time.Sleep(100 * time.Millisecond) // Simulate work

	result := map[string]interface{}{
		"worker_id": a.Name(),
		"completed": true,
		"output":    fmt.Sprintf("Processed by %s", a.Name()),
	}
	return result, nil
}

func (a *BaseCollaborativeAgent) executeCoordinatorTask(ctx context.Context, task *CollaborativeTask) (interface{}, error) {
	// Coordinator manages communication and synchronization
	coordination := map[string]interface{}{
		"synchronized": true,
		"agents_ready": true,
		"next_phase":   "execution",
	}
	return coordination, nil
}

func (a *BaseCollaborativeAgent) executeSpecialistTask(ctx context.Context, task *CollaborativeTask) (interface{}, error) {
	// Specialist applies domain expertise
	analysis := map[string]interface{}{
		"specialist_id": a.Name(),
		"analysis":      "Domain-specific analysis completed",
		"recommendations": []string{
			"Optimize algorithm X",
			"Apply technique Y",
			"Consider approach Z",
		},
	}
	return analysis, nil
}

func (a *BaseCollaborativeAgent) executeValidatorTask(ctx context.Context, task *CollaborativeTask) (interface{}, error) {
	// Validator checks results
	validation := map[string]interface{}{
		"valid":      true,
		"confidence": 0.95,
		"issues":     []string{},
	}
	return validation, nil
}

func (a *BaseCollaborativeAgent) executeObserverTask(ctx context.Context, task *CollaborativeTask) (interface{}, error) {
	// Observer monitors and reports
	observation := map[string]interface{}{
		"observed_agents": len(task.Assignments),
		"status":          "monitoring",
		"metrics": map[string]interface{}{
			"avg_response_time": "150ms",
			"success_rate":      0.98,
		},
	}
	return observation, nil
}

func (a *BaseCollaborativeAgent) executeDefaultTask(ctx context.Context, task *CollaborativeTask) (interface{}, error) {
	// Default execution
	return map[string]interface{}{
		"agent_id": a.Name(),
		"role":     a.role,
		"result":   "Task completed",
	}, nil
}

// SpecializedAgent demonstrates a specialized collaborative agent
type SpecializedAgent struct {
	*BaseCollaborativeAgent
	specialization string
	expertise      []string
}

// NewSpecializedAgent creates a new specialized agent
func NewSpecializedAgent(id, specialization string, system *MultiAgentSystem) *SpecializedAgent {
	return &SpecializedAgent{
		BaseCollaborativeAgent: NewBaseCollaborativeAgent(
			id,
			fmt.Sprintf("Specialized agent in %s", specialization),
			RoleSpecialist,
			system,
		),
		specialization: specialization,
		expertise:      []string{},
	}
}

// Collaborate overrides base collaboration with specialized logic
func (a *SpecializedAgent) Collaborate(ctx context.Context, task *CollaborativeTask) (*Assignment, error) {
	assignment := &Assignment{
		AgentID:   a.Name(),
		Role:      a.GetRole(),
		Subtask:   task.Input,
		Status:    TaskStatusExecuting,
		StartTime: time.Now(),
	}

	// Apply specialization
	result := map[string]interface{}{
		"specialization": a.specialization,
		"analysis":       a.applyExpertise(task.Input),
		"confidence":     0.85,
	}

	assignment.Result = result
	assignment.Status = TaskStatusCompleted
	assignment.EndTime = time.Now()

	return assignment, nil
}

func (a *SpecializedAgent) applyExpertise(input interface{}) interface{} {
	// Apply domain-specific expertise
	return map[string]interface{}{
		"expert_opinion": fmt.Sprintf("%s analysis of input", a.specialization),
		"recommendations": []string{
			"Apply domain best practices",
			"Consider specialized algorithms",
			"Optimize for domain constraints",
		},
	}
}

// NegotiatingAgent demonstrates an agent that can negotiate
type NegotiatingAgent struct {
	*BaseCollaborativeAgent
	preferences        map[string]float64
	negotiationHistory []NegotiationRound
}

// NegotiationRound represents a round of negotiation
type NegotiationRound struct {
	Round    int                    `json:"round"`
	Proposal interface{}            `json:"proposal"`
	Offers   map[string]interface{} `json:"offers"`
	Accepted bool                   `json:"accepted"`
}

// NewNegotiatingAgent creates a new negotiating agent
func NewNegotiatingAgent(id string, system *MultiAgentSystem) *NegotiatingAgent {
	return &NegotiatingAgent{
		BaseCollaborativeAgent: NewBaseCollaborativeAgent(
			id,
			"Agent capable of negotiation",
			RoleWorker,
			system,
		),
		preferences:        make(map[string]float64),
		negotiationHistory: []NegotiationRound{},
	}
}

// Negotiate conducts negotiation with other agents
func (a *NegotiatingAgent) Negotiate(ctx context.Context, proposal interface{}, partners []string) (interface{}, error) {
	maxRounds := 5
	currentProposal := proposal

	for round := 1; round <= maxRounds; round++ {
		negotiationRound := NegotiationRound{
			Round:    round,
			Proposal: currentProposal,
			Offers:   make(map[string]interface{}),
		}

		// Send proposal to partners
		for _, partner := range partners {
			message := Message{
				ID:        fmt.Sprintf("nego_%d_%d", time.Now().Unix(), round),
				From:      a.Name(),
				To:        partner,
				Type:      MessageTypeRequest,
				Content:   currentProposal,
				Priority:  1,
				Timestamp: time.Now(),
			}

			if err := a.SendMessage(ctx, message); err != nil {
				return nil, fmt.Errorf("failed to send negotiation: %w", err)
			}
		}

		// Wait for responses
		responses := a.collectResponses(ctx, partners, 5*time.Second)

		// Evaluate responses
		if a.evaluateOffers(responses) {
			negotiationRound.Accepted = true
			a.negotiationHistory = append(a.negotiationHistory, negotiationRound)
			return currentProposal, nil
		}

		// Modify proposal based on feedback
		currentProposal = a.modifyProposal(currentProposal, responses)
		negotiationRound.Offers = responses
		a.negotiationHistory = append(a.negotiationHistory, negotiationRound)
	}

	return nil, fmt.Errorf("negotiation failed after %d rounds", maxRounds)
}

func (a *NegotiatingAgent) collectResponses(ctx context.Context, partners []string, timeout time.Duration) map[string]interface{} {
	responses := make(map[string]interface{})
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case msg := <-a.messageBox:
			if msg.Type == MessageTypeResponse {
				responses[msg.From] = msg.Content
				if len(responses) == len(partners) {
					return responses
				}
			}
		case <-timer.C:
			return responses
		case <-ctx.Done():
			return responses
		}
	}
}

func (a *NegotiatingAgent) evaluateOffers(offers map[string]interface{}) bool {
	// Simple evaluation - accept if majority agrees
	acceptCount := 0
	for _, offer := range offers {
		if accepted, ok := offer.(bool); ok && accepted {
			acceptCount++
		}
	}
	return acceptCount > len(offers)/2
}

func (a *NegotiatingAgent) modifyProposal(current interface{}, feedback map[string]interface{}) interface{} {
	// Modify proposal based on feedback
	// This is simplified - real implementation would be more sophisticated
	modified := map[string]interface{}{
		"original": current,
		"modified": true,
		"adjustments": []string{
			"Reduced requirements",
			"Increased incentives",
			"Added flexibility",
		},
	}
	return modified
}
