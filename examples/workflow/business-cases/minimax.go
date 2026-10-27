package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// This example adapter supports native tools with buffered, final-only output.
// It preserves reasoning_content for continuation, never as user-facing text.
type minimaxCompletion struct {
	key, endpoint, model string
	client               *http.Client
}

func minimaxConfig() (key, base string, err error) {
	key, base = os.Getenv("MINIMAX_API_KEY"), "https://api.minimax.io/v1"
	if key == "" {
		key, base = os.Getenv("MINIMAX_CN_API_KEY"), "https://api.minimax.cn/v1"
	}
	if key == "" {
		return "", "", fmt.Errorf("MINIMAX_API_KEY or MINIMAX_CN_API_KEY is required")
	}
	if override := os.Getenv("MINIMAX_BASE_URL"); override != "" {
		base = override
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fmt.Errorf("MINIMAX_BASE_URL must be an HTTP(S) base URL without credentials, query or fragment")
	}
	return key, strings.TrimRight(base, "/"), nil
}

func newMiniMaxCompletion() (*minimaxCompletion, error) {
	key, base, err := minimaxConfig()
	if err != nil {
		return nil, err
	}
	return &minimaxCompletion{key: key, endpoint: base + "/chat/completions", client: &http.Client{
		Timeout: modelTimeout,
		// One request, no automatic redirects or retries.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (m *minimaxCompletion) completeTurn(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (*llm.StreamChunk, error) {
	if req != nil {
		copy := *req
		req = &copy
		if req.Model == "" {
			req.Model = m.model
		}
	}
	if req == nil || req.Model == "" || len(req.Messages) == 0 || req.MaxTokens < 1 {
		return nil, fmt.Errorf("MiniMax requires model, messages and positive max tokens")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	type definition struct {
		Type     string   `json:"type"`
		Function function `json:"function"`
	}
	definitions := make([]definition, 0, len(tools))
	for _, tool := range tools {
		definitions = append(definitions, definition{"function", function{tool.Name(), tool.Description(), json.RawMessage(tool.ArgsSchema())}})
	}
	request := struct {
		Tools           []definition  `json:"tools,omitempty"`
		Model           string        `json:"model"`
		Messages        []llm.Message `json:"messages"`
		MaxTokens       int           `json:"max_completion_tokens"`
		Temperature     float64       `json:"temperature"`
		ReasoningSplit  bool          `json:"reasoning_split"`
		ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	}{Tools: definitions, Model: req.Model, Messages: req.Messages, MaxTokens: req.MaxTokens, Temperature: req.Temperature, ReasoningSplit: true}
	// M3.1 defaults to max reasoning. These short classification cases use low
	// effort to stay within the same bounded output budget as the other models.
	if req.Model == "MiniMax-M3.1-Flash" {
		request.ReasoningEffort = "low"
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.key)
	response, err := m.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	const maxResponseBytes = 2 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Do not print response bodies: they may contain echoed credentials or
		// untrusted model text. The status code remains useful for diagnostics.
		return nil, fmt.Errorf("MiniMax HTTP status %d", response.StatusCode)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("MiniMax response exceeds size limit")
	}
	var wire struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   string         `json:"content"`
				Reasoning string         `json:"reasoning_content"`
				ToolCalls []llm.ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage    *interfaces.TokenUsage `json:"usage"`
		Error    json.RawMessage        `json:"error"`
		BaseResp struct {
			StatusCode int `json:"status_code"`
		} `json:"base_resp"`
	}
	if err = json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("invalid MiniMax response JSON")
	}
	if wire.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("MiniMax API status %d", wire.BaseResp.StatusCode)
	}
	if len(wire.Error) > 0 && string(wire.Error) != "null" {
		return nil, fmt.Errorf("MiniMax API returned an error")
	}
	if len(wire.Choices) != 1 {
		return nil, fmt.Errorf("MiniMax expected exactly one response choice")
	}
	choice := wire.Choices[0]
	out := &llm.StreamChunk{Content: choice.Message.Content, Delta: choice.Message.Content, Role: "assistant", Done: true,
		ReasoningContent: choice.Message.Reasoning, ToolCalls: choice.Message.ToolCalls, FinishReason: choice.FinishReason}
	if wire.Usage != nil {
		out.Usage = &llm.Usage{PromptTokens: wire.Usage.PromptTokens, CompletionTokens: wire.Usage.CompletionTokens, TotalTokens: wire.Usage.TotalTokens}
	}
	return out, nil
}

func (m *minimaxCompletion) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	turn, err := m.completeTurn(ctx, req, nil)
	if err != nil {
		return nil, err
	}
	model := m.model
	if req.Model != "" {
		model = req.Model
	}
	out := &llm.CompletionResponse{Content: turn.Content, Model: model, Provider: "minimax", FinishReason: turn.FinishReason}
	if turn.Usage != nil {
		out.Usage = &interfaces.TokenUsage{PromptTokens: turn.Usage.PromptTokens, CompletionTokens: turn.Usage.CompletionTokens, TotalTokens: turn.Usage.TotalTokens}
		out.TokensUsed = turn.Usage.TotalTokens
	}
	return out, nil
}

func (m *minimaxCompletion) Provider() constants.Provider { return constants.Provider("minimax") }
func (m *minimaxCompletion) IsAvailable() bool            { return m.key != "" }
func (m *minimaxCompletion) StreamMode() core.StreamMode  { return core.StreamModeFinal }
func (m *minimaxCompletion) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return m.Complete(ctx, &llm.CompletionRequest{Messages: messages, Model: m.model, MaxTokens: modelMaxTokens})
}
func (m *minimaxCompletion) ChatStream(ctx context.Context, messages []llm.Message) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, &llm.CompletionRequest{Messages: messages, Model: m.model, MaxTokens: modelMaxTokens})
}
func (m *minimaxCompletion) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return m.CompleteToolsStream(ctx, req, nil)
}
func (m *minimaxCompletion) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	turn, err := m.completeTurn(ctx, req, tools)
	if err != nil {
		return nil, err
	}
	stream := make(chan *llm.StreamChunk, 1)
	stream <- turn
	close(stream)
	return stream, nil
}

var _ llm.ToolStreamClient = (*minimaxCompletion)(nil)
