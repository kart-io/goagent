package document

import (
	"context"
	"os"
	"strings"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/retrieval"
	"github.com/kart-io/k8s-agent/common/errors"
)

// MarkdownLoader Markdown 文件加载器
//
// 加载 Markdown 文件并提取结构信息
type MarkdownLoader struct {
	*BaseDocumentLoader
	filePath      string
	removeImages  bool
	removeLinks   bool
	removeCodeFmt bool
}

// MarkdownLoaderConfig Markdown 加载器配置
type MarkdownLoaderConfig struct {
	FilePath        string
	RemoveImages    bool
	RemoveLinks     bool
	RemoveCodeFmt   bool
	Metadata        map[string]interface{}
	CallbackManager *core.CallbackManager
}

// NewMarkdownLoader 创建 Markdown 加载器
func NewMarkdownLoader(config MarkdownLoaderConfig) *MarkdownLoader {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	config.Metadata["source"] = config.FilePath
	config.Metadata["source_type"] = "markdown"

	return &MarkdownLoader{
		BaseDocumentLoader: NewBaseDocumentLoader(config.Metadata, config.CallbackManager),
		filePath:           config.FilePath,
		removeImages:       config.RemoveImages,
		removeLinks:        config.RemoveLinks,
		removeCodeFmt:      config.RemoveCodeFmt,
	}
}

// Load 加载 Markdown 文件
func (l *MarkdownLoader) Load(ctx context.Context) ([]*interfaces.Document, error) {
	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnStart(ctx, map[string]interface{}{
			"loader":    "markdown",
			"file_path": l.filePath,
		}); err != nil {
			return nil, err
		}
	}

	// 读取文件
	content, err := os.ReadFile(l.filePath)
	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, errors.Wrap(errors.CodeInternalError, "failed to read markdown file", err)
	}

	text := string(content)

	// 处理 Markdown 内容
	text = l.processMarkdown(text)

	// 提取元数据
	metadata := l.GetMetadata()
	metadata["file_size"] = len(content)

	// 提取标题作为元数据
	title := l.extractTitle(text)
	if title != "" {
		metadata["title"] = title
	}

	doc := retrieval.NewDocument(text, metadata)

	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnEnd(ctx, map[string]interface{}{
			"num_docs": 1,
		}); err != nil {
			return nil, err
		}
	}

	return []*interfaces.Document{doc}, nil
}

// LoadAndSplit 加载并分割
func (l *MarkdownLoader) LoadAndSplit(ctx context.Context, splitter TextSplitter) ([]*interfaces.Document, error) {
	return l.BaseDocumentLoader.LoadAndSplit(ctx, l, splitter)
}

// processMarkdown 处理 Markdown 内容
func (l *MarkdownLoader) processMarkdown(text string) string {
	if l.removeImages {
		text = removeMarkdownImages(text)
	}

	if l.removeLinks {
		text = removeMarkdownLinks(text)
	}

	if l.removeCodeFmt {
		text = removeMarkdownCodeFormatting(text)
	}

	return text
}

// extractTitle 提取标题
func (l *MarkdownLoader) extractTitle(text string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

// removeMarkdownImages 移除图片
func removeMarkdownImages(text string) string {
	// 简单实现:移除 ![alt](url) 格式
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))

	for _, line := range lines {
		// 简单的正则替换
		if !strings.Contains(line, "![") {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// removeMarkdownLinks 移除链接但保留文本
func removeMarkdownLinks(text string) string {
	// 将 [text](url) 替换为 text
	result := text
	// 简单实现
	for {
		start := strings.Index(result, "[")
		if start == -1 {
			break
		}

		mid := strings.Index(result[start:], "](")
		if mid == -1 {
			break
		}
		mid += start

		end := strings.Index(result[mid:], ")")
		if end == -1 {
			break
		}
		end += mid

		linkText := result[start+1 : mid]
		result = result[:start] + linkText + result[end+1:]
	}

	return result
}

// removeMarkdownCodeFormatting 移除代码格式但保留内容
func removeMarkdownCodeFormatting(text string) string {
	// 移除代码块标记 ``` 但保留内容
	text = strings.ReplaceAll(text, "```", "")
	// 移除行内代码标记 ` 但保留内容
	text = strings.ReplaceAll(text, "`", "")
	return text
}
