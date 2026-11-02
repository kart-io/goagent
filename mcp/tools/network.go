package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/mcp/core"
	"github.com/kart-io/goagent/utils/httpclient"
	"github.com/kart-io/goagent/utils/json"
)

// HTTPRequestTool HTTP 请求工具
type HTTPRequestTool struct {
	*core.BaseTool
	client *httpclient.Client
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
		client: httpclient.NewClient(&httpclient.Config{
			Timeout: 30 * time.Second,
		}),
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
	req := t.client.R().SetContext(ctx)

	// 设置请求头
	if headers, ok := input["headers"].(map[string]interface{}); ok {
		for key, value := range headers {
			if strValue, ok := value.(string); ok {
				req.SetHeader(key, strValue)
			}
		}
	}

	// 设置请求体
	if body, ok := input["body"].(string); ok && body != "" {
		req.SetBody(body)
	}

	// 设置超时
	if timeout, ok := intInput(input, "timeout"); ok {
		client := httpclient.NewClient(&httpclient.Config{
			Timeout: time.Duration(timeout) * time.Second,
		})
		req = client.R().SetContext(ctx)

		// 重新设置headers和body
		if headers, ok := input["headers"].(map[string]interface{}); ok {
			for key, value := range headers {
				if strValue, ok := value.(string); ok {
					req.SetHeader(key, strValue)
				}
			}
		}
		if body, ok := input["body"].(string); ok && body != "" {
			req.SetBody(body)
		}
	}

	// 发送请求
	var resp *resty.Response
	var err error

	switch strings.ToUpper(method) {
	case "GET":
		resp, err = req.Get(url)
	case "POST":
		resp, err = req.Post(url)
	case "PUT":
		resp, err = req.Put(url)
	case "DELETE":
		resp, err = req.Delete(url)
	case "PATCH":
		resp, err = req.Patch(url)
	default:
		return &core.ToolResult{
			Success:   false,
			Error:     fmt.Sprintf("unsupported HTTP method: %s", method),
			ErrorCode: "METHOD_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, fmt.Errorf("unsupported method: %s", method)
	}

	if err != nil {
		// 错误文本含完整请求 URL，先脱敏
		err = agentErrors.RedactURLError(err)
		return &core.ToolResult{
			Success:   false,
			Error:     fmt.Sprintf("request failed: %v", err),
			ErrorCode: "HTTP_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}

	// 读取响应体
	respBody := resp.Body()

	// 尝试解析为 JSON
	var jsonBody interface{}
	_ = json.Unmarshal(respBody, &jsonBody)

	result := &core.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"status_code": resp.StatusCode(),
			"status":      resp.Status(),
			"headers":     resp.Header(),
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

	resultData := map[string]interface{}{"parsed": data}
	if path, _ := input["path"].(string); path != "" {
		value, err := lookupJSONPath(data, path)
		if err != nil {
			return &core.ToolResult{
				Success:   false,
				Error:     err.Error(),
				ErrorCode: "PATH_ERROR",
				Duration:  time.Since(startTime),
				Timestamp: time.Now(),
			}, err
		}
		resultData["value"] = value
	}

	result := &core.ToolResult{
		Success:   true,
		Data:      resultData,
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
	}

	return result, nil
}

// lookupJSONPath 支持 $.a.b[0] 形式的路径；不支持通配符和过滤表达式。
func lookupJSONPath(data interface{}, path string) (interface{}, error) {
	rest, ok := strings.CutPrefix(path, "$")
	if !ok {
		return nil, fmt.Errorf("JSON path must start with $: %s", path)
	}

	current := data
	for rest != "" {
		switch rest[0] {
		case '.':
			rest = rest[1:]
			end := strings.IndexAny(rest, ".[")
			if end < 0 {
				end = len(rest)
			}
			object, isObject := current.(map[string]interface{})
			value, exists := object[rest[:end]]
			if !isObject || !exists {
				return nil, fmt.Errorf("JSON path %s: key %q not found", path, rest[:end])
			}
			current, rest = value, rest[end:]
		case '[':
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return nil, fmt.Errorf("JSON path %s: unterminated index", path)
			}
			index, err := strconv.Atoi(rest[1:end])
			array, isArray := current.([]interface{})
			if err != nil || !isArray || index < 0 || index >= len(array) {
				return nil, fmt.Errorf("JSON path %s: invalid index %q", path, rest[1:end])
			}
			current, rest = array[index], rest[end+1:]
		default:
			return nil, fmt.Errorf("JSON path %s: unexpected %q", path, rest[0])
		}
	}
	return current, nil
}

// intInput 读取整数参数：JSON 解码得到 float64，Go 调用方可能直接传整数。
func intInput(input map[string]interface{}, key string) (int, bool) {
	switch v := input[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
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

	// 本工具不执行命令，必须明确失败；需要执行命令请使用带白名单的 tools/shell。
	const message = "shell execution is not supported by this tool; use tools/shell"
	return &core.ToolResult{
		Success:   false,
		Data:      map[string]interface{}{"command": command},
		Error:     message,
		ErrorCode: "NOT_SUPPORTED",
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
	}, agentErrors.New(agentErrors.CodeNotImplemented, message).
		WithComponent("shell_execute_tool").
		WithOperation("execute")
}

// Validate 验证输入
func (t *ShellExecuteTool) Validate(input map[string]interface{}) error {
	command, ok := input["command"].(string)
	if !ok || command == "" {
		return &core.ErrInvalidInput{Field: "command", Message: "must be a non-empty string"}
	}

	return nil
}
