package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	agentllm "github.com/kart-io/goagent/llm"
)

// WithCustomHeaders 必须作用于每个 Provider 的模型请求，网关鉴权（如只认 X-Api-Key）依赖它。
func TestCustomHeadersReachUpstream(t *testing.T) {
	constructors := map[string]func(...agentllm.ClientOption) (agentllm.Client, error){
		"openai":      func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewOpenAIWithOptions(o...) },
		"kimi":        func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewKimiWithOptions(o...) },
		"siliconflow": func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewSiliconFlowWithOptions(o...) },
		"ollama":      func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewOllamaWithOptions(o...) },
		"deepseek":    func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewDeepSeekWithOptions(o...) },
		"anthropic":   func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewAnthropicWithOptions(o...) },
		"gemini":      func(o ...agentllm.ClientOption) (agentllm.Client, error) { return NewGeminiWithOptions(o...) },
	}
	for name, newClient := range constructors {
		t.Run(name, func(t *testing.T) {
			got := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case got <- r.Header.Clone():
				default:
				}
				http.Error(w, `{"error":{"message":"stop"}}`, http.StatusBadRequest)
			}))
			defer server.Close()

			client, err := newClient(
				agentllm.WithAPIKey("sk-test"),
				agentllm.WithBaseURL(server.URL),
				agentllm.WithModel("m"),
				agentllm.WithCustomHeaders(map[string]string{"X-Api-Key": "sk-test", "X-Tenant": "t1"}),
			)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = agentllm.Complete(context.Background(), client, &agentllm.CompletionRequest{Messages: []agentllm.Message{agentllm.UserMessage("hi")}})

			header := <-got
			if header.Get("X-Tenant") != "t1" || header.Get("X-Api-Key") != "sk-test" {
				t.Fatalf("自定义请求头未发送: %v", header)
			}
		})
	}
}
