package document

import (
	"strings"
	"unicode/utf8"

	"github.com/kart-io/goagent/core"
)

// CharacterTextSplitter 字符分割器
//
// 按字符数分割文本,支持自定义分隔符
type CharacterTextSplitter struct {
	*BaseTextSplitter
	separator string
}

// CharacterTextSplitterConfig 字符分割器配置
type CharacterTextSplitterConfig struct {
	Separator       string
	ChunkSize       int
	ChunkOverlap    int
	KeepSeparator   bool
	CallbackManager *core.CallbackManager
}

// NewCharacterTextSplitter 创建字符分割器
func NewCharacterTextSplitter(config CharacterTextSplitterConfig) *CharacterTextSplitter {
	if config.Separator == "" {
		config.Separator = "\n\n"
	}

	baseConfig := BaseTextSplitterConfig{
		ChunkSize:       config.ChunkSize,
		ChunkOverlap:    config.ChunkOverlap,
		KeepSeparator:   config.KeepSeparator,
		CallbackManager: config.CallbackManager,
		LengthFunction:  utf8.RuneCountInString,
	}

	return &CharacterTextSplitter{
		BaseTextSplitter: NewBaseTextSplitter(baseConfig),
		separator:        config.Separator,
	}
}

// SplitText 分割文本
func (s *CharacterTextSplitter) SplitText(text string) ([]string, error) {
	// 按分隔符分割
	var splits []string

	if s.separator == "" {
		// 没有分隔符,按字符分割
		splits = []string{text}
	} else {
		splits = strings.Split(text, s.separator)
	}

	// 合并分割后的文本块
	return s.MergeSplits(splits, s.separator), nil
}

// RecursiveCharacterTextSplitter 递归字符分割器
//
// 使用多个分隔符递归分割,优先使用段落、句子等自然边界
type RecursiveCharacterTextSplitter struct {
	*BaseTextSplitter
	separators []string
}

// RecursiveCharacterTextSplitterConfig 递归分割器配置
type RecursiveCharacterTextSplitterConfig struct {
	Separators      []string
	ChunkSize       int
	ChunkOverlap    int
	KeepSeparator   bool
	CallbackManager *core.CallbackManager
}

// NewRecursiveCharacterTextSplitter 创建递归分割器
func NewRecursiveCharacterTextSplitter(config RecursiveCharacterTextSplitterConfig) *RecursiveCharacterTextSplitter {
	if len(config.Separators) == 0 {
		// 默认分隔符,从大到小
		config.Separators = []string{
			"\n\n", // 段落
			"\n",   // 行
			". ",   // 句子
			"! ",   // 句子
			"? ",   // 句子
			"; ",   // 子句
			", ",   // 短语
			" ",    // 单词
			"",     // 字符
		}
	}

	baseConfig := BaseTextSplitterConfig{
		ChunkSize:       config.ChunkSize,
		ChunkOverlap:    config.ChunkOverlap,
		KeepSeparator:   config.KeepSeparator,
		CallbackManager: config.CallbackManager,
		LengthFunction:  utf8.RuneCountInString,
	}

	return &RecursiveCharacterTextSplitter{
		BaseTextSplitter: NewBaseTextSplitter(baseConfig),
		separators:       config.Separators,
	}
}

// SplitText 递归分割文本
func (s *RecursiveCharacterTextSplitter) SplitText(text string) ([]string, error) {
	return s.splitTextRecursive(text, s.separators), nil
}

// splitTextRecursive 递归分割实现
func (s *RecursiveCharacterTextSplitter) splitTextRecursive(text string, separators []string) []string {
	finalChunks := make([]string, 0)

	// 当前分隔符
	separator := separators[len(separators)-1]
	newSeparators := []string{}

	// 选择合适的分隔符
	for i, sep := range separators {
		if sep == "" {
			separator = sep
			break
		}
		if strings.Contains(text, sep) {
			separator = sep
			newSeparators = separators[i+1:]
			break
		}
	}

	// 分割文本
	var splits []string
	if separator == "" {
		splits = []string{text}
	} else {
		splits = strings.Split(text, separator)
	}

	// 处理每个分割块
	goodSplits := make([]string, 0)
	for _, split := range splits {
		if s.lengthFunction(split) < s.chunkSize {
			goodSplits = append(goodSplits, split)
		} else {
			// 如果有累积的好块,先合并
			if len(goodSplits) > 0 {
				merged := s.MergeSplits(goodSplits, separator)
				finalChunks = append(finalChunks, merged...)
				goodSplits = make([]string, 0)
			}

			// 递归分割大块
			if len(newSeparators) == 0 {
				finalChunks = append(finalChunks, split)
			} else {
				otherInfo := s.splitTextRecursive(split, newSeparators)
				finalChunks = append(finalChunks, otherInfo...)
			}
		}
	}

	// 处理剩余的好块
	if len(goodSplits) > 0 {
		merged := s.MergeSplits(goodSplits, separator)
		finalChunks = append(finalChunks, merged...)
	}

	return finalChunks
}
