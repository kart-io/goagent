package main

import (
	"context"
	"fmt"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/retrieval"
)

func main() {
	fmt.Println("=== Advanced Retrieval Example ===")
	fmt.Println()

	// 创建技术文档集合
	docs := createTechDocs()
	ctx := context.Background()

	// 1. 带回调的检索
	fmt.Println("1. Retrieval with Callbacks")
	fmt.Println("---------------------------")
	retrievalWithCallbacks(ctx, docs)

	// 2. 管道式检索
	fmt.Println("\n2. Retrieval Pipeline")
	fmt.Println("---------------------------")
	retrievalPipeline(ctx, docs)

	// 3. 批量检索
	fmt.Println("\n3. Batch Retrieval")
	fmt.Println("---------------------------")
	batchRetrieval(ctx, docs)

	// 4. 自定义检索器
	fmt.Println("\n4. Custom Retriever")
	fmt.Println("---------------------------")
	customRetriever(ctx, docs)
}

// createTechDocs 创建技术文档集合
func createTechDocs() []*interfaces.Document {
	return []*interfaces.Document{
		retrieval.NewDocument(
			"Kubernetes provides automatic bin packing, self-healing, horizontal scaling, service discovery and load balancing, automated rollouts and rollbacks, secret and configuration management, storage orchestration, and batch execution.",
			map[string]interface{}{
				"source":   "k8s_features.md",
				"category": "kubernetes",
				"tags":     []string{"orchestration", "containers", "cloud-native"},
			},
		),
		retrieval.NewDocument(
			"Docker Compose is a tool for defining and running multi-container Docker applications. With Compose, you use a YAML file to configure your application's services, networks, and volumes.",
			map[string]interface{}{
				"source":   "docker_compose.md",
				"category": "docker",
				"tags":     []string{"containers", "development"},
			},
		),
		retrieval.NewDocument(
			"Microservices architecture is an approach where a single application is composed of many loosely coupled and independently deployable smaller services. Each service runs in its own process and communicates via HTTP APIs or messaging queues.",
			map[string]interface{}{
				"source":   "microservices.md",
				"category": "architecture",
				"tags":     []string{"design-patterns", "distributed-systems"},
			},
		),
		retrieval.NewDocument(
			"CI/CD (Continuous Integration/Continuous Deployment) automates the software delivery pipeline. CI involves automatically building and testing code changes, while CD automates deployment to production environments.",
			map[string]interface{}{
				"source":   "cicd.md",
				"category": "devops",
				"tags":     []string{"automation", "deployment"},
			},
		),
		retrieval.NewDocument(
			"Service mesh provides a dedicated infrastructure layer for handling service-to-service communication. Popular service mesh solutions include Istio, Linkerd, and Consul Connect, offering features like traffic management, security, and observability.",
			map[string]interface{}{
				"source":   "service_mesh.md",
				"category": "infrastructure",
				"tags":     []string{"networking", "microservices"},
			},
		),
	}
}

// retrievalWithCallbacks 带回调的检索示例
func retrievalWithCallbacks(ctx context.Context, docs []*interfaces.Document) {
	// 创建自定义回调
	callback := &CustomRetrievalCallback{}

	// 创建检索器
	config := retrieval.DefaultRetrieverConfig()
	config.TopK = 3

	retriever := retrieval.NewKeywordRetriever(docs, config)

	// 添加回调
	retrieverWithCB := retriever.WithCallbacks(callback)

	// 执行检索
	query := "container orchestration features"
	fmt.Printf("Query: %s\n\n", query)

	results, err := retrieverWithCB.Invoke(ctx, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Retrieved %d documents\n", len(results))
}

// CustomRetrievalCallback 自定义检索回调
type CustomRetrievalCallback struct {
	core.BaseCallback
}

func (c *CustomRetrievalCallback) OnStart(ctx context.Context, input interface{}) error {
	query, ok := input.(string)
	if ok {
		fmt.Printf("  [Callback] Starting retrieval for query: '%s'\n", query)
	}
	return nil
}

func (c *CustomRetrievalCallback) OnEnd(ctx context.Context, output interface{}) error {
	docs, ok := output.([]*retrieval.Document)
	if ok {
		fmt.Printf("  [Callback] Retrieval completed, found %d documents\n", len(docs))
		for i, doc := range docs {
			fmt.Printf("    %d. Score: %.4f, ID: %s\n", i+1, doc.Score, doc.ID)
		}
	}
	return nil
}

func (c *CustomRetrievalCallback) OnError(ctx context.Context, err error) error {
	fmt.Printf("  [Callback] Retrieval error: %v\n", err)
	return nil
}

// retrievalPipeline 管道式检索示例
func retrievalPipeline(ctx context.Context, docs []*interfaces.Document) {
	// 创建检索器
	config := retrieval.DefaultRetrieverConfig()
	config.TopK = 5

	retriever := retrieval.NewKeywordRetriever(docs, config)

	// 创建自定义处理检索器
	filterRetriever := &FilterRetriever{
		BaseRetriever: retrieval.NewBaseRetriever(),
		retriever:     retriever,
		minScore:      0.3,
	}

	enrichRetriever := &EnrichRetriever{
		BaseRetriever: retrieval.NewBaseRetriever(),
		retriever:     filterRetriever,
	}

	// 使用组合的检索器
	pipeline := enrichRetriever

	// 执行管道
	query := "microservices deployment automation"
	fmt.Printf("Query: %s\n\n", query)

	results, err := pipeline.GetRelevantDocuments(ctx, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// 显示结果
	fmt.Printf("\nFinal Results:\n")
	for i, doc := range results {
		fmt.Printf("%d. Score: %.4f, Enriched: %v\n",
			i+1, doc.Score, doc.Metadata["enriched"])
	}
}

// batchRetrieval 批量检索示例
func batchRetrieval(ctx context.Context, docs []*retrieval.Document) {
	// 创建检索器
	config := retrieval.DefaultRetrieverConfig()
	config.TopK = 2

	retriever := retrieval.NewKeywordRetriever(docs, config)

	// 批量查询
	queries := []string{
		"container orchestration",
		"continuous deployment",
		"service communication",
	}

	fmt.Printf("Batch queries: %v\n\n", queries)

	// 执行批量检索
	results, err := retriever.Batch(ctx, queries)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// 显示每个查询的结果
	for i, query := range queries {
		fmt.Printf("Query %d: '%s'\n", i+1, query)
		if i < len(results) {
			for j, doc := range results[i] {
				fmt.Printf("  %d. Score: %.4f, Category: %v\n",
					j+1, doc.Score, doc.Metadata["category"])
			}
		}
		fmt.Println()
	}
}

// customRetriever 自定义检索器示例
func customRetriever(ctx context.Context, docs []*interfaces.Document) {
	// 创建自定义检索器
	retriever := NewCategoryRetriever(docs, "kubernetes")

	query := "orchestration features"
	fmt.Printf("Query: %s\n", query)
	fmt.Printf("Filter Category: kubernetes\n\n")

	results, err := retriever.GetRelevantDocuments(ctx, query)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// 显示结果
	for i, doc := range results {
		fmt.Printf("%d. Score: %.4f\n", i+1, doc.Score)
		fmt.Printf("   Category: %v\n", doc.Metadata["category"])
		fmt.Printf("   Content: %s\n\n", truncate(doc.PageContent, 80))
	}
}

// CategoryRetriever 基于类别的自定义检索器
type CategoryRetriever struct {
	*retrieval.BaseRetriever
	docs     []*retrieval.Document
	category string
}

// NewCategoryRetriever 创建类别检索器
func NewCategoryRetriever(docs []*retrieval.Document, category string) *CategoryRetriever {
	return &CategoryRetriever{
		BaseRetriever: retrieval.NewBaseRetriever(),
		docs:          docs,
		category:      category,
	}
}

// GetRelevantDocuments 实现检索逻辑
func (c *CategoryRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*retrieval.Document, error) {
	// 1. 先按类别过滤
	filtered := make([]*retrieval.Document, 0)
	for _, doc := range c.docs {
		if cat, ok := doc.Metadata["category"]; ok && cat == c.category {
			filtered = append(filtered, doc)
		}
	}

	if len(filtered) == 0 {
		return []*retrieval.Document{}, nil
	}

	// 2. 使用关键词检索对过滤后的文档进行排序
	keywordRetriever := retrieval.NewKeywordRetriever(filtered, retrieval.DefaultRetrieverConfig())
	results, err := keywordRetriever.GetRelevantDocuments(ctx, query)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// FilterRetriever 过滤检索器
type FilterRetriever struct {
	*retrieval.BaseRetriever
	retriever retrieval.Retriever
	minScore  float64
}

func (f *FilterRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*retrieval.Document, error) {
	docs, err := f.retriever.GetRelevantDocuments(ctx, query)
	if err != nil {
		return nil, err
	}

	filtered := make([]*retrieval.Document, 0)
	for _, doc := range docs {
		if doc.Score > f.minScore {
			filtered = append(filtered, doc)
		}
	}
	fmt.Printf("  Filtered: %d -> %d documents (score > %.2f)\n", len(docs), len(filtered), f.minScore)
	return filtered, nil
}

// EnrichRetriever 增强检索器
type EnrichRetriever struct {
	*retrieval.BaseRetriever
	retriever retrieval.Retriever
}

func (e *EnrichRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*retrieval.Document, error) {
	docs, err := e.retriever.GetRelevantDocuments(ctx, query)
	if err != nil {
		return nil, err
	}

	for _, doc := range docs {
		doc.SetMetadata("enriched", true)
		doc.SetMetadata("pipeline_stage", "enriched")
	}
	fmt.Printf("  Enriched %d documents with metadata\n", len(docs))
	return docs, nil
}
