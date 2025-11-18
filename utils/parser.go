package utils

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
)

// 预编译的正则表达式，避免重复编译带来的性能开销
// 这些正则表达式在包加载时编译一次，后续调用直接使用
var (
	// JSON 提取相关正则表达式
	// reJSONCodeBlock 匹配 ```json ... ``` 格式的 JSON 代码块
	reJSONCodeBlock = regexp.MustCompile("```json\\s*([\\s\\S]*?)```")
	// reJSONBraces 匹配花括号包裹的 JSON 对象
	reJSONBraces = regexp.MustCompile(`\{[\s\S]*\}`)

	// 代码块提取相关正则表达式
	// reCodeBlockGeneric 匹配 ```language ... ``` 格式的通用代码块
	reCodeBlockGeneric = regexp.MustCompile("```(\\w+)\\s*([\\s\\S]*?)```")

	// 列表提取相关正则表达式
	// reListNumbered 匹配 "1. item" 格式的数字列表
	reListNumbered = regexp.MustCompile(`(?m)^\d+\.\s+(.+)$`)
	// reListBullet 匹配 "- item" 或 "* item" 格式的符号列表
	reListBullet = regexp.MustCompile(`(?m)^[\-\*]\s+(.+)$`)

	// Markdown 格式清理相关正则表达式（RemoveMarkdown 方法使用）
	// reMarkdownCodeBlock 移除代码块 ```...```
	reMarkdownCodeBlock = regexp.MustCompile("```[\\s\\S]*?```")
	// reMarkdownInlineCode 移除内联代码 `...`
	reMarkdownInlineCode = regexp.MustCompile("`[^`]+`")
	// reMarkdownHeading 移除标题标记 # ## ###
	reMarkdownHeading = regexp.MustCompile(`(?m)^#+\s+`)
	// reMarkdownBoldDouble 移除双星号粗体 **text**
	reMarkdownBoldDouble = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	// reMarkdownItalicSingle 移除单星号斜体 *text*
	reMarkdownItalicSingle = regexp.MustCompile(`\*([^*]+)\*`)
	// reMarkdownBoldUnderscore 移除双下划线粗体 __text__
	reMarkdownBoldUnderscore = regexp.MustCompile("__([^_]+)__")
	// reMarkdownItalicUnderscore 移除单下划线斜体 _text_
	reMarkdownItalicUnderscore = regexp.MustCompile("_([^_]+)_")
	// reMarkdownLink 移除链接 [text](url)
	reMarkdownLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)

	// 动态正则表达式缓存，避免重复编译相同参数的正则
	// 使用 sync.Map 提供线程安全的缓存
	regexCache sync.Map // key: string, value: *regexp.Regexp
)

// getCachedRegex 获取或创建缓存的正则表达式
// 对于需要动态构建的正则表达式（如带参数的），使用缓存避免重复编译
func getCachedRegex(pattern string) *regexp.Regexp {
	// 尝试从缓存获取
	if cached, ok := regexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp)
	}

	// 编译新的正则表达式
	re := regexp.MustCompile(pattern)

	// 存入缓存
	regexCache.Store(pattern, re)

	return re
}

// ResponseParser 提供响应解析工具
type ResponseParser struct {
	content string
}

// NewResponseParser 创建响应解析器
func NewResponseParser(content string) *ResponseParser {
	return &ResponseParser{
		content: content,
	}
}

// ExtractJSON 提取 JSON 内容
func (p *ResponseParser) ExtractJSON() (string, error) {
	// 尝试直接解析
	if json.Valid([]byte(p.content)) {
		return p.content, nil
	}

	// 尝试提取 JSON 代码块（使用预编译正则）
	matches := reJSONCodeBlock.FindStringSubmatch(p.content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}

	// 尝试提取 {} 包裹的内容（使用预编译正则）
	match := reJSONBraces.FindString(p.content)
	if match != "" && json.Valid([]byte(match)) {
		return match, nil
	}

	return "", errors.New("no valid JSON found in response")
}

// ParseToMap 解析为 map
func (p *ResponseParser) ParseToMap() (map[string]interface{}, error) {
	jsonStr, err := p.ExtractJSON()
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, err
	}

	return result, nil
}

// ParseToStruct 解析为结构体
func (p *ResponseParser) ParseToStruct(v interface{}) error {
	jsonStr, err := p.ExtractJSON()
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(jsonStr), v)
}

// ExtractCodeBlock 提取指定语言的代码块
func (p *ResponseParser) ExtractCodeBlock(language string) (string, error) {
	// 使用缓存的正则表达式
	pattern := "```" + language + "\\s*([\\s\\S]*?)```"
	re := getCachedRegex(pattern)
	matches := re.FindStringSubmatch(p.content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}
	return "", errors.New("code block not found")
}

// ExtractAllCodeBlocks 提取所有代码块
func (p *ResponseParser) ExtractAllCodeBlocks() map[string]string {
	result := make(map[string]string)
	// 使用预编译正则表达式
	matches := reCodeBlockGeneric.FindAllStringSubmatch(p.content, -1)

	for i, match := range matches {
		if len(match) > 2 {
			lang := match[1]
			code := strings.TrimSpace(match[2])
			key := lang
			if _, exists := result[key]; exists {
				key = lang + "_" + string(rune(i))
			}
			result[key] = code
		}
	}

	return result
}

// ExtractList 提取列表项
func (p *ResponseParser) ExtractList() []string {
	items := make([]string, 0)

	// 匹配数字列表: 1. item（使用预编译正则）
	numberMatches := reListNumbered.FindAllStringSubmatch(p.content, -1)
	for _, match := range numberMatches {
		if len(match) > 1 {
			items = append(items, strings.TrimSpace(match[1]))
		}
	}

	// 如果没有数字列表，尝试匹配符号列表: - item 或 * item（使用预编译正则）
	if len(items) == 0 {
		bulletMatches := reListBullet.FindAllStringSubmatch(p.content, -1)
		for _, match := range bulletMatches {
			if len(match) > 1 {
				items = append(items, strings.TrimSpace(match[1]))
			}
		}
	}

	return items
}

// ExtractKeyValue 提取键值对
func (p *ResponseParser) ExtractKeyValue(key string) (string, error) {
	// 尝试 JSON 格式
	data, err := p.ParseToMap()
	if err == nil {
		if value, ok := data[key]; ok {
			if str, ok := value.(string); ok {
				return str, nil
			}
			return "", errors.New("value is not a string")
		}
	}

	// 尝试键值对格式: key: value（使用缓存正则）
	pattern := "(?i)" + regexp.QuoteMeta(key) + "\\s*[:=]\\s*(.+?)(?:\n|$)"
	re := getCachedRegex(pattern)
	matches := re.FindStringSubmatch(p.content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}

	return "", errors.New("key not found")
}

// ExtractSection 提取指定章节
func (p *ResponseParser) ExtractSection(title string) (string, error) {
	// 匹配章节标题和内容（使用缓存正则）
	pattern := "(?i)(?:^|\\n)#+\\s*" + regexp.QuoteMeta(title) + "\\s*\\n([\\s\\S]*?)(?:\\n#+|$)"
	re := getCachedRegex(pattern)
	matches := re.FindStringSubmatch(p.content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}
	return "", errors.New("section not found")
}

// RemoveMarkdown 移除 Markdown 格式
// 使用预编译的正则表达式提升性能，避免每次调用都重新编译
func (p *ResponseParser) RemoveMarkdown() string {
	content := p.content

	// 移除代码块（使用预编译正则）
	content = reMarkdownCodeBlock.ReplaceAllString(content, "")

	// 移除内联代码（使用预编译正则）
	content = reMarkdownInlineCode.ReplaceAllString(content, "")

	// 移除标题标记（使用预编译正则）
	content = reMarkdownHeading.ReplaceAllString(content, "")

	// 移除粗体和斜体（使用预编译正则）
	content = reMarkdownBoldDouble.ReplaceAllString(content, "$1")
	content = reMarkdownItalicSingle.ReplaceAllString(content, "$1")
	content = reMarkdownBoldUnderscore.ReplaceAllString(content, "$1")
	content = reMarkdownItalicUnderscore.ReplaceAllString(content, "$1")

	// 移除链接（使用预编译正则）
	content = reMarkdownLink.ReplaceAllString(content, "$1")

	return strings.TrimSpace(content)
}

// GetPlainText 获取纯文本内容
func (p *ResponseParser) GetPlainText() string {
	return p.RemoveMarkdown()
}

// IsEmpty 检查内容是否为空
func (p *ResponseParser) IsEmpty() bool {
	return strings.TrimSpace(p.content) == ""
}

// Length 返回内容长度
func (p *ResponseParser) Length() int {
	return len(p.content)
}
