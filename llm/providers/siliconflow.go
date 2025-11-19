package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
)

// SiliconFlowClient SiliconFlow LLM 客户端
// SiliconFlow 是一个提供多种开源模型的服务平台
type SiliconFlowClient struct {
	apiKey      string
	baseURL     string
	model       string
	temperature float64
	maxTokens   int
	client      *resty.Client
}

// SiliconFlowConfig SiliconFlow 配置
type SiliconFlowConfig struct {
	APIKey      string  // API 密钥
	BaseURL     string  // API 地址，默认 https://api.siliconflow.cn/v1
	Model       string  // 模型名称，如 Qwen/Qwen2-7B-Instruct, deepseek-ai/DeepSeek-V2-Chat
	Temperature float64 // 温度参数
	MaxTokens   int     // 最大 token 数
	Timeout     int     // 请求超时（秒）
}

// DefaultSiliconFlowConfig 返回默认 SiliconFlow 配置
func DefaultSiliconFlowConfig() *SiliconFlowConfig {
	return &SiliconFlowConfig{
		BaseURL:     SiliconFlowBaseURL,
		Model:       "Qwen/Qwen2-7B-Instruct", // 默认使用 Qwen2
		Temperature: DefaultTemperature,
		MaxTokens:   DefaultMaxTokens,
		Timeout:     int(DefaultTimeout / time.Second),
	}
}

// NewSiliconFlowClient 创建新的 SiliconFlow 客户端
func NewSiliconFlowClient(config *SiliconFlowConfig) (*SiliconFlowClient, error) {
	if config == nil {
		config = DefaultSiliconFlowConfig()
	}

	if config.APIKey == "" {
		return nil, agentErrors.NewInvalidConfigError(ProviderSiliconFlow, agentllm.ErrorFieldAPIKey, "SiliconFlow API key is required")
	}

	if config.BaseURL == "" {
		config.BaseURL = SiliconFlowBaseURL
	}

	if config.Model == "" {
		config.Model = "Qwen/Qwen2-7B-Instruct"
	}

	if config.Temperature == 0 {
		config.Temperature = DefaultTemperature
	}

	if config.MaxTokens == 0 {
		config.MaxTokens = DefaultMaxTokens
	}

	if config.Timeout == 0 {
		config.Timeout = int(DefaultTimeout / time.Second)
	}

	return &SiliconFlowClient{
		apiKey:      config.APIKey,
		baseURL:     strings.TrimRight(config.BaseURL, "/"),
		model:       config.Model,
		temperature: config.Temperature,
		maxTokens:   config.MaxTokens,
		client: resty.New().
			SetTimeout(time.Duration(config.Timeout) * time.Second).
			SetHeader(HeaderContentType, ContentTypeJSON).
			SetHeader(HeaderAuthorization, AuthBearerPrefix+config.APIKey),
	}, nil
}

// NewSiliconFlow 创建 SiliconFlow provider（兼容 llm.Config）
func NewSiliconFlow(config *agentllm.Config) (*SiliconFlowClient, error) {
	sfConfig := &SiliconFlowConfig{
		APIKey:      config.APIKey,
		BaseURL:     config.BaseURL,
		Model:       config.Model,
		Temperature: config.Temperature,
		MaxTokens:   config.MaxTokens,
		Timeout:     config.Timeout,
	}

	if sfConfig.APIKey == "" {
		sfConfig.APIKey = os.Getenv(agentllm.EnvSiliconFlowAPIKey)
	}

	if sfConfig.BaseURL == "" {
		sfConfig.BaseURL = os.Getenv(agentllm.EnvSiliconFlowBaseURL)
	}
	if sfConfig.BaseURL == "" {
		sfConfig.BaseURL = SiliconFlowBaseURL
	}

	if sfConfig.Model == "" {
		sfConfig.Model = os.Getenv(agentllm.EnvSiliconFlowModel)
	}
	if sfConfig.Model == "" {
		sfConfig.Model = "Qwen/Qwen2-7B-Instruct"
	}

	return NewSiliconFlowClient(sfConfig)
}

// siliconFlowRequest SiliconFlow 请求格式（兼容 OpenAI 格式）
type siliconFlowRequest struct {
	Model       string               `json:"model"`
	Messages    []siliconFlowMessage `json:"messages"`
	Temperature float64              `json:"temperature,omitempty"`
	MaxTokens   int                  `json:"max_tokens,omitempty"`
	TopP        float64              `json:"top_p,omitempty"`
	Stream      bool                 `json:"stream"`
	Stop        []string             `json:"stop,omitempty"`
}

// siliconFlowMessage 消息格式
type siliconFlowMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// siliconFlowResponse 响应格式
type siliconFlowResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []siliconFlowChoice `json:"choices"`
	Usage   siliconFlowUsage    `json:"usage"`
}

// siliconFlowChoice 选择项
type siliconFlowChoice struct {
	Index        int                `json:"index"`
	Message      siliconFlowMessage `json:"message"`
	FinishReason string             `json:"finish_reason"`
}

// siliconFlowUsage 使用统计
type siliconFlowUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Complete 实现 llm.Client 接口的 Complete 方法
func (c *SiliconFlowClient) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// 转换消息格式
	messages := make([]siliconFlowMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = siliconFlowMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// 构建请求
	sfReq := siliconFlowRequest{
		Model:       c.getModel(req.Model),
		Messages:    messages,
		Temperature: c.getTemperature(req.Temperature),
		MaxTokens:   c.getMaxTokens(req.MaxTokens),
		Stream:      false,
	}

	if len(req.Stop) > 0 {
		sfReq.Stop = req.Stop
	}

	if req.TopP > 0 {
		sfReq.TopP = req.TopP
	}

	// 发送请求
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(sfReq).
		Post(c.baseURL + "/chat/completions")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError("siliconflow", c.getModel(req.Model), err)
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError("siliconflow", c.getModel(req.Model),
			fmt.Sprintf("API error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	// 解析响应
	var sfResp siliconFlowResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&sfResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("response body", err).
			WithContext("provider", "siliconflow")
	}

	if len(sfResp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError("siliconflow", c.getModel(req.Model), "no choices in response")
	}

	// 构建响应
	return &agentllm.CompletionResponse{
		Content:      strings.TrimSpace(sfResp.Choices[0].Message.Content),
		Model:        sfResp.Model,
		TokensUsed:   sfResp.Usage.TotalTokens,
		FinishReason: sfResp.Choices[0].FinishReason,
		Provider:     string(agentllm.ProviderSiliconFlow),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     sfResp.Usage.PromptTokens,
			CompletionTokens: sfResp.Usage.CompletionTokens,
			TotalTokens:      sfResp.Usage.TotalTokens,
		},
	}, nil
}

// Chat 实现 llm.Client 接口的 Chat 方法
func (c *SiliconFlowClient) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return c.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Provider 返回提供商类型
func (c *SiliconFlowClient) Provider() agentllm.Provider {
	return agentllm.ProviderSiliconFlow
}

// IsAvailable 检查 SiliconFlow 是否可用
func (c *SiliconFlowClient) IsAvailable() bool {
	// 简单检查 API Key 是否存在
	// SiliconFlow 没有专门的健康检查端点，可以通过发送一个小请求来验证
	if c.apiKey == "" {
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

	_, err := c.Complete(ctx, testReq)
	return err == nil
}

// ListModels 列出可用的模型
func (c *SiliconFlowClient) ListModels() []string {
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

func (c *SiliconFlowClient) getModel(model string) string {
	if model != "" {
		return model
	}
	return c.model
}

func (c *SiliconFlowClient) getTemperature(temp float64) float64 {
	if temp > 0 {
		return temp
	}
	return c.temperature
}

func (c *SiliconFlowClient) getMaxTokens(maxTokens int) int {
	if maxTokens > 0 {
		return maxTokens
	}
	return c.maxTokens
}

// WithModel 设置模型
func (c *SiliconFlowClient) WithModel(model string) *SiliconFlowClient {
	c.model = model
	return c
}

// WithTemperature 设置温度
func (c *SiliconFlowClient) WithTemperature(temperature float64) *SiliconFlowClient {
	c.temperature = temperature
	return c
}

// WithMaxTokens 设置最大 token 数
func (c *SiliconFlowClient) WithMaxTokens(maxTokens int) *SiliconFlowClient {
	c.maxTokens = maxTokens
	return c
}
