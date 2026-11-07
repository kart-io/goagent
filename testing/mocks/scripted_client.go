package mocks

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

var _ llm.ToolStreamClient = (*ScriptedClient)(nil)

// ScriptedReply 是一条规则给出的回复。ToolCalls 非空时回复结构化工具调用（放在成功终止块中），否则回复 Text。
type ScriptedReply struct {
	Text      string
	ToolCalls []llm.ToolCall
}

// ScriptedRule 是一条“请求断言 → 回复”规则。Match 为 nil 时接受任意请求；
// Respond 非 nil 时按请求内容生成回复（可阻塞到 ctx 结束以模拟挂起的请求），否则使用 Reply。
type ScriptedRule struct {
	Name    string
	Match   func(*llm.CompletionRequest) bool
	Reply   ScriptedReply
	Respond func(context.Context, *llm.CompletionRequest) (ScriptedReply, error)
}

// ScriptedClient 按顺序消费规则：每次请求取下一条未用的规则，断言不成立或规则已用完时返回错误（严格模式），
// 测试据此发现意外的重放。请求历史深拷贝保存。可并发调用，但同一实例的请求按到达顺序消费规则，
// 并行分支应各用独立实例。
type ScriptedClient struct {
	mu       sync.Mutex
	rules    []ScriptedRule
	next     int
	hits     []int
	requests []llm.CompletionRequest
}

// NewScriptedClient 创建按 rules 顺序回复的客户端。
func NewScriptedClient(rules ...ScriptedRule) *ScriptedClient {
	return &ScriptedClient{rules: slices.Clone(rules), hits: make([]int, len(rules))}
}

// TextReply 返回回复文本的规则。
func TextReply(name, text string) ScriptedRule {
	return ScriptedRule{Name: name, Reply: ScriptedReply{Text: text}}
}

// ToolCallReply 返回回复结构化工具调用的规则。
func ToolCallReply(name string, calls ...llm.ToolCall) ScriptedRule {
	return ScriptedRule{Name: name, Reply: ScriptedReply{ToolCalls: calls}}
}

// ToolCall 构造带稳定 ID 的工具调用。
func ToolCall(id, tool, arguments string) llm.ToolCall {
	call := llm.ToolCall{ID: id, Type: "function"}
	call.Function.Name = tool
	call.Function.Arguments = arguments
	return call
}

// RequestContains 断言请求的全部消息中依次出现 parts 中的每一段。
func RequestContains(parts ...string) func(*llm.CompletionRequest) bool {
	return func(req *llm.CompletionRequest) bool {
		var all strings.Builder
		for _, m := range req.Messages {
			all.WriteString(m.Content)
			all.WriteString("\n")
		}
		text := all.String()
		for _, p := range parts {
			i := strings.Index(text, p)
			if i < 0 {
				return false
			}
			text = text[i+len(p):]
		}
		return true
	}
}

func (c *ScriptedClient) Provider() shared.Provider { return shared.ProviderCustom }

func (c *ScriptedClient) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return c.CompleteToolsStream(ctx, req, nil)
}

func (c *ScriptedClient) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	if req == nil {
		return nil, fmt.Errorf("ScriptedClient: 请求为 nil")
	}
	c.mu.Lock()
	c.requests = append(c.requests, copyRequest(req))
	call := len(c.requests)
	if c.next >= len(c.rules) {
		c.mu.Unlock()
		return nil, fmt.Errorf("ScriptedClient: 第 %d 次请求没有可用规则", call)
	}
	index := c.next
	rule := c.rules[index]
	if rule.Match != nil && !rule.Match(req) {
		c.mu.Unlock()
		return nil, fmt.Errorf("ScriptedClient: 第 %d 次请求不满足规则 %d（%s）的断言", call, index, rule.Name)
	}
	c.next++
	c.hits[index]++
	c.mu.Unlock()
	reply := rule.Reply
	if rule.Respond != nil {
		var err error
		if reply, err = rule.Respond(ctx, req); err != nil {
			return nil, err
		}
	}
	return replyStream(ctx, reply), nil
}

func replyStream(ctx context.Context, reply ScriptedReply) <-chan *llm.StreamChunk {
	usage := &llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}
	chunks := []*llm.StreamChunk{{Done: true, FinishReason: "stop", Usage: usage}}
	switch {
	case len(reply.ToolCalls) > 0:
		chunks = []*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: slices.Clone(reply.ToolCalls), Usage: usage}}
	case reply.Text != "":
		chunks = []*llm.StreamChunk{{Delta: reply.Text}, {Done: true, FinishReason: "stop", Content: reply.Text, Usage: usage}}
	}
	out := make(chan *llm.StreamChunk)
	go func() {
		defer close(out)
		for _, chunk := range chunks {
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func copyRequest(req *llm.CompletionRequest) llm.CompletionRequest {
	out := *req
	out.Stop = slices.Clone(req.Stop)
	out.Messages = make([]llm.Message, len(req.Messages))
	for i, m := range req.Messages {
		m.ToolCalls = slices.Clone(m.ToolCalls)
		if m.ProviderState != nil {
			state := *m.ProviderState
			state.Data = slices.Clone(state.Data)
			m.ProviderState = &state
		}
		out.Messages[i] = m
	}
	if req.Reasoning != nil {
		reasoning := *req.Reasoning
		out.Reasoning = &reasoning
	}
	return out
}

// Calls 返回收到的请求总数（含被拒绝的请求）。
func (c *ScriptedClient) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

// Hits 返回第 index 条规则被匹配的次数。
func (c *ScriptedClient) Hits(index int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[index]
}

// Remaining 返回尚未使用的规则数。
func (c *ScriptedClient) Remaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rules) - c.next
}

// Requests 返回请求历史的深拷贝，应在执行结束后读取。
func (c *ScriptedClient) Requests() []llm.CompletionRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]llm.CompletionRequest, len(c.requests))
	for i := range c.requests {
		out[i] = copyRequest(&c.requests[i])
	}
	return out
}
