package document

import (
	"strings"
	"unicode/utf8"

	"github.com/kart-io/goagent/core"
)

// TokenTextSplitter Token 分割器
//
// 按 token 数分割文本(简化实现,使用空格分词)
type TokenTextSplitter struct {
	*BaseTextSplitter
	encoding string
}

// TokenTextSplitterConfig Token 分割器配置
type TokenTextSplitterConfig struct {
	Encoding        string // 编码方式(保留用于扩展)
	ChunkSize       int
	ChunkOverlap    int
	CallbackManager *core.CallbackManager
}

// NewTokenTextSplitter 创建 Token 分割器
func NewTokenTextSplitter(config TokenTextSplitterConfig) *TokenTextSplitter {
	if config.Encoding == "" {
		config.Encoding = "cl100k_base" // OpenAI 默认编码
	}

	baseConfig := BaseTextSplitterConfig{
		ChunkSize:       config.ChunkSize,
		ChunkOverlap:    config.ChunkOverlap,
		CallbackManager: config.CallbackManager,
		// 使用简单的 token 计数(按空格分词)
		LengthFunction: func(s string) int {
			return len(strings.Fields(s))
		},
	}

	return &TokenTextSplitter{
		BaseTextSplitter: NewBaseTextSplitter(baseConfig),
		encoding:         config.Encoding,
	}
}

// SplitText 按 token 分割文本
func (s *TokenTextSplitter) SplitText(text string) ([]string, error) {
	// 简化实现:按单词分割
	words := strings.Fields(text)

	if len(words) == 0 {
		return []string{}, nil
	}

	chunks := make([]string, 0)
	currentChunk := make([]string, 0)
	currentSize := 0

	for _, word := range words {
		wordSize := 1 // 每个单词算作 1 个 token

		if currentSize+wordSize > s.chunkSize && len(currentChunk) > 0 {
			// 达到块大小,保存当前块
			chunks = append(chunks, strings.Join(currentChunk, " "))

			// 处理重叠
			overlapSize := 0
			overlapChunk := make([]string, 0)

			for i := len(currentChunk) - 1; i >= 0; i-- {
				if overlapSize >= s.chunkOverlap {
					break
				}
				overlapChunk = append([]string{currentChunk[i]}, overlapChunk...)
				overlapSize++
			}

			currentChunk = overlapChunk
			currentSize = overlapSize
		}

		currentChunk = append(currentChunk, word)
		currentSize += wordSize
	}

	// 添加最后一个块
	if len(currentChunk) > 0 {
		chunks = append(chunks, strings.Join(currentChunk, " "))
	}

	return chunks, nil
}

// MarkdownTextSplitter Markdown 智能分割器
//
// 按 Markdown 结构分割(标题、段落等)
type MarkdownTextSplitter struct {
	*BaseTextSplitter
	headersToSplitOn []string
}

// MarkdownTextSplitterConfig Markdown 分割器配置
type MarkdownTextSplitterConfig struct {
	HeadersToSplitOn []string // 要分割的标题级别,如 []string{"#", "##", "###"}
	ChunkSize        int
	ChunkOverlap     int
	CallbackManager  *core.CallbackManager
}

// NewMarkdownTextSplitter 创建 Markdown 分割器
func NewMarkdownTextSplitter(config MarkdownTextSplitterConfig) *MarkdownTextSplitter {
	if len(config.HeadersToSplitOn) == 0 {
		config.HeadersToSplitOn = []string{"#", "##", "###", "####"}
	}

	baseConfig := BaseTextSplitterConfig{
		ChunkSize:       config.ChunkSize,
		ChunkOverlap:    config.ChunkOverlap,
		CallbackManager: config.CallbackManager,
		LengthFunction:  utf8.RuneCountInString,
		KeepSeparator:   true,
	}

	return &MarkdownTextSplitter{
		BaseTextSplitter: NewBaseTextSplitter(baseConfig),
		headersToSplitOn: config.HeadersToSplitOn,
	}
}

// SplitText 按 Markdown 结构分割
func (s *MarkdownTextSplitter) SplitText(text string) ([]string, error) {
	lines := strings.Split(text, "\n")
	chunks := make([]string, 0)
	currentChunk := make([]string, 0)

	for _, line := range lines {
		// 检查是否是标题
		isHeader := false
		for _, header := range s.headersToSplitOn {
			if strings.HasPrefix(strings.TrimSpace(line), header+" ") {
				isHeader = true
				break
			}
		}

		if isHeader && len(currentChunk) > 0 {
			// 遇到标题,保存当前块
			chunk := strings.Join(currentChunk, "\n")
			if s.lengthFunction(chunk) > 0 {
				chunks = append(chunks, chunk)
			}
			currentChunk = make([]string, 0)
		}

		currentChunk = append(currentChunk, line)
	}

	// 添加最后一个块
	if len(currentChunk) > 0 {
		chunk := strings.Join(currentChunk, "\n")
		if s.lengthFunction(chunk) > 0 {
			chunks = append(chunks, chunk)
		}
	}

	// 如果块太大,进一步分割
	finalChunks := make([]string, 0)
	for _, chunk := range chunks {
		if s.lengthFunction(chunk) > s.chunkSize {
			// 使用递归分割器进一步分割
			subSplitter := NewRecursiveCharacterTextSplitter(RecursiveCharacterTextSplitterConfig{
				ChunkSize:       s.chunkSize,
				ChunkOverlap:    s.chunkOverlap,
				CallbackManager: s.callbackManager,
			})
			subChunks, _ := subSplitter.SplitText(chunk)
			finalChunks = append(finalChunks, subChunks...)
		} else {
			finalChunks = append(finalChunks, chunk)
		}
	}

	return finalChunks, nil
}
