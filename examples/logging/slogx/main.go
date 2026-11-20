// Command slogx shows how a host wires github.com/kart-io/slogx into goagent.
//
// slogx.Init configures the process-wide logger; Builder.WithLogger then
// writes every run event (agent, model call, tool call, usage) as one JSON
// record carrying run_id, path, duration and error kind, never inputs or
// outputs. With DEEPSEEK_API_KEY set the example calls DeepSeek, otherwise a
// scripted offline model.
//
// It lives in its own module so slogx and its dependencies stay out of
// goagent's go.mod:
//
//	cd examples/logging/slogx && go run .
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/kart-io/slogx"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
)

func main() {
	answer, err := run(context.Background(), os.Stdout, os.Getenv("DEEPSEEK_API_KEY"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "运行失败:", err)
		os.Exit(1)
	}
	// 日志写标准输出，答案写标准错误，便于把日志直接交给 jq 等工具。
	fmt.Fprintln(os.Stderr, "回答:", answer)
}

func run(ctx context.Context, logs io.Writer, apiKey string) (string, error) {
	logger, cleanup, err := slogx.Init(logs, slogx.Config{
		Level:   "debug",
		Format:  "json",
		Service: "goagent-slogx-example",
		Env:     "development",
	})
	if err != nil {
		return "", err
	}
	defer cleanup()

	client, err := newClient(apiKey)
	if err != nil {
		return "", err
	}
	agent, err := builder.NewSimpleBuilder(client).WithMaxIterations(10).WithTimeout(5 * time.Minute).
		WithSystemPrompt("你是电商客服。回答发货问题前必须调用 lookup_order 查询订单。").
		WithTools(lookupOrder()).
		WithMaxIterations(3).
		WithLogger(logger).
		Build()
	if err != nil {
		return "", err
	}
	out, err := agent.AsRunnable().Invoke(ctx, "订单 A1001 发货了吗？")
	if err != nil {
		return "", err
	}
	answer := fmt.Sprint(out.Result)
	// 宿主自己的业务日志与运行事件写入同一个日志器，按 run 之外的维度（如订单号）检索。
	logger.InfoContext(ctx, "客服回答完成", "order_id", "A1001", "answer_len", len([]rune(answer)))
	return answer, nil
}

func newClient(apiKey string) (llm.Client, error) {
	if apiKey != "" {
		return providers.NewDeepSeekWithOptions(llm.WithAPIKey(apiKey), llm.WithModel("deepseek-chat"), llm.WithMaxTokens(300))
	}
	return mocks.NewScriptedClient(
		mocks.ToolCallReply("查询订单", mocks.ToolCall("call-1", "lookup_order", `{"order_id":"A1001"}`)),
		mocks.TextReply("回答", "订单 A1001 已于今天上午发货，顺丰单号 SF123456。"),
	), nil
}

func lookupOrder() interfaces.Tool {
	tool, err := tools.NewFunctionToolBuilder("lookup_order").
		WithDescription("按订单号查询发货状态").
		WithArgsSchema(`{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"]}`).
		WithFunction(func(_ context.Context, args map[string]interface{}) (interface{}, error) {
			return map[string]string{"order_id": fmt.Sprint(args["order_id"]), "status": "已发货", "tracking_no": "SF123456"}, nil
		}).
		Build()
	if err != nil {
		panic(err)
	}
	return tool
}
