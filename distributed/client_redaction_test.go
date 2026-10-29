package distributed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kart-io/logger"
	"github.com/kart-io/logger/option"

	agentcore "github.com/kart-io/goagent/core"
)

// 传输错误文本含完整请求 URL，endpoint 中的 userinfo 不得进入返回错误与 Debug 日志。
func TestClient_TransportErrorRedactsURL(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "client.log")
	log, err := logger.New(&option.LogOption{Engine: "zap", Level: "DEBUG", Format: "json", OutputPaths: []string{logPath}})
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "http://tokenuser:pw@127.0.0.1:1"
	ctx := context.Background()
	input := &agentcore.AgentInput{}

	calls := map[string]func(c *Client) error{
		"ExecuteAgent": func(c *Client) error {
			_, err := c.ExecuteAgent(ctx, endpoint, "agent", input)
			return err
		},
		"ExecuteAgentAsync": func(c *Client) error {
			_, err := c.ExecuteAgentAsync(ctx, endpoint, "agent", input)
			return err
		},
		"GetAsyncResult": func(c *Client) error {
			_, _, err := c.GetAsyncResult(ctx, endpoint, "task")
			return err
		},
		"Ping": func(c *Client) error {
			return c.Ping(ctx, endpoint)
		},
		"ListAgents": func(c *Client) error {
			_, err := c.ListAgents(ctx, endpoint)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call(NewClient(log))
			if err == nil {
				t.Fatal("期望连接失败")
			}
			if s := err.Error(); strings.Contains(s, "tokenuser") {
				t.Errorf("未脱敏: %s", s)
			}
		})
	}

	_ = log.Flush()
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "Sending agent execution request") {
		t.Fatalf("未写出 Debug 日志: %s", data)
	}
	if strings.Contains(string(data), "tokenuser") || strings.Contains(string(data), ":pw@") {
		t.Errorf("日志未脱敏: %s", data)
	}
}
