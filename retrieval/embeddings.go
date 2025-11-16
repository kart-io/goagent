package retrieval

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// Embedder 嵌入模型接口
//
// 将文本转换为向量表示，用于语义搜索和相似度计算
type Embedder interface {
	// Embed 批量嵌入文本
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// EmbedQuery 嵌入单个查询文本
	EmbedQuery(ctx context.Context, query string) ([]float32, error)

	// Dimensions 返回向量维度
	Dimensions() int
}

// BaseEmbedder 基础嵌入器实现
type BaseEmbedder struct {
	dimensions int
}

// NewBaseEmbedder 创建基础嵌入器
func NewBaseEmbedder(dimensions int) *BaseEmbedder {
	return &BaseEmbedder{
		dimensions: dimensions,
	}
}

// Dimensions 返回向量维度
func (e *BaseEmbedder) Dimensions() int {
	return e.dimensions
}

// SimpleEmbedder 简单的 TF-IDF 风格嵌入器（用于测试和开发）
//
// 使用简单的词频向量表示，不依赖外部模型
type SimpleEmbedder struct {
	*BaseEmbedder
	vocabulary map[string]int
	maxWords   int
}

// NewSimpleEmbedder 创建简单嵌入器
func NewSimpleEmbedder(dimensions int) *SimpleEmbedder {
	if dimensions <= 0 {
		dimensions = 100
	}

	return &SimpleEmbedder{
		BaseEmbedder: NewBaseEmbedder(dimensions),
		vocabulary:   make(map[string]int),
		maxWords:     dimensions,
	}
}

// Embed 批量嵌入文本
func (e *SimpleEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// 首先构建词汇表
	e.buildVocabulary(texts)

	// 为每个文本生成向量
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vectors[i] = e.textToVector(text)
	}

	return vectors, nil
}

// EmbedQuery 嵌入单个查询文本
func (e *SimpleEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return e.textToVector(query), nil
}

// buildVocabulary 构建词汇表
func (e *SimpleEmbedder) buildVocabulary(texts []string) {
	// 统计词频
	wordFreq := make(map[string]int)
	for _, text := range texts {
		words := simpleTokenize(text)
		for _, word := range words {
			wordFreq[word]++
		}
	}

	// 选择最常见的词
	type wordCount struct {
		word  string
		count int
	}

	wordCounts := make([]wordCount, 0, len(wordFreq))
	for word, count := range wordFreq {
		wordCounts = append(wordCounts, wordCount{word: word, count: count})
	}

	// 简单排序（冒泡排序）
	for i := 0; i < len(wordCounts); i++ {
		for j := i + 1; j < len(wordCounts); j++ {
			if wordCounts[i].count < wordCounts[j].count {
				wordCounts[i], wordCounts[j] = wordCounts[j], wordCounts[i]
			}
		}
	}

	// 构建词汇表（映射到向量维度）
	e.vocabulary = make(map[string]int)
	maxWords := e.maxWords
	if len(wordCounts) < maxWords {
		maxWords = len(wordCounts)
	}

	for i := 0; i < maxWords; i++ {
		e.vocabulary[wordCounts[i].word] = i
	}
}

// textToVector 将文本转换为向量
func (e *SimpleEmbedder) textToVector(text string) []float32 {
	vector := make([]float32, e.dimensions)

	// 如果词汇表为空，使用哈希方法
	if len(e.vocabulary) == 0 {
		return e.hashToVector(text)
	}

	// 统计文本中的词频
	words := simpleTokenize(text)
	wordCount := make(map[string]int)
	for _, word := range words {
		wordCount[word]++
	}

	// 计算 TF（词频）
	totalWords := len(words)
	for word, count := range wordCount {
		if idx, ok := e.vocabulary[word]; ok {
			tf := float32(count) / float32(totalWords)
			vector[idx] = tf
		}
	}

	// 归一化向量
	return normalizeVector(vector)
}

// hashToVector 使用哈希方法生成向量（当词汇表为空时）
func (e *SimpleEmbedder) hashToVector(text string) []float32 {
	vector := make([]float32, e.dimensions)
	words := simpleTokenize(text)

	for _, word := range words {
		// 简单哈希：累加字符 ASCII 值
		hash := 0
		for _, ch := range word {
			hash += int(ch)
		}
		idx := hash % e.dimensions
		vector[idx]++
	}

	// 归一化
	return normalizeVector(vector)
}

// simpleTokenize 简单的分词函数
func simpleTokenize(text string) []string {
	// 转换为小写
	text = strings.ToLower(text)

	// 移除标点符号
	text = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			return r
		}
		return ' '
	}, text)

	// 按空格分割
	words := strings.Fields(text)

	// 过滤停用词（简单版本）
	stopWords := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true,
		"at": true, "be": true, "by": true, "for": true, "from": true,
		"has": true, "he": true, "in": true, "is": true, "it": true,
		"its": true, "of": true, "on": true, "that": true, "the": true,
		"to": true, "was": true, "will": true, "with": true,
	}

	filtered := make([]string, 0, len(words))
	for _, word := range words {
		if !stopWords[word] && len(word) > 1 {
			filtered = append(filtered, word)
		}
	}

	return filtered
}

// normalizeVector 归一化向量（L2 norm）
func normalizeVector(vec []float32) []float32 {
	var sumSquares float32
	for _, v := range vec {
		sumSquares += v * v
	}

	if sumSquares == 0 {
		return vec
	}

	norm := float32(math.Sqrt(float64(sumSquares)))
	normalized := make([]float32, len(vec))
	for i, v := range vec {
		normalized[i] = v / norm
	}

	return normalized
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(vec1, vec2 []float32) (float32, error) {
	if len(vec1) != len(vec2) {
		return 0, fmt.Errorf("vectors must have same length: %d != %d", len(vec1), len(vec2))
	}

	var dotProduct, norm1, norm2 float32
	for i := 0; i < len(vec1); i++ {
		dotProduct += vec1[i] * vec2[i]
		norm1 += vec1[i] * vec1[i]
		norm2 += vec2[i] * vec2[i]
	}

	if norm1 == 0 || norm2 == 0 {
		return 0, nil
	}

	return dotProduct / (float32(math.Sqrt(float64(norm1))) * float32(math.Sqrt(float64(norm2)))), nil
}

// euclideanDistance 计算欧氏距离
func euclideanDistance(vec1, vec2 []float32) (float32, error) {
	if len(vec1) != len(vec2) {
		return 0, fmt.Errorf("vectors must have same length: %d != %d", len(vec1), len(vec2))
	}

	var sumSquares float32
	for i := 0; i < len(vec1); i++ {
		diff := vec1[i] - vec2[i]
		sumSquares += diff * diff
	}

	return float32(math.Sqrt(float64(sumSquares))), nil
}

// dotProduct 计算点积
func dotProduct(vec1, vec2 []float32) (float32, error) {
	if len(vec1) != len(vec2) {
		return 0, fmt.Errorf("vectors must have same length: %d != %d", len(vec1), len(vec2))
	}

	var result float32
	for i := 0; i < len(vec1); i++ {
		result += vec1[i] * vec2[i]
	}

	return result, nil
}
