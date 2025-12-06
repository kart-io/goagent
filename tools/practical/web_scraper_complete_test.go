package practical

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
)

// TestWebScraperTool_Execute_BasicScraping 测试基本网页抓取
func TestWebScraperTool_Execute_BasicScraping(t *testing.T) {
	html := `
	<html>
		<head>
			<title>Test Page</title>
		</head>
		<body>
			<h1>Hello World</h1>
			<p>This is a test page</p>
			<a href="/page1">Link 1</a>
			<a href="/page2">Link 2</a>
		</body>
	</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})

	// 检查标题
	if title, ok := result["title"].(string); !ok || title != "Test Page" {
		t.Errorf("期望标题 'Test Page'，得到 '%v'", result["title"])
	}

	// 检查链接 - 实现返回 []string 类型
	if links, ok := result["links"].([]string); !ok || len(links) == 0 {
		t.Error("链接列表应该存在且不为空")
	}
}

// TestWebScraperTool_Execute_CustomSelectors 测试自定义选择器
func TestWebScraperTool_Execute_CustomSelectors(t *testing.T) {
	html := `
	<html>
		<head>
			<title>Test Page</title>
		</head>
		<body>
			<div class="main-content">
				<h2>Custom Title</h2>
				<p>Custom content here</p>
			</div>
		</body>
	</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
			"selectors": map[string]interface{}{
				"title":   "h2",
				"content": ".main-content",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})

	// 检查自定义标题
	if title, ok := result["title"].(string); !ok || !strings.Contains(title, "Custom Title") {
		t.Errorf("期望自定义标题，得到 '%v'", result["title"])
	}
}

// TestWebScraperTool_Execute_ExtractImages 测试图片提取
func TestWebScraperTool_Execute_ExtractImages(t *testing.T) {
	html := `
	<html>
		<body>
			<img src="image1.jpg" alt="Image 1">
			<img src="/path/to/image2.png" alt="Image 2">
			<img src="http://example.com/image3.gif" alt="Image 3">
		</body>
	</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})

	// 检查图片列表 - 实现返回 []string 类型
	images, ok := result["images"].([]string)
	if !ok || len(images) == 0 {
		t.Error("图片列表应该存在且不为空")
	}

	if len(images) != 3 {
		t.Errorf("期望 3 张图片，得到 %d", len(images))
	}
}

// TestWebScraperTool_Execute_ExtractMetadata 测试元数据提取
func TestWebScraperTool_Execute_ExtractMetadata(t *testing.T) {
	html := `
	<html>
		<head>
			<title>Test Page</title>
			<meta name="description" content="This is a test page">
			<meta name="keywords" content="test, page, meta">
			<meta property="og:title" content="OG Title">
			<meta property="og:description" content="OG Description">
			<meta property="og:image" content="og-image.jpg">
		</head>
		<body>
			<h1>Hello World</h1>
		</body>
	</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":              server.URL,
			"extract_metadata": true,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})

	// 检查元数据
	metadata, ok := result["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("元数据应该存在")
	}

	if _, ok := metadata["description"]; !ok {
		t.Error("description 元数据应该存在")
	}

	if _, ok := metadata["og:title"]; !ok {
		t.Error("og:title 元数据应该存在")
	}
}

// TestWebScraperTool_Execute_InvalidURL_Extended 测试额外的无效 URL 场景
func TestWebScraperTool_Execute_InvalidURL_Extended(t *testing.T) {
	tool := NewWebScraperTool()
	ctx := context.Background()

	tests := []struct {
		name string
		url  string
	}{
		{"FTP 协议", "ftp://example.com"},
		{"错误的 URL 格式", "htp://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{
				Args: map[string]interface{}{
					"url": tt.url,
				},
			}

			_, err := tool.Execute(ctx, input)
			if err == nil {
				t.Error("无效 URL 应该返回错误")
			}
		})
	}
}

// TestWebScraperTool_Execute_NetworkError 测试网络错误
func TestWebScraperTool_Execute_NetworkError(t *testing.T) {
	tool := NewWebScraperTool()
	ctx := context.Background()

	// 使用无效的服务器地址
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": "http://localhost:9999/nonexistent",
		},
	}

	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Error("网络错误应该返回错误")
	}
}

// TestWebScraperTool_Execute_LargeContent 测试大内容限制
func TestWebScraperTool_Execute_LargeContent(t *testing.T) {
	// 创建大内容
	largeText := strings.Repeat("x", 50000)
	html := fmt.Sprintf(`
	<html>
		<body>
			<p>%s</p>
		</body>
	</html>
	`, largeText)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":                server.URL,
			"max_content_length": 10000,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	content, ok := result["content"].(string)
	if !ok {
		t.Fatal("内容应该存在")
	}

	// 内容应该被截断（实现会添加 "..." 后缀，所以允许稍微超出）
	// 实现截断时会添加 "..." (3字符)，所以实际长度可能是 maxLength + 3
	if len(content) > 10003 {
		t.Errorf("内容应该受限制于约 10000 个字符，实际 %d", len(content))
	}
}

// TestWebScraperTool_Execute_RelativeURLs 测试相对 URL 转换
func TestWebScraperTool_Execute_RelativeURLs(t *testing.T) {
	html := `
	<html>
		<body>
			<a href="/relative/path">Relative Link</a>
			<a href="./same/dir">Same Dir Link</a>
			<a href="../parent/dir">Parent Dir Link</a>
			<a href="http://absolute.com">Absolute Link</a>
		</body>
	</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 []string 类型的 links
	links, ok := result["links"].([]string)
	if !ok {
		t.Fatal("链接列表应该存在")
	}

	// 验证相对 URL 已转换为绝对 URL
	if len(links) > 0 {
		url := links[0]

		// 应该是绝对 URL
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			t.Errorf("相对 URL 应该转换为绝对 URL，得到 %s", url)
		}
	}
}

// TestWebScraperTool_Execute_CustomSelectors_Complex 测试复杂的自定义选择器
func TestWebScraperTool_Execute_CustomSelectors_Complex(t *testing.T) {
	html := `
	<html>
		<body>
			<div class="product">
				<h3>Product 1</h3>
				<span class="price">$10.99</span>
			</div>
			<div class="product">
				<h3>Product 2</h3>
				<span class="price">$20.99</span>
			</div>
		</body>
	</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
			"selectors": map[string]interface{}{
				"custom": map[string]interface{}{
					"products": ".product h3",
					"prices":   ".price",
				},
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})

	// 检查自定义选择器结果
	custom, ok := result["custom"].(map[string]interface{})
	if !ok {
		t.Fatal("自定义选择器结果应该存在")
	}

	if _, ok := custom["products"]; !ok {
		t.Error("products 应该存在")
	}

	if _, ok := custom["prices"]; !ok {
		t.Error("prices 应该存在")
	}
}

// TestWebScraperTool_Invoke 测试 Invoke 方法
func TestWebScraperTool_Invoke(t *testing.T) {
	html := `<html><body><h1>Test</h1></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	if _, ok := result["title"]; !ok {
		t.Error("title 应该存在")
	}
}

// TestWebScraperTool_Stream 测试 Stream 方法
func TestWebScraperTool_Stream(t *testing.T) {
	html := `<html><body><h1>Stream Test</h1></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
		},
	}

	ch, err := tool.Stream(ctx, input)
	if err != nil {
		t.Errorf("创建流失败：%v", err)
		return
	}

	// 从通道读取结果
	received := false
	for chunk := range ch {
		if chunk.Error != nil {
			t.Errorf("流错误：%v", chunk.Error)
		}
		if chunk.Data != nil {
			received = true
			result := chunk.Data.Result.(map[string]interface{})
			if _, ok := result["title"]; !ok {
				t.Error("title 应该存在")
			}
		}
	}

	if !received {
		t.Error("应该从流接收数据")
	}
}

// TestWebScraperTool_Batch 测试 Batch 方法
func TestWebScraperTool_Batch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><h1>Batch Test</h1></body></html>`))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	inputs := []*interfaces.ToolInput{
		{
			Args: map[string]interface{}{
				"url": server.URL,
			},
		},
		{
			Args: map[string]interface{}{
				"url": server.URL,
			},
		},
	}

	outputs, err := tool.Batch(ctx, inputs)
	if err != nil {
		t.Errorf("批处理失败：%v", err)
		return
	}

	if len(outputs) != 2 {
		t.Errorf("期望 2 个输出，得到 %d", len(outputs))
	}

	for _, output := range outputs {
		result := output.Result.(map[string]interface{})
		if _, ok := result["title"]; !ok {
			t.Error("title 应该存在")
		}
	}
}

// TestWebScraperTool_WithCallbacks 测试 WithCallbacks 方法
func TestWebScraperTool_WithCallbacks(t *testing.T) {
	tool := NewWebScraperTool()

	// 应该返回相同的工具实例
	result := tool.WithCallbacks()
	if result != tool {
		t.Error("WithCallbacks 应该返回同一个实例")
	}
}

// TestWebScraperTool_WithConfig 测试 WithConfig 方法
func TestWebScraperTool_WithConfig(t *testing.T) {
	tool := NewWebScraperTool()

	// 应该返回相同的工具实例
	// 使用适当的 RunnableConfig 结构体
	cfg := agentcore.RunnableConfig{}
	result := tool.WithConfig(cfg)
	if result != tool {
		t.Error("WithConfig 应该返回同一个实例")
	}
}

// TestWebScraperTool_Pipe 测试 Pipe 方法
func TestWebScraperTool_Pipe(t *testing.T) {
	tool := NewWebScraperTool()

	// Pipe 返回 nil
	result := tool.Pipe(nil)
	if result != nil {
		t.Error("Pipe 应该返回 nil")
	}
}

// TestWebScraperTool_ExtractMetadata_TwitterCard 测试 Twitter Card 元数据
func TestWebScraperTool_ExtractMetadata_TwitterCard(t *testing.T) {
	html := `
	<html>
		<head>
			<meta name="twitter:card" content="summary">
			<meta name="twitter:title" content="Tweet Title">
			<meta name="twitter:description" content="Tweet Description">
			<meta name="twitter:image" content="tweet-image.jpg">
		</head>
		<body></body>
	</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"url":              server.URL,
			"extract_metadata": true,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	metadata, ok := result["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("元数据应该存在")
	}

	if _, ok := metadata["twitter:card"]; !ok {
		t.Error("twitter:card 应该存在")
	}
}

// TestWebScraperTool_Name_Extended 测试 Name 方法扩展
func TestWebScraperTool_Name_Extended(t *testing.T) {
	tool := NewWebScraperTool()
	if tool.Name() != "web_scraper" {
		t.Errorf("期望名称 'web_scraper'，得到 '%s'", tool.Name())
	}
}

// TestWebScraperTool_Description_Extended 测试 Description 方法扩展
func TestWebScraperTool_Description_Extended(t *testing.T) {
	tool := NewWebScraperTool()
	desc := tool.Description()
	if desc == "" {
		t.Error("描述应该不为空")
	}
}

// TestWebScraperTool_ArgsSchema 测试 ArgsSchema 方法
func TestWebScraperTool_ArgsSchema(t *testing.T) {
	tool := NewWebScraperTool()
	schema := tool.ArgsSchema()
	if schema == "" {
		t.Error("schema 应该不为空")
	}

	// 验证是有效的 JSON
	if !strings.Contains(schema, "url") {
		t.Error("schema 应该包含 'url' 字段")
	}
}
