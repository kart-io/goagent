package main

import (
	"fmt"

	"github.com/kart-io/goagent/document"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/retrieval"
)

func main() {
	fmt.Println("=== Text Splitters Examples ===")

	// 1. Character Text Splitter
	characterSplitterExample()

	// 2. Recursive Character Text Splitter
	recursiveSplitterExample()

	// 3. Token Text Splitter
	tokenSplitterExample()

	// 4. Markdown Text Splitter
	markdownSplitterExample()

	// 5. Code Text Splitter
	codeSplitterExample()

	// 6. Load and Split
	loadAndSplitExample()
}

func characterSplitterExample() {
	fmt.Println("1. Character Text Splitter")
	fmt.Println("--------------------------")

	text := `This is the first paragraph. It contains several sentences.

This is the second paragraph. It also has multiple sentences.

This is the third paragraph with more content.`

	splitter := document.NewCharacterTextSplitter(document.CharacterTextSplitterConfig{
		Separator:    "\n\n",
		ChunkSize:    100,
		ChunkOverlap: 20,
	})

	chunks, _ := splitter.SplitText(text)

	fmt.Printf("Split into %d chunks:\n", len(chunks))
	for i, chunk := range chunks {
		fmt.Printf("Chunk %d (length %d): %s...\n", i+1, len(chunk), truncate(chunk, 50))
	}
	fmt.Println()
}

func recursiveSplitterExample() {
	fmt.Println("2. Recursive Character Text Splitter")
	fmt.Println("-------------------------------------")

	text := `First paragraph here with some content.

Second paragraph. It has multiple sentences. Each sentence is important.

Third paragraph with even more detailed content that needs to be properly split.`

	splitter := document.NewRecursiveCharacterTextSplitter(document.RecursiveCharacterTextSplitterConfig{
		ChunkSize:    80,
		ChunkOverlap: 15,
	})

	chunks, _ := splitter.SplitText(text)

	fmt.Printf("Split into %d chunks using recursive strategy:\n", len(chunks))
	for i, chunk := range chunks {
		fmt.Printf("Chunk %d: %s...\n", i+1, truncate(chunk, 60))
	}
	fmt.Println()
}

func tokenSplitterExample() {
	fmt.Println("3. Token Text Splitter")
	fmt.Println("----------------------")

	text := "This is a sample text with many words that will be split based on token count"

	splitter := document.NewTokenTextSplitter(document.TokenTextSplitterConfig{
		ChunkSize:    5, // 5 tokens (words)
		ChunkOverlap: 2,
	})

	chunks, _ := splitter.SplitText(text)

	fmt.Printf("Split into %d chunks by tokens:\n", len(chunks))
	for i, chunk := range chunks {
		fmt.Printf("Chunk %d: %s\n", i+1, chunk)
	}
	fmt.Println()
}

func markdownSplitterExample() {
	fmt.Println("4. Markdown Text Splitter")
	fmt.Println("-------------------------")

	markdown := `# Main Title

Introduction paragraph.

## Section 1

Content for section 1.

### Subsection 1.1

Detailed content here.

## Section 2

Content for section 2.`

	splitter := document.NewMarkdownTextSplitter(document.MarkdownTextSplitterConfig{
		ChunkSize:    100,
		ChunkOverlap: 20,
	})

	chunks, _ := splitter.SplitText(markdown)

	fmt.Printf("Split into %d chunks preserving markdown structure:\n", len(chunks))
	for i, chunk := range chunks {
		lines := chunk[:min(50, len(chunk))]
		fmt.Printf("Chunk %d: %s...\n", i+1, lines)
	}
	fmt.Println()
}

func codeSplitterExample() {
	fmt.Println("5. Code Text Splitter (Go)")
	fmt.Println("--------------------------")

	code := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}

func helper() {
	doSomething()
}

type MyStruct struct {
	Field1 string
	Field2 int
}`

	splitter := document.NewCodeTextSplitter(document.CodeTextSplitterConfig{
		Language:     document.LanguageGo,
		ChunkSize:    150,
		ChunkOverlap: 20,
	})

	chunks, _ := splitter.SplitText(code)

	fmt.Printf("Split into %d chunks preserving code structure:\n", len(chunks))
	for i, chunk := range chunks {
		fmt.Printf("Chunk %d:\n%s\n", i+1, truncate(chunk, 100))
		fmt.Println("---")
	}
	fmt.Println()
}

func loadAndSplitExample() {
	fmt.Println("6. Load and Split Combined")
	fmt.Println("---------------------------")

	// 创建文档
	docs := []*interfaces.Document{
		retrieval.NewDocument(
			"This is a long document that needs to be split into smaller chunks for processing.",
			map[string]interface{}{"source": "doc1.txt"},
		),
		retrieval.NewDocument(
			"Another document with substantial content that also requires splitting.",
			map[string]interface{}{"source": "doc2.txt"},
		),
	}

	splitter := document.NewCharacterTextSplitter(document.CharacterTextSplitterConfig{
		Separator:    " ",
		ChunkSize:    40,
		ChunkOverlap: 10,
	})

	splitDocs, _ := splitter.SplitDocuments(docs)

	fmt.Printf("Split %d documents into %d chunks:\n", len(docs), len(splitDocs))
	for i, doc := range splitDocs {
		fmt.Printf("Chunk %d:\n", i+1)
		fmt.Printf("  Content: %s...\n", truncate(doc.PageContent, 50))
		fmt.Printf("  Source: %v\n", doc.Metadata["source"])
		fmt.Printf("  Chunk Index: %v/%v\n",
			doc.Metadata["chunk_index"],
			doc.Metadata["chunk_total"],
		)
	}
	fmt.Println()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
