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

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// This example adapter implements completionClient only, not the full GoAgent
// provider/stream/tool protocol. Explicit reasoning_split keeps thinking outside
// the business JSON. Never strip arbitrary text to turn invalid output into JSON.
type minimaxCompletion struct {
	key, endpoint string
	client        *http.Client
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

func (m *minimaxCompletion) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if req == nil || req.Model == "" || len(req.Messages) == 0 || req.MaxTokens < 1 {
		return nil, fmt.Errorf("MiniMax requires model, messages and positive max tokens")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request := struct {
		Model           string        `json:"model"`
		Messages        []llm.Message `json:"messages"`
		MaxTokens       int           `json:"max_completion_tokens"`
		Temperature     float64       `json:"temperature"`
		ReasoningSplit  bool          `json:"reasoning_split"`
		ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	}{Model: req.Model, Messages: req.Messages, MaxTokens: req.MaxTokens, Temperature: req.Temperature, ReasoningSplit: true}
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
				Content string `json:"content"`
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
	out := &llm.CompletionResponse{Content: wire.Choices[0].Message.Content, FinishReason: wire.Choices[0].FinishReason, Model: wire.Model, Provider: "minimax", Usage: wire.Usage}
	if wire.Usage != nil {
		out.TokensUsed = wire.Usage.TotalTokens
	}
	return out, nil
}
