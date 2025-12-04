package document

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPDFLoader 测试 PDF 加载器创建
func TestNewPDFLoader(t *testing.T) {
	tests := []struct {
		name    string
		config  PDFLoaderConfig
		wantURL bool
		wantSrc string
	}{
		{
			name: "本地文件路径",
			config: PDFLoaderConfig{
				Source: "/path/to/document.pdf",
			},
			wantURL: false,
			wantSrc: "/path/to/document.pdf",
		},
		{
			name: "HTTP URL",
			config: PDFLoaderConfig{
				Source: "http://example.com/document.pdf",
			},
			wantURL: true,
			wantSrc: "http://example.com/document.pdf",
		},
		{
			name: "HTTPS URL",
			config: PDFLoaderConfig{
				Source: "https://example.com/document.pdf",
			},
			wantURL: true,
			wantSrc: "https://example.com/document.pdf",
		},
		{
			name: "带元数据",
			config: PDFLoaderConfig{
				Source: "/path/to/doc.pdf",
				Metadata: map[string]interface{}{
					"author": "test",
				},
			},
			wantURL: false,
			wantSrc: "/path/to/doc.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := NewPDFLoader(tt.config)

			assert.NotNil(t, loader)
			assert.Equal(t, tt.wantURL, loader.isURL)
			assert.Equal(t, tt.wantSrc, loader.source)

			// 验证元数据
			metadata := loader.GetMetadata()
			assert.Equal(t, tt.wantSrc, metadata["source"])
			assert.Equal(t, "pdf", metadata["loader_type"])
		})
	}
}

// TestPDFLoader_LoadNonExistentFile 测试加载不存在的文件
func TestPDFLoader_LoadNonExistentFile(t *testing.T) {
	loader := NewPDFLoader(PDFLoaderConfig{
		Source: "/nonexistent/path/to/document.pdf",
	})

	ctx := context.Background()
	_, err := loader.Load(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open PDF file")
}

// TestPDFLoader_LoadInvalidURL 测试加载无效 URL
func TestPDFLoader_LoadInvalidURL(t *testing.T) {
	loader := NewPDFLoader(PDFLoaderConfig{
		Source: "http://invalid-url-that-does-not-exist.example.com/doc.pdf",
	})

	ctx := context.Background()
	_, err := loader.Load(ctx)

	assert.Error(t, err)
}

// TestPDFLoader_MetadataPreservation 测试元数据保留
func TestPDFLoader_MetadataPreservation(t *testing.T) {
	customMetadata := map[string]interface{}{
		"custom_key": "custom_value",
		"author":     "test_author",
	}

	loader := NewPDFLoader(PDFLoaderConfig{
		Source:   "/path/to/test.pdf",
		Metadata: customMetadata,
	})

	metadata := loader.GetMetadata()

	// 验证自定义元数据被保留
	assert.Equal(t, "custom_value", metadata["custom_key"])
	assert.Equal(t, "test_author", metadata["author"])

	// 验证默认元数据被添加
	assert.Equal(t, "/path/to/test.pdf", metadata["source"])
	assert.Equal(t, "pdf", metadata["loader_type"])
}

// TestPDFLoader_LoadRealPDF 测试加载真实 PDF 文件（如果存在）
func TestPDFLoader_LoadRealPDF(t *testing.T) {
	// 查找测试 PDF 文件
	testPDFPath := findTestPDF()
	if testPDFPath == "" {
		t.Skip("未找到测试 PDF 文件，跳过此测试")
	}

	loader := NewPDFLoader(PDFLoaderConfig{
		Source: testPDFPath,
	})

	ctx := context.Background()
	docs, err := loader.Load(ctx)

	require.NoError(t, err)
	require.Len(t, docs, 1)

	doc := docs[0]
	assert.NotEmpty(t, doc.PageContent)
	assert.NotNil(t, doc.Metadata)
	assert.Contains(t, doc.Metadata, "page_count")
	assert.Contains(t, doc.Metadata, "content_length")
}

// TestPDFLoader_LoadAndSplit 测试加载并分割 PDF
func TestPDFLoader_LoadAndSplit(t *testing.T) {
	testPDFPath := findTestPDF()
	if testPDFPath == "" {
		t.Skip("未找到测试 PDF 文件，跳过此测试")
	}

	loader := NewPDFLoader(PDFLoaderConfig{
		Source: testPDFPath,
	})

	// 创建分割器
	splitter := NewCharacterTextSplitter(CharacterTextSplitterConfig{
		ChunkSize:    200,
		ChunkOverlap: 50,
	})

	ctx := context.Background()
	docs, err := loader.LoadAndSplit(ctx, splitter)

	require.NoError(t, err)
	assert.Greater(t, len(docs), 0)

	// 验证每个文档都有正确的元数据
	for i, doc := range docs {
		assert.NotEmpty(t, doc.PageContent)
		assert.Contains(t, doc.Metadata, "chunk_index")
		assert.Equal(t, i, doc.Metadata["chunk_index"])
	}
}

// TestPDFLoader_LoadByPage 测试按页加载 PDF
func TestPDFLoader_LoadByPage(t *testing.T) {
	testPDFPath := findTestPDF()
	if testPDFPath == "" {
		t.Skip("未找到测试 PDF 文件，跳过此测试")
	}

	loader := NewPDFLoader(PDFLoaderConfig{
		Source: testPDFPath,
	})

	ctx := context.Background()
	docs, err := loader.LoadByPage(ctx)

	require.NoError(t, err)
	assert.Greater(t, len(docs), 0)

	// 验证每页的元数据
	for _, doc := range docs {
		assert.NotEmpty(t, doc.PageContent)
		assert.Contains(t, doc.Metadata, "page_number")
		assert.Contains(t, doc.Metadata, "total_pages")

		pageNum := doc.Metadata["page_number"].(int)
		totalPages := doc.Metadata["total_pages"].(int)
		assert.Greater(t, pageNum, 0)
		assert.LessOrEqual(t, pageNum, totalPages)
	}
}

// findTestPDF 查找测试用的 PDF 文件
func findTestPDF() string {
	// 尝试常见位置
	possiblePaths := []string{
		"testdata/sample.pdf",
		"../testdata/sample.pdf",
		"../../testdata/sample.pdf",
	}

	// 也可以从环境变量获取
	if envPath := os.Getenv("TEST_PDF_PATH"); envPath != "" {
		possiblePaths = append([]string{envPath}, possiblePaths...)
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			absPath, _ := filepath.Abs(path)
			return absPath
		}
	}

	return ""
}

// TestPDFLoaderConfig_Defaults 测试配置默认值
func TestPDFLoaderConfig_Defaults(t *testing.T) {
	// 空元数据应被初始化
	loader := NewPDFLoader(PDFLoaderConfig{
		Source: "/test/path.pdf",
	})

	metadata := loader.GetMetadata()
	assert.NotNil(t, metadata)
	assert.Equal(t, "/test/path.pdf", metadata["source"])
}

// TestPDFLoader_URLDetection 测试 URL 检测
func TestPDFLoader_URLDetection(t *testing.T) {
	tests := []struct {
		source   string
		expected bool
	}{
		{"http://example.com/doc.pdf", true},
		{"https://example.com/doc.pdf", true},
		{"HTTP://EXAMPLE.COM/DOC.PDF", false}, // 大写不匹配
		{"/local/path/doc.pdf", false},
		{"./relative/path.pdf", false},
		{"file:///local/file.pdf", false}, // file:// 协议不算 URL
		{"ftp://example.com/doc.pdf", false},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			loader := NewPDFLoader(PDFLoaderConfig{
				Source: tt.source,
			})

			assert.Equal(t, tt.expected, loader.isURL,
				"Source: %s, expected isURL=%v, got %v",
				tt.source, tt.expected, loader.isURL)
		})
	}
}

// BenchmarkPDFLoader_Load 基准测试
func BenchmarkPDFLoader_Load(b *testing.B) {
	testPDFPath := findTestPDF()
	if testPDFPath == "" {
		b.Skip("未找到测试 PDF 文件，跳过此测试")
	}

	loader := NewPDFLoader(PDFLoaderConfig{
		Source: testPDFPath,
	})

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := loader.Load(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestPDFLoader_ContextCancellation 测试上下文取消
func TestPDFLoader_ContextCancellation(t *testing.T) {
	loader := NewPDFLoader(PDFLoaderConfig{
		Source: "https://example.com/large-document.pdf",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err := loader.Load(ctx)

	// 应该返回错误（上下文已取消）
	assert.Error(t, err)
}

// TestPDFLoader_EmptyContent 测试空内容处理
func TestPDFLoader_EmptyContent(t *testing.T) {
	// 创建一个临时的无效 PDF 文件
	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.pdf")

	err := os.WriteFile(emptyFile, []byte("not a valid pdf"), 0644)
	require.NoError(t, err)

	loader := NewPDFLoader(PDFLoaderConfig{
		Source: emptyFile,
	})

	ctx := context.Background()
	_, err = loader.Load(ctx)

	// 无效的 PDF 应该返回错误
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "failed to parse PDF") ||
		strings.Contains(err.Error(), "failed to read PDF"))
}
