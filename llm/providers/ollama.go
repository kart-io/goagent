package providers

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/utils/json"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/httpclient"
)

// OllamaClient Ollama LLM 客户端
type OllamaClient struct {
	*BaseProvider
	baseURL string
	client  *httpclient.Client
}

// NewOllamaWithOptions 使用选项模式创建 Ollama 客户端
func NewOllamaWithOptions(opts ...agentllm.ClientOption) (*OllamaClient, error) {
	// 创建 BaseProvider，统一处理 Options
	base := NewBaseProvider(opts...)

	// 应用 Provider 特定的默认值（Ollama 不需要 API Key）
	base.ApplyProviderDefaults(
		constants.ProviderOllama,
		"http://localhost:11434",
		"llama2",
		constants.EnvOllamaBaseURL,
		constants.EnvOllamaModel,
	)

	// 设置超时时间，Ollama 默认需要更长的超时
	timeout := base.GetTimeout()
	if timeout == constants.DefaultTimeout {
		timeout = 120 * time.Second
	}

	// 使用 BaseProvider 的 NewHTTPClient 方法创建 HTTP 客户端
	client := base.NewHTTPClient(HTTPClientConfig{
		Timeout: timeout,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		BaseURL: base.Config.BaseURL,
	})

	return &OllamaClient{
		BaseProvider: base,
		baseURL:      strings.TrimRight(base.Config.BaseURL, "/"),
		client:       client,
	}, nil
}

// NewOllama 使用标准配置创建 Ollama 客户端 (向后兼容)
func NewOllama(config *agentllm.LLMOptions) (*OllamaClient, error) {
	// 将现有配置转换为 Options，使用 Options 模式创建 Provider
	return NewOllamaWithOptions(ConfigToOptions(config)...)
}

// NewOllamaClientSimple 使用默认配置创建 Ollama 客户端（便捷函数）
func NewOllamaClientSimple(model string) (*OllamaClient, error) {
	config := &agentllm.LLMOptions{
		Provider: constants.ProviderOllama,
		Model:    model,
	}
	if model == "" {
		config.Model = "llama2"
	}
	return NewOllama(config)
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
func (c *OllamaClient) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
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
		Model:  c.GetModel(req.Model),
		Prompt: prompt,
		Stream: false,
		Options: map[string]interface{}{
			"temperature": c.GetTemperature(req.Temperature),
			"num_predict": c.GetMaxTokens(req.MaxTokens),
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
		return nil, agentErrors.NewLLMRequestError(c.ProviderName(), c.GetModel(req.Model), err)
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError(c.ProviderName(), c.GetModel(req.Model),
			fmt.Sprintf("API error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	// 解析响应
	var ollamaResp ollamaGenerateResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&ollamaResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("response body", err).
			WithContext("provider", c.ProviderName())
	}

	// 构建响应
	return &agentllm.CompletionResponse{
		Content:      strings.TrimSpace(ollamaResp.Response),
		Model:        ollamaResp.Model,
		TokensUsed:   ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		FinishReason: c.getFinishReason(ollamaResp.Done),
		Provider:     string(constants.ProviderOllama),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}, nil
}

// Chat 实现 llm.Client 接口的 Chat 方法
func (c *OllamaClient) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	// 转换消息格式
	ollamaMessages := make([]ollamaMessage, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = ollamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// 使用 BaseProvider 的统一参数处理方法
	model := c.GetModel("")
	maxTokens := c.GetMaxTokens(0)
	temperature := c.GetTemperature(0)

	// 构建请求
	ollamaReq := ollamaChatRequest{
		Model:    model,
		Messages: ollamaMessages,
		Stream:   false,
		Options: map[string]interface{}{
			"temperature": temperature,
			"num_predict": maxTokens,
		},
	}

	// 发送请求
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(ollamaReq).
		Post(c.baseURL + "/api/chat")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError(c.ProviderName(), model, err).
			WithContext("operation", "chat")
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError(c.ProviderName(), model,
			fmt.Sprintf("chat API error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	// 解析响应
	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&ollamaResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("chat response body", err).
			WithContext("provider", c.ProviderName())
	}

	// 构建响应
	return &agentllm.CompletionResponse{
		Content:      strings.TrimSpace(ollamaResp.Message.Content),
		Model:        ollamaResp.Model,
		TokensUsed:   ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		FinishReason: c.getFinishReason(ollamaResp.Done),
		Provider:     string(constants.ProviderOllama),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}, nil
}

// Provider 返回提供商类型
func (c *OllamaClient) Provider() constants.Provider {
	return constants.ProviderOllama
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

	model := c.GetModel("")
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(c.ProviderName(), model, err).
			WithContext("operation", "list_models")
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError(c.ProviderName(), model,
			fmt.Sprintf("list models error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&result); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("models list response", err).
			WithContext("provider", c.ProviderName())
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
		return agentErrors.NewLLMRequestError(c.ProviderName(), modelName, err).
			WithContext("operation", "pull_model")
	}

	if !resp.IsSuccess() {
		return agentErrors.NewLLMResponseError(c.ProviderName(), modelName,
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
				WithContext("provider", c.ProviderName())
		}
		// 可以在这里添加进度显示逻辑
	}

	return nil
}

// 辅助方法

func (c *OllamaClient) getFinishReason(done bool) string {
	if done {
		return "complete"
	}
	return "length"
}

// WithModel 设置模型
func (c *OllamaClient) WithModel(model string) *OllamaClient {
	c.Config.Model = model
	return c
}

// WithTemperature 设置温度
func (c *OllamaClient) WithTemperature(temperature float64) *OllamaClient {
	c.Config.Temperature = temperature
	return c
}

// WithMaxTokens 设置最大 token 数
func (c *OllamaClient) WithMaxTokens(maxTokens int) *OllamaClient {
	c.Config.MaxTokens = maxTokens
	return c
}
