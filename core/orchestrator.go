package core

import (
	"context"
	"time"
)

// Orchestrator 定义编排器接口
//
// Orchestrator 负责协调多个 Agent、Chain 和 Tool 的执行，适用于：
// - 复杂的多步骤工作流
// - 需要多个 Agent 协作的场景
// - 动态决策和条件分支
type Orchestrator interface {
	// Execute 执行编排任务
	Execute(ctx context.Context, request *OrchestratorRequest) (*OrchestratorResponse, error)

	// RegisterAgent 注册 Agent
	RegisterAgent(name string, agent Agent) error

	// RegisterChain 注册 Chain
	RegisterChain(name string, chain Chain) error

	// RegisterTool 注册 Tool
	RegisterTool(name string, tool Tool) error

	// Name 返回编排器名称
	Name() string
}

// OrchestratorRequest 编排器请求
type OrchestratorRequest struct {
	// 任务信息
	TaskID      string                 `json:"task_id"`     // 任务 ID
	TaskType    string                 `json:"task_type"`   // 任务类型
	Description string                 `json:"description"` // 任务描述
	Parameters  map[string]interface{} `json:"parameters"`  // 任务参数

	// 执行策略
	Strategy OrchestratorStrategy `json:"strategy"` // 编排策略

	// 选项
	Options OrchestratorOptions `json:"options"` // 执行选项

	// 元数据
	SessionID string    `json:"session_id"` // 会话 ID
	Timestamp time.Time `json:"timestamp"`  // 时间戳
}

// OrchestratorResponse 编排器响应
type OrchestratorResponse struct {
	// 执行结果
	Result  interface{} `json:"result"`  // 最终结果
	Status  string      `json:"status"`  // 状态: "success", "failed", "partial"
	Message string      `json:"message"` // 结果消息

	// 执行过程
	ExecutionPlan  []ExecutionStep `json:"execution_plan"`  // 执行计划
	ExecutionSteps []ExecutionStep `json:"execution_steps"` // 实际执行的步骤

	// 性能指标
	TotalLatency time.Duration `json:"total_latency"` // 总延迟
	StartTime    time.Time     `json:"start_time"`    // 开始时间
	EndTime      time.Time     `json:"end_time"`      // 结束时间

	// 元数据
	Metadata map[string]interface{} `json:"metadata"` // 额外元数据
}

// OrchestratorStrategy 编排策略
type OrchestratorStrategy struct {
	// 执行模式
	Mode string `json:"mode"` // "sequential", "parallel", "hybrid"

	// 重试策略
	EnableRetry  bool `json:"enable_retry"`  // 是否启用重试
	MaxRetries   int  `json:"max_retries"`   // 最大重试次数
	RetryBackoff int  `json:"retry_backoff"` // 重试退避时间（秒）

	// 失败处理
	FailurePolicy string `json:"failure_policy"` // "stop", "continue", "rollback"

	// 超时策略
	GlobalTimeout time.Duration `json:"global_timeout"` // 全局超时
	StepTimeout   time.Duration `json:"step_timeout"`   // 单步超时
}

// OrchestratorOptions 编排器选项
type OrchestratorOptions struct {
	// 日志选项
	EnableLogging bool   `json:"enable_logging"` // 是否启用日志
	LogLevel      string `json:"log_level"`      // 日志级别

	// 监控选项
	EnableMetrics bool `json:"enable_metrics"` // 是否启用指标
	EnableTracing bool `json:"enable_tracing"` // 是否启用追踪

	// 回调
	OnStepStart    func(step ExecutionStep) `json:"-"` // 步骤开始回调
	OnStepComplete func(step ExecutionStep) `json:"-"` // 步骤完成回调
	OnError        func(err error)          `json:"-"` // 错误回调

	// 额外选项
	Extra map[string]interface{} `json:"extra,omitempty"` // 额外选项
}

// ExecutionStep 执行步骤
type ExecutionStep struct {
	// 步骤信息
	Step        int    `json:"step"`        // 步骤编号
	Name        string `json:"name"`        // 步骤名称
	Type        string `json:"type"`        // 类型: "agent", "chain", "tool", "decision"
	Description string `json:"description"` // 描述

	// 执行信息
	ComponentName string      `json:"component_name"` // 组件名称
	Input         interface{} `json:"input"`          // 输入
	Output        interface{} `json:"output"`         // 输出
	Status        string      `json:"status"`         // 状态: "pending", "running", "success", "failed", "skipped"

	// 时间信息
	StartTime time.Time     `json:"start_time"` // 开始时间
	EndTime   time.Time     `json:"end_time"`   // 结束时间
	Duration  time.Duration `json:"duration"`   // 耗时

	// 错误信息
	Error        string `json:"error,omitempty"`         // 错误信息
	RetryCount   int    `json:"retry_count,omitempty"`   // 重试次数
	PartialError string `json:"partial_error,omitempty"` // 部分错误

	// 元数据
	Metadata map[string]interface{} `json:"metadata,omitempty"` // 额外元数据
}

// Tool 定义工具接口
type Tool interface {
	// Execute 执行工具
	Execute(ctx context.Context, input *ToolInput) (*ToolOutput, error)

	// Name 返回工具名称
	Name() string

	// Description 返回工具描述
	Description() string

	// Parameters 返回工具参数定义
	Parameters() []ToolParameter
}

// ToolInput 工具输入
type ToolInput struct {
	Action     string                 `json:"action"`     // 操作类型
	Parameters map[string]interface{} `json:"parameters"` // 参数
	Context    map[string]interface{} `json:"context"`    // 上下文
}

// ToolOutput 工具输出
type ToolOutput struct {
	Success bool        `json:"success"` // 是否成功
	Data    interface{} `json:"data"`    // 输出数据
	Message string      `json:"message"` // 消息
	Error   string      `json:"error"`   // 错误信息
}

// ToolParameter 工具参数定义
type ToolParameter struct {
	Name        string      `json:"name"`        // 参数名称
	Type        string      `json:"type"`        // 参数类型
	Description string      `json:"description"` // 参数描述
	Required    bool        `json:"required"`    // 是否必需
	Default     interface{} `json:"default"`     // 默认值
}

// BaseOrchestrator 提供编排器的基础实现
type BaseOrchestrator struct {
	name   string
	agents map[string]Agent
	chains map[string]Chain
	tools  map[string]Tool
}

// NewBaseOrchestrator 创建基础编排器
func NewBaseOrchestrator(name string) *BaseOrchestrator {
	return &BaseOrchestrator{
		name:   name,
		agents: make(map[string]Agent),
		chains: make(map[string]Chain),
		tools:  make(map[string]Tool),
	}
}

// Name 返回编排器名称
func (o *BaseOrchestrator) Name() string {
	return o.name
}

// RegisterAgent 注册 Agent
func (o *BaseOrchestrator) RegisterAgent(name string, agent Agent) error {
	if _, exists := o.agents[name]; exists {
		return ErrAgentAlreadyExists
	}
	o.agents[name] = agent
	return nil
}

// RegisterChain 注册 Chain
func (o *BaseOrchestrator) RegisterChain(name string, chain Chain) error {
	if _, exists := o.chains[name]; exists {
		return ErrChainAlreadyExists
	}
	o.chains[name] = chain
	return nil
}

// RegisterTool 注册 Tool
func (o *BaseOrchestrator) RegisterTool(name string, tool Tool) error {
	if _, exists := o.tools[name]; exists {
		return ErrToolAlreadyExists
	}
	o.tools[name] = tool
	return nil
}

// GetAgent 获取 Agent
func (o *BaseOrchestrator) GetAgent(name string) (Agent, bool) {
	agent, exists := o.agents[name]
	return agent, exists
}

// GetChain 获取 Chain
func (o *BaseOrchestrator) GetChain(name string) (Chain, bool) {
	chain, exists := o.chains[name]
	return chain, exists
}

// GetTool 获取 Tool
func (o *BaseOrchestrator) GetTool(name string) (Tool, bool) {
	tool, exists := o.tools[name]
	return tool, exists
}

// Execute returns ErrNotImplemented.
//
// Concrete orchestrator implementations must override this method.
// Using composition: embed BaseOrchestrator and implement Execute.
func (o *BaseOrchestrator) Execute(ctx context.Context, request *OrchestratorRequest) (*OrchestratorResponse, error) {
	return nil, ErrNotImplemented
}

// DefaultOrchestratorStrategy 返回默认编排策略
func DefaultOrchestratorStrategy() OrchestratorStrategy {
	return OrchestratorStrategy{
		Mode:          "sequential",
		EnableRetry:   false,
		MaxRetries:    3,
		RetryBackoff:  2,
		FailurePolicy: "stop",
		GlobalTimeout: 5 * time.Minute,
		StepTimeout:   60 * time.Second,
	}
}

// DefaultOrchestratorOptions 返回默认编排选项
func DefaultOrchestratorOptions() OrchestratorOptions {
	return OrchestratorOptions{
		EnableLogging: true,
		LogLevel:      "info",
		EnableMetrics: false,
		EnableTracing: false,
		Extra:         make(map[string]interface{}),
	}
}
