package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/utils/httpclient"
)

// KimiClient Kimi (Moonshot AI) LLM 客户端
// Kimi 是月之暗面推出的智能助手，支持超长上下文（最高200K tokens）
type KimiClient struct {
	apiKey      string
	baseURL     string
	model       string
	temperature float64
	maxTokens   int
	client      *httpclient.Client
}

// KimiConfig Kimi 配置
type KimiConfig struct {
	APIKey      string  // API 密钥
	BaseURL     string  // API 地址，默认 https://api.moonshot.cn/v1
	Model       string  // 模型名称，如 moonshot-v1-8k, moonshot-v1-32k, moonshot-v1-128k
	Temperature float64 // 温度参数
	MaxTokens   int     // 最大 token 数
	Timeout     int     // 请求超时（秒）
}

// DefaultKimiConfig 返回默认 Kimi 配置
func DefaultKimiConfig() *KimiConfig {
	return &KimiConfig{
		BaseURL:     KimiBaseURL,
		Model:       "moonshot-v1-8k", // 默认使用 8K 上下文模型
		Temperature: DefaultTemperature,
		MaxTokens:   DefaultMaxTokens,
		Timeout:     int(DefaultTimeout / time.Second),
	}
}

// NewKimiClient 创建新的 Kimi 客户端
func NewKimiClient(config *KimiConfig) (*KimiClient, error) {
	if config == nil {
		config = DefaultKimiConfig()
	}

	if config.APIKey == "" {
		return nil, agentErrors.NewInvalidConfigError(ProviderKimi, agentllm.ErrorFieldAPIKey, "kimi API key is required")
	}

	if config.BaseURL == "" {
		config.BaseURL = KimiBaseURL
	}

	if config.Model == "" {
		config.Model = "moonshot-v1-8k"
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

	return &KimiClient{
		apiKey:      config.APIKey,
		baseURL:     strings.TrimRight(config.BaseURL, "/"),
		model:       config.Model,
		temperature: config.Temperature,
		maxTokens:   config.MaxTokens,
		client: httpclient.NewClient(&httpclient.Config{
			Timeout: time.Duration(config.Timeout) * time.Second,
			Headers: map[string]string{
				HeaderContentType:  ContentTypeJSON,
				HeaderAuthorization: AuthBearerPrefix + config.APIKey,
			},
		}),
	}, nil
}

// NewKimi 创建 Kimi provider（兼容 llm.Config）
func NewKimi(config *agentllm.Config) (*KimiClient, error) {
	kimiConfig := &KimiConfig{
		APIKey:      config.APIKey,
		BaseURL:     config.BaseURL,
		Model:       config.Model,
		Temperature: config.Temperature,
		MaxTokens:   config.MaxTokens,
		Timeout:     config.Timeout,
	}

	if kimiConfig.APIKey == "" {
		kimiConfig.APIKey = os.Getenv(agentllm.EnvKimiAPIKey)
	}

	if kimiConfig.BaseURL == "" {
		kimiConfig.BaseURL = os.Getenv(agentllm.EnvKimiBaseURL)
	}
	if kimiConfig.BaseURL == "" {
		kimiConfig.BaseURL = KimiBaseURL
	}

	if kimiConfig.Model == "" {
		kimiConfig.Model = os.Getenv(agentllm.EnvKimiModel)
	}
	if kimiConfig.Model == "" {
		kimiConfig.Model = "moonshot-v1-8k"
	}

	return NewKimiClient(kimiConfig)
}

// kimiRequest Kimi 请求格式（兼容 OpenAI 格式）
type kimiRequest struct {
	Model       string        `json:"model"`
	Messages    []kimiMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	TopP        float64       `json:"top_p,omitempty"`
	N           int           `json:"n,omitempty"`
	Stream      bool          `json:"stream"`
	Stop        []string      `json:"stop,omitempty"`
}

// kimiMessage 消息格式
type kimiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// kimiResponse 响应格式
type kimiResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []kimiChoice `json:"choices"`
	Usage   kimiUsage    `json:"usage"`
}

// kimiChoice 选择项
type kimiChoice struct {
	Index        int         `json:"index"`
	Message      kimiMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// kimiUsage 使用统计
type kimiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// kimiError 错误响应
type kimiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Complete 实现 llm.Client 接口的 Complete 方法
func (c *KimiClient) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
	// 转换消息格式
	messages := make([]kimiMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = kimiMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		}
	}

	// 构建请求
	kimiReq := kimiRequest{
		Model:       c.getModel(req.Model),
		Messages:    messages,
		Temperature: c.getTemperature(req.Temperature),
		MaxTokens:   c.getMaxTokens(req.MaxTokens),
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
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(kimiReq).
		Post(c.baseURL + "/chat/completions")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError("kimi", c.getModel(req.Model), err)
	}

	body := resp.Body()

	if !resp.IsSuccess() {
		var errResp kimiError
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, agentErrors.NewLLMResponseError("kimi", c.getModel(req.Model),
				fmt.Sprintf("%s (type: %s, code: %s)",
					errResp.Error.Message, errResp.Error.Type, errResp.Error.Code))
		}
		return nil, agentErrors.NewLLMResponseError("kimi", c.getModel(req.Model),
			fmt.Sprintf("API error (status %d): %s", resp.StatusCode(), string(body)))
	}

	// 解析响应
	var kimiResp kimiResponse
	if err := json.Unmarshal(body, &kimiResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError(string(body), err).
			WithContext("provider", "kimi")
	}

	if len(kimiResp.Choices) == 0 {
		return nil, agentErrors.NewLLMResponseError("kimi", c.getModel(req.Model), "no choices in response")
	}

	// 构建响应
	return &agentllm.CompletionResponse{
		Content:      strings.TrimSpace(kimiResp.Choices[0].Message.Content),
		Model:        kimiResp.Model,
		TokensUsed:   kimiResp.Usage.TotalTokens,
		FinishReason: kimiResp.Choices[0].FinishReason,
		Provider:     string(agentllm.ProviderKimi),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     kimiResp.Usage.PromptTokens,
			CompletionTokens: kimiResp.Usage.CompletionTokens,
			TotalTokens:      kimiResp.Usage.TotalTokens,
		},
	}, nil
}

// Chat 实现 llm.Client 接口的 Chat 方法
func (c *KimiClient) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	return c.Complete(ctx, &agentllm.CompletionRequest{
		Messages: messages,
	})
}

// Provider 返回提供商类型
func (c *KimiClient) Provider() agentllm.Provider {
	return agentllm.ProviderKimi
}

// IsAvailable 检查 Kimi 是否可用
func (c *KimiClient) IsAvailable() bool {
	// 检查 API Key
	if c.apiKey == "" {
		return false
	}

	// 可以通过获取模型列表来检查 API 是否可用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.client.R().
		SetContext(ctx).
		Get(c.baseURL + "/models")

	if err != nil {
		return false
	}

	return resp.IsSuccess()
}

// ListModels 列出可用的模型
func (c *KimiClient) ListModels() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.client.R().
		SetContext(ctx).
		Get(c.baseURL + "/models")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError("kimi", c.model, err).
			WithContext("operation", "list_models")
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError("kimi", c.model,
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
			WithContext("provider", "kimi")
	}

	models := make([]string, len(result.Data))
	for i, m := range result.Data {
		models[i] = m.ID
	}

	return models, nil
}

// GetSupportedModels 获取 Kimi 支持的模型列表（静态）
func (c *KimiClient) GetSupportedModels() []string {
	return []string{
		"moonshot-v1-8k",   // 8K 上下文窗口
		"moonshot-v1-32k",  // 32K 上下文窗口
		"moonshot-v1-128k", // 128K 上下文窗口
	}
}

// GetModelContextSize 获取模型的上下文大小
func (c *KimiClient) GetModelContextSize(model string) int {
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
func (c *KimiClient) EstimateTokenCount(text string) int {
	// 简单估算：中英文混合内容平均每个字符 0.75 tokens
	return len(text) * 3 / 4
}

// 辅助方法

func (c *KimiClient) getModel(model string) string {
	if model != "" {
		return model
	}
	return c.model
}

func (c *KimiClient) getTemperature(temp float64) float64 {
	if temp > 0 {
		return temp
	}
	return c.temperature
}

func (c *KimiClient) getMaxTokens(maxTokens int) int {
	if maxTokens > 0 {
		return maxTokens
	}
	return c.maxTokens
}

// WithModel 设置模型
func (c *KimiClient) WithModel(model string) *KimiClient {
	c.model = model
	return c
}

// WithTemperature 设置温度
func (c *KimiClient) WithTemperature(temperature float64) *KimiClient {
	c.temperature = temperature
	return c
}

// WithMaxTokens 设置最大 token 数
func (c *KimiClient) WithMaxTokens(maxTokens int) *KimiClient {
	c.maxTokens = maxTokens
	return c
}

// CalculateFileUploadTokens 计算文件上传所需的 token 数
// Kimi 支持文件上传，需要计算文件内容的 token 数
func (c *KimiClient) CalculateFileUploadTokens(fileContent string) int {
	return c.EstimateTokenCount(fileContent)
}

// ValidateContextSize 验证消息是否超过模型的上下文限制
func (c *KimiClient) ValidateContextSize(messages []agentllm.Message) error {
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += c.EstimateTokenCount(msg.Content)
	}

	maxContext := c.GetModelContextSize(c.model)
	if totalTokens > maxContext {
		return agentErrors.NewInvalidInputError("kimi", "messages",
			fmt.Sprintf("estimated tokens (%d) exceed model context size (%d)", totalTokens, maxContext))
	}

	return nil
}
