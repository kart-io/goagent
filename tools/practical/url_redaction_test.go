package practical

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/utils/httpclient"
)

// 传输错误（*url.Error）的文本含完整请求 URL，query 中注入的 api key 不得进入返回错误与 ToolOutput。
func TestAPICallerTool_TransportErrorRedactsQueryKey(t *testing.T) {
	out, err := NewAPICallerTool().Execute(context.Background(), &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": "http://127.0.0.1:1/v1/data",
			"auth": map[string]interface{}{
				"type":        "api_key",
				"credentials": map[string]interface{}{"key": "APIKEYSECRET", "location": "query", "name": "key"},
			},
			"retry": map[string]interface{}{"max_attempts": 1},
		},
	})
	if err == nil {
		t.Fatal("期望连接失败")
	}
	for _, s := range []string{err.Error(), out.Error, out.Result.(map[string]interface{})["error"].(string)} {
		if strings.Contains(s, "APIKEYSECRET") {
			t.Errorf("凭据泄漏: %s", s)
		}
		if !strings.Contains(s, "http://127.0.0.1:1/v1/data") {
			t.Errorf("应保留脱敏后的 URL: %s", s)
		}
	}
	var ue *url.Error
	if !errors.As(err, &ue) {
		t.Error("应仍可取到 *url.Error")
	}
}

// ExecuteWithRuntime 从 runtime state 读取的 api key 必须真正用于请求。
func TestAPICallerRuntimeTool_InjectsAPIKeyFromState(t *testing.T) {
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	st := state.NewAgentState()
	st.Set("api_key_"+server.URL, "FROM_STATE")
	rt := tools.NewToolRuntime(context.Background(), st, memory.New())

	input := &interfaces.ToolInput{Args: map[string]interface{}{
		"url":  server.URL,
		"auth": map[string]interface{}{"type": "api_key"},
	}}
	if _, err := NewAPICallerRuntimeTool().ExecuteWithRuntime(context.Background(), input, rt); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if gotKey != "FROM_STATE" {
		t.Errorf("服务端收到的 key = %q, want FROM_STATE", gotKey)
	}
}

func TestWebScraperTool_TransportErrorRedactsURL(t *testing.T) {
	tool := NewWebScraperTool()
	tool.client = httpclient.NewClient(&httpclient.Config{Timeout: time.Second})
	out, err := tool.Execute(context.Background(), &interfaces.ToolInput{
		Args: map[string]interface{}{"url": "http://127.0.0.1:1/page?token=SECRET"},
	})
	if err == nil {
		t.Fatal("期望连接失败")
	}
	for _, s := range []string{err.Error(), out.Error, out.Result.(map[string]interface{})["error"].(string)} {
		if strings.Contains(s, "SECRET") || !strings.Contains(s, "http://127.0.0.1:1/page") {
			t.Errorf("未脱敏或缺少 URL: %s", s)
		}
	}
}
