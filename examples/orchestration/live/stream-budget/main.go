// 流式摘要与预算争用：
//   - Summarize 把 Agent 的流式文本接成 core/flow 的 Source，预览即时下发，终态只提交一次；
//   - Contend 让多个子 Agent 并发争用同一根预算，额度用完的请求在发出前被拒绝。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/flow"
	"github.com/kart-io/goagent/examples/internal/livemodel"
)

// Report 是需要摘要的合成故障复盘材料。
const Report = `故障复盘材料：10:02 订单服务发布 v2.8.1，新版本将库存查询从缓存改为直连数据库。
10:05 数据库 CPU 升至 95%，订单接口 P99 从 120ms 升至 2.4s，下单成功率降至 71%。
10:11 值班回滚到 v2.8.0，10:14 指标恢复。影响约 9 分钟，约 3200 笔下单失败，无数据丢失。
后续：库存查询恢复缓存并增加压测门禁；发布流程增加数据库负载告警。`

const summaryPrompt = `你是复盘助手。用三行中文概括输入材料：
第一行“结论：<一句话根因>”；第二行“影响：<时长与规模>”；第三行“改进：<最重要的一项措施>”。不要输出其他内容。`

// SummaryResult 区分预览与终态：预览只是展示，终态在 Source 成功结束后才提交。
type SummaryResult struct {
	Final                 string
	Previews              []string
	FirstPreview, Elapsed time.Duration
	NewModelCalls         int64
	Budget                budget.Snapshot
}

// agentSource 同步消费 ExecuteStream 的文本增量；下游出错时取消上游并排空通道。
func agentSource(agent *builder.ConfigurableAgent[any, *core.AgentState], input string) flow.Source[string] {
	return func(ctx context.Context, yield func(string) error) error {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		events, err := agent.ExecuteStream(runCtx, input)
		if err != nil {
			return err
		}
		var first error
		for e := range events {
			if first != nil {
				continue
			}
			switch {
			case e.Error != nil:
				first = e.Error
			case e.Type == "error":
				first = fmt.Errorf("执行失败：%s", e.Message)
			case e.Type == "text" && e.Delta != "":
				first = yield(e.Delta)
			}
			if first != nil {
				cancel()
			}
		}
		return errors.Join(first, ctx.Err())
	}
}

// Summarize 以增量方式生成复盘摘要，onPreview 可为 nil。
func Summarize(ctx context.Context, client *livemodel.Client, onPreview func(string)) (SummaryResult, error) {
	var out SummaryResult
	controller, err := budget.New(budget.Config{RootKey: "summary/inc-7", Version: "v1", MaxModelRequests: 1})
	if err != nil {
		return out, err
	}
	agent, err := builder.NewSimpleBuilder(client).WithDurableScope("summary").WithSystemPrompt(summaryPrompt).
		WithMaxIterations(1).WithMaxTokens(livemodel.MaxTokens).Build()
	if err != nil {
		return out, err
	}
	run, err := flow.AsRunnable(func(_ context.Context, input string) (flow.Source[string], error) {
		// Map 只做逐项规整；不在这里拼接，避免预览和终态重复。
		return flow.Map(agentSource(agent, input), func(_ context.Context, s string) (string, error) {
			return strings.ReplaceAll(s, "\r", ""), nil
		}), nil
	}, flow.Fold[string, string]{
		Init: func() string { return "" },
		Add:  func(_ context.Context, acc, s string) (string, error) { return acc + s, nil },
		Finish: func(_ context.Context, acc string) (string, error) {
			if strings.TrimSpace(acc) == "" {
				return "", fmt.Errorf("模型没有输出正文")
			}
			return strings.TrimSpace(acc), nil
		},
	}, func(s string) (*core.StreamEvent, error) { return &core.StreamEvent{Type: "preview", Delta: s}, nil },
		flow.Limits[string]{MaxItems: 4096, MaxBytes: 64 << 10, Size: func(s string) int64 { return int64(len(s)) }})
	if err != nil {
		return out, err
	}
	start := time.Now()
	before := client.Calls()
	stream, err := run.Stream(budget.WithController(ctx, controller), Report)
	if err != nil {
		return out, err
	}
	for chunk := range stream {
		switch {
		case chunk.Error != nil:
			err = chunk.Error
		case chunk.Event != nil:
			if len(out.Previews) == 0 {
				out.FirstPreview = time.Since(start)
			}
			out.Previews = append(out.Previews, chunk.Event.Delta)
			if onPreview != nil {
				onPreview(chunk.Event.Delta)
			}
		case chunk.Done:
			out.Final = chunk.Data
		}
	}
	out.Elapsed = time.Since(start)
	out.NewModelCalls = client.Calls() - before
	out.Budget, _ = controller.Snapshot(ctx)
	return out, err
}

// ContendResult 记录每个子 Agent 的结果与根预算的最终占用。
type ContendResult struct {
	Succeeded, Rejected int
	Answers             []string
	Errors              []error
	NewModelCalls       int64
	Budget              budget.Snapshot
}

// Contend 让 children 个子 Agent 并发运行，共享额度为 limit 的根预算。
func Contend(ctx context.Context, client *livemodel.Client, children, limit int) (ContendResult, error) {
	var out ContendResult
	controller, err := budget.New(budget.Config{RootKey: "contend/batch-1", Version: "v1", MaxModelRequests: limit})
	if err != nil {
		return out, err
	}
	ctx = budget.WithController(ctx, controller)
	questions := []string{"用一句话解释什么是幂等。", "用一句话解释什么是熔断。", "用一句话解释什么是背压。", "用一句话解释什么是限流。"}
	var mu sync.Mutex
	var wg sync.WaitGroup
	before := client.Calls()
	for i := 0; i < children; i++ {
		agent, err := builder.NewSimpleBuilder(client).WithDurableScope(fmt.Sprintf("child-%d", i)).
			WithSystemPrompt("你是技术术语助手，回答不超过 40 字。").WithMaxIterations(1).WithMaxTokens(livemodel.MaxTokens).Build()
		if err != nil {
			return out, err
		}
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			res, err := agent.AsRunnable().Invoke(ctx, q)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Errors = append(out.Errors, err)
				if errors.Is(err, budget.ErrBudgetExceeded) {
					out.Rejected++
				}
				return
			}
			out.Succeeded++
			out.Answers = append(out.Answers, fmt.Sprint(res.Result))
		}(questions[i%len(questions)])
	}
	wg.Wait()
	out.NewModelCalls = client.Calls() - before
	out.Budget, err = controller.Snapshot(ctx)
	return out, err
}

func main() {
	children := flag.Int("children", 3, "并发子 Agent 数")
	limit := flag.Int("limit", 2, "根预算的模型请求额度")
	flag.Parse()
	client, err := livemodel.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("== 流式摘要（预览逐段到达，终态只提交一次）")
	s, err := Summarize(context.Background(), client, func(d string) { fmt.Print(d) })
	if err != nil {
		fmt.Fprintln(os.Stderr, "\n摘要失败：", livemodel.Redact(err.Error()))
		os.Exit(1)
	}
	fmt.Printf("\n预览 %d 段，首段 %v，总耗时 %v，终态 %d 字\n", len(s.Previews), s.FirstPreview.Round(time.Millisecond), s.Elapsed.Round(time.Millisecond), utf8.RuneCountInString(s.Final))
	fmt.Printf("\n== 预算争用：%d 个子 Agent 共享 %d 次额度\n", *children, *limit)
	c, err := Contend(context.Background(), client, *children, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(1)
	}
	fmt.Printf("成功 %d，被预算拒绝 %d，实际请求 %d，预留 %d\n", c.Succeeded, c.Rejected, c.NewModelCalls, c.Budget.ReservedRequests)
	for _, a := range c.Answers {
		fmt.Println(" -", a)
	}
}
