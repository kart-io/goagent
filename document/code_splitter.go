package document

import (
	"strings"
	"unicode/utf8"

	"github.com/kart-io/goagent/core"
)

// CodeTextSplitter 代码分割器
//
// 针对代码进行智能分割,保持代码结构完整性
type CodeTextSplitter struct {
	*BaseTextSplitter
	language   string
	separators []string
}

// Language 支持的编程语言
const (
	LanguageGo         = "go"
	LanguagePython     = "python"
	LanguageJavaScript = "javascript"
	LanguageTypeScript = "typescript"
	LanguageJava       = "java"
	LanguageRust       = "rust"
	LanguageCpp        = "cpp"
	LanguageC          = "c"
)

// CodeTextSplitterConfig 代码分割器配置
type CodeTextSplitterConfig struct {
	Language        string
	ChunkSize       int
	ChunkOverlap    int
	CallbackManager *core.CallbackManager
}

// NewCodeTextSplitter 创建代码分割器
func NewCodeTextSplitter(config CodeTextSplitterConfig) *CodeTextSplitter {
	if config.Language == "" {
		config.Language = LanguageGo
	}

	baseConfig := BaseTextSplitterConfig{
		ChunkSize:       config.ChunkSize,
		ChunkOverlap:    config.ChunkOverlap,
		CallbackManager: config.CallbackManager,
		LengthFunction:  utf8.RuneCountInString,
		KeepSeparator:   true,
	}

	splitter := &CodeTextSplitter{
		BaseTextSplitter: NewBaseTextSplitter(baseConfig),
		language:         config.Language,
	}

	splitter.separators = splitter.getSeparators()

	return splitter
}

// SplitText 分割代码
func (s *CodeTextSplitter) SplitText(text string) ([]string, error) {
	// 使用递归分割策略
	return s.splitCodeRecursive(text, s.separators), nil
}

// getSeparators 获取语言特定的分隔符
func (s *CodeTextSplitter) getSeparators() []string {
	switch s.language {
	case LanguageGo:
		return []string{
			"\nfunc ",  // 函数定义
			"\ntype ",  // 类型定义
			"\nconst ", // 常量定义
			"\nvar ",   // 变量定义
			"\n\n",     // 段落
			"\n",       // 行
			" ",        // 空格
			"",         // 字符
		}

	case LanguagePython:
		return []string{
			"\nclass ", // 类定义
			"\ndef ",   // 函数定义
			"\n\tdef ", // 类方法定义
			"\n\n",     // 段落
			"\n",       // 行
			" ",        // 空格
			"",         // 字符
		}

	case LanguageJavaScript, LanguageTypeScript:
		return []string{
			"\nfunction ", // 函数定义
			"\nconst ",    // 常量定义
			"\nlet ",      // 变量定义
			"\nvar ",      // 变量定义
			"\nclass ",    // 类定义
			"\nif ",       // 条件语句
			"\n\n",        // 段落
			"\n",          // 行
			" ",           // 空格
			"",            // 字符
		}

	case LanguageJava:
		return []string{
			"\npublic ",    // 公共成员
			"\nprotected ", // 保护成员
			"\nprivate ",   // 私有成员
			"\nclass ",     // 类定义
			"\ninterface ", // 接口定义
			"\n\n",         // 段落
			"\n",           // 行
			" ",            // 空格
			"",             // 字符
		}

	case LanguageRust:
		return []string{
			"\nfn ",     // 函数定义
			"\nstruct ", // 结构体定义
			"\nenum ",   // 枚举定义
			"\nimpl ",   // 实现块
			"\ntrait ",  // trait 定义
			"\n\n",      // 段落
			"\n",        // 行
			" ",         // 空格
			"",          // 字符
		}

	case LanguageCpp, LanguageC:
		return []string{
			"\nclass ",  // 类定义
			"\nvoid ",   // 函数定义
			"\nint ",    // 函数定义
			"\nstruct ", // 结构体定义
			"\n\n",      // 段落
			"\n",        // 行
			" ",         // 空格
			"",          // 字符
		}

	default:
		// 默认分隔符
		return []string{
			"\n\n",
			"\n",
			" ",
			"",
		}
	}
}

// splitCodeRecursive 递归分割代码
func (s *CodeTextSplitter) splitCodeRecursive(text string, separators []string) []string {
	if len(separators) == 0 {
		return []string{text}
	}

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
		splits = s.splitKeepingSeparator(text, separator)
	}

	// 处理每个分割块
	goodSplits := make([]string, 0)
	for _, split := range splits {
		if s.lengthFunction(split) < s.chunkSize {
			goodSplits = append(goodSplits, split)
		} else {
			// 如果有累积的好块,先合并
			if len(goodSplits) > 0 {
				merged := s.MergeSplits(goodSplits, "")
				finalChunks = append(finalChunks, merged...)
				goodSplits = make([]string, 0)
			}

			// 递归分割大块
			if len(newSeparators) == 0 {
				finalChunks = append(finalChunks, split)
			} else {
				otherInfo := s.splitCodeRecursive(split, newSeparators)
				finalChunks = append(finalChunks, otherInfo...)
			}
		}
	}

	// 处理剩余的好块
	if len(goodSplits) > 0 {
		merged := s.MergeSplits(goodSplits, "")
		finalChunks = append(finalChunks, merged...)
	}

	return finalChunks
}

// splitKeepingSeparator 分割时保留分隔符
func (s *CodeTextSplitter) splitKeepingSeparator(text, separator string) []string {
	if separator == "" {
		return []string{text}
	}

	parts := strings.Split(text, separator)
	if len(parts) == 0 {
		return []string{text}
	}

	result := make([]string, 0)

	// 第一部分
	if parts[0] != "" {
		result = append(result, parts[0])
	}

	// 其他部分加上分隔符
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			result = append(result, separator+parts[i])
		}
	}

	return result
}
