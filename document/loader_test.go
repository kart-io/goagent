package document

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextLoader(t *testing.T) {
	// 创建临时文件
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	content := "This is a test file.\nIt has multiple lines.\nFor testing purposes."
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	// 测试加载
	loader := NewTextLoader(TextLoaderConfig{
		FilePath: testFile,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, content, docs[0].PageContent)

	// 检查元数据
	source, ok := docs[0].GetMetadata("source")
	assert.True(t, ok)
	assert.Equal(t, testFile, source)
}

func TestDirectoryLoader(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()

	// 创建测试文件
	files := map[string]string{
		"file1.txt": "Content 1",
		"file2.txt": "Content 2",
		"file3.log": "Content 3",
	}

	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		err := os.WriteFile(path, []byte(content), 0o644)
		require.NoError(t, err)
	}

	// 测试加载所有 .txt 文件
	loader := NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath: tmpDir,
		Glob:    "*.txt",
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 2) // 只有 2 个 .txt 文件
}

func TestMarkdownLoader(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	content := `# Title

This is a paragraph with a [link](https://example.com) and an image ![alt](image.jpg).

## Section

More content here.`

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	// 测试加载(保留所有格式)
	loader := NewMarkdownLoader(MarkdownLoaderConfig{
		FilePath: testFile,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)

	// 检查标题元数据
	title, ok := docs[0].GetMetadata("title")
	assert.True(t, ok)
	assert.Equal(t, "Title", title)

	// 测试移除链接
	loader = NewMarkdownLoader(MarkdownLoaderConfig{
		FilePath:    testFile,
		RemoveLinks: true,
	})

	docs, err = loader.Load(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, docs[0].PageContent, "](")
}

func TestJSONLoader(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("Single JSON Object", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test.json")
		content := `{"content": "Test content", "author": "John"}`
		err := os.WriteFile(testFile, []byte(content), 0o644)
		require.NoError(t, err)

		loader := NewJSONLoader(JSONLoaderConfig{
			FilePath:     testFile,
			ContentKey:   "content",
			MetadataKeys: []string{"author"},
		})

		docs, err := loader.Load(context.Background())
		require.NoError(t, err)
		assert.Len(t, docs, 1)
		assert.Equal(t, "Test content", docs[0].PageContent)

		author, ok := docs[0].GetMetadata("author")
		assert.True(t, ok)
		assert.Equal(t, "John", author)
	})

	t.Run("JSON Array", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test_array.json")
		content := `[
			{"content": "First", "id": 1},
			{"content": "Second", "id": 2}
		]`
		err := os.WriteFile(testFile, []byte(content), 0o644)
		require.NoError(t, err)

		loader := NewJSONLoader(JSONLoaderConfig{
			FilePath: testFile,
		})

		docs, err := loader.Load(context.Background())
		require.NoError(t, err)
		assert.Len(t, docs, 2)
	})

	t.Run("JSON Lines", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test.jsonl")
		content := `{"content": "Line 1"}
{"content": "Line 2"}
{"content": "Line 3"}`
		err := os.WriteFile(testFile, []byte(content), 0o644)
		require.NoError(t, err)

		loader := NewJSONLoader(JSONLoaderConfig{
			FilePath:  testFile,
			JSONLines: true,
		})

		docs, err := loader.Load(context.Background())
		require.NoError(t, err)
		assert.Len(t, docs, 3)
	})
}

func TestLoadAndSplit(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	content := "This is a long text that should be split into multiple chunks. " +
		"Each chunk should be small enough to process efficiently."
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewTextLoader(TextLoaderConfig{
		FilePath: testFile,
	})

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		Separator:    " ",
		ChunkSize:    50,
		ChunkOverlap: 10,
	})

	docs, err := loader.LoadAndSplit(context.Background(), splitter)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(docs), 1) // 至少 1 个块

	// 检查每个块的大小(考虑到重叠和分割逻辑)
	for _, doc := range docs {
		assert.LessOrEqual(t, len(doc.PageContent), 150) // 允许足够的余地
	}

	// 检查元数据
	for i, doc := range docs {
		chunkIndex, ok := doc.GetMetadata("chunk_index")
		assert.True(t, ok)
		assert.Equal(t, i, chunkIndex)
	}
}

func BenchmarkTextLoader(b *testing.B) {
	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Test content for benchmarking"
	_ = os.WriteFile(testFile, []byte(content), 0o644)

	loader := NewTextLoader(TextLoaderConfig{
		FilePath: testFile,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = loader.Load(context.Background())
	}
}

func BenchmarkDirectoryLoader(b *testing.B) {
	tmpDir := b.TempDir()

	// 创建 10 个测试文件
	for i := 0; i < 10; i++ {
		path := filepath.Join(tmpDir, "file"+string(rune(i))+".txt")
		_ = os.WriteFile(path, []byte("Content"), 0o644)
	}

	loader := NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath: tmpDir,
		Glob:    "*.txt",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = loader.Load(context.Background())
	}
}

// 测试 Web Loader (需要模拟 HTTP 服务器)
// 实际使用时应该使用 httptest
func TestWebLoaderStripHTML(t *testing.T) {
	html := `<html>
		<head><title>Test</title></head>
		<body>
			<script>alert('test')</script>
			<p>Hello World</p>
			<style>body { color: red; }</style>
		</body>
	</html>`

	stripped := stripHTMLTags(html)
	assert.NotContains(t, stripped, "<")
	assert.NotContains(t, stripped, "alert")
	assert.Contains(t, stripped, "Hello World")
}
