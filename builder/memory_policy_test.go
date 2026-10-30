package builder

import (
	"context"
	"encoding/json"
	"errors"
	agentErrors "github.com/kart-io/goagent/errors"
	"io"
	"os"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

type failingMemory struct {
	interfaces.MemoryManager
	readErr, writeErr error
}

func (m *failingMemory) GetConversationHistory(context.Context, string, int) ([]*interfaces.Conversation, error) {
	return nil, m.readErr
}
func (m *failingMemory) AddConversation(context.Context, *interfaces.Conversation) error {
	return m.writeErr
}

type failingCheckpoint struct {
	checkpoint.Checkpointer
	existsErr, loadErr error
	state              core.State
}

func (c *failingCheckpoint) Exists(context.Context, string) (bool, error) { return true, c.existsErr }
func (c *failingCheckpoint) Load(context.Context, string) (core.State, error) {
	return c.state, c.loadErr
}

type otherState struct{ *core.AgentState }

func TestInitializeRejectsCheckpointFailureWithoutMutation(t *testing.T) {
	sentinel := errors.New("storage unavailable")
	for _, tc := range []struct {
		name  string
		cp    *failingCheckpoint
		cause error
	}{
		{"exists", &failingCheckpoint{existsErr: sentinel}, sentinel},
		{"load", &failingCheckpoint{loadErr: sentinel}, sentinel},
		{"type", &failingCheckpoint{state: &otherState{core.NewAgentState()}}, nil},
		{"nil", &failingCheckpoint{}, nil},
		{"typed_nil", &failingCheckpoint{state: (*core.AgentState)(nil)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewSimpleBuilder(NewMockLLMClient()).Build()
			require.NoError(t, err)
			previous := a.runtime.State
			cb := NewMockCallback()
			a.callbacks = []core.Callback{cb}
			a.runtime.Checkpointer = tc.cp
			err = a.Initialize(context.Background())
			require.Error(t, err)
			var panicErr *agentErrors.PanicError
			require.False(t, errors.As(err, &panicErr))
			if tc.cause != nil {
				require.ErrorIs(t, err, tc.cause)
			}
			require.Same(t, previous, a.runtime.State)
			require.False(t, cb.onStartCalled)
		})
	}
}

type memoryEvents struct{ events []observe.Event }

func (s *memoryEvents) Observe(_ context.Context, e observe.Event) { s.events = append(s.events, e) }

// 各入口必须在严格模式下拒绝成功终态，并保留存储错误链。
func TestMemoryFailurePoliciesAcrossEntries(t *testing.T) {
	for _, entry := range []string{"execute", "legacy", "plain_stream", "native_stream", "runnable"} {
		for _, operation := range []string{"read", "write"} {
			for _, strict := range []bool{false, true} {
				t.Run(entry+"/"+operation+"/strict="+map[bool]string{true: "true", false: "false"}[strict], func(t *testing.T) {
					sentinel := errors.New("private error containing conversation secret")
					mem := &failingMemory{}
					if operation == "read" {
						mem.readErr = sentinel
					} else {
						mem.writeErr = sentinel
					}
					var client llm.Client = NewMockLLMClient("answer")
					if entry == "legacy" {
						client = &evidenceLLM{MockLLMClient: NewMockLLMClient()}
					}
					if entry == "plain_stream" || entry == "runnable" {
						client = &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "answer"}, {Done: true, FinishReason: "stop"}}}
					}
					if entry == "native_stream" {
						client = nativeModel([]*llm.StreamChunk{{Delta: "answer"}, {Done: true, FinishReason: "stop"}})
					}
					b := NewSimpleBuilder(client).WithMemory(mem)
					if strict {
						b.WithMemoryFailurePolicy(MemoryFailureStrict)
					}
					if entry == "legacy" || entry == "native_stream" {
						b.WithTools(NewMockTool("lookup", "found"))
					}
					a, err := b.Build()
					require.NoError(t, err)
					sink := &memoryEvents{}
					ctx := observe.WithObserver(context.Background(), sink)
					var output *AgentOutput
					switch entry {
					case "execute":
						output, err = a.Execute(ctx, "private input")
					case "legacy":
						output, err = a.ExecuteWithTools(ctx, "private input")
					case "runnable":
						output, err = a.AsRunnable().Invoke(ctx, "private input")
					default:
						var events <-chan AgentEvent
						events, err = a.ExecuteStream(ctx, "private input")
						require.NoError(t, err)
						for event := range events {
							if event.Type == "error" {
								err = event.Error
							}
							if event.Type == "done" {
								output = event.Output
							}
						}
					}
					if strict {
						require.ErrorIs(t, err, sentinel)
						require.Nil(t, output)
					} else {
						require.NoError(t, err)
						require.NotNil(t, output)
						found := false
						for _, event := range sink.events {
							if event.Type == "memory_"+operation+"_error" {
								found = true
								require.NotEmpty(t, event.ErrorKind)
							}
						}
						require.True(t, found, "缺少记忆错误观察事件")
						encoded, err := json.Marshal(sink.events)
						require.NoError(t, err)
						require.NotContains(t, string(encoded), "private")
					}
				})
			}
		}
	}
}
func TestMemoryFailurePolicyRejectsUnknownValue(t *testing.T) {
	_, err := NewSimpleBuilder(NewMockLLMClient()).WithMemoryFailurePolicy(MemoryFailurePolicy("invalid")).Build()
	require.Error(t, err)
}

// Build 同样必须保留初始化存储错误，不能触发初始化成功回调。
func TestBuildPreservesCheckpointError(t *testing.T) {
	sentinel := errors.New("checkpoint unavailable")
	cb := NewMockCallback()
	_, err := NewSimpleBuilder(NewMockLLMClient()).WithCheckpointer(&failingCheckpoint{existsErr: sentinel}).WithCallbacks(cb).Build()
	require.ErrorIs(t, err, sentinel)
	require.False(t, cb.onStartCalled)
}

func TestBestEffortMemoryDoesNotPrintPrivateError(t *testing.T) {
	previous := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = previous; _ = r.Close(); _ = w.Close() }()
	a, err := NewSimpleBuilder(NewMockLLMClient()).WithMemory(&failingMemory{readErr: errors.New("private read"), writeErr: errors.New("private write")}).Build()
	require.NoError(t, err)
	_, err = a.Execute(context.Background(), "private input")
	require.NoError(t, err)
	require.NoError(t, w.Close())
	captured, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Empty(t, string(captured))
}
