// Package livemodel 为编排案例接入真实模型（DeepSeek、MiniMax、Gemini）。
// 客户端不做隐式重试，并统计真实发出的请求数，回放不会进入传输层。
package livemodel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

// 案例的单次请求上限；超出说明模型或网络异常，交给调用方处理。
const (
	MaxTokens      = 2048
	RequestTimeout = 90 * time.Second
)

// Config 由环境变量提供，避免把密钥写进命令行历史。
type Config struct {
	Provider, Model string
}

// ConfigFromEnv 读取 GOAGENT_LIVE_PROVIDER（默认 deepseek）及 <PROVIDER>_MODEL。
func ConfigFromEnv() (Config, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("GOAGENT_LIVE_PROVIDER")))
	if provider == "" {
		provider = "deepseek"
	}
	defaults := map[string]string{"deepseek": "deepseek-flash", "minimax": "MiniMax-M3.1-Flash", "gemini": "gemini-2.5-flash"}
	model, ok := defaults[provider]
	if !ok {
		return Config{}, fmt.Errorf("GOAGENT_LIVE_PROVIDER 只支持 deepseek、minimax 或 gemini")
	}
	if m := strings.TrimSpace(os.Getenv(strings.ToUpper(provider) + "_MODEL")); m != "" {
		model = m
	}
	return Config{Provider: provider, Model: model}, nil
}

// Client 统计真实传输请求；StreamMode 声明底层的实际流能力。
type Client struct {
	llm.ToolStreamClient
	Live  Config
	mode  core.StreamMode
	calls atomic.Int64
}

// New 创建不重试的真实模型客户端。
func New(cfg Config) (*Client, error) {
	switch cfg.Provider {
	case "deepseek":
		p, err := providers.NewDeepSeekWithOptions(llm.WithModel(cfg.Model), llm.WithMaxTokens(MaxTokens), llm.WithTemperature(0.2), llm.WithTimeout(RequestTimeout), llm.WithRetryCount(0))
		if err != nil {
			return nil, fmt.Errorf("创建 DeepSeek 客户端：%s", Redact(err.Error()))
		}
		return Wrap(p, cfg), nil
	case "minimax":
		m, err := newMiniMax(cfg.Model)
		if err != nil {
			return nil, err
		}
		return Wrap(m, cfg), nil
	case "gemini":
		p, err := providers.NewGeminiWithOptions(llm.WithModel(cfg.Model), llm.WithMaxTokens(MaxTokens), llm.WithTemperature(0.2), llm.WithTimeout(RequestTimeout), llm.WithRetryCount(0))
		if err != nil {
			return nil, fmt.Errorf("创建 Gemini 客户端：%s", Redact(err.Error()))
		}
		return Wrap(p, cfg), nil
	}
	return nil, fmt.Errorf("不支持的模型提供方 %q", cfg.Provider)
}

// Wrap 为任意客户端加上请求计数；未声明流能力的客户端按逐 token 流处理。
// 离线测试用它包装本地模拟模型，复用与真实模型相同的案例代码。
func Wrap(inner llm.ToolStreamClient, cfg Config) *Client {
	mode := core.StreamModeTokens
	if c, ok := inner.(core.StreamCapability); ok {
		mode = c.StreamMode()
	}
	return &Client{ToolStreamClient: inner, Live: cfg, mode: mode}
}

// FromEnv 组合 ConfigFromEnv 与 New。
func FromEnv() (*Client, error) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

// Calls 返回本客户端真实发出的模型请求数。
func (c *Client) Calls() int64                { return c.calls.Load() }
func (c *Client) StreamMode() core.StreamMode { return c.mode }

func (c *Client) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	c.calls.Add(1)
	return c.ToolStreamClient.CompleteStream(ctx, c.withModel(req))
}
func (c *Client) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	c.calls.Add(1)
	return c.ToolStreamClient.CompleteToolsStream(ctx, c.withModel(req), tools)
}
func (c *Client) ChatStream(ctx context.Context, messages []llm.Message) (<-chan *llm.StreamChunk, error) {
	return c.CompleteStream(ctx, &llm.CompletionRequest{Messages: messages})
}

// withModel 补齐 Builder 未设置的模型名与输出上限，不修改调用方请求。
func (c *Client) withModel(req *llm.CompletionRequest) *llm.CompletionRequest {
	if req == nil {
		return nil
	}
	copy := *req
	if copy.Model == "" {
		copy.Model = c.Live.Model
	}
	if copy.MaxTokens < 1 {
		copy.MaxTokens = MaxTokens
	}
	return &copy
}

// Redact 去掉错误信息中可能回显的密钥。
func Redact(s string) string {
	for _, name := range []string{"DEEPSEEK_API_KEY", "MINIMAX_API_KEY", "MINIMAX_CN_API_KEY", "GEMINI_API_KEY"} {
		if key := os.Getenv(name); key != "" {
			s = strings.ReplaceAll(s, key, "[REDACTED]")
		}
	}
	return s
}

var _ llm.ToolStreamClient = (*Client)(nil)
