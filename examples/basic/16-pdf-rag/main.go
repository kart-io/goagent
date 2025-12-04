// Package main 演示使用 PDF 文档构建 RAG（检索增强生成）系统
//
// 本示例参考 https://github.com/meteatamel/genai-beyond-basics/tree/main/samples/grounding/rag-pdf-annoy
// 展示如何：
// 1. 加载 PDF 文档并提取文本
// 2. 将文本分割成小块（Chunking）
// 3. 使用向量存储进行文档索引
// 4. 构建 RAG 链进行问答
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/document"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/retrieval"
)

// 命令行参数
var (
	pdfPath   = flag.String("pdf", "", "PDF 文件路径（必需）")
	prompt    = flag.String("prompt", "", "查询问题（必需）")
	withRAG   = flag.Bool("rag", true, "是否启用 RAG（默认启用）")
	chunkSize = flag.Int("chunk-size", 500, "文本分块大小")
	overlap   = flag.Int("overlap", 100, "分块重叠大小")
	topK      = flag.Int("topk", 4, "检索返回的文档数量")
)

func main() {
	flag.Parse()

	// 验证必需参数
	if *prompt == "" {
		fmt.Println("错误：必须提供查询问题 (-prompt)")
		flag.Usage()
		os.Exit(1)
	}

	// 获取 API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Println("错误：请设置 DEEPSEEK_API_KEY 环境变量")
		fmt.Println("提示：export DEEPSEEK_API_KEY=your-api-key")
		os.Exit(1)
	}

	ctx := context.Background()

	// 创建 LLM 客户端
	fmt.Println("========================================")
	fmt.Println("PDF RAG 演示")
	fmt.Println("========================================")

	llmClient, err := setupLLMClient(apiKey)
	if err != nil {
		fmt.Printf("创建 LLM 客户端失败: %v\n", err)
		os.Exit(1)
	}

	// 根据是否提供 PDF 文件决定执行模式
	if *pdfPath != "" && *withRAG {
		// 启用 RAG 模式
		fmt.Println("\n[RAG 模式] 使用 PDF 文档增强回答")
		if err := runWithRAG(ctx, llmClient, *pdfPath, *prompt); err != nil {
			fmt.Printf("RAG 执行失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		// 无 RAG 模式
		fmt.Println("\n[无 RAG 模式] 直接使用 LLM 回答")
		if err := runWithoutRAG(ctx, llmClient, *prompt); err != nil {
			fmt.Printf("执行失败: %v\n", err)
			os.Exit(1)
		}
	}
}

// setupLLMClient 创建 DeepSeek LLM 客户端
func setupLLMClient(apiKey string) (llm.Client, error) {
	return providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithTemperature(0.3),
		llm.WithMaxTokens(2000),
	)
}

// runWithoutRAG 不使用 RAG 直接查询 LLM
func runWithoutRAG(ctx context.Context, client llm.Client, query string) error {
	fmt.Printf("\n问题: %s\n", query)
	fmt.Println("\n正在查询 LLM...")

	startTime := time.Now()

	response, err := client.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.SystemMessage("你是一个有帮助的 AI 助手。请根据你的知识回答问题。如果你不确定，请坦诚说明。"),
			llm.UserMessage(query),
		},
	})
	if err != nil {
		return fmt.Errorf("LLM 调用失败: %w", err)
	}

	elapsed := time.Since(startTime)

	fmt.Println("\n回答:")
	fmt.Println("----------------------------------------")
	fmt.Println(response.Content)
	fmt.Println("----------------------------------------")
	fmt.Printf("\n耗时: %v\n", elapsed)
	if response.Usage.TotalTokens > 0 {
		fmt.Printf("Token 使用: %d (输入: %d, 输出: %d)\n",
			response.Usage.TotalTokens,
			response.Usage.PromptTokens,
			response.Usage.CompletionTokens)
	}

	return nil
}

// runWithRAG 使用 RAG 增强查询
func runWithRAG(ctx context.Context, client llm.Client, pdfPath, query string) error {
	// 步骤 1: 加载 PDF 文档
	fmt.Println("\n步骤 1: 加载 PDF 文档...")
	docs, err := loadPDF(ctx, pdfPath)
	if err != nil {
		return fmt.Errorf("加载 PDF 失败: %w", err)
	}
	fmt.Printf("  已加载 PDF，共 %d 个文档片段\n", len(docs))

	// 显示文档统计
	totalChars := 0
	for _, doc := range docs {
		totalChars += len(doc.PageContent)
	}
	fmt.Printf("  总字符数: %d\n", totalChars)

	// 步骤 2: 创建向量存储并索引文档
	fmt.Println("\n步骤 2: 创建向量存储并索引文档...")
	vectorStore, err := setupVectorStore(ctx, docs)
	if err != nil {
		return fmt.Errorf("创建向量存储失败: %w", err)
	}
	fmt.Printf("  已索引 %d 个文档片段\n", len(docs))

	// 步骤 3: 创建 RAG 检索器
	fmt.Println("\n步骤 3: 创建 RAG 检索器...")
	ragRetriever, err := createRAGRetriever(vectorStore)
	if err != nil {
		return fmt.Errorf("创建 RAG 检索器失败: %w", err)
	}
	fmt.Printf("  TopK: %d\n", *topK)

	// 步骤 4: 创建 RAG 链
	fmt.Println("\n步骤 4: 创建 RAG 链...")
	ragChain := retrieval.NewRAGChain(ragRetriever, client)

	// 步骤 5: 执行 RAG 查询
	fmt.Printf("\n步骤 5: 执行 RAG 查询...\n")
	fmt.Printf("问题: %s\n", query)

	startTime := time.Now()

	// 先显示检索到的相关文档
	fmt.Println("\n检索到的相关文档:")
	fmt.Println("----------------------------------------")
	retrievedDocs, err := ragRetriever.Retrieve(ctx, query)
	if err != nil {
		return fmt.Errorf("检索文档失败: %w", err)
	}

	for i, doc := range retrievedDocs {
		fmt.Printf("[%d] (相似度: %.4f)\n", i+1, doc.Score)
		preview := doc.PageContent
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		fmt.Printf("    %s\n\n", preview)
	}

	// 执行完整的 RAG 查询
	answer, err := ragChain.Run(ctx, query)
	if err != nil {
		return fmt.Errorf("RAG 查询失败: %w", err)
	}

	elapsed := time.Since(startTime)

	fmt.Println("回答:")
	fmt.Println("----------------------------------------")
	fmt.Println(answer)
	fmt.Println("----------------------------------------")
	fmt.Printf("\n总耗时: %v\n", elapsed)

	return nil
}

// loadPDF 加载并分割 PDF 文档
func loadPDF(ctx context.Context, pdfPath string) ([]*interfaces.Document, error) {
	// 创建 PDF 加载器
	loader := document.NewPDFLoader(document.PDFLoaderConfig{
		Source: pdfPath,
	})

	// 先加载 PDF
	docs, err := loader.Load(ctx)
	if err != nil {
		return nil, err
	}

	// 创建递归文本分割器
	splitter := document.NewRecursiveCharacterTextSplitter(
		document.RecursiveCharacterTextSplitterConfig{
			ChunkSize:    *chunkSize,
			ChunkOverlap: *overlap,
		},
	)

	// 手动分割文档
	return splitter.SplitDocuments(docs)
}

// setupVectorStore 创建并配置向量存储
func setupVectorStore(ctx context.Context, docs []*interfaces.Document) (*retrieval.MemoryVectorStore, error) {
	// 使用简单嵌入器（用于演示）
	// 生产环境建议使用 OpenAI Embeddings 或其他专业嵌入模型
	embedder := retrieval.NewSimpleEmbedder(384)

	// 创建内存向量存储
	store := retrieval.NewMemoryVectorStore(retrieval.MemoryVectorStoreConfig{
		Embedder:       embedder,
		DistanceMetric: retrieval.DistanceMetricCosine,
	})

	// 添加文档
	if err := store.AddDocuments(ctx, docs); err != nil {
		return nil, err
	}

	return store, nil
}

// createRAGRetriever 创建 RAG 检索器
func createRAGRetriever(store *retrieval.MemoryVectorStore) (*retrieval.RAGRetriever, error) {
	return retrieval.NewRAGRetriever(retrieval.RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             *topK,
		ScoreThreshold:   0.0, // 禁用分数阈值，让所有文档都返回
		IncludeMetadata:  true,
		MaxContentLength: 1000,
	})
}
