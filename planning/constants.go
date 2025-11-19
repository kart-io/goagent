package planning

// Agent names
const (
	AgentPlanning          = "planning_agent"
	AgentTaskDecomposition = "task_decomposition_agent"
	AgentStrategy          = "strategy_agent"
)

// Agent descriptions
const (
	DescPlanning          = "Creates and executes plans to achieve goals"
	DescTaskDecomposition = "Decomposes complex tasks into manageable subtasks"
	DescStrategyAgent     = "Selects and applies planning strategies to optimize plans"
)

// Strategy names
const (
	StrategyDecomposition    = "decomposition"
	StrategyBackwardChaining = "backward_chaining"
	StrategyHierarchical     = "hierarchical"
	StrategyForwardChaining  = "forward_chaining"
	StrategyGoalOriented     = "goal_oriented"
)

// Plan statuses
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusCanceled   = "canceled"
)

// Additional step types (extend StepType defined in planner.go)
// Note: StepTypeAction, StepTypeDecision, etc. are defined in planner.go

// Plan constraints defaults
const (
	DefaultMaxSteps       = 20
	DefaultMaxDuration    = 3600 // seconds
	DefaultMaxParallelism = 5
	DefaultRetryAttempts  = 3
)

// Planning error messages
const (
	ErrPlanningFailed      = "planning failed"
	ErrPlanExecutionFailed = "plan execution failed"
	ErrInvalidPlan         = "invalid plan"
	ErrStepFailed          = "step execution failed"
	ErrDependencyNotMet    = "dependency not met"
	ErrTimeoutExceeded     = "timeout exceeded"
)
