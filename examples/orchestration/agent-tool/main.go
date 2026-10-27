// Stateful child agents require host-owned session identities and resource cleanup.
package main

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/tools"
	"sync"
	"sync/atomic"
)

type host struct {
	mu            sync.Mutex
	sessions      map[string]bool
	memory        interfaces.MemoryManager
	calls, closed atomic.Int64
}

func newHost() *host {
	return &host{sessions: map[string]bool{}, memory: memory.NewInMemoryManager(nil)}
}
func (h *host) tool(nextID func() string) (interfaces.Tool, error) {
	return tools.NewRunnableToolFactory(func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		id := nextID()
		h.mu.Lock()
		used := h.sessions[id]
		h.sessions[id] = true
		h.mu.Unlock()
		if used || id == "" {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, fmt.Errorf("host reused child SessionID")
		}
		model := &demomodel.Model{Calls: &h.calls, Respond: func(*llm.CompletionRequest) llm.StreamChunk { return llm.StreamChunk{Content: "政策允许"} }}
		child, err := builder.NewSimpleBuilder(model).WithSystemPrompt("独立政策助手").WithMemory(h.memory).WithSessionID(id).Build()
		if err != nil {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
		}
		return tools.OwnedRunnable[any, *builder.AgentOutput]{Runnable: child.AsRunnable(), Close: func(context.Context) error { h.closed.Add(1); return nil }}, nil
	}, tools.RunnableToolConfig[any, *builder.AgentOutput]{Name: "policy", Version: "v1", Description: "Assess policy", ArgsSchema: `{"type":"object","properties":{"question":{"type":"string"}},"required":["question"],"additionalProperties":false}`, ModelInvoking: true, Decode: func(args map[string]any) (any, error) {
		s, ok := args["question"].(string)
		if !ok {
			return nil, fmt.Errorf("question required")
		}
		return s, nil
	}, Encode: func(out *builder.AgentOutput) (any, error) {
		if out == nil {
			return nil, fmt.Errorf("missing result")
		}
		return out.Result, nil
	}})
}
func main() {
	h := newHost()
	tool, err := h.tool(uuid.NewString)
	if err != nil {
		panic(err)
	}
	c, err := budget.New(budget.Config{RootKey: "demo", Version: "v1", MaxModelRequests: 2})
	if err != nil {
		panic(err)
	}
	out, err := tool.Invoke(budget.WithController(context.Background(), c), &interfaces.ToolInput{Args: map[string]any{"question": "能否退款"}})
	if err != nil {
		panic(err)
	}
	fmt.Println(out.Result)
}
