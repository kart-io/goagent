package http

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/go-resty/resty/v2"

	shared "github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/toolargs"
	"github.com/kart-io/goagent/utils/httpclient"
)

// maxResponseBodyBytes 是响应体上限，超出时请求失败。
const maxResponseBodyBytes = 10 << 20

// maxRedirects 与 net/http 默认的重定向次数上限一致。
const maxRedirects = 10

// APITool HTTP API 调用工具
//
// 提供通用的 HTTP 请求能力，使用统一的 HTTP 客户端管理器。
// 配置 baseURL 时只允许访问该 scheme 与主机（含重定向），默认请求头里的凭据不会发往其他主机；
// 未配置 baseURL 时默认拒绝连接非公网地址（回环、私网、链路本地与云元数据地址），按实际拨号的 IP 判断，
// 且不经环境变量中的 HTTP 代理直连；需要访问内网时用 APIToolBuilder.WithPrivateNetwork。
type APITool struct {
	*tools.BaseTool
	client  *httpclient.Client
	baseURL string            // 基础 URL（可选）
	base    *url.URL          // 解析后的基础 URL，nil 表示不限主机
	headers map[string]string // 默认请求头
}

// NewAPITool 创建 API 工具
//
// Parameters:
//   - baseURL: 基础 URL（可选，为空则每次请求需要提供完整 URL）
//   - timeout: 请求超时时间
//   - headers: 默认请求头
func NewAPITool(baseURL string, timeout time.Duration, headers map[string]string) *APITool {
	return newAPITool(baseURL, timeout, headers, false)
}

func newAPITool(baseURL string, timeout time.Duration, headers map[string]string, privateNetwork bool) *APITool {
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
	client.Resty().SetResponseBodyLimit(maxResponseBodyBytes)

	tool := &APITool{
		client:  client,
		baseURL: baseURL,
		headers: headers,
	}
	if base, err := url.Parse(baseURL); baseURL != "" && err == nil {
		tool.base = base
		client.Resty().SetRedirectPolicy(resty.RedirectPolicyFunc(func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return agentErrors.Newf(agentErrors.CodeToolValidation, "stopped after %d redirects", maxRedirects)
			}
			if !sameOrigin(req.URL, base) {
				return agentErrors.Newf(agentErrors.CodeToolValidation, "redirect to a host other than base URL is not allowed")
			}
			return nil
		}))
	} else if !privateNetwork {
		if transport, ok := client.Resty().GetClient().Transport.(*http.Transport); ok {
			transport = transport.Clone()
			// 代理会让拨号目标变成代理地址，绕开检查，因此直连。
			transport.Proxy = nil
			dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: rejectNonPublic}
			transport.DialContext = dialer.DialContext
			client.Resty().SetTransport(transport)
		}
	}

	tool.BaseTool = tools.NewBaseTool(
		shared.ToolAPI,
		shared.DescAPI,
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
	resp := tools.NewToolErrorResponse(a.Name()).WithOperation("run")

	// 解析参数
	method, _ := input.Args[shared.FieldMethod].(string)
	if method == "" {
		method = shared.MethodGet
	}

	urlStr, ok := input.Args[shared.FieldURL].(string)
	if !ok || urlStr == "" {
		return resp.ValidationError("url is required and must be a non-empty string", "field", "url")
	}

	// 配置了 baseURL 时，绝对 URL 必须与之同源
	if a.base != nil {
		u, err := url.Parse(urlStr)
		if err != nil {
			return resp.ValidationError("invalid url", "field", "url")
		}
		if (u.Scheme != "" || u.Host != "") && !sameOrigin(u, a.base) {
			return resp.ValidationError("url must stay on the configured base URL host", "field", "url")
		}
	}

	// 解析请求头
	headers := make(map[string]string)
	if h, ok := input.Args[shared.FieldHeaders].(map[string]interface{}); ok {
		for k, v := range h {
			headers[k] = fmt.Sprint(v)
		}
	}

	// 解析请求体
	var body interface{}
	if b, ok := input.Args[shared.FieldBody]; ok {
		body = b
	}

	// 解析超时并应用到 context
	if timeoutSec, ok := toolargs.Float64(input.Args[shared.FieldTimeout]); ok && timeoutSec > 0 {
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
			SetHeader(shared.HeaderContentType, shared.ContentTypeJSON)
	}

	// 执行请求
	startTime := time.Now()
	var httpResp *resty.Response
	var err error

	switch method {
	case shared.MethodGet:
		httpResp, err = req.Get(urlStr)
	case shared.MethodPost:
		httpResp, err = req.Post(urlStr)
	case shared.MethodPut:
		httpResp, err = req.Put(urlStr)
	case shared.MethodDelete:
		httpResp, err = req.Delete(urlStr)
	case shared.MethodPatch:
		httpResp, err = req.Patch(urlStr)
	default:
		return resp.ValidationError(fmt.Sprintf("unsupported HTTP method: %s", method), shared.FieldMethod, method)
	}

	duration := time.Since(startTime)

	if err != nil {
		// 网络错误，完全失败；错误文本含完整请求 URL，先脱敏
		err = agentErrors.RedactURLError(err)
		return resp.ExecutionError(fmt.Sprintf("http request failed: %v", err), nil, err)
	}

	// 尝试解析 JSON
	var jsonBody interface{}
	respBody := httpResp.Body()
	if err := json.Unmarshal(respBody, &jsonBody); err != nil {
		// 不是 JSON，返回原始文本
		jsonBody = string(respBody)
	}

	// 构建结果
	result := map[string]interface{}{
		shared.FieldStatusCode: httpResp.StatusCode(),
		shared.FieldStatus:     httpResp.Status(),
		shared.FieldHeaders:    httpResp.Header(),
		shared.FieldBody:       jsonBody,
		shared.FieldDuration:   duration.String(),
	}

	success := httpResp.IsSuccess()

	if !success {
		// HTTP 请求失败但有响应，返回部分结果
		return resp.ExecutionError(fmt.Sprintf("HTTP request failed with status %d", httpResp.StatusCode()), result,
			agentErrors.New(agentErrors.CodeToolExecution, "HTTP request failed with non-2xx status").
				WithContext(shared.FieldStatusCode, httpResp.StatusCode()).
				WithContext(shared.FieldURL, agentErrors.RedactURL(urlStr)))
	}

	return resp.Success(result, map[string]interface{}{
		shared.FieldMethod: method,
		shared.FieldURL:    urlStr,
	})
}

// cgnat 是运营商级 NAT 地址段，部分云厂商的元数据服务位于其中（如 100.100.100.200）。
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func publicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() && !addr.IsPrivate() && !cgnat.Contains(addr)
}

// rejectNonPublic 在建立连接前检查实际拨号的 IP，域名解析到内网同样拒绝。
func rejectNonPublic(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if addr, err := netip.ParseAddr(host); err != nil || !publicAddress(addr) {
		return agentErrors.Newf(agentErrors.CodeToolValidation, "connection to non-public address %s is not allowed", host)
	}
	return nil
}

func sameOrigin(u, base *url.URL) bool {
	return u.Scheme == base.Scheme && strings.EqualFold(u.Host, base.Host)
}

// Get 执行 GET 请求的便捷方法
func (a *APITool) Get(ctx context.Context, url string, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			shared.FieldMethod:  shared.MethodGet,
			shared.FieldURL:     url,
			shared.FieldHeaders: headers,
		},
		Context: ctx,
	})
}

// Post 执行 POST 请求的便捷方法
func (a *APITool) Post(ctx context.Context, url string, body interface{}, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			shared.FieldMethod:  shared.MethodPost,
			shared.FieldURL:     url,
			shared.FieldBody:    body,
			shared.FieldHeaders: headers,
		},
		Context: ctx,
	})
}

// Put 执行 PUT 请求的便捷方法
func (a *APITool) Put(ctx context.Context, url string, body interface{}, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			shared.FieldMethod:  shared.MethodPut,
			shared.FieldURL:     url,
			shared.FieldBody:    body,
			shared.FieldHeaders: headers,
		},
		Context: ctx,
	})
}

// Delete 执行 DELETE 请求的便捷方法
func (a *APITool) Delete(ctx context.Context, url string, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			shared.FieldMethod:  shared.MethodDelete,
			shared.FieldURL:     url,
			shared.FieldHeaders: headers,
		},
		Context: ctx,
	})
}

// Patch 执行 PATCH 请求的便捷方法
func (a *APITool) Patch(ctx context.Context, url string, body interface{}, headers map[string]string) (*interfaces.ToolOutput, error) {
	return a.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			shared.FieldMethod:  shared.MethodPatch,
			shared.FieldURL:     url,
			shared.FieldBody:    body,
			shared.FieldHeaders: headers,
		},
		Context: ctx,
	})
}

// APIToolBuilder API 工具构建器
type APIToolBuilder struct {
	baseURL        string
	timeout        time.Duration
	headers        map[string]string
	privateNetwork bool
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
	b.headers[shared.HeaderAuthorization] = shared.Bearer + " " + token
	return b
}

// WithPrivateNetwork 允许未配置 baseURL 的工具连接内网、回环与云元数据地址，并沿用环境变量中的 HTTP 代理。
// 模型可借此访问宿主网络内的任意服务，仅在模型输入可信或有其他出口限制时使用。
func (b *APIToolBuilder) WithPrivateNetwork() *APIToolBuilder {
	b.privateNetwork = true
	return b
}

// Build 构建工具
func (b *APIToolBuilder) Build() *APITool {
	return newAPITool(b.baseURL, b.timeout, b.headers, b.privateNetwork)
}
