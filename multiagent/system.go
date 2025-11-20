// Package multiagent provides multi-agent collaboration capabilities
package multiagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	loggercore "github.com/kart-io/logger/core"
)

// Role defines the role of an agent in collaboration
type Role string

const (
	RoleLeader      Role = "leader"
	RoleWorker      Role = "worker"
	RoleCoordinator Role = "coordinator"
	RoleSpecialist  Role = "specialist"
	RoleValidator   Role = "validator"
	RoleObserver    Role = "observer"
)

// CollaborationType defines the type of collaboration
type CollaborationType string

const (
	CollaborationTypeParallel     CollaborationType = "parallel"
	CollaborationTypeSequential   CollaborationType = "sequential"
	CollaborationTypeHierarchical CollaborationType = "hierarchical"
	CollaborationTypeConsensus    CollaborationType = "consensus"
	CollaborationTypePipeline     CollaborationType = "pipeline"
)

// Message represents a message between agents
type Message struct {
	ID        string                 `json:"id"`
	From      string                 `json:"from"`
	To        string                 `json:"to"`
	Type      MessageType            `json:"type"`
	Content   interface{}            `json:"content"`
	Priority  int                    `json:"priority"`
	Timestamp time.Time              `json:"timestamp"`
	ReplyTo   string                 `json:"reply_to,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// MessageType defines the type of message
type MessageType string

const (
	MessageTypeRequest      MessageType = "request"
	MessageTypeResponse     MessageType = "response"
	MessageTypeBroadcast    MessageType = "broadcast"
	MessageTypeNotification MessageType = "notification"
	MessageTypeCommand      MessageType = "command"
	MessageTypeReport       MessageType = "report"
	MessageTypeVote         MessageType = "vote"
)

// CollaborativeTask represents a task for multi-agent collaboration
type CollaborativeTask struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        CollaborationType      `json:"type"`
	Input       interface{}            `json:"input"`
	Output      interface{}            `json:"output,omitempty"`
	Status      TaskStatus             `json:"status"`
	Assignments map[string]Assignment  `json:"assignments"`
	Results     map[string]interface{} `json:"results"`
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Assignment represents an agent's assignment in a task
type Assignment struct {
	AgentID   string      `json:"agent_id"`
	Role      Role        `json:"role"`
	Subtask   interface{} `json:"subtask"`
	Status    TaskStatus  `json:"status"`
	Result    interface{} `json:"result,omitempty"`
	StartTime time.Time   `json:"start_time,omitempty"`
	EndTime   time.Time   `json:"end_time,omitempty"`
}

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusAssigned  TaskStatus = "assigned"
	TaskStatusExecuting TaskStatus = "executing"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// MultiAgentSystem manages multiple agents working together
type MultiAgentSystem struct {
	agents       map[string]CollaborativeAgent
	teams        map[string]*Team
	messageQueue chan Message
	tasks        map[string]*CollaborativeTask
	logger       loggercore.Logger
	mu           sync.RWMutex

	// Configuration
	maxAgents         int
	messageBufferSize int
	timeout           time.Duration
}

// CollaborativeAgent interface for agents that can collaborate
type CollaborativeAgent interface {
	core.Agent

	// GetRole returns the agent's role
	GetRole() Role

	// SetRole sets the agent's role
	SetRole(role Role)

	// ReceiveMessage handles incoming messages
	ReceiveMessage(ctx context.Context, message Message) error

	// SendMessage sends a message to another agent
	SendMessage(ctx context.Context, message Message) error

	// Collaborate participates in a collaborative task
	Collaborate(ctx context.Context, task *CollaborativeTask) (*Assignment, error)

	// Vote participates in consensus decision making
	Vote(ctx context.Context, proposal interface{}) (bool, error)
}

// Team represents a team of agents
type Team struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Leader       string                 `json:"leader"`
	Members      []string               `json:"members"`
	Purpose      string                 `json:"purpose"`
	Capabilities []string               `json:"capabilities"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// NewMultiAgentSystem creates a new multi-agent system
func NewMultiAgentSystem(log loggercore.Logger, opts ...SystemOption) *MultiAgentSystem {
	system := &MultiAgentSystem{
		agents:            make(map[string]CollaborativeAgent),
		teams:             make(map[string]*Team),
		messageQueue:      make(chan Message, 1000),
		tasks:             make(map[string]*CollaborativeTask),
		logger:            log,
		maxAgents:         100,
		messageBufferSize: 1000,
		timeout:           30 * time.Second,
	}

	for _, opt := range opts {
		opt(system)
	}

	// Start message router
	go system.routeMessages()

	return system
}

// SystemOption configures the multi-agent system
type SystemOption func(*MultiAgentSystem)

// WithMaxAgents sets the maximum number of agents
func WithMaxAgents(max int) SystemOption {
	return func(s *MultiAgentSystem) {
		s.maxAgents = max
	}
}

// WithTimeout sets the default timeout
func WithTimeout(timeout time.Duration) SystemOption {
	return func(s *MultiAgentSystem) {
		s.timeout = timeout
	}
}

// RegisterAgent registers an agent in the system
func (s *MultiAgentSystem) RegisterAgent(id string, agent CollaborativeAgent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.agents) >= s.maxAgents {
		return agentErrors.Newf(agentErrors.CodeMultiAgentRegistration, "maximum number of agents (%d) reached", s.maxAgents).
			WithComponent("multiagent_system").
			WithOperation("register_agent").
			WithContext("max_agents", s.maxAgents).
			WithContext("current_count", len(s.agents))
	}

	if _, exists := s.agents[id]; exists {
		return agentErrors.Newf(agentErrors.CodeMultiAgentRegistration, "agent %s already registered", id).
			WithComponent("multiagent_system").
			WithOperation("register_agent").
			WithContext("agent_id", id)
	}

	s.agents[id] = agent
	s.logger.Infow("Agent registered",
		"agent_id", id,
		"role", string(agent.GetRole()))

	return nil
}

// UnregisterAgent removes an agent from the system
func (s *MultiAgentSystem) UnregisterAgent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.agents[id]; !exists {
		return agentErrors.Newf(agentErrors.CodeAgentNotFound, "agent %s not found", id).
			WithComponent("multiagent_system").
			WithOperation("unregister_agent").
			WithContext("agent_id", id)
	}

	delete(s.agents, id)

	// Remove from teams
	for _, team := range s.teams {
		s.removeFromTeam(team, id)
	}

	s.logger.Infow("Agent unregistered", "agent_id", id)
	return nil
}

// CreateTeam creates a new team of agents
func (s *MultiAgentSystem) CreateTeam(team *Team) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.teams[team.ID]; exists {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "team %s already exists", team.ID).
			WithComponent("multiagent_system").
			WithOperation("create_team").
			WithContext("team_id", team.ID)
	}

	// Verify all members exist
	for _, memberID := range team.Members {
		if _, exists := s.agents[memberID]; !exists {
			return agentErrors.Newf(agentErrors.CodeAgentNotFound, "agent %s not found", memberID).
				WithComponent("multiagent_system").
				WithOperation("create_team").
				WithContext("team_id", team.ID).
				WithContext("missing_agent_id", memberID)
		}
	}

	// Verify leader exists and is a member
	if team.Leader != "" {
		if _, exists := s.agents[team.Leader]; !exists {
			return agentErrors.Newf(agentErrors.CodeAgentNotFound, "leader %s not found", team.Leader).
				WithComponent("multiagent_system").
				WithOperation("create_team").
				WithContext("team_id", team.ID).
				WithContext("leader_id", team.Leader)
		}
		// Set leader role
		s.agents[team.Leader].SetRole(RoleLeader)
	}

	s.teams[team.ID] = team
	s.logger.Infow("Team created",
		"team_id", team.ID,
		"name", team.Name,
		"members", len(team.Members))

	return nil
}

// ExecuteTask executes a collaborative task
func (s *MultiAgentSystem) ExecuteTask(ctx context.Context, task *CollaborativeTask) (*CollaborativeTask, error) {
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()

	task.Status = TaskStatusAssigned
	task.StartTime = time.Now()
	task.Results = make(map[string]interface{})

	s.logger.Infow("Starting collaborative task",
		"task_id", task.ID,
		"type", string(task.Type))

	// Execute based on collaboration type
	var err error
	switch task.Type {
	case CollaborationTypeParallel:
		err = s.executeParallelTask(ctx, task)
	case CollaborationTypeSequential:
		err = s.executeSequentialTask(ctx, task)
	case CollaborationTypeHierarchical:
		err = s.executeHierarchicalTask(ctx, task)
	case CollaborationTypeConsensus:
		err = s.executeConsensusTask(ctx, task)
	case CollaborationTypePipeline:
		err = s.executePipelineTask(ctx, task)
	default:
		err = agentErrors.Newf(agentErrors.CodeInvalidInput, "unknown collaboration type: %s", task.Type).
			WithComponent("multiagent_system").
			WithOperation("execute_task").
			WithContext("task_id", task.ID).
			WithContext("collaboration_type", string(task.Type))
	}

	task.EndTime = time.Now()

	if err != nil {
		task.Status = TaskStatusFailed
		s.logger.Errorw("Task failed",
			"task_id", task.ID,
			"error", err)
	} else {
		task.Status = TaskStatusCompleted
		s.logger.Infow("Task completed",
			"task_id", task.ID,
			"duration", task.EndTime.Sub(task.StartTime))
	}

	return task, err
}

// executeParallelTask executes tasks in parallel
func (s *MultiAgentSystem) executeParallelTask(ctx context.Context, task *CollaborativeTask) error {
	s.mu.RLock()
	agents := s.getAvailableAgents()
	s.mu.RUnlock()

	if len(agents) == 0 {
		return agentErrors.New(agentErrors.CodeAgentNotFound, "no available agents").
			WithComponent("multiagent_system").
			WithOperation("execute_parallel_task").
			WithContext("task_id", task.ID)
	}

	// Distribute subtasks to agents
	var wg sync.WaitGroup
	results := make(chan Assignment, len(agents))
	errors := make(chan error, len(agents))

	for agentID, agent := range agents {
		assignment := Assignment{
			AgentID:   agentID,
			Role:      agent.GetRole(),
			Subtask:   task.Input,
			Status:    TaskStatusExecuting,
			StartTime: time.Now(),
		}
		task.Assignments[agentID] = assignment

		wg.Add(1)
		go func(id string, a CollaborativeAgent) {
			defer wg.Done()

			result, err := a.Collaborate(ctx, task)
			if err != nil {
				errors <- err
				return
			}

			result.EndTime = time.Now()
			result.Status = TaskStatusCompleted
			results <- *result
		}(agentID, agent)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Collect results
	for result := range results {
		task.Results[result.AgentID] = result.Result
		task.Assignments[result.AgentID] = result
	}

	// Check for errors
	for err := range errors {
		if err != nil {
			return err
		}
	}

	return nil
}

// executeSequentialTask executes tasks sequentially
func (s *MultiAgentSystem) executeSequentialTask(ctx context.Context, task *CollaborativeTask) error {
	s.mu.RLock()
	agents := s.getAvailableAgentsOrdered()
	s.mu.RUnlock()

	if len(agents) == 0 {
		return agentErrors.New(agentErrors.CodeAgentNotFound, "no available agents").
			WithComponent("multiagent_system").
			WithOperation("execute_sequential_task").
			WithContext("task_id", task.ID)
	}

	// Execute in sequence
	previousOutput := task.Input

	for _, agentID := range agents {
		agent := s.agents[agentID]

		assignment := Assignment{
			AgentID:   agentID,
			Role:      agent.GetRole(),
			Subtask:   previousOutput,
			Status:    TaskStatusExecuting,
			StartTime: time.Now(),
		}

		// Create task with previous output as input
		sequentialTask := *task
		sequentialTask.Input = previousOutput

		result, err := agent.Collaborate(ctx, &sequentialTask)
		if err != nil {
			assignment.Status = TaskStatusFailed
			task.Assignments[agentID] = assignment
			return agentErrors.Wrapf(err, agentErrors.CodeAgentExecution, "agent %s failed", agentID).
				WithComponent("multiagent_system").
				WithOperation("execute_sequential_task").
				WithContext("task_id", task.ID).
				WithContext("agent_id", agentID)
		}

		result.EndTime = time.Now()
		result.Status = TaskStatusCompleted
		task.Assignments[agentID] = *result
		task.Results[agentID] = result.Result

		// Use this agent's output as next input
		previousOutput = result.Result
	}

	// Final output is the last agent's result
	task.Output = previousOutput

	return nil
}

// executeHierarchicalTask executes tasks in a hierarchical manner
func (s *MultiAgentSystem) executeHierarchicalTask(ctx context.Context, task *CollaborativeTask) error {
	s.mu.RLock()
	leader := s.findLeader()
	workers := s.findWorkers()
	s.mu.RUnlock()

	if leader == nil {
		return agentErrors.New(agentErrors.CodeAgentNotFound, "no leader agent available").
			WithComponent("multiagent_system").
			WithOperation("execute_hierarchical_task").
			WithContext("task_id", task.ID)
	}

	// Leader creates plan
	leaderTask := *task
	leaderResult, err := leader.Collaborate(ctx, &leaderTask)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "leader failed to plan").
			WithComponent("multiagent_system").
			WithOperation("execute_hierarchical_task").
			WithContext("task_id", task.ID)
	}

	// Distribute work to workers based on leader's plan
	plan, ok := leaderResult.Result.(map[string]interface{})
	if !ok {
		return agentErrors.New(agentErrors.CodeInvalidInput, "invalid plan from leader").
			WithComponent("multiagent_system").
			WithOperation("execute_hierarchical_task").
			WithContext("task_id", task.ID)
	}

	// Execute worker tasks in parallel
	var wg sync.WaitGroup
	workerResults := make(map[string]interface{})
	var mu sync.Mutex

	for workerID, worker := range workers {
		if subtask, exists := plan[workerID]; exists {
			wg.Add(1)
			go func(id string, agent CollaborativeAgent, work interface{}) {
				defer wg.Done()

				workerTask := *task
				workerTask.Input = work

				result, err := agent.Collaborate(ctx, &workerTask)
				if err != nil {
					s.logger.Errorw("Worker failed",
						"worker_id", id,
						"error", err)
					return
				}

				mu.Lock()
				workerResults[id] = result.Result
				mu.Unlock()
			}(workerID, worker, subtask)
		}
	}

	wg.Wait()

	// Leader validates and aggregates results
	validationTask := *task
	validationTask.Input = workerResults
	finalResult, err := leader.Collaborate(ctx, &validationTask)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "leader failed to validate results").
			WithComponent("multiagent_system").
			WithOperation("execute_hierarchical_task").
			WithContext("task_id", task.ID)
	}

	task.Output = finalResult.Result
	task.Results["final"] = finalResult.Result

	return nil
}

// executeConsensusTask executes tasks requiring consensus
func (s *MultiAgentSystem) executeConsensusTask(ctx context.Context, task *CollaborativeTask) error {
	s.mu.RLock()
	agents := s.getAvailableAgents()
	s.mu.RUnlock()

	if len(agents) < 3 {
		return agentErrors.New(agentErrors.CodeInvalidConfig, "consensus requires at least 3 agents").
			WithComponent("multiagent_system").
			WithOperation("execute_consensus_task").
			WithContext("task_id", task.ID).
			WithContext("available_agents", len(agents))
	}

	// Each agent votes on the proposal
	votes := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for agentID, agent := range agents {
		wg.Add(1)
		go func(id string, a CollaborativeAgent) {
			defer wg.Done()

			vote, err := a.Vote(ctx, task.Input)
			if err != nil {
				s.logger.Errorw("Agent failed to vote",
					"agent_id", id,
					"error", err)
				return
			}

			mu.Lock()
			votes[id] = vote
			mu.Unlock()
		}(agentID, agent)
	}

	wg.Wait()

	// Count votes
	yesVotes := 0
	for _, vote := range votes {
		if vote {
			yesVotes++
		}
	}

	// Check if consensus reached (simple majority)
	consensusThreshold := len(votes)/2 + 1
	consensusReached := yesVotes >= consensusThreshold

	task.Output = map[string]interface{}{
		"consensus_reached": consensusReached,
		"yes_votes":         yesVotes,
		"total_votes":       len(votes),
		"votes":             votes,
	}

	if !consensusReached {
		return agentErrors.Newf(agentErrors.CodeMultiAgentConsensus, "consensus not reached: %d/%d votes", yesVotes, len(votes)).
			WithComponent("multiagent_system").
			WithOperation("execute_consensus_task").
			WithContext("task_id", task.ID).
			WithContext("yes_votes", yesVotes).
			WithContext("total_votes", len(votes))
	}

	return nil
}

// executePipelineTask executes tasks in a pipeline
func (s *MultiAgentSystem) executePipelineTask(ctx context.Context, task *CollaborativeTask) error {
	// Similar to sequential but with defined stages
	pipeline, ok := task.Input.([]interface{})
	if !ok {
		return agentErrors.New(agentErrors.CodeInvalidInput, "pipeline task requires array of stages").
			WithComponent("multiagent_system").
			WithOperation("execute_pipeline_task").
			WithContext("task_id", task.ID)
	}

	s.mu.RLock()
	agents := s.getAvailableAgentsOrdered()
	s.mu.RUnlock()

	if len(agents) < len(pipeline) {
		return agentErrors.New(agentErrors.CodeInvalidConfig, "not enough agents for pipeline stages").
			WithComponent("multiagent_system").
			WithOperation("execute_pipeline_task").
			WithContext("task_id", task.ID).
			WithContext("available_agents", len(agents)).
			WithContext("required_stages", len(pipeline))
	}

	// Execute each stage
	var previousOutput interface{}
	for i, stage := range pipeline {
		if i >= len(agents) {
			break
		}

		agentID := agents[i]
		agent := s.agents[agentID]

		stageTask := *task
		stageTask.Input = map[string]interface{}{
			"stage":    stage,
			"previous": previousOutput,
		}

		result, err := agent.Collaborate(ctx, &stageTask)
		if err != nil {
			return agentErrors.Wrapf(err, agentErrors.CodeAgentExecution, "pipeline stage %d failed", i).
				WithComponent("multiagent_system").
				WithOperation("execute_pipeline_task").
				WithContext("task_id", task.ID).
				WithContext("stage_index", i).
				WithContext("agent_id", agentID)
		}

		previousOutput = result.Result
		task.Results[fmt.Sprintf("stage_%d", i)] = result.Result
	}

	task.Output = previousOutput
	return nil
}

// SendMessage sends a message between agents
func (s *MultiAgentSystem) SendMessage(message Message) error {
	select {
	case s.messageQueue <- message:
		return nil
	case <-time.After(s.timeout):
		return agentErrors.New(agentErrors.CodeMultiAgentMessage, "message queue full, timeout sending message").
			WithComponent("multiagent_system").
			WithOperation("send_message").
			WithContext("from", message.From).
			WithContext("to", message.To).
			WithContext("timeout", s.timeout.String())
	}
}

// routeMessages routes messages between agents
func (s *MultiAgentSystem) routeMessages() {
	for message := range s.messageQueue {
		s.mu.RLock()
		recipient, exists := s.agents[message.To]
		s.mu.RUnlock()

		if !exists {
			s.logger.Errorw("Recipient not found",
				"to", message.To,
				"from", message.From)
			continue
		}

		// NOTE: Using background context here is acceptable as this is a long-running
		// background goroutine for message routing. Each message should have its own
		// lifecycle independent of specific request contexts.
		ctx := context.Background()
		if err := recipient.ReceiveMessage(ctx, message); err != nil {
			s.logger.Errorw("Failed to deliver message",
				"to", message.To,
				"from", message.From,
				"error", err)
		}
	}
}

// Helper methods

func (s *MultiAgentSystem) getAvailableAgents() map[string]CollaborativeAgent {
	available := make(map[string]CollaborativeAgent)
	for id, agent := range s.agents {
		// Check if agent is not busy (simplified)
		available[id] = agent
	}
	return available
}

func (s *MultiAgentSystem) getAvailableAgentsOrdered() []string {
	ordered := make([]string, 0, len(s.agents))
	for id := range s.agents {
		ordered = append(ordered, id)
	}
	return ordered
}

func (s *MultiAgentSystem) findLeader() CollaborativeAgent {
	for _, agent := range s.agents {
		if agent.GetRole() == RoleLeader {
			return agent
		}
	}
	// If no leader, pick coordinator
	for _, agent := range s.agents {
		if agent.GetRole() == RoleCoordinator {
			return agent
		}
	}
	return nil
}

func (s *MultiAgentSystem) findWorkers() map[string]CollaborativeAgent {
	workers := make(map[string]CollaborativeAgent)
	for id, agent := range s.agents {
		if agent.GetRole() == RoleWorker {
			workers[id] = agent
		}
	}
	return workers
}

func (s *MultiAgentSystem) removeFromTeam(team *Team, agentID string) {
	newMembers := []string{}
	for _, member := range team.Members {
		if member != agentID {
			newMembers = append(newMembers, member)
		}
	}
	team.Members = newMembers

	if team.Leader == agentID {
		team.Leader = ""
	}
}
