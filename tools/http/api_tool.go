package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// APITool HTTP API 调用工具
//
// 提供通用的 HTTP 请求能力
type APITool struct {
	*tools.BaseTool
	client  *http.Client
	baseURL string            // 基础 URL（可选）
	headers map[string]string // 默认请求头
}

// NewAPITool 创建 API 工具
//
// Parameters:
//   - baseURL: 基础 URL（可选，为空则每次请求需要提供完整 URL）
//   - timeout: 请求超时时间
//   - headers: 默认请求头
func NewAPITool(baseURL string, timeout time.Duration, headers map[string]string) *APITool {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	if headers == nil {
		headers = make(map[string]string)
	}

	tool := &APITool{
		client:  &http.Client{Timeout: timeout},
		baseURL: baseURL,
		headers: headers,
	}

	tool.BaseTool = tools.NewBaseTool(
		"api",
		"Makes HTTP API requests. Supports GET, POST, PUT, DELETE, and PATCH methods. "+
			"Can send JSON payloads and custom headers.",
		`{
			"type": "object",
			"properties": {
				"method": {
					"type": "string",
					"enum": ["GET", "POST", "PUT", "DELETE", "PATCH"],
					"description": "HTTP method (default: GET)"
				},
				"url": {
					"type": "string",
					"description": "Request URL (can be relative if base URL is configured)"
				},
				"headers": {
					"type": "object",
					"description": "Request headers (optional)"
				},
				"body": {
					"type": "object",
					"description": "Request body (for POST, PUT, PATCH)"
				},
				"timeout": {
					"type": "integer",
					"description": "Request timeout in seconds (optional)"
				}
			},
			"required": ["url"]
		}`,
		tool.run,
	)

	return tool
}

// run 执行 HTTP 请求
func (a *APITool) run(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	// 解析参数
	method, _ := input.Args["method"].(string)
	if method == "" {
		method = "GET"
	}

	urlStr, ok := input.Args["url"].(string)
	if !ok || urlStr == "" {
		return &interfaces.ToolOutput{
			Success: false,
			Error:   "url is required and must be a non-empty string",
		}, tools.NewToolError(a.Name(), "invalid input", fmt.Errorf("url is required"))
	}

	// 如果配置了基础 URL 且提供的是相对路径，则拼接
	if a.baseURL != "" && !isAbsoluteURL(urlStr) {
		urlStr = a.baseURL + urlStr
	}

	// 解析请求头
	headers := make(map[string]string)
	// 先复制默认请求头
	for k, v := range a.headers {
		headers[k] = v
	}
	// 再合并用户提供的请求头
	if h, ok := input.Args["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			headers[k] = fmt.Sprint(v)
		}
	}

	// 解析请求体
	var body interface{}
	if b, ok := input.Args["body"]; ok {
		body = b
	}

	// 解析超时
	if timeoutSec, ok := input.Args["timeout"].(float64); ok {
		timeout := time.Duration(timeoutSec) * time.Second
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	} else if a.client.Timeout > 0 {
		// 使用默认超时
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.client.Timeout)
		defer cancel()
	}

	// 构建请求
	var reqBody io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return &interfaces.ToolOutput{
				Success: false,
				Error:   fmt.Sprintf("failed to marshal body: %v", err),
			}, tools.NewToolError(a.Name(), "invalid body", err)
		}
		reqBody = bytes.NewBuffer(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, reqBody)
	if err != nil {
		return &interfaces.ToolOutput{
			Success: false,
			Error:   fmt.Sprintf("failed to create request: %v", err),
		}, tools.NewToolError(a.Name(), "request creation failed", err)
	}

	// 设置请求头
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if reqBody != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// 发送请求
	startTime := time.Now()
	resp, err := a.client.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		return &interfaces.ToolOutput{
			Success: false,
			Error:   fmt.Sprintf("http request failed: %v", err),
			Metadata: map[string]interface{}{
				"method":   method,
				"url":      urlStr,
				"duration": duration.String(),
			},
		}, tools.NewToolError(a.Name(), "request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &interfaces.ToolOutput{
			Success: false,
			Error:   fmt.Sprintf("failed to read response: %v", err),
		}, tools.NewToolError(a.Name(), "response read failed", err)
	}

	// 尝试解析 JSON
	var jsonBody interface{}
	if err := json.Unmarshal(respBody, &jsonBody); err != nil {
		// 不是 JSON，返回原始文本
		jsonBody = string(respBody)
	}

	// 构建结果
	result := map[string]interface{}{
		"status_code": resp.StatusCode,
		"status":      resp.Status,
		"headers":     resp.Header,
		"body":        jsonBody,
		"duration":    duration.String(),
	}

	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	if !success {
		return &interfaces.ToolOutput{
			Result:  result,
			Success: false,
			Error:   fmt.Sprintf("HTTP request failed with status %d", resp.StatusCode),
			Metadata: map[string]interface{}{
				"method": method,
				"url":    urlStr,
			},
		}, tools.NewToolError(a.Name(), "non-2xx status code", fmt.Errorf("status: %d", resp.StatusCode))
	}

	return &interfaces.ToolOutput{
		Result:  result,
		Success: true,
		Metadata: map[string]interface{}{
			"method": method,
			"url":    urlStr,
		},
	}, nil
}

// Get 执行 GET 请求的便捷方法
func (a *APITool) Get(ctx context.Context, url string, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method":  "GET",
			"url":     url,
			"headers": headers,
		},
		Context: ctx,
	})
}

// Post 执行 POST 请求的便捷方法
func (a *APITool) Post(ctx context.Context, url string, body interface{}, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method":  "POST",
			"url":     url,
			"body":    body,
			"headers": headers,
		},
		Context: ctx,
	})
}

// Put 执行 PUT 请求的便捷方法
func (a *APITool) Put(ctx context.Context, url string, body interface{}, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method":  "PUT",
			"url":     url,
			"body":    body,
			"headers": headers,
		},
		Context: ctx,
	})
}

// Delete 执行 DELETE 请求的便捷方法
func (a *APITool) Delete(ctx context.Context, url string, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method":  "DELETE",
			"url":     url,
			"headers": headers,
		},
		Context: ctx,
	})
}

// Patch 执行 PATCH 请求的便捷方法
func (a *APITool) Patch(ctx context.Context, url string, body interface{}, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"method":  "PATCH",
			"url":     url,
			"body":    body,
			"headers": headers,
		},
		Context: ctx,
	})
}

// isAbsoluteURL 检查是否为绝对 URL
func isAbsoluteURL(urlStr string) bool {
	if len(urlStr) < 7 {
		return false
	}
	if len(urlStr) >= 8 && urlStr[0:8] == "https://" {
		return true
	}
	if len(urlStr) >= 7 && urlStr[0:7] == "http://" {
		return true
	}
	return false
}

// APIToolBuilder API 工具构建器
type APIToolBuilder struct {
	baseURL string
	timeout time.Duration
	headers map[string]string
}

// NewAPIToolBuilder 创建 API 工具构建器
func NewAPIToolBuilder() *APIToolBuilder {
	return &APIToolBuilder{
		headers: make(map[string]string),
		timeout: 30 * time.Second,
	}
}

// WithBaseURL 设置基础 URL
func (b *APIToolBuilder) WithBaseURL(baseURL string) *APIToolBuilder {
	b.baseURL = baseURL
	return b
}

// WithTimeout 设置超时
func (b *APIToolBuilder) WithTimeout(timeout time.Duration) *APIToolBuilder {
	b.timeout = timeout
	return b
}

// WithHeader 添加默认请求头
func (b *APIToolBuilder) WithHeader(key, value string) *APIToolBuilder {
	b.headers[key] = value
	return b
}

// WithHeaders 批量添加默认请求头
func (b *APIToolBuilder) WithHeaders(headers map[string]string) *APIToolBuilder {
	for k, v := range headers {
		b.headers[k] = v
	}
	return b
}

// WithAuth 设置认证头
func (b *APIToolBuilder) WithAuth(token string) *APIToolBuilder {
	b.headers["Authorization"] = "Bearer " + token
	return b
}

// Build 构建工具
func (b *APIToolBuilder) Build() *APITool {
	return NewAPITool(b.baseURL, b.timeout, b.headers)
}
