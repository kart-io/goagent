package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/interfaces"
)

type plainClient struct{}

func (plainClient) CompleteStream(context.Context, *CompletionRequest) (<-chan *StreamChunk, error) {
	return nil, nil
}
func (plainClient) Provider() shared.Provider { return shared.ProviderCustom }

type embedClient struct{ plainClient }

func (embedClient) Embed(context.Context, string) ([]float32, error) { return nil, nil }

type batchEmbedClient struct{ embedClient }

func (batchEmbedClient) EmbedBatch(context.Context, []string) ([][]float32, error) { return nil, nil }

// deniedEmbedClient 实现了 Embed，但通过能力声明显式拒绝。
type deniedEmbedClient struct{ batchEmbedClient }

func (deniedEmbedClient) HasCapability(c Capability) bool { return c != CapabilityEmbedding }
func (deniedEmbedClient) Capabilities() []Capability      { return nil }

type toolStreamClient struct{ plainClient }

func (toolStreamClient) CompleteToolsStream(context.Context, *CompletionRequest, []interfaces.Tool) (<-chan *StreamChunk, error) {
	return nil, nil
}

func TestCapabilityHelpers(t *testing.T) {
	cases := []struct {
		name                       string
		client                     Client
		embedding, batch, toolSync bool
	}{
		{name: "仅基础接口", client: plainClient{}},
		{name: "单条嵌入", client: embedClient{}, embedding: true},
		{name: "批量嵌入", client: batchEmbedClient{}, embedding: true, batch: true},
		{name: "能力声明拒绝嵌入", client: deniedEmbedClient{}},
		{name: "原生工具流", client: toolStreamClient{}, toolSync: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.embedding, HasEmbedding(c.client))
			require.Equal(t, c.embedding, AsEmbedder(c.client) != nil)
			require.Equal(t, c.batch, AsBatchEmbedder(c.client) != nil)
			require.Equal(t, c.toolSync, HasToolStreaming(c.client))
			require.Equal(t, c.toolSync, AsToolStreamClient(c.client) != nil)
		})
	}
}
