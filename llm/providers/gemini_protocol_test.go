package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	llm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/utils/json"
	"github.com/stretchr/testify/require"
)

const geminiTextResponse = `{"candidates":[{"content":{"role":"model","parts":[{"text":"private thought","thought":true},{"text":"42"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":19}}`
const geminiToolResponse = `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"order_id":"SHARED-1"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":10,"totalTokenCount":35}}`
const geminiOrderSchema = `{"type":"object","properties":{"order_id":{"type":"string","enum":["SHARED-1"]}},"required":["order_id"],"additionalProperties":false}`

func TestGeminiNativeRequestAndConcurrentIsolation(t *testing.T) {
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
	t.Setenv("GOOGLE_API_KEY", "wrong-key")
	var requests atomic.Int32
	var requestErrors atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
			System struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"systemInstruction"`
			Config struct {
				Max         int      `json:"maxOutputTokens"`
				Temperature float64  `json:"temperature"`
				TopP        float64  `json:"topP"`
				Stop        []string `json:"stopSequences"`
			} `json:"generationConfig"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Contents) != 3 || len(body.System.Parts) != 1 {
			requestErrors.Add(1)
			http.Error(w, "invalid body", 400)
			return
		}
		prompt := body.Contents[2].Parts[0].Text
		if r.Header.Get("x-goog-api-key") != "native-key" || r.URL.RawQuery != "alt=sse" || r.URL.Path != "/v1beta/models/"+prompt+":streamGenerateContent" || body.Contents[1].Role != "model" || body.System.Parts[0].Text != "configured\n\nrequest-system" || body.Config.Max != 123 || body.Config.Temperature != 0.2 || body.Config.TopP != 0.8 || len(body.Config.Stop) != 1 || body.Config.Stop[0] != "end" {
			requestErrors.Add(1)
			http.Error(w, "request mismatch", 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", geminiTextResponse)
	}))
	defer server.Close()
	p, err := NewGeminiWithOptions(llm.WithAPIKey("native-key"), llm.WithBaseURL(server.URL), llm.WithModel("default-model"), llm.WithSystemPrompt("configured"))
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			model := fmt.Sprintf("model-%d", i)
			r, err := llm.Complete(context.Background(), p, &llm.CompletionRequest{Model: model, MaxTokens: 123, Temperature: 0.2, TopP: 0.8, Stop: []string{"end"}, Messages: []llm.Message{llm.SystemMessage("request-system"), llm.UserMessage("first"), llm.AssistantMessage("history"), llm.UserMessage(model)}})
			if err != nil {
				t.Error(err)
				return
			}
			if r.Content != "42" || r.Model != model || r.TokensUsed != 19 {
				t.Errorf("unexpected result: %+v", r)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 12, requests.Load())
	require.Zero(t, requestErrors.Load())
	require.Equal(t, "default-model", p.ModelName())
}

func TestGeminiNativeToolsAndStreams(t *testing.T) {
	var requests atomic.Int32
	var schemas atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		response := geminiTextResponse
		if declarations, ok := body["tools"]; ok {
			data, _ := json.Marshal(declarations)
			if !strings.Contains(string(data), `"additionalProperties":false`) || !strings.Contains(string(data), `"required":["order_id"]`) || !strings.Contains(string(data), `"enum":["SHARED-1"]`) {
				http.Error(w, "lost schema constraints", 400)
				return
			}
			schemas.Add(1)
			response = geminiToolResponse
		}
		if strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
			if r.URL.Query().Get("alt") != "sse" {
				http.Error(w, "missing sse", 400)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", response)
		} else {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, response)
		}
	}))
	defer server.Close()
	p, err := NewGeminiStreamingWithOptions(llm.WithAPIKey("native-key"), llm.WithBaseURL(server.URL))
	require.NoError(t, err)
	tool := tools.NewBaseTool("lookup", "查询订单", geminiOrderSchema, nil)
	check := func(call llm.ToolCall) {
		require.NotEmpty(t, call.ID)
		require.Equal(t, "lookup", call.Function.Name)
		require.JSONEq(t, `{"order_id":"SHARED-1"}`, call.Function.Arguments)
	}
	stream, err := p.CompleteToolsStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("query")}}, []interfaces.Tool{tool})
	require.NoError(t, err)
	var done *llm.StreamChunk
	for c := range stream {
		require.NoError(t, c.Error)
		if c.Done {
			done = c
		}
	}
	require.NotNil(t, done)
	require.Len(t, done.ToolCalls, 1)
	check(done.ToolCalls[0])
	require.Equal(t, 35, done.Usage.TotalTokens)
	events, err := p.StreamWithContext(context.Background(), "query")
	require.NoError(t, err)
	completed := false
	for e := range events {
		require.NoError(t, e.Error)
		if e.Type == "complete" {
			completed = true
			require.Equal(t, 19, e.Metadata["total_tokens"])
		}
	}
	require.True(t, completed)
	require.EqualValues(t, 2, requests.Load())
	require.EqualValues(t, 1, schemas.Load())
}

func TestGeminiNativeErrorsAndMissingFields(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{"unauthorized", `{"error":{"code":401,"message":"invalid key","status":"UNAUTHENTICATED"}}`, 401, true},
		{"quota_no_retries", `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, 429, true},
		{"missing_candidates", `{}`, 200, true},
		{"nil_candidate", `{"candidates":[null]}`, 200, true},
		{"blocked", `{"promptFeedback":{"blockReason":"SAFETY"}}`, 200, true},
		{"truncated", `{"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"MAX_TOKENS"}]}`, 200, true},
		{"missing_usage", `{"candidates":[{"content":{"parts":[{"text":"42"}]},"finishReason":"STOP"}]}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if tc.status != http.StatusOK {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", tc.body)
			}))
			defer server.Close()
			p, err := NewGeminiWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
			require.NoError(t, err)
			r, err := llm.Chat(context.Background(), p, []llm.Message{llm.UserMessage("query")})
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, r)
			} else {
				require.NoError(t, err)
				require.Nil(t, r.Usage)
			}
			require.EqualValues(t, 1, requests.Load())
		})
	}
}

func TestGeminiStreamTruncationAndCancellation(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelRequest), func(t *testing.T) {
			closed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(closed)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}\n\n")
				w.(http.Flusher).Flush()
				if cancelRequest {
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			p, err := NewGeminiStreamingWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			stream, err := p.StreamWithContext(ctx, "query")
			require.NoError(t, err)
			gotError := false
			for e := range stream {
				require.NotEqual(t, "complete", e.Type)
				if e.Type == "token" && cancelRequest {
					cancel()
				}
				if e.Type == "error" {
					gotError = true
				}
			}
			if !cancelRequest {
				require.True(t, gotError)
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("upstream not closed")
			}
		})
	}
}

func TestGeminiRejectsInvalidInputBeforeRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "unexpected", 500) }))
	defer server.Close()
	p, err := NewGeminiWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
	require.NoError(t, err)
	for _, req := range []*llm.CompletionRequest{nil, {}, {Messages: []llm.Message{llm.SystemMessage("system")}}, {Messages: []llm.Message{{Role: "tool", Content: "result"}}}, {Messages: []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call"}}}, llm.UserMessage("next")}}} {
		_, err := llm.Complete(context.Background(), p, req)
		require.Error(t, err)
	}
	_, err = p.CompleteToolsStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("query")}}, []interfaces.Tool{tools.NewBaseTool("lookup", "lookup", `{broken`, nil)})
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = llm.Chat(ctx, p, []llm.Message{llm.UserMessage("query")})
	require.Error(t, err)
	require.Zero(t, calls.Load())
}

func TestGeminiMalformedStreamReturnsErrorInsteadOfPanic(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if partial {
					fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}\n\n")
				}
				fmt.Fprint(w, "data: {\"candidates\":[null]}\n\n")
			}))
			defer server.Close()
			p, err := NewGeminiStreamingWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stream, err := p.StreamWithContext(ctx, "query")
			if !partial {
				require.ErrorContains(t, err, "decode stream response")
				require.Nil(t, stream)
				return
			}
			require.NoError(t, err)
			failed := false
			for event := range stream {
				require.NotEqual(t, "complete", event.Type)
				if event.Type == "error" {
					require.ErrorContains(t, event.Error, "decode stream response")
					failed = true
				}
			}
			require.True(t, failed)
		})
	}
}
