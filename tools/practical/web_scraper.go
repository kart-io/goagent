package practical

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/tools"
)

// WebScraperTool scrapes web pages and extracts structured data
type WebScraperTool struct {
	httpClient *http.Client
	maxRetries int
	userAgent  string
}

// NewWebScraperTool creates a new web scraper tool
func NewWebScraperTool() *WebScraperTool {
	return &WebScraperTool{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		maxRetries: 3,
		userAgent:  "Mozilla/5.0 (compatible; AgentFramework/1.0)",
	}
}

// Name returns the tool name
func (t *WebScraperTool) Name() string {
	return "web_scraper"
}

// Description returns the tool description
func (t *WebScraperTool) Description() string {
	return "Scrapes web pages and extracts structured data including text, links, images, and metadata"
}

// ArgsSchema returns the arguments schema as a JSON string
func (t *WebScraperTool) ArgsSchema() string {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to scrape",
			},
			"selectors": map[string]interface{}{
				"type":        "object",
				"description": "CSS selectors to extract specific elements",
				"properties": map[string]interface{}{
					"title":   map[string]interface{}{"type": "string"},
					"content": map[string]interface{}{"type": "string"},
					"links":   map[string]interface{}{"type": "string"},
					"images":  map[string]interface{}{"type": "string"},
					"custom":  map[string]interface{}{"type": "object"},
				},
			},
			"extract_metadata": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to extract page metadata",
				"default":     true,
			},
			"max_content_length": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum content length to extract",
				"default":     10000,
			},
		},
		"required": []string{"url"},
	}

	schemaJSON, _ := json.Marshal(schema)
	return string(schemaJSON)
}

// Execute runs the web scraper with ToolInput/ToolOutput
func (t *WebScraperTool) Execute(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
	params, err := t.parseInput(input.Args)
	if err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	// Validate URL
	parsedURL, err := url.Parse(params.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("only HTTP(S) URLs are supported")
	}

	// Fetch the page
	doc, err := t.fetchPage(ctx, params.URL)
	if err != nil {
		return &tools.ToolOutput{
			Result: map[string]interface{}{
				"url":   params.URL,
				"error": err.Error(),
			},
			Error: err.Error(),
		}, err
	}

	// Extract data
	result := map[string]interface{}{
		"url": params.URL,
	}

	// Extract title
	if params.Selectors.Title != "" {
		result["title"] = doc.Find(params.Selectors.Title).First().Text()
	} else {
		result["title"] = doc.Find("title").First().Text()
	}

	// Extract content
	if params.Selectors.Content != "" {
		content := t.extractText(doc, params.Selectors.Content, params.MaxContentLength)
		result["content"] = content
	} else {
		// Default: extract main content areas
		content := t.extractMainContent(doc, params.MaxContentLength)
		result["content"] = content
	}

	// Extract links
	links := t.extractLinks(doc, params.Selectors.Links, params.URL)
	result["links"] = links

	// Extract images
	images := t.extractImages(doc, params.Selectors.Images, params.URL)
	result["images"] = images

	// Extract metadata if requested
	if params.ExtractMetadata {
		metadata := t.extractMetadata(doc)
		result["metadata"] = metadata
	}

	// Extract custom selectors
	if len(params.Selectors.Custom) > 0 {
		custom := t.extractCustom(doc, params.Selectors.Custom)
		result["custom"] = custom
	}

	return &tools.ToolOutput{
		Result: result,
	}, nil
}

// Implement Runnable interface
func (t *WebScraperTool) Invoke(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
	return t.Execute(ctx, input)
}

func (t *WebScraperTool) Stream(ctx context.Context, input *tools.ToolInput) (<-chan agentcore.StreamChunk[*tools.ToolOutput], error) {
	ch := make(chan agentcore.StreamChunk[*tools.ToolOutput])
	go func() {
		defer close(ch)
		output, err := t.Execute(ctx, input)
		if err != nil {
			ch <- agentcore.StreamChunk[*tools.ToolOutput]{Error: err}
		} else {
			ch <- agentcore.StreamChunk[*tools.ToolOutput]{Data: output}
		}
	}()
	return ch, nil
}

func (t *WebScraperTool) Batch(ctx context.Context, inputs []*tools.ToolInput) ([]*tools.ToolOutput, error) {
	outputs := make([]*tools.ToolOutput, len(inputs))
	for i, input := range inputs {
		output, err := t.Execute(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

func (t *WebScraperTool) Pipe(next agentcore.Runnable[*tools.ToolOutput, any]) agentcore.Runnable[*tools.ToolInput, any] {
	return nil
}

func (t *WebScraperTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*tools.ToolInput, *tools.ToolOutput] {
	return t
}

func (t *WebScraperTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*tools.ToolInput, *tools.ToolOutput] {
	return t
}

// Helper types and methods

type webScraperParams struct {
	URL              string
	Selectors        scraperSelectors
	ExtractMetadata  bool
	MaxContentLength int
}

type scraperSelectors struct {
	Title   string
	Content string
	Links   string
	Images  string
	Custom  map[string]string
}

func (t *WebScraperTool) parseInput(input interface{}) (*webScraperParams, error) {
	params := &webScraperParams{
		ExtractMetadata:  true,
		MaxContentLength: 10000,
	}

	switch v := input.(type) {
	case map[string]interface{}:
		// Parse URL
		if url, ok := v["url"].(string); ok {
			params.URL = url
		} else {
			return nil, fmt.Errorf("url is required")
		}

		// Parse selectors
		if selectors, ok := v["selectors"].(map[string]interface{}); ok {
			if title, ok := selectors["title"].(string); ok {
				params.Selectors.Title = title
			}
			if content, ok := selectors["content"].(string); ok {
				params.Selectors.Content = content
			}
			if links, ok := selectors["links"].(string); ok {
				params.Selectors.Links = links
			}
			if images, ok := selectors["images"].(string); ok {
				params.Selectors.Images = images
			}
			if custom, ok := selectors["custom"].(map[string]interface{}); ok {
				params.Selectors.Custom = make(map[string]string)
				for k, v := range custom {
					if str, ok := v.(string); ok {
						params.Selectors.Custom[k] = str
					}
				}
			}
		}

		// Parse other options
		if extractMeta, ok := v["extract_metadata"].(bool); ok {
			params.ExtractMetadata = extractMeta
		}
		if maxLen, ok := v["max_content_length"].(float64); ok {
			params.MaxContentLength = int(maxLen)
		}
	case string:
		// Simple URL input
		params.URL = v
	default:
		return nil, fmt.Errorf("unsupported input type: %T", input)
	}

	return params, nil
}

func (t *WebScraperTool) fetchPage(ctx context.Context, urlStr string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", t.userAgent)

	var resp *http.Response
	var lastErr error

	for i := 0; i < t.maxRetries; i++ {
		resp, lastErr = t.httpClient.Do(req)
		if lastErr == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	defer resp.Body.Close()
	return goquery.NewDocumentFromReader(resp.Body)
}

func (t *WebScraperTool) extractText(doc *goquery.Document, selector string, maxLength int) string {
	var texts []string
	totalLength := 0

	doc.Find(selector).Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text != "" && totalLength < maxLength {
			remainingLength := maxLength - totalLength
			if len(text) > remainingLength {
				text = text[:remainingLength] + "..."
			}
			texts = append(texts, text)
			totalLength += len(text)
		}
	})

	return strings.Join(texts, "\n\n")
}

func (t *WebScraperTool) extractMainContent(doc *goquery.Document, maxLength int) string {
	// Try common content selectors
	selectors := []string{
		"main", "article", "[role='main']", ".content", "#content",
		".post", ".entry-content", ".article-body",
	}

	for _, selector := range selectors {
		content := t.extractText(doc, selector, maxLength)
		if len(content) > 100 { // Minimum content threshold
			return content
		}
	}

	// Fallback to body text
	return t.extractText(doc, "body", maxLength)
}

func (t *WebScraperTool) extractLinks(doc *goquery.Document, selector string, baseURL string) []string {
	if selector == "" {
		selector = "a[href]"
	}

	var links []string
	seen := make(map[string]bool)

	doc.Find(selector).Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if exists {
			// Resolve relative URLs
			if !strings.HasPrefix(href, "http") && baseURL != "" {
				if base, err := url.Parse(baseURL); err == nil {
					if resolved, err := base.Parse(href); err == nil {
						href = resolved.String()
					}
				}
			}

			if !seen[href] {
				links = append(links, href)
				seen[href] = true
			}
		}
	})

	return links
}

func (t *WebScraperTool) extractImages(doc *goquery.Document, selector string, baseURL string) []string {
	if selector == "" {
		selector = "img[src]"
	}

	var images []string
	seen := make(map[string]bool)

	doc.Find(selector).Each(func(i int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if exists {
			// Resolve relative URLs
			if !strings.HasPrefix(src, "http") && baseURL != "" {
				if base, err := url.Parse(baseURL); err == nil {
					if resolved, err := base.Parse(src); err == nil {
						src = resolved.String()
					}
				}
			}

			if !seen[src] {
				images = append(images, src)
				seen[src] = true
			}
		}
	})

	return images
}

func (t *WebScraperTool) extractMetadata(doc *goquery.Document) map[string]interface{} {
	metadata := make(map[string]interface{})

	// Extract meta tags
	doc.Find("meta").Each(func(i int, s *goquery.Selection) {
		if name, exists := s.Attr("name"); exists {
			if content, exists := s.Attr("content"); exists {
				metadata[name] = content
			}
		}
		if property, exists := s.Attr("property"); exists {
			if content, exists := s.Attr("content"); exists {
				metadata[property] = content
			}
		}
	})

	// Extract structured data (JSON-LD)
	doc.Find("script[type='application/ld+json']").Each(func(i int, s *goquery.Selection) {
		var jsonData interface{}
		if err := json.Unmarshal([]byte(s.Text()), &jsonData); err == nil {
			metadata["structured_data"] = jsonData
		}
	})

	return metadata
}

func (t *WebScraperTool) extractCustom(doc *goquery.Document, selectors map[string]string) map[string]interface{} {
	custom := make(map[string]interface{})

	for key, selector := range selectors {
		var values []string
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if text != "" {
				values = append(values, text)
			}
		})

		if len(values) == 1 {
			custom[key] = values[0]
		} else if len(values) > 1 {
			custom[key] = values
		}
	}

	return custom
}
