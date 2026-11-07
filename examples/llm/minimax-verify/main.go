// minimax-verify 验证国内 Anthropic 接口的精确参数、最终回执和持久重放，最多三个模型请求。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/evaluation/agentbench"
	"github.com/kart-io/goagent/execution"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
)

type result struct{ Requests, Effects, ReplayRequests, ReplayEffects int }

type countedClient struct {
	llm.ToolStreamClient
	requests atomic.Int64
}

func (c *countedClient) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	c.requests.Add(1)
	return c.ToolStreamClient.CompleteToolsStream(ctx, req, tools)
}

func verify(ctx context.Context, client llm.ToolStreamClient) (got result, err error) {
	directory, err := os.MkdirTemp("", "goagent-minimax-probe-")
	if err != nil {
		return got, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(directory)) }()
	store, err := durable.NewFileStore(directory)
	if err != nil {
		return got, err
	}
	model := &countedClient{ToolStreamClient: client}
	defer func() { got.Requests = int(model.requests.Load()) }()
	contract := agentbench.ToolContract{ID: "9007199254740993", Rate: "0.1234567890123456789", Label: "<核验&>", Receipt: "minimax-probe-ok", StringNumbers: true}
	for round := range 2 {
		beforeRequests, beforeEffects := int(model.requests.Load()), got.Effects
		ctrl, e := budget.NewDurable(budget.Config{RootKey: "minimax-probe", Version: "v1", MaxModelRequests: 3, MaxToolCalls: 1}, store)
		if e != nil {
			return got, e
		}
		scope, e := execution.NewScope(execution.Config{RunID: "minimax-probe", Journal: store, Budget: ctrl, Timeout: 150 * time.Second})
		if e != nil {
			return got, e
		}
		registry := tools.NewRegistry()
		e = registry.Register(tools.NewBaseTool("verify", "核验合成参数并生成回执", contract.Schema(), func(_ context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			if e := contract.CheckArguments(in); e != nil {
				return nil, e
			}
			got.Effects++
			return &interfaces.ToolOutput{Success: true, Result: map[string]string{"receipt": contract.Receipt}}, nil
		}))
		if e != nil {
			return got, e
		}
		b, _, e := execution.NewGovernedBuilder(model, scope, "assistant", registry, tools.DispatchConfig{
			MaxAdmissionConcurrency: 1, MaxQueued: 1,
			Authorize: func(_ context.Context, name string, in *interfaces.ToolInput) error {
				if name != "verify" {
					return fmt.Errorf("未知工具")
				}
				return contract.CheckArguments(in)
			},
		})
		if e != nil {
			return got, e
		}
		b.WithMaxIterations(5).WithTurnStopping(contract.Stop).WithOutputValidator(func(_ context.Context, out *builder.AgentOutput) error {
			if got.Effects != 1 {
				return fmt.Errorf("必须恰好执行一次核验工具")
			}
			return contract.CheckAnswer(out.Result)
		})
		bound, cancel, e := scope.Bind(ctx)
		if e != nil {
			return got, e
		}
		a, e := b.BuildContext(bound)
		if e == nil {
			_, e = a.AsRunnable().Invoke(bound, `调用 verify 一次，参数 id="9007199254740993"、rate="0.1234567890123456789"、label="<核验&>"。三个字段均按字符串原样传入。完成后只输出工具结果 receipt 的字符串原值，不要输出JSON或说明。`)
		}
		cancel()
		if e != nil {
			return got, e
		}
		if round == 1 {
			got.ReplayRequests = int(model.requests.Load()) - beforeRequests
			got.ReplayEffects = got.Effects - beforeEffects
			if got.ReplayRequests != 0 || got.ReplayEffects != 0 {
				return got, fmt.Errorf("重放新增模型请求或工具效果")
			}
		}
	}
	return got, nil
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("minimax-verify", flag.ContinueOnError)
	live := flags.Bool("live", false, "显式允许最多三个真实请求")
	model := flags.String("model", "MiniMax-M3.1-Flash", "MiniMax模型")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*live || flags.NArg() != 0 {
		return fmt.Errorf("请显式传入 -live；最多三个请求，不重试网络错误")
	}
	key := os.Getenv("MINIMAX_CN_API_KEY")
	if key == "" {
		return fmt.Errorf("必须设置专用 MINIMAX_CN_API_KEY")
	}
	client, err := providers.NewAnthropicWithOptions(
		llm.WithAPIKey(key), llm.WithBaseURL("https://api.minimax.cn/anthropic"), llm.WithModel(*model),
		llm.WithMaxTokens(4096), llm.WithTimeout(80*time.Second), llm.WithStreamIdleTimeout(40*time.Second), llm.WithStreamTimeout(100*time.Second),
		llm.WithCustomHeaders(map[string]string{"Authorization": "Bearer " + key}),
	)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	got, err := verify(ctx, client)
	if err != nil {
		return fmt.Errorf("MiniMax验收失败（请求%d、效果%d）：%w", got.Requests, got.Effects, err)
	}
	_, err = fmt.Fprintf(out, "MiniMax Anthropic验收通过：请求%d、工具效果%d；重放新增请求%d、新增效果%d。\n", got.Requests, got.Effects, got.ReplayRequests, got.ReplayEffects)
	return err
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
