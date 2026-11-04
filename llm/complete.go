package llm

import (
	"context"
	"io"
	"strings"
)

// Complete 发起流式请求并收集为完整响应。
// 只有成功终止块才算完成：块上的错误原样返回，流在终止块前关闭视为 io.ErrUnexpectedEOF。
// 返回前取消内部 ctx，提供方的读取 goroutine 与连接随之释放。
func Complete(ctx context.Context, client Client, req *CompletionRequest) (*CompletionResponse, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := client.CompleteStream(ctx, req)
	if err != nil {
		return nil, err
	}
	var deltas strings.Builder
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case chunk, ok := <-stream:
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
			return response, nil
		}
	}
}

// Chat 以消息列表调用 Complete。
func Chat(ctx context.Context, client Client, messages []Message) (*CompletionResponse, error) {
	return Complete(ctx, client, &CompletionRequest{Messages: messages})
}
