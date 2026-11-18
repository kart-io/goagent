// Command json-response 演示 llm.WithJSONResponse：同一提示词分别不带、带该选项各请求一次，
// 并解析回复。默认连本地模拟的 OpenAI 兼容服务，无需 API Key；-live 时调用真实模型。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

const prompt = "Return the weather in Paris as JSON with fields city (string) and temp_c (number)."

type weather struct {
	City  string  `json:"city"`
	TempC float64 `json:"temp_c"`
}

// liveProviders 列出 -live 支持的提供方及其凭据环境变量。
var liveProviders = map[string]struct {
	env string
	new func(...llm.ClientOption) (llm.Client, error)
}{
	"openai":   {"OPENAI_API_KEY", func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewOpenAIWithOptions(o...) }},
	"deepseek": {"DEEPSEEK_API_KEY", func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewDeepSeekWithOptions(o...) }},
	"gemini":   {"GEMINI_API_KEY", func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewGeminiWithOptions(o...) }},
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("json-response", flag.ContinueOnError)
	live := flags.Bool("live", false, "调用真实模型；默认使用本地模拟服务")
	provider := flags.String("provider", "deepseek", "-live 时的提供方：openai、deepseek 或 gemini")
	model := flags.String("model", "", "模型名，留空用提供方默认值")
	if err := flags.Parse(args); err != nil {
		return err
	}
	opts := []llm.ClientOption{llm.WithTimeout(60 * time.Second), llm.WithMaxTokens(2048)}
	if *model != "" {
		opts = append(opts, llm.WithModel(*model))
	}
	newClient := func(o ...llm.ClientOption) (llm.Client, error) { return providers.NewOpenAIWithOptions(o...) }
	if *live {
		p, ok := liveProviders[*provider]
		if !ok {
			return fmt.Errorf("unknown provider %q", *provider)
		}
		key := os.Getenv(p.env)
		if key == "" {
			return fmt.Errorf("%s is required for -live", p.env)
		}
		newClient, opts = p.new, append(opts, llm.WithAPIKey(key))
	} else {
		server := httptest.NewServer(fakeOpenAI())
		defer server.Close()
		opts = append(opts, llm.WithAPIKey("offline"), llm.WithBaseURL(server.URL), llm.WithModel("fake-model"))
	}

	for _, mode := range []struct {
		name string
		opts []llm.ClientOption
	}{
		{"without WithJSONResponse", opts},
		{"with WithJSONResponse", append(opts[:len(opts):len(opts)], llm.WithJSONResponse())},
	} {
		client, err := newClient(mode.opts...)
		if err != nil {
			return err
		}
		resp, err := llm.Complete(ctx, client, &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage(prompt)}})
		if err != nil {
			return fmt.Errorf("%s: %w", mode.name, err)
		}
		_, _ = fmt.Fprintf(out, "%s\n  reply: %s\n", mode.name, resp.Content)
		var w weather
		if err := json.Unmarshal([]byte(resp.Content), &w); err != nil {
			_, _ = fmt.Fprintf(out, "  parse: failed (%v)\n", err)
			continue
		}
		_, _ = fmt.Fprintf(out, "  parse: ok city=%s temp_c=%g\n", w.City, w.TempC)
	}
	return nil
}

// fakeOpenAI 模拟 OpenAI 兼容的流式接口：请求带 response_format=json_object 时回 JSON，
// 否则回带说明文字的 Markdown，近似真实模型不开 JSON 模式时的常见输出。
func fakeOpenAI() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ResponseFormat *struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		if err := json.Decode(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		content := "Sure! Here is the weather:\n```json\n{\"city\": \"Paris\", \"temp_c\": 18}\n```"
		if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
			content = `{"city":"Paris","temp_c":18}`
		}
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": content}, "finish_reason": "stop"}}})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	})
}
