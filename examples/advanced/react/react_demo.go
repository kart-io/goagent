package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/agents/executor"
	"github.com/kart-io/goagent/agents/react"
	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// SimpleLLMClient 简单的 LLM 客户端实现
type SimpleLLMClient struct {
	responses []string
	index     int
}

func NewSimpleLLMClient() *SimpleLLMClient {
	return &SimpleLLMClient{
		responses: []string{},
		index:     0,
	}
}

func (s *SimpleLLMClient) AddResponse(response string) {
	s.responses = append(s.responses, response)
}

func (s *SimpleLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	if s.index >= len(s.responses) {
		return &llm.CompletionResponse{
			Content:    "Final Answer: I don't have enough information.",
			TokensUsed: 10,
		}, nil
	}

	response := s.responses[s.index]
	s.index++

	return &llm.CompletionResponse{
		Content:    response,
		Model:      "simple-model",
		TokensUsed: len(response) / 4,
	}, nil
}

func (s *SimpleLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return s.Chat(ctx, req.Messages)
}

func (s *SimpleLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (s *SimpleLLMClient) IsAvailable() bool {
	return true
}

func main() {
	fmt.Println("=== ReAct Agent Example ===")
	fmt.Println()

	// 创建工具
	calculatorTool := createCalculatorTool()
	weatherTool := createWeatherTool()
	searchTool := createSearchTool()

	// 创建 LLM 客户端
	llmClient := NewSimpleLLMClient()

	// 添加预设的响应（模拟 LLM 输出）
	llmClient.AddResponse(`Thought: I need to search for the current weather in Beijing
Action: weather
Action Input: {"city": "Beijing"}`)

	llmClient.AddResponse(`Thought: Now I have the weather information. The temperature is 25°C. I need to calculate what this is in Fahrenheit.
Action: calculator
Action Input: {"expression": "25 * 9/5 + 32"}`)

	llmClient.AddResponse(`Thought: I now have all the information needed to provide a complete answer
Final Answer: The current weather in Beijing is 25°C (77°F) with sunny skies.`)

	// 创建 ReAct Agent
	agent := react.NewReActAgent(react.ReActConfig{
		Name:        "WeatherAgent",
		Description: "An agent that can provide weather information and perform calculations",
		LLM:         llmClient,
		Tools:       []tools.Tool{calculatorTool, weatherTool, searchTool},
		MaxSteps:    10,
	})

	// 添加回调以观察执行过程
	callback := &LoggingCallback{}

	// 创建执行器
	executor := executor.NewAgentExecutor(executor.ExecutorConfig{
		Agent:             agent,
		Tools:             []tools.Tool{calculatorTool, weatherTool, searchTool},
		MaxIterations:     10,
		MaxExecutionTime:  30 * time.Second,
		ReturnIntermSteps: true,
		Verbose:           true,
	})

	// 执行 Agent
	ctx := context.Background()
	input := &agentcore.AgentInput{
		Task: "What's the weather like in Beijing, and what's that temperature in Fahrenheit?",
	}

	// 直接执行 agent 进行测试
	fmt.Println("=== Executing Agent ===")
	output, err := agent.Invoke(ctx, input)
	if err != nil {
		log.Fatalf("Error executing agent directly: %v", err)
	}

	fmt.Printf("Direct agent execution succeeded: %v\n\n", output.Result)

	// 重置 LLM 客户端索引用于第二次执行
	llmClient.index = 0

	// 添加回调
	output, err = executor.ExecuteWithCallbacks(ctx, input, callback)
	if err != nil {
		log.Fatalf("Error executing agent: %v", err)
	}

	// 打印结果
	fmt.Println("\n=== Execution Result ===")
	fmt.Printf("Status: %s\n", output.Status)
	fmt.Printf("Result: %v\n", output.Result)
	fmt.Printf("Message: %s\n", output.Message)
	fmt.Printf("Latency: %v\n", output.Latency)
	fmt.Printf("Steps: %d\n", len(output.ReasoningSteps))
	fmt.Printf("Tool Calls: %d\n", len(output.ToolCalls))

	fmt.Println("\n=== Reasoning Steps ===")
	for i, step := range output.ReasoningSteps {
		fmt.Printf("%d. [%s] %s -> %s (%.2fms)\n",
			i+1,
			step.Action,
			step.Description,
			step.Result,
			float64(step.Duration.Microseconds())/1000,
		)
	}

	fmt.Println("\n=== Tool Calls ===")
	for i, toolCall := range output.ToolCalls {
		status := "SUCCESS"
		if !toolCall.Success {
			status = "FAILED"
		}
		fmt.Printf("%d. %s [%s] (%.2fms)\n   Input: %v\n   Output: %v\n",
			i+1,
			toolCall.ToolName,
			status,
			float64(toolCall.Duration.Microseconds())/1000,
			toolCall.Input,
			toolCall.Output,
		)
	}
}

// createCalculatorTool 创建计算器工具
func createCalculatorTool() tools.Tool {
	return tools.NewBaseTool(
		"calculator",
		"Useful for mathematical calculations. Input should be a mathematical expression.",
		`{
			"type": "object",
			"properties": {
				"expression": {
					"type": "string",
					"description": "The mathematical expression to evaluate"
				}
			},
			"required": ["expression"]
		}`,
		func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
			expr, ok := input.Args["expression"].(string)
			if !ok {
				return &tools.ToolOutput{
					Success: false,
					Error:   "expression must be a string",
				}, nil
			}

			// 简化实现：这里应该使用真实的表达式求值器
			// 为了示例，我们直接返回一个固定结果
			var result interface{}
			switch expr {
			case "25 * 9/5 + 32":
				result = 77.0
			case "100 / 2":
				result = 50.0
			default:
				result = 42.0
			}

			return &tools.ToolOutput{
				Result:  result,
				Success: true,
			}, nil
		},
	)
}

// createWeatherTool 创建天气查询工具
func createWeatherTool() tools.Tool {
	return tools.NewBaseTool(
		"weather",
		"Get current weather information for a city.",
		`{
			"type": "object",
			"properties": {
				"city": {
					"type": "string",
					"description": "The name of the city"
				}
			},
			"required": ["city"]
		}`,
		func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
			city, ok := input.Args["city"].(string)
			if !ok {
				return &tools.ToolOutput{
					Success: false,
					Error:   "city must be a string",
				}, nil
			}

			// 模拟天气数据
			weather := map[string]interface{}{
				"city":        city,
				"temperature": 25,
				"unit":        "celsius",
				"condition":   "sunny",
				"humidity":    60,
			}

			return &tools.ToolOutput{
				Result:  weather,
				Success: true,
				Metadata: map[string]interface{}{
					"source": "mock_api",
				},
			}, nil
		},
	)
}

// createSearchTool 创建搜索工具
func createSearchTool() tools.Tool {
	return tools.NewBaseTool(
		"search",
		"Search for information on the internet.",
		`{
			"type": "object",
			"properties": {
				"query": {
					"type": "string",
					"description": "The search query"
				}
			},
			"required": ["query"]
		}`,
		func(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
			query, ok := input.Args["query"].(string)
			if !ok {
				return &tools.ToolOutput{
					Success: false,
					Error:   "query must be a string",
				}, nil
			}

			// 模拟搜索结果
			results := []string{
				fmt.Sprintf("Result 1 for '%s'", query),
				fmt.Sprintf("Result 2 for '%s'", query),
				fmt.Sprintf("Result 3 for '%s'", query),
			}

			return &tools.ToolOutput{
				Result:  results,
				Success: true,
			}, nil
		},
	)
}

// LoggingCallback 日志回调
type LoggingCallback struct {
	agentcore.BaseCallback
}

func (l *LoggingCallback) OnStart(ctx context.Context, input interface{}) error {
	fmt.Printf("\n[START] Input: %v\n", input)
	return nil
}

func (l *LoggingCallback) OnEnd(ctx context.Context, output interface{}) error {
	fmt.Printf("\n[END] Output: %v\n", output)
	return nil
}

func (l *LoggingCallback) OnLLMStart(ctx context.Context, prompts []string, model string) error {
	fmt.Printf("\n[LLM START] Model: %s, Prompts: %d\n", model, len(prompts))
	return nil
}

func (l *LoggingCallback) OnLLMEnd(ctx context.Context, output string, tokenUsage int) error {
	fmt.Printf("[LLM END] Tokens: %d\n", tokenUsage)
	fmt.Printf("Output: %s\n", output)
	return nil
}

func (l *LoggingCallback) OnToolStart(ctx context.Context, toolName string, input interface{}) error {
	fmt.Printf("\n[TOOL START] %s\n  Input: %v\n", toolName, input)
	return nil
}

func (l *LoggingCallback) OnToolEnd(ctx context.Context, toolName string, output interface{}) error {
	fmt.Printf("[TOOL END] %s\n  Output: %v\n", toolName, output)
	return nil
}

func (l *LoggingCallback) OnError(ctx context.Context, err error) error {
	fmt.Printf("\n[ERROR] %v\n", err)
	return nil
}
