package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/retrieval"
)

// DeepSeekRAGExample demonstrates comprehensive RAG functionality with DeepSeek LLM
//
// This example showcases:
// 1. Setting up DeepSeek LLM client
// 2. Creating a Qdrant vector store
// 3. Adding documents to the vector store
// 4. Creating a RAG retriever
// 5. Building a RAG chain with DeepSeek
// 6. Running queries with RAG
// 7. Advanced features: multi-query retrieval, document reranking
//
// Prerequisites:
// - DeepSeek API key (set DEEPSEEK_API_KEY environment variable)
// - Qdrant instance running (default: localhost:6334)
func main() {
	ctx := context.Background()

	// Step 1: Setup DeepSeek LLM client
	fmt.Println("=== Step 1: Setting up DeepSeek LLM Client ===")
	llmClient, err := setupDeepSeekClient()
	if err != nil {
		log.Fatalf("Failed to setup DeepSeek client: %v", err)
	}
	fmt.Println("✓ DeepSeek client initialized successfully")

	// Step 2: Setup Qdrant vector store
	fmt.Println("\n=== Step 2: Setting up Qdrant Vector Store ===")
	vectorStore, err := setupQdrantStore(ctx)
	if err != nil {
		log.Fatalf("Failed to setup Qdrant store: %v", err)
	}
	defer func() {
		if err := vectorStore.Close(); err != nil {
			log.Printf("Error closing vector store: %v", err)
		}
	}()
	fmt.Println("✓ Qdrant vector store initialized successfully")

	// Step 3: Add sample documents about AI and ML
	fmt.Println("\n=== Step 3: Adding Sample Documents ===")
	if err := addSampleDocuments(ctx, vectorStore); err != nil {
		log.Fatalf("Failed to add documents: %v", err)
	}
	fmt.Println("✓ Sample documents added successfully")

	// Step 4: Create RAG retriever with different configurations
	fmt.Println("\n=== Step 4: Creating RAG Retriever ===")
	ragRetriever, err := createRAGRetriever(vectorStore)
	if err != nil {
		log.Fatalf("Failed to create RAG retriever: %v", err)
	}
	fmt.Println("✓ RAG retriever created successfully")

	// Step 5: Create RAG chain combining retriever and LLM
	fmt.Println("\n=== Step 5: Creating RAG Chain ===")
	ragChain := retrieval.NewRAGChain(ragRetriever, llmClient)
	fmt.Println("✓ RAG chain created successfully")

	// Step 6: Run example queries with basic RAG
	fmt.Println("\n=== Step 6: Basic RAG Query ===")
	if err := runBasicRAGQuery(ctx, ragChain); err != nil {
		log.Printf("Basic RAG query failed: %v", err)
	}

	// Step 7: Demonstrate retrieval with different TopK values
	fmt.Println("\n=== Step 7: TopK Configuration ===")
	demonstrateTopKConfiguration(ctx, ragRetriever)

	// Step 8: Demonstrate score threshold filtering
	fmt.Println("\n=== Step 8: Score Threshold Filtering ===")
	demonstrateScoreThreshold(ctx, ragRetriever)

	// Step 9: Demonstrate multi-query retrieval
	fmt.Println("\n=== Step 9: Multi-Query Retrieval ===")
	if err := demonstrateMultiQueryRetrieval(ctx, vectorStore, llmClient); err != nil {
		log.Printf("Multi-query retrieval failed: %v", err)
	}

	// Step 10: Demonstrate document reranking
	fmt.Println("\n=== Step 10: Document Reranking ===")
	demonstrateReranking(ctx, ragRetriever)

	// Step 11: Advanced RAG with custom prompts
	fmt.Println("\n=== Step 11: Advanced RAG with Custom Prompts ===")
	demonstrateCustomPrompts(ctx, ragRetriever, llmClient)

	fmt.Println("\n=== RAG Demo Completed Successfully ===")
}

// setupDeepSeekClient initializes the DeepSeek LLM client
func setupDeepSeekClient() (llm.Client, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY environment variable not set")
	}

	config := &llm.Config{
		Provider:    llm.ProviderDeepSeek,
		APIKey:      apiKey,
		Model:       "deepseek-chat",
		Temperature: 0.7,
		MaxTokens:   2000,
		Timeout:     60,
	}

	return providers.NewDeepSeek(config)
}

// setupQdrantStore initializes the Qdrant vector store
func setupQdrantStore(ctx context.Context) (*retrieval.QdrantVectorStore, error) {
	// Get Qdrant URL from environment or use default
	qdrantURL := os.Getenv("QDRANT_URL")
	if qdrantURL == "" {
		qdrantURL = "localhost:6334"
	}

	// Create embedder (using simple embedder for demo)
	// In production, use a real embedding model like OpenAI embeddings
	embedder := retrieval.NewSimpleEmbedder(384) // 384 dimensions for demo

	config := retrieval.QdrantConfig{
		URL:            qdrantURL,
		CollectionName: "goagent_rag_demo",
		VectorSize:     384,
		Distance:       "cosine",
		Embedder:       embedder,
	}

	return retrieval.NewQdrantVectorStore(ctx, config)
}

// addSampleDocuments adds knowledge base documents to the vector store
func addSampleDocuments(ctx context.Context, store *retrieval.QdrantVectorStore) error {
	documents := []*interfaces.Document{
		{
			ID:          "ml-001",
			PageContent: "Machine Learning is a subset of artificial intelligence that enables systems to learn and improve from experience without being explicitly programmed. It focuses on the development of computer programs that can access data and use it to learn for themselves. The process of learning begins with observations or data, such as examples, direct experience, or instruction, in order to look for patterns in data and make better decisions in the future.",
			Metadata: map[string]interface{}{
				"topic":    "machine_learning",
				"category": "introduction",
				"keywords": []string{"ML", "AI", "learning", "data"},
			},
		},
		{
			ID:          "dl-001",
			PageContent: "Deep Learning is a specialized subset of machine learning that uses artificial neural networks with multiple layers (deep neural networks) to progressively extract higher-level features from raw input. For example, in image processing, lower layers may identify edges, while higher layers may identify concepts relevant to a human such as digits, letters, or faces. Deep learning is particularly effective at processing large amounts of unstructured data and has achieved remarkable results in computer vision, natural language processing, and speech recognition.",
			Metadata: map[string]interface{}{
				"topic":    "deep_learning",
				"category": "advanced",
				"keywords": []string{"neural networks", "layers", "features"},
			},
		},
		{
			ID:          "nlp-001",
			PageContent: "Natural Language Processing (NLP) is a branch of artificial intelligence that helps computers understand, interpret, and manipulate human language. NLP draws from many disciplines, including computer science and computational linguistics, in its pursuit to fill the gap between human communication and computer understanding. Modern NLP techniques use machine learning and deep learning to process and analyze large amounts of natural language data. Applications include sentiment analysis, machine translation, chatbots, and text summarization.",
			Metadata: map[string]interface{}{
				"topic":    "natural_language_processing",
				"category": "application",
				"keywords": []string{"NLP", "language", "text", "processing"},
			},
		},
		{
			ID:          "cv-001",
			PageContent: "Computer Vision is a field of artificial intelligence that trains computers to interpret and understand the visual world. Using digital images from cameras and videos and deep learning models, machines can accurately identify and classify objects and then react to what they see. Computer vision works in three basic steps: acquiring an image, processing the image, and understanding the image. Applications include facial recognition, autonomous vehicles, medical image analysis, and augmented reality.",
			Metadata: map[string]interface{}{
				"topic":    "computer_vision",
				"category": "application",
				"keywords": []string{"vision", "images", "recognition", "visual"},
			},
		},
		{
			ID:          "rl-001",
			PageContent: "Reinforcement Learning is an area of machine learning concerned with how intelligent agents ought to take actions in an environment in order to maximize the notion of cumulative reward. Unlike supervised learning, reinforcement learning does not need labeled input/output pairs. Instead, the focus is on finding a balance between exploration of uncharted territory and exploitation of current knowledge. RL has been successfully applied to game playing, robotics, resource management, and autonomous systems.",
			Metadata: map[string]interface{}{
				"topic":    "reinforcement_learning",
				"category": "advanced",
				"keywords": []string{"RL", "agents", "rewards", "environment"},
			},
		},
		{
			ID:          "nn-001",
			PageContent: "Neural Networks are computing systems inspired by the biological neural networks that constitute animal brains. An artificial neural network is based on a collection of connected units or nodes called artificial neurons, which loosely model the neurons in a biological brain. Each connection, like the synapses in a biological brain, can transmit a signal to other neurons. The receiving neuron processes the signal and then signals downstream neurons connected to it. Neural networks can be trained to perform classification, regression, and pattern recognition tasks.",
			Metadata: map[string]interface{}{
				"topic":    "neural_networks",
				"category": "fundamental",
				"keywords": []string{"neurons", "connections", "layers", "training"},
			},
		},
		{
			ID:          "transformer-001",
			PageContent: "Transformers are a type of neural network architecture that has become the foundation for most modern natural language processing models. Introduced in the paper 'Attention is All You Need', transformers use a mechanism called self-attention to process sequential data in parallel, making them much more efficient than previous recurrent architectures. The transformer architecture consists of an encoder and decoder, each composed of multiple layers with self-attention and feed-forward networks. Models like BERT, GPT, and T5 are all based on the transformer architecture.",
			Metadata: map[string]interface{}{
				"topic":    "transformers",
				"category": "architecture",
				"keywords": []string{"attention", "encoder", "decoder", "BERT", "GPT"},
			},
		},
		{
			ID:          "rag-001",
			PageContent: "Retrieval-Augmented Generation (RAG) is a technique that combines information retrieval with text generation to create more accurate and contextually relevant responses. RAG systems first retrieve relevant documents from a knowledge base using semantic search, then use these documents as context for a language model to generate responses. This approach helps overcome the limitations of language models by grounding their outputs in factual information from the knowledge base. RAG is particularly useful for question answering, chatbots, and knowledge-intensive tasks.",
			Metadata: map[string]interface{}{
				"topic":    "rag",
				"category": "technique",
				"keywords": []string{"retrieval", "generation", "knowledge base", "context"},
			},
		},
	}

	return store.AddDocuments(ctx, documents)
}

// createRAGRetriever creates a RAG retriever with optimized configuration
func createRAGRetriever(store *retrieval.QdrantVectorStore) (*retrieval.RAGRetriever, error) {
	config := retrieval.RAGRetrieverConfig{
		VectorStore:      store,
		TopK:             4,
		ScoreThreshold:   0.3,
		IncludeMetadata:  true,
		MaxContentLength: 500,
	}

	return retrieval.NewRAGRetriever(config)
}

// runBasicRAGQuery demonstrates a basic RAG query
func runBasicRAGQuery(ctx context.Context, chain *retrieval.RAGChain) error {
	query := "What is machine learning and how does it work?"

	fmt.Printf("Query: %s\n", query)
	fmt.Println("Retrieving relevant documents and generating answer...")

	startTime := time.Now()
	answer, err := chain.Run(ctx, query)
	if err != nil {
		return fmt.Errorf("RAG query failed: %w", err)
	}
	elapsed := time.Since(startTime)

	fmt.Printf("\nAnswer:\n%s\n", answer)
	fmt.Printf("\nQuery completed in: %v\n", elapsed)

	return nil
}

// demonstrateTopKConfiguration shows different TopK settings
func demonstrateTopKConfiguration(ctx context.Context, retriever *retrieval.RAGRetriever) {
	query := "Tell me about neural networks and deep learning"

	// Test with different TopK values
	topKValues := []int{2, 4, 6}

	for _, topK := range topKValues {
		retriever.SetTopK(topK)
		fmt.Printf("\n--- TopK = %d ---\n", topK)

		docs, err := retriever.Retrieve(ctx, query)
		if err != nil {
			log.Printf("Retrieval failed: %v", err)
			continue
		}

		fmt.Printf("Retrieved %d documents:\n", len(docs))
		for i, doc := range docs {
			fmt.Printf("%d. [Score: %.4f] %s\n",
				i+1,
				doc.Score,
				truncateString(doc.PageContent, 100))
		}
	}
}

// demonstrateScoreThreshold shows score threshold filtering
func demonstrateScoreThreshold(ctx context.Context, retriever *retrieval.RAGRetriever) {
	query := "What is reinforcement learning?"

	// Test with different score thresholds
	thresholds := []float32{0.0, 0.3, 0.5}

	for _, threshold := range thresholds {
		retriever.SetScoreThreshold(threshold)
		fmt.Printf("\n--- Score Threshold = %.2f ---\n", threshold)

		docs, err := retriever.Retrieve(ctx, query)
		if err != nil {
			log.Printf("Retrieval failed: %v", err)
			continue
		}

		fmt.Printf("Retrieved %d documents (filtered by threshold):\n", len(docs))
		for i, doc := range docs {
			fmt.Printf("%d. [Score: %.4f] Topic: %v\n",
				i+1,
				doc.Score,
				doc.Metadata["topic"])
		}
	}
}

// demonstrateMultiQueryRetrieval shows multi-query retrieval for improved recall
func demonstrateMultiQueryRetrieval(ctx context.Context, store *retrieval.QdrantVectorStore, llmClient llm.Client) error {
	query := "How do neural networks learn?"

	// Create base retriever
	baseRetriever, err := createRAGRetriever(store)
	if err != nil {
		return fmt.Errorf("failed to create base retriever: %w", err)
	}

	// Create multi-query retriever
	multiQueryRetriever := retrieval.NewRAGMultiQueryRetriever(
		baseRetriever,
		3, // Generate 3 query variations
		llmClient,
	)

	fmt.Printf("Original Query: %s\n", query)
	fmt.Println("Generating query variations and retrieving documents...")

	startTime := time.Now()
	docs, err := multiQueryRetriever.Retrieve(ctx, query)
	if err != nil {
		return fmt.Errorf("multi-query retrieval failed: %w", err)
	}
	elapsed := time.Since(startTime)

	fmt.Printf("\nRetrieved %d unique documents (merged from multiple queries):\n", len(docs))
	for i, doc := range docs {
		fmt.Printf("%d. [Score: %.4f] Topic: %v\n   Preview: %s\n",
			i+1,
			doc.Score,
			doc.Metadata["topic"],
			truncateString(doc.PageContent, 100))
	}
	fmt.Printf("\nMulti-query retrieval completed in: %v\n", elapsed)

	return nil
}

// demonstrateReranking shows document reranking strategies
func demonstrateReranking(ctx context.Context, baseRetriever *retrieval.RAGRetriever) {
	query := "What are the applications of artificial intelligence?"

	fmt.Printf("Query: %s\n", query)

	// Get initial retrieval results
	docs, err := baseRetriever.Retrieve(ctx, query)
	if err != nil {
		log.Printf("Initial retrieval failed: %v", err)
		return
	}

	fmt.Println("\n--- Original Ranking (by similarity score) ---")
	printDocuments(docs)

	// Demonstrate MMR reranking (balances relevance and diversity)
	fmt.Println("\n--- MMR Reranking (lambda=0.7) ---")
	mmrReranker := retrieval.NewMMRReranker(0.7, 4)
	rerankedMMR, err := mmrReranker.Rerank(ctx, query, docs)
	if err != nil {
		log.Printf("MMR reranking failed: %v", err)
	} else {
		printDocuments(rerankedMMR)
	}

	// Demonstrate Cross-Encoder reranking (simulated)
	fmt.Println("\n--- Cross-Encoder Reranking ---")
	crossEncoderReranker := retrieval.NewCrossEncoderReranker("cross-encoder/ms-marco-MiniLM-L-6-v2", 4)
	rerankedCE, err := crossEncoderReranker.Rerank(ctx, query, docs)
	if err != nil {
		log.Printf("Cross-encoder reranking failed: %v", err)
	} else {
		printDocuments(rerankedCE)
	}

	// Demonstrate rank fusion
	fmt.Println("\n--- Rank Fusion (RRF) ---")
	rankFusion := retrieval.NewRankFusion("rrf")
	fusedDocs := rankFusion.Fuse([][]*interfaces.Document{rerankedMMR, rerankedCE})
	printDocuments(fusedDocs[:min(4, len(fusedDocs))])
}

// demonstrateCustomPrompts shows RAG with custom prompt templates
func demonstrateCustomPrompts(ctx context.Context, retriever *retrieval.RAGRetriever, llmClient llm.Client) {
	query := "Explain transformers in simple terms"

	// Custom prompt template for educational content
	customTemplate := `You are an AI tutor helping students understand complex topics.

Using the following reference materials, explain the concept in a clear, structured way suitable for beginners.

Reference Materials:
{documents}

Student Question: {query}

Please provide:
1. A simple explanation
2. Key concepts to remember
3. A real-world analogy if applicable

Your Response:`

	fmt.Printf("Query: %s\n", query)
	fmt.Println("Using custom educational prompt template...")

	// Format the prompt with retrieved documents
	formattedPrompt, err := retriever.RetrieveAndFormat(ctx, query, customTemplate)
	if err != nil {
		log.Printf("Failed to format prompt: %v", err)
		return
	}

	// Generate answer using the custom prompt
	response, err := llmClient.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage(formattedPrompt),
		},
		Temperature: 0.7,
		MaxTokens:   1000,
	})

	if err != nil {
		log.Printf("LLM generation failed: %v", err)
		return
	}

	fmt.Printf("\nCustom Prompt Response:\n%s\n", response.Content)
	fmt.Printf("\nTokens used: %d (Prompt: %d, Completion: %d)\n",
		response.TokensUsed,
		response.Usage.PromptTokens,
		response.Usage.CompletionTokens)
}

// Helper functions

// printDocuments prints document information
func printDocuments(docs []*interfaces.Document) {
	for i, doc := range docs {
		fmt.Printf("%d. [Score: %.4f] Topic: %v\n   Content: %s\n",
			i+1,
			doc.Score,
			doc.Metadata["topic"],
			truncateString(doc.PageContent, 150))
	}
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
