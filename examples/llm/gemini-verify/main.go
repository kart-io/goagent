// Command gemini-verify checks GeminiProvider public methods and native agents against
// the native Gemini API. It never uses the OpenAI compatibility endpoint.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
)

type result struct {
	Method     string                 `json:"method"`
	Repeat     int                    `json:"repeat"`
	Passed     bool                   `json:"passed"`
	Answer     string                 `json:"answer,omitempty"`
	ToolCalls  []llm.ToolCall         `json:"tool_calls,omitempty"`
	Usage      *interfaces.TokenUsage `json:"usage,omitempty"`
	Error      string                 `json:"error,omitempty"`
	DurationMS int64                  `json:"duration_ms"`
	Details    map[string]any         `json:"details,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	flags := flag.NewFlagSet("gemini-verify", flag.ContinueOnError)
	live := flags.Bool("live", false, "调用真实 Gemini；默认仅显示计划")
	model := flags.String("model", "gemini-2.5-flash", "Gemini 模型")
	repeats := flags.Int("repeats", 2, "每个方法重复次数（1 到 5）")
	suite := flags.String("suite", "legacy", "legacy 或 native（AsRunnable 与 Embedding）")
	embeddingModel := flags.String("embedding-model", "", "独立 Embedding 模型，默认读取 GEMINI_EMBEDDING_MODEL")
	out := flags.String("out", "gemini-provider-results.json", "新建 JSON 文件，不覆盖")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *repeats < 1 || *repeats > 5 || *model == "" || (*suite != "legacy" && *suite != "native") {
		return fmt.Errorf("invalid arguments")
	}
	methods := []string{"Complete", "Chat", "Stream", "GenerateWithTools", "StreamWithTools", "StreamWithContext"}
	if *suite == "native" {
		methods = []string{"RunnableInvoke", "RunnableStream", "RunnableToolLoop", "RunnableDirectTool", "RunnableSequentialTools", "Embedding"}
	}
	plannedRequests := 0
	for _, m := range methods {
		plannedRequests += requestBudget(m) * (*repeats)
	}
	if !*live {
		fmt.Printf("GeminiProvider 原生 API：%s，%d 次调用，无自动重试；使用 -live 执行。\n", *model, plannedRequests)
		return nil
	}
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		return fmt.Errorf("GEMINI_API_KEY is required")
	}
	p, err := providers.NewGeminiWithOptions(llm.WithAPIKey(key), llm.WithModel(*model), llm.WithEmbeddingModel(*embeddingModel), llm.WithMaxTokens(2048), llm.WithTemperature(0.2), llm.WithTimeout(90*time.Second), llm.WithSystemPrompt("按用户要求执行。最终仅输出答案；若用户要求查询订单，必须调用 lookup 工具。"))
	if err != nil {
		return fmt.Errorf("create GeminiProvider: %s", strings.ReplaceAll(err.Error(), key, "[REDACTED]"))
	}
	file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	var results []result
	for repeat := 0; repeat < *repeats; repeat++ {
		for _, method := range methods {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			var r result
			if *suite == "native" {
				r = checkNative(ctx, p, method)
			} else {
				r = check(ctx, p, method)
			}
			cancel()
			r.Repeat = repeat
			r.Error = strings.ReplaceAll(r.Error, key, "[REDACTED]")
			results = append(results, r)
			fmt.Printf("%s [%d]: %t\n", method, repeat+1, r.Passed)
		}
	}
	passed := 0
	known := 0
	usage := interfaces.TokenUsage{}
	for _, r := range results {
		if r.Passed {
			passed++
		}
		if r.Usage != nil {
			known++
			usage.Add(r.Usage)
		}
	}
	report := map[string]any{"provider": "GeminiProvider", "backend": "Gemini native API (Google GenAI SDK)", "model": *model, "timestamp": time.Now().UTC(), "suite": *suite, "embedding_model": p.Config.EmbeddingModel, "maximum_upstream_requests": plannedRequests, "completed": len(results), "passed": passed, "max_output_tokens": 2048, "temperature": 0.2, "timeout_seconds": 90, "sdk_retries": 0, "usage_reported_cases": known, "known_usage_only": usage, "limitations": "Legacy string/tool streams and Embed do not expose token usage; known usage excludes those requests. Native suite exercises bounded tool loops on synthetic read-only tools; no production backend or broad retrieval quality validation.", "results": results}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	// Guard against unexpected credential echo in any returned content.
	data = []byte(strings.ReplaceAll(string(data), key, "[REDACTED]"))
	if _, err = file.Write(append(data, '\n')); err != nil {
		return err
	}
	if passed != len(results) {
		return fmt.Errorf("%d/%d passed; all failures retained in %s", passed, len(results), *out)
	}
	fmt.Printf("%d/%d 通过，结果：%s\n", passed, len(results), *out)
	return nil
}

func check(ctx context.Context, p *providers.GeminiProvider, method string) (r result) {
	r.Method = method
	start := time.Now()
	defer func() { r.DurationMS = time.Since(start).Milliseconds() }()
	var err error
	prompt := "计算 17+25，只输出整数。"
	toolPrompt := "查询订单 SHARED-1，必须调用 lookup，order_id 为 SHARED-1。"
	lookup := tools.NewBaseTool("lookup", "按订单号查询订单状态", `{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`, func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if len(input.Args) != 1 || input.Args["order_id"] != "SHARED-1" {
			return nil, fmt.Errorf("wrong order arguments")
		}
		return &interfaces.ToolOutput{Success: true, Result: "订单 SHARED-1 已发货。"}, nil
	})
	switch method {
	case "Complete":
		var response *llm.CompletionResponse
		response, err = p.Complete(ctx, &llm.CompletionRequest{Messages: []llm.Message{llm.SystemMessage("只输出数字，不加标点。"), llm.UserMessage(prompt)}})
		if response != nil {
			r.Answer = response.Content
			r.Usage = response.Usage
		}
	case "Chat":
		var response *llm.CompletionResponse
		response, err = p.Chat(ctx, []llm.Message{llm.UserMessage("记住数字 17。"), llm.AssistantMessage("17"), llm.UserMessage("把刚才的数字加上 25，只输出整数。")})
		if response != nil {
			r.Answer = response.Content
			r.Usage = response.Usage
		}
	case "Stream":
		var stream <-chan string
		stream, err = p.Stream(ctx, prompt)
		if err == nil {
			for token := range stream {
				r.Answer += token
			}
		}
	case "GenerateWithTools":
		var response *llm.ToolCallResponse
		response, err = p.GenerateWithTools(ctx, toolPrompt, []interfaces.Tool{lookup})
		if response != nil {
			r.ToolCalls = response.ToolCalls
			r.Usage = response.Usage
		}
	case "StreamWithTools":
		var stream <-chan llm.ToolChunk
		stream, err = p.StreamWithTools(ctx, toolPrompt, []interfaces.Tool{lookup})
		if err == nil {
			for chunk := range stream {
				switch chunk.Type {
				case "error":
					err = fmt.Errorf("%v", chunk.Value)
				case "tool_call":
					call, ok := chunk.Value.(*llm.ToolCall)
					if !ok {
						err = fmt.Errorf("unexpected tool value %T", chunk.Value)
					} else {
						r.ToolCalls = append(r.ToolCalls, *call)
					}
				}
			}
		}
	case "StreamWithContext":
		wrapper := &providers.GeminiStreamingProvider{GeminiProvider: p}
		var stream <-chan providers.StreamEvent
		stream, err = wrapper.StreamWithContext(ctx, prompt)
		completed := false
		if err == nil {
			for event := range stream {
				switch event.Type {
				case "token":
					r.Answer += event.Content
				case "error":
					err = event.Error
				case "complete":
					completed = true
					r.Usage, _ = event.Metadata["usage"].(*interfaces.TokenUsage)
				}
			}
		}
		if err == nil && !completed {
			err = fmt.Errorf("missing completion event")
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		r.Error = err.Error()
		return r
	}
	if method == "GenerateWithTools" || method == "StreamWithTools" {
		if len(r.ToolCalls) != 1 || r.ToolCalls[0].Function.Name != "lookup" || r.ToolCalls[0].ID == "" {
			r.Error = "expected one lookup call with an ID"
			return r
		}
		var args map[string]any
		if err = json.Unmarshal([]byte(r.ToolCalls[0].Function.Arguments), &args); err == nil {
			var output *interfaces.ToolOutput
			output, err = lookup.Invoke(ctx, &interfaces.ToolInput{Args: args})
			if err == nil {
				r.Answer = fmt.Sprint(output.Result)
			}
		}
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.Passed = r.Answer == "订单 SHARED-1 已发货。"
	} else {
		r.Passed = strings.TrimSpace(r.Answer) == "42"
	}
	return r
}
