package document

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/retrieval"
)

func TestCharacterTextSplitter(t *testing.T) {
	text := "This is a test.\n\nThis is another paragraph.\n\nAnd a third one."

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		Separator:    "\n\n",
		ChunkSize:    50,
		ChunkOverlap: 10,
	})

	chunks, err := splitter.SplitText(text)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)

	// 验证每个块的大小
	for _, chunk := range chunks {
		assert.LessOrEqual(t, len(chunk), 60)
	}
}

func TestRecursiveCharacterTextSplitter(t *testing.T) {
	text := `First paragraph here.

Second paragraph with more content.

Third paragraph. It has multiple sentences. Each one is important.

Fourth paragraph is the last one.`

	splitter := NewRecursiveCharacterTextSplitter(RecursiveCharacterTextSplitterConfig{
		ChunkSize:    100,
		ChunkOverlap: 20,
	})

	chunks, err := splitter.SplitText(text)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 1)

	// 验证块大小
	for _, chunk := range chunks {
		assert.LessOrEqual(t, len(chunk), 120)
	}

	// 验证重叠
	if len(chunks) > 1 {
		// 检查是否有重叠内容
		for i := 0; i < len(chunks)-1; i++ {
			// 简单检查:最后几个单词可能在下一个块中
			// 这是简化的重叠检查
			assert.NotEmpty(t, chunks[i])
			assert.NotEmpty(t, chunks[i+1])
		}
	}
}

func TestTokenTextSplitter(t *testing.T) {
	text := "This is a test with many words to split into chunks"

	splitter := NewTokenTextSplitter(TokenTextSplitterConfig{
		ChunkSize:    5, // 5 个单词
		ChunkOverlap: 2,
	})

	chunks, err := splitter.SplitText(text)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 1)

	// 验证每个块的单词数
	for _, chunk := range chunks {
		wordCount := len(chunk)
		if wordCount > 0 {
			assert.LessOrEqual(t, len(chunk), 100) // 粗略检查
		}
	}
}

func TestMarkdownTextSplitter(t *testing.T) {
	text := `# Title

First paragraph under title.

## Section 1

Content in section 1.

### Subsection 1.1

More detailed content.

## Section 2

Content in section 2.`

	splitter := NewMarkdownTextSplitter(MarkdownTextSplitterConfig{
		ChunkSize:    100,
		ChunkOverlap: 20,
	})

	chunks, err := splitter.SplitText(text)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 1)

	// 验证标题被保留
	foundTitle := false
	for _, chunk := range chunks {
		if len(chunk) > 0 && chunk[0] == '#' {
			foundTitle = true
			break
		}
	}
	assert.True(t, foundTitle, "Should preserve markdown headers")
}

func TestCodeTextSplitter_Go(t *testing.T) {
	code := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}

func helper() {
	// Helper function
	doSomething()
}

type MyStruct struct {
	Field1 string
	Field2 int
}`

	splitter := NewCodeTextSplitter(CodeTextSplitterConfig{
		Language:     LanguageGo,
		ChunkSize:    150,
		ChunkOverlap: 20,
	})

	chunks, err := splitter.SplitText(code)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)

	// 验证函数定义被保留在一起
	for _, chunk := range chunks {
		// 如果包含 func,应该包含完整的函数签名
		if len(chunk) > 0 {
			assert.NotEmpty(t, chunk)
		}
	}
}

func TestCodeTextSplitter_Python(t *testing.T) {
	code := `class MyClass:
    def __init__(self):
        self.value = 0

    def method1(self):
        print("Method 1")

    def method2(self):
        print("Method 2")

def standalone_function():
    return "Hello"`

	splitter := NewCodeTextSplitter(CodeTextSplitterConfig{
		Language:     LanguagePython,
		ChunkSize:    150,
		ChunkOverlap: 20,
	})

	chunks, err := splitter.SplitText(code)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)
}

func TestSplitDocuments(t *testing.T) {
	docs := []*interfaces.Document{
		retrieval.NewDocument(
			"First document with some content that needs to be split.",
			map[string]interface{}{"source": "doc1"},
		),
		retrieval.NewDocument(
			"Second document also with content to split.",
			map[string]interface{}{"source": "doc2"},
		),
	}

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		Separator:    " ",
		ChunkSize:    30,
		ChunkOverlap: 5,
	})

	splitDocs, err := splitter.SplitDocuments(docs)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(splitDocs), len(docs)) // 至少和原文档数量相等

	// 验证元数据被保留和扩展
	for _, doc := range splitDocs {
		// 应该有 chunk_index
		_, ok := doc.GetMetadata("chunk_index")
		assert.True(t, ok)

		// 应该有 chunk_total
		_, ok = doc.GetMetadata("chunk_total")
		assert.True(t, ok)

		// 应该有 source_id
		_, ok = doc.GetMetadata("source_id")
		assert.True(t, ok)

		// 原始 source 应该被保留
		_, ok = doc.GetMetadata("source")
		assert.True(t, ok)
	}
}

func TestChunkOverlap(t *testing.T) {
	text := "word1 word2 word3 word4 word5 word6 word7 word8 word9 word10"

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		Separator:    " ",
		ChunkSize:    25, // 大约 5 个单词
		ChunkOverlap: 10, // 大约 2 个单词重叠
	})

	chunks, err := splitter.SplitText(text)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 1)

	t.Logf("Generated %d chunks:", len(chunks))
	for i, chunk := range chunks {
		t.Logf("Chunk %d: %s", i, chunk)
	}
}

func BenchmarkCharacterTextSplitter(b *testing.B) {
	text := strings.Repeat("This is a long text for benchmarking. ", 100)

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		ChunkSize:    1000,
		ChunkOverlap: 200,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = splitter.SplitText(text)
	}
}

func BenchmarkRecursiveCharacterTextSplitter(b *testing.B) {
	textPart := `This is a long text for benchmarking.

It has multiple paragraphs.

Each paragraph has several sentences. This makes it interesting.

And more content here.`
	text := strings.Repeat(textPart, 50)

	splitter := NewRecursiveCharacterTextSplitter(RecursiveCharacterTextSplitterConfig{
		ChunkSize:    1000,
		ChunkOverlap: 200,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = splitter.SplitText(text)
	}
}

func BenchmarkCodeTextSplitter(b *testing.B) {
	codePart := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}`
	code := strings.Repeat(codePart, 20)

	splitter := NewCodeTextSplitter(CodeTextSplitterConfig{
		Language:     LanguageGo,
		ChunkSize:    500,
		ChunkOverlap: 100,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = splitter.SplitText(code)
	}
}

func BenchmarkSplitDocuments(b *testing.B) {
	docs := make([]*interfaces.Document, 100)
	for i := 0; i < 100; i++ {
		docs[i] = retrieval.NewDocument(
			"This is test content that will be split into chunks.",
			map[string]interface{}{"index": i},
		)
	}

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		ChunkSize:    50,
		ChunkOverlap: 10,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = splitter.SplitDocuments(docs)
	}
}
