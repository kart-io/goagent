package httpclient

import (
	"bytes"
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
