package siliconflow

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
	// 自动注册 SiliconFlow provider 到全局注册表
	registry.Register(constants.ProviderSiliconFlow, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
		return New(opts...)
	})
}

// Provider SiliconFlow LLM 客户端
// SiliconFlow 是一个提供多种开源模型的服务平台
type Provider struct {
	*common.BaseProvider
	apiKey  string
	baseURL string
	client  *httpclient.Client
}

// New 使用选项模式创建 SiliconFlow provider
func New(opts ...agentllm.ClientOption) (*Provider, error) {
	// 创建 BaseProvider，统一处理 Options
	base := common.NewBaseProvider(opts...)

	// 应用 Provider 特定的默认值
	base.ApplyProviderDefaults(
		constants.ProviderSiliconFlow,
		constants.SiliconFlowBaseURL,
		"Qwen/Qwen2-7B-Instruct",
		constants.EnvSiliconFlowBaseURL,
		constants.EnvSiliconFlowModel,
	)

	// 统一处理 API Key
	if err := base.EnsureAPIKey(constants.EnvSiliconFlowAPIKey, constants.ProviderSiliconFlow); err != nil {
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

// request SiliconFlow 请求格式
type request struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Stream      bool      `json:"stream"`
	Stop        []string  `json:"stop,omitempty"`
}

// message 消息格式
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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

// Complete 实现 llm.Client 接口的 Complete 方法
func (p *Provider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// 转换消息格式
	messages := make([]message, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = message{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// 构建请求
	sfReq := request{
		Model:       p.GetModel(req.Model),
		Messages:    messages,
		Temperature: p.GetTemperature(req.Temperature),
		MaxTokens:   p.GetMaxTokens(req.MaxTokens),
		Stream:      false,
	}

	if len(req.Stop) > 0 {
		sfReq.Stop = req.Stop
	}

	if req.TopP > 0 {
		sfReq.TopP = req.TopP
	}

	// 发送请求
	model := p.GetModel(req.Model)
	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(sfReq).
		Post(p.baseURL + "/chat/completions")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err)
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model,
			fmt.Sprintf("API error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	// 解析响应
	var sfResp response
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&sfResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("response body", err).
			WithContext("provider", p.ProviderName())
	}

	if len(sfResp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model, "no choices in response")
	}

	// 构建响应
	return &agentllm.CompletionResponse{
		Content:      strings.TrimSpace(sfResp.Choices[0].Message.Content),
		Model:        sfResp.Model,
		TokensUsed:   sfResp.Usage.TotalTokens,
		FinishReason: sfResp.Choices[0].FinishReason,
		Provider:     string(constants.ProviderSiliconFlow),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     sfResp.Usage.PromptTokens,
			CompletionTokens: sfResp.Usage.CompletionTokens,
			TotalTokens:      sfResp.Usage.TotalTokens,
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
	return constants.ProviderSiliconFlow
}

// ProviderName 返回提供商名称
func (p *Provider) ProviderName() string {
	return string(constants.ProviderSiliconFlow)
}

// IsAvailable 检查 SiliconFlow 是否可用
func (p *Provider) IsAvailable() bool {
	// 简单检查 API Key 是否存在
	// SiliconFlow 没有专门的健康检查端点，可以通过发送一个小请求来验证
	if p.apiKey == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 发送一个最小的测试请求
	testReq := &agentllm.CompletionRequest{
		Messages: []agentllm.Message{
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: 1,
	}

	_, err := p.Complete(ctx, testReq)
	return err == nil
}

// ListModels 列出可用的模型
func (p *Provider) ListModels() []string {
	// SiliconFlow 支持的模型列表
	return []string{
		// Qwen 系列
		"Qwen/Qwen2-7B-Instruct",
		"Qwen/Qwen2-1.5B-Instruct",
		"Qwen/Qwen2.5-7B-Instruct",
		"Qwen/Qwen2.5-14B-Instruct",
		"Qwen/Qwen2.5-32B-Instruct",
		"Qwen/Qwen2.5-72B-Instruct",
		"Qwen/Qwen2.5-Coder-7B-Instruct",

		// DeepSeek 系列
		"deepseek-ai/DeepSeek-V2-Chat",
		"deepseek-ai/DeepSeek-V2.5",
		"deepseek-ai/DeepSeek-Coder-V2-Instruct",

		// GLM 系列
		"THUDM/glm-4-9b-chat",
		"THUDM/chatglm3-6b",

		// Yi 系列
		"01-ai/Yi-1.5-34B-Chat-16K",
		"01-ai/Yi-1.5-9B-Chat-16K",
		"01-ai/Yi-1.5-6B-Chat",

		// Mistral 系列
		"mistralai/Mistral-7B-Instruct-v0.2",
		"mistralai/Mixtral-8x7B-Instruct-v0.1",

		// Meta Llama 系列
		"meta-llama/Meta-Llama-3.1-8B-Instruct",
		"meta-llama/Meta-Llama-3.1-70B-Instruct",
		"meta-llama/Meta-Llama-3-8B-Instruct",
		"meta-llama/Meta-Llama-3-70B-Instruct",

		// 其他模型
		"internlm/internlm2_5-7b-chat",
		"google/gemma-2-9b-it",
	}
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
