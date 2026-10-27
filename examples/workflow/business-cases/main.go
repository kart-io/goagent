// Three native GoAgent examples, with opt-in live model decisions and local effects.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
)

type options struct {
	live                                       bool
	provider, model                            string
	scenario, dir, run, input, approval, actor string
	crash                                      bool
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := runCLI(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, args []string, out io.Writer) error {
	var opts options
	fs := flag.NewFlagSet("business-cases", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&opts.scenario, "case", "devops", "devops | article-review | auto-reply")
	fs.StringVar(&opts.dir, "dir", "", "required local checkpoint and simulated effect directory")
	fs.StringVar(&opts.run, "run", "demo-1", "workflow run ID; reuse to resume")
	fs.StringVar(&opts.input, "input", "", "optional input JSON file (used only for new runs)")
	fs.StringVar(&opts.approval, "approve", "", "exact waiting snapshot Key")
	fs.StringVar(&opts.actor, "actor", "", "demo-operator for DevOps; demo-reviewer for article review")
	fs.BoolVar(&opts.crash, "crash-after-effect", false, "exit 86 after first new local effect is persisted")
	fs.BoolVar(&opts.live, "live", false, "call a real model; business effects stay simulated")
	fs.StringVar(&opts.provider, "provider", "gemini", "live provider: gemini | deepseek | minimax")
	fs.StringVar(&opts.model, "model", "", "explicit live model; defaults to provider MODEL environment variable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(opts.dir) == "" || strings.TrimSpace(opts.run) == "" {
		return fmt.Errorf("-dir and nonempty -run required; positional arguments unsupported")
	}
	if opts.scenario != "devops" && opts.scenario != "article-review" && opts.scenario != "auto-reply" {
		return fmt.Errorf("unknown case %q", opts.scenario)
	}
	backend, err := openBackend(opts.dir)
	if err != nil {
		return err
	}
	if opts.live {
		if opts.model == "" {
			opts.model = os.Getenv(strings.ToUpper(opts.provider) + "_MODEL")
		}
		backend.model, err = newModelJudge(opts.provider, opts.model)
		if err != nil {
			return err
		}
	}
	if opts.crash {
		backend.after = func() error { os.Exit(86); return nil }
	}
	store, err := durable.NewFileStore(filepath.Join(opts.dir, opts.scenario, "checkpoints"))
	if err != nil {
		return err
	}
	switch opts.scenario {
	case "devops":
		return execute(ctx, opts, out, store, backend, devopsGraph,
			devopsState{IncidentID: "incident-001", Service: "demo-api", ErrorPercent: 25}, "demo-operator")
	case "article-review":
		return execute(ctx, opts, out, store, backend, articleGraph,
			articleState{ArticleID: "article-001", Title: "GoAgent 工作流实践", Body: strings.Repeat("示例介绍如何通过并行检查、条件分支和持久化恢复构建可靠的业务流程。", 4)}, "demo-reviewer")
	default:
		return execute(ctx, opts, out, store, backend, replyGraph,
			replyState{MessageID: "message-001", Recipient: "demo-user", Message: "营业时间是什么？"}, "")
	}
}

func execute[T any](ctx context.Context, opts options, out io.Writer, store durable.Store, backend *demoBackend,
	build func(context.Context, durable.Store, *demoBackend) (*workflow.DurableGraph[T], error), initial T, approver string) error {
	if opts.input != "" {
		f, err := os.Open(opts.input)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		// A supplied document replaces the default fixture completely.
		var supplied *T
		decoder := json.NewDecoder(f)
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&supplied); err != nil {
			return err
		}
		if supplied == nil {
			return fmt.Errorf("input must be a JSON object")
		}
		var extra any
		if err = decoder.Decode(&extra); err != io.EOF {
			return fmt.Errorf("input must contain exactly one JSON object")
		}
		initial = *supplied
	}
	g, err := build(ctx, store, backend)
	if err != nil {
		return err
	}
	if opts.approval != "" {
		// CLI labels simulate host authorization only. Production must derive the
		// actor from an authenticated identity, never a user/model supplied string.
		if approver == "" || opts.actor != approver {
			return fmt.Errorf("actor is not authorized for %s", opts.scenario)
		}
		snapshot, e := g.Snapshot(ctx, opts.run)
		if e != nil {
			return e
		}
		if !snapshot.Waiting || snapshot.Key != opts.approval {
			return fmt.Errorf("approval does not match waiting node")
		}
	}
	snapshot, err := g.AsRunnable().Invoke(ctx, workflow.RunRequest[T]{RunID: opts.run, Initial: initial, ApprovalKey: opts.approval})
	if err != nil {
		return err
	}
	status := "completed"
	if snapshot.Waiting {
		status = "approval_required"
	}
	modelCalls := int32(0)
	if backend.model != nil {
		modelCalls = backend.model.calls.Load()
	}
	return json.NewEncoder(out).Encode(struct {
		LiveModel  bool                       `json:"live_model"`
		ModelCalls int32                      `json:"model_calls_this_invocation"`
		Case       string                     `json:"case"`
		Status     string                     `json:"status"`
		Simulated  bool                       `json:"simulated"`
		Snapshot   *workflow.GraphSnapshot[T] `json:"snapshot"`
	}{opts.live, modelCalls, opts.scenario, status, true, snapshot})
}
