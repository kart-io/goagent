package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kart-io/goagent/document"
)

func main() {
	fmt.Println("=== Document Loaders Examples ===")

	// 创建示例文件
	setupExampleFiles()

	// 1. Text Loader
	textLoaderExample()

	// 2. Markdown Loader
	markdownLoaderExample()

	// 3. JSON Loader
	jsonLoaderExample()

	// 4. Directory Loader
	directoryLoaderExample()

	// 清理
	cleanupExampleFiles()
}

func textLoaderExample() {
	fmt.Println("1. Text Loader Example")
	fmt.Println("----------------------")

	loader := document.NewTextLoader(document.TextLoaderConfig{
		FilePath: "/tmp/example.txt",
	})

	docs, err := loader.Load(context.Background())
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Loaded %d document(s)\n", len(docs))
	if len(docs) > 0 {
		fmt.Printf("Content preview: %s...\n", truncate(docs[0].PageContent, 100))
		fmt.Printf("Metadata: %v\n", docs[0].Metadata)
	}
	fmt.Println()
}

func markdownLoaderExample() {
	fmt.Println("2. Markdown Loader Example")
	fmt.Println("---------------------------")

	loader := document.NewMarkdownLoader(document.MarkdownLoaderConfig{
		FilePath:    "/tmp/example.md",
		RemoveLinks: true,
	})

	docs, err := loader.Load(context.Background())
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Loaded %d document(s)\n", len(docs))
	if len(docs) > 0 {
		fmt.Printf("Title: %v\n", docs[0].Metadata["title"])
		fmt.Printf("Content preview: %s...\n", truncate(docs[0].PageContent, 100))
	}
	fmt.Println()
}

func jsonLoaderExample() {
	fmt.Println("3. JSON Loader Example")
	fmt.Println("----------------------")

	loader := document.NewJSONLoader(document.JSONLoaderConfig{
		FilePath:     "/tmp/example.json",
		ContentKey:   "text",
		MetadataKeys: []string{"author", "category"},
	})

	docs, err := loader.Load(context.Background())
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Loaded %d document(s)\n", len(docs))
	for i, doc := range docs {
		fmt.Printf("Document %d:\n", i+1)
		fmt.Printf("  Content: %s\n", truncate(doc.PageContent, 80))
		fmt.Printf("  Author: %v\n", doc.Metadata["author"])
		fmt.Printf("  Category: %v\n", doc.Metadata["category"])
	}
	fmt.Println()
}

func directoryLoaderExample() {
	fmt.Println("4. Directory Loader Example")
	fmt.Println("----------------------------")

	loader := document.NewDirectoryLoader(document.DirectoryLoaderConfig{
		DirPath:   "/tmp/documents",
		Glob:      "*.txt",
		Recursive: false,
	})

	docs, err := loader.Load(context.Background())
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Loaded %d document(s) from directory\n", len(docs))
	for i, doc := range docs {
		fmt.Printf("Document %d: %s\n", i+1, doc.Metadata["source"])
	}
	fmt.Println()
}

func setupExampleFiles() {
	// 创建示例文本文件
	_ = os.WriteFile("/tmp/example.txt", []byte(`This is a sample text file.
It contains multiple lines of text.
This is useful for testing the text loader.`), 0o644)

	// 创建示例 Markdown 文件
	_ = os.WriteFile("/tmp/example.md", []byte(`# Document Loaders

This is a markdown document with [links](https://example.com) and formatting.

## Features

- Easy to use
- Supports multiple formats
- Extensible`), 0o644)

	// 创建示例 JSON 文件
	_ = os.WriteFile("/tmp/example.json", []byte(`[
  {
    "text": "First article about AI",
    "author": "John Doe",
    "category": "Technology"
  },
  {
    "text": "Second article about ML",
    "author": "Jane Smith",
    "category": "Science"
  }
]`), 0o644)

	// 创建示例目录
	_ = os.MkdirAll("/tmp/documents", 0o755)
	_ = os.WriteFile("/tmp/documents/doc1.txt", []byte("Document 1 content"), 0o644)
	_ = os.WriteFile("/tmp/documents/doc2.txt", []byte("Document 2 content"), 0o644)
	_ = os.WriteFile("/tmp/documents/doc3.txt", []byte("Document 3 content"), 0o644)
}

func cleanupExampleFiles() {
	_ = os.Remove("/tmp/example.txt")
	_ = os.Remove("/tmp/example.md")
	_ = os.Remove("/tmp/example.json")
	_ = os.RemoveAll("/tmp/documents")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
