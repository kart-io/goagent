package document

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/retrieval"
	"github.com/kart-io/k8s-agent/common/errors"
)

// WebLoader Web 页面加载器
//
// 通过 HTTP 加载 Web 页面内容
type WebLoader struct {
	*BaseDocumentLoader
	url       string
	headers   map[string]string
	timeout   time.Duration
	client    *http.Client
	stripHTML bool
}

// WebLoaderConfig Web 加载器配置
type WebLoaderConfig struct {
	URL             string
	Headers         map[string]string
	Timeout         time.Duration
	StripHTML       bool
	Metadata        map[string]interface{}
	CallbackManager *core.CallbackManager
}

// NewWebLoader 创建 Web 加载器
func NewWebLoader(config WebLoaderConfig) *WebLoader {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}

	config.Metadata["source"] = config.URL
	config.Metadata["source_type"] = "web"

	client := &http.Client{
		Timeout: config.Timeout,
	}

	return &WebLoader{
		BaseDocumentLoader: NewBaseDocumentLoader(config.Metadata, config.CallbackManager),
		url:                config.URL,
		headers:            config.Headers,
		timeout:            config.Timeout,
		client:             client,
		stripHTML:          config.StripHTML,
	}
}

// Load 加载 Web 页面
func (l *WebLoader) Load(ctx context.Context) ([]*retrieval.Document, error) {
	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnStart(ctx, map[string]interface{}{
			"loader": "web",
			"url":    l.url,
		}); err != nil {
			return nil, err
		}
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", l.url, nil)
	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, errors.Wrap(errors.CodeInternalError, "failed to create request", err)
	}

	// 设置请求头
	for key, value := range l.headers {
		req.Header.Set(key, value)
	}

	// 设置默认 User-Agent
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "k8s-agent-document-loader/1.0")
	}

	// 发送请求
	resp, err := l.client.Do(req.WithContext(ctx))
	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, errors.Wrap(errors.CodeInternalError, "failed to fetch url", err)
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		err := errors.New(errors.CodeInternalError, "http status: "+resp.Status)
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, err
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, errors.Wrap(errors.CodeInternalError, "failed to read response", err)
	}

	content := string(body)

	// 处理 HTML
	if l.stripHTML {
		content = stripHTMLTags(content)
	}

	// 创建文档
	metadata := l.GetMetadata()
	metadata["content_type"] = resp.Header.Get("Content-Type")
	metadata["content_length"] = len(body)
	metadata["status_code"] = resp.StatusCode

	doc := retrieval.NewDocument(content, metadata)

	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnEnd(ctx, map[string]interface{}{
			"num_docs":       1,
			"content_length": len(body),
		}); err != nil {
			return nil, err
		}
	}

	return []*retrieval.Document{doc}, nil
}

// LoadAndSplit 加载并分割
func (l *WebLoader) LoadAndSplit(ctx context.Context, splitter TextSplitter) ([]*retrieval.Document, error) {
	return l.BaseDocumentLoader.LoadAndSplit(ctx, l, splitter)
}

// stripHTMLTags 移除 HTML 标签(简单实现)
func stripHTMLTags(html string) string {
	// 移除脚本和样式标签
	html = removeTag(html, "script")
	html = removeTag(html, "style")

	// 移除所有 HTML 标签
	inTag := false
	var result strings.Builder

	for _, char := range html {
		if char == '<' {
			inTag = true
			continue
		}
		if char == '>' {
			inTag = false
			result.WriteRune(' ')
			continue
		}
		if !inTag {
			result.WriteRune(char)
		}
	}

	// 清理多余空白
	text := result.String()
	text = strings.Join(strings.Fields(text), " ")

	return strings.TrimSpace(text)
}

// removeTag 移除指定标签及其内容
func removeTag(html, tag string) string {
	startTag := "<" + tag
	endTag := "</" + tag + ">"

	for {
		start := strings.Index(strings.ToLower(html), startTag)
		if start == -1 {
			break
		}

		end := strings.Index(strings.ToLower(html[start:]), endTag)
		if end == -1 {
			break
		}

		end += start + len(endTag)
		html = html[:start] + html[end:]
	}

	return html
}
