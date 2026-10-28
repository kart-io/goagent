package livemodel

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

// minimax 支持原生工具调用，但只返回一次性完整结果。
// reasoning_content 只用于续接，不作为面向用户的正文。
type minimax struct {
	key, endpoint, model string
	client               *http.Client
}

func newMiniMax(model string) (*minimax, error) {
	key, base := os.Getenv("MINIMAX_API_KEY"), "https://api.minimax.io/v1"
	if key == "" {
		key, base = os.Getenv("MINIMAX_CN_API_KEY"), "https://api.minimax.cn/v1"
	}
	if key == "" {
		return nil, fmt.Errorf("需要 MINIMAX_API_KEY 或 MINIMAX_CN_API_KEY")
	}
	if override := os.Getenv("MINIMAX_BASE_URL"); override != "" {
		base = override
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("MINIMAX_BASE_URL 必须是不含凭据、查询和片段的 HTTP(S) 地址")
	}
	return &minimax{key: key, model: model, endpoint: strings.TrimRight(base, "/") + "/chat/completions", client: &http.Client{
		Timeout: RequestTimeout,
		// 单次请求：不自动重定向，也不重试。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (m *minimax) completeTurn(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (*llm.StreamChunk, error) {
	if req == nil || req.Model == "" || len(req.Messages) == 0 || req.MaxTokens < 1 {
		return nil, fmt.Errorf("MiniMax 需要模型名、消息和正数输出上限")
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
	// M3.1 默认最大推理强度；案例都是短任务，用 low 控制输出预算。
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
		return nil, fmt.Errorf("MiniMax 请求失败：%s", Redact(err.Error()))
	}
	defer func() { _ = response.Body.Close() }()
	const maxResponseBytes = 2 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// 不输出响应正文：其中可能回显凭据或不可信的模型文本。
		return nil, fmt.Errorf("MiniMax HTTP 状态 %d", response.StatusCode)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("MiniMax 响应超过大小上限")
	}
	var wire struct {
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
		return nil, fmt.Errorf("MiniMax 响应不是合法 JSON")
	}
	if wire.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("MiniMax API 状态 %d", wire.BaseResp.StatusCode)
	}
	if len(wire.Error) > 0 && string(wire.Error) != "null" {
		return nil, fmt.Errorf("MiniMax API 返回错误")
	}
	if len(wire.Choices) != 1 {
		return nil, fmt.Errorf("MiniMax 应返回且仅返回一个候选")
	}
	choice := wire.Choices[0]
	out := &llm.StreamChunk{Content: choice.Message.Content, Delta: choice.Message.Content, Role: "assistant", Done: true,
		ReasoningContent: choice.Message.Reasoning, ToolCalls: choice.Message.ToolCalls, FinishReason: choice.FinishReason}
	if wire.Usage != nil {
		out.Usage = &llm.Usage{PromptTokens: wire.Usage.PromptTokens, CompletionTokens: wire.Usage.CompletionTokens, TotalTokens: wire.Usage.TotalTokens}
	}
	return out, nil
}

func (m *minimax) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	turn, err := m.completeTurn(ctx, req, nil)
	if err != nil {
		return nil, err
	}
	out := &llm.CompletionResponse{Content: turn.Content, Model: req.Model, Provider: "minimax", FinishReason: turn.FinishReason}
	if turn.Usage != nil {
		out.Usage = &interfaces.TokenUsage{PromptTokens: turn.Usage.PromptTokens, CompletionTokens: turn.Usage.CompletionTokens, TotalTokens: turn.Usage.TotalTokens}
		out.TokensUsed = turn.Usage.TotalTokens
	}
	return out, nil
}
func (m *minimax) Provider() constants.Provider { return constants.Provider("minimax") }
func (m *minimax) IsAvailable() bool            { return m.key != "" }
func (m *minimax) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return m.Complete(ctx, &llm.CompletionRequest{Messages: messages, Model: m.model, MaxTokens: MaxTokens})
}
func (m *minimax) ChatStream(ctx context.Context, messages []llm.Message) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, &llm.CompletionRequest{Messages: messages, Model: m.model, MaxTokens: MaxTokens})
}
func (m *minimax) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return m.CompleteToolsStream(ctx, req, nil)
}
func (m *minimax) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	turn, err := m.completeTurn(ctx, req, tools)
	if err != nil {
		return nil, err
	}
	stream := make(chan *llm.StreamChunk, 1)
	stream <- turn
	close(stream)
	return stream, nil
}
func (m *minimax) StreamMode() core.StreamMode { return core.StreamModeFinal }
