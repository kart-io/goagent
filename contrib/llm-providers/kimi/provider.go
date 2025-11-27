package kimi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/registry"
	"github.com/kart-io/goagent/utils/httpclient"
)

func init() {
	// 自动注册 Kimi provider 到全局注册表
	registry.Register(constants.ProviderKimi, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
		return New(opts...)
	})
}

// Provider Kimi (Moonshot AI) LLM 客户端
// Kimi 是月之暗面推出的智能助手，支持超长上下文（最高200K tokens）
type Provider struct {
	*common.BaseProvider
	apiKey  string
	baseURL string
	client  *httpclient.Client
}

// New 使用选项模式创建 Kimi provider
func New(opts ...agentllm.ClientOption) (*Provider, error) {
	// 创建 BaseProvider，统一处理 Options
	base := common.NewBaseProvider(opts...)

	// 应用 Provider 特定的默认值
	base.ApplyProviderDefaults(
		constants.ProviderKimi,
		constants.KimiBaseURL,
		"moonshot-v1-8k",
		constants.EnvKimiBaseURL,
		constants.EnvKimiModel,
	)

	// 统一处理 API Key
	if err := base.EnsureAPIKey(constants.EnvKimiAPIKey, constants.ProviderKimi); err != nil {
		return nil, err
	}

	// 使用 BaseProvider 的 NewHTTPClient 方法创建 HTTP 客户端
	client := base.NewHTTPClient(common.HTTPClientConfig{
		Timeout: base.GetTimeout(),
		Headers: map[string]string{
			constants.HeaderContentType:   constants.ContentTypeJSON,
			constants.HeaderAuthorization: constants.AuthBearerPrefix + base.Config.APIKey,
		},
		BaseURL: base.Config.BaseURL,
	})

	return &Provider{
		BaseProvider: base,
		apiKey:       base.Config.APIKey,
		baseURL:      strings.TrimRight(base.Config.BaseURL, "/"),
		client:       client,
	}, nil
}

// request Kimi 请求格式
type request struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	N           int       `json:"n,omitempty"`
	Stream      bool      `json:"stream"`
	Stop        []string  `json:"stop,omitempty"`
}

// message 消息格式
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// response 响应格式
type response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

// choice 选择项
type choice struct {
	Index        int     `json:"index"`
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// usage 使用统计
type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// errorResponse 错误响应
type errorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Complete 实现 llm.Client 接口的 Complete 方法
func (p *Provider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// 转换消息格式
	messages := make([]message, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = message{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		}
	}

	// 构建请求
	kimiReq := request{
		Model:       p.GetModel(req.Model),
		Messages:    messages,
		Temperature: p.GetTemperature(req.Temperature),
		MaxTokens:   p.GetMaxTokens(req.MaxTokens),
		Stream:      false,
		N:           1,
	}

	if len(req.Stop) > 0 {
		kimiReq.Stop = req.Stop
	}

	if req.TopP > 0 {
		kimiReq.TopP = req.TopP
	}

	// 发送请求
	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(kimiReq).
		Post(p.baseURL + "/chat/completions")

	model := p.GetModel(req.Model)
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
	}

	body := resp.Body()

	if !resp.IsSuccess() {
		var errResp errorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model,
				fmt.Sprintf("%s (type: %s, code: %s)",
					errResp.Error.Message, errResp.Error.Type, errResp.Error.Code))
		}
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model,
			fmt.Sprintf("API error (status %d): %s", resp.StatusCode(), string(body)))
	}

	// 解析响应
	var kimiResp response
	if err := json.Unmarshal(body, &kimiResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError(string(body), err).
			WithContext("provider", p.ProviderName())
	}

	if len(kimiResp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, "no choices in response")
	}

	// 构建响应
	return &agentllm.CompletionResponse{
		Content:      strings.TrimSpace(kimiResp.Choices[0].Message.Content),
		Model:        kimiResp.Model,
		TokensUsed:   kimiResp.Usage.TotalTokens,
		FinishReason: kimiResp.Choices[0].FinishReason,
		Provider:     string(constants.ProviderKimi),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     kimiResp.Usage.PromptTokens,
			CompletionTokens: kimiResp.Usage.CompletionTokens,
			TotalTokens:      kimiResp.Usage.TotalTokens,
		},
	}, nil
}

// Chat 实现 llm.Client 接口的 Chat 方法
func (p *Provider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return p.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Provider 返回提供商类型
func (p *Provider) Provider() constants.Provider {
	return constants.ProviderKimi
}

// ProviderName 返回提供商名称
func (p *Provider) ProviderName() string {
	return string(constants.ProviderKimi)
}

// IsAvailable 检查 Kimi 是否可用
func (p *Provider) IsAvailable() bool {
	// 检查 API Key
	if p.apiKey == "" {
		return false
	}

	// 可以通过获取模型列表来检查 API 是否可用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := p.client.R().
		SetContext(ctx).
		Get(p.baseURL + "/models")

	if err != nil {
		return false
	}

	return resp.IsSuccess()
}

// ListModels 列出可用的模型
func (p *Provider) ListModels() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := p.client.R().
		SetContext(ctx).
		Get(p.baseURL + "/models")

	model := p.GetModel("")
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("operation", "list_models")
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model,
			fmt.Sprintf("failed to list models (status %d): %s", resp.StatusCode(), resp.String()))
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&result); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("models list response", err).
			WithContext("provider", p.ProviderName())
	}

	models := make([]string, len(result.Data))
	for i, m := range result.Data {
		models[i] = m.ID
	}

	return models, nil
}

// GetSupportedModels 获取 Kimi 支持的模型列表（静态）
func (p *Provider) GetSupportedModels() []string {
	return []string{
		"moonshot-v1-8k",   // 8K 上下文窗口
		"moonshot-v1-32k",  // 32K 上下文窗口
		"moonshot-v1-128k", // 128K 上下文窗口
	}
}

// GetModelContextSize 获取模型的上下文大小
func (p *Provider) GetModelContextSize(model string) int {
	switch model {
	case "moonshot-v1-8k":
		return 8000
	case "moonshot-v1-32k":
		return 32000
	case "moonshot-v1-128k":
		return 128000
	default:
		return 8000 // 默认返回 8K
	}
}

// EstimateTokenCount 估算文本的 token 数量
// Kimi 使用类似 GPT 的分词器，平均每个中文字符约 1.5 tokens，英文单词约 1.3 tokens
func (p *Provider) EstimateTokenCount(text string) int {
	// 简单估算：中英文混合内容平均每个字符 0.75 tokens
	return len(text) * 3 / 4
}

// CalculateFileUploadTokens 计算文件上传所需的 token 数
// Kimi 支持文件上传，需要计算文件内容的 token 数
func (p *Provider) CalculateFileUploadTokens(fileContent string) int {
	return p.EstimateTokenCount(fileContent)
}

// ValidateContextSize 验证消息是否超过模型的上下文限制
func (p *Provider) ValidateContextSize(messages []agentllm.Message) error {
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += p.EstimateTokenCount(msg.Content)
	}

	maxContext := p.GetModelContextSize(p.GetModel(""))
	if totalTokens > maxContext {
		return agentErrors.NewInvalidInputError(p.ProviderName(), "messages",
			fmt.Sprintf("estimated tokens (%d) exceed model context size (%d)", totalTokens, maxContext))
	}

	return nil
}

// 辅助方法

// WithModel 设置模型
func (p *Provider) WithModel(model string) *Provider {
	p.Config.Model = model
	return p
}

// WithTemperature 设置温度
func (p *Provider) WithTemperature(temperature float64) *Provider {
	p.Config.Temperature = temperature
	return p
}

// WithMaxTokens 设置最大 token 数
func (p *Provider) WithMaxTokens(maxTokens int) *Provider {
	p.Config.MaxTokens = maxTokens
	return p
}
