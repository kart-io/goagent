package utils

import (
	"testing"
)

// 测试用的 Markdown 内容
const benchmarkMarkdownContent = `# Introduction

This is a **bold** text and this is *italic*.

Here's a [link](https://example.com).

## Code Example

` + "```go" + `
func main() {
    fmt.Println("Hello, World!")
}
` + "```" + `

### List Items

1. First item
2. Second item
3. Third item

- Bullet one
- Bullet two

### Inline Code

Use ` + "`" + `fmt.Println()` + "`" + ` to print output.

**Important**: Remember to __use__ _proper_ formatting.
`

// BenchmarkRemoveMarkdown 基准测试 RemoveMarkdown 方法
// 这是优化效果最显著的方法，预期性能提升 60%+
func BenchmarkRemoveMarkdown(b *testing.B) {
	parser := NewResponseParser(benchmarkMarkdownContent)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.RemoveMarkdown()
	}
}

// BenchmarkExtractJSON_CodeBlock 基准测试从代码块提取 JSON
func BenchmarkExtractJSON_CodeBlock(b *testing.B) {
	content := "```json\n{\"key\": \"value\", \"number\": 42}\n```"
	parser := NewResponseParser(content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ExtractJSON()
	}
}

// BenchmarkExtractJSON_Braces 基准测试从花括号提取 JSON
func BenchmarkExtractJSON_Braces(b *testing.B) {
	content := `Some text before {"key": "value", "number": 42} some text after`
	parser := NewResponseParser(content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ExtractJSON()
	}
}

// BenchmarkExtractAllCodeBlocks 基准测试提取所有代码块
func BenchmarkExtractAllCodeBlocks(b *testing.B) {
	content := "```go\nfunc main() {}\n```\n\n```python\nprint('hello')\n```\n\n```javascript\nconsole.log('test')\n```"
	parser := NewResponseParser(content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.ExtractAllCodeBlocks()
	}
}

// BenchmarkExtractList_Numbered 基准测试提取数字列表
func BenchmarkExtractList_Numbered(b *testing.B) {
	content := `1. First item
2. Second item
3. Third item
4. Fourth item
5. Fifth item`
	parser := NewResponseParser(content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.ExtractList()
	}
}

// BenchmarkExtractList_Bullet 基准测试提取符号列表
func BenchmarkExtractList_Bullet(b *testing.B) {
	content := `- First item
- Second item
* Third item
* Fourth item
- Fifth item`
	parser := NewResponseParser(content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.ExtractList()
	}
}

// BenchmarkExtractCodeBlock 基准测试提取指定语言代码块（动态正则缓存）
func BenchmarkExtractCodeBlock(b *testing.B) {
	content := "```go\nfunc main() {}\n```"
	parser := NewResponseParser(content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ExtractCodeBlock("go")
	}
}

// BenchmarkExtractCodeBlock_CacheHit 测试缓存命中效果
func BenchmarkExtractCodeBlock_CacheHit(b *testing.B) {
	content := "```go\nfunc main() {}\n```"
	parser := NewResponseParser(content)

	// 预热缓存
	_, _ = parser.ExtractCodeBlock("go")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ExtractCodeBlock("go")
	}
}

// BenchmarkExtractKeyValue 基准测试提取键值对（动态正则缓存）
func BenchmarkExtractKeyValue(b *testing.B) {
	content := `name: John Doe
age: 30
city: New York`
	parser := NewResponseParser(content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ExtractKeyValue("name")
	}
}

// BenchmarkExtractKeyValue_CacheHit 测试缓存命中效果
func BenchmarkExtractKeyValue_CacheHit(b *testing.B) {
	content := `name: John Doe
age: 30
city: New York`
	parser := NewResponseParser(content)

	// 预热缓存
	_, _ = parser.ExtractKeyValue("name")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ExtractKeyValue("name")
	}
}

// BenchmarkExtractSection 基准测试提取章节（动态正则缓存）
func BenchmarkExtractSection(b *testing.B) {
	content := `# Introduction
This is intro

## Details
This is details section with content

### Subsection
More detailed information here`
	parser := NewResponseParser(content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ExtractSection("Details")
	}
}

// BenchmarkExtractSection_CacheHit 测试缓存命中效果
func BenchmarkExtractSection_CacheHit(b *testing.B) {
	content := `# Introduction
This is intro

## Details
This is details section with content

### Subsection
More detailed information here`
	parser := NewResponseParser(content)

	// 预热缓存
	_, _ = parser.ExtractSection("Details")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ExtractSection("Details")
	}
}

// BenchmarkGetPlainText 基准测试获取纯文本（内部调用 RemoveMarkdown）
func BenchmarkGetPlainText(b *testing.B) {
	parser := NewResponseParser(benchmarkMarkdownContent)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.GetPlainText()
	}
}

// BenchmarkParseToMap 基准测试解析为 Map
func BenchmarkParseToMap(b *testing.B) {
	content := `{"name": "test", "value": 123, "active": true, "items": [1, 2, 3]}`
	parser := NewResponseParser(content)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.ParseToMap()
	}
}

// BenchmarkParseToStruct 基准测试解析为结构体
func BenchmarkParseToStruct(b *testing.B) {
	content := `{"name": "test", "value": 123, "active": true}`
	parser := NewResponseParser(content)

	type TestStruct struct {
		Name   string `json:"name"`
		Value  int    `json:"value"`
		Active bool   `json:"active"`
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result TestStruct
		_ = parser.ParseToStruct(&result)
	}
}

// BenchmarkRemoveMarkdown_Large 测试大文本的性能
func BenchmarkRemoveMarkdown_Large(b *testing.B) {
	// 构造一个大的 Markdown 文档（重复内容100次）
	largeContent := ""
	for i := 0; i < 100; i++ {
		largeContent += benchmarkMarkdownContent
	}
	parser := NewResponseParser(largeContent)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.RemoveMarkdown()
	}
}

// BenchmarkExtractList_Large 测试大列表的性能
func BenchmarkExtractList_Large(b *testing.B) {
	// 构造一个大列表（1000项）
	largeList := ""
	for i := 1; i <= 1000; i++ {
		largeList += "1. Item " + string(rune(i)) + "\n"
	}
	parser := NewResponseParser(largeList)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.ExtractList()
	}
}

// BenchmarkConcurrentRemoveMarkdown 测试并发性能
func BenchmarkConcurrentRemoveMarkdown(b *testing.B) {
	parser := NewResponseParser(benchmarkMarkdownContent)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = parser.RemoveMarkdown()
		}
	})
}

// BenchmarkConcurrentExtractJSON 测试并发性能
func BenchmarkConcurrentExtractJSON(b *testing.B) {
	content := "```json\n{\"key\": \"value\"}\n```"
	parser := NewResponseParser(content)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = parser.ExtractJSON()
		}
	})
}

// BenchmarkConcurrentExtractCodeBlock 测试动态缓存的并发性能
func BenchmarkConcurrentExtractCodeBlock(b *testing.B) {
	content := "```go\nfunc main() {}\n```"
	parser := NewResponseParser(content)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = parser.ExtractCodeBlock("go")
		}
	})
}
