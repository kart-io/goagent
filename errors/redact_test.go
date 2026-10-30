package errors

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestRedactURL_Bypass(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"NATS 多地址", "nats://u:p1@h1:4222,nats://u:p2@h2:4222", "nats://h1:4222,nats://h2:4222"},
		{"多地址无凭据", "nats://h1:4222,nats://h2:4222", "nats://h1:4222,nats://h2:4222"},
		{"query 中逗号后的片段不保留", "https://h/p?token=abc,def", "https://h/p"},
		{"query 后接第二个地址", "http://h1/a?k=s1,http://u:p@h2/b", "http://h1/a,http://h2/b"},
		{"密码含未转义斜杠", "http://user:pa/ss@host/path", "http://host/path"},
		{"密码形似端口加路径", "http://user:1234/5@host/", "http://host/"},
		{"密码含未转义问号", "http://user:pa?ss@host", "http://host"},
		{"密码含未转义井号", "http://user:pa#ss@host/x", "http://host/x"},
		{"密码含 @", "http://user:p@ss@host/x", "http://host/x"},
		{"路径中的 @ 不当作 userinfo", "https://host/users/a@b.com", "https://host/users/a@b.com"},
		{"空白之后的 @ 不处理", "http://host/p and a:b@c", "http://host/p%20and%20a:b@c"},
		{"key=value DSN", "host=db port=5432 user=admin password=secret dbname=app sslmode=disable", "host=db port=5432 dbname=app"},
		{"key=value DSN 引号值", "host=db password='a b' dbname=app", "host=db dbname=app"},
		{"key=value DSN 仅密码", "password=secret", ""},
		{"MySQL DSN 不当作 key=value", "user:pw@tcp(db:3306)/app?parseTime=true", "tcp(db:3306)/app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactURL(tt.in); got != tt.want {
				t.Errorf("RedactURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func newURLErr() (*url.Error, *net.OpError) {
	op := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	return &url.Error{Op: "Get", URL: "http://u:pw@127.0.0.1:1/v1/data?key=APIKEYSECRET", Err: op}, op
}

func TestRedactURLError(t *testing.T) {
	t.Run("nil 与无 url.Error 原样返回", func(t *testing.T) {
		if RedactURLError(nil) != nil {
			t.Fatal("nil 应原样返回")
		}
		plain := errors.New("x")
		if RedactURLError(plain) != plain {
			t.Fatal("无 *url.Error 应原样返回")
		}
		clean := &url.Error{Op: "Get", URL: "http://h/p", Err: plain}
		if RedactURLError(clean) != error(clean) {
			t.Fatal("URL 无需脱敏时应原样返回")
		}
	})

	t.Run("顶层 url.Error", func(t *testing.T) {
		orig, op := newURLErr()
		got := RedactURLError(orig)
		if s := got.Error(); strings.Contains(s, "APIKEYSECRET") || strings.Contains(s, "pw") {
			t.Fatalf("未脱敏: %s", s)
		}
		if want := `Get "http://127.0.0.1:1/v1/data": ` + op.Error(); got.Error() != want {
			t.Errorf("Error() = %q, want %q", got.Error(), want)
		}
		if !strings.Contains(orig.URL, "APIKEYSECRET") {
			t.Error("原错误被修改")
		}
		var gotOp *net.OpError
		if !errors.As(got, &gotOp) || gotOp != op {
			t.Error("Unwrap 链应保留原底层 *net.OpError")
		}
		var ue *url.Error
		if !errors.As(got, &ue) || strings.Contains(ue.URL, "APIKEYSECRET") {
			t.Error("errors.As 取到的 *url.Error 应已脱敏")
		}
	})

	t.Run("deadline 与 Timeout 保持", func(t *testing.T) {
		orig := &url.Error{Op: "Get", URL: "http://h/?token=T", Err: context.DeadlineExceeded}
		got := RedactURLError(orig)
		if !errors.Is(got, context.DeadlineExceeded) {
			t.Error("errors.Is(DeadlineExceeded) 应保持")
		}
		if te, ok := got.(interface{ Timeout() bool }); !ok || !te.Timeout() {
			t.Error("Timeout() 应保持")
		}
	})

	t.Run("链中间的 url.Error", func(t *testing.T) {
		orig, op := newURLErr()
		sentinel := errors.New("sentinel")
		wrapped := fmt.Errorf("外层: %w", fmt.Errorf("%w: %w", sentinel, orig))
		got := RedactURLError(wrapped)
		if s := got.Error(); strings.Contains(s, "APIKEYSECRET") || !strings.HasPrefix(s, "外层: sentinel: Get ") {
			t.Fatalf("Error() = %s", s)
		}
		if !errors.Is(got, sentinel) {
			t.Error("errors.Is 应按原错误链判断")
		}
		var gotOp *net.OpError
		if !errors.As(got, &gotOp) || gotOp != op {
			t.Error("应能取到原底层 *net.OpError")
		}
		var ue *url.Error
		if !errors.As(got, &ue) || strings.Contains(ue.URL, "APIKEYSECRET") {
			t.Error("errors.As 取到的 *url.Error 应已脱敏")
		}
	})

	t.Run("AgentError 包装后 Code 判断保持", func(t *testing.T) {
		orig, _ := newURLErr()
		got := RedactURLError(Wrap(orig, CodeToolExecution, "failed"))
		if strings.Contains(got.Error(), "APIKEYSECRET") {
			t.Fatalf("未脱敏: %s", got)
		}
		var ae *AgentError
		if !errors.As(got, &ae) || ae.Code != CodeToolExecution || ae.Message != "failed" {
			t.Errorf("应仍为 *AgentError 且保留 Code: %#v", got)
		}
		var gotURL *url.Error
		if !errors.As(got, &gotURL) || strings.Contains(gotURL.URL, "APIKEYSECRET") {
			t.Error("Cause 中的 *url.Error 应已脱敏")
		}
		if !strings.Contains(orig.URL, "APIKEYSECRET") {
			t.Error("原错误被修改")
		}
	})
}

func TestAgentError_MarshalLogObject(t *testing.T) {
	err := Wrap(fmt.Errorf("底层错误"), CodeToolExecution, "tool failed").
		WithComponent("tool").
		WithOperation("run").
		WithContext("tool_name", "search").
		WithContext("attempt", 2)

	enc := zapcore.NewMapObjectEncoder()
	if merr := err.MarshalLogObject(enc); merr != nil {
		t.Fatal(merr)
	}
	want := map[string]any{
		"code":      "TOOL_EXECUTION",
		"component": "tool",
		"operation": "run",
		"message":   "tool failed",
		"context":   map[string]any{"tool_name": "search", "attempt": 2},
		"cause":     "底层错误",
	}
	if got, w := fmt.Sprint(enc.Fields), fmt.Sprint(want); got != w {
		t.Errorf("MarshalLogObject = %s, want %s", got, w)
	}

	min := zapcore.NewMapObjectEncoder()
	_ = New(CodeInternal, "boom").MarshalLogObject(min)
	if len(min.Fields) != 2 || min.Fields["code"] != "INTERNAL_ERROR" || min.Fields["message"] != "boom" {
		t.Errorf("最小字段 = %v", min.Fields)
	}
}

func TestAgentError_Format(t *testing.T) {
	err := Wrap(errors.New("cause"), CodeToolExecution, "failed").
		WithContext("b", 2).
		WithContext("a", "x")
	if got := fmt.Sprintf("%v", err); got != err.Error() {
		t.Errorf("%%v = %q", got)
	}
	if got := fmt.Sprintf("%s", err); got != err.Error() {
		t.Errorf("%%s = %q", got)
	}
	if got, want := fmt.Sprintf("%q", err), fmt.Sprintf("%q", err.Error()); got != want {
		t.Errorf("%%q = %s, want %s", got, want)
	}
	if got, want := fmt.Sprintf("%+v", err), err.Error()+" context: a=x, b=2"; got != want {
		t.Errorf("%%+v = %q, want %q", got, want)
	}
	if got := fmt.Sprintf("%+v", New(CodeInternal, "boom")); got != "[INTERNAL_ERROR]: boom" {
		t.Errorf("无 Context 的 %%+v = %q", got)
	}
}
