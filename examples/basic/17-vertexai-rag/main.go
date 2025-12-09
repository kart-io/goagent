// Package main 演示使用 Vertex AI（Gemini）构建 RAG 系统
//
// 本示例参考 https://github.com/meteatamel/genai-beyond-basics/tree/main/samples/grounding/llamaindex-vertexai
// 展示如何：
// 1. 使用 Vertex AI 嵌入模型进行文档向量化
// 2. 使用 Gemini 模型进行问答生成
// 3. 构建完整的 RAG 流程
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
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
	projectID = flag.String("project", "", "Google Cloud 项目 ID")
	location  = flag.String("location", "us-central1", "Vertex AI 区域")
	model     = flag.String("model", "gemini-2.0-flash", "Gemini 模型名称")
	chunkSize = flag.Int("chunk-size", 1024, "文本分块大小")
	overlap   = flag.Int("overlap", 20, "分块重叠大小")
	topK      = flag.Int("topk", 4, "检索返回的文档数量")
	useSimple = flag.Bool("simple", false, "使用简单嵌入器（无需 Vertex AI）")
)

func main() {
	flag.Parse()

	// 验证必需参数
	if *prompt == "" {
		fmt.Println("错误：必须提供查询问题 (-prompt)")
		flag.Usage()
		os.Exit(1)
	}

	// 检查项目 ID
	if *projectID == "" {
		*projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		if *projectID == "" {
			*projectID = os.Getenv("GCLOUD_PROJECT")
		}
	}

	// 仅在 RAG 模式且未使用简单嵌入器时需要 project ID
	if *pdfPath != "" && *projectID == "" && !*useSimple {
		fmt.Println("错误：RAG 模式需要设置 Google Cloud 项目 ID")
		fmt.Println("  方式 1: 使用 -project 参数")
		fmt.Println("  方式 2: 设置 GOOGLE_CLOUD_PROJECT 环境变量")
		fmt.Println("  方式 3: 运行 gcloud config set project YOUR_PROJECT_ID")
		fmt.Println("\n如果没有 Google Cloud 账号，可以使用 -simple 参数运行简单模式")
		os.Exit(1)
	}

	ctx := context.Background()

	fmt.Println("========================================")
	fmt.Println("Vertex AI RAG 演示")
	fmt.Println("========================================")

	// 创建 Gemini LLM 客户端
	fmt.Println("\n初始化 Gemini 模型...")
	llmClient, err := setupGeminiClient()
	if err != nil {
		fmt.Printf("创建 Gemini 客户端失败: %v\n", err)
		fmt.Println("\n提示：确保已完成以下配置：")
		fmt.Println("  1. gcloud auth application-default login")
		fmt.Println("  2. gcloud config set project YOUR_PROJECT_ID")
		os.Exit(1)
	}
	fmt.Printf("  模型: %s\n", *model)

	// 根据是否提供 PDF 文件决定执行模式
	if *pdfPath != "" {
		fmt.Println("\n[RAG 模式] 使用 PDF 文档增强回答")
		if err := runWithRAG(ctx, llmClient, *pdfPath, *prompt); err != nil {
			fmt.Printf("RAG 执行失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("\n[无 RAG 模式] 直接使用 Gemini 回答")
		if err := runWithoutRAG(ctx, llmClient, *prompt); err != nil {
			fmt.Printf("执行失败: %v\n", err)
			os.Exit(1)
		}
	}
}

// setupGeminiClient 创建 Gemini LLM 客户端
func setupGeminiClient() (llm.Client, error) {
	return providers.NewGeminiWithOptions(
		llm.WithModel(*model),
		llm.WithTemperature(0.3),
		llm.WithMaxTokens(2000),
	)
}

// runWithoutRAG 不使用 RAG 直接查询 LLM
func runWithoutRAG(ctx context.Context, client llm.Client, query string) error {
	fmt.Printf("\n问题: %s\n", query)
	fmt.Println("\n正在查询 Gemini...")

	startTime := time.Now()

	response, err := client.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.SystemMessage("你是一个有帮助的 AI 助手。请根据你的知识回答问题。回答请控制在三句话以内。"),
			llm.UserMessage(query),
		},
	})
	if err != nil {
		return fmt.Errorf("gemini 调用失败: %w", err)
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
	fmt.Println("\n步骤 1: 读取 PDF 文档...")
	docs, err := loadPDF(ctx, pdfPath)
	if err != nil {
		return fmt.Errorf("加载 PDF 失败: %w", err)
	}
	fmt.Printf("  已加载 PDF，共 %d 个文档片段\n", len(docs))

	// 步骤 2: 初始化嵌入模型
	fmt.Println("\n步骤 2: 初始化嵌入模型...")
	embedder, err := setupEmbedder(ctx)
	if err != nil {
		return fmt.Errorf("创建嵌入器失败: %w", err)
	}
	if closer, ok := embedder.(interface{ Close() error }); ok {
		defer func() {
			_ = closer.Close()
		}()
	}
	if *useSimple {
		fmt.Println("  使用简单嵌入器（演示模式）")
	} else {
		fmt.Println("  使用 Vertex AI text-embedding-005")
	}

	// 步骤 3: 索引文档
	fmt.Println("\n步骤 3: 索引文档...")
	vectorStore, err := indexDocuments(ctx, docs, embedder)
	if err != nil {
		return fmt.Errorf("索引文档失败: %w", err)
	}
	fmt.Printf("  已索引 %d 个文档片段\n", len(docs))

	// 步骤 4: 创建 RAG 检索器
	fmt.Println("\n步骤 4: 创建检索器...")
	ragRetriever, err := createRAGRetriever(vectorStore)
	if err != nil {
		return fmt.Errorf("创建 RAG 检索器失败: %w", err)
	}

	// 步骤 5: 执行 RAG 查询
	fmt.Printf("\n步骤 5: 查询...\n")
	fmt.Printf("问题: %s\n", query)

	startTime := time.Now()

	// 检索相关文档
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

	// 构建带上下文的提示
	contextPrompt, err := ragRetriever.RetrieveWithContext(ctx, query)
	if err != nil {
		return fmt.Errorf("构建上下文失败: %w", err)
	}
	contextPrompt = strings.ToValidUTF8(contextPrompt, "")

	// 调用 Gemini 生成回答
	response, err := client.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.SystemMessage("你是一个有帮助的 AI 助手。请基于提供的上下文回答问题。回答请控制在三句话以内。如果上下文中没有相关信息，请如实说明。"),
			llm.UserMessage(contextPrompt),
		},
	})
	if err != nil {
		return fmt.Errorf("gemini 调用失败: %w", err)
	}

	elapsed := time.Since(startTime)

	fmt.Println("回答:")
	fmt.Println("----------------------------------------")
	fmt.Println(response.Content)
	fmt.Println("----------------------------------------")
	fmt.Printf("\n总耗时: %v\n", elapsed)
	if response.Usage.TotalTokens > 0 {
		fmt.Printf("Token 使用: %d (输入: %d, 输出: %d)\n",
			response.Usage.TotalTokens,
			response.Usage.PromptTokens,
			response.Usage.CompletionTokens)
	}

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

	// 清理无效的 UTF-8 字符，防止 gRPC 调用失败
	for _, doc := range docs {
		doc.PageContent = strings.ToValidUTF8(doc.PageContent, "")
	}

	// 创建分割器（LlamaIndex 风格：1024 字符块，20 字符重叠）
	splitter := document.NewRecursiveCharacterTextSplitter(
		document.RecursiveCharacterTextSplitterConfig{
			ChunkSize:    *chunkSize,
			ChunkOverlap: *overlap,
		},
	)

	// 分割文档
	return splitter.SplitDocuments(docs)
}

// setupEmbedder 创建嵌入器
//
// 使用统一的 NewEmbedder 工厂函数，支持多种服务商
func setupEmbedder(ctx context.Context) (retrieval.Embedder, error) {
	if *useSimple {
		// 使用简单嵌入器（演示用）
		return retrieval.NewEmbedder(ctx,
			retrieval.WithProvider(retrieval.EmbedderProviderSimple),
			retrieval.WithDimensions(768),
		)
	}

	// 使用 Vertex AI 嵌入器
	return retrieval.NewEmbedder(ctx,
		retrieval.WithProvider(retrieval.EmbedderProviderVertexAI),
		retrieval.WithProjectID(*projectID),
		retrieval.WithLocation(*location),
		retrieval.WithModel("text-embedding-005"),
		retrieval.WithDimensions(768),
	)
}

// indexDocuments 索引文档到向量存储
func indexDocuments(ctx context.Context, docs []*interfaces.Document, embedder retrieval.Embedder) (*retrieval.MemoryVectorStore, error) {
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
		ScoreThreshold:   0.0,
		IncludeMetadata:  true,
		MaxContentLength: 1000,
	})
}
