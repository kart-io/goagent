// Package handler 请求处理层
//
// Handler 层只负责：
// 1. 请求解析和验证
// 2. 调用 Logic 层处理业务
// 3. 响应格式化和返回
//
// 禁止在 Handler 层编写业务逻辑
// 保持 Handler 层轻量简洁
package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/logic"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/svc"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/types"
)

// Handler 请求处理器
type Handler struct {
	svcCtx *svc.ServiceContext
}

// NewHandler 创建处理器
func NewHandler(svcCtx *svc.ServiceContext) *Handler {
	return &Handler{
		svcCtx: svcCtx,
	}
}

// HandleGetWeather 处理天气查询请求
//
// Handler 只负责请求解析和响应构建
// 业务逻辑委托给 Logic 层
func (h *Handler) HandleGetWeather(ctx context.Context, city string) *types.SkillOutput {
	startTime := time.Now()

	// 构建请求
	req := &types.WeatherRequest{
		City: city,
	}

	// 创建 Logic 并执行业务逻辑
	l := logic.NewWeatherLogic(ctx, h.svcCtx)
	result, err := l.GetWeather(req)

	// 构建统一响应
	return h.buildOutput("get_weather", startTime, result, err)
}

// HandleGetForecast 处理天气预报请求
func (h *Handler) HandleGetForecast(ctx context.Context, city string, days int) *types.SkillOutput {
	startTime := time.Now()

	// 构建请求
	req := &types.ForecastRequest{
		City: city,
		Days: days,
	}

	// 创建 Logic 并执行业务逻辑
	l := logic.NewWeatherLogic(ctx, h.svcCtx)
	result, err := l.GetForecast(req)

	// 构建统一响应
	return h.buildOutput("get_forecast", startTime, result, err)
}

// HandleSkillInput 处理通用技能输入
//
// 支持通过 Action 字段路由到不同的处理方法
func (h *Handler) HandleSkillInput(ctx context.Context, input *types.SkillInput) *types.SkillOutput {
	switch input.Action {
	case "get_weather":
		city := h.extractString(input.Args, "city")
		return h.HandleGetWeather(ctx, city)

	case "get_forecast":
		city := h.extractString(input.Args, "city")
		days := h.extractInt(input.Args, "days", 3)
		return h.HandleGetForecast(ctx, city, days)

	case "list_cities":
		return h.handleListCities()

	default:
		return &types.SkillOutput{
			Success:   false,
			Error:     types.ErrInvalidAction.Error(),
			SkillName: h.svcCtx.Config.Skill.Name,
			Action:    input.Action,
		}
	}
}

// handleListCities 列出支持的城市
func (h *Handler) handleListCities() *types.SkillOutput {
	return &types.SkillOutput{
		Success:    true,
		Result:     h.svcCtx.Config.Skill.SupportedCities,
		SkillName:  h.svcCtx.Config.Skill.Name,
		Action:     "list_cities",
		Confidence: 1.0,
	}
}

// buildOutput 构建统一输出
func (h *Handler) buildOutput(action string, startTime time.Time, result interface{}, err error) *types.SkillOutput {
	output := &types.SkillOutput{
		SkillName:  h.svcCtx.Config.Skill.Name,
		Action:     action,
		Duration:   time.Since(startTime).String(),
		Confidence: 0.9,
	}

	if err != nil {
		output.Success = false
		output.Error = err.Error()
		output.Confidence = 0
	} else {
		output.Success = true
		output.Result = result
	}

	return output
}

// extractString 从参数中提取字符串
func (h *Handler) extractString(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractInt 从参数中提取整数
func (h *Handler) extractInt(args map[string]interface{}, key string, defaultValue int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case int64:
			return int(n)
		}
	}
	return defaultValue
}

// FormatWeatherOutput 格式化天气输出为人类可读格式
func FormatWeatherOutput(resp *types.WeatherResponse) string {
	w := resp.Weather
	return fmt.Sprintf(`
🌤️ %s 天气
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
天气状况: %s
当前温度: %.1f°C
最高/最低: %.1f°C / %.1f°C
湿度: %d%%
风向风速: %s %.1fkm/h
紫外线指数: %d
空气质量: %d
更新时间: %s
━━━━━━━━━━━━━━━━━━━━━━━━━━━━`,
		resp.City,
		w.Condition,
		w.Temperature,
		w.HighTemp, w.LowTemp,
		w.Humidity,
		w.WindDir, w.WindSpeed,
		w.UV,
		w.AQI,
		resp.UpdatedAt.Format("15:04:05"),
	)
}

// FormatForecastOutput 格式化预报输出
func FormatForecastOutput(resp *types.ForecastResponse) string {
	result := fmt.Sprintf("\n📅 %s 天气预报\n", resp.City)
	result += "━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"

	for _, f := range resp.Forecast {
		result += fmt.Sprintf("%s | %s | %.0f~%.0f°C | 💧%d%% | 🌬️%s %.0fkm/h\n",
			f.Date, f.Condition, f.LowTemp, f.HighTemp, f.Humidity, f.WindDir, f.WindSpeed)
	}

	result += "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	return result
}
