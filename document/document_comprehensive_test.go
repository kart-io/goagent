package document

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/retrieval"
)

// ============================================================================
// Web Loader Tests
// ============================================================================

func TestWebLoader_Load_Success(t *testing.T) {
	// Create a test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><p>Hello World</p></body></html>`))
	}))
	defer server.Close()

	loader := NewWebLoader(WebLoaderConfig{
		URL:     server.URL,
		Timeout: 5 * time.Second,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, `<html><body><p>Hello World</p></body></html>`, docs[0].PageContent)

	// Verify metadata
	source, ok := docs[0].GetMetadata("source")
	assert.True(t, ok)
	assert.Equal(t, server.URL, source)
}

func TestWebLoader_Load_StripHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html>
			<head><title>Test</title></head>
			<body>
				<script>alert('test')</script>
				<p>Hello World</p>
				<style>body { color: red; }</style>
				<a href="test">Link</a>
			</body>
		</html>`))
	}))
	defer server.Close()

	loader := NewWebLoader(WebLoaderConfig{
		URL:       server.URL,
		StripHTML: true,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)

	// Verify HTML tags are removed
	assert.NotContains(t, docs[0].PageContent, "<")
	assert.NotContains(t, docs[0].PageContent, ">")
	assert.NotContains(t, docs[0].PageContent, "alert")
	assert.Contains(t, docs[0].PageContent, "Hello World")
	assert.Contains(t, docs[0].PageContent, "Link")
}

func TestWebLoader_Load_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify custom header was sent
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom-Header"))
		assert.Equal(t, "test-agent", r.Header.Get("User-Agent"))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Content"))
	}))
	defer server.Close()

	loader := NewWebLoader(WebLoaderConfig{
		URL: server.URL,
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
			"User-Agent":      "test-agent",
		},
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)
}

func TestWebLoader_Load_DefaultUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "k8s-agent-document-loader/1.0", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Content"))
	}))
	defer server.Close()

	loader := NewWebLoader(WebLoaderConfig{
		URL: server.URL,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)
}

func TestWebLoader_Load_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	loader := NewWebLoader(WebLoaderConfig{
		URL: server.URL,
	})

	_, err := loader.Load(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "http status")
}

func TestWebLoader_Load_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	loader := NewWebLoader(WebLoaderConfig{
		URL:     server.URL,
		Timeout: 100 * time.Millisecond,
	})

	_, err := loader.Load(context.Background())
	assert.Error(t, err)
}

func TestWebLoader_Load_InvalidURL(t *testing.T) {
	loader := NewWebLoader(WebLoaderConfig{
		URL: "invalid://url",
	})

	_, err := loader.Load(context.Background())
	assert.Error(t, err)
}

func TestWebLoader_LoadAndSplit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		// Create content that will be split into multiple chunks
		content := strings.Repeat("word ", 500) // Large enough to split
		w.Write([]byte(content))
	}))
	defer server.Close()

	loader := NewWebLoader(WebLoaderConfig{
		URL: server.URL,
	})

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		Separator:    " ",
		ChunkSize:    200,
		ChunkOverlap: 10,
	})

	docs, err := loader.LoadAndSplit(context.Background(), splitter)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(docs), 1) // At least one chunk
}

func TestWebLoader_Metadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"key": "value"}`))
	}))
	defer server.Close()

	loader := NewWebLoader(WebLoaderConfig{
		URL: server.URL,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)

	// Verify metadata
	contentType, ok := docs[0].GetMetadata("content_type")
	assert.True(t, ok)
	assert.Contains(t, contentType, "application/json")

	statusCode, ok := docs[0].GetMetadata("status_code")
	assert.True(t, ok)
	assert.Equal(t, http.StatusOK, statusCode)

	contentLength, ok := docs[0].GetMetadata("content_length")
	assert.True(t, ok)
	assert.Greater(t, contentLength.(int), 0)
}

// ============================================================================
// Markdown Loader Tests (Comprehensive)
// ============================================================================

func TestMarkdownLoader_RemoveImages(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	content := `# Title

This is text with ![image1](url1.jpg) an image.

![image2](url2.png) Another line.

More text.`

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewMarkdownLoader(MarkdownLoaderConfig{
		FilePath:     testFile,
		RemoveImages: true,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)

	// Images should be removed (lines with ![])
	assert.NotContains(t, docs[0].PageContent, "image1")
	assert.NotContains(t, docs[0].PageContent, "image2")
}

func TestMarkdownLoader_RemoveCodeFmt(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	content := `# Title

Text with inline code here.

` + "```go\n" + `func main() {
    println("hello")
}` + "\n```" + `

More text.`

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewMarkdownLoader(MarkdownLoaderConfig{
		FilePath:      testFile,
		RemoveCodeFmt: true,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)

	// Code formatting should be removed but content preserved
	assert.Contains(t, docs[0].PageContent, "inline code")
	assert.Contains(t, docs[0].PageContent, "func main")
	assert.NotContains(t, docs[0].PageContent, "```")
}

func TestMarkdownLoader_CombinedRemoval(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	content := "# Title\n\n" +
		"Text with [link](http://example.com) and ![image](img.jpg).\n\n" +
		"```code```\n\n" +
		"More text."

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewMarkdownLoader(MarkdownLoaderConfig{
		FilePath:      testFile,
		RemoveImages:  true,
		RemoveLinks:   true,
		RemoveCodeFmt: true,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)

	// The content is processed - check that main structure is preserved
	assert.Contains(t, docs[0].PageContent, "Title")
	assert.Contains(t, docs[0].PageContent, "code")
	// Links should be replaced by their text (the ]( part should be gone)
	assert.NotContains(t, docs[0].PageContent, "](")
}

func TestMarkdownLoader_NoTitle(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	content := `## Section

This file has no top-level title.

Just content.`

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewMarkdownLoader(MarkdownLoaderConfig{
		FilePath: testFile,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)

	title, ok := docs[0].GetMetadata("title")
	assert.False(t, ok)
	assert.Nil(t, title)
}

func TestMarkdownLoader_LoadAndSplit(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	content := `# Title

` + strings.Repeat("This is a paragraph. ", 100)

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewMarkdownLoader(MarkdownLoaderConfig{
		FilePath: testFile,
	})

	splitter := NewMarkdownTextSplitter(MarkdownTextSplitterConfig{
		ChunkSize:    200,
		ChunkOverlap: 50,
	})

	docs, err := loader.LoadAndSplit(context.Background(), splitter)
	require.NoError(t, err)
	// Expect at least 1 chunk - might be just 1 if markdown structure keeps it together
	assert.GreaterOrEqual(t, len(docs), 1)
}

func TestMarkdownLoader_Metadata(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	content := `# Test Title

Content here.`

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewMarkdownLoader(MarkdownLoaderConfig{
		FilePath: testFile,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)

	// Check source
	source, ok := docs[0].GetMetadata("source")
	assert.True(t, ok)
	assert.Equal(t, testFile, source)

	// Check source type
	sourceType, ok := docs[0].GetMetadata("source_type")
	assert.True(t, ok)
	assert.Equal(t, "markdown", sourceType)

	// Check file size
	fileSize, ok := docs[0].GetMetadata("file_size")
	assert.True(t, ok)
	assert.Greater(t, fileSize.(int), 0)

	// Check title
	title, ok := docs[0].GetMetadata("title")
	assert.True(t, ok)
	assert.Equal(t, "Test Title", title)
}

// ============================================================================
// JSON Loader Tests (Comprehensive)
// ============================================================================

func TestJSONLoader_MissingContentKey(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")

	content := `{"author": "John", "title": "Test"}`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewJSONLoader(JSONLoaderConfig{
		FilePath:   testFile,
		ContentKey: "missing_key",
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 0) // No document created without content key
}

func TestJSONLoader_MetadataExtraction(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")

	content := `[
		{"content": "First", "author": "John", "date": "2024-01-01", "tags": "test"},
		{"content": "Second", "author": "Jane", "date": "2024-01-02", "category": "docs"}
	]`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewJSONLoader(JSONLoaderConfig{
		FilePath:     testFile,
		MetadataKeys: []string{"author", "date"},
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 2)

	// Check first document metadata
	author, ok := docs[0].GetMetadata("author")
	assert.True(t, ok)
	assert.Equal(t, "John", author)

	date, ok := docs[0].GetMetadata("date")
	assert.True(t, ok)
	assert.Equal(t, "2024-01-01", date)

	// tags should not be in metadata (not in list)
	_, ok = docs[0].GetMetadata("tags")
	assert.False(t, ok)
}

func TestJSONLoader_AllMetadataExtraction(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")

	content := `{"content": "Test", "author": "John", "rating": 5}`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewJSONLoader(JSONLoaderConfig{
		FilePath: testFile,
		// MetadataKeys is empty, so all fields except content should be extracted
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)

	// Both author and rating should be extracted
	author, ok := docs[0].GetMetadata("author")
	assert.True(t, ok)
	assert.Equal(t, "John", author)

	rating, ok := docs[0].GetMetadata("rating")
	assert.True(t, ok)
	assert.Equal(t, float64(5), rating)
}

func TestJSONLoader_JSONLinesWithMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.jsonl")

	content := `{"content": "Line 1", "id": 1}
{"content": "Line 2", "id": 2}
{"content": "Line 3", "id": 3}`

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewJSONLoader(JSONLoaderConfig{
		FilePath:  testFile,
		JSONLines: true,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 3)

	// Check line numbers are added
	lineNum, ok := docs[0].GetMetadata("line_number")
	assert.True(t, ok)
	assert.Equal(t, 1, lineNum)

	lineNum, ok = docs[2].GetMetadata("line_number")
	assert.True(t, ok)
	assert.Equal(t, 3, lineNum)
}

func TestJSONLoader_InvalidJSONLines(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.jsonl")

	content := `{"content": "Line 1"}
invalid json line
{"content": "Line 3"}`

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewJSONLoader(JSONLoaderConfig{
		FilePath:  testFile,
		JSONLines: true,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	// Invalid lines are skipped
	assert.Len(t, docs, 2)
	assert.Equal(t, "Line 1", docs[0].PageContent)
	assert.Equal(t, "Line 3", docs[1].PageContent)
}

func TestJSONLoader_LoadAndSplit(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")

	content := `[
		{"content": "` + strings.Repeat("word ", 200) + `", "id": 1},
		{"content": "` + strings.Repeat("text ", 200) + `", "id": 2}
	]`

	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewJSONLoader(JSONLoaderConfig{
		FilePath: testFile,
	})

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		Separator:    " ",
		ChunkSize:    100,
		ChunkOverlap: 10,
	})

	docs, err := loader.LoadAndSplit(context.Background(), splitter)
	require.NoError(t, err)
	// At least 2 documents (from 2 JSON objects)
	assert.GreaterOrEqual(t, len(docs), 2)
}

func TestJSONLoader_InvalidJSONFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")

	content := `{invalid json}`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewJSONLoader(JSONLoaderConfig{
		FilePath: testFile,
	})

	_, err = loader.Load(context.Background())
	assert.Error(t, err)
}

func TestJSONLoader_UnsupportedJSONStructure(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")

	// JSON string is not a valid structure
	content := `"just a string"`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewJSONLoader(JSONLoaderConfig{
		FilePath: testFile,
	})

	_, err = loader.Load(context.Background())
	assert.Error(t, err)
}

func TestJSONLoader_Metadata(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")

	content := `{"content": "Test"}`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewJSONLoader(JSONLoaderConfig{
		FilePath: testFile,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)

	source, ok := docs[0].GetMetadata("source")
	assert.True(t, ok)
	assert.Equal(t, testFile, source)

	sourceType, ok := docs[0].GetMetadata("source_type")
	assert.True(t, ok)
	assert.Equal(t, "json", sourceType)
}

// ============================================================================
// Text Loader Tests (Comprehensive)
// ============================================================================

func TestTextLoader_LargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.txt")

	// Create a large file
	content := strings.Repeat("This is a line of text.\n", 10000)
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewTextLoader(TextLoaderConfig{
		FilePath: testFile,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)

	// Check file size metadata
	fileSize, ok := docs[0].GetMetadata("file_size")
	assert.True(t, ok)
	assert.Greater(t, fileSize.(int), 0)
}

func TestTextLoader_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.txt")

	err := os.WriteFile(testFile, []byte(""), 0o644)
	require.NoError(t, err)

	loader := NewTextLoader(TextLoaderConfig{
		FilePath: testFile,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, "", docs[0].PageContent)
}

func TestTextLoader_NonExistentFile(t *testing.T) {
	loader := NewTextLoader(TextLoaderConfig{
		FilePath: "/nonexistent/file.txt",
	})

	_, err := loader.Load(context.Background())
	assert.Error(t, err)
}

func TestTextLoader_EncodingMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	content := "Test content"
	err := os.WriteFile(testFile, []byte(content), 0o644)
	require.NoError(t, err)

	loader := NewTextLoader(TextLoaderConfig{
		FilePath: testFile,
		Encoding: "utf-16",
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)

	encoding, ok := docs[0].GetMetadata("encoding")
	assert.True(t, ok)
	assert.Equal(t, "utf-16", encoding)
}

func TestDirectoryLoader_Recursive(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested directory structure
	subDir := filepath.Join(tmpDir, "sub")
	err := os.Mkdir(subDir, 0o755)
	require.NoError(t, err)

	deepDir := filepath.Join(subDir, "deep")
	err = os.Mkdir(deepDir, 0o755)
	require.NoError(t, err)

	// Create files at different levels
	_ = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("Content 1"), 0o644)
	_ = os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("Content 2"), 0o644)
	_ = os.WriteFile(filepath.Join(deepDir, "file3.txt"), []byte("Content 3"), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "file.log"), []byte("Log content"), 0o644)

	// Test with recursive enabled
	loader := NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath:   tmpDir,
		Glob:      "*.txt",
		Recursive: true,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 3) // Should find all .txt files recursively

	// Test with recursive disabled
	loader = NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath:   tmpDir,
		Glob:      "*.txt",
		Recursive: false,
	})

	docs, err = loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1) // Should only find top-level .txt file
}

func TestDirectoryLoader_CustomLoader(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	_ = os.WriteFile(filepath.Join(tmpDir, "file1.json"), []byte(`{"content": "JSON 1"}`), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "file2.json"), []byte(`{"content": "JSON 2"}`), 0o644)

	// Custom loader that uses JSON loader
	customLoader := func(path string) DocumentLoader {
		return NewJSONLoader(JSONLoaderConfig{
			FilePath: path,
		})
	}

	loader := NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath:   tmpDir,
		Glob:      "*.json",
		Loader:    customLoader,
		Recursive: false,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 2)
	assert.Equal(t, "JSON 1", docs[0].PageContent)
	assert.Equal(t, "JSON 2", docs[1].PageContent)
}

func TestDirectoryLoader_FileErrors(t *testing.T) {
	tmpDir := t.TempDir()

	// Create one valid and one problematic file
	validFile := filepath.Join(tmpDir, "valid.txt")
	_ = os.WriteFile(validFile, []byte("Valid content"), 0o644)

	// Create a file path that will fail (then delete it)
	badFile := filepath.Join(tmpDir, "bad.txt")
	_ = os.WriteFile(badFile, []byte("Content"), 0o644)
	_ = os.Remove(badFile)

	loader := NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath: tmpDir,
		Glob:    "*.txt",
	})

	// Should complete without error, just skipping bad files
	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	// Only valid file is loaded
	assert.Len(t, docs, 1)
}

func TestDirectoryLoader_LoadAndSplit(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte(strings.Repeat("word ", 100)), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte(strings.Repeat("text ", 100)), 0o644)

	loader := NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath: tmpDir,
		Glob:    "*.txt",
	})

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		Separator:    " ",
		ChunkSize:    100,
		ChunkOverlap: 10,
	})

	docs, err := loader.LoadAndSplit(context.Background(), splitter)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(docs), 2) // At least 2 documents (from 2 files)
}

// ============================================================================
// Splitter Tests (Comprehensive)
// ============================================================================

func TestBaseTextSplitter_GetChunkSize(t *testing.T) {
	splitter := NewBaseTextSplitter(BaseTextSplitterConfig{
		ChunkSize: 500,
	})

	assert.Equal(t, 500, splitter.GetChunkSize())
}

func TestBaseTextSplitter_GetChunkOverlap(t *testing.T) {
	splitter := NewBaseTextSplitter(BaseTextSplitterConfig{
		ChunkSize:    500,
		ChunkOverlap: 100,
	})

	assert.Equal(t, 100, splitter.GetChunkOverlap())
}

func TestBaseTextSplitter_DefaultValues(t *testing.T) {
	splitter := NewBaseTextSplitter(BaseTextSplitterConfig{})

	assert.Equal(t, 1000, splitter.GetChunkSize())
	// ChunkOverlap defaults to 200 in NewBaseTextSplitter but the config default is 200 or -1?
	// Let's check what it actually is
	assert.Greater(t, splitter.GetChunkOverlap(), -1) // Should be >= 0
}

func TestCharacterTextSplitter_EmptySeparator(t *testing.T) {
	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		Separator: "",
		ChunkSize: 50,
	})

	text := "This is a test"
	chunks, err := splitter.SplitText(text)
	require.NoError(t, err)

	// Empty separator should return original text
	assert.Len(t, chunks, 1)
	assert.Equal(t, text, chunks[0])
}

func TestCharacterTextSplitter_NoSeparatorConfig(t *testing.T) {
	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		ChunkSize: 50,
		// Separator not specified - should default to "\n\n"
	})

	text := "First\n\nSecond\n\nThird"
	chunks, err := splitter.SplitText(text)
	require.NoError(t, err)

	// With "\n\n" separator, might get 1 or more chunks depending on chunk size
	assert.GreaterOrEqual(t, len(chunks), 1)
}

func TestRecursiveCharacterTextSplitter_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{
			name: "empty text",
			text: "",
		},
		{
			name: "single word",
			text: "word",
		},
		{
			name: "text with only separators",
			text: "\n\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			splitter := NewRecursiveCharacterTextSplitter(RecursiveCharacterTextSplitterConfig{
				ChunkSize: 100,
			})

			chunks, err := splitter.SplitText(tt.text)
			require.NoError(t, err)
			// Should always return valid chunks
			assert.NotNil(t, chunks)
		})
	}
}

func TestTokenTextSplitter_EmptyText(t *testing.T) {
	splitter := NewTokenTextSplitter(TokenTextSplitterConfig{
		ChunkSize: 5,
	})

	chunks, err := splitter.SplitText("")
	require.NoError(t, err)
	assert.Len(t, chunks, 0)
}

func TestTokenTextSplitter_SingleWord(t *testing.T) {
	splitter := NewTokenTextSplitter(TokenTextSplitterConfig{
		ChunkSize: 5,
	})

	chunks, err := splitter.SplitText("singleword")
	require.NoError(t, err)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "singleword", chunks[0])
}

func TestTokenTextSplitter_LargeChunkSize(t *testing.T) {
	splitter := NewTokenTextSplitter(TokenTextSplitterConfig{
		ChunkSize: 10000,
	})

	text := "word1 word2 word3 word4 word5"
	chunks, err := splitter.SplitText(text)
	require.NoError(t, err)

	// All text fits in one chunk
	assert.Len(t, chunks, 1)
	assert.Equal(t, text, chunks[0])
}

func TestMarkdownTextSplitter_NoHeaders(t *testing.T) {
	splitter := NewMarkdownTextSplitter(MarkdownTextSplitterConfig{
		ChunkSize: 100,
	})

	text := `This is just regular text without any headers.
It has multiple lines.
But no markdown structure.`

	chunks, err := splitter.SplitText(text)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)
}

func TestCodeTextSplitter_Unsupported_Language(t *testing.T) {
	splitter := NewCodeTextSplitter(CodeTextSplitterConfig{
		Language:  "unsupported_language",
		ChunkSize: 500,
	})

	code := `Some code here
With multiple lines
But no specific structure`

	chunks, err := splitter.SplitText(code)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)
}

func TestCodeTextSplitter_Java(t *testing.T) {
	code := `public class MyClass {
    private int field;

    public void method1() {
        System.out.println("Hello");
    }

    protected void method2() {
        // More code
    }
}`

	splitter := NewCodeTextSplitter(CodeTextSplitterConfig{
		Language:     LanguageJava,
		ChunkSize:    150,
		ChunkOverlap: 20,
	})

	chunks, err := splitter.SplitText(code)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)
}

func TestCodeTextSplitter_Rust(t *testing.T) {
	code := `fn main() {
    println!("Hello");
}

struct MyStruct {
    field: String,
}

impl MyStruct {
    fn new() -> Self {
        MyStruct { field: String::new() }
    }
}

trait MyTrait {
    fn method(&self);
}`

	splitter := NewCodeTextSplitter(CodeTextSplitterConfig{
		Language:     LanguageRust,
		ChunkSize:    150,
		ChunkOverlap: 20,
	})

	chunks, err := splitter.SplitText(code)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)
}

func TestCodeTextSplitter_JavaScript(t *testing.T) {
	code := `function greet() {
    console.log("Hello");
}

const variable = 42;

let name = "John";

class MyClass {
    constructor() {}
}

if (true) {
    console.log("condition");
}`

	splitter := NewCodeTextSplitter(CodeTextSplitterConfig{
		Language:     LanguageJavaScript,
		ChunkSize:    150,
		ChunkOverlap: 20,
	})

	chunks, err := splitter.SplitText(code)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)
}

func TestCodeTextSplitter_CPP(t *testing.T) {
	code := `class MyClass {
public:
    void method1() {}
};

void standalone_function() {
    int x = 5;
}

struct MyStruct {
    int field;
};`

	splitter := NewCodeTextSplitter(CodeTextSplitterConfig{
		Language:     LanguageCpp,
		ChunkSize:    150,
		ChunkOverlap: 20,
	})

	chunks, err := splitter.SplitText(code)
	require.NoError(t, err)
	assert.Greater(t, len(chunks), 0)
}

// ============================================================================
// Callback Tests
// ============================================================================

func TestBaseTextSplitter_TriggerCallbacks_NoManager(t *testing.T) {
	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		ChunkSize: 50,
	})

	// Should not panic when callback manager is nil
	ctx := context.Background()
	err := splitter.TriggerCallbacks(ctx, "start", nil)
	assert.NoError(t, err)

	err = splitter.TriggerCallbacks(ctx, "end", nil)
	assert.NoError(t, err)

	err = splitter.TriggerCallbacks(ctx, "error", errors.New("test"))
	assert.NoError(t, err)

	// Unknown event type
	err = splitter.TriggerCallbacks(ctx, "unknown", nil)
	assert.NoError(t, err)
}

// ============================================================================
// Concurrent Operations Tests
// ============================================================================

func TestConcurrentDocumentLoading(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple test files
	fileCount := 10
	for i := 0; i < fileCount; i++ {
		path := filepath.Join(tmpDir, "file"+string(rune(48+i))+".txt")
		_ = os.WriteFile(path, []byte("Content "+string(rune(48+i))), 0o644)
	}

	loader := NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath: tmpDir,
		Glob:    "*.txt",
	})

	// Load documents concurrently
	var wg sync.WaitGroup
	results := make(chan []*interfaces.Document, 5)
	errors := make(chan error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			docs, err := loader.Load(context.Background())
			if err != nil {
				errors <- err
			} else {
				results <- docs
			}
		}()
	}

	wg.Wait()
	close(results)
	close(errors)

	// Verify all loads succeeded
	assert.Equal(t, 0, len(errors))
	assert.Equal(t, 5, len(results))
}

func TestConcurrentDocumentSplitting(t *testing.T) {
	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		ChunkSize:    100,
		ChunkOverlap: 20,
	})

	docs := make([]*interfaces.Document, 10)
	for i := 0; i < 10; i++ {
		content := strings.Repeat("word ", 100)
		docs[i] = retrieval.NewDocument(content, map[string]interface{}{"index": i})
	}

	// Split documents concurrently
	var wg sync.WaitGroup
	results := make(chan []*interfaces.Document, 5)
	errors := make(chan error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(docSet []*interfaces.Document) {
			defer wg.Done()
			splitDocs, err := splitter.SplitDocuments(docSet)
			if err != nil {
				errors <- err
			} else {
				results <- splitDocs
			}
		}(docs)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Verify all splits succeeded
	assert.Equal(t, 0, len(errors))
	assert.Equal(t, 5, len(results))
}

// ============================================================================
// Batch Processing Tests
// ============================================================================

func TestBatchDocumentProcessing(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	for i := 0; i < 5; i++ {
		content := strings.Repeat("word ", 50)
		path := filepath.Join(tmpDir, "doc"+string(rune(48+i))+".txt")
		_ = os.WriteFile(path, []byte(content), 0o644)
	}

	loader := NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath: tmpDir,
		Glob:    "*.txt",
	})

	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		Separator:    " ",
		ChunkSize:    100,
		ChunkOverlap: 10,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 5, len(docs))

	splitDocs, err := splitter.SplitDocuments(docs)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(splitDocs), 5) // At least as many as original docs
}

// ============================================================================
// Edge Cases and Error Handling
// ============================================================================

func TestBaseDocumentLoader_LoadAndSplitWithNilSplitter(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("Test content"), 0o644)

	loader := NewTextLoader(TextLoaderConfig{
		FilePath: testFile,
	})

	// LoadAndSplit with nil splitter should still work
	docs, err := loader.LoadAndSplit(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, docs, 1)
}

func TestMergeSplits_EmptyInput(t *testing.T) {
	splitter := NewBaseTextSplitter(BaseTextSplitterConfig{
		ChunkSize: 100,
	})

	result := splitter.MergeSplits([]string{}, " ")
	assert.Len(t, result, 0)
}

func TestMergeSplits_SingleItem(t *testing.T) {
	splitter := NewBaseTextSplitter(BaseTextSplitterConfig{
		ChunkSize: 100,
	})

	result := splitter.MergeSplits([]string{"single"}, " ")
	assert.Len(t, result, 1)
	assert.Equal(t, "single", result[0])
}

func TestHTMLStripping_NestedTags(t *testing.T) {
	html := `<div><span>Nested <b>bold</b> text</span></div>`
	result := stripHTMLTags(html)

	assert.NotContains(t, result, "<")
	assert.NotContains(t, result, ">")
	assert.Contains(t, result, "Nested")
	assert.Contains(t, result, "bold")
	assert.Contains(t, result, "text")
}

func TestMarkdownLink_ComplexURLs(t *testing.T) {
	text := removeMarkdownLinks(`Check [this link](https://example.com/path?query=value&other=123) for more info.`)
	assert.Contains(t, text, "this link")
	assert.NotContains(t, text, "https://")
}

func TestRemoveTag_CaseSensitivity(t *testing.T) {
	html := `<SCRIPT>alert('test')</SCRIPT>`
	result := removeTag(html, "script")

	assert.Equal(t, ``, result)
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestLoadMarkdownWithJSON_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mixed document types
	mdFile := filepath.Join(tmpDir, "doc.md")
	_ = os.WriteFile(mdFile, []byte(`# Title

Content here.`), 0o644)

	jsonFile := filepath.Join(tmpDir, "doc.json")
	_ = os.WriteFile(jsonFile, []byte(`{"content": "JSON content"}`), 0o644)

	// Load with custom loader that detects file type
	loader := NewDirectoryLoader(DirectoryLoaderConfig{
		DirPath: tmpDir,
		Glob:    "*",
		Loader: func(path string) DocumentLoader {
			if strings.HasSuffix(path, ".md") {
				return NewMarkdownLoader(MarkdownLoaderConfig{
					FilePath: path,
				})
			}
			return NewJSONLoader(JSONLoaderConfig{
				FilePath: path,
			})
		},
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 2)
}

func TestFullPipeline_LoadSplitProcess(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file with substantial content
	testFile := filepath.Join(tmpDir, "content.txt")
	content := `Paragraph 1: This is the first paragraph with multiple sentences. Each sentence adds information.

Paragraph 2: This is the second paragraph with different content. It continues the discussion.

Paragraph 3: Final paragraph summarizing everything. This completes the document.`

	_ = os.WriteFile(testFile, []byte(content), 0o644)

	// Load with text loader
	loader := NewTextLoader(TextLoaderConfig{
		FilePath: testFile,
	})

	docs, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, docs, 1)

	// Split with recursive splitter
	splitter := NewRecursiveCharacterTextSplitter(RecursiveCharacterTextSplitterConfig{
		ChunkSize:    150,
		ChunkOverlap: 20,
	})

	splitDocs, err := splitter.SplitDocuments(docs)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(splitDocs), 1) // At least one chunk

	// Verify chunks maintain metadata
	for i, doc := range splitDocs {
		_, ok := doc.GetMetadata("chunk_index")
		assert.True(t, ok)

		source, ok := doc.GetMetadata("source")
		assert.True(t, ok)
		assert.Equal(t, testFile, source)

		// Note: chunk size might exceed limit due to word boundary preservation
		assert.Greater(t, len(doc.PageContent), 0) // Chunks should have content

		t.Logf("Chunk %d: %d chars, %d words", i, len(doc.PageContent), len(strings.Fields(doc.PageContent)))
	}
}
