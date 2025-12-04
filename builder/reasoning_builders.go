package builder

import (
	"time"

	"github.com/kart-io/goagent/agents"
	"github.com/kart-io/goagent/agents/cot"
	"github.com/kart-io/goagent/agents/executor"
	"github.com/kart-io/goagent/agents/got"
	"github.com/kart-io/goagent/agents/metacot"
	"github.com/kart-io/goagent/agents/pot"
	"github.com/kart-io/goagent/agents/react"
	"github.com/kart-io/goagent/agents/sot"
	"github.com/kart-io/goagent/agents/tot"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// ==============================
// 推理型 Agent 快速构建函数
// ==============================

// CoTAgentConfig CoT Agent 配置选项
type CoTAgentConfig struct {
	Name                 string
	Description          string
	Tools                []interfaces.Tool
	MaxSteps             int
	ShowStepNumbers      bool
	RequireJustification bool
	ZeroShot             bool
	FewShot              bool
	FewShotExamples      []cot.CoTExample
}

// CoTAgent 创建一个 Chain-of-Thought 推理 Agent
//
// CoT Agent 通过分步骤推理来解决复杂问题：
//   - 鼓励模型逐步分解问题
//   - 展示中间推理过程
//   - 提高复杂任务的准确性
//   - 支持 Zero-Shot 和 Few-Shot 提示
//
// 参数：
//   - llmClient: LLM 客户端
//   - config: 可选配置，为 nil 时使用默认值
//
// 示例：
//
//	agent := builder.CoTAgent(llmClient, &builder.CoTAgentConfig{
//	    MaxSteps: 10,
//	    ZeroShot: true,
//	})
func CoTAgent(llmClient llm.Client, config *CoTAgentConfig) *cot.CoTAgent {
	cfg := cot.CoTConfig{
		Name:        "cot-agent",
		Description: "Chain-of-Thought reasoning agent",
		LLM:         llmClient,
		MaxSteps:    10,
		ZeroShot:    true,
	}

	if config != nil {
		if config.Name != "" {
			cfg.Name = config.Name
		}
		if config.Description != "" {
			cfg.Description = config.Description
		}
		if len(config.Tools) > 0 {
			cfg.Tools = config.Tools
		}
		if config.MaxSteps > 0 {
			cfg.MaxSteps = config.MaxSteps
		}
		cfg.ShowStepNumbers = config.ShowStepNumbers
		cfg.RequireJustification = config.RequireJustification
		cfg.ZeroShot = config.ZeroShot
		cfg.FewShot = config.FewShot
		cfg.FewShotExamples = config.FewShotExamples
	}

	return cot.NewCoTAgent(cfg)
}

// ReActAgentConfig ReAct Agent 配置选项
type ReActAgentConfig struct {
	Name         string
	Description  string
	Tools        []interfaces.Tool
	MaxSteps     int
	StopPattern  []string
	PromptPrefix string
	PromptSuffix string
}

// ReActAgent 创建一个 ReAct (Reasoning + Acting) Agent
//
// ReAct Agent 实现思考-行动-观察循环：
//   - Thought: 分析当前情况
//   - Action: 决定使用哪个工具
//   - Observation: 执行工具并观察结果
//   - 循环直到得出最终答案
//
// 参数：
//   - llmClient: LLM 客户端
//   - tools: 可用工具列表
//   - config: 可选配置，为 nil 时使用默认值
//
// 示例：
//
//	agent := builder.ReActAgent(llmClient, tools, &builder.ReActAgentConfig{
//	    MaxSteps: 15,
//	})
func ReActAgent(llmClient llm.Client, tools []interfaces.Tool, config *ReActAgentConfig) *react.ReActAgent {
	cfg := react.ReActConfig{
		Name:        "react-agent",
		Description: "ReAct reasoning and acting agent",
		LLM:         llmClient,
		Tools:       tools,
		MaxSteps:    10,
	}

	if config != nil {
		if config.Name != "" {
			cfg.Name = config.Name
		}
		if config.Description != "" {
			cfg.Description = config.Description
		}
		if len(config.Tools) > 0 {
			cfg.Tools = config.Tools
		}
		if config.MaxSteps > 0 {
			cfg.MaxSteps = config.MaxSteps
		}
		if len(config.StopPattern) > 0 {
			cfg.StopPattern = config.StopPattern
		}
		if config.PromptPrefix != "" {
			cfg.PromptPrefix = config.PromptPrefix
		}
		if config.PromptSuffix != "" {
			cfg.PromptSuffix = config.PromptSuffix
		}
	}

	return react.NewReActAgent(cfg)
}

// ToTAgentConfig ToT Agent 配置选项
type ToTAgentConfig struct {
	Name             string
	Description      string
	Tools            []interfaces.Tool
	MaxDepth         int
	BranchingFactor  int
	BeamWidth        int
	SearchStrategy   interfaces.ReasoningStrategy
	EvaluationMethod string
	PruneThreshold   float64
}

// ToTAgent 创建一个 Tree-of-Thought 推理 Agent
//
// ToT Agent 探索多条推理路径的树形结构：
//   - 支持 BFS、DFS、Beam Search、Monte Carlo 搜索策略
//   - 动态评估和剪枝低分支
//   - 选择最优推理路径
//
// 参数：
//   - llmClient: LLM 客户端
//   - config: 可选配置，为 nil 时使用默认值
//
// 示例：
//
//	agent := builder.ToTAgent(llmClient, &builder.ToTAgentConfig{
//	    MaxDepth: 5,
//	    BranchingFactor: 3,
//	    SearchStrategy: interfaces.StrategyBeamSearch,
//	})
func ToTAgent(llmClient llm.Client, config *ToTAgentConfig) *tot.ToTAgent {
	cfg := tot.ToTConfig{
		Name:             "tot-agent",
		Description:      "Tree-of-Thought reasoning agent",
		LLM:              llmClient,
		MaxDepth:         5,
		BranchingFactor:  3,
		SearchStrategy:   interfaces.StrategyBeamSearch,
		EvaluationMethod: "llm",
		PruneThreshold:   0.3,
	}

	if config != nil {
		if config.Name != "" {
			cfg.Name = config.Name
		}
		if config.Description != "" {
			cfg.Description = config.Description
		}
		if len(config.Tools) > 0 {
			cfg.Tools = config.Tools
		}
		if config.MaxDepth > 0 {
			cfg.MaxDepth = config.MaxDepth
		}
		if config.BranchingFactor > 0 {
			cfg.BranchingFactor = config.BranchingFactor
		}
		if config.BeamWidth > 0 {
			cfg.BeamWidth = config.BeamWidth
		}
		if config.SearchStrategy != "" {
			cfg.SearchStrategy = config.SearchStrategy
		}
		if config.EvaluationMethod != "" {
			cfg.EvaluationMethod = config.EvaluationMethod
		}
		if config.PruneThreshold > 0 {
			cfg.PruneThreshold = config.PruneThreshold
		}
	}

	return tot.NewToTAgent(cfg)
}

// PoTAgentConfig PoT Agent 配置选项
type PoTAgentConfig struct {
	Name             string
	Description      string
	Tools            []interfaces.Tool
	Language         string
	AllowedLanguages []string
	MaxCodeLength    int
	ExecutionTimeout time.Duration
	SafeMode         bool
	PythonPath       string
	NodePath         string
	MaxIterations    int
}

// PoTAgent 创建一个 Program-of-Thought Agent
//
// PoT Agent 生成可执行代码来解决问题：
//   - 支持多语言：Python、JavaScript、Go
//   - 包含代码验证、执行、调试
//   - 迭代优化直到获得正确答案
//
// 参数：
//   - llmClient: LLM 客户端
//   - config: 可选配置，为 nil 时使用默认值
//
// 示例：
//
//	agent := builder.PoTAgent(llmClient, &builder.PoTAgentConfig{
//	    Language: "python",
//	    ExecutionTimeout: 30 * time.Second,
//	})
func PoTAgent(llmClient llm.Client, config *PoTAgentConfig) *pot.PoTAgent {
	cfg := pot.PoTConfig{
		Name:             "pot-agent",
		Description:      "Program-of-Thought reasoning agent",
		LLM:              llmClient,
		Language:         "python",
		ExecutionTimeout: 30 * time.Second,
		SafeMode:         true,
		MaxIterations:    5,
	}

	if config != nil {
		if config.Name != "" {
			cfg.Name = config.Name
		}
		if config.Description != "" {
			cfg.Description = config.Description
		}
		if len(config.Tools) > 0 {
			cfg.Tools = config.Tools
		}
		if config.Language != "" {
			cfg.Language = config.Language
		}
		if len(config.AllowedLanguages) > 0 {
			cfg.AllowedLanguages = config.AllowedLanguages
		}
		if config.MaxCodeLength > 0 {
			cfg.MaxCodeLength = config.MaxCodeLength
		}
		if config.ExecutionTimeout > 0 {
			cfg.ExecutionTimeout = config.ExecutionTimeout
		}
		cfg.SafeMode = config.SafeMode
		if config.PythonPath != "" {
			cfg.PythonPath = config.PythonPath
		}
		if config.NodePath != "" {
			cfg.NodePath = config.NodePath
		}
		if config.MaxIterations > 0 {
			cfg.MaxIterations = config.MaxIterations
		}
	}

	return pot.NewPoTAgent(cfg)
}

// SoTAgentConfig SoT Agent 配置选项
type SoTAgentConfig struct {
	Name                string
	Description         string
	Tools               []interfaces.Tool
	MaxSkeletonPoints   int
	MinSkeletonPoints   int
	MaxConcurrency      int
	ElaborationTimeout  time.Duration
	AggregationStrategy string
}

// SoTAgent 创建一个 Skeleton-of-Thought Agent
//
// SoT Agent 先生成高层骨架，再并行详细阐述：
//   - 优化推理延迟，提高效率
//   - 支持依赖管理和并行执行
//   - 适合需要结构化输出的任务
//
// 参数：
//   - llmClient: LLM 客户端
//   - config: 可选配置，为 nil 时使用默认值
//
// 示例：
//
//	agent := builder.SoTAgent(llmClient, &builder.SoTAgentConfig{
//	    MaxSkeletonPoints: 10,
//	    MaxConcurrency: 5,
//	})
func SoTAgent(llmClient llm.Client, config *SoTAgentConfig) *sot.SoTAgent {
	cfg := sot.SoTConfig{
		Name:                "sot-agent",
		Description:         "Skeleton-of-Thought reasoning agent",
		LLM:                 llmClient,
		MaxSkeletonPoints:   10,
		MinSkeletonPoints:   3,
		MaxConcurrency:      5,
		ElaborationTimeout:  30 * time.Second,
		AggregationStrategy: "sequential",
	}

	if config != nil {
		if config.Name != "" {
			cfg.Name = config.Name
		}
		if config.Description != "" {
			cfg.Description = config.Description
		}
		if len(config.Tools) > 0 {
			cfg.Tools = config.Tools
		}
		if config.MaxSkeletonPoints > 0 {
			cfg.MaxSkeletonPoints = config.MaxSkeletonPoints
		}
		if config.MinSkeletonPoints > 0 {
			cfg.MinSkeletonPoints = config.MinSkeletonPoints
		}
		if config.MaxConcurrency > 0 {
			cfg.MaxConcurrency = config.MaxConcurrency
		}
		if config.ElaborationTimeout > 0 {
			cfg.ElaborationTimeout = config.ElaborationTimeout
		}
		if config.AggregationStrategy != "" {
			cfg.AggregationStrategy = config.AggregationStrategy
		}
	}

	return sot.NewSoTAgent(cfg)
}

// GoTAgentConfig GoT Agent 配置选项
type GoTAgentConfig struct {
	Name              string
	Description       string
	Tools             []interfaces.Tool
	MaxNodes          int
	MaxEdgesPerNode   int
	ParallelExecution bool
	MergeStrategy     string
	CycleDetection    bool
	PruneThreshold    float64

	// 性能优化参数
	FastEvaluation bool          // 快速评估模式：使用启发式评分代替 LLM 评估
	NodeTimeout    time.Duration // 单节点处理超时时间

	// DeepSeek/慢速 API 优化参数
	MinimalMode     bool // 极简模式：仅使用2次LLM调用，适用于慢速API
	BatchGeneration bool // 批量生成：一次调用生成所有思考
	DirectSynthesis bool // 直接合成：跳过图执行
}

// GoTAgent 创建一个 Graph-of-Thought Agent
//
// GoT Agent 构建有向无环图（DAG）的思想依赖关系：
//   - 支持复杂的多路径推理
//   - 包含循环检测和并行执行
//   - 合并多条路径的见解
//
// 参数：
//   - llmClient: LLM 客户端
//   - config: 可选配置，为 nil 时使用默认值
//
// 示例：
//
//	agent := builder.GoTAgent(llmClient, &builder.GoTAgentConfig{
//	    MaxNodes: 50,
//	    ParallelExecution: true,
//	})
func GoTAgent(llmClient llm.Client, config *GoTAgentConfig) *got.GoTAgent {
	cfg := got.GoTConfig{
		Name:              "got-agent",
		Description:       "Graph-of-Thought reasoning agent",
		LLM:               llmClient,
		MaxNodes:          10, // 优化：从 50 降低到 10
		MaxEdgesPerNode:   3,  // 优化：从 5 降低到 3
		ParallelExecution: true,
		MergeStrategy:     "weighted",
		CycleDetection:    true,
		PruneThreshold:    0.5,  // 优化：从 0.3 提高到 0.5
		FastEvaluation:    true, // 优化：默认启用快速评估
		NodeTimeout:       30 * time.Second,
	}

	if config != nil {
		if config.Name != "" {
			cfg.Name = config.Name
		}
		if config.Description != "" {
			cfg.Description = config.Description
		}
		if len(config.Tools) > 0 {
			cfg.Tools = config.Tools
		}
		if config.MaxNodes > 0 {
			cfg.MaxNodes = config.MaxNodes
		}
		if config.MaxEdgesPerNode > 0 {
			cfg.MaxEdgesPerNode = config.MaxEdgesPerNode
		}
		cfg.ParallelExecution = config.ParallelExecution
		if config.MergeStrategy != "" {
			cfg.MergeStrategy = config.MergeStrategy
		}
		cfg.CycleDetection = config.CycleDetection
		if config.PruneThreshold > 0 {
			cfg.PruneThreshold = config.PruneThreshold
		}
		// 性能优化参数
		cfg.FastEvaluation = config.FastEvaluation
		if config.NodeTimeout > 0 {
			cfg.NodeTimeout = config.NodeTimeout
		}
		// DeepSeek/慢速 API 优化参数
		cfg.MinimalMode = config.MinimalMode
		cfg.BatchGeneration = config.BatchGeneration
		cfg.DirectSynthesis = config.DirectSynthesis
	}

	return got.NewGoTAgent(cfg)
}

// GoTAgentForSlowAPI 创建一个针对慢速 API（如 DeepSeek）优化的 GoT Agent
//
// 此预设专为响应较慢的 API 提供商优化：
//   - 使用极简模式，仅 2 次 LLM 调用
//   - 一次生成多个思考路径
//   - 一次合成最终答案
//
// 参数：
//   - llmClient: LLM 客户端
//
// 示例：
//
//	agent := builder.GoTAgentForSlowAPI(llmClient)
func GoTAgentForSlowAPI(llmClient llm.Client) *got.GoTAgent {
	return GoTAgent(llmClient, &GoTAgentConfig{
		Name:           "got-agent-minimal",
		Description:    "Graph-of-Thought agent optimized for slow APIs",
		MinimalMode:    true,
		FastEvaluation: true,
		NodeTimeout:    120 * time.Second, // 慢速 API 需要更长超时
	})
}

// MetaCoTAgentConfig MetaCoT Agent 配置选项
type MetaCoTAgentConfig struct {
	Name                string
	Description         string
	Tools               []interfaces.Tool
	MaxQuestions        int
	MaxDepth            int
	AutoDecompose       bool
	RequireEvidence     bool
	SelfCritique        bool
	QuestionStrategy    string
	VerifyAnswers       bool
	ConfidenceThreshold float64
}

// MetaCoTAgent 创建一个 Meta Chain-of-Thought / Self-Ask Agent
//
// MetaCoT Agent 实现自我提问和问题分解：
//   - 递归回答子问题来构建完整答案
//   - 支持自我批判和答案验证
//   - 适合需要深度分析的复杂问题
//
// 参数：
//   - llmClient: LLM 客户端
//   - config: 可选配置，为 nil 时使用默认值
//
// 示例：
//
//	agent := builder.MetaCoTAgent(llmClient, &builder.MetaCoTAgentConfig{
//	    MaxQuestions: 5,
//	    SelfCritique: true,
//	})
func MetaCoTAgent(llmClient llm.Client, config *MetaCoTAgentConfig) *metacot.MetaCoTAgent {
	cfg := metacot.MetaCoTConfig{
		Name:                "metacot-agent",
		Description:         "Meta Chain-of-Thought / Self-Ask agent",
		LLM:                 llmClient,
		MaxQuestions:        5,
		MaxDepth:            3,
		AutoDecompose:       true,
		QuestionStrategy:    "focused",
		ConfidenceThreshold: 0.7,
	}

	if config != nil {
		if config.Name != "" {
			cfg.Name = config.Name
		}
		if config.Description != "" {
			cfg.Description = config.Description
		}
		if len(config.Tools) > 0 {
			cfg.Tools = config.Tools
		}
		if config.MaxQuestions > 0 {
			cfg.MaxQuestions = config.MaxQuestions
		}
		if config.MaxDepth > 0 {
			cfg.MaxDepth = config.MaxDepth
		}
		cfg.AutoDecompose = config.AutoDecompose
		cfg.RequireEvidence = config.RequireEvidence
		cfg.SelfCritique = config.SelfCritique
		if config.QuestionStrategy != "" {
			cfg.QuestionStrategy = config.QuestionStrategy
		}
		cfg.VerifyAnswers = config.VerifyAnswers
		if config.ConfidenceThreshold > 0 {
			cfg.ConfidenceThreshold = config.ConfidenceThreshold
		}
	}

	return metacot.NewMetaCoTAgent(cfg)
}

// ==============================
// 管理型 Agent 快速构建函数
// ==============================

// SupervisorAgentConfig Supervisor Agent 配置选项
type SupervisorAgentConfig struct {
	MaxConcurrentAgents int
	SubAgentTimeout     time.Duration
	EnableCaching       bool
	CacheTTL            time.Duration
	EnableMetrics       bool
	RoutingStrategy     agents.RoutingStrategy
	AggregationStrategy agents.AggregationStrategy
}

// SupervisorAgent 创建一个 Supervisor Agent
//
// Supervisor Agent 协调多个子 Agent 来处理复杂任务：
//   - 任务分解、路由、结果聚合
//   - 支持缓存、指标、重试策略
//   - 适合需要多 Agent 协作的场景
//
// 参数：
//   - llmClient: LLM 客户端
//   - subAgents: 子 Agent 映射 (名称 -> Agent)
//   - config: 可选配置，为 nil 时使用默认值
//
// 示例：
//
//	supervisor := builder.SupervisorAgent(llmClient, map[string]core.Agent{
//	    "researcher": researchAgent,
//	    "analyzer": analysisAgent,
//	}, &builder.SupervisorAgentConfig{
//	    MaxConcurrentAgents: 5,
//	})
func SupervisorAgent(llmClient llm.Client, subAgents map[string]agentcore.Agent, config *SupervisorAgentConfig) *agents.SupervisorAgent {
	cfg := agents.DefaultSupervisorConfig()

	if config != nil {
		if config.MaxConcurrentAgents > 0 {
			cfg.MaxConcurrentAgents = config.MaxConcurrentAgents
		}
		if config.SubAgentTimeout > 0 {
			cfg.SubAgentTimeout = config.SubAgentTimeout
		}
		cfg.EnableCaching = config.EnableCaching
		if config.CacheTTL > 0 {
			cfg.CacheTTL = config.CacheTTL
		}
		cfg.EnableMetrics = config.EnableMetrics
		if config.RoutingStrategy != "" {
			cfg.RoutingStrategy = config.RoutingStrategy
		}
		if config.AggregationStrategy != "" {
			cfg.AggregationStrategy = config.AggregationStrategy
		}
	}

	supervisor := agents.NewSupervisorAgent(llmClient, cfg)

	// 添加子 Agent
	for name, agent := range subAgents {
		supervisor.AddSubAgent(name, agent)
	}

	return supervisor
}

// ExecutorConfig Executor 配置选项
type ExecutorConfig struct {
	Tools               []interfaces.Tool
	Memory              executor.Memory
	MaxIterations       int
	MaxExecutionTime    time.Duration
	EarlyStoppingMethod string
	HandleParsingErrors bool
	ReturnIntermSteps   bool
	Verbose             bool
}

// AgentExecutor 创建一个 Agent 执行器
//
// AgentExecutor 提供高级执行逻辑：
//   - 记忆管理、对话历史
//   - 工具管理
//   - 错误处理、重试和早停机制
//
// 参数：
//   - agent: 要包装的 Agent 实例
//   - config: 可选配置，为 nil 时使用默认值
//
// 示例：
//
//	exec := builder.AgentExecutor(reactAgent, &builder.ExecutorConfig{
//	    MaxIterations: 15,
//	    Verbose: true,
//	})
func AgentExecutor(agent agentcore.Agent, config *ExecutorConfig) *executor.AgentExecutor {
	cfg := executor.ExecutorConfig{
		Agent:               agent,
		MaxIterations:       15,
		MaxExecutionTime:    5 * time.Minute,
		EarlyStoppingMethod: "force",
	}

	if config != nil {
		if len(config.Tools) > 0 {
			cfg.Tools = config.Tools
		}
		if config.Memory != nil {
			cfg.Memory = config.Memory
		}
		if config.MaxIterations > 0 {
			cfg.MaxIterations = config.MaxIterations
		}
		if config.MaxExecutionTime > 0 {
			cfg.MaxExecutionTime = config.MaxExecutionTime
		}
		if config.EarlyStoppingMethod != "" {
			cfg.EarlyStoppingMethod = config.EarlyStoppingMethod
		}
		cfg.HandleParsingErrors = config.HandleParsingErrors
		cfg.ReturnIntermSteps = config.ReturnIntermSteps
		cfg.Verbose = config.Verbose
	}

	return executor.NewAgentExecutor(cfg)
}

// ==============================
// 便捷组合函数
// ==============================

// QuickCoTAgent 快速创建一个简单的 CoT Agent
func QuickCoTAgent(llmClient llm.Client) *cot.CoTAgent {
	return CoTAgent(llmClient, nil)
}

// QuickReActAgent 快速创建一个简单的 ReAct Agent
func QuickReActAgent(llmClient llm.Client, tools []interfaces.Tool) *react.ReActAgent {
	return ReActAgent(llmClient, tools, nil)
}

// QuickToTAgent 快速创建一个简单的 ToT Agent
func QuickToTAgent(llmClient llm.Client) *tot.ToTAgent {
	return ToTAgent(llmClient, nil)
}

// QuickPoTAgent 快速创建一个简单的 PoT Agent（Python）
func QuickPoTAgent(llmClient llm.Client) *pot.PoTAgent {
	return PoTAgent(llmClient, nil)
}

// QuickSoTAgent 快速创建一个简单的 SoT Agent
func QuickSoTAgent(llmClient llm.Client) *sot.SoTAgent {
	return SoTAgent(llmClient, nil)
}

// QuickGoTAgent 快速创建一个简单的 GoT Agent
func QuickGoTAgent(llmClient llm.Client) *got.GoTAgent {
	return GoTAgent(llmClient, nil)
}

// QuickMetaCoTAgent 快速创建一个简单的 MetaCoT Agent
func QuickMetaCoTAgent(llmClient llm.Client) *metacot.MetaCoTAgent {
	return MetaCoTAgent(llmClient, nil)
}
