package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

type tenantKey struct{}

// filteringRetriever 同时实现两种入口，用于确认配置了 Filter 时只走 Retrieve。
type filteringRetriever struct {
	requests []interfaces.RetrievalRequest
	plain    int
}

func (r *filteringRetriever) GetRelevantDocuments(context.Context, string) ([]*interfaces.Document, error) {
	r.plain++
	return []*interfaces.Document{{ID: "unfiltered", PageContent: "其他租户的数据"}}, nil
}

func (r *filteringRetriever) Retrieve(_ context.Context, req interfaces.RetrievalRequest) ([]*interfaces.Document, error) {
	r.requests = append(r.requests, req)
	return []*interfaces.Document{{ID: "t1-doc", PageContent: "租户 t1 的数据"}}, nil
}

func tenantFilter(ctx context.Context, _ string) (map[string]any, error) {
	tenant, _ := ctx.Value(tenantKey{}).(string)
	if tenant == "" {
		return nil, nil
	}
	return map[string]any{"tenant_id": tenant}, nil
}

func TestRetrievalFilterPassedToRequestRetriever(t *testing.T) {
	client := &evidenceLLM{MockLLMClient: NewMockLLMClient("Answer [1]")}
	retriever := &filteringRetriever{}
	agent, err := NewSimpleBuilder(client).WithRetriever(retriever, RetrievalConfig{Filter: tenantFilter}).Build()
	require.NoError(t, err)

	ctx := context.WithValue(context.Background(), tenantKey{}, "t1")
	out, err := agent.AsRunnable().Invoke(ctx, "退款政策？")
	require.NoError(t, err)

	require.Equal(t, []interfaces.RetrievalRequest{{Query: "退款政策？", Filter: map[string]any{"tenant_id": "t1"}}}, retriever.requests)
	require.Zero(t, retriever.plain, "配置 Filter 后不得走无过滤的入口")
	require.Equal(t, []RetrievalSource{{Reference: 1, ID: "t1-doc"}}, out.Metadata["retrieval_sources"])
}

func TestRetrievalFilterRequiresRequestRetriever(t *testing.T) {
	plain := documentRetrieverFunc(func(context.Context, string) ([]*interfaces.Document, error) { return nil, nil })
	_, err := NewSimpleBuilder(NewMockLLMClient()).WithRetriever(plain, RetrievalConfig{Filter: tenantFilter}).Build()
	require.Error(t, err)
	require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
}

func TestRetrievalFilterFailuresDoNotCallModel(t *testing.T) {
	sentinel := errors.New("tenant lookup failed")
	cases := map[string]func(context.Context, string) (map[string]any, error){
		"filter error": func(context.Context, string) (map[string]any, error) { return nil, sentinel },
		"empty filter": tenantFilter, // ctx 中没有租户
	}
	for name, filter := range cases {
		t.Run(name, func(t *testing.T) {
			client := &evidenceLLM{MockLLMClient: NewMockLLMClient()}
			retriever := &filteringRetriever{}
			agent, err := NewSimpleBuilder(client).WithRetriever(retriever, RetrievalConfig{Filter: filter}).Build()
			require.NoError(t, err)

			_, err = agent.AsRunnable().Invoke(context.Background(), "question")
			require.Error(t, err)
			if name == "filter error" {
				require.ErrorIs(t, err, sentinel)
			}
			require.Empty(t, retriever.requests)
			require.Zero(t, retriever.plain)
			require.Empty(t, client.requests)
		})
	}
}
