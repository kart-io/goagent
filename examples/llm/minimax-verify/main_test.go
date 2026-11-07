package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/testing/mocks"
	"github.com/stretchr/testify/require"
)

func TestVerifyCorrectsOnceAndReplays(t *testing.T) {
	model := mocks.NewScriptedClient(
		mocks.ToolCallReply("工具", mocks.ToolCall("c1", "verify", `{"id":"9007199254740993","rate":"0.1234567890123456789","label":"<核验&>"}`)),
		mocks.TextReply("错误格式", `{"receipt":"minimax-probe-ok"}`),
		mocks.TextReply("纠正", "minimax-probe-ok"),
	)
	got, err := verify(context.Background(), model)
	require.NoError(t, err)
	require.Equal(t, 3, got.Requests)
	require.Equal(t, 1, got.Effects)
	require.Zero(t, got.ReplayRequests)
	require.Zero(t, got.ReplayEffects)
}

func TestVerifyRejectsRoundedArgument(t *testing.T) {
	model := mocks.NewScriptedClient(mocks.ToolCallReply("错误参数", mocks.ToolCall("c1", "verify", `{"id":"9007199254740993","rate":"0.12345678901234568","label":"<核验&>"}`)))
	got, err := verify(context.Background(), model)
	require.Error(t, err)
	require.Zero(t, got.Effects)
	require.LessOrEqual(t, got.Requests, 3)
}

func TestVerifyRejectsReceiptWithoutTool(t *testing.T) {
	got, err := verify(context.Background(), mocks.NewScriptedClient(mocks.TextReply("伪造回执", "minimax-probe-ok")))
	require.Error(t, err)
	require.Zero(t, got.Effects)
}

func TestVerifyFailsAfterOneCorrection(t *testing.T) {
	model := mocks.NewScriptedClient(
		mocks.ToolCallReply("工具", mocks.ToolCall("c1", "verify", `{"id":"9007199254740993","rate":"0.1234567890123456789","label":"<核验&>"}`)),
		mocks.TextReply("格式错误", "错误"), mocks.TextReply("再次错误", "错误"),
	)
	got, err := verify(context.Background(), model)
	require.Error(t, err)
	require.Equal(t, 3, got.Requests)
	require.Equal(t, 1, got.Effects)
}

func TestRunRequiresExplicitLiveAndDedicatedCredential(t *testing.T) {
	var output bytes.Buffer
	require.Error(t, run(context.Background(), nil, &output))
	t.Setenv("MINIMAX_CN_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "other-provider-key")
	require.ErrorContains(t, run(context.Background(), []string{"-live"}, &output), "MINIMAX_CN_API_KEY")
	require.Empty(t, output.String())
}

func TestVerifyUsesAnthropicWireAndPreservesNativeState(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/anthropic/v1/messages", r.URL.Path)
		require.Equal(t, "Bearer minimax-test", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.Decode(r.Body, &body))
		require.Equal(t, "MiniMax-M3.1-Flash", body["model"])
		require.Equal(t, true, body["stream"])
		n := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		event := func(name string, value any) {
			data, err := json.Marshal(value)
			require.NoError(t, err)
			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
			require.NoError(t, err)
		}
		event("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg", "type": "message", "role": "assistant", "model": "MiniMax-M3.1-Flash", "content": []any{}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 1}}})
		if n == 1 {
			event("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "native-signature"}})
			event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
			event("content_block_start", map[string]any{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "tool_use", "id": "call-1", "name": "verify", "input": map[string]any{}}})
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"id":"9007199254740993","rate":"0.1234567890123456789","label":"<核验&>"}`}})
			event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 1})
		} else {
			require.EqualValues(t, 2, n, "不得在重放时新增HTTP请求")
			messages := body["messages"].([]any)
			assistant := messages[len(messages)-2].(map[string]any)
			blocks := assistant["content"].([]any)
			require.Equal(t, "thinking", blocks[0].(map[string]any)["type"])
			require.Equal(t, "native-signature", blocks[0].(map[string]any)["signature"])
			event("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "minimax-probe-ok"}})
			event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		}
		reason := "end_turn"
		if n == 1 {
			reason = "tool_use"
		}
		event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason}, "usage": map[string]int{"output_tokens": 5}})
		event("message_stop", map[string]any{"type": "message_stop"})
	}))
	defer server.Close()
	client, err := providers.NewAnthropicWithOptions(llm.WithAPIKey("minimax-test"), llm.WithBaseURL(server.URL+"/anthropic"), llm.WithModel("MiniMax-M3.1-Flash"), llm.WithCustomHeaders(map[string]string{"Authorization": "Bearer minimax-test"}))
	require.NoError(t, err)
	got, err := verify(context.Background(), client)
	require.NoError(t, err)
	require.Equal(t, 2, got.Requests)
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, 1, got.Effects)
	require.Zero(t, got.ReplayRequests)
}
