package providers

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/utils/json"
	"io"
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/utils/httpclient"
)

// OllamaClient Ollama LLM 客户端
type OllamaClient struct {
	baseURL     string
	model       string
	temperature float64
	maxTokens   int
	client      *httpclient.Client
}

// OllamaConfig Ollama 配置
type OllamaConfig struct {
	BaseURL     string  // Ollama API 地址，默认 http://localhost:11434
	Model       string  // 模型名称，如 llama2, codellama, mistral 等
	Temperature float64 // 温度参数
	MaxTokens   int     // 最大 token 数
	Timeout     int     // 请求超时（秒）
}

// DefaultOllamaConfig 返回默认 Ollama 配置
func DefaultOllamaConfig() *OllamaConfig {
	return &OllamaConfig{
		BaseURL:     "http://localhost:11434",
		Model:       "llama2",
		Temperature: 0.7,
		MaxTokens:   2000,
		Timeout:     120, // Ollama 可能需要更长的超时时间
	}
}

// NewOllama 使用标准配置创建 Ollama 客户端
func NewOllama(config *llm.Config) (*OllamaClient, error) {
	ollamaConfig := &OllamaConfig{
		BaseURL:     config.BaseURL,
		Model:       config.Model,
		Temperature: config.Temperature,
		MaxTokens:   config.MaxTokens,
		Timeout:     config.Timeout,
	}

	// 设置默认值
	if ollamaConfig.BaseURL == "" {
		ollamaConfig.BaseURL = "http://localhost:11434"
	}
	if ollamaConfig.Model == "" {
		ollamaConfig.Model = "llama2"
	}
	if ollamaConfig.Temperature == 0 {
		ollamaConfig.Temperature = 0.7
	}
	if ollamaConfig.MaxTokens == 0 {
		ollamaConfig.MaxTokens = 2000
	}
	if ollamaConfig.Timeout == 0 {
		ollamaConfig.Timeout = 120
	}

	return NewOllamaClient(ollamaConfig), nil
}

// NewOllamaClient 创建新的 Ollama 客户端
func NewOllamaClient(config *OllamaConfig) *OllamaClient {
	if config == nil {
		config = DefaultOllamaConfig()
	}

	if config.BaseURL == "" {
		config.BaseURL = "http://localhost:11434"
	}

	if config.Model == "" {
		config.Model = "llama2"
	}

	if config.Temperature == 0 {
		config.Temperature = 0.7
	}

	if config.MaxTokens == 0 {
		config.MaxTokens = 2000
	}

	if config.Timeout == 0 {
		config.Timeout = 120
	}

	return &OllamaClient{
		baseURL:     strings.TrimRight(config.BaseURL, "/"),
		model:       config.Model,
		temperature: config.Temperature,
		maxTokens:   config.MaxTokens,
		client: httpclient.NewClient(&httpclient.Config{
			Timeout: time.Duration(config.Timeout) * time.Second,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}),
	}
}

// NewOllamaClientSimple 使用默认配置创建 Ollama 客户端
func NewOllamaClientSimple(model string) *OllamaClient {
	config := DefaultOllamaConfig()
	if model != "" {
		config.Model = model
	}
	return NewOllamaClient(config)
}

// ollamaChatRequest Ollama 聊天请求格式
type ollamaChatRequest struct {
	Model    string                 `json:"model"`
	Messages []ollamaMessage        `json:"messages"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// ollamaMessage Ollama 消息格式
type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaChatResponse Ollama 聊天响应格式
type ollamaChatResponse struct {
	Model              string        `json:"model"`
	CreatedAt          string        `json:"created_at"`
	Message            ollamaMessage `json:"message"`
	Done               bool          `json:"done"`
	TotalDuration      int64         `json:"total_duration,omitempty"`
	LoadDuration       int64         `json:"load_duration,omitempty"`
	PromptEvalCount    int           `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64         `json:"prompt_eval_duration,omitempty"`
	EvalCount          int           `json:"eval_count,omitempty"`
	EvalDuration       int64         `json:"eval_duration,omitempty"`
	Context            []int         `json:"context,omitempty"`
}

// ollamaGenerateRequest Ollama 生成请求格式
type ollamaGenerateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

// ollamaGenerateResponse Ollama 生成响应格式
type ollamaGenerateResponse struct {
	Model              string `json:"model"`
	CreatedAt          string `json:"created_at"`
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	Context            []int  `json:"context,omitempty"`
	TotalDuration      int64  `json:"total_duration,omitempty"`
	LoadDuration       int64  `json:"load_duration,omitempty"`
	PromptEvalCount    int    `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64  `json:"prompt_eval_duration,omitempty"`
	EvalCount          int    `json:"eval_count,omitempty"`
	EvalDuration       int64  `json:"eval_duration,omitempty"`
}

// Complete 实现 llm.Client 接口的 Complete 方法
func (c *OllamaClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// 构建 prompt
	var prompt string
	if len(req.Messages) > 0 {
		// 将消息转换为 prompt
		for _, msg := range req.Messages {
			switch msg.Role {
			case "system":
				prompt += fmt.Sprintf("System: %s\n", msg.Content)
			case "user":
				prompt += fmt.Sprintf("User: %s\n", msg.Content)
			case "assistant":
				prompt += fmt.Sprintf("Assistant: %s\n", msg.Content)
			}
		}
		prompt += "Assistant: "
	} else {
		return nil, agentErrors.NewInvalidInputError("ollama", "messages", "no messages provided")
	}

	// 构建请求
	ollamaReq := ollamaGenerateRequest{
		Model:  c.getModel(req.Model),
		Prompt: prompt,
		Stream: false,
		Options: map[string]interface{}{
			"temperature": c.getTemperature(req.Temperature),
			"num_predict": c.getMaxTokens(req.MaxTokens),
		},
	}

	if len(req.Stop) > 0 {
		ollamaReq.Options["stop"] = req.Stop
	}

	if req.TopP > 0 {
		ollamaReq.Options["top_p"] = req.TopP
	}

	// 发送请求
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(ollamaReq).
		Post(c.baseURL + "/api/generate")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError("ollama", c.getModel(req.Model), err)
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError("ollama", c.getModel(req.Model),
			fmt.Sprintf("API error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	// 解析响应
	var ollamaResp ollamaGenerateResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&ollamaResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("response body", err).
			WithContext("provider", "ollama")
	}

	// 构建响应
	return &llm.CompletionResponse{
		Content:      strings.TrimSpace(ollamaResp.Response),
		Model:        ollamaResp.Model,
		TokensUsed:   ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		FinishReason: c.getFinishReason(ollamaResp.Done),
		Provider:     string(llm.ProviderOllama),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}, nil
}

// Chat 实现 llm.Client 接口的 Chat 方法
func (c *OllamaClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	// 转换消息格式
	ollamaMessages := make([]ollamaMessage, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = ollamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// 构建请求
	ollamaReq := ollamaChatRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Stream:   false,
		Options: map[string]interface{}{
			"temperature": c.temperature,
			"num_predict": c.maxTokens,
		},
	}

	// 发送请求
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(ollamaReq).
		Post(c.baseURL + "/api/chat")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError("ollama", c.model, err).
			WithContext("operation", "chat")
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError("ollama", c.model,
			fmt.Sprintf("chat API error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	// 解析响应
	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&ollamaResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("chat response body", err).
			WithContext("provider", "ollama")
	}

	// 构建响应
	return &llm.CompletionResponse{
		Content:      strings.TrimSpace(ollamaResp.Message.Content),
		Model:        ollamaResp.Model,
		TokensUsed:   ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		FinishReason: c.getFinishReason(ollamaResp.Done),
		Provider:     string(llm.ProviderOllama),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}, nil
}

// Provider 返回提供商类型
func (c *OllamaClient) Provider() llm.Provider {
	return llm.ProviderOllama
}

// IsAvailable 检查 Ollama 是否可用
func (c *OllamaClient) IsAvailable() bool {
	// 尝试调用 API 检查服务是否可用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.client.R().
		SetContext(ctx).
		Get(c.baseURL + "/api/tags")

	if err != nil {
		return false
	}

	return resp.IsSuccess()
}

// ListModels 列出可用的模型
func (c *OllamaClient) ListModels() ([]string, error) {
	resp, err := c.client.R().
		Get(c.baseURL + "/api/tags")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError("ollama", c.model, err).
			WithContext("operation", "list_models")
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError("ollama", c.model,
			fmt.Sprintf("list models error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&result); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("models list response", err).
			WithContext("provider", "ollama")
	}

	models := make([]string, len(result.Models))
	for i, m := range result.Models {
		models[i] = m.Name
	}

	return models, nil
}

// PullModel 拉取模型
func (c *OllamaClient) PullModel(modelName string) error {
	pullReq := map[string]interface{}{
		"name": modelName,
	}

	// 使用更长的超时时间用于模型下载
	pullClient := httpclient.NewClient(&httpclient.Config{
		Timeout: 30 * time.Minute,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	})

	resp, err := pullClient.Resty().R().
		SetBody(pullReq).
		Post(c.baseURL + "/api/pull")

	if err != nil {
		return agentErrors.NewLLMRequestError("ollama", modelName, err).
			WithContext("operation", "pull_model")
	}

	if !resp.IsSuccess() {
		return agentErrors.NewLLMResponseError("ollama", modelName,
			fmt.Sprintf("pull model error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	// 读取流式响应
	decoder := json.NewDecoder(strings.NewReader(resp.String()))
	for {
		var status map[string]interface{}
		if err := decoder.Decode(&status); err != nil {
			if err == io.EOF {
				break
			}
			return agentErrors.NewParserInvalidJSONError("pull model response stream", err).
				WithContext("provider", "ollama")
		}
		// 可以在这里添加进度显示逻辑
	}

	return nil
}

// 辅助方法

func (c *OllamaClient) getModel(model string) string {
	if model != "" {
		return model
	}
	return c.model
}

func (c *OllamaClient) getTemperature(temp float64) float64 {
	if temp > 0 {
		return temp
	}
	return c.temperature
}

func (c *OllamaClient) getMaxTokens(maxTokens int) int {
	if maxTokens > 0 {
		return maxTokens
	}
	return c.maxTokens
}

func (c *OllamaClient) getFinishReason(done bool) string {
	if done {
		return "complete"
	}
	return "length"
}

// WithModel 设置模型
func (c *OllamaClient) WithModel(model string) *OllamaClient {
	c.model = model
	return c
}

// WithTemperature 设置温度
func (c *OllamaClient) WithTemperature(temperature float64) *OllamaClient {
	c.temperature = temperature
	return c
}

// WithMaxTokens 设置最大 token 数
func (c *OllamaClient) WithMaxTokens(maxTokens int) *OllamaClient {
	c.maxTokens = maxTokens
	return c
}
