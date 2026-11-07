package llm

import (
	"context"
	"errors"
	shared2 "github.com/kart-io/goagent/constants"
	"io"
	"testing"
	"time"
)

type scriptedStream struct {
	chunks []*StreamChunk
	err    error
}

func (s scriptedStream) CompleteStream(ctx context.Context, _ *CompletionRequest) (<-chan *StreamChunk, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(chan *StreamChunk, len(s.chunks))
	for _, chunk := range s.chunks {
		out <- chunk
	}
	close(out)
	return out, nil
}

func (scriptedStream) Provider() shared2.Provider { return shared2.ProviderCustom }

func TestCompleteCollectsTerminalChunk(t *testing.T) {
	client := scriptedStream{chunks: []*StreamChunk{
		{Delta: "你"},
		{Delta: "好"},
		{Done: true, FinishReason: "stop", ResponseModel: "m-1", Usage: &Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}},
	}}
	resp, err := Complete(context.Background(), client, &CompletionRequest{Model: "requested"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "你好" || resp.Model != "m-1" || resp.FinishReason != "stop" || resp.TokensUsed != 5 || resp.Usage.PromptTokens != 2 || resp.Provider != string(shared2.ProviderCustom) {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCompletePrefersTerminalContentAndRequestModel(t *testing.T) {
	client := scriptedStream{chunks: []*StreamChunk{{Delta: "草稿"}, {Done: true, Content: "定稿", FinishReason: "stop"}}}
	resp, err := Complete(context.Background(), client, &CompletionRequest{Model: "requested"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "定稿" || resp.Model != "requested" || resp.Usage != nil {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCompleteFailures(t *testing.T) {
	sentinel := errors.New("boom")
	for name, tc := range map[string]struct {
		client Client
		want   error
	}{
		"请求失败":   {scriptedStream{err: sentinel}, sentinel},
		"流中错误":   {scriptedStream{chunks: []*StreamChunk{{Delta: "半"}, {Done: true, Error: sentinel}}}, sentinel},
		"无终止块关闭": {scriptedStream{chunks: []*StreamChunk{{Delta: "半"}}}, io.ErrUnexpectedEOF},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Complete(context.Background(), tc.client, &CompletionRequest{}); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := make(chan *StreamChunk)
	if _, err := Complete(ctx, blockingStream(blocked), &CompletionRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

type blockingStream chan *StreamChunk

func (b blockingStream) CompleteStream(context.Context, *CompletionRequest) (<-chan *StreamChunk, error) {
	return b, nil
}

func (blockingStream) Provider() shared2.Provider { return shared2.ProviderCustom }

type completionStreamFunc func(context.Context, *CompletionRequest) (<-chan *StreamChunk, error)

func (f completionStreamFunc) CompleteStream(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error) {
	return f(ctx, req)
}

func (completionStreamFunc) Provider() shared2.Provider { return shared2.ProviderCustom }

func TestCompleteDoesNotRequestWithFinishedContext(t *testing.T) {
	for _, mode := range []string{"取消", "超时"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if mode == "超时" {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(1, 0))
			} else {
				cancel()
			}
			defer cancel()
			calls := 0
			client := completionStreamFunc(func(context.Context, *CompletionRequest) (<-chan *StreamChunk, error) {
				calls++
				return nil, nil
			})
			response, err := Complete(ctx, client, &CompletionRequest{})
			if !errors.Is(err, ctx.Err()) || response != nil || calls != 0 {
				t.Fatalf("已结束的请求不应调用提供方: response=%+v, err=%v, calls=%d", response, err, calls)
			}
		})
	}
}

func TestCompleteCancellationCannotBecomeEOFOrSuccess(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		for attempt := range 128 {
			ctx, cancel := context.WithCancel(context.Background())
			client := completionStreamFunc(func(context.Context, *CompletionRequest) (<-chan *StreamChunk, error) {
				out := make(chan *StreamChunk, 1)
				cancel()
				if terminal {
					out <- &StreamChunk{Done: true, FinishReason: "stop", Content: "完成"}
				}
				close(out)
				return out, nil
			})
			response, err := Complete(ctx, client, &CompletionRequest{})
			cancel()
			if !errors.Is(err, context.Canceled) || response != nil {
				t.Fatalf("终态=%v，第%d次：取消不能变成成功或断流: response=%+v, err=%v", terminal, attempt, response, err)
			}
		}
	}
}

func TestCompleteReportsCancellationWhenActiveStreamCloses(t *testing.T) {
	for attempt := range 128 {
		ctx, cancel := context.WithCancel(context.Background())
		client := completionStreamFunc(func(context.Context, *CompletionRequest) (<-chan *StreamChunk, error) {
			out := make(chan *StreamChunk)
			go func() {
				// 先让消费者进入读流，再在同一个 goroutine 中先取消、后关闭。
				out <- nil
				cancel()
				close(out)
			}()
			return out, nil
		})
		response, err := Complete(ctx, client, &CompletionRequest{})
		cancel()
		if !errors.Is(err, context.Canceled) || response != nil {
			t.Fatalf("第%d次：取消后的关闭必须报告取消: response=%+v, err=%v", attempt, response, err)
		}
	}
}

func TestCompleteRejectsNilStreamWithoutWaitingForDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	client := completionStreamFunc(func(context.Context, *CompletionRequest) (<-chan *StreamChunk, error) {
		return nil, nil
	})
	response, err := Complete(ctx, client, &CompletionRequest{})
	if err == nil || errors.Is(err, context.DeadlineExceeded) || response != nil {
		t.Fatalf("提供方返回空流应直接失败，不能挂起到宿主期限: response=%+v, err=%v", response, err)
	}
}

func TestCompleteCancellationTakesPrecedenceOverNilStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := completionStreamFunc(func(context.Context, *CompletionRequest) (<-chan *StreamChunk, error) {
		cancel()
		return nil, nil
	})
	response, err := Complete(ctx, client, &CompletionRequest{})
	if !errors.Is(err, context.Canceled) || response != nil {
		t.Fatalf("取消不能被提供方的空流掩盖: response=%+v, err=%v", response, err)
	}
}
