package llm

import (
	"context"
	agentErrors "github.com/kart-io/goagent/errors"
	"io"
	"strings"
)

// Complete 发起流式请求并收集为完整响应。
// 只有成功终止块才算完成：块上的错误原样返回，流在终止块前关闭视为 io.ErrUnexpectedEOF。
// 请求已取消或超时时不调用提供方；读流与取消同时就绪时返回 ctx 的错误。
// 返回前取消内部 ctx，提供方的读取 goroutine 与连接随之释放。
func Complete(ctx context.Context, client Client, req *CompletionRequest) (*CompletionResponse, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stream, err := client.CompleteStream(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, agentErrors.New(agentErrors.CodeLLMResponse, "模型返回空流").WithComponent("llm").WithOperation("complete")
	}
	var deltas strings.Builder
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case chunk, ok := <-stream:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !ok {
				return nil, io.ErrUnexpectedEOF
			}
			if chunk == nil {
				continue
			}
			if chunk.Error != nil {
				return nil, chunk.Error
			}
			deltas.WriteString(chunk.Delta)
			if !chunk.Done {
				continue
			}
			response := &CompletionResponse{
				Content:       chunk.Content,
				Model:         chunk.ResponseModel,
				ResponseModel: chunk.ResponseModel,
				FinishReason:  chunk.FinishReason,
				Provider:      string(client.Provider()),
			}
			if response.Content == "" {
				response.Content = deltas.String()
			}
			if response.Model == "" && req != nil {
				response.Model = req.Model
			}
			if chunk.Usage != nil {
				tokens := chunk.Usage.Tokens()
				response.Usage = &tokens
				response.TokensUsed = tokens.TotalTokens
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return response, nil
		}
	}
}

// Chat 以消息列表调用 Complete。
func Chat(ctx context.Context, client Client, messages []Message) (*CompletionResponse, error) {
	return Complete(ctx, client, &CompletionRequest{Messages: messages})
}
