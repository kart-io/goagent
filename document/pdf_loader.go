package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/retrieval"
	"github.com/ledongthuc/pdf"
)

// PDFLoader PDF 文档加载器
//
// 支持从本地文件或 URL 加载 PDF 文档，提取文本内容
type PDFLoader struct {
	*BaseDocumentLoader
	source   string // 文件路径或 URL
	isURL    bool
	password string // PDF 密码（可选）
}

// PDFLoaderConfig PDF 加载器配置
type PDFLoaderConfig struct {
	// Source PDF 文件路径或 URL
	Source string
	// Password PDF 密码（如果文档加密）
	Password string
	// Metadata 额外的元数据
	Metadata map[string]interface{}
	// CallbackManager 回调管理器
	CallbackManager *core.CallbackManager
}

// NewPDFLoader 创建 PDF 加载器
func NewPDFLoader(config PDFLoaderConfig) *PDFLoader {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	// 判断是否为 URL
	isURL := strings.HasPrefix(config.Source, "http://") || strings.HasPrefix(config.Source, "https://")

	// 添加来源元数据
	config.Metadata["source"] = config.Source
	config.Metadata["loader_type"] = "pdf"

	return &PDFLoader{
		BaseDocumentLoader: NewBaseDocumentLoader(config.Metadata, config.CallbackManager),
		source:             config.Source,
		isURL:              isURL,
		password:           config.Password,
	}
}

// Load 加载 PDF 文档
func (l *PDFLoader) Load(ctx context.Context) ([]*interfaces.Document, error) {
	// 触发开始回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnStart(ctx, map[string]interface{}{
			"loader": "pdf",
			"source": l.source,
		}); err != nil {
			return nil, err
		}
	}

	var pdfReader io.ReaderAt
	var size int64
	var err error

	if l.isURL {
		// 从 URL 下载 PDF
		pdfReader, size, err = l.downloadPDF(ctx)
	} else {
		// 从本地文件读取
		pdfReader, size, err = l.readLocalPDF()
	}

	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, err
	}

	// 解析 PDF
	content, pageCount, err := l.parsePDF(pdfReader, size)
	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, err
	}

	// 创建文档
	metadata := l.GetMetadata()
	metadata["page_count"] = pageCount
	metadata["content_length"] = len(content)

	doc := retrieval.NewDocument(content, metadata)

	// 触发完成回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnEnd(ctx, map[string]interface{}{
			"num_docs":   1,
			"page_count": pageCount,
		}); err != nil {
			return nil, err
		}
	}

	return []*interfaces.Document{doc}, nil
}

// LoadAndSplit 加载并分割 PDF 文档
func (l *PDFLoader) LoadAndSplit(ctx context.Context, splitter TextSplitter) ([]*interfaces.Document, error) {
	return l.BaseDocumentLoader.LoadAndSplit(ctx, l, splitter)
}

// LoadByPage 按页加载 PDF 文档
//
// 返回每页作为单独的文档
func (l *PDFLoader) LoadByPage(ctx context.Context) ([]*interfaces.Document, error) {
	// 触发开始回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnStart(ctx, map[string]interface{}{
			"loader": "pdf",
			"source": l.source,
			"mode":   "by_page",
		}); err != nil {
			return nil, err
		}
	}

	var pdfReader io.ReaderAt
	var size int64
	var err error

	if l.isURL {
		pdfReader, size, err = l.downloadPDF(ctx)
	} else {
		pdfReader, size, err = l.readLocalPDF()
	}

	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, err
	}

	// 解析 PDF 并按页提取
	docs, err := l.parsePDFByPage(pdfReader, size)
	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, err
	}

	// 触发完成回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnEnd(ctx, map[string]interface{}{
			"num_docs":   len(docs),
			"page_count": len(docs),
		}); err != nil {
			return nil, err
		}
	}

	return docs, nil
}

// downloadPDF 从 URL 下载 PDF
func (l *PDFLoader) downloadPDF(ctx context.Context) (io.ReaderAt, int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", l.source, nil)
	if err != nil {
		return nil, 0, errors.Wrap(err, errors.CodeInternal, "failed to create HTTP request")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, errors.Wrap(err, errors.CodeInternal, "failed to download PDF")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, errors.New(errors.CodeInternal, fmt.Sprintf("failed to download PDF: HTTP %d", resp.StatusCode))
	}

	// 读取全部内容到内存
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, errors.Wrap(err, errors.CodeInternal, "failed to read PDF content")
	}

	return bytes.NewReader(data), int64(len(data)), nil
}

// readLocalPDF 从本地文件读取 PDF
func (l *PDFLoader) readLocalPDF() (io.ReaderAt, int64, error) {
	file, err := os.Open(l.source)
	if err != nil {
		return nil, 0, errors.Wrap(err, errors.CodeInternal, "failed to open PDF file")
	}
	// 确保文件在函数返回时关闭
	defer func() {
		if cerr := file.Close(); cerr != nil {
			// 记录关闭错误（在读取成功后发生的关闭错误通常不是致命的）
			fmt.Fprintf(os.Stderr, "warning: failed to close PDF file: %v\n", cerr)
		}
	}()

	// 获取文件大小
	stat, err := file.Stat()
	if err != nil {
		return nil, 0, errors.Wrap(err, errors.CodeInternal, "failed to get file info")
	}

	// 读取全部内容到内存（避免文件句柄问题）
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, errors.Wrap(err, errors.CodeInternal, "failed to read PDF file")
	}

	return bytes.NewReader(data), stat.Size(), nil
}

// parsePDF 解析 PDF 并提取全部文本
func (l *PDFLoader) parsePDF(reader io.ReaderAt, size int64) (string, int, error) {
	pdfReader, err := pdf.NewReader(reader, size)
	if err != nil {
		return "", 0, errors.Wrap(err, errors.CodeInternal, "failed to parse PDF")
	}

	pageCount := pdfReader.NumPage()
	var content strings.Builder

	for i := 1; i <= pageCount; i++ {
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			// 跳过无法解析的页面
			continue
		}

		if content.Len() > 0 {
			content.WriteString("\n\n")
		}
		content.WriteString(strings.TrimSpace(text))
	}

	return content.String(), pageCount, nil
}

// parsePDFByPage 解析 PDF 并按页提取文本
func (l *PDFLoader) parsePDFByPage(reader io.ReaderAt, size int64) ([]*interfaces.Document, error) {
	pdfReader, err := pdf.NewReader(reader, size)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "failed to parse PDF")
	}

	pageCount := pdfReader.NumPage()
	docs := make([]*interfaces.Document, 0, pageCount)

	for i := 1; i <= pageCount; i++ {
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			// 跳过无法解析的页面
			continue
		}

		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		// 创建每页的元数据
		metadata := make(map[string]interface{})
		for k, v := range l.GetMetadata() {
			metadata[k] = v
		}
		metadata["page_number"] = i
		metadata["total_pages"] = pageCount

		doc := retrieval.NewDocument(text, metadata)
		docs = append(docs, doc)
	}

	return docs, nil
}
