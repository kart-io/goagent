# GoAgent Retrieval Package - Usage Examples

## Overview

The GoAgent retrieval package provides production-ready implementations for:
- Qdrant vector store integration
- RAG (Retrieval-Augmented Generation) workflows
- Document reranking with Cohere
- Multi-query retrieval strategies

## 1. Qdrant Vector Store

### Basic Setup

```go
import (
    "context"
    "github.com/kart-io/goagent/retrieval"
)

func main() {
    ctx := context.Background()

    // Create Qdrant vector store
    store, err := retrieval.NewQdrantVectorStore(ctx, retrieval.QdrantConfig{
        URL:            "localhost:6334",
        CollectionName: "my_documents",
        VectorSize:     384,
        Distance:       "cosine",
        // Optional: Provide custom embedder
        // Embedder: myCustomEmbedder,
    })
    if err != nil {
        panic(err)
    }
    defer store.Close()
}
```

### Adding Documents

```go
// Create documents
docs := []*retrieval.Document{
    retrieval.NewDocument("Machine learning is a subset of AI", map[string]interface{}{
        "category": "tech",
        "year":     2024,
    }),
    retrieval.NewDocument("Python is a popular programming language", map[string]interface{}{
        "category": "programming",
        "year":     2024,
    }),
}

// Add documents (automatically generates vectors using embedder)
err = store.AddDocuments(ctx, docs)
if err != nil {
    panic(err)
}
```

### Searching Documents

```go
// Simple similarity search
results, err := store.Search(ctx, "what is machine learning?", 5)
if err != nil {
    panic(err)
}

for _, doc := range results {
    fmt.Printf("Score: %.4f, Content: %s\n", doc.Score, doc.PageContent)
}
```

### Advanced: Manual Vector Management

```go
// Manually provide vectors
vectors := [][]float32{
    {0.1, 0.2, 0.3, ...}, // 384 dimensions
    {0.2, 0.3, 0.4, ...},
}

err = store.Add(ctx, docs, vectors)
if err != nil {
    panic(err)
}

// Search by vector
queryVector := []float32{0.15, 0.25, 0.35, ...}
results, err := store.SearchByVector(ctx, queryVector, 10)
```

### Updating and Deleting Documents

```go
// Update documents
docs[0].PageContent = "Updated content"
err = store.Update(ctx, []*retrieval.Document{docs[0]})

// Delete documents by ID
err = store.Delete(ctx, []string{"doc-id-1", "doc-id-2"})
```

## 2. RAG (Retrieval-Augmented Generation)

### Basic RAG Chain

```go
import (
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/retrieval"
)

func main() {
    ctx := context.Background()

    // Setup vector store
    store, _ := retrieval.NewQdrantVectorStore(ctx, retrieval.QdrantConfig{
        CollectionName: "knowledge_base",
        VectorSize:     384,
    })

    // Create RAG retriever
    ragRetriever, err := retrieval.NewRAGRetriever(retrieval.RAGRetrieverConfig{
        VectorStore:      store,
        TopK:             5,
        ScoreThreshold:   0.7,
        IncludeMetadata:  true,
        MaxContentLength: 1000,
    })
    if err != nil {
        panic(err)
    }

    // Create LLM client
    llmClient := llm.NewOpenAIClient(&llm.Config{
        APIKey: "your-api-key",
        Model:  "gpt-4",
    })

    // Create RAG chain
    ragChain := retrieval.NewRAGChain(ragRetriever, llmClient)

    // Run query
    answer, err := ragChain.Run(ctx, "What is the capital of France?")
    if err != nil {
        panic(err)
    }

    fmt.Println("Answer:", answer)
}
```

### RAG Retriever Only (No Generation)

```go
// Create RAG retriever without LLM
ragRetriever, _ := retrieval.NewRAGRetriever(retrieval.RAGRetrieverConfig{
    VectorStore:      store,
    TopK:             5,
    ScoreThreshold:   0.7,
    MaxContentLength: 500,
})

// Retrieve relevant documents
docs, err := ragRetriever.Retrieve(ctx, "machine learning algorithms")
if err != nil {
    panic(err)
}

for _, doc := range docs {
    fmt.Printf("Score: %.4f\n%s\n\n", doc.Score, doc.PageContent)
}
```

### Custom Formatting

```go
// Retrieve and format with custom template
template := `Based on the following {num_docs} documents, answer the question.

Documents:
{documents}

Question: {query}

Provide a concise answer:`

formattedPrompt, err := ragRetriever.RetrieveAndFormat(ctx, "What is Docker?", template)
if err != nil {
    panic(err)
}

// Use formatted prompt with your LLM
```

## 3. Multi-Query Retrieval

Multi-query retrieval generates multiple variations of the query to improve recall.

```go
// Create multi-query retriever
multiQueryRetriever := retrieval.NewRAGMultiQueryRetriever(
    ragRetriever,
    5,         // Generate 5 query variations
    llmClient, // LLM to generate variations
)

// Retrieve with query expansion
docs, err := multiQueryRetriever.Retrieve(ctx, "kubernetes deployment")
// This will:
// 1. Generate 5 query variations using LLM
// 2. Search for each query
// 3. Merge and deduplicate results
// 4. Return top-K documents
```

## 4. Document Reranking

### Cohere Reranker

```go
// Create Cohere reranker
reranker, err := retrieval.NewCohereReranker(
    "your-cohere-api-key",
    "rerank-english-v2.0",
    3, // Return top 3
)
if err != nil {
    panic(err)
}

// Rerank documents
rerankedDocs, err := reranker.Rerank(ctx, "machine learning", docs)
if err != nil {
    panic(err)
}
```

### MMR (Maximal Marginal Relevance) Reranker

```go
// Create MMR reranker for diversity
mmrReranker := retrieval.NewMMRReranker(
    0.7, // Lambda: 0.7 = 70% relevance, 30% diversity
    5,   // Return top 5
)

rerankedDocs, err := mmrReranker.Rerank(ctx, "AI and ML", docs)
```

### Cross-Encoder Reranker

```go
// Create cross-encoder reranker (simulated)
crossEncoder := retrieval.NewCrossEncoderReranker("model-name", 10)
rerankedDocs, err := crossEncoder.Rerank(ctx, query, docs)
```

### Reranking Retriever

Combine retrieval with reranking in one step:

```go
// Create base retriever
baseRetriever := retrieval.NewVectorStoreRetriever(
    store,
    retrieval.DefaultRetrieverConfig(),
)

// Wrap with reranking
rerankingRetriever := retrieval.NewRerankingRetriever(
    baseRetriever,
    reranker,
    20, // Fetch top 20, then rerank
    retrieval.RetrieverConfig{
        TopK:     5,
        MinScore: 0.5,
    },
)

// Get reranked results in one call
finalDocs, err := rerankingRetriever.GetRelevantDocuments(ctx, "query")
```

## 5. Advanced Patterns

### Rank Fusion

Combine results from multiple rerankers:

```go
// Create multiple rerankers
rerankers := []retrieval.Reranker{
    retrieval.NewCrossEncoderReranker("model1", 10),
    retrieval.NewMMRReranker(0.7, 10),
}

// Compare rerankers
comparer := &retrieval.CompareRankers{
    Rerankers: rerankers,
}

results, err := comparer.Compare(ctx, "query", docs)
// results["cross_encoder"] = docs from cross encoder
// results["mmr"] = docs from MMR

// Fuse results with RRF
fusion := retrieval.NewRankFusion("rrf")
rankings := [][]*retrieval.Document{
    results["cross_encoder"],
    results["mmr"],
}
fusedDocs := fusion.Fuse(rankings)
```

### Score Thresholding

```go
ragRetriever.SetScoreThreshold(0.8) // Only return docs with score >= 0.8
ragRetriever.SetTopK(10)             // Return up to 10 docs
```

### Custom Embedder

```go
type MyEmbedder struct{}

func (e *MyEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    // Your embedding logic
    vectors := make([][]float32, len(texts))
    // ... generate vectors
    return vectors, nil
}

func (e *MyEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
    vectors, err := e.Embed(ctx, []string{text})
    if err != nil {
        return nil, err
    }
    return vectors[0], nil
}

// Use custom embedder
store, _ := retrieval.NewQdrantVectorStore(ctx, retrieval.QdrantConfig{
    CollectionName: "my_docs",
    VectorSize:     768,
    Embedder:       &MyEmbedder{},
})
```

## Configuration Options

### Qdrant Config

```go
type QdrantConfig struct {
    URL            string      // Qdrant server address (default: "localhost:6334")
    APIKey         string      // Optional API key
    CollectionName string      // Collection name (required)
    VectorSize     int         // Vector dimensions (default: 100)
    Distance       string      // Distance metric: "cosine", "euclidean", "dot" (default: "cosine")
    Embedder       Embedder    // Embedder for automatic vectorization
}
```

### RAG Retriever Config

```go
type RAGRetrieverConfig struct {
    VectorStore      VectorStore // Vector store instance (required)
    Embedder         Embedder    // Optional embedder
    TopK             int         // Max documents to return (default: 4)
    ScoreThreshold   float32     // Min score threshold (default: 0)
    IncludeMetadata  bool        // Include metadata in output
    MaxContentLength int         // Truncate content (default: 1000)
}
```

## Error Handling

```go
result, err := store.Search(ctx, query, 5)
if err != nil {
    // Check error type
    if agentErr, ok := err.(*errors.AgentError); ok {
        switch agentErr.Code {
        case errors.CodeRetrievalSearch:
            // Handle search error
        case errors.CodeRetrievalEmbedding:
            // Handle embedding error
        case errors.CodeInvalidConfig:
            // Handle config error
        }
    }
}
```

## Performance Tips

1. **Batch Operations**: Use `Add()` with multiple documents instead of adding one by one
2. **Vector Caching**: Pre-compute and cache vectors for frequently used documents
3. **Adjust TopK**: Fetch more documents for reranking (e.g., fetch 20, rerank to top 5)
4. **Connection Pooling**: Reuse Qdrant client connections
5. **Context Timeouts**: Set appropriate context deadlines for long-running operations

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

results, err := store.Search(ctx, query, 10)
```

## Testing

The package includes comprehensive test coverage:

- Unit tests for all components
- Integration tests (skipped in short mode)
- Mock vector store for testing without Qdrant

```go
// Use mock store in tests
mockStore := retrieval.NewMockVectorStore()
mockStore.AddDocuments(ctx, testDocs)

retriever, _ := retrieval.NewRAGRetriever(retrieval.RAGRetrieverConfig{
    VectorStore: mockStore,
    TopK:        5,
})
```

## Migration from Other Libraries

If migrating from LangChain or similar:

```python
# LangChain Python
retriever = vectorstore.as_retriever(search_kwargs={"k": 5})
```

```go
// GoAgent equivalent
retriever := retrieval.NewVectorStoreRetriever(
    vectorstore,
    retrieval.RetrieverConfig{TopK: 5},
)
```

## Additional Resources

- **API Documentation**: See godoc for detailed API reference
- **Examples**: Check `examples/` directory for complete working examples
- **Architecture**: See `DOCUMENTATION_INDEX.md` for architectural overview
- **Testing**: See `TESTING_BEST_PRACTICES.md` for testing guidelines
