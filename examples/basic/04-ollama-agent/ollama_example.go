package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/utils/json"
)

func main() {
	fmt.Println("=== Ollama LLM Example ===")
	fmt.Println()

	// 示例 1: 基本 Ollama 客户端使用
	basicOllamaExample()

	// 示例 2: 使用 Ollama 的 Chat 方法
	ollamaAgentExample()

	// 示例 3: 使用 Complete 方法
	ollamaWithCompletionExample()

	// 示例 4: 列出可用模型
	listOllamaModels()

	// 示例 5: 使用 Ollama LLM 调用工具（getCurrentTimeTool）
	ollamaWithToolsExample()
}

// basicOllamaExample 演示基本的 Ollama 客户端使用
func basicOllamaExample() {
	fmt.Println("1. Basic Ollama Client Usage")
	fmt.Println("----------------------------")

	// 创建 Ollama 客户端（使用默认配置）
	client, err := providers.NewOllamaClientSimple("gemma3:12b")
	if err != nil {
		log.Printf("Error creating Ollama client: %v\n", err)
		return
	}

	// 检查 Ollama 是否可用
	if !client.IsAvailable() {
		fmt.Println("❌ Ollama is not available. Please ensure Ollama is running on http://localhost:11434")
		fmt.Println("   Install Ollama: https://ollama.ai/")
		fmt.Println("   Start Ollama: ollama serve")
		fmt.Println("   Pull a model: ollama pull gemma3:12b")
		return
	}

	fmt.Println("✅ Ollama is available")

	// 简单对话
	ctx := context.Background()
	messages := []llm.Message{
		llm.SystemMessage("You are a helpful assistant."),
		llm.UserMessage("What is Go programming language in one sentence?"),
	}

	response, err := client.Chat(ctx, messages)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n", response.Content)
	fmt.Printf("Model: %s\n", response.Model)
	fmt.Printf("Tokens used: %d\n", response.TokensUsed)
	fmt.Println()
}

// ollamaAgentExample 演示使用 Ollama 创建简单对话
func ollamaAgentExample() {
	fmt.Println("2. Ollama Chat Example")
	fmt.Println("-----------------------")

	// 创建 Ollama 客户端（使用标准配置）
	ollamaClient, err := providers.NewOllamaWithOptions(
		llm.WithModel("gemma3:12b"), // 或者使用其他模型如 "mistral", "codellama", "phi"
		llm.WithTemperature(0.7),
		llm.WithMaxTokens(1000),
	)
	if err != nil {
		log.Printf("Error creating Ollama client: %v\n", err)
		return
	}

	// 检查可用性
	if !ollamaClient.IsAvailable() {
		fmt.Println("❌ Ollama not available. Skipping this example.")
		return
	}

	// 直接使用 Chat 方法
	ctx := context.Background()
	messages := []llm.Message{
		llm.SystemMessage("You are a helpful AI assistant powered by Ollama. Be concise and clear."),
		llm.UserMessage("Explain Docker in 2 sentences."),
	}

	response, err := ollamaClient.Chat(ctx, messages)
	if err != nil {
		log.Printf("Chat failed: %v\n", err)
		return
	}

	fmt.Printf("Ollama Response: %s\n", response.Content)
	fmt.Printf("Model used: %s\n", response.Model)
	fmt.Println()
}

// ollamaWithCompletionExample 演示使用 Complete 方法
func ollamaWithCompletionExample() {
	fmt.Println("3. Ollama Completion Example")
	fmt.Println("-----------------------------")

	// 创建 Ollama 客户端
	ollamaClient, err := providers.NewOllamaClientSimple("gemma3:12b")
	if err != nil {
		log.Printf("Failed to create Ollama client: %v\n", err)
		return
	}

	if !ollamaClient.IsAvailable() {
		fmt.Println("❌ Ollama not available. Skipping this example.")
		return
	}

	// 使用 Complete 方法
	ctx := context.Background()
	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.SystemMessage("You are a helpful assistant that provides clear, concise answers."),
			llm.UserMessage("What is 25 * 4? Just give me the number."),
		},
		Temperature: 0.1, // 低温度获得更确定的答案
		MaxTokens:   50,
	}

	response, err := ollamaClient.Complete(ctx, req)
	if err != nil {
		log.Printf("Completion failed: %v\n", err)
		return
	}

	fmt.Printf("Response: %s\n", response.Content)
	fmt.Println()
}

// listOllamaModels 列出可用的 Ollama 模型
func listOllamaModels() {
	fmt.Println("4. Available Ollama Models")
	fmt.Println("--------------------------")

	client, err := providers.NewOllamaClientSimple("")
	if err != nil {
		log.Printf("Failed to create Ollama client: %v\n", err)
		return
	}

	if !client.IsAvailable() {
		fmt.Println("❌ Ollama not available")
		return
	}

	models, err := client.ListModels()
	if err != nil {
		log.Printf("Failed to list models: %v\n", err)
		return
	}

	if len(models) == 0 {
		fmt.Println("No models installed. Pull a model first:")
		fmt.Println("  ollama pull gemma3:12b")
		fmt.Println("  ollama pull mistral")
		fmt.Println("  ollama pull codellama")
		return
	}

	fmt.Println("Available models:")
	for _, model := range models {
		fmt.Printf("  - %s\n", model)
	}
	fmt.Println()

	// 演示如何使用不同的模型
	fmt.Println("Example: Using different models")
	for _, modelName := range []string{"gemma3:12b", "mistral", "phi"} {
		// 检查模型是否在可用列表中
		modelAvailable := false
		for _, m := range models {
			if strings.HasPrefix(m, modelName) {
				modelAvailable = true
				break
			}
		}

		if modelAvailable {
			fmt.Printf("\n  Using %s model:\n", modelName)
			client, err := providers.NewOllamaClientSimple(modelName)
			if err != nil {
				fmt.Printf("    Error creating client: %v\n", err)
				continue
			}
			ctx := context.Background()

			resp, err := client.Chat(ctx, []llm.Message{
				llm.UserMessage("Say hello in one word"),
			})

			if err != nil {
				fmt.Printf("    Error: %v\n", err)
			} else {
				fmt.Printf("    Response: %s\n", resp.Content)
			}
		}
	}
}

// 额外的辅助函数：拉取模型（如果需要） - kept for reference
/*
func pullModelIfNeeded(modelName string) error {
	client := providers.NewOllamaClientSimple(modelName)

	// 检查模型是否已存在
	models, err := client.ListModels()
	if err != nil {
		return err
	}

	for _, m := range models {
		if strings.HasPrefix(m, modelName) {
			return nil // 模型已存在
		}
	}

	// 拉取模型
	fmt.Printf("Pulling model %s... (this may take a while)\n", modelName)
	return client.PullModel(modelName)
}
*/

// getCurrentTimeTool 获取当前时间的工具
func getCurrentTimeTool() interfaces.Tool {
	return tools.NewBaseTool(
		"get_current_time",
		"Get the current local time in format YYYY-MM-DD HH:MM:SS",
		`{
			"type": "object",
			"properties": {},
			"required": []
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			currentTime := time.Now().Format("2006-01-02 15:04:05")
			return &interfaces.ToolOutput{
				Result:  currentTime,
				Success: true,
			}, nil
		},
	)
}

// ollamaWithToolsExample 演示使用 Ollama LLM 调用工具
func ollamaWithToolsExample() {
	fmt.Println("5. Ollama with Tools Example (getCurrentTimeTool)")
	fmt.Println("--------------------------------------------------")

	// 创建 Ollama 客户端
	ollamaClient, err := providers.NewOllamaClientSimple("gemma3:12b")
	if err != nil {
		log.Printf("Failed to create Ollama client: %v\n", err)
		return
	}

	if !ollamaClient.IsAvailable() {
		fmt.Println("❌ Ollama not available. Skipping this example.")
		return
	}

	// 创建获取当前时间的工具
	timeTool := getCurrentTimeTool()

	// 创建一个简单的天气查询工具作为对比
	weatherTool := tools.NewBaseTool(
		"get_weather",
		"Get the current weather for a given city",
		`{
			"type": "object",
			"properties": {
				"city": {
					"type": "string",
					"description": "The city name"
				}
			},
			"required": ["city"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			city, ok := input.Args["city"].(string)
			if !ok {
				return &interfaces.ToolOutput{
					Success: false,
					Error:   "city must be a string",
				}, nil
			}

			// 模拟天气数据
			weatherData := map[string]interface{}{
				"city":        city,
				"temperature": 25,
				"condition":   "Sunny",
				"humidity":    60,
			}

			return &interfaces.ToolOutput{
				Result:  weatherData,
				Success: true,
			}, nil
		},
	)

	fmt.Println("\n📌 示例 1: 直接调用工具")
	fmt.Println("-------------------------")

	// 直接调用时间工具
	ctx := context.Background()
	timeInput := &interfaces.ToolInput{
		Args:    map[string]interface{}{},
		Context: ctx,
	}

	timeOutput, err := timeTool.Invoke(ctx, timeInput)
	if err != nil {
		log.Printf("Error invoking time tool: %v\n", err)
	} else {
		fmt.Printf("✅ Current Time: %v\n", timeOutput.Result)
	}

	// 直接调用天气工具
	weatherInput := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"city": "Beijing",
		},
		Context: ctx,
	}

	weatherOutput, err := weatherTool.Invoke(ctx, weatherInput)
	if err != nil {
		log.Printf("Error invoking weather tool: %v\n", err)
	} else {
		weatherJSON, _ := json.MarshalIndent(weatherOutput.Result, "", "  ")
		fmt.Printf("✅ Weather Data:\n%s\n", string(weatherJSON))
	}

	fmt.Println("\n📌 示例 2: 通过 LLM 调用工具（模拟）")
	fmt.Println("-------------------------------------")

	// 构建工具描述，用于 LLM 理解
	toolDescriptions := buildToolDescriptions([]interfaces.Tool{timeTool, weatherTool})

	// 创建包含工具信息的 prompt
	userQuery := "What time is it now?"
	messages := []llm.Message{
		llm.SystemMessage(fmt.Sprintf(`You are a helpful AI assistant with access to the following tools:

%s

When you need to use a tool, respond in this format:
Tool: <tool_name>
Input: <tool_input_as_json>

After receiving the tool result, provide a natural language response to the user.`, toolDescriptions)),
		llm.UserMessage(userQuery),
	}

	// 调用 LLM
	response, err := ollamaClient.Chat(ctx, messages)
	if err != nil {
		log.Printf("Error calling LLM: %v\n", err)
		return
	}

	fmt.Printf("🤖 LLM Response:\n%s\n\n", response.Content)

	// 解析 LLM 响应，检查是否需要调用工具
	if strings.Contains(response.Content, "Tool:") {
		toolName, toolInput := parseToolCall(response.Content)
		fmt.Printf("🔧 Detected Tool Call:\n")
		fmt.Printf("   Tool: %s\n", toolName)
		fmt.Printf("   Input: %s\n\n", toolInput)

		// 执行工具调用
		var toolResult interface{}
		switch toolName {
		case "get_current_time":
			output, toolErr := timeTool.Invoke(ctx, &interfaces.ToolInput{
				Args:    map[string]interface{}{},
				Context: ctx,
			})
			if toolErr == nil {
				toolResult = output.Result
			}
		case "get_weather":
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(toolInput), &args); err != nil {
				log.Printf("Error unmarshalling tool input: %v\n", err)
				return
			}
			output, toolErr := weatherTool.Invoke(ctx, &interfaces.ToolInput{
				Args:    args,
				Context: ctx,
			})
			if toolErr == nil {
				toolResult = output.Result
			}
		}

		if toolResult != nil {
			fmt.Printf("🎯 Tool Result: %v\n\n", toolResult)

			// 将工具结果返回给 LLM 生成最终回复
			messages = append(messages,
				llm.AssistantMessage(response.Content),
				llm.UserMessage(fmt.Sprintf("Tool Result: %v\n\nNow provide a natural language response to the user.", toolResult)),
			)

			finalResponse, finalErr := ollamaClient.Chat(ctx, messages)
			if finalErr != nil {
				log.Printf("Error getting final response: %v\n", finalErr)
				return
			}

			fmt.Printf("💬 Final Response:\n%s\n", finalResponse.Content)
		}
	}

	fmt.Println("\n📌 示例 3: 多个工具调用场景")
	fmt.Println("-----------------------------")

	userQuery2 := "What's the weather in Shanghai?"
	messages2 := []llm.Message{
		llm.SystemMessage(fmt.Sprintf(`You are a helpful AI assistant with access to the following tools:

%s

When you need to use a tool, respond in this format:
Tool: <tool_name>
Input: <tool_input_as_json>`, toolDescriptions)),
		llm.UserMessage(userQuery2),
	}

	response2, err := ollamaClient.Chat(ctx, messages2)
	if err != nil {
		log.Printf("Error calling LLM: %v\n", err)
		return
	}

	fmt.Printf("🤖 LLM Response:\n%s\n\n", response2.Content)

	// 解析并执行工具调用
	if strings.Contains(response2.Content, "Tool:") {
		toolName, toolInput := parseToolCall(response2.Content)
		fmt.Printf("🔧 Detected Tool Call:\n")
		fmt.Printf("   Tool: %s\n", toolName)
		fmt.Printf("   Input: %s\n\n", toolInput)

		if toolName == "get_weather" {
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(toolInput), &args); err != nil {
				log.Printf("Error unmarshalling tool input: %v\n", err)
				return
			}
			output, err := weatherTool.Invoke(ctx, &interfaces.ToolInput{
				Args:    args,
				Context: ctx,
			})
			if err == nil {
				weatherJSON, _ := json.MarshalIndent(output.Result, "", "  ")
				fmt.Printf("🎯 Tool Result:\n%s\n", string(weatherJSON))
			}
		}
	}

	fmt.Println()
}

// buildToolDescriptions 构建工具描述字符串
func buildToolDescriptions(tools []interfaces.Tool) string {
	var descriptions []string
	for _, tool := range tools {
		desc := fmt.Sprintf("- %s: %s\n  Schema: %s",
			tool.Name(),
			tool.Description(),
			tool.ArgsSchema(),
		)
		descriptions = append(descriptions, desc)
	}
	return strings.Join(descriptions, "\n\n")
}

// parseToolCall 解析 LLM 响应中的工具调用
func parseToolCall(response string) (toolName string, input string) {
	lines := strings.Split(response, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "Tool:") {
			toolName = strings.TrimSpace(strings.TrimPrefix(line, "Tool:"))
		}
		if strings.HasPrefix(line, "Input:") {
			input = strings.TrimSpace(strings.TrimPrefix(line, "Input:"))
			// 如果输入跨多行，合并它们
			if i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "{") {
				input = strings.TrimSpace(lines[i+1])
			}
		}
	}
	return
}
