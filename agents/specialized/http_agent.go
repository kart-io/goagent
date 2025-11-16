package specialized

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/logger/core"
)

// HTTPAgent HTTP 调用 Agent
// 提供通用的 HTTP 请求能力
type HTTPAgent struct {
	*agentcore.BaseAgent
	client *http.Client
	logger core.Logger
}

// NewHTTPAgent 创建 HTTP Agent
func NewHTTPAgent(logger core.Logger) *HTTPAgent {
	return &HTTPAgent{
		BaseAgent: agentcore.NewBaseAgent(
			"http-agent",
			"General purpose HTTP client for making web requests",
			[]string{
				"http_get",
				"http_post",
				"http_put",
				"http_delete",
				"http_patch",
			},
		),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger.With("agent", "http"),
	}
}

// Execute 执行 HTTP 请求
func (a *HTTPAgent) Execute(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	start := time.Now()

	// 解析参数
	method, _ := input.Context["method"].(string)
	url, _ := input.Context["url"].(string)
	headers, _ := input.Context["headers"].(map[string]string)
	body := input.Context["body"]

	if method == "" {
		method = "GET"
	}

	if url == "" {
		return nil, fmt.Errorf("url is required")
	}

	// 应用超时
	if input.Options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, input.Options.Timeout)
		defer cancel()
	}

	a.logger.Info("Executing HTTP request",
		"method", method,
		"url", url)

	// 构建请求
	var reqBody io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		reqBody = bytes.NewBuffer(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if reqBody != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// 发送请求
	resp, err := a.client.Do(req)
	if err != nil {
		return &agentcore.AgentOutput{
			Status:    "failed",
			Message:   fmt.Sprintf("HTTP request failed: %v", err),
			Latency:   time.Since(start),
			Timestamp: start,
		}, fmt.Errorf("http request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			a.logger.Warnw("Failed to close response body", "error", closeErr)
		}
	}()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// 尝试解析 JSON
	var jsonBody interface{}
	if err := json.Unmarshal(respBody, &jsonBody); err != nil {
		// 不是 JSON，返回原始文本
		jsonBody = string(respBody)
	}

	// 构建输出
	output := &agentcore.AgentOutput{
		Status:  "success",
		Message: fmt.Sprintf("HTTP %s request completed with status %d", method, resp.StatusCode),
		Result: map[string]interface{}{
			"status_code": resp.StatusCode,
			"headers":     resp.Header,
			"body":        jsonBody,
		},
		ToolCalls: []agentcore.ToolCall{
			{
				ToolName: "http",
				Input: map[string]interface{}{
					"method": method,
					"url":    url,
					"body":   body,
				},
				Output: map[string]interface{}{
					"status_code": resp.StatusCode,
					"body":        jsonBody,
				},
				Duration: time.Since(start),
				Success:  resp.StatusCode >= 200 && resp.StatusCode < 300,
			},
		},
		Latency:   time.Since(start),
		Timestamp: start,
	}

	if resp.StatusCode >= 400 {
		output.Status = "failed"
		output.Message = fmt.Sprintf("HTTP request failed with status %d", resp.StatusCode)
	}

	return output, nil
}

// Get 执行 GET 请求
func (a *HTTPAgent) Get(ctx context.Context, url string, headers map[string]string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method":  "GET",
			"url":     url,
			"headers": headers,
		},
	})
}

// Post 执行 POST 请求
func (a *HTTPAgent) Post(ctx context.Context, url string, body interface{}, headers map[string]string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method":  "POST",
			"url":     url,
			"body":    body,
			"headers": headers,
		},
	})
}

// Put 执行 PUT 请求
func (a *HTTPAgent) Put(ctx context.Context, url string, body interface{}, headers map[string]string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method":  "PUT",
			"url":     url,
			"body":    body,
			"headers": headers,
		},
	})
}

// Delete 执行 DELETE 请求
func (a *HTTPAgent) Delete(ctx context.Context, url string, headers map[string]string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"method":  "DELETE",
			"url":     url,
			"headers": headers,
		},
	})
}
