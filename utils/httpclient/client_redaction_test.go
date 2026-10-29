package httpclient

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRestyRetryLogRedactsURL(t *testing.T) {
	c := NewClient(&Config{Timeout: 2 * time.Second, RetryCount: 1, RetryWaitTime: time.Millisecond, RetryMaxWaitTime: time.Millisecond})
	var buf bytes.Buffer
	c.resty.SetLogger(newRestyLogger(&buf))

	_, err := c.R().Get("http://user:pw@127.0.0.1:1/v1?key=APIKEYSECRET")
	if err == nil {
		t.Fatal("期望连接失败")
	}
	out := buf.String()
	if !strings.Contains(out, "Attempt") {
		t.Fatalf("期望出现重试日志，实际：%q", out)
	}
	for _, secret := range []string{"APIKEYSECRET", "pw@"} {
		if strings.Contains(out, secret) {
			t.Fatalf("重试日志泄漏 %q：%s", secret, out)
		}
	}
}

func TestRestyDebugLogRedactsRequestURI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	c := NewClient(&Config{Timeout: 2 * time.Second, Debug: true})
	var buf bytes.Buffer
	c.resty.SetLogger(newRestyLogger(&buf))

	if _, err := c.R().Get(srv.URL + "/v1/models?key=APIKEYSECRET&alt=sse"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "/v1/models") {
		t.Fatalf("期望出现调试日志，实际：%q", out)
	}
	if strings.Contains(out, "APIKEYSECRET") {
		t.Fatalf("调试日志泄漏 query：%s", out)
	}
}
