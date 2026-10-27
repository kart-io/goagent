package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/tools"
)

func requestBudget(method string) int {
	switch method {
	case "RunnableToolLoop", "Embedding":
		return 2
	case "RunnableSequentialTools":
		return 3
	default:
		return 1
	}
}

func checkNative(ctx context.Context, p *providers.GeminiProvider, method string) (r result) {
	r.Method = method
	r.Details = map[string]any{}
	start := time.Now()
	defer func() { r.DurationMS = time.Since(start).Milliseconds() }()
	if method == "Embedding" {
		return checkEmbedding(ctx, p, r)
	}
	prompt, expected := "计算 17+25，只输出整数。", "42"
	var orders []string
	var expectedOrders []string
	b := builder.NewSimpleBuilder(p).WithSystemPrompt("完成用户任务。存在 lookup 工具时必须先调用工具。依据结果回答，最终只输出答案，不加解释。").WithMaxIterations(requestBudget(method)).WithMaxTokens(2048).WithTemperature(0.2).WithTimeout(90 * time.Second)
	if method == "RunnableToolLoop" || method == "RunnableDirectTool" || method == "RunnableSequentialTools" {
		prompt = "查询订单 SHARED-1 的退款期限，只输出天数数字。"
		expected = "7"
		expectedOrders = []string{"SHARED-1"}
		if method == "RunnableDirectTool" {
			prompt = "查询订单 SHARED-1，返回完整工具文本。"
			expected = "订单 SHARED-1 已发货。"
			b.WithReturnDirectTools("lookup")
		}
		if method == "RunnableSequentialTools" {
			prompt = "先查询 FIRST-1，再根据查询结果中的关联订单号查询该关联订单，最后只输出关联订单的退款期限天数数字。"
			expectedOrders = []string{"FIRST-1", "SECOND-2"}
		}
		tool := tools.NewBaseTool("lookup", "按 order_id 查询订单资料", `{"type":"object","properties":{"order_id":{"type":"string"}},"required":["order_id"],"additionalProperties":false}`, func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			id, ok := input.Args["order_id"].(string)
			if !ok || len(input.Args) != 1 {
				return nil, fmt.Errorf("invalid order arguments")
			}
			orders = append(orders, id)
			text := "NOT_FOUND"
			switch id {
			case "SHARED-1":
				text = "配送时效为3天，退款期限为7天。"
				if method == "RunnableDirectTool" {
					text = expected
				}
			case "FIRST-1":
				text = "关联订单号为 SECOND-2。"
			case "SECOND-2":
				text = "退款期限为7天。"
			}
			return &interfaces.ToolOutput{Success: true, Result: text}, nil
		})
		b.WithTools(tool)
	}
	a, err := b.Build()
	if err != nil {
		r.Error = err.Error()
		return r
	}
	var out *builder.AgentOutput
	if method == "RunnableStream" {
		stream, streamErr := a.AsRunnable().Stream(ctx, prompt)
		err = streamErr
		var text strings.Builder
		if err == nil {
			for c := range stream {
				if c.Error != nil {
					err = c.Error
				}
				if c.Event != nil {
					text.WriteString(c.Event.Delta)
				}
				if c.Done {
					out = c.Data
				}
			}
		}
		r.Details["stream_text"] = text.String()
	} else {
		out, err = a.AsRunnable().Invoke(ctx, prompt)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		r.Error = err.Error()
		return r
	}
	if out == nil {
		r.Error = "missing final output"
		return r
	}
	r.Answer = fmt.Sprint(out.Result)
	r.Usage = out.TokenUsage
	r.Details["agent_metadata"] = out.Metadata
	r.Details["executed_order_ids"] = orders
	r.Passed = strings.TrimSpace(r.Answer) == expected && reflect.DeepEqual(orders, expectedOrders)
	if method == "RunnableStream" {
		r.Passed = r.Passed && strings.TrimSpace(fmt.Sprint(r.Details["stream_text"])) == expected
	}
	if len(expectedOrders) > 0 {
		r.Passed = r.Passed && out.Metadata["model_calls"] == requestBudget(method) && out.Metadata["usage_complete"] == true
	}
	return r
}

func checkEmbedding(ctx context.Context, p *providers.GeminiProvider, r result) result {
	texts := []string{"订单已发货，预计明天送达。", "The galaxy contains billions of stars and distant planets."}
	vectors := make([][]float64, 0, 2)
	var hashes []string
	for i, text := range texts {
		r.Details["embedding_requests"] = i + 1
		v, err := p.Embed(ctx, text)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		data := make([]byte, 8*len(v))
		var norm float64
		for j, x := range v {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				r.Error = "non-finite vector"
				return r
			}
			binary.LittleEndian.PutUint64(data[j*8:], math.Float64bits(x))
			norm += x * x
		}
		if len(v) == 0 || norm == 0 {
			r.Error = "invalid vector"
			return r
		}
		hash := sha256.Sum256(data)
		hashes = append(hashes, hex.EncodeToString(hash[:]))
		vectors = append(vectors, v)
	}
	if len(vectors[0]) != len(vectors[1]) {
		r.Error = "inconsistent dimensions"
		return r
	}
	var dot, normA, normB float64
	for i, a := range vectors[0] {
		b := vectors[1][i]
		dot += a * b
		normA += a * a
		normB += b * b
	}
	r.Details["dimensions"] = len(vectors[0])
	r.Details["vector_sha256"] = hashes
	r.Details["cosine_similarity"] = dot / math.Sqrt(normA*normB)
	r.Details["input_texts"] = texts
	r.Passed = hashes[0] != hashes[1]
	r.Answer = "distinct finite vectors"
	return r
}
