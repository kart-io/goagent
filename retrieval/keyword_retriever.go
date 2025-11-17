package retrieval

import (
	"context"
	"math"
	"strings"
	"unicode"

	agentErrors "github.com/kart-io/goagent/errors"
)

// KeywordRetriever 关键词检索器
//
// 使用 BM25 或 TF-IDF 算法进行基于关键词的检索
type KeywordRetriever struct {
	*BaseRetriever

	// Documents 文档集合
	Documents []*Document

	// Algorithm 检索算法
	Algorithm KeywordAlgorithm

	// Index 倒排索引
	Index *InvertedIndex
}

// KeywordAlgorithm 关键词检索算法
type KeywordAlgorithm string

const (
	// AlgorithmBM25 BM25 算法
	AlgorithmBM25 KeywordAlgorithm = "bm25"

	// AlgorithmTFIDF TF-IDF 算法
	AlgorithmTFIDF KeywordAlgorithm = "tfidf"
)

// NewKeywordRetriever 创建关键词检索器
func NewKeywordRetriever(docs []*Document, config RetrieverConfig) *KeywordRetriever {
	retriever := &KeywordRetriever{
		BaseRetriever: NewBaseRetriever(),
		Documents:     docs,
		Algorithm:     AlgorithmBM25,
		Index:         NewInvertedIndex(),
	}

	retriever.TopK = config.TopK
	retriever.MinScore = config.MinScore
	retriever.Name = config.Name

	// 构建索引
	retriever.buildIndex()

	return retriever
}

// GetRelevantDocuments 检索相关文档
func (k *KeywordRetriever) GetRelevantDocuments(ctx context.Context, query string) ([]*Document, error) {
	if len(k.Documents) == 0 {
		return []*Document{}, nil
	}

	// 根据算法计算得分
	var scores []float64
	switch k.Algorithm {
	case AlgorithmBM25:
		scores = k.calculateBM25Scores(query)
	case AlgorithmTFIDF:
		scores = k.calculateTFIDFScores(query)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unknown algorithm").
			WithComponent("keyword_retriever").
			WithOperation("get_relevant_documents").
			WithContext("algorithm", string(k.Algorithm))
	}

	// 创建结果文档
	results := make([]*Document, 0)
	for i, doc := range k.Documents {
		if scores[i] > 0 {
			docCopy := doc.Clone()
			docCopy.Score = scores[i]
			results = append(results, docCopy)
		}
	}

	// 排序和过滤
	collection := DocumentCollection(results)
	collection.SortByScore()

	// 应用分数过滤
	filtered := k.FilterByScore(collection)

	// 应用 top-k 限制
	limited := k.LimitTopK(filtered)

	return limited, nil
}

// buildIndex 构建倒排索引
func (k *KeywordRetriever) buildIndex() {
	k.Index = NewInvertedIndex()

	for i, doc := range k.Documents {
		words := tokenize(doc.PageContent)
		k.Index.AddDocument(i, words)
	}
}

// calculateBM25Scores 计算 BM25 得分
//
// BM25 是一种改进的 TF-IDF 算法，考虑了文档长度和词频饱和度
func (k *KeywordRetriever) calculateBM25Scores(query string) []float64 {
	queryTerms := tokenize(query)
	scores := make([]float64, len(k.Documents))

	// BM25 参数
	k1 := 1.5 // 词频饱和参数
	b := 0.75 // 长度归一化参数

	numDocs := float64(len(k.Documents))
	avgDocLen := k.Index.AverageDocLength()

	for _, term := range queryTerms {
		// 计算 IDF
		df := float64(k.Index.DocumentFrequency(term))
		if df == 0 {
			continue
		}
		idf := math.Log((numDocs - df + 0.5) / (df + 0.5))

		// 对每个文档计算得分
		for i, doc := range k.Documents {
			tf := float64(k.Index.TermFrequency(i, term))
			docLen := float64(len(tokenize(doc.PageContent)))

			// BM25 公式
			numerator := tf * (k1 + 1)
			denominator := tf + k1*(1-b+b*docLen/avgDocLen)

			scores[i] += idf * (numerator / denominator)
		}
	}

	return scores
}

// calculateTFIDFScores 计算 TF-IDF 得分
func (k *KeywordRetriever) calculateTFIDFScores(query string) []float64 {
	queryTerms := tokenize(query)
	scores := make([]float64, len(k.Documents))

	numDocs := float64(len(k.Documents))

	for _, term := range queryTerms {
		// 计算 IDF
		df := float64(k.Index.DocumentFrequency(term))
		if df == 0 {
			continue
		}
		idf := math.Log(numDocs / df)

		// 对每个文档计算得分
		for i, doc := range k.Documents {
			tf := float64(k.Index.TermFrequency(i, term))
			totalTerms := float64(len(tokenize(doc.PageContent)))

			// TF-IDF 公式
			if totalTerms > 0 {
				tfNormalized := tf / totalTerms
				scores[i] += tfNormalized * idf
			}
		}
	}

	return scores
}

// WithAlgorithm 设置检索算法
func (k *KeywordRetriever) WithAlgorithm(algorithm KeywordAlgorithm) *KeywordRetriever {
	k.Algorithm = algorithm
	return k
}

// InvertedIndex 倒排索引
type InvertedIndex struct {
	// 词 -> 文档列表的映射
	index map[string][]int

	// 文档 -> 词频的映射
	docTermFreq map[int]map[string]int

	// 文档长度
	docLengths map[int]int
}

// NewInvertedIndex 创建倒排索引
func NewInvertedIndex() *InvertedIndex {
	return &InvertedIndex{
		index:       make(map[string][]int),
		docTermFreq: make(map[int]map[string]int),
		docLengths:  make(map[int]int),
	}
}

// AddDocument 添加文档到索引
func (idx *InvertedIndex) AddDocument(docID int, terms []string) {
	// 初始化文档的词频映射
	if idx.docTermFreq[docID] == nil {
		idx.docTermFreq[docID] = make(map[string]int)
	}

	// 记录文档长度
	idx.docLengths[docID] = len(terms)

	// 处理每个词
	seen := make(map[string]bool)
	for _, term := range terms {
		// 更新词频
		idx.docTermFreq[docID][term]++

		// 更新倒排表（每个词在每个文档中只记录一次）
		if !seen[term] {
			idx.index[term] = append(idx.index[term], docID)
			seen[term] = true
		}
	}
}

// DocumentFrequency 获取词的文档频率（包含该词的文档数）
func (idx *InvertedIndex) DocumentFrequency(term string) int {
	return len(idx.index[term])
}

// TermFrequency 获取词在文档中的频率
func (idx *InvertedIndex) TermFrequency(docID int, term string) int {
	if freq, ok := idx.docTermFreq[docID]; ok {
		return freq[term]
	}
	return 0
}

// AverageDocLength 获取平均文档长度
func (idx *InvertedIndex) AverageDocLength() float64 {
	if len(idx.docLengths) == 0 {
		return 0
	}

	sum := 0
	for _, length := range idx.docLengths {
		sum += length
	}

	return float64(sum) / float64(len(idx.docLengths))
}

// 文本处理辅助函数

// tokenize 分词
func tokenize(text string) []string {
	// 转换为小写
	text = strings.ToLower(text)

	// 分割单词
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})

	// 过滤停用词（简化版本）
	filtered := make([]string, 0)
	for _, word := range words {
		if len(word) > 2 && !isStopWord(word) {
			filtered = append(filtered, word)
		}
	}

	return filtered
}

// isStopWord 判断是否为停用词
func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "is": true, "at": true, "which": true, "on": true,
		"and": true, "a": true, "an": true, "as": true, "are": true,
		"was": true, "for": true, "with": true, "this": true, "that": true,
		"of": true, "to": true, "in": true, "it": true, "be": true,
	}
	return stopWords[word]
}

// splitWords 简单分词（用于辅助函数）
func splitWords(text string) []string {
	return strings.Fields(strings.ToLower(text))
}

// contains 检查文本是否包含子串（忽略大小写）
func contains(text, substr string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(substr))
}
