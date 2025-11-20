package http

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/utils/httpclient"
)

// APITool HTTP API 调用工具
//
// 提供通用的 HTTP 请求能力，使用统一的 HTTP 客户端管理器
type APITool struct {
	*tools.BaseTool
	client  *httpclient.Client
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

	// 创建 HTTP 客户端配置
	config := &httpclient.Config{
		Timeout: timeout,
		BaseURL: baseURL,
		Headers: headers,
	}

	// 创建 HTTP 客户端
	client := httpclient.NewClient(config)

	tool := &APITool{
		client:  client,
		baseURL: baseURL,
		headers: headers,
	}

	tool.BaseTool = tools.NewBaseTool(
		tools.ToolAPI,
		tools.DescAPI,
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
	method, _ := input.Args[interfaces.FieldMethod].(string)
	if method == "" {
		method = interfaces.MethodGet
	}

	urlStr, ok := input.Args[interfaces.FieldURL].(string)
	if !ok || urlStr == "" {
		return &interfaces.ToolOutput{
				Success: false,
				Error:   "url is required and must be a non-empty string",
			}, tools.NewToolError(a.Name(), "invalid input", agentErrors.New(agentErrors.CodeInvalidInput, "url is required").
				WithComponent("api_tool").
				WithOperation("run"))
	}

	// 解析请求头
	headers := make(map[string]string)
	if h, ok := input.Args[interfaces.FieldHeaders].(map[string]interface{}); ok {
		for k, v := range h {
			headers[k] = fmt.Sprint(v)
		}
	}

	// 解析请求体
	var body interface{}
	if b, ok := input.Args[interfaces.FieldBody]; ok {
		body = b
	}

	// 解析超时并应用到 context
	if timeoutSec, ok := input.Args[interfaces.FieldTimeout].(float64); ok && timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}

	// 创建请求
	req := a.client.R().
		SetContext(ctx).
		SetHeaders(headers)

	// 如果有请求体，设置为 JSON
	if body != nil {
		req.SetBody(body).
			SetHeader(interfaces.HeaderContentType, interfaces.ContentTypeJSON)
	}

	// 执行请求
	startTime := time.Now()
	var resp *resty.Response
	var err error

	switch method {
	case interfaces.MethodGet:
		resp, err = req.Get(urlStr)
	case interfaces.MethodPost:
		resp, err = req.Post(urlStr)
	case interfaces.MethodPut:
		resp, err = req.Put(urlStr)
	case interfaces.MethodDelete:
		resp, err = req.Delete(urlStr)
	case interfaces.MethodPatch:
		resp, err = req.Patch(urlStr)
	default:
		return &interfaces.ToolOutput{
			Success: false,
			Error:   fmt.Sprintf("unsupported HTTP method: %s", method),
		}, tools.NewToolError(a.Name(), "invalid method", agentErrors.New(agentErrors.CodeInvalidInput, "unsupported HTTP method").
			WithComponent("api_tool").
			WithOperation("run").
			WithContext(interfaces.FieldMethod, method))
	}

	duration := time.Since(startTime)

	if err != nil {
		return &interfaces.ToolOutput{
			Success: false,
			Error:   fmt.Sprintf("http request failed: %v", err),
			Metadata: map[string]interface{}{
				interfaces.FieldMethod:   method,
				interfaces.FieldURL:      urlStr,
				interfaces.FieldDuration: duration.String(),
			},
		}, tools.NewToolError(a.Name(), "request failed", err)
	}

	// 尝试解析 JSON
	var jsonBody interface{}
	respBody := resp.Body()
	if err := json.Unmarshal(respBody, &jsonBody); err != nil {
		// 不是 JSON，返回原始文本
		jsonBody = string(respBody)
	}

	// 构建结果
	result := map[string]interface{}{
		interfaces.FieldStatusCode: resp.StatusCode(),
		interfaces.FieldStatus:     resp.Status(),
		interfaces.FieldHeaders:    resp.Header(),
		interfaces.FieldBody:       jsonBody,
		interfaces.FieldDuration:   duration.String(),
	}

	success := resp.IsSuccess()

	if !success {
		return &interfaces.ToolOutput{
				Result:  result,
				Success: false,
				Error:   fmt.Sprintf("HTTP request failed with status %d", resp.StatusCode()),
				Metadata: map[string]interface{}{
					interfaces.FieldMethod: method,
					interfaces.FieldURL:    urlStr,
				},
			}, tools.NewToolError(a.Name(), "non-2xx status code", agentErrors.New(agentErrors.CodeToolExecution, "HTTP request failed with non-2xx status").
				WithComponent("api_tool").
				WithOperation("run").
				WithContext(interfaces.FieldStatusCode, resp.StatusCode()).
				WithContext(interfaces.FieldURL, urlStr))
	}

	return &interfaces.ToolOutput{
		Result:  result,
		Success: true,
		Metadata: map[string]interface{}{
			interfaces.FieldMethod: method,
			interfaces.FieldURL:    urlStr,
		},
	}, nil
}

// Get 执行 GET 请求的便捷方法
func (a *APITool) Get(ctx context.Context, url string, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			interfaces.FieldMethod:  interfaces.MethodGet,
			interfaces.FieldURL:     url,
			interfaces.FieldHeaders: headers,
		},
		Context: ctx,
	})
}

// Post 执行 POST 请求的便捷方法
func (a *APITool) Post(ctx context.Context, url string, body interface{}, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			interfaces.FieldMethod:  interfaces.MethodPost,
			interfaces.FieldURL:     url,
			interfaces.FieldBody:    body,
			interfaces.FieldHeaders: headers,
		},
		Context: ctx,
	})
}

// Put 执行 PUT 请求的便捷方法
func (a *APITool) Put(ctx context.Context, url string, body interface{}, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			interfaces.FieldMethod:  interfaces.MethodPut,
			interfaces.FieldURL:     url,
			interfaces.FieldBody:    body,
			interfaces.FieldHeaders: headers,
		},
		Context: ctx,
	})
}

// Delete 执行 DELETE 请求的便捷方法
func (a *APITool) Delete(ctx context.Context, url string, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			interfaces.FieldMethod:  interfaces.MethodDelete,
			interfaces.FieldURL:     url,
			interfaces.FieldHeaders: headers,
		},
		Context: ctx,
	})
}

// Patch 执行 PATCH 请求的便捷方法
func (a *APITool) Patch(ctx context.Context, url string, body interface{}, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			interfaces.FieldMethod:  interfaces.MethodPatch,
			interfaces.FieldURL:     url,
			interfaces.FieldBody:    body,
			interfaces.FieldHeaders: headers,
		},
		Context: ctx,
	})
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
	b.headers[interfaces.HeaderAuthorization] = interfaces.Bearer + " " + token
	return b
}

// Build 构建工具
func (b *APIToolBuilder) Build() *APITool {
	return NewAPITool(b.baseURL, b.timeout, b.headers)
}
