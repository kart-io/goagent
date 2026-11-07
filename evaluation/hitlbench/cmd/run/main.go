// run advances one bounded live-model HITL phase. Each invocation is a new process.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/workflow"
)

const (
	maxOutput   = 512
	callTimeout = 60 * time.Second
	crashExit   = 86
	orderID     = "DEMO-HITL-01"
	draftPrompt = "你是客服通知起草员。根据输入写一句中文草稿，必须原样包含订单号和金额，说明退款仍待人工审核。不要增加其他事实。"
	finalPrompt = "你是客服通知编辑。输入是经过人工审核的状态，approved_amount 才是最终金额，draft 中的旧金额已作废。只输出一个合法 JSON 对象，不要 Markdown：{\"order_id\":\"原订单号\",\"amount_yuan\":最终金额整数,\"notice\":\"一条包含原订单号和最终金额的中文退款通知\"}。notice 不得包含旧金额88，不得声称已真实支付。"
)

type state struct {
	OrderID        string `json:"order_id"`
	OriginalAmount int    `json:"original_amount"`
	ApprovedAmount int    `json:"approved_amount,omitzero"`
	Draft          string `json:"draft,omitempty"`
	Decision       string `json:"decision,omitempty"`
	Reviewer       string `json:"reviewer,omitempty"`
	Final          string `json:"final,omitempty"`
	Status         string `json:"status"`
}

type response struct {
	Decision string `json:"decision"`
	Amount   int    `json:"amount"`
}

type principalKey struct{}

type countedClient struct {
	llm.ToolStreamClient
	calls atomic.Int64
	model string
}

func (c *countedClient) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	c.calls.Add(1)
	return c.ToolStreamClient.CompleteStream(ctx, c.withModel(req))
}

func (c *countedClient) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, ts []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	c.calls.Add(1)
	return c.ToolStreamClient.CompleteToolsStream(ctx, c.withModel(req), ts)
}

func (c *countedClient) withModel(req *llm.CompletionRequest) *llm.CompletionRequest {
	if req == nil {
		return nil
	}
	copy := *req
	if copy.Model == "" {
		copy.Model = c.model
	}
	return &copy
}

type crashStore struct{ durable.Store }

func (s crashStore) Save(ctx context.Context, id string, cp *durable.Snapshot) error {
	if err := s.Store.Save(ctx, id, cp); err != nil {
		return err
	}
	if cp.Approved {
		os.Exit(crashExit)
	}
	return nil
}

type result struct {
	Provider   string                         `json:"provider"`
	Model      string                         `json:"model"`
	RunID      string                         `json:"run_id"`
	Phase      string                         `json:"phase"`
	StartedAt  time.Time                      `json:"started_at"`
	DurationMS int64                          `json:"duration_ms"`
	NewCalls   int64                          `json:"new_model_calls"`
	Snapshot   *workflow.GraphSnapshot[state] `json:"snapshot,omitzero"`
	Budget     budget.Snapshot                `json:"budget"`
	Error      string                         `json:"error,omitempty"`
}

func redact(s string) string {
	for _, name := range []string{"GEMINI_API_KEY", "DEEPSEEK_API_KEY"} {
		if key := os.Getenv(name); key != "" {
			s = strings.ReplaceAll(s, key, "[REDACTED]")
		}
	}
	return s
}

func build(ctx context.Context, client *countedClient, store durable.Store, provider, model string) (*workflow.DurableGraph[state], error) {
	g := workflow.NewGraph[state]()
	modelNode := func(stage, prompt string) workflow.Node[state] {
		return func(ctx context.Context, s state) (state, error) {
			a, err := builder.NewSimpleBuilder(client).WithDurableScope(stage).
				WithSystemPrompt(prompt).WithMaxIterations(1).WithMaxTokens(maxOutput).
				WithTemperature(0).WithTimeout(callTimeout).Build()
			if err != nil {
				return s, err
			}
			input, err := json.Marshal(s)
			if err != nil {
				return s, err
			}
			out, err := a.AsRunnable().Invoke(ctx, string(input))
			if err != nil {
				return s, err
			}
			if stage == "draft" {
				s.Draft = fmt.Sprint(out.Result)
			} else {
				s.Final = fmt.Sprint(out.Result)
				s.Status = "accepted"
			}
			return s, nil
		}
	}
	nodes := []struct {
		name string
		fn   workflow.Node[state]
	}{
		{"draft", modelNode("draft", draftPrompt)},
		{"review", func(_ context.Context, s state) (state, error) { return s, nil }},
		{"final", modelNode("final", finalPrompt)},
		{"reject", func(_ context.Context, s state) (state, error) { s.Status = "rejected"; return s, nil }},
	}
	for _, node := range nodes {
		if err := g.AddNode(node.name, node.fn); err != nil {
			return nil, err
		}
	}
	for _, edge := range [][2]string{{workflow.START, "draft"}, {"draft", "review"}, {"final", workflow.END}, {"reject", workflow.END}} {
		if err := g.AddEdge(edge[0], edge[1]); err != nil {
			return nil, err
		}
	}
	if err := g.AddBranch("review", func(_ context.Context, s state) (string, error) {
		if s.Decision == "approve" {
			return "final", nil
		}
		return "reject", nil
	}, "final", "reject"); err != nil {
		return nil, err
	}
	return g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "live-hitl-v1/" + provider + "/" + model, ApprovalNodes: []string{"review"}},
		workflow.WithResumeHandler(func(ctx context.Context, _ string, snap *workflow.GraphSnapshot[state], input json.RawMessage) (state, error) {
			var r response
			if err := json.Unmarshal(input, &r); err != nil {
				return state{}, err
			}
			if r.Decision != "approve" && r.Decision != "reject" {
				return state{}, errors.New("invalid human decision")
			}
			if r.Amount <= 0 || r.Amount > snap.State.OriginalAmount {
				return state{}, errors.New("invalid approved amount")
			}
			s := snap.State
			s.ApprovedAmount, s.Decision = r.Amount, r.Decision
			s.Reviewer, _ = ctx.Value(principalKey{}).(string)
			return s, nil
		}), workflow.WithBeforeApprove(func(ctx context.Context, _ string, snap *workflow.GraphSnapshot[state]) error {
			if ctx.Value(principalKey{}) != "synthetic-reviewer" {
				return errors.New("reviewer not authorized")
			}
			if snap.State.Decision == "" {
				return errors.New("human decision is required")
			}
			return nil
		}))
}

func main() { os.Exit(run()) }

func run() int {
	live := flag.Bool("live", false, "explicitly allow real bounded model calls")
	provider := flag.String("provider", "", "gemini or deepseek")
	model := flag.String("model", "", "explicit model ID")
	dir := flag.String("dir", "", "persistent checkpoint directory")
	id := flag.String("run", "", "stable scenario run ID")
	phase := flag.String("phase", "run", "run, status or resume")
	key := flag.String("key", "", "waiting checkpoint key")
	decision := flag.String("decision", "approve", "approve or reject")
	amount := flag.Int("amount", 66, "human-reviewed amount")
	actor := flag.String("actor", "synthetic-reviewer", "synthetic actor for this test")
	crash := flag.Bool("crash-after-accept", false, "exit 86 after human decision is saved")
	flag.Parse()
	if !*live || *model == "" || *dir == "" || *id == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "explicit -live, -provider, -model, -dir and -run are required")
		return 2
	}
	started := time.Now().UTC()
	out := result{Provider: *provider, Model: *model, RunID: *id, Phase: *phase, StartedAt: started}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, principalKey{}, *actor)
	var client *countedClient
	var controller *budget.Controller
	err := func() error {
		opts := []llm.ClientOption{llm.WithModel(*model), llm.WithMaxTokens(maxOutput), llm.WithTemperature(0), llm.WithTimeout(callTimeout), llm.WithStreamIdleTimeout(20 * time.Second), llm.WithStreamTimeout(callTimeout)}
		var raw llm.ToolStreamClient
		var e error
		switch *provider {
		case "gemini":
			raw, e = providers.NewGeminiWithOptions(append(opts, llm.WithReasoning(usage.ReasoningConfig{BudgetSet: true, BudgetTokens: 0}))...)
		case "deepseek":
			raw, e = providers.NewDeepSeekWithOptions(opts...)
		default:
			return errors.New("provider must be gemini or deepseek")
		}
		if e != nil {
			return e
		}
		client = &countedClient{ToolStreamClient: raw, model: *model}
		base, e := durable.NewFileStore(*dir)
		if e != nil {
			return e
		}
		controller, e = budget.NewDurable(budget.Config{RootKey: *id, Version: "live-hitl-v1/" + *provider + "/" + *model, MaxModelRequests: 2}, base)
		if e != nil {
			return e
		}
		ctx = budget.WithController(ctx, controller)
		var store durable.Store = base
		if *crash {
			store = crashStore{base}
		}
		graph, e := build(ctx, client, store, *provider, *model)
		if e != nil {
			return e
		}
		switch *phase {
		case "run":
			out.Snapshot, e = graph.Run(ctx, *id, state{OrderID: orderID, OriginalAmount: 88, Status: "draft"}, "")
		case "status":
			out.Snapshot, e = graph.Snapshot(ctx, *id)
		case "resume":
			input, marshalErr := json.Marshal(response{Decision: *decision, Amount: *amount})
			if marshalErr != nil {
				return marshalErr
			}
			out.Snapshot, e = graph.Resume(ctx, *id, workflow.ResumeRequest{Key: *key, Input: input})
		default:
			return errors.New("unknown phase")
		}
		if e == agentErrors.ErrApprovalRequired && out.Snapshot != nil && out.Snapshot.Waiting {
			return nil
		}
		return e
	}()
	if client != nil {
		out.NewCalls = client.calls.Load()
	}
	if controller != nil {
		statsCtx, statsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		var statsErr error
		out.Budget, statsErr = controller.Snapshot(statsCtx)
		statsCancel()
		err = errors.Join(err, statsErr)
	}
	out.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		out.Error = redact(err.Error())
	}
	if err := json.Encode(os.Stdout, out); err != nil {
		fmt.Fprintln(os.Stderr, redact(err.Error()))
		return 1
	}
	if out.Error != "" {
		return 1
	}
	return 0
}
