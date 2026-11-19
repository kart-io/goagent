// Package llmtools LLM 工具定义
//
// 将天气功能封装为可供 LLM 调用的工具
// 使用 goagent 的 tools.FunctionTool 实现工具接口
package llmtools

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/logic"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/svc"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/types"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// WeatherTools 天气工具集
//
// 提供天气相关的工具供 LLM 调用
type WeatherTools struct {
	svcCtx *svc.ServiceContext
	tools  []interfaces.Tool
}

// NewWeatherTools 创建天气工具集
func NewWeatherTools(svcCtx *svc.ServiceContext) *WeatherTools {
	wt := &WeatherTools{
		svcCtx: svcCtx,
	}

	// 注册所有天气工具
	wt.tools = []interfaces.Tool{
		wt.createGetWeatherTool(),
		wt.createGetForecastTool(),
		wt.createListCitiesTool(),
	}

	return wt
}

// GetTools 获取所有工具
func (wt *WeatherTools) GetTools() []interfaces.Tool {
	return wt.tools
}

// createGetWeatherTool 创建获取天气工具
func (wt *WeatherTools) createGetWeatherTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"get_weather",
		"获取指定城市的当前天气信息，包括温度、湿度、天气状况等",
		`{
			"type": "object",
			"properties": {
				"city": {
					"type": "string",
					"description": "要查询天气的城市名称，如：北京、上海、广州"
				}
			},
			"required": ["city"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			// 提取参数
			city, ok := args["city"].(string)
			if !ok || city == "" {
				city = wt.svcCtx.Config.Skill.DefaultCity
			}

			// 调用 Logic 层获取天气
			l := logic.NewWeatherLogic(ctx, wt.svcCtx)
			resp, err := l.GetWeather(&types.WeatherRequest{City: city})
			if err != nil {
				return nil, err
			}

			// 返回格式化的天气信息
			return map[string]interface{}{
				"city":        resp.City,
				"condition":   resp.Weather.Condition,
				"temperature": resp.Weather.Temperature,
				"high_temp":   resp.Weather.HighTemp,
				"low_temp":    resp.Weather.LowTemp,
				"humidity":    resp.Weather.Humidity,
				"wind_speed":  resp.Weather.WindSpeed,
				"wind_dir":    resp.Weather.WindDir,
				"uv_index":    resp.Weather.UV,
				"aqi":         resp.Weather.AQI,
				"updated_at":  resp.UpdatedAt.Format(time.RFC3339),
			}, nil
		},
	)
}

// createGetForecastTool 创建获取天气预报工具
func (wt *WeatherTools) createGetForecastTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"get_forecast",
		"获取指定城市未来几天的天气预报",
		`{
			"type": "object",
			"properties": {
				"city": {
					"type": "string",
					"description": "要查询天气预报的城市名称，如：北京、上海、广州"
				},
				"days": {
					"type": "integer",
					"description": "预报天数，范围 1-7 天，默认 3 天",
					"minimum": 1,
					"maximum": 7,
					"default": 3
				}
			},
			"required": ["city"]
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			// 提取参数
			city, ok := args["city"].(string)
			if !ok || city == "" {
				city = wt.svcCtx.Config.Skill.DefaultCity
			}

			days := 3
			if d, ok := args["days"].(float64); ok {
				days = int(d)
			}

			// 调用 Logic 层获取预报
			l := logic.NewWeatherLogic(ctx, wt.svcCtx)
			resp, err := l.GetForecast(&types.ForecastRequest{City: city, Days: days})
			if err != nil {
				return nil, err
			}

			// 格式化预报数据
			forecastList := make([]map[string]interface{}, len(resp.Forecast))
			for i, f := range resp.Forecast {
				forecastList[i] = map[string]interface{}{
					"date":       f.Date,
					"condition":  f.Condition,
					"high_temp":  f.HighTemp,
					"low_temp":   f.LowTemp,
					"humidity":   f.Humidity,
					"wind_speed": f.WindSpeed,
					"wind_dir":   f.WindDir,
				}
			}

			return map[string]interface{}{
				"city":     resp.City,
				"days":     days,
				"forecast": forecastList,
			}, nil
		},
	)
}

// createListCitiesTool 创建列出城市工具
func (wt *WeatherTools) createListCitiesTool() *tools.FunctionTool {
	return tools.NewFunctionTool(
		"list_cities",
		"获取支持查询天气的城市列表",
		`{
			"type": "object",
			"properties": {}
		}`,
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{
				"cities":       wt.svcCtx.Config.Skill.SupportedCities,
				"default_city": wt.svcCtx.Config.Skill.DefaultCity,
				"total":        len(wt.svcCtx.Config.Skill.SupportedCities),
			}, nil
		},
	)
}

// FormatToolResult 格式化工具调用结果为可读文本
func FormatToolResult(toolName string, result interface{}) string {
	switch toolName {
	case "get_weather":
		if data, ok := result.(map[string]interface{}); ok {
			return fmt.Sprintf(
				`
🌤️ %s 天气
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
天气状况: %v
当前温度: %.1f°C
最高/最低: %.1f°C / %.1f°C
湿度: %v%%
风向风速: %v %.1fkm/h
紫外线指数: %v
空气质量: %v
━━━━━━━━━━━━━━━━━━━━━━━━━━━━`,
				data["city"],
				data["condition"],
				data["temperature"],
				data["high_temp"], data["low_temp"],
				data["humidity"],
				data["wind_dir"], data["wind_speed"],
				data["uv_index"],
				data["aqi"],
			)
		}

	case "get_forecast":
		if data, ok := result.(map[string]interface{}); ok {
			output := fmt.Sprintf("\n📅 %s 天气预报\n", data["city"])
			output += "━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
			if forecast, ok := data["forecast"].([]map[string]interface{}); ok {
				for _, f := range forecast {
					output += fmt.Sprintf("%s | %s | %.0f~%.0f°C | 💧%v%% | 🌬️%s %.0fkm/h\n",
						f["date"], f["condition"],
						f["low_temp"], f["high_temp"],
						f["humidity"],
						f["wind_dir"], f["wind_speed"])
				}
			}
			output += "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
			return output
		}

	case "list_cities":
		if data, ok := result.(map[string]interface{}); ok {
			return fmt.Sprintf("支持的城市: %v (默认: %v, 共 %v 个)",
				data["cities"], data["default_city"], data["total"])
		}
	}

	return fmt.Sprintf("%v", result)
}
