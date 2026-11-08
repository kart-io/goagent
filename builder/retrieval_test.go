package builder

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	factmemory "github.com/kart-io/goagent/memory"
	"time"
)

type evidenceLLM struct {
	*MockLLMClient
	requests []*llm.CompletionRequest
}

type testFactService struct {
	factmemory.MemoryService
	calls   int
	content string
}

func (s *testFactService) Recall(context.Context, factmemory.MemoryAccess, string, factmemory.RecallOptions) (factmemory.RecallResult, error) {
	s.calls++
	return factmemory.RecallResult{Matches: []factmemory.FactMatch{{Fact: factmemory.Fact{ID: "f1", Content: s.content}, Score: 1, MatchKind: factmemory.MatchAlways}}}, nil
}
func (s *testFactService) ResolveSnapshot(context.Context, factmemory.MemoryAccess, factmemory.SnapshotRef) (factmemory.RecallResult, error) {
	return factmemory.RecallResult{Matches: []factmemory.FactMatch{{Fact: factmemory.Fact{ID: "f1", Content: s.content}, Score: 1, MatchKind: factmemory.MatchAlways}}}, nil
}

type testFactResolver struct {
	access factmemory.MemoryAccess
	err    error
}

func (r testFactResolver) Resolve(context.Context, string) (factmemory.MemoryAccess, error) {
	return r.access, r.err
}
func testFactAccess() factmemory.MemoryAccess {
	scope := factmemory.MemoryScope{TenantID: "tenant", OwnerKind: factmemory.OwnerAgent, OwnerID: "agent", AgentID: "agent"}
	return factmemory.MemoryAccess{Principal: factmemory.Principal{TenantID: "tenant", UserID: "user", AgentID: "agent"}, ReadScopes: []factmemory.MemoryScope{scope}}
}

func TestFactBuilderInjectsOnlyCurrentRequestAndBypassesCache(t *testing.T) {
	cache := middleware.NewCacheMiddleware(time.Minute)
	seed := &evidenceLLM{MockLLMClient: NewMockLLMClient("cached answer")}
	seedAgent, err := NewSimpleBuilder(seed).WithMiddleware(cache).Build()
	require.NoError(t, err)
	_, err = seedAgent.AsRunnable().Invoke(context.Background(), "same query")
	require.NoError(t, err)
	require.Equal(t, 1, cache.Size())
	model := &evidenceLLM{MockLLMClient: NewMockLLMClient("fresh 1", "fresh 2")}
	service := &testFactService{content: "likes tea"}
	agent, err := NewSimpleBuilder(model).WithAgentID("agent").WithMiddleware(cache).WithFactMemory(service, testFactResolver{access: testFactAccess()}).Build()
	require.NoError(t, err)
	first, err := agent.AsRunnable().Invoke(context.Background(), "same query")
	require.NoError(t, err)
	service.content = "likes coffee"
	second, err := agent.AsRunnable().Invoke(context.Background(), "same query")
	require.NoError(t, err)
	require.Equal(t, "fresh 1", first.Result)
	require.Equal(t, "fresh 2", second.Result)
	require.Equal(t, 2, service.calls)
	require.Equal(t, 1, cache.Size())
	require.Contains(t, model.requests[0].Messages[len(model.requests[0].Messages)-1].Content, "likes tea")
	require.NotContains(t, model.requests[0].Messages[0].Content, "likes tea", "facts must not be injected into system policy")
}

func TestFactBuilderRejectsMissingPrincipalBeforeCache(t *testing.T) {
	cache := middleware.NewCacheMiddleware(time.Minute)
	seedAgent, err := NewSimpleBuilder(NewMockLLMClient("cached")).WithMiddleware(cache).Build()
	require.NoError(t, err)
	_, err = seedAgent.AsRunnable().Invoke(context.Background(), "protected query")
	require.NoError(t, err)
	model := &evidenceLLM{MockLLMClient: NewMockLLMClient("should not run")}
	service := &testFactService{content: "private"}
	agent, err := NewSimpleBuilder(model).WithAgentID("agent").WithMiddleware(cache).WithFactMemory(service, testFactResolver{err: errors.New("missing principal")}).Build()
	require.NoError(t, err)
	_, err = agent.AsRunnable().Invoke(context.Background(), "protected query")
	require.Error(t, err)
	require.Empty(t, model.requests)
	require.Zero(t, service.calls)
	require.Equal(t, 1, cache.Size())
}

func (m *evidenceLLM) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	m.requests = append(m.requests, req)
	return m.MockLLMClient.CompleteStream(ctx, req)
}

func (m *evidenceLLM) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	return m.CompleteStream(ctx, req)
}

func TestRAGAgentRetrievesFreshEvidence(t *testing.T) {
	client := &evidenceLLM{MockLLMClient: NewMockLLMClient("Answer [1]")}
	text := "Original evidence"
	var queries []string
	retriever := documentRetrieverFunc(func(_ context.Context, query string) ([]*interfaces.Document, error) {
		queries = append(queries, query)
		return []*interfaces.Document{{ID: "doc-1", PageContent: text, Score: 0.9, Metadata: map[string]interface{}{"source": "handbook"}}}, nil
	})
	agent, err := RAGAgent(client, retriever)
	require.NoError(t, err)
	first, err := agent.AsRunnable().Invoke(context.Background(), "What is the policy?")
	require.NoError(t, err)
	text = "Updated evidence"
	second, err := agent.AsRunnable().Invoke(context.Background(), "What is the policy?")
	require.NoError(t, err)
	require.Equal(t, []string{"What is the policy?", "What is the policy?"}, queries)
	require.Len(t, client.requests, 2, "RAG must not return a stale answer for an unchanged query")
	for i, expected := range []string{"Original evidence", "Updated evidence"} {
		last := client.requests[i].Messages[len(client.requests[i].Messages)-1]
		require.Equal(t, "user", last.Role)
		require.Contains(t, last.Content, expected)
		require.True(t, strings.HasSuffix(last.Content, "用户请求：What is the policy?"))
	}
	wantSources := []RetrievalSource{{Reference: 1, ID: "doc-1", Source: "handbook", Score: 0.9}}
	require.Equal(t, wantSources, first.Metadata["retrieval_sources"])
	require.Equal(t, wantSources, second.Metadata["retrieval_sources"])
}

func TestRetrievalLimitsAndSourceAlignment(t *testing.T) {
	doc := &interfaces.Document{ID: "unicode", PageContent: "中文证据内容"}
	client := &evidenceLLM{MockLLMClient: NewMockLLMClient()}
	retriever := documentRetrieverFunc(func(context.Context, string) ([]*interfaces.Document, error) {
		return []*interfaces.Document{nil, {PageContent: "  "}, doc, {ID: "excluded", PageContent: "secret"}}, nil
	})
	agent, err := NewSimpleBuilder(client).WithRetriever(retriever, RetrievalConfig{MaxDocuments: 1, MaxContextRunes: 3}).Build()
	require.NoError(t, err)
	out, err := agent.AsRunnable().Invoke(context.Background(), "question")
	require.NoError(t, err)
	evidence := client.requests[0].Messages[1].Content
	require.True(t, utf8.ValidString(evidence))
	require.Contains(t, evidence, "[1]\n中文证")
	require.NotContains(t, evidence, "secret")
	require.Equal(t, "中文证据内容", doc.PageContent, "retriever-owned documents must not be mutated")
	require.Equal(t, 3, out.Metadata["retrieval_context_runes"])
	require.Equal(t, true, out.Metadata["retrieval_truncated"])
	require.Equal(t, []RetrievalSource{{Reference: 1, ID: "unicode"}}, out.Metadata["retrieval_sources"])
}

func TestRetrievalFailureAndCancellationDoNotCallModel(t *testing.T) {
	sentinel := errors.New("retrieval unavailable")
	for _, canceled := range []bool{false, true} {
		client := &evidenceLLM{MockLLMClient: NewMockLLMClient()}
		calls := 0
		retriever := documentRetrieverFunc(func(context.Context, string) ([]*interfaces.Document, error) {
			calls++
			return nil, sentinel
		})
		agent, err := RAGAgent(client, retriever)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		if canceled {
			cancel()
		}
		_, err = agent.AsRunnable().Invoke(ctx, "question")
		cancel()
		if canceled {
			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, calls)
		} else {
			require.ErrorIs(t, err, sentinel)
		}
		require.Empty(t, client.requests)
	}
}

func TestRetrievalEmptyEvidenceAndTools(t *testing.T) {
	for _, withTools := range []bool{false, true} {
		client := &evidenceLLM{MockLLMClient: NewMockLLMClient()}
		b := NewSimpleBuilder(client).WithRetriever(documentRetrieverFunc(func(context.Context, string) ([]*interfaces.Document, error) {
			return nil, nil
		}))
		if withTools {
			b.WithTools(NewMockTool("lookup", "result"))
		}
		agent, err := b.Build()
		require.NoError(t, err)
		var out *AgentOutput
		var prompt string
		out, err = agent.AsRunnable().Invoke(context.Background(), "question")
		for _, message := range client.requests[0].Messages {
			prompt += message.Content
		}
		require.NoError(t, err)
		require.Contains(t, prompt, "No relevant documents were found")
		require.Equal(t, 0, out.Metadata["retrieval_document_count"])
	}
}

type retrieveAdapter struct{}

func (*retrieveAdapter) Retrieve(context.Context, string) ([]*interfaces.Document, error) {
	return []*interfaces.Document{{PageContent: "retrieved"}}, nil
}

type vectorAdapter struct {
	interfaces.VectorStore
	topK int
}

func (v *vectorAdapter) SimilaritySearch(_ context.Context, _ string, topK int) ([]*interfaces.Document, error) {
	v.topK = topK
	return []*interfaces.Document{{PageContent: "vector evidence"}}, nil
}

func TestRAGAgentAdaptersAndValidation(t *testing.T) {
	vector := &vectorAdapter{}
	for _, backend := range []interface{}{&retrieveAdapter{}, vector} {
		client := &evidenceLLM{MockLLMClient: NewMockLLMClient()}
		agent, err := RAGAgent(client, backend)
		require.NoError(t, err)
		out, err := agent.AsRunnable().Invoke(context.Background(), "question")
		require.NoError(t, err)
		require.Equal(t, 1, out.Metadata["retrieval_document_count"])
	}
	require.Equal(t, 4, vector.topK)
	var typedNil *retrieveAdapter
	for _, invalid := range []interface{}{typedNil, "invalid", documentRetrieverFunc(nil)} {
		_, err := RAGAgent(NewMockLLMClient(), invalid)
		require.Error(t, err)
	}
	_, err := NewSimpleBuilder(NewMockLLMClient()).WithRetriever(documentRetrieverFunc(nil)).Build()
	require.Error(t, err)
	_, err = NewSimpleBuilder(NewMockLLMClient()).WithRetriever(nil, RetrievalConfig{MaxContextRunes: -1}).Build()
	require.Error(t, err)
}

func TestRetrievalDocumentLimitWithoutTextTruncation(t *testing.T) {
	r := documentRetrieverFunc(func(context.Context, string) ([]*interfaces.Document, error) {
		return []*interfaces.Document{{PageContent: "one"}, {PageContent: "two"}}, nil
	})
	evidence, metadata, err := retrieveContext(context.Background(), r, RetrievalConfig{MaxDocuments: 1}, "question")
	require.NoError(t, err)
	require.True(t, strings.Contains(evidence, "one"))
	require.NotContains(t, evidence, "two")
	require.Equal(t, true, metadata["retrieval_truncated"])
}
