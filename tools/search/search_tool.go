package search

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kart-io/goagent/tools"
)

// SearchTool 搜索工具
//
// 提供模拟搜索功能（实际项目中应集成真实搜索 API）
type SearchTool struct {
	*tools.BaseTool
	searchEngine SearchEngine
}

// SearchEngine 搜索引擎接口
type SearchEngine interface {
	Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
}

// SearchResult 搜索结果
type SearchResult struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Snippet     string    `json:"snippet"`
	Source      string    `json:"source"`
	PublishDate time.Time `json:"publish_date,omitempty"`
	Score       float64   `json:"score,omitempty"`
}

// NewSearchTool 创建搜索工具
func NewSearchTool(engine SearchEngine) *SearchTool {
	tool := &SearchTool{
		searchEngine: engine,
	}

	tool.BaseTool = tools.NewBaseTool(
		"search",
		"Search the web for information. Returns top search results with titles, URLs, and snippets.",
		`{
			"type": "object",
			"properties": {
				"query": {
					"type": "string",
					"description": "Search query string"
				},
				"max_results": {
					"type": "integer",
					"description": "Maximum number of results to return (default: 5)",
					"default": 5
				}
			},
			"required": ["query"]
		}`,
		tool.run,
	)
	return tool
}

// run 执行搜索
func (s *SearchTool) run(ctx context.Context, input *tools.ToolInput) (*tools.ToolOutput, error) {
	query, ok := input.Args["query"].(string)
	if !ok || query == "" {
		return &tools.ToolOutput{
			Success: false,
			Error:   "query is required and must be a non-empty string",
		}, tools.NewToolError(s.Name(), "invalid input", fmt.Errorf("query is required"))
	}

	maxResults := 5
	if mr, ok := input.Args["max_results"].(float64); ok {
		maxResults = int(mr)
	}

	// 执行搜索
	results, err := s.searchEngine.Search(ctx, query, maxResults)
	if err != nil {
		return &tools.ToolOutput{
			Success: false,
			Error:   err.Error(),
		}, tools.NewToolError(s.Name(), "search failed", err)
	}

	return &tools.ToolOutput{
		Result:  results,
		Success: true,
		Metadata: map[string]interface{}{
			"query":        query,
			"result_count": len(results),
			"max_results":  maxResults,
		},
	}, nil
}

// MockSearchEngine 模拟搜索引擎
//
// 用于测试和演示
type MockSearchEngine struct {
	responses map[string][]SearchResult
}

// NewMockSearchEngine 创建模拟搜索引擎
func NewMockSearchEngine() *MockSearchEngine {
	return &MockSearchEngine{
		responses: make(map[string][]SearchResult),
	}
}

// AddResponse 添加模拟响应
func (m *MockSearchEngine) AddResponse(query string, results []SearchResult) {
	m.responses[strings.ToLower(query)] = results
}

// Search 执行模拟搜索
func (m *MockSearchEngine) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	query = strings.ToLower(query)

	// 检查是否有预设响应
	if results, ok := m.responses[query]; ok {
		if len(results) > maxResults {
			return results[:maxResults], nil
		}
		return results, nil
	}

	// 返回通用模拟结果
	return []SearchResult{
		{
			Title:       fmt.Sprintf("Result for '%s' - Article 1", query),
			URL:         fmt.Sprintf("https://example.com/article1?q=%s", url.QueryEscape(query)),
			Snippet:     fmt.Sprintf("This is a mock search result for query: %s. This article provides detailed information...", query),
			Source:      "example.com",
			PublishDate: time.Now().AddDate(0, -1, 0),
			Score:       0.95,
		},
		{
			Title:       fmt.Sprintf("Result for '%s' - Article 2", query),
			URL:         fmt.Sprintf("https://example.org/article2?q=%s", url.QueryEscape(query)),
			Snippet:     fmt.Sprintf("Another relevant result about %s with comprehensive coverage...", query),
			Source:      "example.org",
			PublishDate: time.Now().AddDate(0, -2, 0),
			Score:       0.87,
		},
	}, nil
}

// GoogleSearchEngine Google 搜索引擎适配器
//
// 实际项目中应集成 Google Custom Search API
type GoogleSearchEngine struct {
	apiKey string
	cx     string // Custom Search Engine ID
}

// NewGoogleSearchEngine 创建 Google 搜索引擎
func NewGoogleSearchEngine(apiKey, cx string) *GoogleSearchEngine {
	return &GoogleSearchEngine{
		apiKey: apiKey,
		cx:     cx,
	}
}

// Search 执行 Google 搜索
func (g *GoogleSearchEngine) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	// TODO: 实现真实的 Google Custom Search API 调用
	// 这里返回模拟结果
	return []SearchResult{
		{
			Title:   "Google Search Result",
			URL:     "https://www.google.com/search?q=" + url.QueryEscape(query),
			Snippet: "This would be a real Google search result",
			Source:  "google.com",
			Score:   1.0,
		},
	}, nil
}

// DuckDuckGoSearchEngine DuckDuckGo 搜索引擎适配器
type DuckDuckGoSearchEngine struct{}

// NewDuckDuckGoSearchEngine 创建 DuckDuckGo 搜索引擎
func NewDuckDuckGoSearchEngine() *DuckDuckGoSearchEngine {
	return &DuckDuckGoSearchEngine{}
}

// Search 执行 DuckDuckGo 搜索
func (d *DuckDuckGoSearchEngine) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	// TODO: 实现真实的 DuckDuckGo API 调用
	return []SearchResult{
		{
			Title:   "DuckDuckGo Search Result",
			URL:     "https://duckduckgo.com/?q=" + url.QueryEscape(query),
			Snippet: "This would be a real DuckDuckGo search result",
			Source:  "duckduckgo.com",
			Score:   1.0,
		},
	}, nil
}

// AggregatedSearchEngine 聚合搜索引擎
//
// 从多个搜索引擎聚合结果
type AggregatedSearchEngine struct {
	engines []SearchEngine
}

// NewAggregatedSearchEngine 创建聚合搜索引擎
func NewAggregatedSearchEngine(engines ...SearchEngine) *AggregatedSearchEngine {
	return &AggregatedSearchEngine{
		engines: engines,
	}
}

// Search 执行聚合搜索
func (a *AggregatedSearchEngine) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if len(a.engines) == 0 {
		return nil, fmt.Errorf("no search engines configured")
	}

	resultsChan := make(chan []SearchResult, len(a.engines))
	errorsChan := make(chan error, len(a.engines))

	// 并发查询所有搜索引擎
	for _, engine := range a.engines {
		go func(e SearchEngine) {
			results, err := e.Search(ctx, query, maxResults)
			if err != nil {
				errorsChan <- err
				return
			}
			resultsChan <- results
		}(engine)
	}

	// 收集结果
	var allResults []SearchResult
	var errors []error

	for i := 0; i < len(a.engines); i++ {
		select {
		case results := <-resultsChan:
			allResults = append(allResults, results...)
		case err := <-errorsChan:
			errors = append(errors, err)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// 如果所有引擎都失败，返回错误
	if len(allResults) == 0 && len(errors) > 0 {
		return nil, fmt.Errorf("all search engines failed: %v", errors)
	}

	// 去重和排序
	allResults = deduplicateResults(allResults)
	allResults = sortResultsByScore(allResults)

	// 限制结果数量
	if len(allResults) > maxResults {
		allResults = allResults[:maxResults]
	}

	return allResults, nil
}

// deduplicateResults 去除重复结果
func deduplicateResults(results []SearchResult) []SearchResult {
	seen := make(map[string]bool)
	unique := make([]SearchResult, 0, len(results))

	for _, result := range results {
		if !seen[result.URL] {
			seen[result.URL] = true
			unique = append(unique, result)
		}
	}

	return unique
}

// sortResultsByScore 按分数排序结果
func sortResultsByScore(results []SearchResult) []SearchResult {
	// 简单的冒泡排序（实际项目中应使用 sort.Slice）
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	return results
}
