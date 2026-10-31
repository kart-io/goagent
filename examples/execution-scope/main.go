// execution-scope 演示宿主标准装配入口，使用内置模拟模型，不发起真实模型请求。
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/execution"
	"github.com/kart-io/goagent/llm"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	const runID = "offline-scope-demo"
	controller, err := budget.New(budget.Config{RootKey: runID, Version: "demo-v1", MaxModelRequests: 2})
	if err != nil {
		return err
	}
	scope, err := execution.NewScope(execution.Config{RunID: runID, SessionID: "demo-session", Budget: controller, Timeout: 10 * time.Second})
	if err != nil {
		return err
	}
	runCtx, cancel, err := scope.Bind(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	configured, err := execution.ConfigureBuilder(builder.NewSimpleBuilder(llm.NewMockStreamClient()), scope, "assistant")
	if err != nil {
		return err
	}
	agent, err := configured.BuildContext(runCtx)
	if err != nil {
		return err
	}
	result, err := agent.AsRunnable().Invoke(runCtx, "演示统一会话、预算和取消上下文")
	if err != nil {
		return err
	}
	fmt.Printf("离线模拟结果：%v\n", result.Result)
	snapshot, err := controller.Snapshot(runCtx)
	if err != nil {
		return err
	}
	fmt.Printf("运行：%s，会话：%s，已预留请求：%d\n", scope.RunID(), scope.SessionID(), snapshot.ReservedRequests)
	return nil
}
