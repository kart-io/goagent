// Package main 演示如何创建自定义 MCP 工具
//
// 本示例展示：
// - 创建自定义工具结构
// - 实现 MCPTool 接口
// - 复杂参数验证
// - 流式输出支持
// - 工具元数据定义
// - 错误处理最佳实践
package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kart-io/goagent/mcp/core"
	"github.com/kart-io/goagent/mcp/toolbox"
)

// TextAnalyzerTool 文本分析工具
// 分析文本的字符数、单词数、行数等信息
type TextAnalyzerTool struct {
	*core.BaseTool
}

// NewTextAnalyzerTool 创建文本分析工具
func NewTextAnalyzerTool() *TextAnalyzerTool {
	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"text": {
				Type:        "string",
				Description: "要分析的文本内容",
			},
			"include_word_frequency": {
				Type:        "boolean",
				Description: "是否包含词频统计",
				Default:     false,
			},
			"top_words": {
				Type:        "integer",
				Description: "返回最常见的前 N 个词",
				Default:     10,
			},
		},
		Required: []string{"text"},
	}

	return &TextAnalyzerTool{
		BaseTool: core.NewBaseTool(
			"text_analyzer",
			"分析文本的统计信息（字符数、单词数、行数、词频等）",
			"text",
			schema,
		),
	}
}

// Execute 执行文本分析
func (t *TextAnalyzerTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
	startTime := time.Now()

	text, _ := input["text"].(string)
	includeFreq := false
	if v, ok := input["include_word_frequency"].(bool); ok {
		includeFreq = v
	}
	topWords := 10
	if v, ok := input["top_words"].(float64); ok {
		topWords = int(v)
	}

	// 基础统计
	charCount := len(text)
	charCountNoSpaces := len(strings.ReplaceAll(text, " ", ""))
	lines := strings.Split(text, "\n")
	lineCount := len(lines)

	// 单词统计
	words := strings.Fields(text)
	wordCount := len(words)

	// 句子统计（简单实现：以 . ! ? 结尾）
	sentencePattern := regexp.MustCompile(`[.!?]+`)
	sentences := sentencePattern.Split(text, -1)
	sentenceCount := 0
	for _, s := range sentences {
		if strings.TrimSpace(s) != "" {
			sentenceCount++
		}
	}

	result := map[string]interface{}{
		"character_count":           charCount,
		"character_count_no_spaces": charCountNoSpaces,
		"word_count":                wordCount,
		"line_count":                lineCount,
		"sentence_count":            sentenceCount,
		"average_word_length":       0.0,
		"average_words_per_line":    0.0,
	}

	if wordCount > 0 {
		totalWordLen := 0
		for _, w := range words {
			totalWordLen += len(w)
		}
		result["average_word_length"] = float64(totalWordLen) / float64(wordCount)
	}

	if lineCount > 0 {
		result["average_words_per_line"] = float64(wordCount) / float64(lineCount)
	}

	// 词频统计
	if includeFreq {
		wordFreq := make(map[string]int)
		for _, word := range words {
			// 转小写，去除标点
			cleanWord := strings.ToLower(regexp.MustCompile(`[^\p{L}]`).ReplaceAllString(word, ""))
			if cleanWord != "" {
				wordFreq[cleanWord]++
			}
		}

		// 获取前 N 个高频词
		type wordCount struct {
			Word  string
			Count int
		}
		var sortedWords []wordCount
		for w, c := range wordFreq {
			sortedWords = append(sortedWords, wordCount{w, c})
		}
		// 简单排序
		for i := 0; i < len(sortedWords); i++ {
			for j := i + 1; j < len(sortedWords); j++ {
				if sortedWords[j].Count > sortedWords[i].Count {
					sortedWords[i], sortedWords[j] = sortedWords[j], sortedWords[i]
				}
			}
		}

		topWordsList := make([]map[string]interface{}, 0)
		for i := 0; i < topWords && i < len(sortedWords); i++ {
			topWordsList = append(topWordsList, map[string]interface{}{
				"word":  sortedWords[i].Word,
				"count": sortedWords[i].Count,
			})
		}
		result["top_words"] = topWordsList
		result["unique_words"] = len(wordFreq)
	}

	return &core.ToolResult{
		Success:   true,
		Data:      result,
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
	}, nil
}

// Validate 验证输入参数
func (t *TextAnalyzerTool) Validate(input map[string]interface{}) error {
	text, ok := input["text"].(string)
	if !ok {
		return &core.ErrInvalidInput{Field: "text", Message: "必须是字符串类型"}
	}
	if text == "" {
		return &core.ErrInvalidInput{Field: "text", Message: "不能为空"}
	}
	if len(text) > 1000000 {
		return &core.ErrInvalidInput{Field: "text", Message: "文本长度不能超过 1MB"}
	}

	if topWords, ok := input["top_words"].(float64); ok {
		if topWords < 1 || topWords > 100 {
			return &core.ErrInvalidInput{Field: "top_words", Message: "必须在 1-100 之间"}
		}
	}

	return nil
}

// CalculatorTool 计算器工具
// 支持基本数学运算和高级函数
type CalculatorTool struct {
	*core.BaseTool
}

// NewCalculatorTool 创建计算器工具
func NewCalculatorTool() *CalculatorTool {
	minVal := float64(-1e15)
	maxVal := float64(1e15)

	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"operation": {
				Type:        "string",
				Description: "运算类型",
				Enum:        []interface{}{"add", "subtract", "multiply", "divide", "power", "sqrt", "percentage"},
			},
			"a": {
				Type:        "number",
				Description: "第一个操作数",
				Minimum:     &minVal,
				Maximum:     &maxVal,
			},
			"b": {
				Type:        "number",
				Description: "第二个操作数（某些运算可选）",
				Minimum:     &minVal,
				Maximum:     &maxVal,
			},
		},
		Required: []string{"operation", "a"},
	}

	return &CalculatorTool{
		BaseTool: core.NewBaseTool(
			"calculator",
			"执行数学运算（加减乘除、幂运算、平方根、百分比）",
			"math",
			schema,
		),
	}
}

// Execute 执行计算
func (t *CalculatorTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
	startTime := time.Now()

	operation, _ := input["operation"].(string)
	a, _ := input["a"].(float64)
	b, _ := input["b"].(float64)

	var result float64
	var expression string

	switch operation {
	case "add":
		result = a + b
		expression = fmt.Sprintf("%v + %v = %v", a, b, result)
	case "subtract":
		result = a - b
		expression = fmt.Sprintf("%v - %v = %v", a, b, result)
	case "multiply":
		result = a * b
		expression = fmt.Sprintf("%v × %v = %v", a, b, result)
	case "divide":
		if b == 0 {
			return &core.ToolResult{
				Success:   false,
				Error:     "除数不能为零",
				ErrorCode: "DIVISION_BY_ZERO",
				Duration:  time.Since(startTime),
				Timestamp: time.Now(),
			}, nil
		}
		result = a / b
		expression = fmt.Sprintf("%v ÷ %v = %v", a, b, result)
	case "power":
		result = 1
		for i := 0; i < int(b); i++ {
			result *= a
		}
		expression = fmt.Sprintf("%v ^ %v = %v", a, b, result)
	case "sqrt":
		if a < 0 {
			return &core.ToolResult{
				Success:   false,
				Error:     "不能对负数开平方根",
				ErrorCode: "INVALID_OPERATION",
				Duration:  time.Since(startTime),
				Timestamp: time.Now(),
			}, nil
		}
		// 简单的牛顿迭代法
		result = a
		for i := 0; i < 100; i++ {
			result = (result + a/result) / 2
		}
		expression = fmt.Sprintf("√%v = %v", a, result)
	case "percentage":
		result = a * b / 100
		expression = fmt.Sprintf("%v%% of %v = %v", b, a, result)
	default:
		return &core.ToolResult{
			Success:   false,
			Error:     fmt.Sprintf("不支持的运算: %s", operation),
			ErrorCode: "UNSUPPORTED_OPERATION",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, nil
	}

	return &core.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"result":     result,
			"expression": expression,
			"operation":  operation,
			"operands":   []float64{a, b},
		},
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
	}, nil
}

// Validate 验证输入
func (t *CalculatorTool) Validate(input map[string]interface{}) error {
	operation, ok := input["operation"].(string)
	if !ok || operation == "" {
		return &core.ErrInvalidInput{Field: "operation", Message: "必须指定运算类型"}
	}

	validOps := map[string]bool{
		"add": true, "subtract": true, "multiply": true, "divide": true,
		"power": true, "sqrt": true, "percentage": true,
	}
	if !validOps[operation] {
		return &core.ErrInvalidInput{Field: "operation", Message: "不支持的运算类型"}
	}

	if _, ok := input["a"].(float64); !ok {
		return &core.ErrInvalidInput{Field: "a", Message: "必须是数字类型"}
	}

	// 检查需要第二个操作数的运算
	needsB := map[string]bool{
		"add": true, "subtract": true, "multiply": true, "divide": true,
		"power": true, "percentage": true,
	}
	if needsB[operation] {
		if _, ok := input["b"].(float64); !ok {
			return &core.ErrInvalidInput{Field: "b", Message: "此运算需要第二个操作数"}
		}
	}

	return nil
}

// StreamingCounterTool 流式计数工具
// 演示流式输出功能
type StreamingCounterTool struct {
	*core.BaseTool
}

// NewStreamingCounterTool 创建流式计数工具
func NewStreamingCounterTool() *StreamingCounterTool {
	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"start": {
				Type:        "integer",
				Description: "起始数字",
				Default:     1,
			},
			"end": {
				Type:        "integer",
				Description: "结束数字",
				Default:     10,
			},
			"delay_ms": {
				Type:        "integer",
				Description: "每次计数的延迟（毫秒）",
				Default:     100,
			},
		},
		Required: []string{},
	}

	return &StreamingCounterTool{
		BaseTool: core.NewBaseTool(
			"streaming_counter",
			"流式输出计数（演示流式功能）",
			"demo",
			schema,
		),
	}
}

// Execute 执行流式计数
func (t *StreamingCounterTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
	startTime := time.Now()

	start := 1
	end := 10
	delayMs := 100

	if v, ok := input["start"].(float64); ok {
		start = int(v)
	}
	if v, ok := input["end"].(float64); ok {
		end = int(v)
	}
	if v, ok := input["delay_ms"].(float64); ok {
		delayMs = int(v)
	}

	// 创建流式输出通道
	ch := make(chan interface{}, end-start+1)

	// 启动计数协程
	go func() {
		defer close(ch)
		for i := start; i <= end; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				ch <- map[string]interface{}{
					"current": i,
					"total":   end - start + 1,
					"percent": float64(i-start+1) / float64(end-start+1) * 100,
				}
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
		}
	}()

	return &core.ToolResult{
		Success:       true,
		IsStreaming:   true,
		StreamChannel: ch,
		Data: map[string]interface{}{
			"start":    start,
			"end":      end,
			"delay_ms": delayMs,
			"message":  "流式计数已启动",
		},
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
	}, nil
}

// Validate 验证输入
func (t *StreamingCounterTool) Validate(input map[string]interface{}) error {
	if start, ok := input["start"].(float64); ok {
		if end, ok := input["end"].(float64); ok {
			if start > end {
				return &core.ErrInvalidInput{Field: "start", Message: "起始值不能大于结束值"}
			}
			if end-start > 1000 {
				return &core.ErrInvalidInput{Field: "end", Message: "计数范围不能超过 1000"}
			}
		}
	}
	return nil
}

func main() {
	fmt.Println("=== MCP 自定义工具示例 ===")

	// 创建工具箱
	tb := toolbox.NewStandardToolBox()

	// 注册自定义工具
	fmt.Println("步骤 1: 注册自定义工具")

	customTools := []core.MCPTool{
		NewTextAnalyzerTool(),
		NewCalculatorTool(),
		NewStreamingCounterTool(),
	}

	for _, tool := range customTools {
		if err := tb.Register(tool); err != nil {
			fmt.Printf("  ✗ 注册 %s 失败: %v\n", tool.Name(), err)
		} else {
			fmt.Printf("  ✓ 已注册: %s - %s\n", tool.Name(), tool.Description())
		}
	}
	fmt.Println()

	// 列出所有工具
	fmt.Println("步骤 2: 列出所有已注册工具")
	for _, tool := range tb.List() {
		fmt.Printf("  - %s [%s]: %s\n", tool.Name(), tool.Category(), tool.Description())
	}
	fmt.Println()

	ctx := context.Background()

	// 测试文本分析工具
	fmt.Println("步骤 3: 测试文本分析工具")
	testText := `
The quick brown fox jumps over the lazy dog.
This is a sample text for testing the text analyzer tool.
It includes multiple sentences, lines, and words.
The analyzer will count characters, words, sentences, and more.
It can also provide word frequency analysis if requested.
`

	analyzerCall := &core.ToolCall{
		ID:       "text-1",
		ToolName: "text_analyzer",
		Input: map[string]interface{}{
			"text":                   testText,
			"include_word_frequency": true,
			"top_words":              5,
		},
	}

	result, err := tb.Execute(ctx, analyzerCall)
	if err != nil {
		fmt.Printf("  ✗ 执行失败: %v\n", err)
	} else {
		fmt.Println("  ✓ 分析结果:")
		data := result.Result.Data.(map[string]interface{})
		fmt.Printf("    字符数: %v\n", data["character_count"])
		fmt.Printf("    单词数: %v\n", data["word_count"])
		fmt.Printf("    行数: %v\n", data["line_count"])
		fmt.Printf("    句子数: %v\n", data["sentence_count"])
		fmt.Printf("    平均词长: %.2f\n", data["average_word_length"])
		fmt.Printf("    独立单词数: %v\n", data["unique_words"])
		fmt.Println("    高频词:")
		if topWords, ok := data["top_words"].([]map[string]interface{}); ok {
			for _, w := range topWords {
				fmt.Printf("      - %s: %v 次\n", w["word"], w["count"])
			}
		}
	}
	fmt.Println()

	// 测试计算器工具
	fmt.Println("步骤 4: 测试计算器工具")

	calculations := []map[string]interface{}{
		{"operation": "add", "a": 10.0, "b": 5.0},
		{"operation": "multiply", "a": 7.0, "b": 8.0},
		{"operation": "divide", "a": 100.0, "b": 4.0},
		{"operation": "power", "a": 2.0, "b": 10.0},
		{"operation": "sqrt", "a": 144.0},
		{"operation": "percentage", "a": 200.0, "b": 15.0},
	}

	for i, calc := range calculations {
		calcCall := &core.ToolCall{
			ID:       fmt.Sprintf("calc-%d", i+1),
			ToolName: "calculator",
			Input:    calc,
		}

		result, err := tb.Execute(ctx, calcCall)
		if err != nil {
			fmt.Printf("  ✗ 计算失败: %v\n", err)
		} else if !result.Result.Success {
			fmt.Printf("  ✗ 计算错误: %s\n", result.Result.Error)
		} else {
			data := result.Result.Data.(map[string]interface{})
			fmt.Printf("  ✓ %s\n", data["expression"])
		}
	}

	// 测试除零错误
	divZeroCall := &core.ToolCall{
		ID:       "calc-divzero",
		ToolName: "calculator",
		Input:    map[string]interface{}{"operation": "divide", "a": 10.0, "b": 0.0},
	}
	result, _ = tb.Execute(ctx, divZeroCall)
	if !result.Result.Success {
		fmt.Printf("  ✓ 除零错误处理: %s\n", result.Result.Error)
	}
	fmt.Println()

	// 测试流式计数工具
	fmt.Println("步骤 5: 测试流式计数工具")
	streamCall := &core.ToolCall{
		ID:       "stream-1",
		ToolName: "streaming_counter",
		Input: map[string]interface{}{
			"start":    1.0,
			"end":      5.0,
			"delay_ms": 200.0,
		},
	}

	result, err = tb.Execute(ctx, streamCall)
	if err != nil {
		fmt.Printf("  ✗ 执行失败: %v\n", err)
	} else {
		fmt.Println("  ✓ 流式输出:")
		if result.Result.IsStreaming && result.Result.StreamChannel != nil {
			for item := range result.Result.StreamChannel {
				if data, ok := item.(map[string]interface{}); ok {
					fmt.Printf("    计数: %v / %v (%.0f%%)\n",
						data["current"], data["total"], data["percent"])
				}
			}
		}
	}
	fmt.Println()

	// 测试参数验证
	fmt.Println("步骤 6: 测试参数验证")

	invalidCalls := []struct {
		name  string
		call  *core.ToolCall
		error string
	}{
		{
			name: "空文本",
			call: &core.ToolCall{
				ID:       "invalid-1",
				ToolName: "text_analyzer",
				Input:    map[string]interface{}{"text": ""},
			},
			error: "text 不能为空",
		},
		{
			name: "无效运算",
			call: &core.ToolCall{
				ID:       "invalid-2",
				ToolName: "calculator",
				Input:    map[string]interface{}{"operation": "invalid", "a": 1.0},
			},
			error: "不支持的运算类型",
		},
		{
			name: "计数范围过大",
			call: &core.ToolCall{
				ID:       "invalid-3",
				ToolName: "streaming_counter",
				Input:    map[string]interface{}{"start": 1.0, "end": 2000.0},
			},
			error: "计数范围不能超过 1000",
		},
	}

	for _, tc := range invalidCalls {
		err := tb.Validate(tc.call)
		if err != nil {
			fmt.Printf("  ✓ %s: 验证失败（预期）- %v\n", tc.name, err)
		} else {
			fmt.Printf("  ✗ %s: 验证应该失败但通过了\n", tc.name)
		}
	}
	fmt.Println()

	// 打印统计信息
	fmt.Println("步骤 7: 工具箱统计信息")
	stats := tb.Statistics()
	fmt.Printf("  工具总数: %d\n", stats.TotalTools)
	fmt.Printf("  总调用次数: %d\n", stats.TotalCalls)
	fmt.Printf("  成功调用: %d\n", stats.SuccessfulCalls)
	fmt.Printf("  失败调用: %d\n", stats.FailedCalls)
	fmt.Printf("  平均延迟: %.2f ms\n", stats.AverageLatency)

	fmt.Println("\n=== 自定义工具示例完成 ===")
}
