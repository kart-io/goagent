package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
)

func imageRefServer(t *testing.T) (*httptest.Server, *int32, chan string) {
	t.Helper()
	var requests int32
	bodies := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		body, _ := io.ReadAll(r.Body)
		bodies <- string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server, &requests, bodies
}

func imageRefRequest(part agentllm.ImagePart) *agentllm.CompletionRequest {
	return &agentllm.CompletionRequest{Model: "m", Messages: []agentllm.Message{{Role: "user", Content: "look", Images: []agentllm.ImagePart{part}}}}
}

func TestOpenAIStreamRejectsUnresolvedImageRef(t *testing.T) {
	server, requests, _ := imageRefServer(t)
	client, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL+"/v1"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// 空 URL 一律不出站：未解析的引用与全空部件同样拒绝。
	for _, part := range []agentllm.ImagePart{{Ref: "image:1"}, {}} {
		req := imageRefRequest(part)
		_, err = client.CompleteStream(ctx, req)
		require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err), "%+v", part)
		_, err = client.CompleteToolsStream(ctx, req, nil)
		require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err), "%+v", part)
	}
	require.Zero(t, atomic.LoadInt32(requests))
}

func TestOpenAIStreamSendsResolvedImageURL(t *testing.T) {
	server, requests, bodies := imageRefServer(t)
	client, err := NewOpenAIWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL+"/v1"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	chunks, err := client.CompleteStream(ctx, imageRefRequest(agentllm.ImagePart{URL: "data:image/png;base64,AA", Ref: "image:1", MIME: "image/png", SHA256: "abc"}))
	require.NoError(t, err)
	for range chunks {
	}
	require.EqualValues(t, 1, atomic.LoadInt32(requests))
	body := <-bodies
	require.Contains(t, body, `"url":"data:image/png;base64,AA"`)
	require.False(t, strings.Contains(body, "image:1"), body)
}

func TestDeepSeekRejectsImages(t *testing.T) {
	server, requests, _ := imageRefServer(t)
	client, err := NewDeepSeekWithOptions(agentllm.WithAPIKey("test"), agentllm.WithBaseURL(server.URL))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, part := range []agentllm.ImagePart{{Ref: "image:1"}, {URL: "data:image/png;base64,AA"}} {
		req := imageRefRequest(part)
		_, err = client.CompleteStream(ctx, req)
		require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
		_, err = client.CompleteToolsStream(ctx, req, nil)
		require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
	}
	require.Zero(t, atomic.LoadInt32(requests))
}
