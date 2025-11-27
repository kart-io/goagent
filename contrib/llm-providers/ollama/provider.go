package ollama

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/kart-io/goagent/llm/constants"
	"github.com/kart-io/goagent/llm/registry"
	"github.com/kart-io/goagent/utils/json"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/httpclient"
)

func init() {
	// 自动注册 Ollama provider 到全局注册表
	registry.Register(constants.ProviderOllama, func(opts ...agentllm.ClientOption) (agentllm.Client, error) {
		return New(opts...)
	})
}

// Provider Ollama LLM 客户端
// Ollama 支持本地运行开源大语言模型
type Provider struct {
	*common.BaseProvider
	baseURL string
	client  *httpclient.Client
}

// New 使用选项模式创建 Ollama provider
func New(opts ...agentllm.ClientOption) (*Provider, error) {
	// 创建 BaseProvider，统一处理 Options
	base := common.NewBaseProvider(opts...)

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
	client := base.NewHTTPClient(common.HTTPClientConfig{
		Timeout: timeout,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		BaseURL: base.Config.BaseURL,
	})

	return &Provider{
		BaseProvider: base,
		baseURL:      strings.TrimRight(base.Config.BaseURL, "/"),
		client:       client,
	}, nil
}

// chatRequest Ollama 聊天请求格式
type chatRequest struct {
	Model    string                 `json:"model"`
	Messages []message              `json:"messages"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// message Ollama 消息格式
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse Ollama 聊天响应格式
type chatResponse struct {
	Model              string  `json:"model"`
	CreatedAt          string  `json:"created_at"`
	Message            message `json:"message"`
	Done               bool    `json:"done"`
	TotalDuration      int64   `json:"total_duration,omitempty"`
	LoadDuration       int64   `json:"load_duration,omitempty"`
	PromptEvalCount    int     `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64   `json:"prompt_eval_duration,omitempty"`
	EvalCount          int     `json:"eval_count,omitempty"`
	EvalDuration       int64   `json:"eval_duration,omitempty"`
	Context            []int   `json:"context,omitempty"`
}

// generateRequest Ollama 生成请求格式
type generateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

// generateResponse Ollama 生成响应格式
type generateResponse struct {
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
func (p *Provider) Complete(ctx context.Context, req *agentllm.CompletionRequest) (*agentllm.CompletionResponse, error) {
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
	genReq := generateRequest{
		Model:  p.GetModel(req.Model),
		Prompt: prompt,
		Stream: false,
		Options: map[string]interface{}{
			"temperature": p.GetTemperature(req.Temperature),
			"num_predict": p.GetMaxTokens(req.MaxTokens),
		},
	}

	if len(req.Stop) > 0 {
		genReq.Options["stop"] = req.Stop
	}

	if req.TopP > 0 {
		genReq.Options["top_p"] = req.TopP
	}

	// 发送请求
	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(genReq).
		Post(p.baseURL + "/api/generate")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), p.GetModel(req.Model), err)
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), p.GetModel(req.Model),
			fmt.Sprintf("API error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	// 解析响应
	var genResp generateResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&genResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("response body", err).
			WithContext("provider", p.ProviderName())
	}

	// 构建响应
	return &agentllm.CompletionResponse{
		Content:      strings.TrimSpace(genResp.Response),
		Model:        genResp.Model,
		TokensUsed:   genResp.PromptEvalCount + genResp.EvalCount,
		FinishReason: p.getFinishReason(genResp.Done),
		Provider:     string(constants.ProviderOllama),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     genResp.PromptEvalCount,
			CompletionTokens: genResp.EvalCount,
			TotalTokens:      genResp.PromptEvalCount + genResp.EvalCount,
		},
	}, nil
}

// Chat 实现 llm.Client 接口的 Chat 方法
func (p *Provider) Chat(ctx context.Context, messages []agentllm.Message) (*agentllm.CompletionResponse, error) {
	// 转换消息格式
	ollamaMessages := make([]message, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = message{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// 使用 BaseProvider 的统一参数处理方法
	model := p.GetModel("")
	maxTokens := p.GetMaxTokens(0)
	temperature := p.GetTemperature(0)

	// 构建请求
	chatReq := chatRequest{
		Model:    model,
		Messages: ollamaMessages,
		Stream:   false,
		Options: map[string]interface{}{
			"temperature": temperature,
			"num_predict": maxTokens,
		},
	}

	// 发送请求
	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(chatReq).
		Post(p.baseURL + "/api/chat")

	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("operation", "chat")
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model,
			fmt.Sprintf("chat API error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	// 解析响应
	var chatResp chatResponse
	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&chatResp); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("chat response body", err).
			WithContext("provider", p.ProviderName())
	}

	// 构建响应
	return &agentllm.CompletionResponse{
		Content:      strings.TrimSpace(chatResp.Message.Content),
		Model:        chatResp.Model,
		TokensUsed:   chatResp.PromptEvalCount + chatResp.EvalCount,
		FinishReason: p.getFinishReason(chatResp.Done),
		Provider:     string(constants.ProviderOllama),
		Usage: &interfaces.TokenUsage{
			PromptTokens:     chatResp.PromptEvalCount,
			CompletionTokens: chatResp.EvalCount,
			TotalTokens:      chatResp.PromptEvalCount + chatResp.EvalCount,
		},
	}, nil
}

// Provider 返回提供商类型
func (p *Provider) Provider() constants.Provider {
	return constants.ProviderOllama
}

// ProviderName 返回提供商名称
func (p *Provider) ProviderName() string {
	return string(constants.ProviderOllama)
}

// IsAvailable 检查 Ollama 是否可用
func (p *Provider) IsAvailable() bool {
	// 尝试调用 API 检查服务是否可用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := p.client.R().
		SetContext(ctx).
		Get(p.baseURL + "/api/tags")

	if err != nil {
		return false
	}

	return resp.IsSuccess()
}

// ListModels 列出可用的模型
func (p *Provider) ListModels() ([]string, error) {
	resp, err := p.client.R().
		Get(p.baseURL + "/api/tags")

	model := p.GetModel("")
	if err != nil {
		return nil, agentErrors.NewLLMRequestError(p.ProviderName(), model, err).
			WithContext("operation", "list_models")
	}

	if !resp.IsSuccess() {
		return nil, agentErrors.NewLLMResponseError(p.ProviderName(), model,
			fmt.Sprintf("list models error (status %d): %s", resp.StatusCode(), resp.String()))
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(strings.NewReader(resp.String())).Decode(&result); err != nil {
		return nil, agentErrors.NewParserInvalidJSONError("models list response", err).
			WithContext("provider", p.ProviderName())
	}

	models := make([]string, len(result.Models))
	for i, m := range result.Models {
		models[i] = m.Name
	}

	return models, nil
}

// PullModel 拉取模型到本地
func (p *Provider) PullModel(modelName string) error {
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
		Post(p.baseURL + "/api/pull")

	if err != nil {
		return agentErrors.NewLLMRequestError(p.ProviderName(), modelName, err).
			WithContext("operation", "pull_model")
	}

	if !resp.IsSuccess() {
		return agentErrors.NewLLMResponseError(p.ProviderName(), modelName,
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
				WithContext("provider", p.ProviderName())
		}
		// 可以在这里添加进度显示逻辑
	}

	return nil
}

// 辅助方法

func (p *Provider) getFinishReason(done bool) string {
	if done {
		return "complete"
	}
	return "length"
}

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
