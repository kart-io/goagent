// Package main 演示多技能协作系统
//
// 本示例展示：
// 1. 多 Skill 注册与发现
// 2. 基于上下文的 Skill 自动路由
// 3. 多 Skill 串行 / 并行执行
// 4. 结果聚合与错误处理
package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          多技能协作系统示例                                     ║")
	fmt.Println("║   展示 Skill 注册、路由、串行/并行执行、结果聚合                 ║")
	fmt.Println("╚═══════════════════════���════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 场景 1：多 Skill 注册与发现
	fmt.Println("【场景 1】多 Skill 注册与发现")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateSkillRegistration(ctx)

	// 场景 2：基于上下文的 Skill 自动路由
	fmt.Println("\n【场景 2】基于上下文的 Skill 自动路由")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateSkillRouting(ctx)

	// 场景 3：多 Skill 串行执行
	fmt.Println("\n【场景 3】多 Skill 串行执行")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateSequentialExecution(ctx)

	// 场景 4：多 Skill 并行执行
	fmt.Println("\n【场景 4】多 Skill 并行执行")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateParallelExecution(ctx)

	// 场景 5：结果聚合与错误处理
	fmt.Println("\n【场景 5】结果聚合与错误处理")
	fmt.Println("═════════════════════════════════════��══════════════════════════")
	demonstrateResultAggregation(ctx)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// ============================================================================
// Skill 核心定义
// ============================================================================

// Skill 技能接口
//
// 技能是一组相关工具和能力的封装
type Skill interface {
	// Name 返回技能名称
	Name() string

	// Description 返回技能描述
	Description() string

	// Category 返回技能类别
	Category() SkillCategory

	// Keywords 返回技能关键词，用于路由匹配
	Keywords() []string

	// GetTools 获取技能包含的所有工具
	GetTools() []interfaces.Tool

	// Execute 执行技能
	Execute(ctx context.Context, input *SkillInput) (*SkillOutput, error)

	// CanHandle 判断技能是否能处理指定上下文
	CanHandle(ctx *RoutingContext) float64
}

// SkillCategory 技能类别
type SkillCategory string

const (
	CategoryMath       SkillCategory = "math"       // 数学计算
	CategoryText       SkillCategory = "text"       // 文本处理
	CategoryData       SkillCategory = "data"       // 数据处理
	CategoryUtility    SkillCategory = "utility"    // 实用工具
	CategoryAnalytics  SkillCategory = "analytics"  // 分析统计
	CategoryConversion SkillCategory = "conversion" // 格式转换
)

// SkillInput 技能输入
type SkillInput struct {
	Action   string                 `json:"action"`   // 执行动作
	Args     map[string]interface{} `json:"args"`     // 输入参数
	Context  map[string]interface{} `json:"context"`  // 上下文信息
	Metadata map[string]interface{} `json:"metadata"` // 元数据
}

// SkillOutput 技能输出
type SkillOutput struct {
	SkillName   string                 `json:"skill_name"`  // 技能名称
	Action      string                 `json:"action"`      // 执行动作
	Result      interface{}            `json:"result"`      // 执行结果
	Success     bool                   `json:"success"`     // 是否成功
	Error       string                 `json:"error"`       // 错误信息
	Duration    time.Duration          `json:"duration"`    // 执行时长
	Metadata    map[string]interface{} `json:"metadata"`    // 元数据
	ToolsUsed   []string               `json:"tools_used"`  // 使用的工具
	Confidence  float64                `json:"confidence"`  // ���果置信度
	Suggestions []string               `json:"suggestions"` // 后续建议
}

// RoutingContext 路由上下文
type RoutingContext struct {
	Query       string                 // 用户查询
	Intent      string                 // 意图
	Category    SkillCategory          // 期望类别
	Keywords    []string               // 关键词
	Constraints map[string]interface{} // 约束条件
	History     []string               // 历史技能调用
}

// ============================================================================
// BaseSkill 基础技能实现
// ============================================================================

// BaseSkill 基础技能实现
type BaseSkill struct {
	name        string
	description string
	category    SkillCategory
	keywords    []string
	tools       []interfaces.Tool
	toolMap     map[string]interfaces.Tool
	actions     map[string]SkillAction
	mu          sync.RWMutex
}

// SkillAction 技能动作
type SkillAction func(ctx context.Context, args map[string]interface{}) (interface{}, error)

// NewBaseSkill 创建基础技能
func NewBaseSkill(name, description string, category SkillCategory, keywords []string) *BaseSkill {
	return &BaseSkill{
		name:        name,
		description: description,
		category:    category,
		keywords:    keywords,
		tools:       []interfaces.Tool{},
		toolMap:     make(map[string]interfaces.Tool),
		actions:     make(map[string]SkillAction),
	}
}

// Name 返回技能名称
func (s *BaseSkill) Name() string { return s.name }

// Description 返回技能描述
func (s *BaseSkill) Description() string { return s.description }

// Category 返回技能类别
func (s *BaseSkill) Category() SkillCategory { return s.category }

// Keywords 返回技能关键词
func (s *BaseSkill) Keywords() []string { return s.keywords }

// GetTools 获取所有工具
func (s *BaseSkill) GetTools() []interfaces.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tools
}

// AddTool 添加工具
func (s *BaseSkill) AddTool(tool interfaces.Tool) *BaseSkill {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, tool)
	s.toolMap[tool.Name()] = tool
	return s
}

// RegisterAction 注册动作
func (s *BaseSkill) RegisterAction(name string, action SkillAction) *BaseSkill {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions[name] = action
	return s
}

// Execute 执行技能
func (s *BaseSkill) Execute(ctx context.Context, input *SkillInput) (*SkillOutput, error) {
	startTime := time.Now()
	output := &SkillOutput{
		SkillName: s.name,
		Action:    input.Action,
		Metadata:  make(map[string]interface{}),
		ToolsUsed: []string{},
	}

	s.mu.RLock()
	action, exists := s.actions[input.Action]
	s.mu.RUnlock()

	if !exists {
		output.Success = false
		output.Error = fmt.Sprintf("unknown action: %s", input.Action)
		output.Duration = time.Since(startTime)
		return output, fmt.Errorf("action '%s' not found in skill '%s'", input.Action, s.name)
	}

	result, err := action(ctx, input.Args)
	output.Duration = time.Since(startTime)

	if err != nil {
		output.Success = false
		output.Error = err.Error()
		output.Confidence = 0
		return output, err
	}

	output.Result = result
	output.Success = true
	output.Confidence = 0.9
	return output, nil
}

// CanHandle 判断是否能处理上下文
func (s *BaseSkill) CanHandle(ctx *RoutingContext) float64 {
	score := 0.0

	// 类别匹配
	if ctx.Category == s.category {
		score += 0.4
	}

	// 关键词匹配
	queryLower := strings.ToLower(ctx.Query)
	for _, keyword := range s.keywords {
		if strings.Contains(queryLower, strings.ToLower(keyword)) {
			score += 0.15
		}
	}

	// 用户指定关键词匹配
	for _, ctxKeyword := range ctx.Keywords {
		for _, skillKeyword := range s.keywords {
			if strings.EqualFold(ctxKeyword, skillKeyword) {
				score += 0.1
			}
		}
	}

	// 限制最大分数
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// ============================================================================
// SkillRegistry 技能注册表
// ============================================================================

// SkillRegistry 技能注册表
type SkillRegistry struct {
	skills     map[string]Skill
	categories map[SkillCategory][]Skill
	mu         sync.RWMutex
}

// NewSkillRegistry 创建技能注册表
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills:     make(map[string]Skill),
		categories: make(map[SkillCategory][]Skill),
	}
}

// Register 注册技能
func (r *SkillRegistry) Register(skill Skill) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := skill.Name()
	if _, exists := r.skills[name]; exists {
		return fmt.Errorf("skill '%s' already registered", name)
	}

	r.skills[name] = skill
	r.categories[skill.Category()] = append(r.categories[skill.Category()], skill)
	return nil
}

// Unregister 注销技能
func (r *SkillRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	skill, exists := r.skills[name]
	if !exists {
		return fmt.Errorf("skill '%s' not found", name)
	}

	delete(r.skills, name)

	// 从类别中移除
	category := skill.Category()
	skills := r.categories[category]
	for i, s := range skills {
		if s.Name() == name {
			r.categories[category] = append(skills[:i], skills[i+1:]...)
			break
		}
	}

	return nil
}

// Get 获取技能
func (r *SkillRegistry) Get(name string) (Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skill, exists := r.skills[name]
	if !exists {
		return nil, fmt.Errorf("skill '%s' not found", name)
	}
	return skill, nil
}

// List 列出所有技能
func (r *SkillRegistry) List() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skills := make([]Skill, 0, len(r.skills))
	for _, skill := range r.skills {
		skills = append(skills, skill)
	}
	return skills
}

// ListByCategory 按类别列出技能
func (r *SkillRegistry) ListByCategory(category SkillCategory) []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.categories[category]
}

// FindByKeyword 按关键词查找技能
func (r *SkillRegistry) FindByKeyword(keyword string) []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matched []Skill
	keywordLower := strings.ToLower(keyword)

	for _, skill := range r.skills {
		for _, kw := range skill.Keywords() {
			if strings.Contains(strings.ToLower(kw), keywordLower) {
				matched = append(matched, skill)
				break
			}
		}
	}
	return matched
}

// Size 返回注册的技能数量
func (r *SkillRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}

// ============================================================================
// SkillRouter 技能路由器
// ============================================================================

// SkillRouter 技能路由器
type SkillRouter struct {
	registry   *SkillRegistry
	strategies []RoutingStrategy
	mu         sync.RWMutex
}

// RoutingStrategy 路由策略
type RoutingStrategy interface {
	Name() string
	Route(ctx *RoutingContext, skills []Skill) (*RoutingResult, error)
}

// RoutingResult 路由结果
type RoutingResult struct {
	SelectedSkills []ScoredSkill // 选中的技能及其分数
	Strategy       string        // 使用的策略
	Confidence     float64       // 整体置信度
}

// ScoredSkill 带分数的技能
type ScoredSkill struct {
	Skill Skill
	Score float64
}

// NewSkillRouter 创建技能路由器
func NewSkillRouter(registry *SkillRegistry) *SkillRouter {
	router := &SkillRouter{
		registry:   registry,
		strategies: []RoutingStrategy{},
	}

	// 添加默认路由策略
	router.AddStrategy(&KeywordRoutingStrategy{})
	router.AddStrategy(&CategoryRoutingStrategy{})
	router.AddStrategy(&ScoreRoutingStrategy{})

	return router
}

// AddStrategy 添加路由策略
func (r *SkillRouter) AddStrategy(strategy RoutingStrategy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strategies = append(r.strategies, strategy)
}

// Route 路由到最合适的技能
func (r *SkillRouter) Route(ctx *RoutingContext) (*RoutingResult, error) {
	r.mu.RLock()
	strategies := r.strategies
	r.mu.RUnlock()

	skills := r.registry.List()
	if len(skills) == 0 {
		return nil, fmt.Errorf("no skills available")
	}

	// 收集所有策略的结果
	var allResults []ScoredSkill
	for _, strategy := range strategies {
		result, err := strategy.Route(ctx, skills)
		if err != nil {
			continue
		}
		allResults = append(allResults, result.SelectedSkills...)
	}

	// 合并分数
	scoreMap := make(map[string]float64)
	skillMap := make(map[string]Skill)
	for _, ss := range allResults {
		name := ss.Skill.Name()
		scoreMap[name] += ss.Score
		skillMap[name] = ss.Skill
	}

	// 排序
	var sortedSkills []ScoredSkill
	for name, score := range scoreMap {
		sortedSkills = append(sortedSkills, ScoredSkill{
			Skill: skillMap[name],
			Score: score,
		})
	}
	sort.Slice(sortedSkills, func(i, j int) bool {
		return sortedSkills[i].Score > sortedSkills[j].Score
	})

	if len(sortedSkills) == 0 {
		return nil, fmt.Errorf("no matching skills found")
	}

	// 计算整体置信度
	totalScore := 0.0
	for _, ss := range sortedSkills {
		totalScore += ss.Score
	}

	return &RoutingResult{
		SelectedSkills: sortedSkills,
		Strategy:       "combined",
		Confidence:     sortedSkills[0].Score / totalScore,
	}, nil
}

// RouteTop 路由到分数最高的 N 个技能
func (r *SkillRouter) RouteTop(ctx *RoutingContext, n int) ([]Skill, error) {
	result, err := r.Route(ctx)
	if err != nil {
		return nil, err
	}

	if n > len(result.SelectedSkills) {
		n = len(result.SelectedSkills)
	}

	skills := make([]Skill, n)
	for i := 0; i < n; i++ {
		skills[i] = result.SelectedSkills[i].Skill
	}
	return skills, nil
}

// KeywordRoutingStrategy 关键词路由策略
type KeywordRoutingStrategy struct{}

func (s *KeywordRoutingStrategy) Name() string { return "keyword" }

func (s *KeywordRoutingStrategy) Route(ctx *RoutingContext, skills []Skill) (*RoutingResult, error) {
	var scored []ScoredSkill

	for _, skill := range skills {
		score := 0.0
		queryLower := strings.ToLower(ctx.Query)

		for _, keyword := range skill.Keywords() {
			if strings.Contains(queryLower, strings.ToLower(keyword)) {
				score += 0.3
			}
		}

		if score > 0 {
			scored = append(scored, ScoredSkill{Skill: skill, Score: score})
		}
	}

	return &RoutingResult{
		SelectedSkills: scored,
		Strategy:       s.Name(),
	}, nil
}

// CategoryRoutingStrategy 类别路由策略
type CategoryRoutingStrategy struct{}

func (s *CategoryRoutingStrategy) Name() string { return "category" }

func (s *CategoryRoutingStrategy) Route(ctx *RoutingContext, skills []Skill) (*RoutingResult, error) {
	var scored []ScoredSkill

	for _, skill := range skills {
		score := 0.0

		if ctx.Category != "" && skill.Category() == ctx.Category {
			score = 0.5
		}

		if score > 0 {
			scored = append(scored, ScoredSkill{Skill: skill, Score: score})
		}
	}

	return &RoutingResult{
		SelectedSkills: scored,
		Strategy:       s.Name(),
	}, nil
}

// ScoreRoutingStrategy 评分路由策略
type ScoreRoutingStrategy struct{}

func (s *ScoreRoutingStrategy) Name() string { return "score" }

func (s *ScoreRoutingStrategy) Route(ctx *RoutingContext, skills []Skill) (*RoutingResult, error) {
	var scored []ScoredSkill

	for _, skill := range skills {
		score := skill.CanHandle(ctx)
		if score > 0 {
			scored = append(scored, ScoredSkill{Skill: skill, Score: score})
		}
	}

	return &RoutingResult{
		SelectedSkills: scored,
		Strategy:       s.Name(),
	}, nil
}

// ============================================================================
// SkillExecutor 技能执行器
// ============================================================================

// SkillExecutor 技能执行器
type SkillExecutor struct {
	registry       *SkillRegistry
	router         *SkillRouter
	maxConcurrency int
	timeout        time.Duration
}

// NewSkillExecutor 创建技能执行器
func NewSkillExecutor(registry *SkillRegistry, router *SkillRouter) *SkillExecutor {
	return &SkillExecutor{
		registry:       registry,
		router:         router,
		maxConcurrency: 5,
		timeout:        30 * time.Second,
	}
}

// SetMaxConcurrency 设置最大并发数
func (e *SkillExecutor) SetMaxConcurrency(n int) *SkillExecutor {
	e.maxConcurrency = n
	return e
}

// SetTimeout 设置超时时间
func (e *SkillExecutor) SetTimeout(timeout time.Duration) *SkillExecutor {
	e.timeout = timeout
	return e
}

// ExecuteByName 按名称执行技能
func (e *SkillExecutor) ExecuteByName(ctx context.Context, skillName string, input *SkillInput) (*SkillOutput, error) {
	skill, err := e.registry.Get(skillName)
	if err != nil {
		return nil, err
	}

	// 创建超时上下文
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	return skill.Execute(execCtx, input)
}

// ExecuteByContext 根据上下文自动路由并执行
func (e *SkillExecutor) ExecuteByContext(ctx context.Context, routingCtx *RoutingContext, input *SkillInput) (*SkillOutput, error) {
	skills, err := e.router.RouteTop(routingCtx, 1)
	if err != nil {
		return nil, err
	}

	if len(skills) == 0 {
		return nil, fmt.Errorf("no matching skill found")
	}

	// 创建超时上下文
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	return skills[0].Execute(execCtx, input)
}

// ExecuteSequential 串行执行多个技能
func (e *SkillExecutor) ExecuteSequential(ctx context.Context, skills []Skill, inputs []*SkillInput) ([]*SkillOutput, error) {
	if len(skills) != len(inputs) {
		return nil, fmt.Errorf("skills and inputs length mismatch")
	}

	outputs := make([]*SkillOutput, len(skills))
	var lastOutput *SkillOutput

	for i, skill := range skills {
		input := inputs[i]

		// 如果需要，将上一个输出传递给下一个输入
		if lastOutput != nil && input.Context == nil {
			input.Context = make(map[string]interface{})
		}
		if lastOutput != nil {
			input.Context["previous_result"] = lastOutput.Result
			input.Context["previous_skill"] = lastOutput.SkillName
		}

		// 创建超时上下文
		execCtx, cancel := context.WithTimeout(ctx, e.timeout)

		output, err := skill.Execute(execCtx, input)
		cancel()

		if err != nil {
			outputs[i] = output
			return outputs, fmt.Errorf("skill '%s' failed: %w", skill.Name(), err)
		}

		outputs[i] = output
		lastOutput = output
	}

	return outputs, nil
}

// ExecuteParallel 并行执行多个技能
func (e *SkillExecutor) ExecuteParallel(ctx context.Context, skills []Skill, inputs []*SkillInput) ([]*SkillOutput, error) {
	if len(skills) != len(inputs) {
		return nil, fmt.Errorf("skills and inputs length mismatch")
	}

	outputs := make([]*SkillOutput, len(skills))
	errors := make([]error, len(skills))

	// 信号量控制并发
	sem := make(chan struct{}, e.maxConcurrency)
	var wg sync.WaitGroup

	for i := range skills {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// 获取信号量
			sem <- struct{}{}
			defer func() { <-sem }()

			// 创建超时上下文
			execCtx, cancel := context.WithTimeout(ctx, e.timeout)
			defer cancel()

			output, err := skills[idx].Execute(execCtx, inputs[idx])
			outputs[idx] = output
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// 收集错误
	var firstError error
	for _, err := range errors {
		if err != nil && firstError == nil {
			firstError = err
		}
	}

	return outputs, firstError
}

// ============================================================================
// SkillResultAggregator 结果聚合器
// ============================================================================

// AggregationMode 聚合模式
type AggregationMode string

const (
	AggregateMerge     AggregationMode = "merge"     // 合并所有结果
	AggregateBest      AggregationMode = "best"      // 选择最佳结果
	AggregateConsensus AggregationMode = "consensus" // 共识（多数一致）
	AggregateWeighted  AggregationMode = "weighted"  // 加权平均
	AggregateChain     AggregationMode = "chain"     // 链式（后者覆盖前者）
)

// SkillResultAggregator 结果聚合器
type SkillResultAggregator struct {
	mode AggregationMode
}

// NewSkillResultAggregator 创建结果聚合器
func NewSkillResultAggregator(mode AggregationMode) *SkillResultAggregator {
	return &SkillResultAggregator{mode: mode}
}

// AggregatedResult 聚合结果
type AggregatedResult struct {
	Mode            AggregationMode        `json:"mode"`
	Results         []interface{}          `json:"results"`
	MergedResult    interface{}            `json:"merged_result"`
	SuccessCount    int                    `json:"success_count"`
	FailureCount    int                    `json:"failure_count"`
	TotalDuration   time.Duration          `json:"total_duration"`
	AverageDuration time.Duration          `json:"average_duration"`
	Confidence      float64                `json:"confidence"`
	Errors          []string               `json:"errors"`
	SkillsUsed      []string               `json:"skills_used"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// Aggregate 聚合多个技能输出
func (a *SkillResultAggregator) Aggregate(outputs []*SkillOutput) *AggregatedResult {
	result := &AggregatedResult{
		Mode:       a.mode,
		Results:    make([]interface{}, 0),
		Errors:     make([]string, 0),
		SkillsUsed: make([]string, 0),
		Metadata:   make(map[string]interface{}),
	}

	totalConfidence := 0.0
	var totalDuration time.Duration

	for _, output := range outputs {
		if output == nil {
			continue
		}

		result.SkillsUsed = append(result.SkillsUsed, output.SkillName)
		totalDuration += output.Duration

		if output.Success {
			result.SuccessCount++
			result.Results = append(result.Results, output.Result)
			totalConfidence += output.Confidence
		} else {
			result.FailureCount++
			if output.Error != "" {
				result.Errors = append(result.Errors, fmt.Sprintf("[%s] %s", output.SkillName, output.Error))
			}
		}
	}

	result.TotalDuration = totalDuration
	if len(outputs) > 0 {
		result.AverageDuration = totalDuration / time.Duration(len(outputs))
	}
	if result.SuccessCount > 0 {
		result.Confidence = totalConfidence / float64(result.SuccessCount)
	}

	// 根据模式聚合
	switch a.mode {
	case AggregateMerge:
		result.MergedResult = a.mergeResults(outputs)
	case AggregateBest:
		result.MergedResult = a.selectBest(outputs)
	case AggregateConsensus:
		result.MergedResult = a.findConsensus(outputs)
	case AggregateWeighted:
		result.MergedResult = a.weightedAggregate(outputs)
	case AggregateChain:
		result.MergedResult = a.chainResults(outputs)
	default:
		result.MergedResult = result.Results
	}

	return result
}

// mergeResults 合并所有结果
func (a *SkillResultAggregator) mergeResults(outputs []*SkillOutput) interface{} {
	merged := map[string]interface{}{
		"results": []interface{}{},
	}

	for _, output := range outputs {
		if output != nil && output.Success {
			merged["results"] = append(merged["results"].([]interface{}), map[string]interface{}{
				"skill":  output.SkillName,
				"result": output.Result,
			})
		}
	}

	return merged
}

// selectBest 选择最佳结果
func (a *SkillResultAggregator) selectBest(outputs []*SkillOutput) interface{} {
	var best *SkillOutput
	maxConfidence := 0.0

	for _, output := range outputs {
		if output != nil && output.Success && output.Confidence > maxConfidence {
			best = output
			maxConfidence = output.Confidence
		}
	}

	if best != nil {
		return map[string]interface{}{
			"skill":      best.SkillName,
			"result":     best.Result,
			"confidence": best.Confidence,
		}
	}
	return nil
}

// findConsensus 找到共识结果
func (a *SkillResultAggregator) findConsensus(outputs []*SkillOutput) interface{} {
	resultCounts := make(map[string]int)
	resultMap := make(map[string]interface{})

	for _, output := range outputs {
		if output != nil && output.Success {
			key := fmt.Sprintf("%v", output.Result)
			resultCounts[key]++
			resultMap[key] = output.Result
		}
	}

	maxCount := 0
	var consensus interface{}

	for key, count := range resultCounts {
		if count > maxCount {
			maxCount = count
			consensus = resultMap[key]
		}
	}

	return map[string]interface{}{
		"consensus": consensus,
		"votes":     maxCount,
		"total":     len(outputs),
	}
}

// weightedAggregate 加权聚合
func (a *SkillResultAggregator) weightedAggregate(outputs []*SkillOutput) interface{} {
	type weightedResult struct {
		Skill      string
		Result     interface{}
		Weight     float64
		Confidence float64
	}

	var results []weightedResult
	totalWeight := 0.0

	for _, output := range outputs {
		if output != nil && output.Success {
			weight := output.Confidence
			results = append(results, weightedResult{
				Skill:      output.SkillName,
				Result:     output.Result,
				Weight:     weight,
				Confidence: output.Confidence,
			})
			totalWeight += weight
		}
	}

	// 归一化权重
	for i := range results {
		results[i].Weight = results[i].Weight / totalWeight
	}

	return map[string]interface{}{
		"weighted_results": results,
		"total_weight":     totalWeight,
	}
}

// chainResults 链式结果
func (a *SkillResultAggregator) chainResults(outputs []*SkillOutput) interface{} {
	chain := []map[string]interface{}{}
	var finalResult interface{}

	for _, output := range outputs {
		if output != nil && output.Success {
			chain = append(chain, map[string]interface{}{
				"skill":  output.SkillName,
				"result": output.Result,
			})
			finalResult = output.Result
		}
	}

	return map[string]interface{}{
		"chain":        chain,
		"final_result": finalResult,
	}
}

// ============================================================================
// 具体技能实现
// ============================================================================

// createMathSkill 创建数学计算技能
func createMathSkill() Skill {
	skill := NewBaseSkill(
		"math",
		"执行数学计算，包括基础运算、统计和数学函数",
		CategoryMath,
		[]string{"计算", "数学", "加减乘除", "统计", "求和", "平均", "math", "calculate"},
	)

	// 添加计算器工具
	calcTool := tools.NewFunctionTool(
		"calculator",
		"基础数学计算器",
		`{"type": "object", "properties": {"operation": {"type": "string"}, "a": {"type": "number"}, "b": {"type": "number"}}}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			op := args["operation"].(string)
			a := args["a"].(float64)
			b := args["b"].(float64)

			var result float64
			switch op {
			case "add":
				result = a + b
			case "subtract":
				result = a - b
			case "multiply":
				result = a * b
			case "divide":
				if b == 0 {
					return nil, fmt.Errorf("division by zero")
				}
				result = a / b
			case "power":
				result = math.Pow(a, b)
			case "mod":
				result = math.Mod(a, b)
			default:
				return nil, fmt.Errorf("unknown operation: %s", op)
			}
			return map[string]interface{}{"result": result, "operation": op}, nil
		},
	)
	skill.AddTool(calcTool)

	// 注册动作
	skill.RegisterAction("calculate", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		output, err := calcTool.Invoke(ctx, &interfaces.ToolInput{Args: args})
		if err != nil {
			return nil, err
		}
		return output.Result, nil
	})

	skill.RegisterAction("statistics", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		data, ok := args["data"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("data must be an array")
		}

		numbers := make([]float64, len(data))
		for i, v := range data {
			switch n := v.(type) {
			case float64:
				numbers[i] = n
			case int:
				numbers[i] = float64(n)
			default:
				return nil, fmt.Errorf("invalid number at index %d", i)
			}
		}

		// 计算统计值
		sum := 0.0
		for _, n := range numbers {
			sum += n
		}
		avg := sum / float64(len(numbers))

		// 计算标准差
		variance := 0.0
		for _, n := range numbers {
			diff := n - avg
			variance += diff * diff
		}
		stddev := math.Sqrt(variance / float64(len(numbers)))

		// 排序以找到中位数
		sorted := make([]float64, len(numbers))
		copy(sorted, numbers)
		sort.Float64s(sorted)

		var median float64
		mid := len(sorted) / 2
		if len(sorted)%2 == 0 {
			median = (sorted[mid-1] + sorted[mid]) / 2
		} else {
			median = sorted[mid]
		}

		return map[string]interface{}{
			"count":   len(numbers),
			"sum":     sum,
			"average": avg,
			"median":  median,
			"stddev":  stddev,
			"min":     sorted[0],
			"max":     sorted[len(sorted)-1],
		}, nil
	})

	return skill
}

// createTextSkill 创建文本处理技能
func createTextSkill() Skill {
	skill := NewBaseSkill(
		"text",
		"处理文本，包括转换、分析和格式化",
		CategoryText,
		[]string{"文本", "字符串", "转换", "大小写", "text", "string", "format"},
	)

	// 注册动作
	skill.RegisterAction("transform", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		text, _ := args["text"].(string)
		operation, _ := args["operation"].(string)

		var result string
		switch operation {
		case "uppercase":
			result = strings.ToUpper(text)
		case "lowercase":
			result = strings.ToLower(text)
		case "reverse":
			runes := []rune(text)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			result = string(runes)
		case "trim":
			result = strings.TrimSpace(text)
		case "title":
			result = cases.Title(language.Und).String(strings.ToLower(text))
		default:
			return nil, fmt.Errorf("unknown operation: %s", operation)
		}

		return map[string]interface{}{
			"original":  text,
			"result":    result,
			"operation": operation,
		}, nil
	})

	skill.RegisterAction("analyze", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		text, _ := args["text"].(string)

		words := strings.Fields(text)
		lines := strings.Split(text, "\n")

		// 字符频率
		charFreq := make(map[rune]int)
		for _, r := range text {
			charFreq[r]++
		}

		return map[string]interface{}{
			"length":      len(text),
			"word_count":  len(words),
			"line_count":  len(lines),
			"char_count":  len([]rune(text)),
			"has_numbers": strings.ContainsAny(text, "0123456789"),
			"has_special": strings.ContainsAny(text, "!@#$%^&*()"),
		}, nil
	})

	skill.RegisterAction("split", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		text, _ := args["text"].(string)
		separator, _ := args["separator"].(string)

		if separator == "" {
			separator = " "
		}

		parts := strings.Split(text, separator)
		return map[string]interface{}{
			"parts": parts,
			"count": len(parts),
		}, nil
	})

	return skill
}

// createDataSkill 创建数据处理技能
func createDataSkill() Skill {
	skill := NewBaseSkill(
		"data",
		"处理数据，包括转换、过滤和聚合",
		CategoryData,
		[]string{"数据", "处理", "转换", "过滤", "聚合", "data", "process", "filter"},
	)

	// 注册动作
	skill.RegisterAction("filter", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		data, _ := args["data"].([]interface{})
		condition, _ := args["condition"].(string)
		threshold, _ := args["threshold"].(float64)

		var filtered []interface{}
		for _, item := range data {
			switch v := item.(type) {
			case float64:
				switch condition {
				case "gt":
					if v > threshold {
						filtered = append(filtered, v)
					}
				case "lt":
					if v < threshold {
						filtered = append(filtered, v)
					}
				case "eq":
					if v == threshold {
						filtered = append(filtered, v)
					}
				case "gte":
					if v >= threshold {
						filtered = append(filtered, v)
					}
				case "lte":
					if v <= threshold {
						filtered = append(filtered, v)
					}
				}
			default:
				filtered = append(filtered, item)
			}
		}

		return map[string]interface{}{
			"original_count": len(data),
			"filtered_count": len(filtered),
			"filtered":       filtered,
		}, nil
	})

	skill.RegisterAction("transform", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		data, _ := args["data"].([]interface{})
		operation, _ := args["operation"].(string)
		factor, _ := args["factor"].(float64)

		if factor == 0 {
			factor = 1
		}

		var transformed []interface{}
		for _, item := range data {
			if v, ok := item.(float64); ok {
				switch operation {
				case "multiply":
					transformed = append(transformed, v*factor)
				case "divide":
					if factor != 0 {
						transformed = append(transformed, v/factor)
					}
				case "add":
					transformed = append(transformed, v+factor)
				case "subtract":
					transformed = append(transformed, v-factor)
				case "square":
					transformed = append(transformed, v*v)
				case "sqrt":
					transformed = append(transformed, math.Sqrt(v))
				default:
					transformed = append(transformed, v)
				}
			}
		}

		return map[string]interface{}{
			"original":    data,
			"transformed": transformed,
			"operation":   operation,
		}, nil
	})

	skill.RegisterAction("aggregate", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		data, _ := args["data"].([]interface{})
		groupBy, _ := args["group_by"].(string)

		if groupBy == "" {
			// 简单聚合
			sum := 0.0
			count := 0
			for _, item := range data {
				if v, ok := item.(float64); ok {
					sum += v
					count++
				}
			}

			return map[string]interface{}{
				"sum":     sum,
				"count":   count,
				"average": sum / float64(count),
			}, nil
		}

		// 分组聚合
		groups := make(map[string][]float64)
		for _, item := range data {
			if m, ok := item.(map[string]interface{}); ok {
				key := fmt.Sprintf("%v", m[groupBy])
				if val, exists := m["value"]; exists {
					if v, ok := val.(float64); ok {
						groups[key] = append(groups[key], v)
					}
				}
			}
		}

		aggregated := make(map[string]interface{})
		for key, values := range groups {
			sum := 0.0
			for _, v := range values {
				sum += v
			}
			aggregated[key] = map[string]interface{}{
				"sum":     sum,
				"count":   len(values),
				"average": sum / float64(len(values)),
			}
		}

		return map[string]interface{}{
			"groups": aggregated,
		}, nil
	})

	return skill
}

// createUtilitySkill 创建实用工具技能
func createUtilitySkill() Skill {
	skill := NewBaseSkill(
		"utility",
		"实用工具，包括日期时间、格式化等",
		CategoryUtility,
		[]string{"工具", "日期", "时间", "格式", "utility", "date", "time", "format"},
	)

	// 注册动作
	skill.RegisterAction("datetime", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		format, _ := args["format"].(string)
		now := time.Now()

		var result string
		switch format {
		case "date":
			result = now.Format("2006-01-02")
		case "time":
			result = now.Format("15:04:05")
		case "full":
			result = now.Format("2006-01-02 15:04:05")
		case "iso":
			result = now.Format(time.RFC3339)
		case "unix":
			return map[string]interface{}{
				"unix":      now.Unix(),
				"unix_nano": now.UnixNano(),
			}, nil
		default:
			result = now.Format("2006-01-02 15:04:05")
		}

		return map[string]interface{}{
			"formatted": result,
			"timezone":  now.Location().String(),
		}, nil
	})

	skill.RegisterAction("generate_id", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		prefix, _ := args["prefix"].(string)

		id := fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
		return map[string]interface{}{
			"id":        id,
			"timestamp": time.Now().Unix(),
		}, nil
	})

	skill.RegisterAction("format_number", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		number, _ := args["number"].(float64)
		precision, ok := args["precision"].(float64)
		if !ok {
			precision = 2
		}

		formatted := fmt.Sprintf("%.*f", int(precision), number)
		return map[string]interface{}{
			"original":  number,
			"formatted": formatted,
			"precision": precision,
		}, nil
	})

	return skill
}

// createErrorProneSkill 创建会产生错误的技能（用于测试错误处理）
func createErrorProneSkill() Skill {
	skill := NewBaseSkill(
		"error_prone",
		"用于测试错误处理的技能",
		CategoryUtility,
		[]string{"error", "test", "fail"},
	)

	skill.RegisterAction("success", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{
			"status": "success",
			"data":   "operation completed",
		}, nil
	})

	skill.RegisterAction("fail", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("intentional error for testing")
	})

	skill.RegisterAction("timeout", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		select {
		case <-time.After(10 * time.Second):
			return map[string]interface{}{"status": "completed"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	skill.RegisterAction("random_fail", func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		if time.Now().UnixNano()%2 == 0 {
			return nil, fmt.Errorf("random failure")
		}
		return map[string]interface{}{"status": "success"}, nil
	})

	return skill
}

// ============================================================================
// 场景演示函数
// ============================================================================

func demonstrateSkillRegistration(ctx context.Context) {
	fmt.Println("\n场景描述: 展示技能的注册、发现和管理")
	fmt.Println()

	// 创建注册表
	registry := NewSkillRegistry()
	fmt.Println("✓ 创建技能注册表")

	// 注册技能
	fmt.Println("\n注册技能:")
	fmt.Println("────────────────────────────────────────")

	skills := []Skill{
		createMathSkill(),
		createTextSkill(),
		createDataSkill(),
		createUtilitySkill(),
	}

	for _, skill := range skills {
		if err := registry.Register(skill); err != nil {
			fmt.Printf("  ✗ %s: %v\n", skill.Name(), err)
		} else {
			fmt.Printf("  ✓ %s [%s]: %s\n", skill.Name(), skill.Category(), skill.Description())
		}
	}

	// 查看注册状态
	fmt.Println("\n注册表状态:")
	fmt.Println("────────────────────────────────────────")
	fmt.Printf("  已注册技能数: %d\n", registry.Size())

	// 按类别列出
	fmt.Println("\n按类别列出技能:")
	fmt.Println("────────────────────────────────────────")

	categories := []SkillCategory{CategoryMath, CategoryText, CategoryData, CategoryUtility}
	for _, cat := range categories {
		catSkills := registry.ListByCategory(cat)
		names := make([]string, len(catSkills))
		for i, s := range catSkills {
			names[i] = s.Name()
		}
		fmt.Printf("  [%s]: %v\n", cat, names)
	}

	// 按关键词查找
	fmt.Println("\n按关键词查找技能:")
	fmt.Println("────────────────────────────────────────")

	keywords := []string{"计算", "文本", "数据", "时间"}
	for _, kw := range keywords {
		found := registry.FindByKeyword(kw)
		names := make([]string, len(found))
		for i, s := range found {
			names[i] = s.Name()
		}
		fmt.Printf("  '%s': %v\n", kw, names)
	}

	// 获取技能详情
	fmt.Println("\n获取技能详情:")
	fmt.Println("────────────────────────────────────────")

	if skill, err := registry.Get("math"); err == nil {
		fmt.Printf("  名称: %s\n", skill.Name())
		fmt.Printf("  类别: %s\n", skill.Category())
		fmt.Printf("  描述: %s\n", skill.Description())
		fmt.Printf("  关键词: %v\n", skill.Keywords())
		fmt.Printf("  工具数: %d\n", len(skill.GetTools()))
	}

	// 注销技能
	fmt.Println("\n注销技能:")
	fmt.Println("────────────────────────────────────────")

	if err := registry.Unregister("utility"); err != nil {
		fmt.Printf("  ✗ 注销失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 已注销 'utility'\n")
		fmt.Printf("  当前技能数: %d\n", registry.Size())
	}

	// 尝试重复注册
	fmt.Println("\n重复注册测试:")
	fmt.Println("──────────────────────────��─────────────")

	if err := registry.Register(createMathSkill()); err != nil {
		fmt.Printf("  ✓ 重复注册被拒绝: %v\n", err)
	}
}

func demonstrateSkillRouting(ctx context.Context) {
	fmt.Println("\n场景描述: 展示基于上下文的自动技能路由")
	fmt.Println()

	// 创建注册表和路由器
	registry := NewSkillRegistry()
	_ = registry.Register(createMathSkill())
	_ = registry.Register(createTextSkill())
	_ = registry.Register(createDataSkill())
	_ = registry.Register(createUtilitySkill())

	router := NewSkillRouter(registry)

	fmt.Println("✓ 创建技能路由器")
	fmt.Println("  策略: keyword, category, score")

	// 测试不同的路由上下文
	fmt.Println("\n路由测试:")
	fmt.Println("────────────────────────────────────────")

	testCases := []struct {
		query    string
		category SkillCategory
		keywords []string
	}{
		{"我需要计算两个数的和", CategoryMath, []string{"计算"}},
		{"请把这段文字转成大写", CategoryText, []string{"转换"}},
		{"处理这些数据并求平均值", CategoryData, []string{"数据"}},
		{"获取当前时间", CategoryUtility, []string{"时间"}},
		{"分析文本长度", CategoryText, []string{}},
	}

	for _, tc := range testCases {
		routingCtx := &RoutingContext{
			Query:    tc.query,
			Category: tc.category,
			Keywords: tc.keywords,
		}

		result, err := router.Route(routingCtx)
		if err != nil {
			fmt.Printf("  ✗ '%s': %v\n", tc.query, err)
			continue
		}

		fmt.Printf("\n  查询: '%s'\n", tc.query)
		fmt.Printf("  期望类别: %s\n", tc.category)
		fmt.Printf("  路由结果 (Top 3):\n")

		displayCount := 3
		if len(result.SelectedSkills) < displayCount {
			displayCount = len(result.SelectedSkills)
		}

		for i := 0; i < displayCount; i++ {
			ss := result.SelectedSkills[i]
			fmt.Printf("    %d. %s (分数: %.2f)\n", i+1, ss.Skill.Name(), ss.Score)
		}
	}

	// 自动执行
	fmt.Println("\n自动路由并执行:")
	fmt.Println("────────────────────────────────────────")

	executor := NewSkillExecutor(registry, router)

	routingCtx := &RoutingContext{
		Query:    "计算 100 + 200",
		Category: CategoryMath,
		Keywords: []string{"计算", "加法"},
	}

	input := &SkillInput{
		Action: "calculate",
		Args: map[string]interface{}{
			"operation": "add",
			"a":         100.0,
			"b":         200.0,
		},
	}

	output, err := executor.ExecuteByContext(ctx, routingCtx, input)
	if err != nil {
		fmt.Printf("  ✗ 执行失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 自动路由到: %s\n", output.SkillName)
		fmt.Printf("    执行结果: %v\n", output.Result)
		fmt.Printf("    置信度: %.2f\n", output.Confidence)
	}
}

func demonstrateSequentialExecution(ctx context.Context) {
	fmt.Println("\n场景描述: 展示多技能串行执行")
	fmt.Println()

	// 创建注册表和执行器
	registry := NewSkillRegistry()
	_ = registry.Register(createMathSkill())
	_ = registry.Register(createTextSkill())
	_ = registry.Register(createDataSkill())

	executor := NewSkillExecutor(registry, NewSkillRouter(registry))

	// 技能链: math -> data -> text
	fmt.Println("技能链配置:")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  1. math (统计计算)")
	fmt.Println("  2. data (数据转换)")
	fmt.Println("  3. text (文本格式化)")

	mathSkill, _ := registry.Get("math")
	dataSkill, _ := registry.Get("data")
	textSkill, _ := registry.Get("text")

	skills := []Skill{mathSkill, dataSkill, textSkill}

	inputs := []*SkillInput{
		{
			Action: "statistics",
			Args: map[string]interface{}{
				"data": []interface{}{10.0, 20.0, 30.0, 40.0, 50.0},
			},
		},
		{
			Action: "transform",
			Args: map[string]interface{}{
				"data":      []interface{}{10.0, 20.0, 30.0, 40.0, 50.0},
				"operation": "square",
			},
		},
		{
			Action: "transform",
			Args: map[string]interface{}{
				"text":      "result ready",
				"operation": "uppercase",
			},
		},
	}

	fmt.Println("\n串行执行:")
	fmt.Println("────────────────────────────────────────")

	outputs, err := executor.ExecuteSequential(ctx, skills, inputs)
	if err != nil {
		fmt.Printf("  ✗ 执行出错: %v\n", err)
	}

	for i, output := range outputs {
		if output == nil {
			continue
		}
		fmt.Printf("\n  [Step %d] %s:\n", i+1, output.SkillName)
		fmt.Printf("    动作: %s\n", output.Action)
		fmt.Printf("    成功: %v\n", output.Success)
		if output.Success {
			fmt.Printf("    结果: %v\n", output.Result)
		} else {
			fmt.Printf("    错误: %s\n", output.Error)
		}
		fmt.Printf("    耗时: %v\n", output.Duration)
	}

	// 带数据传递的串行执行
	fmt.Println("\n带数据传递的串行执行:")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  (前一步结果自动传递到下一步上下文)")

	// 简化版：手动演示数据传递
	fmt.Println("\n  Step 1: 计算统计值")
	mathOut, _ := mathSkill.Execute(ctx, &SkillInput{
		Action: "statistics",
		Args:   map[string]interface{}{"data": []interface{}{1.0, 2.0, 3.0, 4.0, 5.0}},
	})
	fmt.Printf("    统计结果: %v\n", mathOut.Result)

	fmt.Println("\n  Step 2: 使用统计结果进行数据转换")
	if stats, ok := mathOut.Result.(map[string]interface{}); ok {
		avg := stats["average"].(float64)
		dataOut, _ := dataSkill.Execute(ctx, &SkillInput{
			Action: "transform",
			Args: map[string]interface{}{
				"data":      []interface{}{avg, avg * 2, avg * 3},
				"operation": "multiply",
				"factor":    10.0,
			},
		})
		fmt.Printf("    转换结果: %v\n", dataOut.Result)

		fmt.Println("\n  Step 3: 格式化输出")
		textOut, _ := textSkill.Execute(ctx, &SkillInput{
			Action: "transform",
			Args: map[string]interface{}{
				"text":      fmt.Sprintf("average: %.2f", avg),
				"operation": "uppercase",
			},
		})
		fmt.Printf("    格式化结果: %v\n", textOut.Result)
	}
}

func demonstrateParallelExecution(ctx context.Context) {
	fmt.Println("\n场景描述: 展示多技能并行执行")
	fmt.Println()

	// 创建注册表和执行器
	registry := NewSkillRegistry()
	_ = registry.Register(createMathSkill())
	_ = registry.Register(createTextSkill())
	_ = registry.Register(createDataSkill())
	_ = registry.Register(createUtilitySkill())

	executor := NewSkillExecutor(registry, NewSkillRouter(registry))
	executor.SetMaxConcurrency(4)

	fmt.Println("并行执行配置:")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  最大并发: 4")
	fmt.Println("  技能: math, text, data, utility")

	mathSkill, _ := registry.Get("math")
	textSkill, _ := registry.Get("text")
	dataSkill, _ := registry.Get("data")
	utilSkill, _ := registry.Get("utility")

	skills := []Skill{mathSkill, textSkill, dataSkill, utilSkill}

	inputs := []*SkillInput{
		{Action: "calculate", Args: map[string]interface{}{"operation": "multiply", "a": 7.0, "b": 8.0}},
		{Action: "transform", Args: map[string]interface{}{"text": "parallel execution", "operation": "uppercase"}},
		{Action: "aggregate", Args: map[string]interface{}{"data": []interface{}{1.0, 2.0, 3.0, 4.0, 5.0}}},
		{Action: "datetime", Args: map[string]interface{}{"format": "full"}},
	}

	fmt.Println("\n���行执行:")
	fmt.Println("────────────────────────────────────────")

	startTime := time.Now()
	outputs, err := executor.ExecuteParallel(ctx, skills, inputs)
	totalTime := time.Since(startTime)

	if err != nil {
		fmt.Printf("  ⚠ 部分执行出错: %v\n", err)
	}

	for _, output := range outputs {
		if output == nil {
			continue
		}
		status := "✓"
		if !output.Success {
			status = "✗"
		}
		fmt.Printf("  %s %s: %v (耗时: %v)\n", status, output.SkillName, output.Result, output.Duration)
	}

	fmt.Printf("\n  总耗时: %v\n", totalTime)
	fmt.Printf("  并行效率: 所有任务同时执行\n")

	// 对比串行执行时间
	fmt.Println("\n与串行执行对比:")
	fmt.Println("────────────────────────────────────────")

	serialStartTime := time.Now()
	_, _ = executor.ExecuteSequential(ctx, skills, inputs)
	serialTime := time.Since(serialStartTime)

	fmt.Printf("  串行耗时: %v\n", serialTime)
	fmt.Printf("  并行耗时: %v\n", totalTime)
	if serialTime > totalTime {
		speedup := float64(serialTime) / float64(totalTime)
		fmt.Printf("  加速比: %.2fx\n", speedup)
	}
}

func demonstrateResultAggregation(ctx context.Context) {
	fmt.Println("\n场景描述: 展示结果聚合与错误处理")
	fmt.Println()

	// 创建注册表
	registry := NewSkillRegistry()
	_ = registry.Register(createMathSkill())
	_ = registry.Register(createTextSkill())
	_ = registry.Register(createDataSkill())
	_ = registry.Register(createErrorProneSkill())

	executor := NewSkillExecutor(registry, NewSkillRouter(registry))

	// 测试不同的聚合模式
	fmt.Println("聚合模式测试:")
	fmt.Println("════════════════════════════════════════")

	// 准备测试数据
	mathSkill, _ := registry.Get("math")
	textSkill, _ := registry.Get("text")
	dataSkill, _ := registry.Get("data")

	skills := []Skill{mathSkill, textSkill, dataSkill}
	inputs := []*SkillInput{
		{Action: "calculate", Args: map[string]interface{}{"operation": "add", "a": 10.0, "b": 20.0}},
		{Action: "analyze", Args: map[string]interface{}{"text": "hello world example"}},
		{Action: "aggregate", Args: map[string]interface{}{"data": []interface{}{1.0, 2.0, 3.0}}},
	}

	outputs, _ := executor.ExecuteParallel(ctx, skills, inputs)

	// 测试各种聚合模式
	modes := []AggregationMode{AggregateMerge, AggregateBest, AggregateConsensus, AggregateWeighted, AggregateChain}

	for _, mode := range modes {
		fmt.Printf("\n【%s ���式】\n", mode)
		fmt.Println("────────────────────────────────────────")

		aggregator := NewSkillResultAggregator(mode)
		result := aggregator.Aggregate(outputs)

		fmt.Printf("  成功数: %d, 失败数: %d\n", result.SuccessCount, result.FailureCount)
		fmt.Printf("  平均耗时: %v\n", result.AverageDuration)
		fmt.Printf("  置信度: %.2f\n", result.Confidence)
		fmt.Printf("  使用技能: %v\n", result.SkillsUsed)
		fmt.Printf("  聚合结果: %v\n", result.MergedResult)
	}

	// 错误处理测试
	fmt.Println("\n\n错误处理测试:")
	fmt.Println("════════════════════════════════════════")

	errorSkill, _ := registry.Get("error_prone")

	// 正常执行
	fmt.Println("\n1. 正常执行:")
	fmt.Println("────────────────────────────────────────")
	output, err := errorSkill.Execute(ctx, &SkillInput{Action: "success"})
	if err != nil {
		fmt.Printf("  ✗ 执行失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 执行成功: %v\n", output.Result)
	}

	// 预期失败
	fmt.Println("\n2. 预期失败:")
	fmt.Println("────────────────────────────────────────")
	output, err = errorSkill.Execute(ctx, &SkillInput{Action: "fail"})
	if err != nil {
		fmt.Printf("  ✓ 错误被正确捕获: %v\n", err)
	} else {
		fmt.Printf("  ✗ 应该失败但成功了: %v\n", output.Result)
	}

	// 超时处理
	fmt.Println("\n3. 超时处理:")
	fmt.Println("────────────────────────────────────────")

	shortExecutor := NewSkillExecutor(registry, NewSkillRouter(registry))
	shortExecutor.SetTimeout(100 * time.Millisecond)

	output, err = shortExecutor.ExecuteByName(ctx, "error_prone", &SkillInput{Action: "timeout"})
	if err != nil {
		fmt.Printf("  ✓ 超时被正确处理: %v\n", err)
	} else {
		fmt.Printf("  ✗ 应该超时但成功了: %v\n", output.Result)
	}

	// 混合执行（部分成功部分失败）
	fmt.Println("\n4. 混合执行（部分成功部分失败）:")
	fmt.Println("────────────────────────────────────────")

	mixedSkills := []Skill{mathSkill, errorSkill, textSkill}
	mixedInputs := []*SkillInput{
		{Action: "calculate", Args: map[string]interface{}{"operation": "add", "a": 1.0, "b": 2.0}},
		{Action: "fail", Args: nil},
		{Action: "transform", Args: map[string]interface{}{"text": "hello", "operation": "uppercase"}},
	}

	mixedOutputs, _ := executor.ExecuteParallel(ctx, mixedSkills, mixedInputs)

	aggregator := NewSkillResultAggregator(AggregateMerge)
	result := aggregator.Aggregate(mixedOutputs)

	fmt.Printf("  成功数: %d, 失败数: %d\n", result.SuccessCount, result.FailureCount)
	fmt.Printf("  错误信息: %v\n", result.Errors)
	fmt.Printf("  成功结果: %v\n", result.Results)

	// 错误恢复建议
	fmt.Println("\n5. 错误恢复建议:")
	fmt.Println("────────────────────────────────────────")
	fmt.Println("  - 对于临时失败: 建议重试")
	fmt.Println("  - 对于超时: 增加超时时间或优化技能")
	fmt.Println("  - 对于持续失败: 检查输入参数或技能实现")
	fmt.Println("  - 对于部分失败: 使用聚合器收集成功结果")
}
