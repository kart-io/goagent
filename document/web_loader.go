package document

import (
	"context"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
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
	client    *resty.Client
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

	client := resty.New().
		SetTimeout(config.Timeout).
		SetHeader("User-Agent", "k8s-agent-document-loader/1.0")

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
func (l *WebLoader) Load(ctx context.Context) ([]*interfaces.Document, error) {
	// 触发回调
	if l.callbackManager != nil {
		if err := l.callbackManager.OnStart(ctx, map[string]interface{}{
			"loader": "web",
			"url":    l.url,
		}); err != nil {
			return nil, err
		}
	}

	// 发送请求
	resp, err := l.client.R().
		SetContext(ctx).
		SetHeaders(l.headers).
		Get(l.url)

	if err != nil {
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, errors.Wrap(errors.CodeInternalError, "failed to fetch url", err)
	}

	// 检查状态码
	if resp.StatusCode() != 200 {
		err := errors.New(errors.CodeInternalError, "http status: "+resp.Status())
		if l.callbackManager != nil {
			_ = l.callbackManager.OnError(ctx, err)
		}
		return nil, err
	}

	// 获取响应内容
	body := resp.Body()
	content := string(body)

	// 处理 HTML
	if l.stripHTML {
		content = stripHTMLTags(content)
	}

	// 创建文档
	metadata := l.GetMetadata()
	metadata["content_type"] = resp.Header().Get("Content-Type")
	metadata["content_length"] = len(body)
	metadata["status_code"] = resp.StatusCode()

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

	return []*interfaces.Document{doc}, nil
}

// LoadAndSplit 加载并分割
func (l *WebLoader) LoadAndSplit(ctx context.Context, splitter TextSplitter) ([]*interfaces.Document, error) {
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
