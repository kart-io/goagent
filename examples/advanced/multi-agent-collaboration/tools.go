package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// ==================== Analysis Tools ====================

// createDataAnalysisTool creates a tool that analyzes data and extracts insights
func createDataAnalysisTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"data": {
				"type": "string",
				"description": "Data or task description to analyze"
			}
		},
		"required": ["data"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		data, ok := input.Args["data"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "data parameter must be a string",
			}, nil
		}

		// Simulate analysis
		words := strings.Fields(data)
		complexity := "low"
		if len(words) > 20 {
			complexity = "medium"
		}
		if len(words) > 50 {
			complexity = "high"
		}

		// Extract key themes (simplified - look for common words)
		keyThemes := []string{}
		themes := map[string]int{}
		for _, word := range words {
			word = strings.ToLower(strings.Trim(word, ".,!?"))
			if len(word) > 4 { // Only consider longer words
				themes[word]++
			}
		}
		for theme, count := range themes {
			if count > 1 {
				keyThemes = append(keyThemes, theme)
			}
		}
		if len(keyThemes) == 0 {
			keyThemes = []string{"general", "analysis", "optimization"}
		}

		// Generate insights
		risks := []string{
			"Potential resource constraints",
			"Timeline dependencies",
			"Technical complexity",
		}

		opportunities := []string{
			"Process automation potential",
			"Performance optimization",
			"Cost reduction possibilities",
		}

		result := map[string]interface{}{
			"data_points":   len(words),
			"complexity":    complexity,
			"key_themes":    keyThemes[:min(3, len(keyThemes))],
			"risks":         risks,
			"opportunities": opportunities,
			"analysis_time": time.Now().Format(time.RFC3339),
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"data_analysis",
		"Analyze data and extract insights including complexity, key themes, risks, and opportunities",
		schema,
		runFunc,
	)
}

// createSummarizeTool creates a tool that summarizes text
func createSummarizeTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"text": {
				"type": "string",
				"description": "Text to summarize"
			}
		},
		"required": ["text"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		text, ok := input.Args["text"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "text parameter must be a string",
			}, nil
		}

		words := strings.Fields(text)
		sentences := strings.Split(text, ".")

		// Create a concise summary (take first 2-3 sentences)
		summaryParts := []string{}
		for i := 0; i < min(3, len(sentences)); i++ {
			if strings.TrimSpace(sentences[i]) != "" {
				summaryParts = append(summaryParts, strings.TrimSpace(sentences[i]))
			}
		}
		summary := strings.Join(summaryParts, ". ")
		if !strings.HasSuffix(summary, ".") {
			summary += "."
		}

		// Extract key points
		keyPoints := fmt.Sprintf("Analyzed %d words across %d sentences. Main topics identified.", len(words), len(sentences))

		result := map[string]interface{}{
			"summary":    summary,
			"word_count": fmt.Sprintf("%d words", len(words)),
			"key_points": keyPoints,
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"summarize",
		"Create a concise summary of text content with key points",
		schema,
		runFunc,
	)
}

// ==================== Strategy Tools ====================

// createFormulateStrategyTool creates a tool that formulates strategic approaches
func createFormulateStrategyTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"analysis": {
				"type": "string",
				"description": "Analysis results to base strategy on"
			}
		},
		"required": ["analysis"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		_, ok := input.Args["analysis"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "analysis parameter must be a string",
			}, nil
		}

		// Generate strategic approach
		phases := []map[string]string{
			{
				"phase":  "Phase 1: Assessment",
				"action": "Evaluate current state and gather baseline metrics",
			},
			{
				"phase":  "Phase 2: Planning",
				"action": "Design optimization approach and allocate resources",
			},
			{
				"phase":  "Phase 3: Implementation",
				"action": "Execute planned optimizations and monitor progress",
			},
		}

		result := map[string]interface{}{
			"approach":        "Phased implementation with continuous monitoring",
			"phases":          phases,
			"timeline":        "2-4 weeks for full implementation",
			"resources":       []string{"Technical team", "Monitoring tools", "Testing environment"},
			"success_metrics": []string{"Performance improvement >20%", "Error rate <1%", "User satisfaction >90%"},
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"formulate_strategy",
		"Create a strategic approach with phases, timeline, resources, and success metrics",
		schema,
		runFunc,
	)
}

// createPrioritizeTasksTool creates a tool that prioritizes tasks
func createPrioritizeTasksTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"tasks": {
				"type": "array",
				"items": {"type": "string"},
				"description": "List of tasks to prioritize"
			}
		},
		"required": ["tasks"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		tasksRaw, ok := input.Args["tasks"]
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "tasks parameter is required",
			}, nil
		}

		// Convert to string slice
		var tasks []string
		switch v := tasksRaw.(type) {
		case []interface{}:
			for _, t := range v {
				if str, ok := t.(string); ok {
					tasks = append(tasks, str)
				}
			}
		case []string:
			tasks = v
		default:
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "tasks must be an array of strings",
			}, nil
		}

		// Prioritize tasks with impact and effort assessment
		prioritized := []map[string]string{}
		priorities := []string{"High", "Medium", "High", "Low", "Medium"}
		impacts := []string{"High", "Medium", "High", "Low", "High"}
		efforts := []string{"Low", "Medium", "Medium", "Low", "High"}

		for i, task := range tasks {
			priorityIdx := i % len(priorities)
			prioritized = append(prioritized, map[string]string{
				"priority": priorities[priorityIdx],
				"task":     task,
				"impact":   impacts[priorityIdx],
				"effort":   efforts[priorityIdx],
			})
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  prioritized,
		}, nil
	}

	return tools.NewBaseTool(
		"prioritize_tasks",
		"Prioritize tasks by impact and effort, returning ordered list with priority levels",
		schema,
		runFunc,
	)
}

// ==================== Execution Tools ====================

// createHTTPRequestTool creates a tool that makes HTTP requests
func createHTTPRequestTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"method": {
				"type": "string",
				"enum": ["GET", "POST", "PUT", "DELETE", "PATCH"],
				"description": "HTTP method"
			},
			"url": {
				"type": "string",
				"description": "URL to request"
			},
			"headers": {
				"type": "object",
				"description": "Request headers (optional)"
			},
			"body": {
				"type": "object",
				"description": "Request body for POST/PUT/PATCH (optional)"
			}
		},
		"required": ["method", "url"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		method, ok := input.Args["method"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "method parameter must be a string",
			}, nil
		}

		url, ok := input.Args["url"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "url parameter must be a string",
			}, nil
		}

		// Create HTTP client with timeout
		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		// Prepare request
		var req *http.Request
		var err error

		if method == "POST" || method == "PUT" || method == "PATCH" {
			bodyData := input.Args["body"]
			if bodyData != nil {
				bodyJSON, _ := json.Marshal(bodyData)
				req, err = http.NewRequestWithContext(ctx, method, url, strings.NewReader(string(bodyJSON)))
				if err == nil {
					req.Header.Set("Content-Type", "application/json")
				}
			} else {
				req, err = http.NewRequestWithContext(ctx, method, url, nil)
			}
		} else {
			req, err = http.NewRequestWithContext(ctx, method, url, nil)
		}

		if err != nil {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   fmt.Sprintf("failed to create request: %v", err),
			}, nil
		}

		// Add custom headers
		if headers, ok := input.Args["headers"].(map[string]interface{}); ok {
			for key, value := range headers {
				if strValue, ok := value.(string); ok {
					req.Header.Set(key, strValue)
				}
			}
		}

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   fmt.Sprintf("request failed: %v", err),
			}, nil
		}
		defer resp.Body.Close()

		// Read response body
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   fmt.Sprintf("failed to read response: %v", err),
			}, nil
		}

		// Parse JSON response if possible
		var bodyData interface{}
		if err := json.Unmarshal(bodyBytes, &bodyData); err != nil {
			bodyData = string(bodyBytes)
		}

		// Collect response headers
		respHeaders := make(map[string]string)
		for key, values := range resp.Header {
			if len(values) > 0 {
				respHeaders[key] = values[0]
			}
		}

		result := map[string]interface{}{
			"status_code": resp.StatusCode,
			"body":        bodyData,
			"headers":     respHeaders,
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"http_request",
		"Make HTTP requests (GET, POST, PUT, DELETE, PATCH) to external APIs",
		schema,
		runFunc,
	)
}

// createExecuteCommandTool creates a tool that simulates command execution
func createExecuteCommandTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "Command to execute"
			}
		},
		"required": ["command"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		command, ok := input.Args["command"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "command parameter must be a string",
			}, nil
		}

		// SAFETY: Simulate command execution instead of actually running it
		// In production, you would add security checks and use exec.CommandContext

		// Simulate different command outputs
		var output string
		var status string

		switch {
		case strings.Contains(command, "ls"):
			output = "file1.txt\nfile2.txt\nconfig.json"
			status = "success"
		case strings.Contains(command, "ps"):
			output = "PID   COMMAND\n1234  app_server\n5678  worker"
			status = "success"
		case strings.Contains(command, "ping"):
			output = "PING successful: 5 packets transmitted, 5 received"
			status = "success"
		default:
			output = fmt.Sprintf("Simulated execution of: %s", command)
			status = "simulated"
		}

		result := map[string]interface{}{
			"command": command,
			"status":  status,
			"output":  output,
			"note":    "Command execution is simulated for safety",
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"execute_command",
		"Execute system commands (simulated for safety). In production, implement with proper security checks.",
		schema,
		runFunc,
	)
}

// createFileOperationsTool creates a tool for file operations
func createFileOperationsTool() interfaces.Tool {
	schema := `{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["read", "write", "list", "delete"],
				"description": "File operation to perform"
			},
			"path": {
				"type": "string",
				"description": "File or directory path"
			},
			"content": {
				"type": "string",
				"description": "Content to write (for write operation)"
			}
		},
		"required": ["operation", "path"]
	}`

	runFunc := func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		operation, ok := input.Args["operation"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "operation parameter must be a string",
			}, nil
		}

		path, ok := input.Args["path"].(string)
		if !ok {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   "path parameter must be a string",
			}, nil
		}

		// SAFETY: Simulate file operations instead of actually performing them
		// In production, add security checks and use os package functions

		var result map[string]interface{}

		switch operation {
		case "read":
			result = map[string]interface{}{
				"operation": "read",
				"path":      path,
				"content":   "Simulated file content for: " + path,
				"size":      "1024 bytes",
				"note":      "File operation is simulated for safety",
			}

		case "write":
			content, _ := input.Args["content"].(string)
			result = map[string]interface{}{
				"operation": "write",
				"path":      path,
				"bytes":     len(content),
				"status":    "simulated",
				"note":      "File operation is simulated for safety",
			}

		case "list":
			result = map[string]interface{}{
				"operation": "list",
				"path":      path,
				"files":     []string{"file1.txt", "file2.json", "config.yaml"},
				"count":     3,
				"note":      "File operation is simulated for safety",
			}

		case "delete":
			result = map[string]interface{}{
				"operation": "delete",
				"path":      path,
				"status":    "simulated",
				"note":      "File operation is simulated for safety",
			}

		default:
			return &interfaces.ToolOutput{
				Success: false,
				Error:   fmt.Sprintf("unknown operation: %s", operation),
			}, nil
		}

		return &interfaces.ToolOutput{
			Success: true,
			Result:  result,
		}, nil
	}

	return tools.NewBaseTool(
		"file_operations",
		"Perform file operations (read, write, list, delete) - simulated for safety",
		schema,
		runFunc,
	)
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
