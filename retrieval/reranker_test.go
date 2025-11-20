package retrieval

import (
	"context"
	"testing"
)

// TestBaseRerankerNoop tests base reranker (no-op)
func TestBaseRerankerNoop(t *testing.T) {
	ctx := context.Background()
	reranker := NewBaseReranker("test_reranker")

	docs := []*Document{
		{ID: "1", PageContent: "Test document 1", Score: 0.8},
		{ID: "2", PageContent: "Test document 2", Score: 0.6},
	}

	results, err := reranker.Rerank(ctx, "test query", docs)
	if err != nil {
		t.Fatalf("Reranking failed: %v", err)
	}

	if len(results) != len(docs) {
		t.Errorf("Expected %d results, got %d", len(docs), len(results))
	}

	// Base reranker should return docs in same order
	if results[0].Score != docs[0].Score {
		t.Error("Base reranker should not change order")
	}
}

// TestCrossEncoderRerankerEmptyDocs tests cross encoder with empty docs
func TestCrossEncoderRerankerEmptyDocs(t *testing.T) {
	ctx := context.Background()
	reranker := NewCrossEncoderReranker("model", 2)

	results, err := reranker.Rerank(ctx, "test", []*Document{})
	if err != nil {
		t.Fatalf("Reranking empty docs failed: %v", err)
	}

	if len(results) != 0 {
		t.Error("Expected empty results for empty input")
	}
}

// TestCrossEncoderRerankerTopN tests top-N limiting
func TestCrossEncoderRerankerTopN(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		numDocs  int
		topN     int
		expected int
	}{
		{
			name:     "TopN less than docs",
			numDocs:  10,
			topN:     3,
			expected: 3,
		},
		{
			name:     "TopN greater than docs",
			numDocs:  3,
			topN:     10,
			expected: 3,
		},
		{
			name:     "TopN zero",
			numDocs:  5,
			topN:     0,
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := make([]*Document, tt.numDocs)
			for i := 0; i < tt.numDocs; i++ {
				docs[i] = &Document{ID: string(rune('0' + i)), PageContent: "content"}
			}

			reranker := NewCrossEncoderReranker("model", tt.topN)

			results, err := reranker.Rerank(ctx, "query", docs)
			if err != nil {
				t.Fatalf("Reranking failed: %v", err)
			}

			if len(results) != tt.expected {
				t.Errorf("Expected %d results, got %d", tt.expected, len(results))
			}
		})
	}
}

// TestLLMRerankerEmptyDocs tests LLM reranker with empty docs
func TestLLMRerankerEmptyDocs(t *testing.T) {
	ctx := context.Background()
	reranker := NewLLMReranker(2)

	results, err := reranker.Rerank(ctx, "test", []*Document{})
	if err != nil {
		t.Fatalf("Reranking empty docs failed: %v", err)
	}

	if len(results) != 0 {
		t.Error("Expected empty results for empty input")
	}
}

// TestLLMRerankerConfiguration tests LLM reranker configuration
func TestLLMRerankerConfiguration(t *testing.T) {
	reranker := NewLLMReranker(5)

	if reranker.TopN != 5 {
		t.Errorf("Expected TopN 5, got %d", reranker.TopN)
	}

	if reranker.Prompt == "" {
		t.Error("Expected default prompt to be set")
	}

	// Test custom prompt
	customPrompt := "Custom prompt: {.Query}"
	reranker.Prompt = customPrompt
	if reranker.Prompt != customPrompt {
		t.Error("Custom prompt not set")
	}
}

// TestMMRRerankerEmptyDocs tests MMR reranker with empty docs
func TestMMRRerankerEmptyDocs(t *testing.T) {
	ctx := context.Background()
	reranker := NewMMRReranker(0.7, 2)

	results, err := reranker.Rerank(ctx, "test", []*Document{})
	if err != nil {
		t.Fatalf("Reranking empty docs failed: %v", err)
	}

	if len(results) != 0 {
		t.Error("Expected empty results for empty input")
	}
}

// TestMMRRerankerLambdaEffect tests lambda parameter effect on diversity
func TestMMRRerankerLambdaEffect(t *testing.T) {
	ctx := context.Background()

	docs := []*Document{
		{ID: "1", PageContent: "Machine learning and AI", Score: 0.9},
		{ID: "2", PageContent: "Deep learning networks", Score: 0.85},
		{ID: "3", PageContent: "Natural language processing", Score: 0.8},
		{ID: "4", PageContent: "Computer vision and CNN", Score: 0.75},
	}

	tests := []struct {
		name   string
		lambda float64
	}{
		{
			name:   "High lambda (more relevant)",
			lambda: 0.9,
		},
		{
			name:   "Low lambda (more diverse)",
			lambda: 0.1,
		},
		{
			name:   "Balanced",
			lambda: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reranker := NewMMRReranker(tt.lambda, 2)

			results, err := reranker.Rerank(ctx, "machine learning", docs)
			if err != nil {
				t.Fatalf("MMR reranking failed: %v", err)
			}

			if len(results) != 2 {
				t.Errorf("Expected 2 results, got %d", len(results))
			}
		})
	}
}

// TestMMRRerankerSingleDoc tests MMR with single document
func TestMMRRerankerSingleDoc(t *testing.T) {
	ctx := context.Background()
	reranker := NewMMRReranker(0.5, 2)

	docs := []*Document{
		{ID: "1", PageContent: "Test document", Score: 0.8},
	}

	results, err := reranker.Rerank(ctx, "test", docs)
	if err != nil {
		t.Fatalf("MMR with single doc failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
}

// TestCohereRerankerConfiguration tests Cohere reranker configuration
func TestCohereRerankerConfiguration(t *testing.T) {
	apiKey := "test-api-key"
	model := "rerank-model"
	topN := 3

	reranker, err := NewCohereReranker(apiKey, model, topN)
	if err != nil {
		t.Fatalf("Failed to create Cohere reranker: %v", err)
	}

	if reranker.APIKey != apiKey {
		t.Errorf("Expected APIKey %s, got %s", apiKey, reranker.APIKey)
	}

	if reranker.Model != model {
		t.Errorf("Expected Model %s, got %s", model, reranker.Model)
	}

	if reranker.TopN != topN {
		t.Errorf("Expected TopN %d, got %d", topN, reranker.TopN)
	}
}

// TestCohereRerankerEmptyDocs tests Cohere reranker with empty docs
func TestCohereRerankerEmptyDocs(t *testing.T) {
	ctx := context.Background()
	reranker, err := NewCohereReranker("key", "model", 2)
	if err != nil {
		t.Fatalf("Failed to create Cohere reranker: %v", err)
	}

	results, err := reranker.Rerank(ctx, "test", []*Document{})
	if err != nil {
		t.Fatalf("Reranking empty docs failed: %v", err)
	}

	if len(results) != 0 {
		t.Error("Expected empty results for empty input")
	}
}

// TestRerankingRetrieverEmptyBaseResults tests with empty base retriever results
func TestRerankingRetrieverEmptyBaseResults(t *testing.T) {
	ctx := context.Background()

	// Create a retriever that returns no results
	baseRetriever := NewKeywordRetriever([]*Document{}, DefaultRetrieverConfig())
	reranker := NewCrossEncoderReranker("model", 2)

	config := DefaultRetrieverConfig()
	config.TopK = 2

	rerankingRetriever := NewRerankingRetriever(
		baseRetriever,
		reranker,
		5,
		config,
	)

	results, err := rerankingRetriever.GetRelevantDocuments(ctx, "test query")
	if err != nil {
		t.Fatalf("Reranking failed: %v", err)
	}

	if len(results) != 0 {
		t.Error("Expected empty results")
	}
}

// TestRerankingRetrieverWithResults tests complete reranking flow
func TestRerankingRetrieverWithResults(t *testing.T) {
	ctx := context.Background()

	docs := []*Document{
		NewDocument("Kubernetes cluster management", nil),
		NewDocument("Docker container runtime", nil),
		NewDocument("Python programming", nil),
	}

	baseRetriever := NewKeywordRetriever(docs, DefaultRetrieverConfig())
	reranker := NewCrossEncoderReranker("model", 2)

	config := DefaultRetrieverConfig()
	config.TopK = 2

	rerankingRetriever := NewRerankingRetriever(
		baseRetriever,
		reranker,
		5,
		config,
	)

	results, err := rerankingRetriever.GetRelevantDocuments(ctx, "Kubernetes")
	if err != nil {
		t.Fatalf("Reranking retrieval failed: %v", err)
	}

	if len(results) > 2 {
		t.Errorf("Expected at most 2 results, got %d", len(results))
	}
}

// TestCompareRankers tests comparing multiple rerankers
func TestCompareRankers(t *testing.T) {
	ctx := context.Background()

	docs := []*Document{
		{ID: "1", PageContent: "Kubernetes orchestration", Score: 0.8},
		{ID: "2", PageContent: "Docker containers", Score: 0.7},
		{ID: "3", PageContent: "Python code", Score: 0.6},
	}

	rerankers := []Reranker{
		NewCrossEncoderReranker("model1", 2),
		NewMMRReranker(0.5, 2),
	}

	comparer := &CompareRankers{
		Rerankers: rerankers,
	}

	results, err := comparer.Compare(ctx, "Kubernetes", docs)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	// Should have results from at least one reranker
	if results == nil {
		t.Error("Expected comparison results map")
	}
}

// TestRankFusionMethods tests different rank fusion methods
func TestRankFusionMethods(t *testing.T) {
	tests := []struct {
		name   string
		method string
	}{
		{"RRF", "rrf"},
		{"Borda", "borda"},
		{"CombSum", "comb_sum"},
		{"Unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fusion := NewRankFusion(tt.method)

			if fusion.Method != tt.method {
				t.Errorf("Expected method %s, got %s", tt.method, fusion.Method)
			}

			if fusion.K != 60.0 {
				t.Errorf("Expected K 60.0, got %.1f", fusion.K)
			}
		})
	}
}

// TestReciprocalRankFusion tests RRF fusion
func TestReciprocalRankFusion(t *testing.T) {
	fusion := NewRankFusion("rrf")

	rankings := [][]*Document{
		{
			{ID: "1", Score: 0.9},
			{ID: "2", Score: 0.8},
		},
		{
			{ID: "2", Score: 0.85},
			{ID: "3", Score: 0.7},
		},
	}

	results := fusion.Fuse(rankings)

	if len(results) == 0 {
		t.Fatal("Expected non-empty results")
	}

	// Doc 2 should have highest score (appears in both)
	if results[0].ID != "2" {
		t.Errorf("Expected doc 2 first, got %s", results[0].ID)
	}
}

// TestBordaCountFusion tests Borda count fusion
func TestBordaCountFusion(t *testing.T) {
	fusion := NewRankFusion("borda")

	rankings := [][]*Document{
		{
			{ID: "1", Score: 0},
			{ID: "2", Score: 0},
			{ID: "3", Score: 0},
		},
		{
			{ID: "2", Score: 0},
			{ID: "3", Score: 0},
			{ID: "1", Score: 0},
		},
	}

	results := fusion.Fuse(rankings)

	if len(results) == 0 {
		t.Fatal("Expected non-empty results")
	}

	// Results should be in descending order by Borda score
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Error("Results not in descending order by score")
		}
	}
}

// TestCombSumFusion tests CombSum fusion
func TestCombSumFusion(t *testing.T) {
	fusion := NewRankFusion("comb_sum")

	rankings := [][]*Document{
		{
			{ID: "1", Score: 0.8},
			{ID: "2", Score: 0.7},
		},
		{
			{ID: "2", Score: 0.6},
			{ID: "3", Score: 0.5},
		},
	}

	results := fusion.Fuse(rankings)

	if len(results) == 0 {
		t.Fatal("Expected non-empty results")
	}

	// Doc 2 should have highest combined score (0.7 + 0.6)
	if results[0].ID != "2" {
		t.Errorf("Expected doc 2 first, got %s", results[0].ID)
	}
}

// TestRankFusionUnknownMethod tests unknown fusion method
func TestRankFusionUnknownMethod(t *testing.T) {
	fusion := NewRankFusion("unknown")

	rankings := [][]*Document{
		{
			{ID: "1", Score: 0.8},
			{ID: "2", Score: 0.7},
		},
	}

	results := fusion.Fuse(rankings)

	// Should return first ranking as fallback
	if len(results) != len(rankings[0]) {
		t.Error("Expected first ranking as fallback for unknown method")
	}
}

// TestRankFusionEmptyRankings tests fusion with empty rankings
func TestRankFusionEmptyRankings(t *testing.T) {
	fusion := NewRankFusion("rrf")

	rankings := [][]*Document{}

	// Should handle gracefully
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Unexpected panic: %v", r)
		}
	}()

	fusion.Fuse(rankings)
}
