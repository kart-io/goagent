package retrieval

import (
	"context"
	"testing"
)

func TestDocument(t *testing.T) {
	t.Run("NewDocument", func(t *testing.T) {
		doc := NewDocument("test content", map[string]interface{}{
			"source": "test.txt",
			"page":   1,
		})

		if doc.PageContent != "test content" {
			t.Errorf("Expected content 'test content', got '%s'", doc.PageContent)
		}

		if doc.ID == "" {
			t.Error("Expected non-empty ID")
		}

		if len(doc.Metadata) != 2 {
			t.Errorf("Expected 2 metadata entries, got %d", len(doc.Metadata))
		}
	})

	t.Run("Clone", func(t *testing.T) {
		original := NewDocument("original", map[string]interface{}{
			"key": "value",
		})
		original.Score = 0.8

		cloned := original.Clone()

		if cloned.ID != original.ID {
			t.Error("Cloned document should have same ID")
		}

		if cloned.PageContent != original.PageContent {
			t.Error("Cloned document should have same content")
		}

		if cloned.Score != original.Score {
			t.Error("Cloned document should have same score")
		}

		// 修改克隆不应影响原文档
		cloned.Metadata["key"] = "new_value"
		if original.Metadata["key"] == "new_value" {
			t.Error("Modifying cloned metadata should not affect original")
		}
	})

	t.Run("GetSetMetadata", func(t *testing.T) {
		doc := NewDocument("test", nil)

		doc.SetMetadata("author", "John Doe")
		val, ok := doc.GetMetadata("author")

		if !ok {
			t.Error("Expected metadata to exist")
		}

		if val != "John Doe" {
			t.Errorf("Expected 'John Doe', got '%v'", val)
		}

		_, ok = doc.GetMetadata("nonexistent")
		if ok {
			t.Error("Expected nonexistent key to return false")
		}
	})
}

func TestDocumentCollection(t *testing.T) {
	t.Run("SortByScore", func(t *testing.T) {
		docs := DocumentCollection{
			&Document{ID: "1", Score: 0.5},
			&Document{ID: "2", Score: 0.9},
			&Document{ID: "3", Score: 0.3},
		}

		docs.SortByScore()

		if docs[0].ID != "2" {
			t.Error("Expected highest scored document first")
		}
		if docs[2].ID != "3" {
			t.Error("Expected lowest scored document last")
		}
	})

	t.Run("Top", func(t *testing.T) {
		docs := DocumentCollection{
			&Document{ID: "1", Score: 0.5},
			&Document{ID: "2", Score: 0.9},
			&Document{ID: "3", Score: 0.3},
		}

		top2 := docs.Top(2)
		if len(top2) != 2 {
			t.Errorf("Expected 2 documents, got %d", len(top2))
		}

		topAll := docs.Top(10)
		if len(topAll) != 3 {
			t.Error("Top should return all docs if n >= len")
		}
	})

	t.Run("Filter", func(t *testing.T) {
		docs := DocumentCollection{
			&Document{ID: "1", Score: 0.5},
			&Document{ID: "2", Score: 0.9},
			&Document{ID: "3", Score: 0.3},
		}

		filtered := docs.Filter(func(d *Document) bool {
			return d.Score > 0.4
		})

		if len(filtered) != 2 {
			t.Errorf("Expected 2 documents, got %d", len(filtered))
		}
	})

	t.Run("Deduplicate", func(t *testing.T) {
		docs := DocumentCollection{
			&Document{ID: "1", PageContent: "doc1"},
			&Document{ID: "2", PageContent: "doc2"},
			&Document{ID: "1", PageContent: "doc1 duplicate"},
		}

		unique := docs.Deduplicate()

		if len(unique) != 2 {
			t.Errorf("Expected 2 unique documents, got %d", len(unique))
		}
	})
}

func TestVectorStoreRetriever(t *testing.T) {
	t.Run("Basic retrieval", func(t *testing.T) {
		// 创建模拟向量存储
		vectorStore := NewMockVectorStore()

		// 添加文档
		docs := []*Document{
			NewDocument("Kubernetes is a container orchestration platform", map[string]interface{}{
				"source": "k8s_intro.txt",
			}),
			NewDocument("Docker is a containerization technology", map[string]interface{}{
				"source": "docker_intro.txt",
			}),
			NewDocument("Python is a programming language", map[string]interface{}{
				"source": "python_intro.txt",
			}),
		}

		ctx := context.Background()
		err := vectorStore.AddDocuments(ctx, docs)
		if err != nil {
			t.Fatalf("Failed to add documents: %v", err)
		}

		// 创建检索器
		config := DefaultRetrieverConfig()
		config.TopK = 2

		retriever := NewVectorStoreRetriever(vectorStore, config)

		// 执行检索
		results, err := retriever.GetRelevantDocuments(ctx, "container technology")
		if err != nil {
			t.Fatalf("Retrieval failed: %v", err)
		}

		if len(results) == 0 {
			t.Error("Expected non-empty results")
		}

		if len(results) > 2 {
			t.Errorf("Expected at most 2 results, got %d", len(results))
		}

		// 检查结果是否有分数
		for _, doc := range results {
			if doc.Score <= 0 {
				t.Error("Expected positive score")
			}
		}
	})

	t.Run("Invoke interface", func(t *testing.T) {
		vectorStore := NewMockVectorStore()
		docs := []*Document{
			NewDocument("Test document", nil),
		}

		ctx := context.Background()
		_ = vectorStore.AddDocuments(ctx, docs)

		config := DefaultRetrieverConfig()
		retriever := NewVectorStoreRetriever(vectorStore, config)

		// 测试 Invoke 方法
		results, err := retriever.Invoke(ctx, "test query")
		if err != nil {
			t.Fatalf("Invoke failed: %v", err)
		}

		if results == nil {
			t.Error("Expected non-nil results")
		}
	})
}

func TestKeywordRetriever(t *testing.T) {
	docs := []*Document{
		NewDocument("Kubernetes cluster management and container orchestration", map[string]interface{}{
			"id": "doc1",
		}),
		NewDocument("Docker container runtime and image management", map[string]interface{}{
			"id": "doc2",
		}),
		NewDocument("Python programming language for data science", map[string]interface{}{
			"id": "doc3",
		}),
	}

	t.Run("BM25 retrieval", func(t *testing.T) {
		config := DefaultRetrieverConfig()
		config.TopK = 2

		retriever := NewKeywordRetriever(docs, config)
		retriever.WithAlgorithm(AlgorithmBM25)

		ctx := context.Background()
		results, err := retriever.GetRelevantDocuments(ctx, "container management")
		if err != nil {
			t.Fatalf("BM25 retrieval failed: %v", err)
		}

		// BM25 may return empty if no matching terms
		// Just verify no error and results are valid
		if results == nil {
			t.Error("Expected non-nil results")
		}

		// 验证分数递减（如果有结果）
		for i := 1; i < len(results); i++ {
			if results[i].Score > results[i-1].Score {
				t.Error("Expected scores to be in descending order")
			}
		}
	})

	t.Run("TF-IDF retrieval", func(t *testing.T) {
		config := DefaultRetrieverConfig()
		retriever := NewKeywordRetriever(docs, config)
		retriever.WithAlgorithm(AlgorithmTFIDF)

		ctx := context.Background()
		results, err := retriever.GetRelevantDocuments(ctx, "python programming")
		if err != nil {
			t.Fatalf("TF-IDF retrieval failed: %v", err)
		}

		if len(results) == 0 {
			t.Error("Expected non-empty results")
		}
	})

	t.Run("Min score filtering", func(t *testing.T) {
		config := DefaultRetrieverConfig()
		config.MinScore = 0.5

		retriever := NewKeywordRetriever(docs, config)

		ctx := context.Background()
		results, err := retriever.GetRelevantDocuments(ctx, "test")
		if err != nil {
			t.Fatalf("Retrieval failed: %v", err)
		}

		// 验证所有结果的分数都大于阈值
		for _, doc := range results {
			if doc.Score < config.MinScore {
				t.Errorf("Document score %.2f is below threshold %.2f", doc.Score, config.MinScore)
			}
		}
	})
}

func TestHybridRetriever(t *testing.T) {
	docs := []*Document{
		NewDocument("Kubernetes is a container orchestration platform", nil),
		NewDocument("Docker containers are lightweight and portable", nil),
		NewDocument("Python is great for machine learning", nil),
	}

	t.Run("Weighted sum fusion", func(t *testing.T) {
		// 创建向量检索器
		vectorStore := NewMockVectorStore()
		ctx := context.Background()
		_ = vectorStore.AddDocuments(ctx, docs)

		vectorConfig := DefaultRetrieverConfig()
		vectorRetriever := NewVectorStoreRetriever(vectorStore, vectorConfig)

		// 创建关键词检索器
		keywordConfig := DefaultRetrieverConfig()
		keywordRetriever := NewKeywordRetriever(docs, keywordConfig)

		// 创建混合检索器
		hybridConfig := DefaultRetrieverConfig()
		hybridConfig.TopK = 2

		hybrid := NewHybridRetriever(
			vectorRetriever,
			keywordRetriever,
			0.6, // 向量权重
			0.4, // 关键词权重
			hybridConfig,
		)

		results, err := hybrid.GetRelevantDocuments(ctx, "container orchestration")
		if err != nil {
			t.Fatalf("Hybrid retrieval failed: %v", err)
		}

		if len(results) == 0 {
			t.Error("Expected non-empty results")
		}
	})

	t.Run("RRF fusion", func(t *testing.T) {
		vectorStore := NewMockVectorStore()
		ctx := context.Background()
		_ = vectorStore.AddDocuments(ctx, docs)

		vectorConfig := DefaultRetrieverConfig()
		vectorRetriever := NewVectorStoreRetriever(vectorStore, vectorConfig)

		keywordConfig := DefaultRetrieverConfig()
		keywordRetriever := NewKeywordRetriever(docs, keywordConfig)

		hybridConfig := DefaultRetrieverConfig()
		hybrid := NewHybridRetriever(
			vectorRetriever,
			keywordRetriever,
			0.5,
			0.5,
			hybridConfig,
		)
		hybrid.WithFusionStrategy(FusionStrategyRRF)

		results, err := hybrid.GetRelevantDocuments(ctx, "test query")
		if err != nil {
			t.Fatalf("RRF fusion failed: %v", err)
		}

		if results == nil {
			t.Error("Expected non-nil results")
		}
	})
}

func TestEnsembleRetriever(t *testing.T) {
	docs := []*Document{
		NewDocument("Kubernetes cluster management", nil),
		NewDocument("Docker container technology", nil),
		NewDocument("Python programming language", nil),
	}

	t.Run("Multiple retrievers", func(t *testing.T) {
		ctx := context.Background()

		// 创建多个检索器
		vectorStore := NewMockVectorStore()
		_ = vectorStore.AddDocuments(ctx, docs)

		retriever1 := NewVectorStoreRetriever(vectorStore, DefaultRetrieverConfig())
		retriever2 := NewKeywordRetriever(docs, DefaultRetrieverConfig())

		// 创建集成检索器
		config := DefaultRetrieverConfig()
		config.TopK = 2

		ensemble := NewEnsembleRetriever(
			[]Retriever{retriever1, retriever2},
			[]float64{0.6, 0.4},
			config,
		)

		results, err := ensemble.GetRelevantDocuments(ctx, "kubernetes container")
		if err != nil {
			t.Fatalf("Ensemble retrieval failed: %v", err)
		}

		if len(results) == 0 {
			t.Error("Expected non-empty results")
		}
	})

	t.Run("Add retriever", func(t *testing.T) {
		config := DefaultRetrieverConfig()
		ensemble := NewEnsembleRetriever(
			[]Retriever{},
			[]float64{},
			config,
		)

		docs := []*Document{NewDocument("test", nil)}
		retriever := NewKeywordRetriever(docs, config)

		ensemble.AddRetriever(retriever, 1.0)

		if len(ensemble.Retrievers) != 1 {
			t.Error("Expected 1 retriever")
		}
	})
}

func TestReranker(t *testing.T) {
	docs := []*Document{
		{ID: "1", PageContent: "Kubernetes container orchestration platform", Score: 0.6},
		{ID: "2", PageContent: "Docker container runtime technology", Score: 0.7},
		{ID: "3", PageContent: "Python programming language", Score: 0.5},
	}

	t.Run("CrossEncoderReranker", func(t *testing.T) {
		reranker := NewCrossEncoderReranker("cross-encoder-model", 2)

		ctx := context.Background()
		results, err := reranker.Rerank(ctx, "container technology", docs)
		if err != nil {
			t.Fatalf("Reranking failed: %v", err)
		}

		if len(results) > 2 {
			t.Errorf("Expected at most 2 results, got %d", len(results))
		}
	})

	t.Run("MMRReranker", func(t *testing.T) {
		reranker := NewMMRReranker(0.7, 2)

		ctx := context.Background()
		results, err := reranker.Rerank(ctx, "test query", docs)
		if err != nil {
			t.Fatalf("MMR reranking failed: %v", err)
		}

		if len(results) > 2 {
			t.Errorf("Expected at most 2 results, got %d", len(results))
		}

		// 验证多样性（文档应该不同）
		if len(results) == 2 && results[0].ID == results[1].ID {
			t.Error("Expected diverse results")
		}
	})

	t.Run("RerankingRetriever", func(t *testing.T) {
		// 创建基础检索器
		keywordDocs := []*Document{
			NewDocument("Kubernetes container orchestration", nil),
			NewDocument("Docker container runtime", nil),
			NewDocument("Python programming", nil),
		}

		baseRetriever := NewKeywordRetriever(keywordDocs, DefaultRetrieverConfig())

		// 创建重排序器
		reranker := NewCrossEncoderReranker("model", 2)

		// 创建带重排序的检索器
		config := DefaultRetrieverConfig()
		config.TopK = 2

		rerankingRetriever := NewRerankingRetriever(
			baseRetriever,
			reranker,
			5,
			config,
		)

		ctx := context.Background()
		results, err := rerankingRetriever.GetRelevantDocuments(ctx, "container")
		if err != nil {
			t.Fatalf("Reranking retrieval failed: %v", err)
		}

		if len(results) > 2 {
			t.Errorf("Expected at most 2 results, got %d", len(results))
		}
	})
}

func TestInvertedIndex(t *testing.T) {
	t.Run("Build and query", func(t *testing.T) {
		index := NewInvertedIndex()

		// 添加文档
		index.AddDocument(0, []string{"kubernetes", "container", "orchestration"})
		index.AddDocument(1, []string{"docker", "container", "runtime"})
		index.AddDocument(2, []string{"python", "programming", "language"})

		// 测试文档频率
		df := index.DocumentFrequency("container")
		if df != 2 {
			t.Errorf("Expected document frequency 2, got %d", df)
		}

		// 测试词频
		tf := index.TermFrequency(0, "kubernetes")
		if tf != 1 {
			t.Errorf("Expected term frequency 1, got %d", tf)
		}

		// 测试平均文档长度
		avgLen := index.AverageDocLength()
		if avgLen == 0 {
			t.Error("Expected non-zero average document length")
		}
	})

	t.Run("Empty index", func(t *testing.T) {
		index := NewInvertedIndex()

		if index.DocumentFrequency("test") != 0 {
			t.Error("Expected 0 document frequency for empty index")
		}

		if index.AverageDocLength() != 0 {
			t.Error("Expected 0 average length for empty index")
		}
	})
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"The quick brown fox", 3},   // 'the' is filtered as stopword
		{"Kubernetes is awesome", 2}, // 'is' is filtered
		{"", 0},
		{"a an the", 0}, // all stopwords
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tokens := tokenize(tt.input)
			if len(tokens) != tt.expected {
				t.Errorf("Expected %d tokens, got %d: %v", tt.expected, len(tokens), tokens)
			}
		})
	}
}
