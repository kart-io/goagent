package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	agentllm "github.com/kart-io/goagent/llm"
)

type embedServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

func (s *embedServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func newEmbedServer(t *testing.T, response string) *embedServer {
	t.Helper()
	s := &embedServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(data))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(s.Close)
	return s
}

func newEmbedOpenAI(t *testing.T, s *embedServer, opts ...agentllm.ClientOption) *OpenAIProvider {
	t.Helper()
	opts = append([]agentllm.ClientOption{agentllm.WithAPIKey("k"), agentllm.WithBaseURL(s.URL)}, opts...)
	p, err := NewOpenAIWithOptions(opts...)
	require.NoError(t, err)
	return p
}

const outOfOrderResponse = `{"data":[{"index":1,"embedding":[0.2,0.2]},{"index":0,"embedding":[0.1,0.1]}]}`

func TestEmbedBatchSendsOneRequestAndOrdersByIndex(t *testing.T) {
	s := newEmbedServer(t, outOfOrderResponse)
	p := newEmbedOpenAI(t, s)

	vecs, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	reqs := s.requests()
	require.Len(t, reqs, 1)
	require.Contains(t, reqs[0], `"input":["a","b"]`)
	require.Equal(t, [][]float32{{0.1, 0.1}, {0.2, 0.2}}, vecs)
}

func TestEmbedBatchRejectsBadResponses(t *testing.T) {
	cases := map[string]string{
		"mismatched count":   `{"data":[{"index":0,"embedding":[0.1]}]}`,
		"duplicate index":    `{"data":[{"index":0,"embedding":[0.1]},{"index":0,"embedding":[0.2]}]}`,
		"index out of range": `{"data":[{"index":0,"embedding":[0.1]},{"index":5,"embedding":[0.2]}]}`,
		"negative index":     `{"data":[{"index":0,"embedding":[0.1]},{"index":-1,"embedding":[0.2]}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s := newEmbedServer(t, body)
			p := newEmbedOpenAI(t, s)
			_, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
			require.Error(t, err)
			require.Equal(t, agentErrors.CodeLLMResponse, agentErrors.GetCode(err))
		})
	}
}

func TestEmbedBatchRejectsEmptyInput(t *testing.T) {
	s := newEmbedServer(t, outOfOrderResponse)
	p := newEmbedOpenAI(t, s)
	for _, in := range [][]string{nil, {}} {
		_, err := p.EmbedBatch(context.Background(), in)
		require.Error(t, err)
		require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
	}
	require.Empty(t, s.requests())
}

func TestEmbedSingleStillWorks(t *testing.T) {
	s := newEmbedServer(t, `{"data":[{"index":0,"embedding":[0.5,0.25]}]}`)
	p := newEmbedOpenAI(t, s)
	vec, err := p.Embed(context.Background(), "solo")
	require.NoError(t, err)
	require.Equal(t, []float32{0.5, 0.25}, vec)
	require.Contains(t, s.requests()[0], `"input":["solo"]`)
}

func TestAsBatchEmbedder(t *testing.T) {
	s := newEmbedServer(t, `{}`)
	openai := newEmbedOpenAI(t, s)
	gemini, err := NewGeminiWithOptions(agentllm.WithAPIKey("k"), agentllm.WithBaseURL(s.URL))
	require.NoError(t, err)
	anthropic, err := NewAnthropicWithOptions(agentllm.WithAPIKey("k"), agentllm.WithBaseURL(s.URL))
	require.NoError(t, err)

	require.NotNil(t, agentllm.AsBatchEmbedder(openai))
	require.NotNil(t, agentllm.AsBatchEmbedder(gemini))
	require.Nil(t, agentllm.AsBatchEmbedder(anthropic))
}

func TestGeminiEmbedBatch(t *testing.T) {
	s := newEmbedServer(t, `{"embeddings":[{"values":[0.1,0.2]},{"values":[0.3,0.4]}]}`)
	p, err := NewGeminiWithOptions(agentllm.WithAPIKey("k"), agentllm.WithBaseURL(s.URL))
	require.NoError(t, err)

	vecs, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Equal(t, [][]float32{{0.1, 0.2}, {0.3, 0.4}}, vecs)
	reqs := s.requests()
	require.Len(t, reqs, 1)
	require.Contains(t, reqs[0], `"text":"a"`)
	require.Contains(t, reqs[0], `"text":"b"`)

	_, err = p.EmbedBatch(context.Background(), nil)
	require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
	require.Len(t, s.requests(), 1)
}

func TestEmbeddingDimensionsSent(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		opts     []agentllm.ClientOption
		field    string
		want     string
	}{
		{"openai set", "openai", []agentllm.ClientOption{agentllm.WithEmbeddingDimensions(1024)}, `"dimensions":1024`, "present"},
		{"gemini set", "gemini", []agentllm.ClientOption{agentllm.WithEmbeddingDimensions(1024)}, `"outputDimensionality":1024`, "present"},
		{"openai unset", "openai", nil, `dimensions`, "absent"},
		{"gemini unset", "gemini", nil, `utputDimensionality`, "absent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newEmbedServer(t, `{"data":[{"index":0,"embedding":[0.1]}],"embeddings":[{"values":[0.1]}]}`)
			opts := append([]agentllm.ClientOption{agentllm.WithAPIKey("k"), agentllm.WithBaseURL(s.URL)}, tc.opts...)
			var embed func() error
			if tc.provider == "openai" {
				p, err := NewOpenAIWithOptions(opts...)
				require.NoError(t, err)
				embed = func() error { _, err := p.Embed(context.Background(), "x"); return err }
			} else {
				p, err := NewGeminiWithOptions(opts...)
				require.NoError(t, err)
				embed = func() error { _, err := p.Embed(context.Background(), "x"); return err }
			}
			require.NoError(t, embed())
			body := s.requests()[0]
			if tc.want == "present" {
				require.True(t, strings.Contains(body, tc.field), body)
			} else {
				require.NotContains(t, body, tc.field)
			}
		})
	}
}

func TestEmbeddingDimensionsRejectsNegative(t *testing.T) {
	opts := []agentllm.ClientOption{agentllm.WithAPIKey("k"), agentllm.WithEmbeddingDimensions(-1)}
	_, err := NewOpenAIWithOptions(opts...)
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
	_, err = NewGeminiWithOptions(opts...)
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
}
