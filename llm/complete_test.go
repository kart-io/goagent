package llm

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/kart-io/goagent/llm/constants"
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

func (scriptedStream) Provider() constants.Provider { return constants.ProviderCustom }

func TestCompleteCollectsTerminalChunk(t *testing.T) {
	client := scriptedStream{chunks: []*StreamChunk{
		{Delta: "你"}, {Delta: "好"},
		{Done: true, FinishReason: "stop", ResponseModel: "m-1", Usage: &Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}},
	}}
	resp, err := Complete(context.Background(), client, &CompletionRequest{Model: "requested"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "你好" || resp.Model != "m-1" || resp.FinishReason != "stop" || resp.TokensUsed != 5 || resp.Usage.PromptTokens != 2 || resp.Provider != string(constants.ProviderCustom) {
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

func (blockingStream) Provider() constants.Provider { return constants.ProviderCustom }
