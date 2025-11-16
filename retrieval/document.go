package retrieval

import (
	"fmt"
	"sort"

	"github.com/kart-io/goagent/interfaces"
)

// Document type alias for backward compatibility
//
// Deprecated: Use interfaces.Document directly.
// This alias will be removed in v1.0.0.
type Document = interfaces.Document

// NewDocument 创建新文档
func NewDocument(content string, metadata map[string]interface{}) *Document {
	return &Document{
		PageContent: content,
		Metadata:    metadata,
		ID:          generateID(),
	}
}

// NewDocumentWithID 创建带 ID 的文档
func NewDocumentWithID(id, content string, metadata map[string]interface{}) *Document {
	return &Document{
		ID:          id,
		PageContent: content,
		Metadata:    metadata,
	}
}

// DocumentCollection 文档集合
type DocumentCollection []*Document

// Len 实现 sort.Interface
func (dc DocumentCollection) Len() int {
	return len(dc)
}

// Less 按分数降序排序
func (dc DocumentCollection) Less(i, j int) bool {
	return dc[i].Score > dc[j].Score
}

// Swap 交换元素
func (dc DocumentCollection) Swap(i, j int) {
	dc[i], dc[j] = dc[j], dc[i]
}

// SortByScore 按分数排序
func (dc DocumentCollection) SortByScore() {
	sort.Sort(dc)
}

// Top 获取前 N 个文档
func (dc DocumentCollection) Top(n int) DocumentCollection {
	if n >= len(dc) {
		return dc
	}
	return dc[:n]
}

// Filter 过滤文档
func (dc DocumentCollection) Filter(predicate func(*Document) bool) DocumentCollection {
	result := make(DocumentCollection, 0)
	for _, doc := range dc {
		if predicate(doc) {
			result = append(result, doc)
		}
	}
	return result
}

// Map 映射文档
func (dc DocumentCollection) Map(mapper func(*Document) *Document) DocumentCollection {
	result := make(DocumentCollection, len(dc))
	for i, doc := range dc {
		result[i] = mapper(doc)
	}
	return result
}

// Deduplicate 去重（基于 ID）
func (dc DocumentCollection) Deduplicate() DocumentCollection {
	seen := make(map[string]bool)
	result := make(DocumentCollection, 0)

	for _, doc := range dc {
		if !seen[doc.ID] {
			seen[doc.ID] = true
			result = append(result, doc)
		}
	}

	return result
}

// 辅助函数

var idCounter int64

// generateID 生成唯一 ID
func generateID() string {
	idCounter++
	return fmt.Sprintf("doc_%d", idCounter)
}
