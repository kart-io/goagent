package builder

import (
	"context"
	"sync"
	"testing"

	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

// orderedSink 把观测事件与调用方的标记记在同一序列里，用来检查先后顺序。
type orderedSink struct {
	mu  sync.Mutex
	seq []string
}

func (s *orderedSink) Observe(_ context.Context, e observe.Event) { s.add(e.Type) }
func (s *orderedSink) add(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq = append(s.seq, v)
}

// 调用方收到终态事件时，agent 的结束观测必须已经发出；否则宿主在 Invoke 返回后
// 立即关闭记录器会丢失 agent_end，OTel 中 agent span 也会晚于父节点结束。
func TestAgentObservationEndsBeforeTerminalEvent(t *testing.T) {
	for i := 0; i < 300; i++ {
		model := &eventModel{MockLLMClient: NewMockLLMClient(), chunks: []*llm.StreamChunk{{Delta: "ok"}, {Done: true, FinishReason: "stop"}}}
		agent, err := NewSimpleBuilder(model).Build()
		require.NoError(t, err)
		sink := &orderedSink{}
		events, err := agent.ExecuteStream(observe.WithObserver(context.Background(), sink), "q")
		require.NoError(t, err)
		for e := range events {
			if e.Type == "done" || e.Type == "error" {
				sink.add("terminal-received")
			}
		}
		end, received := -1, -1
		for j, v := range sink.seq {
			switch v {
			case "agent_end":
				end = j
			case "terminal-received":
				received = j
			}
		}
		require.GreaterOrEqual(t, end, 0, "第 %d 次缺少 agent_end", i)
		require.Less(t, end, received, "第 %d 次：调用方先于 agent_end 收到终态：%v", i, sink.seq)
	}
}
