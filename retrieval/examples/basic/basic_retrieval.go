package main

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/retrieval"
)

func main() {
	fmt.Println("=== Basic Retrieval Example ===")
	fmt.Println()

	// 创建示例文档
	docs := []*retrieval.Document{
		retrieval.NewDocument(
			"Kubernetes is an open-source container orchestration platform that automates deployment, scaling, and management of containerized applications.",
			map[string]interface{}{
				"source": "kubernetes_intro.txt",
				"type":   "documentation",
			},
		),
		retrieval.NewDocument(
			"Docker is a platform for developing, shipping, and running applications in containers. Containers are lightweight, portable, and consistent across environments.",
			map[string]interface{}{
				"source": "docker_intro.txt",
				"type":   "documentation",
			},
		),
		retrieval.NewDocument(
			"Python is a high-level programming language known for its simplicity and readability. It's widely used in data science, web development, and automation.",
			map[string]interface{}{
				"source": "python_intro.txt",
				"type":   "documentation",
			},
		),
		retrieval.NewDocument(
			"Go is a statically typed, compiled programming language designed for simplicity and efficiency. It's popular for building scalable microservices and cloud applications.",
			map[string]interface{}{
				"source": "go_intro.txt",
				"type":   "documentation",
			},
		),
		retrieval.NewDocument(
			"Container orchestration automates the deployment, management, scaling, and networking of containers. Popular tools include Kubernetes, Docker Swarm, and Apache Mesos.",
			map[string]interface{}{
				"source": "orchestration.txt",
				"type":   "tutorial",
			},
		),
	}

	ctx := context.Background()

	// 1. 向量检索示例
	fmt.Println("1. Vector Store Retrieval")
	fmt.Println("---------------------------")
	vectorExample(ctx, docs)

	// 2. 关键词检索示例
	fmt.Println("\n2. Keyword Retrieval (BM25)")
	fmt.Println("---------------------------")
	keywordExample(ctx, docs)

	// 3. 混合检索示例
	fmt.Println("\n3. Hybrid Retrieval")
	fmt.Println("---------------------------")
	hybridExample(ctx, docs)

	// 4. 集成检索示例
	fmt.Println("\n4. Ensemble Retrieval")
	fmt.Println("---------------------------")
	ensembleExample(ctx, docs)

	// 5. 重排序示例
	fmt.Println("\n5. Reranking")
	fmt.Println("---------------------------")
	rerankingExample(ctx, docs)
}

// vectorExample 向量检索示例
func vectorExample(ctx context.Context, docs []*retrieval.Document) {
	// 创建模拟向量存储
	vectorStore := retrieval.NewMockVectorStore()

	// 添加文档
	err := vectorStore.AddDocuments(ctx, docs)
	if err != nil {
		fmt.Printf("Error adding documents: %v\n", err)
		return
	}

	// 创建向量检索器
	config := retrieval.DefaultRetrieverConfig()
	config.TopK = 3
	config.Name = "vector_retriever"

	retriever := retrieval.NewVectorStoreRetriever(vectorStore, config)

	// 执行检索
	query := "container orchestration platform"
	fmt.Printf("Query: %s\n\n", query)

	results, err := retriever.GetRelevantDocuments(ctx, query)
	if err != nil {
		fmt.Printf("Retrieval error: %v\n", err)
		return
	}

	// 显示结果
	printResults(results)
}

// keywordExample 关键词检索示例
func keywordExample(ctx context.Context, docs []*retrieval.Document) {
	// 创建 BM25 检索器
	config := retrieval.DefaultRetrieverConfig()
	config.TopK = 3
	config.Name = "bm25_retriever"

	retriever := retrieval.NewKeywordRetriever(docs, config)
	retriever.WithAlgorithm(retrieval.AlgorithmBM25)

	// 执行检索
	query := "programming language development"
	fmt.Printf("Query: %s\n\n", query)

	results, err := retriever.GetRelevantDocuments(ctx, query)
	if err != nil {
		fmt.Printf("Retrieval error: %v\n", err)
		return
	}

	// 显示结果
	printResults(results)
}

// hybridExample 混合检索示例
func hybridExample(ctx context.Context, docs []*retrieval.Document) {
	// 创建向量检索器
	vectorStore := retrieval.NewMockVectorStore()
	_ = vectorStore.AddDocuments(ctx, docs)

	vectorConfig := retrieval.DefaultRetrieverConfig()
	vectorRetriever := retrieval.NewVectorStoreRetriever(vectorStore, vectorConfig)

	// 创建关键词检索器
	keywordConfig := retrieval.DefaultRetrieverConfig()
	keywordRetriever := retrieval.NewKeywordRetriever(docs, keywordConfig)

	// 创建混合检索器
	hybridConfig := retrieval.DefaultRetrieverConfig()
	hybridConfig.TopK = 3
	hybridConfig.Name = "hybrid_retriever"

	hybrid := retrieval.NewHybridRetriever(
		vectorRetriever,
		keywordRetriever,
		0.6, // 向量权重
		0.4, // 关键词权重
		hybridConfig,
	)

	// 测试不同的融合策略
	strategies := []retrieval.FusionStrategy{
		retrieval.FusionStrategyWeightedSum,
		retrieval.FusionStrategyRRF,
		retrieval.FusionStrategyCombSum,
	}

	query := "container deployment management"
	fmt.Printf("Query: %s\n\n", query)

	for _, strategy := range strategies {
		fmt.Printf("Fusion Strategy: %s\n", strategy)
		hybrid.WithFusionStrategy(strategy)

		results, err := hybrid.GetRelevantDocuments(ctx, query)
		if err != nil {
			fmt.Printf("Retrieval error: %v\n", err)
			continue
		}

		printResults(results)
		fmt.Println()
	}
}

// ensembleExample 集成检索示例
func ensembleExample(ctx context.Context, docs []*retrieval.Document) {
	// 创建多个检索器
	vectorStore := retrieval.NewMockVectorStore()
	_ = vectorStore.AddDocuments(ctx, docs)

	retriever1 := retrieval.NewVectorStoreRetriever(vectorStore, retrieval.DefaultRetrieverConfig())

	retriever2 := retrieval.NewKeywordRetriever(docs, retrieval.DefaultRetrieverConfig())
	retriever2.WithAlgorithm(retrieval.AlgorithmBM25)

	retriever3 := retrieval.NewKeywordRetriever(docs, retrieval.DefaultRetrieverConfig())
	retriever3.WithAlgorithm(retrieval.AlgorithmTFIDF)

	// 创建集成检索器
	config := retrieval.DefaultRetrieverConfig()
	config.TopK = 3
	config.Name = "ensemble_retriever"

	ensemble := retrieval.NewEnsembleRetriever(
		[]retrieval.Retriever{retriever1, retriever2, retriever3},
		[]float64{0.5, 0.3, 0.2},
		config,
	)

	// 执行检索
	query := "scalable cloud applications"
	fmt.Printf("Query: %s\n\n", query)

	results, err := ensemble.GetRelevantDocuments(ctx, query)
	if err != nil {
		fmt.Printf("Retrieval error: %v\n", err)
		return
	}

	printResults(results)
}

// rerankingExample 重排序示例
func rerankingExample(ctx context.Context, docs []*retrieval.Document) {
	// 创建基础检索器
	baseRetriever := retrieval.NewKeywordRetriever(docs, retrieval.DefaultRetrieverConfig())

	// 测试不同的重排序器
	query := "container technology"
	fmt.Printf("Query: %s\n\n", query)

	// 1. Cross-Encoder Reranking
	fmt.Println("Cross-Encoder Reranking:")
	crossEncoder := retrieval.NewCrossEncoderReranker("cross-encoder-model", 3)

	config1 := retrieval.DefaultRetrieverConfig()
	config1.TopK = 3
	rerankingRetriever1 := retrieval.NewRerankingRetriever(baseRetriever, crossEncoder, 5, config1)

	results1, err := rerankingRetriever1.GetRelevantDocuments(ctx, query)
	if err != nil {
		fmt.Printf("Retrieval error: %v\n", err)
	} else {
		printResults(results1)
	}

	// 2. MMR Reranking (balancing relevance and diversity)
	fmt.Println("\nMMR Reranking (lambda=0.7):")
	mmrReranker := retrieval.NewMMRReranker(0.7, 3)

	config2 := retrieval.DefaultRetrieverConfig()
	config2.TopK = 3
	rerankingRetriever2 := retrieval.NewRerankingRetriever(baseRetriever, mmrReranker, 5, config2)

	results2, err := rerankingRetriever2.GetRelevantDocuments(ctx, query)
	if err != nil {
		fmt.Printf("Retrieval error: %v\n", err)
	} else {
		printResults(results2)
	}
}

// printResults 打印检索结果
func printResults(docs []*retrieval.Document) {
	for i, doc := range docs {
		fmt.Printf("%d. Score: %.4f\n", i+1, doc.Score)
		fmt.Printf("   ID: %s\n", doc.ID)

		// 截断内容显示
		content := doc.PageContent
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		fmt.Printf("   Content: %s\n", content)

		// 显示元数据
		if len(doc.Metadata) > 0 {
			fmt.Printf("   Metadata: %v\n", doc.Metadata)
		}
		fmt.Println()
	}
}
