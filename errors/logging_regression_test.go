package errors

import (
	"bytes"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type loggingCause struct{ message string }

func (e *loggingCause) Error() string { return e.message }

// 真实日志必须保留结构字段；slog 将 LogValue panic 转成诊断文本同样属于失败。
func TestAgentErrorStructuredLoggingPreservesCause(t *testing.T) {
	var nilCause *loggingCause
	for _, tc := range []struct {
		name  string
		cause *loggingCause
		text  string
	}{
		{name: "空指针原因", cause: nilCause, text: "<nil>"},
		{name: "普通原因", cause: &loggingCause{message: "底层错误"}, text: "底层错误"},
	} {
		for _, logger := range []string{"slog", "zap"} {
			t.Run(tc.name+"/"+logger, func(t *testing.T) {
				err := Wrap(tc.cause, CodeToolExecution, "工具失败").
					WithComponent("tool").WithOperation("run").
					WithContext("secret", "敏感上下文").WithContext("attempt", 2)
				if got := err.Error(); got != "[TOOL_EXECUTION] [tool] operation=run: 工具失败: "+tc.text {
					t.Fatalf("默认错误文本异常: %s", got)
				}
				if strings.Contains(err.Error(), "敏感上下文") || strings.Contains(err.Error(), "secret") {
					t.Fatal("默认错误文本泄露 Context")
				}
				if !errors.Is(err, tc.cause) {
					t.Fatal("错误链没有保留原因身份")
				}
				var extracted *loggingCause
				if !errors.As(err, &extracted) || extracted != tc.cause {
					t.Fatal("错误链无法提取原始原因")
				}

				var buf bytes.Buffer
				if logger == "slog" {
					slog.New(slog.NewJSONHandler(&buf, nil)).Error("记录失败", "err", err)
				} else {
					log := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&buf), zapcore.InfoLevel))
					log.Error("记录失败", zap.Any("err", err))
				}
				var record struct {
					Err map[string]any `json:"err"`
				}
				if jerr := json.Unmarshal(buf.Bytes(), &record); jerr != nil {
					t.Fatalf("解析结构化日志失败: %v，日志: %s", jerr, buf.String())
				}
				want := map[string]any{
					"code": "TOOL_EXECUTION", "component": "tool", "operation": "run", "message": "工具失败",
					"context": map[string]any{"secret": "敏感上下文", "attempt": float64(2)}, "cause": tc.text,
				}
				if !reflect.DeepEqual(record.Err, want) {
					t.Errorf("结构化日志字段 = %#v，期望 %#v", record.Err, want)
				}
			})
		}
	}
}
