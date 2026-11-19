package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	agentllm "github.com/kart-io/goagent/llm"
)

type recordingTripper struct {
	mu      sync.Mutex
	headers []http.Header
}

func (r *recordingTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.headers = append(r.headers, req.Header.Clone())
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func (r *recordingTripper) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.headers)
}

var injectionConstructors = map[string]func(...agentllm.ClientOption) (agentllm.Client, error){
	"openai":    func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewOpenAIWithOptions(o...) },
	"deepseek":  func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewDeepSeekWithOptions(o...) },
	"kimi":      func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewKimiWithOptions(o...) },
	"anthropic": func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewAnthropicWithOptions(o...) },
	"gemini":    func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewGeminiWithOptions(o...) },
}

func completeOnce(client agentllm.Client) {
	_, _ = agentllm.Complete(context.Background(), client, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("hi")}})
}

func TestWithHTTPClientRoutesEveryProvider(t *testing.T) {
	for name, newClient := range injectionConstructors {
		t.Run(name, func(t *testing.T) {
			tripper := &recordingTripper{}
			client, err := newClient(
				agentllm.WithAPIKey("sk-test"),
				agentllm.WithBaseURL("http://injected.invalid"),
				agentllm.WithModel("m"),
				agentllm.WithHTTPClient(&http.Client{Transport: tripper}),
			)
			if err != nil {
				t.Fatal(err)
			}
			completeOnce(client)
			if tripper.count() < 1 {
				t.Fatalf("注入的传输层未收到请求")
			}
		})
	}
}

func TestWithHTTPClientKeepsRedirectPolicy(t *testing.T) {
	for name, newClient := range injectionConstructors {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				http.Error(w, "{}", http.StatusBadRequest)
			}))
			defer target.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
			}))
			defer origin.Close()

			client, err := newClient(
				agentllm.WithAPIKey("sk-test"),
				agentllm.WithBaseURL(origin.URL),
				agentllm.WithModel("m"),
				agentllm.WithHTTPClient(&http.Client{
					CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") },
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			completeOnce(client)
			if n := hits.Load(); n != 0 {
				t.Fatalf("重定向目标收到 %d 次请求，应为 0", n)
			}
		})
	}
}

func TestWithHTTPClientStillAppliesCustomHeaders(t *testing.T) {
	for name, newClient := range injectionConstructors {
		t.Run(name, func(t *testing.T) {
			tripper := &recordingTripper{}
			client, err := newClient(
				agentllm.WithAPIKey("sk-test"),
				agentllm.WithBaseURL("http://injected.invalid"),
				agentllm.WithModel("m"),
				agentllm.WithCustomHeaders(map[string]string{"X-Test": "1"}),
				agentllm.WithHTTPClient(&http.Client{Transport: tripper}),
			)
			if err != nil {
				t.Fatal(err)
			}
			completeOnce(client)
			if tripper.count() < 1 {
				t.Fatal("注入的传输层未收到请求")
			}
			if got := tripper.headers[0].Get("X-Test"); got != "1" {
				t.Fatalf("X-Test = %q, 期望 1", got)
			}
		})
	}
}
