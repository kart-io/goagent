package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kart-io/goagent/codec/json"
)

// 离线运行一次：每个运行事件都是带 run_id 的 JSON 记录，工具调用可见，日志不含工具结果。
func TestRunLogsLifecycleThroughSlogx(t *testing.T) {
	var logs bytes.Buffer
	answer, err := run(context.Background(), &logs, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "SF123456") {
		t.Fatalf("answer = %q", answer)
	}

	levels := map[string]string{}
	runIDs := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("非 JSON 日志行 %q: %v", line, err)
		}
		if rec["service"] != "goagent-slogx-example" {
			t.Fatalf("缺少 service 字段: %s", line)
		}
		msg, _ := rec["msg"].(string)
		levels[msg], _ = rec["level"].(string)
		if id, ok := rec["run_id"].(string); ok {
			runIDs[id] = true
		}
	}
	for msg, level := range map[string]string{
		"agent_start": "DEBUG", "model_start": "DEBUG", "model_end": "INFO",
		"tool_start": "DEBUG", "tool_end": "INFO", "agent_end": "INFO", "客服回答完成": "INFO",
	} {
		if levels[msg] != level {
			t.Errorf("%s 级别 = %q，期望 %q", msg, levels[msg], level)
		}
	}
	if len(runIDs) != 1 {
		t.Errorf("运行事件应共享一个 run_id，实际 %d 个", len(runIDs))
	}
	if strings.Contains(logs.String(), "已发货") {
		t.Error("日志不应包含工具结果")
	}
}
