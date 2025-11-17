package retrieval

import (
	"context"
	"fmt"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

// MultiQueryRetriever 多查询检索器
//
// 使用 LLM 生成多个查询变体，并对所有变体进行检索，最后合并结果
type MultiQueryRetriever struct {
	*BaseRetriever

	// BaseRetriever 基础检索器
	Retriever Retriever

	// LLMClient LLM 客户端（用于生成查询变体）
	LLMClient llm.Client

	// NumQueries 生成的查询数量
	NumQueries int

	// QueryPrompt 查询生成提示词
	QueryPrompt string
}

// NewMultiQueryRetriever 创建多查询检索器
func NewMultiQueryRetriever(
	baseRetriever Retriever,
	llmClient llm.Client,
	numQueries int,
	config RetrieverConfig,
) *MultiQueryRetriever {
	retriever := &MultiQueryRetriever{
		BaseRetriever: NewBaseRetriever(),
		Retriever:     baseRetriever,
		LLMClient:     llmClient,
		NumQueries:    numQueries,
		QueryPrompt:   defaultQueryGenerationPrompt,
	}

	retriever.TopK = config.TopK
	retriever.MinScore = config.MinScore
	retriever.Name = config.Name

	return retriever
}

// defaultQueryGenerationPrompt 默认的查询生成提示词
const defaultQueryGenerationPrompt = `You are an AI assistant helping generate alternative search queries.
Given the following question, generate {{.NumQueries}} different versions of the question
to retrieve relevant documents from a vector database.

These alternative questions should:
- Use different wording and phrasing
- Approach the question from different angles
- Maintain the same core intent and meaning

Original question: {{.Question}}

Generate {{.NumQueries}} alternative questions (one per line):`

// GetRelevantDocuments 检索相关文档
func (m *MultiQueryRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error) {
	// 1. 生成查询变体
	queries, err := m.generateQueries(ctx, query)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeLLMRequest, "failed to generate queries").
			WithComponent("multi_query_retriever").
			WithOperation("get_relevant_documents").
			WithContext("query", query)
	}

	// 2. 对每个查询执行检索
	allDocs := make(map[string]*Document) // 使用 map 去重
	docCounts := make(map[string]int)     // 记录每个文档出现的次数

	for _, q := range queries {
		docs, err := m.Retriever.GetRelevantDocuments(ctx, q)
		if err != nil {
			// 记录错误但继续处理其他查询
			continue
		}

		for _, doc := range docs {
			if existing, ok := allDocs[doc.ID]; ok {
				// 文档已存在，累加分数
				existing.Score += doc.Score
				docCounts[doc.ID]++
			} else {
				// 新文档
				allDocs[doc.ID] = doc.Clone()
				docCounts[doc.ID] = 1
			}
		}
	}

	// 3. 计算平均分数
	results := make([]*Document, 0, len(allDocs))
	for id, doc := range allDocs {
		doc.Score = doc.Score / float64(docCounts[id])
		results = append(results, doc)
	}

	// 4. 排序和过滤
	collection := DocumentCollection(results)
	collection.SortByScore()

	filtered := m.FilterByScore(collection)
	limited := m.LimitTopK(filtered)

	return limited, nil
}

// generateQueries 生成查询变体
func (m *MultiQueryRetriever) generateQueries(ctx context.Context, query string) ([]string, error) {
	// 构建提示词
	prompt := m.buildPrompt(query)

	// 调用 LLM
	response, err := m.LLMClient.Complete(ctx, &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage(prompt),
		},
		Temperature: 0.7,
		MaxTokens:   500,
	})
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeLLMRequest, "LLM generation failed").
			WithComponent("multi_query_retriever").
			WithOperation("generate_queries").
			WithContext("query", query)
	}

	// 解析生成的查询
	queries := m.parseQueries(response.Content)

	// 包含原始查询
	allQueries := []string{query}
	allQueries = append(allQueries, queries...)

	return allQueries, nil
}

// buildPrompt 构建提示词
func (m *MultiQueryRetriever) buildPrompt(query string) string {
	prompt := strings.ReplaceAll(m.QueryPrompt, "{{.Question}}", query)
	prompt = strings.ReplaceAll(prompt, "{{.NumQueries}}", fmt.Sprintf("%d", m.NumQueries))
	return prompt
}

// parseQueries 解析生成的查询
func (m *MultiQueryRetriever) parseQueries(text string) []string {
	lines := strings.Split(text, "\n")
	queries := make([]string, 0)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// 跳过空行和编号
		if line == "" {
			continue
		}

		// 移除可能的编号前缀 (1., 2., etc.)
		line = strings.TrimLeft(line, "0123456789.)")
		line = strings.TrimSpace(line)

		if line != "" {
			queries = append(queries, line)
		}
	}

	return queries
}

// WithQueryPrompt 设置查询生成提示词
func (m *MultiQueryRetriever) WithQueryPrompt(prompt string) *MultiQueryRetriever {
	m.QueryPrompt = prompt
	return m
}

// WithNumQueries 设置生成的查询数量
func (m *MultiQueryRetriever) WithNumQueries(num int) *MultiQueryRetriever {
	m.NumQueries = num
	return m
}

// EnsembleRetriever 集成检索器
//
// 组合多个检索器，使用加权融合策略
type EnsembleRetriever struct {
	*BaseRetriever

	// Retrievers 检索器列表
	Retrievers []Retriever

	// Weights 每个检索器的权重
	Weights []float64
}

// NewEnsembleRetriever 创建集成检索器
func NewEnsembleRetriever(
	retrievers []Retriever,
	weights []float64,
	config RetrieverConfig,
) *EnsembleRetriever {
	if len(retrievers) != len(weights) {
		panic("number of retrievers must match number of weights")
	}

	retriever := &EnsembleRetriever{
		BaseRetriever: NewBaseRetriever(),
		Retrievers:    retrievers,
		Weights:       weights,
	}

	retriever.TopK = config.TopK
	retriever.MinScore = config.MinScore
	retriever.Name = config.Name

	return retriever
}

// GetRelevantDocuments 检索相关文档
func (e *EnsembleRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error) {
	if len(e.Retrievers) == 0 {
		return []*Document{}, nil
	}

	// 并发执行所有检索器
	type result struct {
		index int
		docs  []*Document
		err   error
	}

	resultsChan := make(chan result, len(e.Retrievers))

	for i, retriever := range e.Retrievers {
		go func(idx int, r Retriever) {
			docs, err := r.GetRelevantDocuments(ctx, query)
			resultsChan <- result{
				index: idx,
				docs:  docs,
				err:   err,
			}
		}(i, retriever)
	}

	// 收集结果
	results := make([]result, len(e.Retrievers))
	for i := 0; i < len(e.Retrievers); i++ {
		res := <-resultsChan
		if res.err != nil {
			// 记录错误但继续处理其他结果
			continue
		}
		results[res.index] = res
	}

	// 融合所有检索器的结果
	docMap := make(map[string]*Document)

	for i, res := range results {
		if res.docs == nil {
			continue
		}

		weight := e.Weights[i]
		for _, doc := range res.docs {
			if existing, ok := docMap[doc.ID]; ok {
				// 文档已存在，累加加权分数
				existing.Score += doc.Score * weight
			} else {
				// 新文档
				docCopy := doc.Clone()
				docCopy.Score = doc.Score * weight
				docMap[doc.ID] = docCopy
			}
		}
	}

	// 转换为列表
	merged := make([]*Document, 0, len(docMap))
	for _, doc := range docMap {
		merged = append(merged, doc)
	}

	// 排序和过滤
	collection := DocumentCollection(merged)
	collection.SortByScore()

	filtered := e.FilterByScore(collection)
	limited := e.LimitTopK(filtered)

	return limited, nil
}

// WithWeights 设置权重
func (e *EnsembleRetriever) WithWeights(weights []float64) *EnsembleRetriever {
	if len(weights) != len(e.Retrievers) {
		panic("number of weights must match number of retrievers")
	}
	e.Weights = weights
	return e
}

// AddRetriever 添加检索器
func (e *EnsembleRetriever) AddRetriever(retriever Retriever, weight float64) *EnsembleRetriever {
	e.Retrievers = append(e.Retrievers, retriever)
	e.Weights = append(e.Weights, weight)
	return e
}
