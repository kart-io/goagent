// Package main 天气技能示例
//
// 本示例演示了完整的 Skill 实现：
// 1. 分层架构：Handler -> Logic -> ServiceContext
// 2. 配置驱动：通过 YAML 配置文件管理技能参数
// 3. Skill 完整生命周期：定义、注册、路由、执行
// 4. LLM 工具调用：集成 DeepSeek/OpenAI 进行自然语言理解
//
// 运行方式：
//
//	# 从项目根目录运行（推荐）
//	go run examples/skills/02-weather-skill/main.go
//
//	# 或从示例目录运行
//	cd examples/skills/02-weather-skill && go run main.go
//
// 使用 LLM 功能需要设置环境变量：
//
//	export DEEPSEEK_API_KEY=your-api-key
//
// 或者：
//
//	export OPENAI_API_KEY=your-api-key
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/config"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/handler"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/llmtools"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/skill"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/svc"
	"github.com/kart-io/goagent/examples/skills/02-weather-skill/internal/types"
	"github.com/kart-io/goagent/interfaces"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          天气技能示例 (Weather Skill Demo)                      ║")
	fmt.Println("║   分层架构实现 + LLM 工具调用                                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 加载配置
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		return
	}

	// 场景 1：Skill 定义与创建
	fmt.Println("【场景 1】Skill 定义与创建")
	fmt.Println("══════════════════════════════════════════════════��═════════════")
	weatherSkill := demonstrateSkillCreation(cfg)

	// 场景 2：Skill 注册与发现
	fmt.Println("\n【场景 2】Skill 注册与发现")
	fmt.Println("════════════════════════════════════════════════════════════════")
	registry := demonstrateSkillRegistration(weatherSkill)

	// 场景 3：Skill 路由
	fmt.Println("\n【场景 3】Skill 路由")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateSkillRouting(registry)

	// 场景 4：Skill 执行
	fmt.Println("\n【场景 4】Skill 执行")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateSkillExecution(ctx, weatherSkill)

	// 场景 5：通过执行器调用
	fmt.Println("\n【场景 5】通过执行器调用")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateExecutor(ctx, registry)

	// 场景 6：LLM 工具调用
	fmt.Println("\n【场景 6】LLM 工具调用 (Tool Calling)")
	fmt.Println("════════════════════════════════════════════════════════════════")
	demonstrateLLMToolCalling(ctx, cfg)

	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                        示例完成                                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
}

// loadConfig 加载配置
func loadConfig() (*config.Config, error) {
	// 尝试从文件加载
	configFile := "etc/config.yaml"
	if _, err := os.Stat(configFile); err == nil {
		data, err := os.ReadFile(configFile)
		if err != nil {
			return nil, err
		}

		var cfg config.Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}

		fmt.Printf("✓ 从 %s 加载配置\n", configFile)
		return &cfg, nil
	}

	// 使用默认配置
	fmt.Println("✓ 使用默认配置")
	return &config.Config{
		Name: "weather-skill",
		Log: config.LogConfig{
			Level: "info",
			Mode:  "console",
		},
		Skill: config.SkillConf{
			Name:        "weather",
			Description: "天气查询与预报技能",
			Version:     "v1.0.0",
			DefaultCity: "北京",
			SupportedCities: []string{
				"北京", "上海", "广州", "深圳", "杭州", "成都", "武汉", "西安",
			},
			Cache: config.CacheConfig{
				Enabled: true,
				TTL:     300,
			},
		},
		Keywords: []string{"天气", "气温", "预报", "weather", "forecast", "temperature"},
		LLM: config.LLMConfig{
			Provider:    "deepseek",
			Model:       "deepseek-chat",
			MaxTokens:   1000,
			Temperature: 0.7,
		},
	}, nil
}

// demonstrateSkillCreation 演示 Skill 创建
func demonstrateSkillCreation(cfg *config.Config) *skill.WeatherSkill {
	fmt.Println("\n创建天气技能实例:")
	fmt.Println("────────────────────────────────────────")

	// 创建技能
	weatherSkill := skill.NewWeatherSkill(cfg)

	// 显示技能信息
	fmt.Printf("  名称: %s\n", weatherSkill.Name())
	fmt.Printf("  描述: %s\n", weatherSkill.Description())
	fmt.Printf("  版本: %s\n", weatherSkill.Version())
	fmt.Printf("  关键词: %v\n", weatherSkill.Keywords())

	fmt.Println("\n✓ 技能创建成功")

	return weatherSkill
}

// demonstrateSkillRegistration 演示 Skill 注册
func demonstrateSkillRegistration(weatherSkill *skill.WeatherSkill) *skill.SkillRegistry {
	fmt.Println("\n注册技能到注册表:")
	fmt.Println("────────────────────────────────────────")

	// 创建注册表
	registry := skill.NewSkillRegistry()
	fmt.Println("  ✓ 创建注册表")

	// 注册技能
	if err := registry.Register(weatherSkill); err != nil {
		fmt.Printf("  ✗ 注册失败: %v\n", err)
		return registry
	}
	fmt.Printf("  ✓ 注册技能: %s\n", weatherSkill.Name())

	// 显示注册表状态
	fmt.Printf("\n注册表状态:\n")
	fmt.Printf("  已注册技能数: %d\n", registry.Size())

	// 列出所有技能
	fmt.Println("\n已注册技能列表:")
	for _, s := range registry.List() {
		fmt.Printf("  - %s (%s): %s\n", s.Name(), s.Version(), s.Description())
	}

	return registry
}

// demonstrateSkillRouting 演示 Skill 路由
func demonstrateSkillRouting(registry *skill.SkillRegistry) {
	fmt.Println("\n测试路由匹配:")
	fmt.Println("────────────────────────────────────────")

	testCases := []struct {
		query    string
		keywords []string
	}{
		{"今天天气怎么样", []string{"天气"}},
		{"北京的气温是多少", []string{"气温", "北京"}},
		{"明天会下雨吗", []string{"天气", "预报"}},
		{"weather in Shanghai", []string{"weather"}},
	}

	for _, tc := range testCases {
		routingCtx := &skill.RoutingContext{
			Query:    tc.query,
			Keywords: tc.keywords,
		}

		matchedSkill, score := registry.Route(routingCtx)
		if matchedSkill != nil {
			fmt.Printf("\n  查询: '%s'\n", tc.query)
			fmt.Printf("  关键词: %v\n", tc.keywords)
			fmt.Printf("  匹配技能: %s (分数: %.2f)\n", matchedSkill.Name(), score)
		} else {
			fmt.Printf("\n  查询: '%s' - 无匹配技能\n", tc.query)
		}
	}
}

// demonstrateSkillExecution 演示 Skill 执行
func demonstrateSkillExecution(ctx context.Context, weatherSkill *skill.WeatherSkill) {
	fmt.Println("\n直接调用技能:")
	fmt.Println("────────────────────────────────────────")

	// 测试获取天气
	fmt.Println("\n1. 获取当前天气:")
	weatherOutput := weatherSkill.GetWeather(ctx, "北京")
	if weatherOutput.Success {
		if resp, ok := weatherOutput.Result.(*types.WeatherResponse); ok {
			fmt.Println(handler.FormatWeatherOutput(resp))
		}
	} else {
		fmt.Printf("  ✗ 失败: %s\n", weatherOutput.Error)
	}

	// 测试获取预报
	fmt.Println("\n2. 获取天气预报:")
	forecastOutput := weatherSkill.GetForecast(ctx, "上海", 5)
	if forecastOutput.Success {
		if resp, ok := forecastOutput.Result.(*types.ForecastResponse); ok {
			fmt.Println(handler.FormatForecastOutput(resp))
		}
	} else {
		fmt.Printf("  ✗ 失败: %s\n", forecastOutput.Error)
	}

	// 测试通用输入
	fmt.Println("\n3. 通过 SkillInput 调用:")
	input := &types.SkillInput{
		Action: "list_cities",
		Args:   map[string]interface{}{},
	}
	output := weatherSkill.Execute(ctx, input)
	if output.Success {
		fmt.Printf("  支持的城市: %v\n", output.Result)
	}

	// 测试错误处理
	fmt.Println("\n4. 错误处理测试:")
	errorOutput := weatherSkill.GetWeather(ctx, "火星")
	if !errorOutput.Success {
		fmt.Printf("  ✓ 错误被正确捕获: %s\n", errorOutput.Error)
	}
}

// demonstrateExecutor 演示执行器
func demonstrateExecutor(ctx context.Context, registry *skill.SkillRegistry) {
	fmt.Println("\n使用执行器调用技能:")
	fmt.Println("────────────────────────────────────────")

	// 创建执行器
	executor := skill.NewSkillExecutor(registry)
	executor.SetTimeout(10 * time.Second)
	fmt.Println("  ✓ 创建执行器")

	// 按名称执行
	fmt.Println("\n1. 按名称执行:")
	output := executor.ExecuteByName(ctx, "weather", &types.SkillInput{
		Action: "get_weather",
		Args:   map[string]interface{}{"city": "广州"},
	})
	fmt.Printf("  技能: %s\n", output.SkillName)
	fmt.Printf("  动作: %s\n", output.Action)
	fmt.Printf("  成功: %v\n", output.Success)
	fmt.Printf("  耗时: %s\n", output.Duration)

	// 按路由执行
	fmt.Println("\n2. 按路由执行:")
	routingCtx := &skill.RoutingContext{
		Query:    "深圳明天天气怎么样",
		Keywords: []string{"天气", "预报"},
	}
	routedOutput := executor.ExecuteByRouting(ctx, routingCtx, &types.SkillInput{
		Action: "get_forecast",
		Args:   map[string]interface{}{"city": "深圳", "days": 3},
	})
	fmt.Printf("  技能: %s\n", routedOutput.SkillName)
	fmt.Printf("  置信度: %.2f\n", routedOutput.Confidence)
	fmt.Printf("  成功: %v\n", routedOutput.Success)

	// 测试不存在的技能
	fmt.Println("\n3. 错误处理 - 技能不存在:")
	notFoundOutput := executor.ExecuteByName(ctx, "not_exist", &types.SkillInput{
		Action: "test",
	})
	if !notFoundOutput.Success {
		fmt.Printf("  ✓ 错误被捕获: %s\n", notFoundOutput.Error)
	}
}

// demonstrateLLMToolCalling 演示 LLM 工具调用
func demonstrateLLMToolCalling(ctx context.Context, cfg *config.Config) {
	fmt.Println("\n创建 LLM 天气技能:")
	fmt.Println("────────────────────────────────────────")

	// 创建 LLM 天气技能
	llmSkill := skill.NewLLMWeatherSkill(cfg)

	// 显示技能信息
	fmt.Printf("  名称: %s\n", llmSkill.Name())
	fmt.Printf("  描述: %s\n", llmSkill.Description())
	fmt.Printf("  LLM 提供商: %s\n", llmSkill.GetLLMProvider())

	// 显示可用工具
	fmt.Println("\n可用工具:")
	for _, tool := range llmSkill.GetTools() {
		fmt.Printf("  - %s: %s\n", tool.Name(), tool.Description())
	}

	// 检查 LLM 是否可用
	if llmSkill.GetLLMProvider() == "none" {
		fmt.Println("\n⚠️ 未检测到 LLM API Key")
		fmt.Println("请设置环境变量以启用 LLM 功能:")
		fmt.Println("  export DEEPSEEK_API_KEY=your-api-key")
		fmt.Println("  或")
		fmt.Println("  export OPENAI_API_KEY=your-api-key")
		fmt.Println("\n使用 Mock 模式演示工具调用:")
		demonstrateMockToolCalling(ctx, cfg)
		return
	}

	// 测试自然语言查询
	fmt.Println("\n自然语言查询测试:")
	fmt.Println("────────────────────────────────────────")

	testQueries := []string{
		"今天北京的天气怎么样？",
		"上海未来三天的天气预报",
		"你支持查询哪些城市的天气？",
	}

	for i, query := range testQueries {
		fmt.Printf("\n%d. 查询: %s\n", i+1, query)
		output := llmSkill.AskNatural(ctx, query)
		if output.Success {
			fmt.Printf("  使用工具: %s\n", output.Action)
			fmt.Printf("  结果: %v\n", output.Result)
		} else {
			fmt.Printf("  ✗ 失败: %s\n", output.Error)
		}
		fmt.Printf("  耗时: %s\n", output.Duration)
	}
}

// demonstrateMockToolCalling 使用 Mock 模式演示工具调用
func demonstrateMockToolCalling(ctx context.Context, cfg *config.Config) {
	fmt.Println("────────────────────────────────────────")

	// 直接创建服务上下文和工具
	svcCtx := svc.NewServiceContext(cfg)
	weatherTools := llmtools.NewWeatherTools(svcCtx)

	// 测试 get_weather 工具
	fmt.Println("\n1. 直接调用 get_weather 工具:")
	for _, tool := range weatherTools.GetTools() {
		if tool.Name() == "get_weather" {
			output, err := tool.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{"city": "北京"},
			})
			if err != nil {
				fmt.Printf("  ✗ 错误: %v\n", err)
			} else if output.Success {
				fmt.Println(llmtools.FormatToolResult("get_weather", output.Result))
			}
		}
	}

	// 测试 get_forecast 工具
	fmt.Println("\n2. 直接调用 get_forecast 工具:")
	for _, tool := range weatherTools.GetTools() {
		if tool.Name() == "get_forecast" {
			output, err := tool.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{"city": "上海", "days": 3.0},
			})
			if err != nil {
				fmt.Printf("  ✗ 错误: %v\n", err)
			} else if output.Success {
				fmt.Println(llmtools.FormatToolResult("get_forecast", output.Result))
			}
		}
	}

	// 测试 list_cities 工具
	fmt.Println("\n3. 直接调用 list_cities 工具:")
	for _, tool := range weatherTools.GetTools() {
		if tool.Name() == "list_cities" {
			output, err := tool.Invoke(ctx, &interfaces.ToolInput{
				Args: map[string]interface{}{},
			})
			if err != nil {
				fmt.Printf("  ✗ 错误: %v\n", err)
			} else if output.Success {
				fmt.Println("  " + llmtools.FormatToolResult("list_cities", output.Result))
			}
		}
	}

	fmt.Println("\n✓ Mock 工具调用演示完成")
	fmt.Println("提示: 设置 LLM API Key 后，可体验 LLM 自动选择工具的智能功能")
}
