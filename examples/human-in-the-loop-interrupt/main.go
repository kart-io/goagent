// human-in-the-loop-interrupt 演示节点执行中途向人要输入，跨进程恢复，不调用模型。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

type refund struct {
	Order  string `json:"order"`
	Amount int    `json:"amount"`
	Status string `json:"status"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("human-in-the-loop-interrupt", flag.ContinueOnError)
	dir := flags.String("dir", "", "检查点目录")
	order := flags.String("order", "A1", "订单号（首次运行）")
	key := flags.String("key", "", "要回应的中断键")
	input := flags.String("input", "", "人工输入（JSON）")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dir == "" || flags.NArg() > 0 {
		return errors.New("需要提供 -dir，且不接受位置参数")
	}
	store, err := durable.NewFileStore(*dir)
	if err != nil {
		return err
	}
	graph, err := build(ctx, store)
	if err != nil {
		return err
	}
	var snap *workflow.GraphSnapshot[refund]
	if *key == "" {
		snap, err = graph.Run(ctx, "refund-"+*order, refund{Order: *order}, "")
	} else {
		// 实际应用须先认证回应者；中断键只定位暂停，不是授权凭证。
		snap, err = graph.Resume(ctx, "refund-"+*order, workflow.ResumeRequest{Key: *key, Input: json.RawMessage(*input)})
	}
	if err != nil && !errors.Is(err, agentErrors.ErrApprovalRequired) {
		return err
	}
	return json.Encode(out, snap, json.Pretty)
}

func build(ctx context.Context, store durable.Store) (*workflow.DurableGraph[refund], error) {
	g := workflow.NewGraph[refund]()
	err := g.AddNode("refund", func(ctx context.Context, r refund) (refund, error) {
		raw, err := durable.AwaitInput(ctx, "amount", json.RawMessage(`{"question":"退款金额（正整数）"}`), func(raw json.RawMessage) error {
			var n int
			if json.Unmarshal(raw, &n) != nil || n <= 0 {
				return errors.New("需要正整数")
			}
			return nil
		})
		if err != nil {
			return r, err
		}
		if err := json.Unmarshal(raw, &r.Amount); err != nil {
			return r, err
		}
		r.Status = "refunded"
		return r, nil
	})
	if err != nil {
		return nil, err
	}
	if err := g.AddEdge(constants.WorkflowStart, "refund"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("refund", constants.WorkflowEnd); err != nil {
		return nil, err
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "refund-interrupt-v1"})
}
