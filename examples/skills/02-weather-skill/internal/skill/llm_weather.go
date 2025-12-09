// Package skill LLM 天气技能实现
//
// 扩展 WeatherSkill 支持 LLM 工具调用
// 让 LLM 自主决定调用哪个天气工具
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/config"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/llmtools"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/svc"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/types"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// LLMWeatherSkill LLM 驱动的天气技能
//
// 结合 LLM 的自然语言理解能力和天气工具
// 让 LLM 自主决定调用哪个工具来回答用户问题
type LLMWeatherSkill struct {
	name         string
	description  string
	version      string
	keywords     []string
	svcCtx       *svc.ServiceContext
	weatherTools *llmtools.WeatherTools
	toolMap      map[string]interfaces.Tool
	systemPrompt string
}

// NewLLMWeatherSkill 创建 LLM 天气技能
func NewLLMWeatherSkill(c *config.Config) *LLMWeatherSkill {
	// 创建服务上下文
	svcCtx := svc.NewServiceContext(c)

	// 创建天气工具集
	weatherTools := llmtools.NewWeatherTools(svcCtx)

	// 建立工具映射
	toolMap := make(map[string]interfaces.Tool)
	for _, tool := range weatherTools.GetTools() {
		toolMap[tool.Name()] = tool
	}

	return &LLMWeatherSkill{
		name:         c.Skill.Name + "-llm",
		description:  c.Skill.Description + " (LLM 增强版)",
		version:      c.Skill.Version,
		keywords:     c.Keywords,
		svcCtx:       svcCtx,
		weatherTools: weatherTools,
		toolMap:      toolMap,
		systemPrompt: buildSystemPrompt(c),
	}
}

// buildSystemPrompt 构建系统提示词
func buildSystemPrompt(c *config.Config) string {
	return fmt.Sprintf(`你是一个专业的天气助手。你可以使用以下工具来帮助用户查询天气信息：

1. get_weather: 获取指定城市的当前天气
2. get_forecast: 获取指定城市未来 1-7 天的天气预报
3. list_cities: 获取支持查询的城市列表

支持的城市: %v
默认城市: %s

当用户询问天气相关问题时，请使用相应的工具来获取准确信息。
- 如果用户询问当前天气，使用 get_weather 工具
- 如果用户询问未来天气或预报，使用 get_forecast 工具
- 如果用户询问支持哪些城市，使用 list_cities 工具

请始终调用工具获取真实数据，不要猜测天气信息。`,
		c.Skill.SupportedCities,
		c.Skill.DefaultCity,
	)
}

// Name 返回技能名称
func (s *LLMWeatherSkill) Name() string {
	return s.name
}

// Description 返回技能描述
func (s *LLMWeatherSkill) Description() string {
	return s.description
}

// Version 返回技能版本
func (s *LLMWeatherSkill) Version() string {
	return s.version
}

// Keywords 返回关键词列表
func (s *LLMWeatherSkill) Keywords() []string {
	return s.keywords
}

// CanHandle 评估技能处理能力
func (s *LLMWeatherSkill) CanHandle(ctx *RoutingContext) float64 {
	score := 0.0

	// 关键词匹配
	for _, keyword := range s.keywords {
		for _, queryKeyword := range ctx.Keywords {
			if keyword == queryKeyword {
				score += 0.2
			}
		}
	}

	// 如果有 LLM 客户端，提高匹配分数
	if s.svcCtx.LLMClient != nil {
		score += 0.1
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}

// Execute 执行技能
func (s *LLMWeatherSkill) Execute(ctx context.Context, input *types.SkillInput) *types.SkillOutput {
	startTime := time.Now()

	// 构建用户查询
	var query string
	if input.Action == "natural_query" {
		if q, ok := input.Args["query"].(string); ok {
			query = q
		}
	} else {
		// 将传统 action 转换为自然语言查询
		query = s.buildQueryFromAction(input)
	}

	if query == "" {
		return &types.SkillOutput{
			Success:   false,
			Error:     "查询内容为空",
			SkillName: s.name,
			Action:    input.Action,
			Duration:  time.Since(startTime).String(),
		}
	}

	// 使用 LLM 处理查询
	result, toolsUsed, err := s.processWithLLM(ctx, query)
	if err != nil {
		return &types.SkillOutput{
			Success:   false,
			Error:     err.Error(),
			SkillName: s.name,
			Action:    input.Action,
			Duration:  time.Since(startTime).String(),
		}
	}

	return &types.SkillOutput{
		Success:    true,
		Result:     result,
		SkillName:  s.name,
		Action:     strings.Join(toolsUsed, ","),
		Duration:   time.Since(startTime).String(),
		Confidence: 0.95,
	}
}

// buildQueryFromAction 将 action 转换为自然语言查询
func (s *LLMWeatherSkill) buildQueryFromAction(input *types.SkillInput) string {
	city := s.svcCtx.Config.Skill.DefaultCity
	if c, ok := input.Args["city"].(string); ok && c != "" {
		city = c
	}

	switch input.Action {
	case "get_weather":
		return fmt.Sprintf("查询%s的当前天气", city)
	case "get_forecast":
		days := 3
		if d, ok := input.Args["days"].(float64); ok {
			days = int(d)
		}
		return fmt.Sprintf("查询%s未来%d天的天气预报", city, days)
	case "list_cities":
		return "列出支持查询的城市"
	default:
		return ""
	}
}

// processWithLLM 使用 LLM 处理查询
func (s *LLMWeatherSkill) processWithLLM(ctx context.Context, query string) (interface{}, []string, error) {
	// 检查 LLM 客户端是否可用
	if s.svcCtx.LLMClient == nil {
		return nil, nil, fmt.Errorf("LLM 客户端未初始化，请检查 API Key 配置")
	}

	// 尝试使用工具调用
	toolCaller := llm.AsToolCaller(s.svcCtx.LLMClient)
	if toolCaller == nil {
		// 降级到普通聊天
		return s.processWithChat(ctx, query)
	}

	// 构建提示词
	prompt := s.buildPrompt(query)

	// 使用带工具的生成
	result, err := toolCaller.GenerateWithTools(ctx, prompt, s.weatherTools.GetTools())
	if err != nil {
		// 降级到普通聊天
		return s.processWithChat(ctx, query)
	}

	// 处理工具调用
	if len(result.ToolCalls) > 0 {
		return s.executeToolCalls(ctx, result.ToolCalls)
	}

	// LLM 选择直接回答
	return map[string]interface{}{
		"response": result.Content,
		"source":   "llm_direct",
	}, nil, nil
}

// processWithChat 使用普通聊天处理
func (s *LLMWeatherSkill) processWithChat(ctx context.Context, query string) (interface{}, []string, error) {
	resp, err := s.svcCtx.LLMClient.Chat(ctx, []llm.Message{
		llm.SystemMessage(s.systemPrompt),
		llm.UserMessage(query),
	})
	if err != nil {
		return nil, nil, err
	}

	return map[string]interface{}{
		"response": resp.Content,
		"source":   "llm_chat",
	}, nil, nil
}

// buildPrompt 构建完整提示词
func (s *LLMWeatherSkill) buildPrompt(query string) string {
	// 构建工具描述
	var toolDescs []string
	for _, tool := range s.weatherTools.GetTools() {
		toolDescs = append(toolDescs, fmt.Sprintf("- %s: %s", tool.Name(), tool.Description()))
	}

	return fmt.Sprintf(`%s

你可以使用以下工具:
%s

重要：请调用上述工具来回答问题，不要直接猜测天气信息。

用户问题: %s`, s.systemPrompt, strings.Join(toolDescs, "\n"), query)
}

// executeToolCalls 执行工具调用
func (s *LLMWeatherSkill) executeToolCalls(ctx context.Context, toolCalls []llm.ToolCall) (interface{}, []string, error) {
	var results []interface{}
	var toolsUsed []string

	for _, tc := range toolCalls {
		toolName := tc.Function.Name

		tool, exists := s.toolMap[toolName]
		if !exists {
			continue
		}

		// 解析参数
		var args map[string]interface{}
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}

		// 执行工具
		output, err := tool.Invoke(ctx, &interfaces.ToolInput{
			Args:    args,
			Context: ctx,
		})

		if err != nil {
			results = append(results, map[string]interface{}{
				"tool":  toolName,
				"error": err.Error(),
			})
		} else if output.Success {
			results = append(results, map[string]interface{}{
				"tool":   toolName,
				"result": output.Result,
			})
			toolsUsed = append(toolsUsed, toolName)
		} else {
			results = append(results, map[string]interface{}{
				"tool":  toolName,
				"error": output.Error,
			})
		}
	}

	if len(results) == 1 {
		return results[0], toolsUsed, nil
	}

	return results, toolsUsed, nil
}

// AskNatural 自然语言查询接口
//
// 允许用户使用自然语言询问天气问题
// LLM 会自动理解意图并调用相应的工具
func (s *LLMWeatherSkill) AskNatural(ctx context.Context, query string) *types.SkillOutput {
	return s.Execute(ctx, &types.SkillInput{
		Action: "natural_query",
		Args:   map[string]interface{}{"query": query},
	})
}

// GetLLMProvider 获取当前使用的 LLM 提供商
func (s *LLMWeatherSkill) GetLLMProvider() string {
	if s.svcCtx.LLMClient == nil {
		return "none"
	}
	return string(s.svcCtx.LLMClient.Provider())
}

// GetTools 获取工具列表
func (s *LLMWeatherSkill) GetTools() []interfaces.Tool {
	return s.weatherTools.GetTools()
}
