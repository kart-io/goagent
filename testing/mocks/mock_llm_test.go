package mocks_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/mocks"
)

// 公开的 Mock 必须能直接交给 Builder，包括配置了工具的场景。
func TestMockLLMClientWorksWithBuilder(t *testing.T) {
	client := mocks.NewMockLLMClient()
	client.SetResponses(llm.CompletionResponse{Content: "预设回答", TokensUsed: 3})
	agent, err := builder.NewSimpleBuilder(client).WithTools(mocks.NewMockTool("lookup", "查询")).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "问题")
	require.NoError(t, err)
	require.Equal(t, "预设回答", out.Result)
	require.Len(t, client.GetRequestHistory(), 1)

	client.SetError(true, "API 调用失败")
	_, err = agent.AsRunnable().Invoke(context.Background(), "问题")
	require.ErrorContains(t, err, "API 调用失败")
}

func TestMockStreamingLLMClientEmitsChunksThenDone(t *testing.T) {
	client := mocks.NewMockStreamingLLMClient()
	client.SetStreamChunks("这是", "流式", "响应")
	stream, err := client.CompleteStream(context.Background(), &llm.CompletionRequest{Messages: []llm.Message{llm.UserMessage("q")}})
	require.NoError(t, err)
	var text strings.Builder
	var last *llm.StreamChunk
	for chunk := range stream {
		text.WriteString(chunk.Delta)
		last = chunk
	}
	require.Equal(t, "这是流式响应", text.String())
	require.True(t, last.Done)
	require.Equal(t, "这是流式响应", last.Content)
	require.Len(t, client.GetRequestHistory(), 1)
}
