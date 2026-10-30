package tools

import (
	"context"
	"strings"
	"testing"
)

// 传输错误文本含完整请求 URL，query 中的凭据不得进入返回错误与 ToolResult.Error。
func TestHTTPRequestTool_TransportErrorRedactsURL(t *testing.T) {
	res, err := NewHTTPRequestTool().Execute(context.Background(), map[string]interface{}{
		"url":     "http://127.0.0.1:1/v1?token=SECRET",
		"timeout": float64(1),
	})
	if err == nil {
		t.Fatal("期望连接失败")
	}
	for _, s := range []string{err.Error(), res.Error} {
		if strings.Contains(s, "SECRET") || !strings.Contains(s, "http://127.0.0.1:1/v1") {
			t.Errorf("未脱敏或缺少 URL: %s", s)
		}
	}
}
