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

// PipelineStage 定义 Pipeline 任务的阶段结构
// 支持更直观的阶段配置，包含名称、描述和配置参数
type PipelineStage struct {
	Name        string                 `json:"name"`                  // 阶段名称
	Description string                 `json:"description,omitempty"` // 阶段描述
	Config      map[string]interface{} `json:"config,omitempty"`      // 阶段配置参数
}

// MultiAgentSystem manages multiple agents working together
type MultiAgentSystem struct {
	agents       map[string]CollaborativeAgent
	agentOrder   []string // 记录 Agent 注册顺序，保证顺序任务执行顺序确定
	teams        map[string]*Team
	messageQueue chan Message
	tasks        map[string]*CollaborativeTask
	logger       loggercore.Logger
	mu           sync.RWMutex

	// Lifecycle management
	done   chan struct{}  // 关闭信号通道
	wg     sync.WaitGroup // 等待后台 goroutine 完成
	closed bool           // 标记系统是否已关闭

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
		agentOrder:        make([]string, 0),
		teams:             make(map[string]*Team),
		messageQueue:      make(chan Message, 1000),
		tasks:             make(map[string]*CollaborativeTask),
		logger:            log,
		done:              make(chan struct{}),
		closed:            false,
		maxAgents:         100,
		messageBufferSize: 1000,
		timeout:           30 * time.Second,
	}

	for _, opt := range opts {
		opt(system)
	}

	// Start message router
	system.wg.Add(1)
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

	if s.closed {
		return agentErrors.New(agentErrors.CodeInvalidConfig, "system is closed").
			WithComponent("multiagent_system").
			WithOperation("register_agent")
	}

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
	s.agentOrder = append(s.agentOrder, id) // 记录注册顺序
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

	// 从 agentOrder 中移除
	newOrder := make([]string, 0, len(s.agentOrder)-1)
	for _, aid := range s.agentOrder {
		if aid != id {
			newOrder = append(newOrder, aid)
		}
	}
	s.agentOrder = newOrder

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

	// 使用互斥锁保护 task.Assignments 的并发写
	var taskMu sync.Mutex

	for agentID, agent := range agents {
		assignment := Assignment{
			AgentID:   agentID,
			Role:      agent.GetRole(),
			Subtask:   task.Input,
			Status:    TaskStatusExecuting,
			StartTime: time.Now(),
		}
		// 在主 goroutine 中安全写入初始 assignment
		taskMu.Lock()
		task.Assignments[agentID] = assignment
		taskMu.Unlock()

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

	// Collect results - 所有 goroutine 已结束，无需加锁
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
// 支持多种 Leader plan 格式：
// - map[string]interface{} 带 "subtasks" 字段: 自动分配任务给 workers
// - map[string]interface{} 带实际 worker ID: 按 ID 分配任务
// - []interface{}: 直接作为任务列表自动分配
func (s *MultiAgentSystem) executeHierarchicalTask(ctx context.Context, task *CollaborativeTask) error {
	s.mu.RLock()
	leader := s.findLeader()
	workers := s.findWorkers()
	workerIDs := s.getWorkerIDsOrdered()
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

	// 解析 Leader 返回的 plan，支持多种格式
	workerTasks, err := s.parseLeaderPlan(leaderResult.Result, workerIDs, workers)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "failed to parse leader plan").
			WithComponent("multiagent_system").
			WithOperation("execute_hierarchical_task").
			WithContext("task_id", task.ID)
	}

	// Execute worker tasks in parallel
	var wg sync.WaitGroup
	workerResults := make(map[string]interface{})
	var mu sync.Mutex

	for workerID, subtask := range workerTasks {
		worker, exists := workers[workerID]
		if !exists {
			s.logger.Warnw("Worker not found for assigned task",
				"worker_id", workerID)
			continue
		}

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

// parseLeaderPlan 解析 Leader 返回的 plan，支持多种格式
func (s *MultiAgentSystem) parseLeaderPlan(planResult interface{}, workerIDs []string, workers map[string]CollaborativeAgent) (map[string]interface{}, error) {
	if planResult == nil {
		return nil, fmt.Errorf("plan result is nil")
	}

	switch plan := planResult.(type) {
	case map[string]interface{}:
		// 检查是否包含 subtasks 字段（任务列表格式）
		if subtasks, exists := plan["subtasks"]; exists {
			return s.distributeSubtasksToWorkers(subtasks, workerIDs)
		}

		// 检查是否包含 tasks 字段（另一种任务列表格式）
		if tasks, exists := plan["tasks"]; exists {
			return s.distributeSubtasksToWorkers(tasks, workerIDs)
		}

		// 尝试作为 worker ID 映射处理
		result := make(map[string]interface{})
		assignedCount := 0

		// 首先尝试使用实际 worker ID
		for workerID := range workers {
			if subtask, exists := plan[workerID]; exists {
				result[workerID] = subtask
				assignedCount++
			}
		}

		// 如果没有匹配到任何 worker，尝试按顺序分配
		if assignedCount == 0 {
			// 可能是占位符格式（worker_1, worker_2 等）
			placeholderTasks := make([]interface{}, 0)
			for key, value := range plan {
				// 跳过元数据字段
				if key == "strategy" || key == "description" || key == "metadata" {
					continue
				}
				placeholderTasks = append(placeholderTasks, value)
			}
			if len(placeholderTasks) > 0 {
				return s.distributeSubtasksToWorkers(placeholderTasks, workerIDs)
			}
		}

		if assignedCount == 0 {
			return nil, fmt.Errorf("no tasks could be assigned to workers from plan")
		}

		return result, nil

	case []interface{}:
		// 直接作为任务列表处理
		return s.distributeSubtasksToWorkers(plan, workerIDs)

	default:
		return nil, fmt.Errorf("unsupported plan type: %T", planResult)
	}
}

// distributeSubtasksToWorkers 将任务列表自动分配给 workers
func (s *MultiAgentSystem) distributeSubtasksToWorkers(subtasks interface{}, workerIDs []string) (map[string]interface{}, error) {
	var taskList []interface{}

	switch v := subtasks.(type) {
	case []interface{}:
		taskList = v
	case []map[string]interface{}:
		taskList = make([]interface{}, len(v))
		for i, item := range v {
			taskList[i] = item
		}
	default:
		return nil, fmt.Errorf("subtasks must be an array, got %T", subtasks)
	}

	if len(taskList) == 0 {
		return nil, fmt.Errorf("subtasks list is empty")
	}

	if len(workerIDs) == 0 {
		return nil, fmt.Errorf("no workers available for task distribution")
	}

	result := make(map[string]interface{})

	// 按顺序分配任务给 workers（如果任务多于 workers，后续任务会被忽略）
	for i, task := range taskList {
		if i >= len(workerIDs) {
			s.logger.Warnw("More subtasks than workers, some tasks will be skipped",
				"total_subtasks", len(taskList),
				"available_workers", len(workerIDs))
			break
		}
		result[workerIDs[i]] = task
	}

	return result, nil
}

// getWorkerIDsOrdered 返回有序的 worker ID 列表
func (s *MultiAgentSystem) getWorkerIDsOrdered() []string {
	workerIDs := make([]string, 0)
	for _, id := range s.agentOrder {
		if agent, exists := s.agents[id]; exists {
			if agent.GetRole() == RoleWorker {
				workerIDs = append(workerIDs, id)
			}
		}
	}
	return workerIDs
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

	// 解析 quorum 参数，默认为简单多数 (0.5)
	quorum := 0.5
	if inputMap, ok := task.Input.(map[string]interface{}); ok {
		if q, exists := inputMap["quorum"]; exists {
			switch v := q.(type) {
			case float64:
				quorum = v
			case int:
				quorum = float64(v)
			}
		}
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

	// 使用用户定义的 quorum 阈值计算是否达成共识
	consensusThreshold := int(float64(len(votes)) * quorum)
	if consensusThreshold < 1 {
		consensusThreshold = 1
	}
	consensusReached := yesVotes >= consensusThreshold

	task.Output = map[string]interface{}{
		"consensus_reached": consensusReached,
		"yes_votes":         yesVotes,
		"total_votes":       len(votes),
		"quorum":            quorum,
		"threshold":         consensusThreshold,
		"votes":             votes,
	}

	if !consensusReached {
		return agentErrors.Newf(agentErrors.CodeMultiAgentConsensus, "consensus not reached: %d/%d votes (need %.0f%%)", yesVotes, len(votes), quorum*100).
			WithComponent("multiagent_system").
			WithOperation("execute_consensus_task").
			WithContext("task_id", task.ID).
			WithContext("yes_votes", yesVotes).
			WithContext("total_votes", len(votes)).
			WithContext("quorum", quorum)
	}

	return nil
}

// executePipelineTask executes tasks in a pipeline
// 支持多种输入格式：
// - []PipelineStage: 推荐的结构化格式
// - []interface{}: 兼容现有格式
// - []map[string]interface{}: 便捷 map 格式
func (s *MultiAgentSystem) executePipelineTask(ctx context.Context, task *CollaborativeTask) error {
	// 解析 Pipeline 阶段，支持多种输入格式
	stages, err := s.parsePipelineStages(task.Input)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "failed to parse pipeline stages").
			WithComponent("multiagent_system").
			WithOperation("execute_pipeline_task").
			WithContext("task_id", task.ID)
	}

	s.mu.RLock()
	agents := s.getAvailableAgentsOrdered()
	s.mu.RUnlock()

	if len(agents) < len(stages) {
		return agentErrors.New(agentErrors.CodeInvalidConfig, "not enough agents for pipeline stages").
			WithComponent("multiagent_system").
			WithOperation("execute_pipeline_task").
			WithContext("task_id", task.ID).
			WithContext("available_agents", len(agents)).
			WithContext("required_stages", len(stages))
	}

	// Execute each stage
	var previousOutput interface{}
	for i, stage := range stages {
		if i >= len(agents) {
			break
		}

		agentID := agents[i]
		agent := s.agents[agentID]

		stageTask := *task
		stageTask.Input = map[string]interface{}{
			"stage":      stage,
			"stage_name": stage.Name,
			"config":     stage.Config,
			"previous":   previousOutput,
		}

		result, err := agent.Collaborate(ctx, &stageTask)
		if err != nil {
			return agentErrors.Wrapf(err, agentErrors.CodeAgentExecution, "pipeline stage %d (%s) failed", i, stage.Name).
				WithComponent("multiagent_system").
				WithOperation("execute_pipeline_task").
				WithContext("task_id", task.ID).
				WithContext("stage_index", i).
				WithContext("stage_name", stage.Name).
				WithContext("agent_id", agentID)
		}

		previousOutput = result.Result
		// 使用阶段名称作为结果 key（如果有），否则使用索引
		resultKey := stage.Name
		if resultKey == "" {
			resultKey = fmt.Sprintf("stage_%d", i)
		}
		task.Results[resultKey] = result.Result
	}

	task.Output = previousOutput
	return nil
}

// parsePipelineStages 解析 Pipeline 输入为统一的 PipelineStage 切片
// 支持多种输入格式以提升开发体验
func (s *MultiAgentSystem) parsePipelineStages(input interface{}) ([]PipelineStage, error) {
	if input == nil {
		return nil, fmt.Errorf("pipeline input is nil")
	}

	switch v := input.(type) {
	case []PipelineStage:
		// 推荐格式：直接使用 PipelineStage 切片
		return v, nil

	case []interface{}:
		// 兼容格式：[]interface{} 切片
		stages := make([]PipelineStage, 0, len(v))
		for i, item := range v {
			stage, err := s.convertToPipelineStage(item, i)
			if err != nil {
				return nil, fmt.Errorf("failed to convert stage %d: %w", i, err)
			}
			stages = append(stages, stage)
		}
		return stages, nil

	case []map[string]interface{}:
		// 便捷格式：map 切片
		stages := make([]PipelineStage, 0, len(v))
		for i, item := range v {
			stage, err := s.convertToPipelineStage(item, i)
			if err != nil {
				return nil, fmt.Errorf("failed to convert stage %d: %w", i, err)
			}
			stages = append(stages, stage)
		}
		return stages, nil

	default:
		return nil, fmt.Errorf("unsupported pipeline input type: %T, expected []PipelineStage, []interface{}, or []map[string]interface{}", input)
	}
}

// convertToPipelineStage 将单个阶段项转换为 PipelineStage
func (s *MultiAgentSystem) convertToPipelineStage(item interface{}, index int) (PipelineStage, error) {
	switch v := item.(type) {
	case PipelineStage:
		return v, nil

	case map[string]interface{}:
		stage := PipelineStage{
			Config: v,
		}
		// 提取 name 字段
		if name, ok := v["name"].(string); ok {
			stage.Name = name
		} else {
			stage.Name = fmt.Sprintf("stage_%d", index)
		}
		// 提取 description 字段
		if desc, ok := v["description"].(string); ok {
			stage.Description = desc
		}
		return stage, nil

	case string:
		// 简单字符串作为阶段名称
		return PipelineStage{
			Name: v,
		}, nil

	default:
		// 其他类型：使用默认名称，将整个项作为配置
		return PipelineStage{
			Name: fmt.Sprintf("stage_%d", index),
			Config: map[string]interface{}{
				"data": item,
			},
		}, nil
	}
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
	defer s.wg.Done()

	for {
		select {
		case <-s.done:
			// 优雅关闭：处理剩余消息
			for {
				select {
				case message := <-s.messageQueue:
					s.deliverMessage(message)
				default:
					return
				}
			}
		case message, ok := <-s.messageQueue:
			if !ok {
				return
			}
			s.deliverMessage(message)
		}
	}
}

// deliverMessage 投递消息到目标 Agent
func (s *MultiAgentSystem) deliverMessage(message Message) {
	s.mu.RLock()
	recipient, exists := s.agents[message.To]
	s.mu.RUnlock()

	if !exists {
		s.logger.Errorw("Recipient not found",
			"to", message.To,
			"from", message.From)
		return
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

// Close 优雅关闭多智能体系统
func (s *MultiAgentSystem) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	// 发送关闭信号
	close(s.done)

	// 等待消息路由 goroutine 完成
	s.wg.Wait()

	// 关闭消息队列
	close(s.messageQueue)

	s.logger.Info("MultiAgentSystem closed")
	return nil
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
	// 返回注册顺序，保证顺序任务执行顺序确定
	result := make([]string, 0, len(s.agentOrder))
	for _, id := range s.agentOrder {
		if _, exists := s.agents[id]; exists {
			result = append(result, id)
		}
	}
	return result
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
