package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kart-io/goagent/document"
)

func main() {
	fmt.Println("=== Advanced Document Processing Example ===")

	// 创建示例文档
	setupDocuments()
	defer cleanup()

	// 场景 1: 处理技术文档
	processTechnicalDocumentation()

	// 场景 2: 处理代码库
	processCodeRepository()

	// 场景 3: 批量处理多种格式
	processMixedFormats()
}

func processTechnicalDocumentation() {
	fmt.Println("Scenario 1: Processing Technical Documentation")
	fmt.Println("===============================================")

	// 加载 Markdown 文档
	loader := document.NewMarkdownLoader(document.MarkdownLoaderConfig{
		FilePath:    "/tmp/docs/api-reference.md",
		RemoveLinks: false, // 保留链接用于参考
	})

	// 使用 Markdown 特定的分割器
	splitter := document.NewMarkdownTextSplitter(document.MarkdownTextSplitterConfig{
		HeadersToSplitOn: []string{"#", "##", "###"},
		ChunkSize:        500,
		ChunkOverlap:     50,
	})

	// 加载并分割
	docs, err := loader.LoadAndSplit(context.Background(), splitter)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Processed technical documentation:\n")
	fmt.Printf("  Total chunks: %d\n", len(docs))
	fmt.Printf("  First chunk preview: %s...\n\n", truncate(docs[0].PageContent, 100))
}

func processCodeRepository() {
	fmt.Println("Scenario 2: Processing Code Repository")
	fmt.Println("=======================================")

	// 加载目录中的所有 Go 文件
	loader := document.NewDirectoryLoader(document.DirectoryLoaderConfig{
		DirPath:   "/tmp/code",
		Glob:      "*.go",
		Recursive: true,
		Loader: func(path string) document.DocumentLoader {
			return document.NewTextLoader(document.TextLoaderConfig{
				FilePath: path,
			})
		},
	})

	// 使用代码分割器
	splitter := document.NewCodeTextSplitter(document.CodeTextSplitterConfig{
		Language:     document.LanguageGo,
		ChunkSize:    300,
		ChunkOverlap: 30,
	})

	docs, err := loader.LoadAndSplit(context.Background(), splitter)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Processed code repository:\n")
	fmt.Printf("  Total code chunks: %d\n", len(docs))

	// 统计函数定义
	functionCount := 0
	for _, doc := range docs {
		if contains(doc.PageContent, "func ") {
			functionCount++
		}
	}
	fmt.Printf("  Chunks containing functions: %d\n\n", functionCount)
}

func processMixedFormats() {
	fmt.Println("Scenario 3: Processing Mixed Formats")
	fmt.Println("=====================================")

	// 处理不同格式的文档
	formats := []struct {
		name     string
		loader   document.DocumentLoader
		splitter document.TextSplitter
	}{
		{
			name: "Text Documents",
			loader: document.NewDirectoryLoader(document.DirectoryLoaderConfig{
				DirPath: "/tmp/mixed",
				Glob:    "*.txt",
			}),
			splitter: document.NewRecursiveCharacterTextSplitter(
				document.RecursiveCharacterTextSplitterConfig{
					ChunkSize:    200,
					ChunkOverlap: 40,
				},
			),
		},
		{
			name: "JSON Data",
			loader: document.NewJSONLoader(document.JSONLoaderConfig{
				FilePath:   "/tmp/mixed/data.jsonl",
				JSONLines:  true,
				ContentKey: "content",
			}),
			splitter: document.NewCharacterTextSplitter(
				document.CharacterTextSplitterConfig{
					Separator:    "\n",
					ChunkSize:    150,
					ChunkOverlap: 30,
				},
			),
		},
	}

	totalDocs := 0
	for _, format := range formats {
		docs, err := format.loader.LoadAndSplit(context.Background(), format.splitter)
		if err != nil {
			fmt.Printf("Error processing %s: %v\n", format.name, err)
			continue
		}

		fmt.Printf("  %s: %d chunks\n", format.name, len(docs))
		totalDocs += len(docs)
	}

	fmt.Printf("  Total chunks across all formats: %d\n\n", totalDocs)
}

func setupDocuments() {
	// 创建目录
	_ = os.MkdirAll("/tmp/docs", 0o755)
	_ = os.MkdirAll("/tmp/code", 0o755)
	_ = os.MkdirAll("/tmp/mixed", 0o755)

	// 技术文档
	apiDoc := `# API Reference

## Authentication

All API requests require authentication using an API key.

### Getting an API Key

Visit the dashboard to generate your API key.

## Endpoints

### GET /api/users

Retrieve a list of users.

### POST /api/users

Create a new user.`

	_ = os.WriteFile("/tmp/docs/api-reference.md", []byte(apiDoc), 0o644)

	// 代码文件
	code1 := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}

func helper() {
	doWork()
}`

	code2 := `package utils

func ProcessData(data string) string {
	return data
}

type Config struct {
	Value string
}`

	_ = os.WriteFile("/tmp/code/main.go", []byte(code1), 0o644)
	_ = os.WriteFile("/tmp/code/utils.go", []byte(code2), 0o644)

	// 混合格式
	_ = os.WriteFile("/tmp/mixed/doc1.txt", []byte("Text document content"), 0o644)
	_ = os.WriteFile("/tmp/mixed/doc2.txt", []byte("Another text document"), 0o644)

	jsonl := `{"content": "First entry in JSON Lines format"}
{"content": "Second entry with more data"}
{"content": "Third entry for testing"}`

	_ = os.WriteFile("/tmp/mixed/data.jsonl", []byte(jsonl), 0o644)
}

func cleanup() {
	_ = os.RemoveAll("/tmp/docs")
	_ = os.RemoveAll("/tmp/code")
	_ = os.RemoveAll("/tmp/mixed")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
