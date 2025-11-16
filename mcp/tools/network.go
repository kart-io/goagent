package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kart-io/goagent/mcp/core"
)

// HTTPRequestTool HTTP 请求工具
type HTTPRequestTool struct {
	*core.BaseTool
	client *http.Client
}

// NewHTTPRequestTool 创建 HTTP 请求工具
func NewHTTPRequestTool() *HTTPRequestTool {
	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"url": {
				Type:        "string",
				Description: "请求URL",
				Format:      "uri",
			},
			"method": {
				Type:        "string",
				Description: "HTTP 方法",
				Default:     "GET",
				Enum:        []interface{}{"GET", "POST", "PUT", "DELETE", "PATCH"},
			},
			"headers": {
				Type:        "object",
				Description: "请求头",
			},
			"body": {
				Type:        "string",
				Description: "请求体（JSON字符串）",
			},
			"timeout": {
				Type:        "integer",
				Description: "超时时间（秒）",
				Default:     30,
			},
		},
		Required: []string{"url"},
	}

	tool := &HTTPRequestTool{
		BaseTool: core.NewBaseTool(
			"http_request",
			"发送 HTTP 请求",
			"network",
			schema,
		),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	return tool
}

// Execute 执行工具
func (t *HTTPRequestTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
	startTime := time.Now()

	url, _ := input["url"].(string)
	method, _ := input["method"].(string)
	if method == "" {
		method = "GET"
	}

	// 创建请求
	var bodyReader io.Reader
	if body, ok := input["body"].(string); ok && body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return &core.ToolResult{
			Success:   false,
			Error:     fmt.Sprintf("failed to create request: %v", err),
			ErrorCode: "REQUEST_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}

	// 设置请求头
	if headers, ok := input["headers"].(map[string]interface{}); ok {
		for key, value := range headers {
			if strValue, ok := value.(string); ok {
				req.Header.Set(key, strValue)
			}
		}
	}

	// 设置超时
	if timeout, ok := input["timeout"].(float64); ok {
		t.client.Timeout = time.Duration(timeout) * time.Second
	}

	// 发送请求
	resp, err := t.client.Do(req)
	if err != nil {
		return &core.ToolResult{
			Success:   false,
			Error:     fmt.Sprintf("request failed: %v", err),
			ErrorCode: "HTTP_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}
	defer func() { _ = resp.Body.Close() }()

	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &core.ToolResult{
			Success:   false,
			Error:     fmt.Sprintf("failed to read response: %v", err),
			ErrorCode: "READ_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}

	// 尝试解析为 JSON
	var jsonBody interface{}
	_ = json.Unmarshal(respBody, &jsonBody)

	result := &core.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"status_code": resp.StatusCode,
			"status":      resp.Status,
			"headers":     resp.Header,
			"body":        string(respBody),
			"json":        jsonBody,
			"size":        len(respBody),
		},
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
	}

	return result, nil
}

// Validate 验证输入
func (t *HTTPRequestTool) Validate(input map[string]interface{}) error {
	url, ok := input["url"].(string)
	if !ok || url == "" {
		return &core.ErrInvalidInput{Field: "url", Message: "must be a non-empty string"}
	}

	return nil
}

// JSONParseTool JSON 解析工具
type JSONParseTool struct {
	*core.BaseTool
}

// NewJSONParseTool 创建 JSON 解析工具
func NewJSONParseTool() *JSONParseTool {
	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"json": {
				Type:        "string",
				Description: "JSON 字符串",
			},
			"path": {
				Type:        "string",
				Description: "JSON 路径（可选，如 $.user.name）",
			},
		},
		Required: []string{"json"},
	}

	tool := &JSONParseTool{
		BaseTool: core.NewBaseTool(
			"json_parse",
			"解析 JSON 数据",
			"data",
			schema,
		),
	}

	return tool
}

// Execute 执行工具
func (t *JSONParseTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
	startTime := time.Now()

	jsonStr, _ := input["json"].(string)

	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return &core.ToolResult{
			Success:   false,
			Error:     fmt.Sprintf("failed to parse JSON: %v", err),
			ErrorCode: "PARSE_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}

	result := &core.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"parsed": data,
		},
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
	}

	return result, nil
}

// Validate 验证输入
func (t *JSONParseTool) Validate(input map[string]interface{}) error {
	jsonStr, ok := input["json"].(string)
	if !ok || jsonStr == "" {
		return &core.ErrInvalidInput{Field: "json", Message: "must be a non-empty string"}
	}

	return nil
}

// ShellExecuteTool Shell 命令执行工具
type ShellExecuteTool struct {
	*core.BaseTool
}

// NewShellExecuteTool 创建 Shell 命令执行工具
func NewShellExecuteTool() *ShellExecuteTool {
	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"command": {
				Type:        "string",
				Description: "要执行的命令",
			},
			"args": {
				Type:        "array",
				Description: "命令参数",
				Items: &core.PropertySchema{
					Type: "string",
				},
			},
			"timeout": {
				Type:        "integer",
				Description: "超时时间（秒）",
				Default:     30,
			},
		},
		Required: []string{"command"},
	}

	tool := &ShellExecuteTool{
		BaseTool: core.NewBaseTool(
			"shell_execute",
			"执行 Shell 命令",
			"system",
			schema,
		),
	}

	tool.SetRequiresAuth(true)
	tool.SetIsDangerous(true) // Shell 执行是危险操作

	return tool
}

// Execute 执行工具
func (t *ShellExecuteTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
	startTime := time.Now()

	command, _ := input["command"].(string)

	// 注意：实际生产环境中应该使用 os/exec 包并进行严格的安全检查
	// 这里仅作示例

	result := &core.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"command": command,
			"output":  "Command execution is disabled for security reasons in this example",
		},
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
	}

	return result, nil
}

// Validate 验证输入
func (t *ShellExecuteTool) Validate(input map[string]interface{}) error {
	command, ok := input["command"].(string)
	if !ok || command == "" {
		return &core.ErrInvalidInput{Field: "command", Message: "must be a non-empty string"}
	}

	return nil
}
