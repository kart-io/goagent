package providers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/utils/json"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestGeminiAsRunnableToolContract(t *testing.T) {
	for _, mode := range []string{"loop", "direct", "parallel", "sequential", "truncated", "duplicate", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			var requests, toolRuns atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				var req struct {
					Contents []*genai.Content `json:"contents"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "invalid", 400)
					return
				}
				// Every model/tool round must retain signed parts and match response order.
				for i, content := range req.Contents {
					if content.Role != "model" {
						continue
					}
					if len(content.Parts) < 3 || string(content.Parts[0].ThoughtSignature) != "empty-signed" || !content.Parts[1].Thought || string(content.Parts[2].ThoughtSignature) != "call-signed" || i+1 >= len(req.Contents) {
						http.Error(w, "signatures lost", 400)
						return
					}
					next := req.Contents[i+1]
					if next.Role != "user" || len(next.Parts) != len(content.Parts)-2 {
						http.Error(w, "wrong result batch", 400)
						return
					}
					for j, part := range next.Parts {
						original := content.Parts[j+2].FunctionCall
						f := part.FunctionResponse
						if f == nil || f.ID != original.ID || f.Name != original.Name || f.Response["output"] != "42" {
							http.Error(w, "tool response mismatch", 400)
							return
						}
					}
				}
				response := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "42"}}}, FinishReason: genai.FinishReasonStop}}, UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 4, CandidatesTokenCount: 2, TotalTokenCount: 6}}
				if n == 1 || (mode == "sequential" && n == 2) {
					parts := []*genai.Part{{ThoughtSignature: []byte("empty-signed")}, {Thought: true, Text: "private reasoning"}, {FunctionCall: &genai.FunctionCall{Name: "lookup", Args: map[string]any{"order_id": "A"}}, ThoughtSignature: []byte("call-signed")}}
					if mode == "parallel" {
						parts[2].FunctionCall.ID = "first"
						parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: "second", Name: "lookup", Args: map[string]any{"order_id": "B"}}})
					}
					if mode == "duplicate" {
						parts[2].FunctionCall.ID = "same"
						parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: "same", Name: "lookup", Args: map[string]any{}}})
					}
					if mode == "unknown" {
						parts[2].FunctionCall.Name = "not_offered"
					}
					response.Candidates[0].Content.Parts = parts
					if mode == "truncated" {
						response.Candidates[0].FinishReason = ""
					}
				}
				data, _ := json.Marshal(response)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", data)
			}))
			defer server.Close()
			p, err := NewGeminiWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
			require.NoError(t, err)
			tool := tools.NewBaseTool("lookup", "lookup", `{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"]}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
				toolRuns.Add(1)
				return &interfaces.ToolOutput{Success: true, Result: "42"}, nil
			})
			b := builder.NewSimpleBuilder(p).WithTools(tool).WithMaxIterations(3)
			if mode == "direct" {
				b.WithReturnDirectTools("lookup")
			}
			a, err := b.Build()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			out, err := a.AsRunnable().Invoke(ctx, "query")
			if mode == "truncated" || mode == "duplicate" || mode == "unknown" {
				require.Error(t, err)
				require.Zero(t, toolRuns.Load())
				return
			}
			require.NoError(t, err)
			require.Equal(t, "42", out.Result)
			expectedCalls, expectedTools := int32(2), int32(1)
			if mode == "direct" {
				expectedCalls = 1
			}
			if mode == "parallel" {
				expectedTools = 2
			}
			if mode == "sequential" {
				expectedCalls = 3
				expectedTools = 2
			}
			require.Equal(t, expectedCalls, requests.Load())
			require.Equal(t, expectedTools, toolRuns.Load())
			require.Equal(t, int(expectedCalls)*6, out.TokenUsage.TotalTokens)
		})
	}
}

func TestGeminiNativeStateRoundTripAndMismatch(t *testing.T) {
	response := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "checking"}, {FunctionCall: &genai.FunctionCall{Name: "lookup", ID: "native", Args: map[string]any{"id": "A"}}, ThoughtSignature: []byte("signed")}}}, FinishReason: genai.FinishReasonStop}}}
	chunk, err := decodeGeminiResponse(response)
	require.NoError(t, err)
	original := llm.Message{Role: "assistant", Content: chunk.Content, ToolCalls: chunk.ToolCalls, ProviderState: chunk.ProviderState}
	data, err := json.Marshal(original)
	require.NoError(t, err)
	var copied llm.Message
	require.NoError(t, json.Unmarshal(data, &copied))
	messages := []llm.Message{llm.UserMessage("query"), copied, {Role: "tool", ToolCallID: "native", Content: `{"real":"tool data"}`}}
	contents, _, err := geminiMessages(messages, "")
	require.NoError(t, err)
	require.Equal(t, []byte("signed"), contents[1].Parts[1].ThoughtSignature)
	require.Equal(t, `{"real":"tool data"}`, contents[2].Parts[0].FunctionResponse.Response["output"])
	for _, change := range []func([]llm.Message){
		func(m []llm.Message) { m[1].Content = "tampered" },
		func(m []llm.Message) { m[2].ToolCallID = "wrong" },
		func(m []llm.Message) {
			m[1].ProviderState = &llm.ProviderState{Provider: constants.ProviderOpenAI, Data: original.ProviderState.Data}
		},
		func(m []llm.Message) { m[1].ToolCalls = []llm.ToolCall{{ID: "native", Type: "function"}} },
	} {
		bad := append([]llm.Message(nil), messages...)
		change(bad)
		_, _, err := geminiMessages(bad, "")
		require.Error(t, err)
	}
	_, _, err = geminiMessages(messages[:2], "")
	require.Error(t, err)
	_, _, err = geminiMessages(append(messages, messages[2]), "")
	require.Error(t, err)
}

func TestGeminiEmbeddingRealProtocol(t *testing.T) {
	t.Setenv(constants.EnvGeminiEmbeddingModel, "env-embed")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if !strings.Contains(r.URL.Path, "custom-embed") || r.Header.Get("x-goog-api-key") != "test" {
			http.Error(w, "wrong configuration", 400)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad json", 400)
			return
		}
		data, _ := json.Marshal(body)
		if !strings.Contains(string(data), "sample input") {
			http.Error(w, "missing input", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"embeddings":[{"values":[0.25,-0.5,0.75]}]}`)
	}))
	defer server.Close()
	p, err := NewGeminiWithOptions(common.ConfigToOptions(&llm.LLMOptions{APIKey: "test", BaseURL: server.URL, Model: "chat-model", EmbeddingModel: "custom-embed"})...)
	require.NoError(t, err)
	require.Equal(t, "chat-model", p.ModelName())
	require.True(t, llm.HasEmbedding(p))
	require.NotNil(t, llm.AsEmbedder(p))
	vector, err := p.Embed(context.Background(), "sample input")
	require.NoError(t, err)
	require.Equal(t, []float64{.25, -.5, .75}, vector)
	_, err = p.Embed(context.Background(), " ")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Embed(ctx, "sample input")
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 1, requests.Load())
	env, err := NewGeminiWithOptions(llm.WithAPIKey("test"))
	require.NoError(t, err)
	require.Equal(t, "env-embed", env.Config.EmbeddingModel)
}

func TestGeminiEmbeddingInvalidResponses(t *testing.T) {
	for _, body := range []string{`{}`, `{"embeddings":[]}`, `{"embeddings":[null]}`, `{"embeddings":[{"values":[]}]}`, `{"embeddings":[{"values":[0,0]}]}`, `{"embeddings":[{"values":[1e100]}]}`, `{"embeddings":[{"values":[1]},{"values":[2]}]}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			p, err := NewGeminiWithOptions(llm.WithAPIKey("test"), llm.WithBaseURL(server.URL))
			require.NoError(t, err)
			vector, err := p.Embed(context.Background(), "sample")
			require.Error(t, err)
			require.Nil(t, vector)
		})
	}
	// The SDK boundary also rejects malformed converter output without panicking.
	_, err := geminiSDKEmbed(func() (*genai.EmbedContentResponse, error) { panic(math.Inf(1)) })
	require.Error(t, err)
}
