package errors

import (
	"errors"
	"net"
	"net/url"
	"testing"
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
