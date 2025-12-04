package practical

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

// TestAPICallerTool_Execute_BasicAuth 测试基本认证
func TestAPICallerTool_Execute_BasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"authenticated": true}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
			"auth": map[string]interface{}{
				"type": "basic",
				"credentials": map[string]interface{}{
					"username": "user",
					"password": "pass",
				},
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"].(int) != 200 {
		t.Errorf("期望状态码 200，得到 %v", result["status_code"])
	}
}

// TestAPICallerTool_Execute_BearerAuth 测试 Bearer 认证
func TestAPICallerTool_Execute_BearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"token": "valid"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
			"auth": map[string]interface{}{
				"type": "bearer",
				"credentials": map[string]interface{}{
					"token": "secret-token",
				},
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"].(int) != 200 {
		t.Errorf("期望状态码 200，得到 %v", result["status_code"])
	}
}

// TestAPICallerTool_Execute_APIKeyAuth_Header 测试 API Key 认证（Header）
func TestAPICallerTool_Execute_APIKeyAuth_Header(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "my-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"key_valid": true}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
			"auth": map[string]interface{}{
				"type": "api_key",
				"credentials": map[string]interface{}{
					"key":      "my-api-key",
					"name":     "X-API-Key",
					"location": "header",
				},
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"].(int) != 200 {
		t.Errorf("期望状态码 200，得到 %v", result["status_code"])
	}
}

// TestAPICallerTool_Execute_APIKeyAuth_Query 测试 API Key 认证（Query）
func TestAPICallerTool_Execute_APIKeyAuth_Query(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.URL.Query().Get("key")
		if apiKey != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"key_valid": true}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
			"auth": map[string]interface{}{
				"type": "api_key",
				"credentials": map[string]interface{}{
					"key":      "secret",
					"name":     "key",
					"location": "query",
				},
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"].(int) != 200 {
		t.Errorf("期望状态码 200，得到 %v", result["status_code"])
	}
}

// TestAPICallerTool_Execute_OAuth2Auth 测试 OAuth2 认证
func TestAPICallerTool_Execute_OAuth2Auth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer oauth-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"oauth": "valid"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
			"auth": map[string]interface{}{
				"type": "oauth2",
				"credentials": map[string]interface{}{
					"access_token": "oauth-token",
				},
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"].(int) != 200 {
		t.Errorf("期望状态码 200，得到 %v", result["status_code"])
	}
}

// TestAPICallerTool_Execute_ResponseCaching 测试响应缓存
func TestAPICallerTool_Execute_ResponseCaching(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`{"call": %d}`, callCount)))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
			"cache":  true,
		},
	}

	// 第一次调用
	output1, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("第一次执行失败：%v", err)
		return
	}

	// 第二次调用应该从缓存返回
	output2, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("第二次执行失败：%v", err)
		return
	}

	// 检查缓存标志
	result2 := output2.Result.(map[string]interface{})
	cached, ok := result2["cached"].(bool)
	if !ok || !cached {
		t.Error("第二次请求应该从缓存返回")
	}

	// 验证调用次数
	if callCount != 1 {
		t.Errorf("期望服务器调用 1 次，实际 %d 次", callCount)
	}

	// 检查 URL 相同（来自缓存）
	url1 := output1.Result.(map[string]interface{})["url"]
	url2 := output2.Result.(map[string]interface{})["url"]
	if url1 != url2 {
		t.Error("缓存的请求 URL 应该相同")
	}
}

// TestAPICallerTool_Execute_CustomHeaders 测试自定义 headers
func TestAPICallerTool_Execute_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customHeader := r.Header.Get("X-Custom-Header")
		if customHeader != "custom-value" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"header": "received"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
			"headers": map[string]interface{}{
				"X-Custom-Header": "custom-value",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"].(int) != 200 {
		t.Errorf("期望状态码 200，得到 %v", result["status_code"])
	}
}

// TestAPICallerTool_Execute_QueryParameters 测试查询参数
func TestAPICallerTool_Execute_QueryParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("name") != "John" || q.Get("age") != "30" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"params": "received"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
			"params": map[string]interface{}{
				"name": "John",
				"age":  30,
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"].(int) != 200 {
		t.Errorf("期望状态码 200，得到 %v", result["status_code"])
	}
}

// TestAPICallerTool_Execute_HTTPErrors 测试 HTTP 错误处理
func TestAPICallerTool_Execute_HTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "not found"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
		},
	}

	output, err := tool.Execute(ctx, input)
	if err == nil {
		t.Error("HTTP 404 应该返回错误")
		return
	}

	// 但输出仍应包含响应信息
	if output == nil || output.Result == nil {
		t.Error("即使出错也应该返回输出")
	}
}

// TestAPICallerTool_Execute_InvalidURL 测试无效 URL
func TestAPICallerTool_Execute_InvalidURL(t *testing.T) {
	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    "not a valid url",
			"method": "GET",
		},
	}

	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Error("无效 URL 应该返回错误")
	}
}

// TestAPICallerTool_Execute_UnsupportedAuthType 测试不支持的认证类型
func TestAPICallerTool_Execute_UnsupportedAuthType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
			"auth": map[string]interface{}{
				"type": "unsupported_type",
				"credentials": map[string]interface{}{
					"token": "token",
				},
			},
		},
	}

	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Error("不支持的认证类型应该返回错误")
	}
}

// TestAPICallerTool_Execute_InvalidAuthCredentials 测试无效认证凭证
func TestAPICallerTool_Execute_InvalidAuthCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "Bearer 缺少 token",
			args: map[string]interface{}{
				"url":    server.URL,
				"method": "GET",
				"auth": map[string]interface{}{
					"type":        "bearer",
					"credentials": map[string]interface{}{},
				},
			},
		},
		{
			name: "API Key 缺少 key",
			args: map[string]interface{}{
				"url":    server.URL,
				"method": "GET",
				"auth": map[string]interface{}{
					"type": "api_key",
					"credentials": map[string]interface{}{
						"location": "header",
					},
				},
			},
		},
		{
			name: "OAuth2 缺少 access_token",
			args: map[string]interface{}{
				"url":    server.URL,
				"method": "GET",
				"auth": map[string]interface{}{
					"type":        "oauth2",
					"credentials": map[string]interface{}{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			_, err := tool.Execute(ctx, input)
			if err == nil {
				t.Error("应该返回错误")
			}
		})
	}
}

// TestAPICallerTool_Execute_VariousHTTPMethods 测试各种 HTTP 方法
func TestAPICallerTool_Execute_VariousHTTPMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "GET":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"method": "GET"}`))
		case "POST":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"method": "POST"}`))
		case "PUT":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"method": "PUT"}`))
		case "DELETE":
			w.WriteHeader(http.StatusNoContent)
		case "PATCH":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"method": "PATCH"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	methods := []struct {
		name       string
		method     string
		statusCode int
	}{
		{"GET", "GET", 200},
		{"POST", "POST", 201},
		{"PUT", "PUT", 200},
		{"DELETE", "DELETE", 204},
		{"PATCH", "PATCH", 200},
	}

	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			input := &interfaces.ToolInput{
				Args: map[string]interface{}{
					"url":    server.URL,
					"method": m.method,
				},
			}

			output, err := tool.Execute(ctx, input)
			if err != nil && m.statusCode < 400 {
				t.Errorf("执行失败：%v", err)
				return
			}

			result := output.Result.(map[string]interface{})
			statusCode := result["status_code"]
			// 处理可能是 int 或 float64 的情况
			var code int
			switch v := statusCode.(type) {
			case int:
				code = v
			case float64:
				code = int(v)
			default:
				t.Errorf("无法解析状态码：%v", statusCode)
				return
			}

			if code != m.statusCode {
				t.Errorf("期望状态码 %d，得到 %d", m.statusCode, code)
			}
		})
	}
}

// TestAPICallerTool_Execute_SimpleStringURL 测试简单字符串 URL 输入
func TestAPICallerTool_Execute_SimpleStringURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	// 简单字符串 URL
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"].(int) != 200 {
		t.Errorf("期望状态码 200，得到 %v", result["status_code"])
	}
}

// TestAPICallerTool_RateLimiter_TokenBucket 测试令牌桶速率限制
func TestAPICallerTool_RateLimiter_TokenBucket(t *testing.T) {
	limiter := NewRateLimiter(3, 100*time.Millisecond)

	// 应该允许前 3 个请求
	for i := 0; i < 3; i++ {
		if !limiter.Allow() {
			t.Errorf("请求 %d 应该被允许", i+1)
		}
	}

	// 第 4 个请求应该被拒绝
	if limiter.Allow() {
		t.Error("第 4 个请求应该被拒绝")
	}

	// 等待令牌重新填充
	time.Sleep(150 * time.Millisecond)

	// 现在应该允许请求
	if !limiter.Allow() {
		t.Error("令牌重新填充后应该允许请求")
	}
}

// TestResponseCache_Eviction 测试缓存驱逐
func TestResponseCache_Eviction(t *testing.T) {
	cache := NewResponseCache(2, 1*time.Minute)

	// 填充缓存到容量
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")

	if len(cache.entries) != 2 {
		t.Errorf("期望 2 个条目，得到 %d", len(cache.entries))
	}

	// 添加第三个条目应该驱逐最旧的
	cache.Set("key3", "value3")

	if len(cache.entries) != 2 {
		t.Errorf("驱逐后期望 2 个条目，得到 %d", len(cache.entries))
	}

	// key1 应该被驱逐
	if cache.Get("key1") != nil {
		t.Error("key1 应该被驱逐")
	}

	// key2 和 key3 应该存在
	if cache.Get("key2") == nil {
		t.Error("key2 应该存在")
	}
	if cache.Get("key3") == nil {
		t.Error("key3 应该存在")
	}
}

// TestAPICallerTool_Execute_Invoke 测试 Invoke 方法
func TestAPICallerTool_Execute_Invoke(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"invoke": "test"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"].(int) != 200 {
		t.Errorf("期望状态码 200，得到 %v", result["status_code"])
	}
}

// TestAPICallerTool_Execute_Stream 测试 Stream 方法
func TestAPICallerTool_Execute_Stream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"stream": "test"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
		},
	}

	ch, err := tool.Stream(ctx, input)
	if err != nil {
		t.Errorf("创建流失败：%v", err)
		return
	}

	// 从通道读取结果
	for chunk := range ch {
		if chunk.Error != nil {
			t.Errorf("流错误：%v", chunk.Error)
		}
		if chunk.Data != nil {
			result := chunk.Data.Result.(map[string]interface{})
			if result["status_code"].(int) != 200 {
				t.Errorf("期望状态码 200，得到 %v", result["status_code"])
			}
		}
	}
}

// TestAPICallerTool_Execute_Batch 测试 Batch 方法
func TestAPICallerTool_Execute_Batch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"batch": "test"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	inputs := []*interfaces.ToolInput{
		{
			Args: map[string]interface{}{
				"url":    server.URL,
				"method": "GET",
			},
		},
		{
			Args: map[string]interface{}{
				"url":    server.URL,
				"method": "POST",
			},
		},
	}

	outputs, err := tool.Batch(ctx, inputs)
	if err != nil {
		t.Errorf("批处理失败：%v", err)
		return
	}

	if len(outputs) != 2 {
		t.Errorf("期望 2 个输出，得到 %d", len(outputs))
	}

	for _, output := range outputs {
		result := output.Result.(map[string]interface{})
		if result["status_code"].(int) != 200 {
			t.Errorf("期望状态码 200，得到 %v", result["status_code"])
		}
	}
}
