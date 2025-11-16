package utils

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

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

	// 尝试提取 JSON 代码块
	jsonPattern := regexp.MustCompile("```json\\s*([\\s\\S]*?)```")
	matches := jsonPattern.FindStringSubmatch(p.content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}

	// 尝试提取 {} 包裹的内容
	bracePattern := regexp.MustCompile(`\{[\s\S]*\}`)
	match := bracePattern.FindString(p.content)
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
	pattern := regexp.MustCompile("```" + language + "\\s*([\\s\\S]*?)```")
	matches := pattern.FindStringSubmatch(p.content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}
	return "", errors.New("code block not found")
}

// ExtractAllCodeBlocks 提取所有代码块
func (p *ResponseParser) ExtractAllCodeBlocks() map[string]string {
	result := make(map[string]string)
	pattern := regexp.MustCompile("```(\\w+)\\s*([\\s\\S]*?)```")
	matches := pattern.FindAllStringSubmatch(p.content, -1)

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

	// 匹配数字列表: 1. item
	numberPattern := regexp.MustCompile(`(?m)^\d+\.\s+(.+)$`)
	numberMatches := numberPattern.FindAllStringSubmatch(p.content, -1)
	for _, match := range numberMatches {
		if len(match) > 1 {
			items = append(items, strings.TrimSpace(match[1]))
		}
	}

	// 如果没有数字列表，尝试匹配符号列表: - item 或 * item
	if len(items) == 0 {
		bulletPattern := regexp.MustCompile(`(?m)^[\-\*]\s+(.+)$`)
		bulletMatches := bulletPattern.FindAllStringSubmatch(p.content, -1)
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

	// 尝试键值对格式: key: value
	pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(key) + "\\s*[:=]\\s*(.+?)(?:\n|$)")
	matches := pattern.FindStringSubmatch(p.content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}

	return "", errors.New("key not found")
}

// ExtractSection 提取指定章节
func (p *ResponseParser) ExtractSection(title string) (string, error) {
	// 匹配章节标题和内容
	pattern := regexp.MustCompile("(?i)(?:^|\\n)#+\\s*" + regexp.QuoteMeta(title) + "\\s*\\n([\\s\\S]*?)(?:\\n#+|$)")
	matches := pattern.FindStringSubmatch(p.content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}
	return "", errors.New("section not found")
}

// RemoveMarkdown 移除 Markdown 格式
func (p *ResponseParser) RemoveMarkdown() string {
	content := p.content

	// 移除代码块
	content = regexp.MustCompile("```[\\s\\S]*?```").ReplaceAllString(content, "")

	// 移除内联代码
	content = regexp.MustCompile("`[^`]+`").ReplaceAllString(content, "")

	// 移除标题标记
	content = regexp.MustCompile(`(?m)^#+\s+`).ReplaceAllString(content, "")

	// 移除粗体和斜体
	content = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(content, "$1")
	content = regexp.MustCompile(`\*([^*]+)\*`).ReplaceAllString(content, "$1")
	content = regexp.MustCompile("__([^_]+)__").ReplaceAllString(content, "$1")
	content = regexp.MustCompile("_([^_]+)_").ReplaceAllString(content, "$1")

	// 移除链接
	content = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`).ReplaceAllString(content, "$1")

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
