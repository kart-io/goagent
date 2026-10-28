package stream

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/panictest"
)

func requireStreamPanic(t *testing.T, err error, value any) {
	t.Helper()
	var p *agentErrors.PanicError
	require.ErrorAs(t, err, &p)
	require.Equal(t, value, p.Value)
}

// panicConsumer 在 panicOn 指定的回调中 panic，其余回调记录调用。
type panicConsumer struct {
	mockConsumer
	panicOn string
	chunkN  atomic.Int32
}

func (p *panicConsumer) OnStart() error {
	if p.panicOn == "start" {
		panic("start boom")
	}
	return nil
}

func (p *panicConsumer) OnChunk(chunk *core.LegacyStreamChunk) error {
	p.chunkN.Add(1)
	if p.panicOn == "chunk" {
		panic("chunk boom")
	}
	return p.mockConsumer.OnChunk(chunk)
}

func (p *panicConsumer) OnError(err error) error {
	if p.panicOn == "finish" || p.panicOn == "error" {
		panic("error boom")
	}
	return p.mockConsumer.OnError(err)
}

func (p *panicConsumer) OnComplete() error {
	if p.panicOn == "finish" || p.panicOn == "complete" {
		panic("complete boom")
	}
	return p.mockConsumer.OnComplete()
}

func textChunks(texts ...string) []*core.LegacyStreamChunk {
	chunks := make([]*core.LegacyStreamChunk, len(texts))
	for i, text := range texts {
		chunks[i] = core.NewTextChunk(text)
	}
	return chunks
}

// 消费者回调在多路复用器的 goroutine 中 panic 时进程不得退出：
// 该消费者不再收到任何回调，其他消费者照常收到全部数据；OnChunk 的 panic 由 Start 返回，
// OnError/OnComplete 的 panic 没有调用方，交给全局 PanicHandler 记录。
func TestMultiplexerConsumerPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &panictest.Recorder{}
		core.SetGlobalMetricsCollector(recorder)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		mux := NewMultiplexer(ctx, nil)
		bad := &panicConsumer{panicOn: "chunk"}
		good := &panicConsumer{}
		// OnError/OnComplete 与 OnChunk 在不同 goroutine 中触发，先后不定，这两个消费者只用于验证 panic 被记录
		for _, c := range []*panicConsumer{bad, good, {panicOn: "error"}, {panicOn: "complete"}} {
			_, err := mux.AddConsumer(c)
			require.NoError(t, err)
		}

		err := mux.Start(ctx, &mockStreamOutput{chunks: textChunks("a", "b", "c")})
		requireStreamPanic(t, err, "chunk boom")
		require.EqualValues(t, 1, bad.chunkN.Load(), "panic 后不再向该消费者投递")
		require.Len(t, good.GetChunks(), 3, "其他消费者照常收到全部数据")
		require.NoError(t, mux.Close())
		require.True(t, good.IsComplete())

		ops := map[string]bool{}
		for _, e := range recorder.Wait(3, 2*time.Second) {
			ops[e.Component+"."+e.Operation] = true
		}
		for _, op := range []string{"stream_multiplexer.consume", "stream_multiplexer.on_error", "stream_multiplexer.on_complete"} {
			require.True(t, ops[op], "PanicHandler 未收到 %s：%v", op, ops)
		}
	})
}

// gatedRecorder 收到第一次 panic 时关闭 panicked，并阻塞到 release 关闭或超时，
// 把发生 panic 的消费者 goroutine 停在 panic 已恢复、后续清理尚未执行的位置。
type gatedRecorder struct {
	panictest.Recorder
	once     sync.Once
	panicked chan struct{}
	release  chan struct{}
}

func (g *gatedRecorder) RecordPanic(ctx context.Context, component, operation string, value any) {
	g.Recorder.RecordPanic(ctx, component, operation, value)
	first := false
	g.once.Do(func() { first = true; close(g.panicked) })
	if first {
		select {
		case <-g.release:
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// gatedSource 产出全部块后，等 gate 关闭才返回结束错误。
type gatedSource struct {
	mockStreamOutput
	gate <-chan struct{}
}

func (s *gatedSource) Next() (*core.LegacyStreamChunk, error) {
	if s.index >= len(s.chunks) {
		<-s.gate
	}
	return s.mockStreamOutput.Next()
}

// 消费者 OnChunk panic 后，即使源恰好在它被清理之前结束，也不得再收到 OnError/OnComplete；
// 其他消费者照常收到全部数据与结束通知。
func TestMultiplexerPanickedConsumerGetsNoFinishCallbacks(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &gatedRecorder{panicked: make(chan struct{}), release: make(chan struct{})}
		core.SetGlobalMetricsCollector(recorder)
		defer close(recorder.release)
		// Start 要等 ctx 结束才返回，这里给一个短超时
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		mux := NewMultiplexer(ctx, nil)
		bad := &panicConsumer{panicOn: "chunk"}
		good := &panicConsumer{}
		_, err := mux.AddConsumer(bad)
		require.NoError(t, err)
		_, err = mux.AddConsumer(good)
		require.NoError(t, err)

		err = mux.Start(ctx, &gatedSource{mockStreamOutput: mockStreamOutput{chunks: textChunks("a")}, gate: recorder.panicked})
		requireStreamPanic(t, err, "chunk boom")
		require.Equal(t, 1, mux.Stats().ActiveConsumers)
		require.NoError(t, mux.Close())

		bad.mu.Lock()
		require.Empty(t, bad.errors, "panic 后不应再收到 OnError")
		require.False(t, bad.complete, "panic 后不应再收到 OnComplete")
		bad.mu.Unlock()
		require.Len(t, good.GetChunks(), 1)
		require.True(t, good.IsComplete())
	})
}

// AddConsumer 在调用方 goroutine 上调用 OnStart：panic 转为 PanicError 返回，消费者不被加入。
func TestMultiplexerOnStartPanicReturnsPanicError(t *testing.T) {
	mux := NewMultiplexer(context.Background(), nil)
	defer mux.Close()
	_, err := mux.AddConsumer(&panicConsumer{panicOn: "start"})
	requireStreamPanic(t, err, "start boom")
	require.Empty(t, mux.Consumers())
}

// StreamManager.Process 在调用方 goroutine 上调用 handler：panic 转为 PanicError 返回，不再调用 OnError/OnComplete。
func TestStreamManagerProcessHandlerPanicReturnsPanicError(t *testing.T) {
	var errorCalls, completeCalls atomic.Int32
	handler := NewFuncStreamHandler(
		func(*StreamChunk) error { panic("handler boom") },
		func() error { completeCalls.Add(1); return nil },
		func(error) error { errorCalls.Add(1); return nil },
	)
	input := make(chan *StreamChunk, 1)
	input <- NewStreamChunk("a")
	close(input)
	err := NewStreamManager(StreamManagerConfig{}).Process(context.Background(), input, handler)
	requireStreamPanic(t, err, "handler boom")
	require.Zero(t, errorCalls.Load())
	require.Zero(t, completeCalls.Load())
}

// StreamManager.Transform/Filter 在 goroutine 中调用宿主函数：panic 作为终止块的错误传出，进程不得退出。
func TestStreamManagerTransformFilterPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		m := NewStreamManager(StreamManagerConfig{})
		for name, run := range map[string]func(<-chan *StreamChunk) <-chan *StreamChunk{
			"transform": func(in <-chan *StreamChunk) <-chan *StreamChunk {
				return m.Transform(context.Background(), in, func(*StreamChunk) (*StreamChunk, error) { panic("transform boom") })
			},
			"filter": func(in <-chan *StreamChunk) <-chan *StreamChunk {
				return m.Filter(context.Background(), in, func(*StreamChunk) bool { panic("filter boom") })
			},
		} {
			input := make(chan *StreamChunk, 1)
			input <- NewStreamChunk("a")
			close(input)
			chunks, err := m.Collect(context.Background(), run(input))
			require.Empty(t, chunks, name)
			requireStreamPanic(t, err, name+" boom")
		}
	})
}

// panicSource 的 Next 在第二次调用时 panic，模拟宿主实现的 StreamOutput。
type panicSource struct{ mockStreamOutput }

func (p *panicSource) Next() (*core.LegacyStreamChunk, error) {
	if p.index > 0 {
		panic("source boom")
	}
	return p.mockStreamOutput.Next()
}

// rawChunks 直接读取 Reader 底层通道：Reader 默认对错误块重试会吞掉错误，这里要看到写出的每个块。
func rawChunks(t *testing.T, out core.StreamOutput) []*core.LegacyStreamChunk {
	t.Helper()
	var chunks []*core.LegacyStreamChunk
	for chunk := range out.(*Reader).ch {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func errorChunk(chunks []*core.LegacyStreamChunk) error {
	for _, chunk := range chunks {
		if chunk.Type == core.ChunkTypeError {
			return chunk.Error
		}
	}
	return nil
}

// 流中间件的 goroutine 中宿主代码（转换函数、过滤条件、上游 StreamOutput）panic 时进程不得退出，
// panic 作为流错误写出，流正常关闭。
func TestStreamMiddlewarePanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &panictest.Recorder{}
		core.SetGlobalMetricsCollector(recorder)
		ctx := context.Background()
		source := func() core.StreamOutput { return &mockStreamOutput{chunks: textChunks("a")} }
		for name, tc := range map[string]struct {
			middleware interface {
				Apply(context.Context, core.StreamOutput) (core.StreamOutput, error)
			}
			source core.StreamOutput
			value  string
		}{
			"transform": {middleware: NewTransformMiddleware(func(*core.LegacyStreamChunk) (*core.LegacyStreamChunk, error) { panic("transform boom") }), source: source(), value: "transform boom"},
			"filter":    {middleware: NewFilterMiddleware(func(*core.LegacyStreamChunk) bool { panic("filter boom") }), source: source(), value: "filter boom"},
			"throttle":  {middleware: NewThrottleMiddleware(1000), source: &panicSource{mockStreamOutput{chunks: textChunks("a", "b")}}, value: "source boom"},
			"batch":     {middleware: NewBatchMiddleware(10, time.Hour), source: &panicSource{mockStreamOutput{chunks: textChunks("a", "b")}}, value: "source boom"},
			"retry":     {middleware: NewRetryMiddleware(0, 0), source: &panicSource{mockStreamOutput{chunks: textChunks("a", "b")}}, value: "source boom"},
		} {
			out, err := tc.middleware.Apply(ctx, tc.source)
			require.NoError(t, err, name)
			chunks := rawChunks(t, out)
			requireStreamPanic(t, errorChunk(chunks), tc.value)
			require.True(t, chunks[len(chunks)-1].IsLast, "%s: 流应正常关闭", name)
		}
	})
}

// TeeMiddleware 在独立 goroutine 中调用消费者 OnChunk：某个消费者 panic 后不再向它投递，
// 其他消费者与主流照常收到全部数据，panic 交给全局 PanicHandler 记录。
func TestTeeMiddlewareConsumerPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &panictest.Recorder{}
		core.SetGlobalMetricsCollector(recorder)
		bad := &panicConsumer{panicOn: "chunk"}
		good := &panicConsumer{}
		out, err := NewTeeMiddleware(bad, good).Apply(context.Background(), &mockStreamOutput{chunks: textChunks("a", "b", "c")})
		require.NoError(t, err)
		main := rawChunks(t, out)
		require.Len(t, main, 4, "主流收到全部数据与结束块")
		require.Nil(t, errorChunk(main))
		events := recorder.Wait(1, 2*time.Second)
		require.NotEmpty(t, events)
		require.Equal(t, "stream_tee", events[0].Component)
		require.Eventually(t, func() bool { return len(good.GetChunks()) == 3 }, 2*time.Second, 5*time.Millisecond)
		require.LessOrEqual(t, bad.chunkN.Load(), int32(3))
	})
}

// MultiModeStream 的 Callback 与 FilterStream/TransformStream 的宿主函数在 goroutine 中 panic 时进程不得退出：
// 没有调用方接收错误，交给全局 PanicHandler 记录；其他模式照常回调。
func TestStreamModesPanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		recorder := &panictest.Recorder{}
		core.SetGlobalMetricsCollector(recorder)
		var updates atomic.Int32
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := NewMultiModeStream(ctx, &StreamConfig{
			Modes:      []StreamMode{StreamModeMessages, StreamModeUpdates},
			BufferSize: 4,
			Callback: func(mode StreamMode, _ StreamEvent) {
				if mode == StreamModeMessages {
					panic("callback boom")
				}
				updates.Add(1)
			},
		})
		require.NoError(t, s.Stream(StreamModeMessages, StreamEvent{}))
		require.NoError(t, s.Stream(StreamModeUpdates, StreamEvent{}))
		require.NoError(t, s.Stream(StreamModeUpdates, StreamEvent{}))
		require.Eventually(t, func() bool { return updates.Load() == 2 }, 2*time.Second, 5*time.Millisecond)

		input := make(chan StreamEvent, 1)
		input <- StreamEvent{}
		close(input)
		for range FilterStream(input, &StreamFilter{Predicate: func(StreamEvent) bool { panic("filter boom") }}) {
		}
		input = make(chan StreamEvent, 1)
		input <- StreamEvent{}
		close(input)
		for range TransformStream(input, func(StreamEvent) StreamEvent { panic("transform boom") }) {
		}

		values := map[any]bool{}
		for _, e := range recorder.Wait(3, 2*time.Second) {
			values[e.Value] = true
		}
		for _, v := range []string{"callback boom", "filter boom", "transform boom"} {
			require.True(t, values[v], "PanicHandler 未收到 %q：%v", v, values)
		}
	})
}

// 数据管道与 StreamingLLMAgent 的处理 goroutine 中宿主函数 panic 时进程不得退出，panic 作为流错误写出。
func TestDataPipelinePanicDoesNotKillProcess(t *testing.T) {
	panictest.Run(t, func(t *testing.T) {
		ctx := context.Background()
		agent := NewDataPipelineAgent(nil)
		out, err := agent.ProcessWithTransform(ctx, []interface{}{1}, func(interface{}) (interface{}, error) { panic("transform boom") })
		require.NoError(t, err)
		requireStreamPanic(t, errorChunk(rawChunks(t, out)), "transform boom")

		out, err = agent.StreamFilter(ctx, &mockStreamOutput{chunks: textChunks("a")}, func(*core.LegacyStreamChunk) bool { panic("filter boom") })
		require.NoError(t, err)
		requireStreamPanic(t, errorChunk(rawChunks(t, out)), "filter boom")

		out, err = agent.StreamMap(ctx, &mockStreamOutput{chunks: textChunks("a")}, func(*core.LegacyStreamChunk) (*core.LegacyStreamChunk, error) { panic("map boom") })
		require.NoError(t, err)
		requireStreamPanic(t, errorChunk(rawChunks(t, out)), "map boom")

		out, err = NewStreamingLLMAgent(panicChatClient{}, nil).ExecuteStream(ctx, &core.AgentInput{Task: "hi"})
		require.NoError(t, err)
		requireStreamPanic(t, errorChunk(rawChunks(t, out)), "chat boom")
	})
}

// panicChatClient 的 Chat panic，模拟宿主提供的 LLM 客户端。
type panicChatClient struct{ llm.Client }

func (panicChatClient) Chat(context.Context, []llm.Message) (*llm.CompletionResponse, error) {
	panic("chat boom")
}

// Writer 的 TransformFunc 在调用方 goroutine 上 panic 时，WriteChunk 返回可识别的 PanicError。
func TestWriterTransformPanicReturnsPanicError(t *testing.T) {
	opts := core.DefaultStreamOptions()
	opts.EnableTransform = true
	opts.TransformFunc = func(*core.LegacyStreamChunk) (*core.LegacyStreamChunk, error) { panic("writer boom") }
	err := NewWriter(context.Background(), opts).WriteChunk(core.NewTextChunk("a"))
	requireStreamPanic(t, err, "writer boom")
	require.False(t, errors.Is(err, context.Canceled))
}
