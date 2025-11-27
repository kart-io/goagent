package retrieval

import (
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/kart-io/goagent/interfaces"
)

// NewDocument 创建新文档
func NewDocument(content string, metadata map[string]interface{}) *interfaces.Document {
	return &interfaces.Document{
		PageContent: content,
		Metadata:    metadata,
		ID:          generateID(),
	}
}

// NewDocumentWithID 创建带 ID 的文档
func NewDocumentWithID(id, content string, metadata map[string]interface{}) *interfaces.Document {
	return &interfaces.Document{
		ID:          id,
		PageContent: content,
		Metadata:    metadata,
	}
}

// DocumentCollection 文档集合
type DocumentCollection []*interfaces.Document

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
func (dc DocumentCollection) Filter(predicate func(*interfaces.Document) bool) DocumentCollection {
	result := make(DocumentCollection, 0)
	for _, doc := range dc {
		if predicate(doc) {
			result = append(result, doc)
		}
	}
	return result
}

// Map 映射文档
func (dc DocumentCollection) Map(mapper func(*interfaces.Document) *interfaces.Document) DocumentCollection {
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
	id := atomic.AddInt64(&idCounter, 1)
	return fmt.Sprintf("doc_%d", id)
}
