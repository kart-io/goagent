package document

import (
	"context"
	"strings"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/retrieval"
)

// TextSplitter 文本分割器接口
//
// 负责将长文本分割成更小的块,用于嵌入和检索
type TextSplitter interface {
	// SplitText 分割文本
	SplitText(text string) ([]string, error)

	// SplitDocuments 分割文档
	SplitDocuments(docs []*interfaces.Document) ([]*interfaces.Document, error)

	// GetChunkSize 获取块大小
	GetChunkSize() int

	// GetChunkOverlap 获取块重叠大小
	GetChunkOverlap() int
}

// BaseTextSplitter 基础文本分割器
//
// 提供通用的分割逻辑
type BaseTextSplitter struct {
	chunkSize       int
	chunkOverlap    int
	lengthFunction  func(string) int
	keepSeparator   bool
	callbackManager *core.CallbackManager
}

// BaseTextSplitterConfig 基础分割器配置
type BaseTextSplitterConfig struct {
	ChunkSize       int
	ChunkOverlap    int
	LengthFunction  func(string) int
	KeepSeparator   bool
	CallbackManager *core.CallbackManager
}

// NewBaseTextSplitter 创建基础分割器
func NewBaseTextSplitter(config BaseTextSplitterConfig) *BaseTextSplitter {
	if config.ChunkSize <= 0 {
		config.ChunkSize = 1000
	}
	if config.ChunkOverlap < 0 {
		config.ChunkOverlap = 200
	}
	if config.LengthFunction == nil {
		config.LengthFunction = func(s string) int {
			return len(s)
		}
	}

	return &BaseTextSplitter{
		chunkSize:       config.ChunkSize,
		chunkOverlap:    config.ChunkOverlap,
		lengthFunction:  config.LengthFunction,
		keepSeparator:   config.KeepSeparator,
		callbackManager: config.CallbackManager,
	}
}

// GetChunkSize 获取块大小
func (s *BaseTextSplitter) GetChunkSize() int {
	return s.chunkSize
}

// GetChunkOverlap 获取块重叠
func (s *BaseTextSplitter) GetChunkOverlap() int {
	return s.chunkOverlap
}

// SplitDocuments 分割文档
func (s *BaseTextSplitter) SplitDocuments(docs []*interfaces.Document) ([]*interfaces.Document, error) {
	result := make([]*interfaces.Document, 0)

	for _, doc := range docs {
		// 分割文本
		chunks, err := s.SplitText(doc.PageContent)
		if err != nil {
			return nil, err
		}

		// 创建新文档
		for i, chunk := range chunks {
			// 复制元数据
			metadata := make(map[string]interface{})
			for k, v := range doc.Metadata {
				metadata[k] = v
			}

			// 添加分割信息
			metadata["chunk_index"] = i
			metadata["chunk_total"] = len(chunks)
			metadata["source_id"] = doc.ID

			newDoc := retrieval.NewDocument(chunk, metadata)
			result = append(result, newDoc)
		}
	}

	return result, nil
}

// MergeSplits 合并文本块
//
// 将多个文本块合并,保持在 chunk size 限制内
func (s *BaseTextSplitter) MergeSplits(splits []string, separator string) []string {
	separatorLen := s.lengthFunction(separator)

	docs := make([]string, 0)
	currentDoc := make([]string, 0)
	total := 0

	for _, split := range splits {
		length := s.lengthFunction(split)

		// 检查是否需要开始新块
		if total+length+(len(currentDoc)*separatorLen) > s.chunkSize {
			if len(currentDoc) > 0 {
				doc := s.joinDocs(currentDoc, separator)
				if doc != "" {
					docs = append(docs, doc)
				}

				// 处理重叠
				for total > s.chunkOverlap || (total+length+(len(currentDoc)*separatorLen) > s.chunkSize && total > 0) {
					total -= s.lengthFunction(currentDoc[0]) + separatorLen
					currentDoc = currentDoc[1:]
				}
			}
		}

		currentDoc = append(currentDoc, split)
		total += length + separatorLen
	}

	// 处理剩余的块
	if len(currentDoc) > 0 {
		doc := s.joinDocs(currentDoc, separator)
		if doc != "" {
			docs = append(docs, doc)
		}
	}

	return docs
}

// joinDocs 连接文档
func (s *BaseTextSplitter) joinDocs(docs []string, separator string) string {
	text := strings.Join(docs, separator)
	text = strings.TrimSpace(text)
	return text
}

// SplitText 需要子类实现
func (s *BaseTextSplitter) SplitText(text string) ([]string, error) {
	// 默认实现:不分割
	return []string{text}, nil
}

// TriggerCallbacks 触发回调
func (s *BaseTextSplitter) TriggerCallbacks(ctx context.Context, event string, data interface{}) error {
	if s.callbackManager == nil {
		return nil
	}

	switch event {
	case "start":
		return s.callbackManager.OnStart(ctx, data)
	case "end":
		return s.callbackManager.OnEnd(ctx, data)
	case "error":
		if err, ok := data.(error); ok {
			return s.callbackManager.OnError(ctx, err)
		}
	}

	return nil
}
