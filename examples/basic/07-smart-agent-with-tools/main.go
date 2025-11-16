package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/http"
)

func main() {
	fmt.Println("=== 智能 Agent 示例 - 时间获取与 API 调用 ===")
	fmt.Println()

	// 示例 1: 创建获取当前时间的工具
	timeToolExample()

	// 示例 2: 创建 API 调用工具
	apiCallExample()

	// 示例 3: 创建集成两个工具的智能 Agent
	smartAgentExample()
}

// 示例 1: 获取当前时间的工具
func timeToolExample() {
	fmt.Println("--- 示例 1: 获取当前时间工具 ---")

	// 创建时间工具
	timeTool := createTimeTool()

	ctx := context.Background()
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"format":   "2006-01-02 15:04:05",
			"timezone": "Asia/Shanghai",
		},
		Context: ctx,
	}

	output, err := timeTool.Invoke(ctx, input)
	if err != nil {
		log.Printf("错误: %v\n", err)
		return
	}

	if output.Success {
		result := output.Result.(map[string]interface{})
		fmt.Printf("当前时间: %s\n", result["time"])
		fmt.Printf("时区: %s\n", result["timezone"])
		fmt.Printf("Unix 时间戳: %v\n", result["timestamp"])
	}
	fmt.Println()
}

// 示例 2: API 调用工具
func apiCallExample() {
	fmt.Println("--- 示例 2: API 调用工具 ---")

	// 创建 API 工具
	apiTool := createAPITool()

	ctx := context.Background()

	// 示例 2.1: GET 请求 - 获取用户信息
	fmt.Println("2.1: GET 请求获取用户信息")
	getUserInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"method": "GET",
			"url":    "https://jsonplaceholder.typicode.com/users/1",
		},
		Context: ctx,
	}

	output, err := apiTool.Invoke(ctx, getUserInput)
	if err != nil {
		log.Printf("错误: %v\n", err)
	} else if output.Success {
		result := output.Result.(map[string]interface{})
		body := result["body"].(map[string]interface{})
		fmt.Printf("用户名: %v\n", body["name"])
		fmt.Printf("邮箱: %v\n", body["email"])
		fmt.Printf("城市: %v\n", body["address"].(map[string]interface{})["city"])
	}
	fmt.Println()

	// 示例 2.2: GET 请求 - 获取文章列表
	fmt.Println("2.2: GET 请求获取文章列表")
	getPostsInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"method": "GET",
			"url":    "https://jsonplaceholder.typicode.com/posts?_limit=3",
		},
		Context: ctx,
	}

	output, err = apiTool.Invoke(ctx, getPostsInput)
	if err != nil {
		log.Printf("错误: %v\n", err)
	} else if output.Success {
		result := output.Result.(map[string]interface{})
		body := result["body"].([]interface{})
		fmt.Printf("获取到 %d 篇文章:\n", len(body))
		for i, post := range body {
			postMap := post.(map[string]interface{})
			fmt.Printf("%d. %s\n", i+1, postMap["title"])
		}
	}
	fmt.Println()

	// 示例 2.3: POST 请求 - 创建新文章
	fmt.Println("2.3: POST 请求创建新文章")
	newPost := map[string]interface{}{
		"title":  "智能 Agent 测试文章",
		"body":   "这是一个由智能 Agent 创建的文章",
		"userId": 1,
	}

	createPostInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"method": "POST",
			"url":    "https://jsonplaceholder.typicode.com/posts",
			"body":   newPost,
			"headers": map[string]interface{}{
				"Content-Type": "application/json",
			},
		},
		Context: ctx,
	}

	output, err = apiTool.Invoke(ctx, createPostInput)
	if err != nil {
		log.Printf("错误: %v\n", err)
	} else if output.Success {
		result := output.Result.(map[string]interface{})
		body := result["body"].(map[string]interface{})
		fmt.Printf("创建成功! ID: %v\n", body["id"])
		fmt.Printf("标题: %v\n", body["title"])
	}
	fmt.Println()
}

// 示例 3: 集成智能 Agent
func smartAgentExample() {
	fmt.Println("--- 示例 3: 集成智能 Agent ---")

	// 创建工具
	timeTool := createTimeTool()
	apiTool := createAPITool()
	weatherTool := createWeatherAPITool()

	// 构建 Agent（注意：这里需要配置实际的 LLM，此处仅演示工具集成）
	ctx := context.Background()

	fmt.Println("可用工具:")
	fmt.Printf("1. %s - %s\n", timeTool.Name(), timeTool.Description())
	fmt.Printf("2. %s - %s\n", apiTool.Name(), apiTool.Description())
	fmt.Printf("3. %s - %s\n", weatherTool.Name(), weatherTool.Description())
	fmt.Println()

	// 演示工具调用链
	fmt.Println("演示场景：获取当前时间并查询天气信息")
	fmt.Println()

	// 步骤 1: 获取当前时间
	fmt.Println("步骤 1: 获取当前时间")
	timeOutput, _ := timeTool.Invoke(ctx, &tools.ToolInput{
		Args: map[string]interface{}{
			"format":   "2006-01-02 15:04:05",
			"timezone": "Asia/Shanghai",
		},
		Context: ctx,
	})

	if timeOutput.Success {
		result := timeOutput.Result.(map[string]interface{})
		fmt.Printf("✓ 当前时间: %s\n\n", result["time"])
	}

	// 步骤 2: 查询天气
	fmt.Println("步骤 2: 查询天气信息")
	weatherOutput, _ := weatherTool.Invoke(ctx, &tools.ToolInput{
		Args: map[string]interface{}{
			"city": "Beijing",
		},
		Context: ctx,
	})

	if weatherOutput.Success {
		result := weatherOutput.Result.(map[string]interface{})
		body := result["body"].(map[string]interface{})
		fmt.Printf("✓ 城市: %v\n", body["name"])
		fmt.Printf("✓ 天气: %v\n", body["weather"])
		fmt.Printf("✓ 温度: %v°C\n", body["temperature"])
		fmt.Printf("✓ 湿度: %v%%\n", body["humidity"])
	}
	fmt.Println()

	// 可选：如果配置了 LLM，可以创建完整的 Agent
	demonstrateAgentWithLLM()
}

// 创建获取时间的工具
func createTimeTool() interfaces.Tool {
	return tools.NewFunctionToolBuilder("get_current_time").
		WithDescription("获取当前时间，支持不同的时区和格式").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"format": {
					"type": "string",
					"description": "时间格式，如 '2006-01-02 15:04:05'",
					"default": "2006-01-02 15:04:05"
				},
				"timezone": {
					"type": "string",
					"description": "时区，如 'Asia/Shanghai', 'UTC', 'America/New_York'",
					"default": "UTC"
				}
			}
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			// 获取时区
			timezone := "UTC"
			if tz, ok := args["timezone"].(string); ok && tz != "" {
				timezone = tz
			}

			// 获取格式
			format := "2006-01-02 15:04:05"
			if f, ok := args["format"].(string); ok && f != "" {
				format = f
			}

			// 加载时区
			loc, err := time.LoadLocation(timezone)
			if err != nil {
				// 如果时区无效，使用 UTC
				loc = time.UTC
				timezone = "UTC"
			}

			// 获取当前时间
			now := time.Now().In(loc)

			return map[string]interface{}{
				"time":      now.Format(format),
				"timezone":  timezone,
				"timestamp": now.Unix(),
				"weekday":   now.Weekday().String(),
				"year":      now.Year(),
				"month":     now.Month().String(),
				"day":       now.Day(),
				"hour":      now.Hour(),
				"minute":    now.Minute(),
				"second":    now.Second(),
			}, nil
		}).
		Build()
}

// 创建通用 API 调用工具
func createAPITool() interfaces.Tool {
	return http.NewAPIToolBuilder().
		WithBaseURL("").
		WithTimeout(30 * time.Second).
		Build()
}

// 创建天气查询 API 工具（使用模拟数据）
func createWeatherAPITool() interfaces.Tool {
	return tools.NewFunctionToolBuilder("get_weather").
		WithDescription("查询指定城市的天气信息").
		WithArgsSchema(`{
			"type": "object",
			"properties": {
				"city": {
					"type": "string",
					"description": "城市名称，如 'Beijing', 'Shanghai', 'New York'"
				}
			},
			"required": ["city"]
		}`).
		WithFunction(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			city, ok := args["city"].(string)
			if !ok {
				return nil, fmt.Errorf("缺少必需参数: city")
			}

			// 模拟天气 API 响应
			// 在实际应用中，这里应该调用真实的天气 API
			weatherData := map[string]interface{}{
				"name":        city,
				"weather":     "晴朗",
				"temperature": 22,
				"humidity":    65,
				"wind_speed":  3.5,
				"description": "天气晴朗，适合出行",
				"updated_at":  time.Now().Format("2006-01-02 15:04:05"),
			}

			return map[string]interface{}{
				"status": 200,
				"body":   weatherData,
			}, nil
		}).
		Build()
}

// 演示如何使用 LLM 创建完整的 Agent
func demonstrateAgentWithLLM() {
	fmt.Println("--- 使用 LLM 创建完整 Agent (示例代码) ---")
	// 打印示例代码
	exampleCode := `
// 1. 创建 Agent 并配置 LLM
agent, err := builder.NewAgentBuilder().
    WithName("SmartAssistant").
    WithDescription("智能助手，可以获取时间和查询天气").
    WithOpenAI(os.Getenv("OPENAI_API_KEY"), "gpt-4").  // 或使用其他 LLM
    WithTools(
        createTimeTool(),
        createWeatherAPITool(),
        createAPITool(),
    ).
    Build()

if err != nil {
    log.Fatal(err)
}

// 2. 运行 Agent
ctx := context.Background()
state := map[string]interface{}{
    "input": "现在几点了？北京的天气怎么样？",
}

result, err := agent.Invoke(ctx, state)
if err != nil {
    log.Fatal(err)
}

// 3. 获取结果
fmt.Printf("Agent 回复: ` + `%v\n", result["output"])` + `
	`
	fmt.Println(exampleCode)
	fmt.Println()
	fmt.Println("注意: 要运行完整的 Agent，需要配置 LLM API Key")
	fmt.Println("例如: export OPENAI_API_KEY=your_api_key")
	fmt.Println()
}

// 完整的 Agent 示例（需要配置 LLM）
// 这个函数展示如何创建一个完整的 Agent
// 需要先设置环境变量: export OPENAI_API_KEY=your_api_key
//
// 使用示例:
// 1. 创建工具
//    timeTool := createTimeTool()
//    weatherTool := createWeatherAPITool()
//    apiTool := createAPITool()
//
// 2. 创建 LLM 客户端
//    import "github.com/kart-io/goagent/llm/providers"
//    llmClient := providers.NewOpenAIClient(os.Getenv("OPENAI_API_KEY"), "gpt-4")
//
// 3. 创建 Agent
//    agent, err := builder.NewAgentBuilder(llmClient).
//        WithName("SmartAssistant").
//        WithDescription("智能助手，可以获取时间信息和调用各种 API").
//        WithTools(timeTool, weatherTool, apiTool).
//        Build()
//
// 4. 运行 Agent
//    ctx := context.Background()
//    state := map[string]interface{}{
//        "input": "现在几点了？",
//    }
//    result, err := agent.Invoke(ctx, state)
//    fmt.Printf("Agent 回复: %v\n", result["output"])
