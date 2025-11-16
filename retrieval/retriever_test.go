package retrieval

import (
	"context"
	"sync"
	"testing"
)

// TestKeywordRetrieverEmptyDocs tests with empty document list
func TestKeywordRetrieverEmptyDocs(t *testing.T) {
	ctx := context.Background()

	config := RetrieverConfig{TopK: 5, MinScore: 0.0}
	retriever := NewKeywordRetriever([]*Document{}, config)

	results, err := retriever.GetRelevantDocuments(ctx, "test query")
	if err != nil {
		t.Fatalf("Retrieval failed: %v", err)
	}

	if len(results) != 0 {
		t.Error("Expected empty results for empty document list")
	}
}

// TestKeywordRetrieverBM25Algorithm tests BM25 algorithm
func TestKeywordRetrieverBM25Algorithm(t *testing.T) {
	docs := []*Document{
		NewDocument("Kubernetes is a container orchestration platform", nil),
		NewDocument("Docker containers are lightweight and portable", nil),
		NewDocument("Python is a popular programming language", nil),
	}

	config := RetrieverConfig{TopK: 2}
	retriever := NewKeywordRetriever(docs, config)
	retriever.Algorithm = AlgorithmBM25

	ctx := context.Background()
	results, err := retriever.GetRelevantDocuments(ctx, "container")

	if err != nil {
		t.Fatalf("BM25 retrieval failed: %v", err)
	}

	// Results should be sorted by score
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Error("Results not sorted in descending order by score")
		}
	}
}

// TestKeywordRetrieverTFIDFAlgorithm tests TF-IDF algorithm
func TestKeywordRetrieverTFIDFAlgorithm(t *testing.T) {
	docs := []*Document{
		NewDocument("machine learning deep neural networks", nil),
		NewDocument("natural language processing NLP", nil),
		NewDocument("computer vision image recognition", nil),
	}

	config := RetrieverConfig{TopK: 2}
	retriever := NewKeywordRetriever(docs, config)
	retriever.Algorithm = AlgorithmTFIDF

	ctx := context.Background()
	results, err := retriever.GetRelevantDocuments(ctx, "machine learning")

	if err != nil {
		t.Fatalf("TF-IDF retrieval failed: %v", err)
	}

	if len(results) > 2 {
		t.Errorf("Expected at most 2 results, got %d", len(results))
	}
}

// TestKeywordRetrieverUnknownAlgorithm tests unknown algorithm
func TestKeywordRetrieverUnknownAlgorithm(t *testing.T) {
	docs := []*Document{
		NewDocument("Test content", nil),
	}

	config := RetrieverConfig{TopK: 5}
	retriever := NewKeywordRetriever(docs, config)
	retriever.Algorithm = "unknown"

	ctx := context.Background()
	_, err := retriever.GetRelevantDocuments(ctx, "test")

	if err == nil {
		t.Error("Expected error for unknown algorithm")
	}
}

// TestKeywordRetrieverMinScoreFiltering tests minimum score filtering
func TestKeywordRetrieverMinScoreFiltering(t *testing.T) {
	docs := []*Document{
		NewDocument("High relevance machine learning deep learning", nil),
		NewDocument("Low relevance content", nil),
	}

	config := RetrieverConfig{TopK: 5, MinScore: 0.1}
	retriever := NewKeywordRetriever(docs, config)

	ctx := context.Background()
	results, err := retriever.GetRelevantDocuments(ctx, "machine learning")

	if err != nil {
		t.Fatalf("Retrieval failed: %v", err)
	}

	// All results should have score >= MinScore
	for _, doc := range results {
		if doc.Score < config.MinScore {
			t.Errorf("Document score %.2f below minimum %.2f", doc.Score, config.MinScore)
		}
	}
}

// TestInvertedIndexAddDocument tests adding documents to index
func TestInvertedIndexAddDocument(t *testing.T) {
	index := NewInvertedIndex()

	terms := []string{"kubernetes", "container", "orchestration"}
	index.AddDocument(0, terms)

	if index.DocumentFrequency("kubernetes") != 1 {
		t.Error("Document frequency incorrect")
	}

	if index.TermFrequency(0, "kubernetes") != 1 {
		t.Error("Term frequency incorrect")
	}
}

// TestInvertedIndexMultipleDocuments tests index with multiple documents
func TestInvertedIndexMultipleDocuments(t *testing.T) {
	index := NewInvertedIndex()

	// Add multiple documents with shared terms
	index.AddDocument(0, []string{"machine", "learning", "ai"})
	index.AddDocument(1, []string{"machine", "learning", "deep"})
	index.AddDocument(2, []string{"deep", "learning", "neural"})

	// "learning" appears in all 3 documents
	if index.DocumentFrequency("learning") != 3 {
		t.Errorf("Expected DF=3 for 'learning', got %d", index.DocumentFrequency("learning"))
	}

	// "machine" appears in docs 0 and 1
	if index.DocumentFrequency("machine") != 2 {
		t.Errorf("Expected DF=2 for 'machine', got %d", index.DocumentFrequency("machine"))
	}

	// "neural" appears only in doc 2
	if index.DocumentFrequency("neural") != 1 {
		t.Errorf("Expected DF=1 for 'neural', got %d", index.DocumentFrequency("neural"))
	}
}

// TestInvertedIndexAverageDocLength tests average document length calculation
func TestInvertedIndexAverageDocLength(t *testing.T) {
	index := NewInvertedIndex()

	index.AddDocument(0, []string{"short", "doc"})
	index.AddDocument(1, []string{"medium", "length", "document", "here"})
	index.AddDocument(2, []string{"very", "long", "document", "with", "many", "words"})

	avgLen := index.AverageDocLength()

	// Expected: (2 + 4 + 6) / 3 = 4
	expectedAvg := 4.0
	if avgLen != expectedAvg {
		t.Errorf("Expected average length %.2f, got %.2f", expectedAvg, avgLen)
	}
}

// TestInvertedIndexDuplicateTerms tests handling of duplicate terms in document
func TestInvertedIndexDuplicateTerms(t *testing.T) {
	index := NewInvertedIndex()

	// Add same term multiple times in single document
	index.AddDocument(0, []string{"test", "test", "test", "word"})

	// Term frequency should count all occurrences
	tf := index.TermFrequency(0, "test")
	if tf != 3 {
		t.Errorf("Expected TF=3 for repeated term, got %d", tf)
	}

	// Document frequency should only count document once
	df := index.DocumentFrequency("test")
	if df != 1 {
		t.Errorf("Expected DF=1 for single document, got %d", df)
	}
}

// TestHybridRetrieverCombSumFusion tests comb sum fusion
func TestHybridRetrieverCombSumFusion(t *testing.T) {
	ctx := context.Background()

	docs := []*Document{
		NewDocument("Kubernetes container orchestration", nil),
		NewDocument("Docker containerization technology", nil),
		NewDocument("Python programming language", nil),
	}

	vectorStore := NewMockVectorStore()
	_ = vectorStore.AddDocuments(ctx, docs)

	vectorRetriever := NewVectorStoreRetriever(vectorStore, DefaultRetrieverConfig())
	keywordRetriever := NewKeywordRetriever(docs, DefaultRetrieverConfig())

	config := DefaultRetrieverConfig()
	hybrid := NewHybridRetriever(vectorRetriever, keywordRetriever, 0.5, 0.5, config)
	hybrid.WithFusionStrategy(FusionStrategyCombSum)

	results, err := hybrid.GetRelevantDocuments(ctx, "Kubernetes")
	if err != nil {
		t.Fatalf("Hybrid retrieval failed: %v", err)
	}

	if results == nil {
		t.Error("Expected non-nil results")
	}
}

// TestHybridRetrieverWeightConfiguration tests weight configuration
func TestHybridRetrieverWeightConfiguration(t *testing.T) {
	vectorStore := NewMockVectorStore()
	keywordDocs := []*Document{NewDocument("test", nil)}

	vectorRetriever := NewVectorStoreRetriever(vectorStore, DefaultRetrieverConfig())
	keywordRetriever := NewKeywordRetriever(keywordDocs, DefaultRetrieverConfig())

	config := DefaultRetrieverConfig()
	hybrid := NewHybridRetriever(vectorRetriever, keywordRetriever, 0.6, 0.4, config)

	if hybrid.VectorWeight != 0.6 {
		t.Errorf("Expected VectorWeight 0.6, got %.2f", hybrid.VectorWeight)
	}

	if hybrid.KeywordWeight != 0.4 {
		t.Errorf("Expected KeywordWeight 0.4, got %.2f", hybrid.KeywordWeight)
	}

	// Test WithWeights
	hybrid.WithWeights(0.3, 0.7)
	if hybrid.VectorWeight != 0.3 {
		t.Error("VectorWeight not updated")
	}
}

// TestNormalizeScoresAllSame tests normalizing identical scores
func TestNormalizeScoresAllSame(t *testing.T) {
	docs := []*Document{
		{ID: "1", Score: 0.5},
		{ID: "2", Score: 0.5},
		{ID: "3", Score: 0.5},
	}

	normalized := normalizeScores(docs)

	if len(normalized) != len(docs) {
		t.Errorf("Expected %d documents, got %d", len(docs), len(normalized))
	}

	// All scores should remain same when normalization range is 0
	for i, doc := range normalized {
		if doc.Score != docs[i].Score {
			t.Errorf("Score changed unexpectedly")
		}
	}
}

// TestNormalizeScoresRange tests normalizing different scores
func TestNormalizeScoresRange(t *testing.T) {
	docs := []*Document{
		{ID: "1", Score: 0.1},
		{ID: "2", Score: 0.5},
		{ID: "3", Score: 0.9},
	}

	normalized := normalizeScores(docs)

	// After normalization, scores should be between 0 and 1
	for i, doc := range normalized {
		if doc.Score < 0.0 || doc.Score > 1.0 {
			t.Errorf("Normalized score %.2f out of range", doc.Score)
		}

		// First should have lowest, last should have highest
		if i == 0 && doc.Score != 0.0 {
			t.Errorf("Expected min normalized score 0.0, got %.2f", doc.Score)
		}
		if i == len(normalized)-1 && doc.Score != 1.0 {
			t.Errorf("Expected max normalized score 1.0, got %.2f", doc.Score)
		}
	}
}

// TestEnsembleRetrieverNoRetrievers tests ensemble with no retrievers
func TestEnsembleRetrieverNoRetrievers(t *testing.T) {
	ctx := context.Background()

	config := DefaultRetrieverConfig()
	ensemble := NewEnsembleRetriever([]Retriever{}, []float64{}, config)

	results, err := ensemble.GetRelevantDocuments(ctx, "test query")
	if err != nil {
		t.Fatalf("Retrieval failed: %v", err)
	}

	if len(results) != 0 {
		t.Error("Expected empty results for no retrievers")
	}
}

// TestEnsembleRetrieverSingleRetriever tests ensemble with single retriever
func TestEnsembleRetrieverSingleRetriever(t *testing.T) {
	ctx := context.Background()

	docs := []*Document{
		NewDocument("Test document", nil),
	}

	retriever := NewKeywordRetriever(docs, DefaultRetrieverConfig())
	config := DefaultRetrieverConfig()

	ensemble := NewEnsembleRetriever(
		[]Retriever{retriever},
		[]float64{1.0},
		config,
	)

	results, err := ensemble.GetRelevantDocuments(ctx, "test")
	if err != nil {
		t.Fatalf("Retrieval failed: %v", err)
	}

	if results == nil {
		t.Error("Expected non-nil results")
	}
}

// TestEnsembleRetrieverWeightMismatch tests weight/retriever count mismatch
func TestEnsembleRetrieverWeightMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for weight mismatch")
		}
	}()

	docs := []*Document{NewDocument("test", nil)}
	retriever := NewKeywordRetriever(docs, DefaultRetrieverConfig())

	config := DefaultRetrieverConfig()
	// This should panic
	NewEnsembleRetriever(
		[]Retriever{retriever},
		[]float64{1.0, 0.5}, // Wrong number of weights
		config,
	)
}

// TestBaseRetrieverFilterByScore tests score filtering
func TestBaseRetrieverFilterByScore(t *testing.T) {
	baseRetriever := NewBaseRetriever()
	baseRetriever.MinScore = 0.5

	docs := []*Document{
		{ID: "1", Score: 0.8},
		{ID: "2", Score: 0.3},
		{ID: "3", Score: 0.6},
	}

	filtered := baseRetriever.FilterByScore(docs)

	if len(filtered) != 2 {
		t.Errorf("Expected 2 results after filtering, got %d", len(filtered))
	}

	for _, doc := range filtered {
		if doc.Score < baseRetriever.MinScore {
			t.Errorf("Filtered document score %.2f below threshold", doc.Score)
		}
	}
}

// TestBaseRetrieverLimitTopK tests top-k limiting
func TestBaseRetrieverLimitTopK(t *testing.T) {
	baseRetriever := NewBaseRetriever()
	baseRetriever.TopK = 2

	docs := DocumentCollection{
		{ID: "1", Score: 0.9},
		{ID: "2", Score: 0.8},
		{ID: "3", Score: 0.7},
		{ID: "4", Score: 0.6},
	}

	limited := baseRetriever.LimitTopK(docs)

	if len(limited) != 2 {
		t.Errorf("Expected 2 results, got %d", len(limited))
	}
}

// TestBaseRetrieverInvoke tests invoke method with callbacks
func TestBaseRetrieverInvoke(t *testing.T) {
	ctx := context.Background()

	baseRetriever := NewBaseRetriever()

	// Create a simple mock to test invoke
	mockStore := NewMockVectorStore()
	docs := []*Document{NewDocument("Test", nil)}
	_ = mockStore.AddDocuments(ctx, docs)

	vectorRetriever := NewVectorStoreRetriever(mockStore, DefaultRetrieverConfig())

	results, err := vectorRetriever.Invoke(ctx, "test query")
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if results == nil {
		t.Error("Expected non-nil results")
	}

	_ = baseRetriever
}

// TestConcurrentHybridRetrieval tests concurrent operations in hybrid retriever
func TestConcurrentHybridRetrieval(t *testing.T) {
	docs := []*Document{
		NewDocument("Kubernetes orchestration", nil),
		NewDocument("Docker containers", nil),
	}

	vectorStore := NewMockVectorStore()
	ctx := context.Background()
	_ = vectorStore.AddDocuments(ctx, docs)

	vectorRetriever := NewVectorStoreRetriever(vectorStore, DefaultRetrieverConfig())
	keywordRetriever := NewKeywordRetriever(docs, DefaultRetrieverConfig())

	config := DefaultRetrieverConfig()
	hybrid := NewHybridRetriever(vectorRetriever, keywordRetriever, 0.5, 0.5, config)

	var wg sync.WaitGroup
	numGoroutines := 10

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			_, _ = hybrid.GetRelevantDocuments(ctx, "Kubernetes")
		}()
	}

	wg.Wait()
}
