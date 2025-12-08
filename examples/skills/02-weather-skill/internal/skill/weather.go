// Package skill 技能定义
//
// Skill 作为独立的能力单元
// 包含完整的定义、注册、调用流程
package skill

import (
	"context"
	"sync"
	"time"

	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/config"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/handler"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/svc"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/types"
)

// ============================================================================
// Skill 接口定义
// ============================================================================

// Skill 技能接口
//
// 定义了技能的核心能力：
// - 元数据：名称、描述、版本
// - 路由：关键词匹配、能力评估
// - 执行：技能调用入口
type Skill interface {
	// 元数据
	Name() string
	Description() string
	Version() string
	Keywords() []string

	// 能力评估
	CanHandle(ctx *RoutingContext) float64

	// 执行入口
	Execute(ctx context.Context, input *types.SkillInput) *types.SkillOutput
}

// RoutingContext 路由上下文
type RoutingContext struct {
	Query    string   // 用户查询
	Keywords []string // 提取的关键词
	Intent   string   // 识别的意图
}

// ============================================================================
// WeatherSkill 天气技能实现
// ============================================================================

// WeatherSkill 天气技能
type WeatherSkill struct {
	name        string
	description string
	version     string
	keywords    []string
	handler     *handler.Handler
	svcCtx      *svc.ServiceContext
}

// NewWeatherSkill 创建天气技能
func NewWeatherSkill(c *config.Config) *WeatherSkill {
	// 创建服务上下文
	svcCtx := svc.NewServiceContext(c)

	// 创建处理器
	h := handler.NewHandler(svcCtx)

	return &WeatherSkill{
		name:        c.Skill.Name,
		description: c.Skill.Description,
		version:     c.Skill.Version,
		keywords:    c.Keywords,
		handler:     h,
		svcCtx:      svcCtx,
	}
}

// Name 返回技能名称
func (s *WeatherSkill) Name() string {
	return s.name
}

// Description 返回技能描述
func (s *WeatherSkill) Description() string {
	return s.description
}

// Version 返回技能版本
func (s *WeatherSkill) Version() string {
	return s.version
}

// Keywords 返回关键词列表
func (s *WeatherSkill) Keywords() []string {
	return s.keywords
}

// CanHandle 评估技能处理能力
//
// 返回 0-1 的分数，表示技能处理该请求的能力
// 基于关键词匹配和意图识别
func (s *WeatherSkill) CanHandle(ctx *RoutingContext) float64 {
	score := 0.0

	// 关键词匹配
	for _, keyword := range s.keywords {
		for _, queryKeyword := range ctx.Keywords {
			if keyword == queryKeyword {
				score += 0.2
			}
		}
	}

	// 限制最大分数
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// Execute 执行技能
func (s *WeatherSkill) Execute(ctx context.Context, input *types.SkillInput) *types.SkillOutput {
	return s.handler.HandleSkillInput(ctx, input)
}

// GetWeather 便捷方法：获取天气
func (s *WeatherSkill) GetWeather(ctx context.Context, city string) *types.SkillOutput {
	return s.handler.HandleGetWeather(ctx, city)
}

// GetForecast 便捷方法：获取预报
func (s *WeatherSkill) GetForecast(ctx context.Context, city string, days int) *types.SkillOutput {
	return s.handler.HandleGetForecast(ctx, city, days)
}

// GetServiceContext 获取服务上下文
//
// 用于需要直接访问服务上下文的场景
func (s *WeatherSkill) GetServiceContext() *svc.ServiceContext {
	return s.svcCtx
}

// ============================================================================
// SkillRegistry 技能注册表
// ============================================================================

// SkillRegistry 技能注册表
//
// 管理技能的注册、发现和调用
// 支持按名称获取、按关键词路由
type SkillRegistry struct {
	skills map[string]Skill
	mu     sync.RWMutex
}

// NewSkillRegistry 创建技能注册表
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills: make(map[string]Skill),
	}
}

// Register 注册技能
func (r *SkillRegistry) Register(skill Skill) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.skills[skill.Name()] = skill
	return nil
}

// Unregister 注销技能
func (r *SkillRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.skills, name)
}

// Get 获取技能
func (r *SkillRegistry) Get(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skill, ok := r.skills[name]
	return skill, ok
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

// Route 路由到最合适的技能
func (r *SkillRegistry) Route(ctx *RoutingContext) (Skill, float64) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var bestSkill Skill
	bestScore := 0.0

	for _, skill := range r.skills {
		score := skill.CanHandle(ctx)
		if score > bestScore {
			bestScore = score
			bestSkill = skill
		}
	}

	return bestSkill, bestScore
}

// Size 返回注册的技能数量
func (r *SkillRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.skills)
}

// ============================================================================
// SkillExecutor 技能执行器
// ============================================================================

// SkillExecutor 技能执行器
//
// 提供技能的统一执行入口
// 支持超时控制、错误处理
type SkillExecutor struct {
	registry *SkillRegistry
	timeout  time.Duration
}

// NewSkillExecutor 创建技能执行器
func NewSkillExecutor(registry *SkillRegistry) *SkillExecutor {
	return &SkillExecutor{
		registry: registry,
		timeout:  30 * time.Second,
	}
}

// SetTimeout 设置超时时间
func (e *SkillExecutor) SetTimeout(timeout time.Duration) {
	e.timeout = timeout
}

// ExecuteByName 按名称执行技能
func (e *SkillExecutor) ExecuteByName(ctx context.Context, name string, input *types.SkillInput) *types.SkillOutput {
	skill, ok := e.registry.Get(name)
	if !ok {
		return &types.SkillOutput{
			Success:   false,
			Error:     "skill not found: " + name,
			SkillName: name,
		}
	}

	// 创建超时上下文
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	return skill.Execute(execCtx, input)
}

// ExecuteByRouting 按路由执行技能
func (e *SkillExecutor) ExecuteByRouting(ctx context.Context, routingCtx *RoutingContext, input *types.SkillInput) *types.SkillOutput {
	skill, score := e.registry.Route(routingCtx)
	if skill == nil || score == 0 {
		return &types.SkillOutput{
			Success: false,
			Error:   "no matching skill found",
		}
	}

	// 创建超时上下文
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	output := skill.Execute(execCtx, input)
	output.Confidence = score
	return output
}
