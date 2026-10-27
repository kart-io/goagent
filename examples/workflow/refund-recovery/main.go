// Command refund-recovery demonstrates local approval, crash and refund recovery.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/kart-io/goagent/core/durable"
)

const crashExitCode = 86

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := runCLI(ctx, os.Args[1:], os.Stdout, func() { os.Exit(crashExitCode) }); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, args []string, output io.Writer, crash func()) error {
	flags := flag.NewFlagSet("refund-recovery", flag.ContinueOnError)
	dir := flags.String("dir", "", "本地状态目录，保存检查点和模拟账本")
	id := flags.String("run", "refund-demo-1", "稳定的退款申请 ID；重启时复用")
	order := flags.String("order", "DEMO-1001", "合成订单：DEMO-1001 或 DEMO-EXPIRED；仅新申请使用")
	amount := flags.Int64("amount-cents", 9900, "退款金额（分）；仅新申请使用")
	approval := flags.String("approve", "", "明确批准等待中的步骤，传入输出的 snapshot.key；仅演示本地审批")
	fail := flags.Bool("crash-after-refund", false, "模拟账本提交后、检查点保存前退出，退出码 86")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 || *dir == "" || *id == "" || *amount <= 0 {
		return fmt.Errorf("dir, run and a positive amount are required; no positional arguments")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	store, err := durable.NewFileStore(filepath.Join(*dir, "checkpoints"))
	if err != nil {
		return err
	}
	ledger, err := openLedger(*dir)
	if err != nil {
		return err
	}
	defer func() { _ = ledger.db.Close() }()
	var afterRefund func()
	if *fail {
		if crash == nil {
			return fmt.Errorf("crash handler required")
		}
		afterRefund = crash
	}
	runner, err := newRunner(ctx, store, ledger, afterRefund)
	if err != nil {
		return err
	}
	initial, err := json.Marshal(refundState{OrderID: *order, AmountCents: *amount})
	if err != nil {
		return err
	}
	snapshot, err := runner.Run(ctx, *id, initial, *approval)
	status := "completed"
	if errors.Is(err, durable.ErrApprovalRequired) {
		status = "approval_required"
	} else if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(struct {
		Status   string            `json:"status"`
		Snapshot *durable.Snapshot `json:"snapshot"`
	}{status, snapshot})
}
