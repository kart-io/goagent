// Package frameworkbench compares real framework execution with deterministic local models.
package frameworkbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	etool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

const Prompt = "Look up refund_days for order DEMO-1001. Return only the number."
const ToolSchema = `{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`

type Outcome struct {
	Answer     string `json:"answer"`
	ModelCalls int64  `json:"model_calls"`
	ToolCalls  int64  `json:"tool_calls"`
	Chunks     int    `json:"chunks"`
	FirstNS    int64  `json:"first_ns,omitempty"`
}
type Runner struct {
	run func(context.Context) (Outcome, error)
}

func (r *Runner) Run(ctx context.Context) (Outcome, error) { return r.run(ctx) }

type counts struct{ model, tool atomic.Int64 }

func (c *counts) reset() { c.model.Store(0); c.tool.Store(0) }
func validateArgs(args map[string]any) error {
	if len(args) != 1 || args["order_id"] != "DEMO-1001" {
		return fmt.Errorf("wrong tool arguments: %v", args)
	}
	return nil
}

func NewRunner(framework, scenario string) (*Runner, error) {
	if scenario != "graph_8" && scenario != "branch_loop_8" && scenario != "agent_tool" && scenario != "stream_64" {
		return nil, fmt.Errorf("unknown scenario %q", scenario)
	}
	switch framework {
	case "goagent":
		return newGoAgent(scenario)
	case "eino":
		return newEino(scenario)
	}
	return nil, fmt.Errorf("unknown framework %q", framework)
}

func newGoAgent(scenario string) (*Runner, error) {
	ctx := context.Background()
	if scenario == "graph_8" || scenario == "branch_loop_8" {
		graph := workflow.NewGraph[int](workflow.Config{MaxSteps: 32})
		nodes := 8
		if scenario == "branch_loop_8" {
			nodes = 1
		}
		prev := workflow.START
		for i := 0; i < nodes; i++ {
			name := fmt.Sprintf("n%d", i)
			if err := graph.AddNode(name, func(_ context.Context, n int) (int, error) { return n + 1, nil }); err != nil {
				return nil, err
			}
			if err := graph.AddEdge(prev, name); err != nil {
				return nil, err
			}
			prev = name
		}
		var err error
		if nodes == 1 {
			err = graph.AddBranch(prev, func(_ context.Context, n int) (string, error) {
				if n < 8 {
					return prev, nil
				}
				return workflow.END, nil
			}, prev, workflow.END)
		} else {
			err = graph.AddEdge(prev, workflow.END)
		}
		if err != nil {
			return nil, err
		}
		compiled, err := graph.Compile(ctx)
		if err != nil {
			return nil, err
		}
		return &Runner{run: func(ctx context.Context) (Outcome, error) {
			n, err := compiled.Invoke(ctx, 0)
			return Outcome{Answer: strconv.Itoa(n)}, err
		}}, nil
	}
	c := &counts{}
	m := &goModel{counts: c, scenario: scenario}
	b := builder.NewSimpleBuilder(m).WithMaxIterations(2)
	if scenario == "agent_tool" {
		b = b.WithTools(tools.NewFunctionTool("lookup", "Look up refund_days for an order", ToolSchema, func(_ context.Context, args map[string]any) (any, error) {
			if err := validateArgs(args); err != nil {
				return nil, err
			}
			c.tool.Add(1)
			return "30", nil
		}))
	}
	agent, err := b.Build()
	if err != nil {
		return nil, err
	}
	runnable := agent.AsRunnable()
	return &Runner{run: func(ctx context.Context) (Outcome, error) {
		c.reset()
		started := time.Now()
		out := Outcome{}
		if scenario == "stream_64" {
			ch, err := runnable.Stream(ctx, Prompt)
			if err != nil {
				return out, err
			}
			for chunk := range ch {
				if chunk.Error != nil {
					return out, chunk.Error
				}
				if chunk.Event != nil && chunk.Event.Type == "text" && chunk.Event.Delta != "" {
					if out.FirstNS == 0 {
						out.FirstNS = time.Since(started).Nanoseconds()
					}
					out.Chunks++
				}
				if chunk.Done {
					out.Answer = fmt.Sprint(chunk.Data.Result)
				}
			}
		} else {
			res, err := runnable.Invoke(ctx, Prompt)
			if err != nil {
				return out, err
			}
			out.Answer = fmt.Sprint(res.Result)
		}
		out.ModelCalls = c.model.Load()
		out.ToolCalls = c.tool.Load()
		return out, nil
	}}, nil
}

type goModel struct {
	counts   *counts
	scenario string
}

func (m *goModel) Provider() constants.Provider { return constants.ProviderOpenAI }
func (m *goModel) IsAvailable() bool            { return true }
func (m *goModel) Complete(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, fmt.Errorf("unexpected nonstream model path")
}
func (m *goModel) Chat(context.Context, []llm.Message) (*llm.CompletionResponse, error) {
	return nil, fmt.Errorf("unexpected chat path")
}
func (m *goModel) CompleteStream(ctx context.Context, r *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return m.CompleteToolsStream(ctx, r, nil)
}
func (m *goModel) ChatStream(ctx context.Context, msg []llm.Message) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, &llm.CompletionRequest{Messages: msg})
}
func (m *goModel) CompleteToolsStream(_ context.Context, r *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.counts.model.Add(1)
	if m.scenario == "stream_64" {
		ch := make(chan *llm.StreamChunk, 65)
		for i := 0; i < 64; i++ {
			ch <- &llm.StreamChunk{Delta: "x"}
		}
		ch <- &llm.StreamChunk{Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 64}}
		close(ch)
		return ch, nil
	}
	ch := make(chan *llm.StreamChunk, 2)
	if len(r.Messages) > 0 && r.Messages[len(r.Messages)-1].Role == "tool" {
		last := r.Messages[len(r.Messages)-1]
		if last.Content != "30" || last.ToolCallID != "call-1" {
			return nil, fmt.Errorf("tool result not preserved: %+v", last)
		}
		ch <- &llm.StreamChunk{Delta: last.Content}
		ch <- &llm.StreamChunk{Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 2}}
	} else {
		call := llm.ToolCall{ID: "call-1", Type: "function"}
		call.Function.Name = "lookup"
		call.Function.Arguments = `{"order_id":"DEMO-1001"}`
		ch <- &llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{call}, Usage: &llm.Usage{TotalTokens: 2}}
	}
	close(ch)
	return ch, nil
}

func newEino(scenario string) (*Runner, error) {
	ctx := context.Background()
	if scenario == "graph_8" || scenario == "branch_loop_8" {
		graph := compose.NewGraph[int, int]()
		nodes := 8
		if scenario == "branch_loop_8" {
			nodes = 1
		}
		prev := compose.START
		for i := 0; i < nodes; i++ {
			name := fmt.Sprintf("n%d", i)
			if err := graph.AddLambdaNode(name, compose.InvokableLambda(func(_ context.Context, n int) (int, error) { return n + 1, nil })); err != nil {
				return nil, err
			}
			if err := graph.AddEdge(prev, name); err != nil {
				return nil, err
			}
			prev = name
		}
		var err error
		if nodes == 1 {
			err = graph.AddBranch(prev, compose.NewGraphBranch(func(_ context.Context, n int) (string, error) {
				if n < 8 {
					return prev, nil
				}
				return compose.END, nil
			}, map[string]bool{prev: true, compose.END: true}))
		} else {
			err = graph.AddEdge(prev, compose.END)
		}
		if err != nil {
			return nil, err
		}
		compiled, err := graph.Compile(ctx, compose.WithMaxRunSteps(32))
		if err != nil {
			return nil, err
		}
		return &Runner{run: func(ctx context.Context) (Outcome, error) {
			n, err := compiled.Invoke(ctx, 0)
			return Outcome{Answer: strconv.Itoa(n)}, err
		}}, nil
	}
	c := &counts{}
	m := &einoModel{counts: c, scenario: scenario}
	cfg := &adk.ChatModelAgentConfig{Name: "bench", Model: m, MaxIterations: 2}
	if scenario == "agent_tool" {
		cfg.ToolsConfig = adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []etool.BaseTool{&einoTool{counts: c}}}}
	}
	agent, err := adk.NewChatModelAgent(ctx, cfg)
	if err != nil {
		return nil, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: scenario == "stream_64"})
	return &Runner{run: func(ctx context.Context) (Outcome, error) {
		c.reset()
		started := time.Now()
		out := Outcome{}
		iter := runner.Query(ctx, Prompt)
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event.Err != nil {
				return out, event.Err
			}
			if event.Output == nil || event.Output.MessageOutput == nil {
				continue
			}
			msg := event.Output.MessageOutput
			if msg.IsStreaming {
				for {
					part, err := msg.MessageStream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						msg.MessageStream.Close()
						return out, err
					}
					if part.Role == schema.Assistant || msg.Role == schema.Assistant {
						if part.Content != "" {
							out.Answer += part.Content
							if out.FirstNS == 0 {
								out.FirstNS = time.Since(started).Nanoseconds()
							}
							out.Chunks++
						}
					}
				}
				msg.MessageStream.Close()
			} else if msg.Message != nil && msg.Message.Role == schema.Assistant && len(msg.Message.ToolCalls) == 0 {
				out.Answer = msg.Message.Content
			}
		}
		out.ModelCalls = c.model.Load()
		out.ToolCalls = c.tool.Load()
		return out, nil
	}}, nil
}

type einoModel struct {
	counts   *counts
	scenario string
}

func (m *einoModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *einoModel) Generate(_ context.Context, msg []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.counts.model.Add(1)
	if m.scenario == "stream_64" {
		return schema.AssistantMessage(strings.Repeat("x", 64), nil), nil
	}
	if len(msg) > 0 && msg[len(msg)-1].Role == schema.Tool {
		last := msg[len(msg)-1]
		if last.Content != "30" || last.ToolCallID != "call-1" {
			return nil, fmt.Errorf("tool result not preserved: %+v", last)
		}
		return &schema.Message{Role: schema.Assistant, Content: last.Content, ResponseMeta: &schema.ResponseMeta{FinishReason: "stop", Usage: &schema.TokenUsage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}}}, nil
	}
	return &schema.Message{Role: schema.Assistant, ResponseMeta: &schema.ResponseMeta{FinishReason: "tool_calls", Usage: &schema.TokenUsage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}}, ToolCalls: []schema.ToolCall{{ID: "call-1", Type: "function", Function: schema.FunctionCall{Name: "lookup", Arguments: `{"order_id":"DEMO-1001"}`}}}}, nil
}
func (m *einoModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.counts.model.Add(1)
	if m.scenario != "stream_64" {
		return nil, fmt.Errorf("unexpected stream")
	}
	parts := make([]*schema.Message, 64)
	for i := range parts {
		parts[i] = schema.AssistantMessage("x", nil)
	}
	parts[63].ResponseMeta = &schema.ResponseMeta{FinishReason: "stop", Usage: &schema.TokenUsage{TotalTokens: 64}}
	return schema.StreamReaderFromArray(parts), nil
}

type einoTool struct{ counts *counts }

func (t *einoTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "lookup", Desc: "Look up refund_days for an order", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"order_id": {Type: schema.String, Required: true}})}, nil
}
func (t *einoTool) InvokableRun(_ context.Context, args string, _ ...etool.Option) (string, error) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(args), &obj); err != nil {
		return "", err
	}
	if err := validateArgs(obj); err != nil {
		return "", err
	}
	t.counts.tool.Add(1)
	return "30", nil
}

// Validate rejects work-skipping adapters before accepting performance samples.
func Validate(scenario string, result Outcome) error {
	want := Outcome{Answer: "8"}
	if scenario == "agent_tool" {
		want = Outcome{Answer: "30", ModelCalls: 2, ToolCalls: 1}
	}
	if scenario == "stream_64" {
		want = Outcome{Answer: strings.Repeat("x", 64), ModelCalls: 1, Chunks: 64}
	}
	if result.Answer != want.Answer || result.ModelCalls != want.ModelCalls || result.ToolCalls != want.ToolCalls || result.Chunks != want.Chunks {
		return fmt.Errorf("contract failure %s: got %+v want %+v", scenario, result, want)
	}
	return nil
}
