// execution-governed 用离线模型演示标准治理装配、最终验收和重建后的零请求重放。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/execution"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	directory, err := os.MkdirTemp("", "goagent-governed-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(directory)) }()
	journal, err := durable.NewFileStore(directory)
	if err != nil {
		return err
	}
	model := mocks.NewScriptedClient(mocks.ToolCallReply("读取", mocks.ToolCall("c1", "lookup", "{}")), mocks.TextReply("答复", "回执"))
	effects := 0
	for round := range 2 {
		controller, e := budget.NewDurable(budget.Config{RootKey: "demo", Version: "v1", MaxModelRequests: 2, MaxToolCalls: 1}, journal)
		if e != nil {
			return e
		}
		scope, e := execution.NewScope(execution.Config{RunID: "demo", Journal: journal, Budget: controller, Timeout: 10 * time.Second})
		if e != nil {
			return e
		}
		registry := tools.NewRegistry()
		if e = registry.Register(tools.NewBaseTool("lookup", "读取合成记录", `{"type":"object","additionalProperties":false}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			effects++
			return &interfaces.ToolOutput{Success: true, Result: "回执"}, nil
		})); e != nil {
			return e
		}
		b, report, e := execution.NewGovernedBuilder(model, scope, "assistant", registry, tools.DispatchConfig{
			MaxAdmissionConcurrency: 2, MaxQueued: 8,
			Authorize: func(_ context.Context, name string, _ *interfaces.ToolInput) error {
				if name != "lookup" {
					return fmt.Errorf("工具未获授权")
				}
				return nil
			},
		})
		if e != nil {
			return e
		}
		b.WithOutputValidator(func(_ context.Context, out *builder.AgentOutput) error {
			if out.Result != "回执" {
				return fmt.Errorf("最终回执不匹配")
			}
			return nil
		})
		bound, cancel, e := scope.Bind(ctx)
		if e != nil {
			return e
		}
		a, e := b.BuildContext(bound)
		if e != nil {
			cancel()
			return e
		}
		out, e := a.AsRunnable().Invoke(bound, "读取记录并只返回回执")
		cancel()
		if e != nil {
			return e
		}
		fmt.Printf("轮次%d：%v；根请求额度=%d，工具额度=%d\n", round+1, out.Result, report.MaxModelRequests, report.MaxToolCalls)
	}
	if effects != 1 || len(model.Requests()) != 2 {
		return fmt.Errorf("恢复产生了重复模型请求或工具效果")
	}
	fmt.Println("重建重放通过：模型请求累计2次，工具效果累计1次。")
	return nil
}
