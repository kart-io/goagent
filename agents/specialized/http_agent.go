package specialized

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/utils/json"
	"time"

	"github.com/go-resty/resty/v2"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/httpclient"
	"github.com/kart-io/logger/core"
)

// HTTPAgent HTTP 调用 Agent
// 提供通用的 HTTP 请求能力，使用 httpclient
type HTTPAgent struct {
	*agentcore.BaseAgent
	client *httpclient.Client
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
		client: httpclient.NewClient(&httpclient.Config{
			Timeout: 30 * time.Second,
		}),
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
		method = interfaces.MethodGet
	}

	if url == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "url is required").
			WithComponent("http_agent").
			WithOperation("Execute")
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
	var resp *resty.Response
	var err error

	switch method {
	case interfaces.MethodGet:
		resp, err = req.Get(url)
	case interfaces.MethodPost:
		resp, err = req.Post(url)
	case interfaces.MethodPut:
		resp, err = req.Put(url)
	case interfaces.MethodDelete:
		resp, err = req.Delete(url)
	case interfaces.MethodPatch:
		resp, err = req.Patch(url)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported HTTP method").
			WithComponent("http_agent").
			WithOperation("Execute").
			WithContext(interfaces.FieldMethod, method)
	}

	if err != nil {
		return &agentcore.AgentOutput{
				Status:    interfaces.StatusFailed,
				Message:   "HTTP request failed",
				Latency:   time.Since(start),
				Timestamp: start,
			}, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "http request failed").
				WithComponent("http_agent").
				WithOperation("Execute").
				WithContext(interfaces.FieldURL, url).
				WithContext(interfaces.FieldMethod, method)
	}

	// 尝试解析 JSON
	var jsonBody interface{}
	respBody := resp.Body()
	if err := json.Unmarshal(respBody, &jsonBody); err != nil {
		// 不是 JSON，返回原始文本
		jsonBody = string(respBody)
	}

	// 构建输出
	output := &agentcore.AgentOutput{
		Status:  interfaces.StatusSuccess,
		Message: fmt.Sprintf("HTTP %s request completed with status %d", method, resp.StatusCode()),
		Result: map[string]interface{}{
			interfaces.FieldStatusCode: resp.StatusCode(),
			interfaces.FieldHeaders:    resp.Header(),
			interfaces.FieldBody:       jsonBody,
		},
		ToolCalls: []agentcore.ToolCall{
			{
				ToolName: "http",
				Input: map[string]interface{}{
					interfaces.FieldMethod: method,
					interfaces.FieldURL:    url,
					interfaces.FieldBody:   body,
				},
				Output: map[string]interface{}{
					interfaces.FieldStatusCode: resp.StatusCode(),
					interfaces.FieldBody:       jsonBody,
				},
				Duration: time.Since(start),
				Success:  resp.IsSuccess(),
			},
		},
		Latency:   time.Since(start),
		Timestamp: start,
	}

	if resp.StatusCode() >= 400 {
		output.Status = interfaces.StatusFailed
		output.Message = fmt.Sprintf("HTTP request failed with status %d", resp.StatusCode())
	}

	return output, nil
}

// Get 执行 GET 请求
func (a *HTTPAgent) Get(ctx context.Context, url string, headers map[string]string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			interfaces.FieldMethod:  interfaces.MethodGet,
			interfaces.FieldURL:     url,
			interfaces.FieldHeaders: headers,
		},
	})
}

// Post 执行 POST 请求
func (a *HTTPAgent) Post(ctx context.Context, url string, body interface{}, headers map[string]string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			interfaces.FieldMethod:  interfaces.MethodPost,
			interfaces.FieldURL:     url,
			interfaces.FieldBody:    body,
			interfaces.FieldHeaders: headers,
		},
	})
}

// Put 执行 PUT 请求
func (a *HTTPAgent) Put(ctx context.Context, url string, body interface{}, headers map[string]string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			interfaces.FieldMethod:  interfaces.MethodPut,
			interfaces.FieldURL:     url,
			interfaces.FieldBody:    body,
			interfaces.FieldHeaders: headers,
		},
	})
}

// Delete 执行 DELETE 请求
func (a *HTTPAgent) Delete(ctx context.Context, url string, headers map[string]string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			interfaces.FieldMethod:  interfaces.MethodDelete,
			interfaces.FieldURL:     url,
			interfaces.FieldHeaders: headers,
		},
	})
}
