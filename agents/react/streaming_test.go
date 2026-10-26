package react_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/agents/react"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

type streamCallbacks struct {
	core.BaseCallback
	starts, finishes atomic.Int32
	finishErr        error
}

func (c *streamCallbacks) OnStart(context.Context, interface{}) error { c.starts.Add(1); return nil }
func (c *streamCallbacks) OnAgentFinish(context.Context, interface{}) error {
	c.finishes.Add(1)
	return c.finishErr
}

func TestStreamEmitsBeforeToolCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	release := make(chan struct{})
	tool := tools.NewBaseTool("lookup", "lookup", `{}`, func(ctx context.Context, _ *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		select {
		case <-release:
			return &interfaces.ToolOutput{Result: "evidence", Success: true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	responses := []string{"Thought: Look up evidence\nAction: lookup\nAction Input: {}", "Final Answer: supported answer"}
	callback := &streamCallbacks{}
	agent := react.NewReActAgent(react.ReActConfig{LLM: NewMockLLMClient(responses), Tools: []interfaces.Tool{tool}}).
		WithCallbacks(callback)
	chunks, err := agent.Stream(ctx, &core.AgentInput{Task: "question"})
	require.NoError(t, err)
	var first core.StreamChunk[*core.AgentOutput]
	select {
	case first = <-chunks:
	case <-ctx.Done():
		t.Fatal("stream did not emit before the tool completed")
	}
	require.False(t, first.Done)
	require.Equal(t, "thought", first.Data.Metadata["step_type"])
	require.Len(t, first.Data.Steps, 1)
	close(release)
	var terminal *core.AgentOutput
	terminalCount := 0
	for chunk := range chunks {
		require.NoError(t, chunk.Error)
		if chunk.Done {
			terminalCount++
			terminal = chunk.Data
		} else {
			require.Equal(t, "action", chunk.Data.Metadata["step_type"])
		}
	}
	require.Equal(t, 1, terminalCount)
	require.Len(t, first.Data.Steps, 1, "earlier snapshots must remain stable")
	require.Empty(t, first.Data.ToolCalls)
	require.EqualValues(t, 1, callback.starts.Load())
	require.EqualValues(t, 1, callback.finishes.Load())

	syncAgent := react.NewReActAgent(react.ReActConfig{LLM: NewMockLLMClient(responses), Tools: []interfaces.Tool{tool}})
	want, err := syncAgent.Invoke(ctx, &core.AgentInput{Task: "question"})
	require.NoError(t, err)
	require.Equal(t, want.Result, terminal.Result)
	require.Equal(t, want.Status, terminal.Status)
	require.Equal(t, len(want.Steps), len(terminal.Steps))
	require.Equal(t, want.ToolCalls[0].Output, terminal.ToolCalls[0].Output)
}

func TestStreamCancellationStopsBlockedSendBeforeTool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	modelCalled := make(chan struct{})
	client := &notifyingModel{MockLLMClient: NewMockLLMClient([]string{"Thought: lookup\nAction: lookup\nAction Input: {}"}), called: modelCalled}
	var toolCalls atomic.Int32
	tool := tools.NewBaseTool("lookup", "lookup", `{}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		toolCalls.Add(1)
		return &interfaces.ToolOutput{Success: true}, nil
	})
	agent := react.NewReActAgent(react.ReActConfig{LLM: client, Tools: []interfaces.Tool{tool}})
	chunks, err := agent.Stream(ctx, &core.AgentInput{Task: "question"})
	require.NoError(t, err)
	select {
	case <-modelCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("model was not called")
	}
	// Leave the unbuffered stream unread until cancellation.
	cancel()
	drained := make(chan struct{})
	go func() {
		for range chunks {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(3 * time.Second):
		t.Fatal("producer remained blocked after cancellation")
	}
	require.Zero(t, toolCalls.Load())
}

type notifyingModel struct {
	*MockLLMClient
	called chan struct{}
}

func (m *notifyingModel) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	defer close(m.called)
	return m.MockLLMClient.Chat(ctx, messages)
}

func TestStreamTerminalPartialAndCallbackError(t *testing.T) {
	for _, callbackError := range []bool{false, true} {
		sentinel := errors.New("finish callback failed")
		callback := &streamCallbacks{}
		if callbackError {
			callback.finishErr = sentinel
		}
		agent := react.NewReActAgent(react.ReActConfig{
			LLM: NewMockLLMClient([]string{"Thought: lookup\nAction: missing\nAction Input: {}"}), MaxSteps: 1,
		}).WithCallbacks(callback)
		chunks, err := agent.Stream(context.Background(), &core.AgentInput{Task: "question"})
		require.NoError(t, err)
		count := 0
		for chunk := range chunks {
			if !chunk.Done {
				continue
			}
			count++
			require.Equal(t, interfaces.StatusPartial, chunk.Data.Status)
			if callbackError {
				require.ErrorIs(t, chunk.Error, sentinel)
			} else {
				require.NoError(t, chunk.Error)
			}
		}
		require.Equal(t, 1, count)
	}
}
