// Package search 提供搜索工具的测试
// 本文件测试 SearchTool 搜索工具的功能
package search

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

// TestNewSearchTool 测试创建搜索工具
func TestNewSearchTool(t *testing.T) {
	engine := NewMockSearchEngine()
	tool := NewSearchTool(engine)

	if tool.Name() != "search" {
		t.Errorf("Expected name 'search', got: %s", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Expected non-empty description")
	}

	if tool.ArgsSchema() == "" {
		t.Error("Expected non-empty args schema")
	}
}

// TestSearchTool_Run_Success 测试成功执行搜索
func TestSearchTool_Run_Success(t *testing.T) {
	engine := NewMockSearchEngine()
	engine.AddResponse("golang", []SearchResult{
		{
			Title:   "Go Programming",
			URL:     "https://golang.org",
			Snippet: "The Go programming language",
			Source:  "golang.org",
			Score:   1.0,
		},
	})

	tool := NewSearchTool(engine)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"query":       "golang",
			"max_results": float64(5),
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !output.Success {
		t.Error("Expected successful output")
	}

	results, ok := output.Result.([]SearchResult)
	if !ok {
		t.Error("Expected result to be []SearchResult")
	}

	if len(results) == 0 {
		t.Error("Expected at least one result")
	}
}

// TestSearchTool_Run_EmptyQuery 测试空查询字符串的错误处理
func TestSearchTool_Run_EmptyQuery(t *testing.T) {
	engine := NewMockSearchEngine()
	tool := NewSearchTool(engine)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"query": "",
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err == nil {
		t.Error("Expected error for empty query")
	}

	if output.Success {
		t.Error("Expected unsuccessful output for empty query")
	}
}

// TestSearchTool_Run_NoQuery 测试缺少查询参数的错误处理
func TestSearchTool_Run_NoQuery(t *testing.T) {
	engine := NewMockSearchEngine()
	tool := NewSearchTool(engine)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{},
	}

	output, err := tool.Invoke(ctx, input)
	if err == nil {
		t.Error("Expected error when query is missing")
	}

	if output.Success {
		t.Error("Expected unsuccessful output")
	}
}

// TestMockSearchEngine_Search 测试模拟搜索引擎的搜索功能
func TestMockSearchEngine_Search(t *testing.T) {
	engine := NewMockSearchEngine()

	results, err := engine.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected at least one result")
	}

	// Check default results
	if results[0].Title == "" {
		t.Error("Expected result to have a title")
	}
	if results[0].URL == "" {
		t.Error("Expected result to have a URL")
	}
}

// TestMockSearchEngine_AddResponse 测试添加自定义搜索响应
func TestMockSearchEngine_AddResponse(t *testing.T) {
	engine := NewMockSearchEngine()
	customResults := []SearchResult{
		{
			Title:       "Custom Result",
			URL:         "https://example.com",
			Snippet:     "Custom snippet",
			Source:      "example.com",
			PublishDate: time.Now(),
			Score:       0.9,
		},
	}

	engine.AddResponse("custom query", customResults)
	results, err := engine.Search(context.Background(), "custom query", 10)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got: %d", len(results))
	}

	if results[0].Title != "Custom Result" {
		t.Errorf("Expected custom result title, got: %s", results[0].Title)
	}
}

// TestMockSearchEngine_MaxResults 测试最大结果数限制
func TestMockSearchEngine_MaxResults(t *testing.T) {
	engine := NewMockSearchEngine()
	results := make([]SearchResult, 10)
	for i := 0; i < 10; i++ {
		results[i] = SearchResult{
			Title: "Result",
			URL:   "https://example.com",
		}
	}

	engine.AddResponse("test", results)

	// Request only 5 results
	searchResults, err := engine.Search(context.Background(), "test", 5)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if len(searchResults) != 5 {
		t.Errorf("Expected 5 results, got: %d", len(searchResults))
	}
}

// TestMockSearchEngine_CaseInsensitive 测试大小写不敏感搜索
func TestMockSearchEngine_CaseInsensitive(t *testing.T) {
	engine := NewMockSearchEngine()
	customResults := []SearchResult{
		{Title: "Test Result", URL: "https://test.com"},
	}

	engine.AddResponse("Test Query", customResults)

	// Search with different case
	results, err := engine.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got: %d", len(results))
	}
}

// TestGoogleSearchEngine_New 测试创建 Google 搜索引擎
func TestGoogleSearchEngine_New(t *testing.T) {
	engine := NewGoogleSearchEngine("api-key", "cx-id")

	if engine == nil {
		t.Error("Expected non-nil engine")
	}

	if engine.apiKey != "api-key" {
		t.Error("Expected API key to be set")
	}

	if engine.cx != "cx-id" {
		t.Error("Expected CX to be set")
	}
}

// TestGoogleSearchEngine_Search 测试 Google 搜索引擎的搜索功能
func TestGoogleSearchEngine_Search(t *testing.T) {
	engine := NewGoogleSearchEngine("test-key", "test-cx")

	results, err := engine.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected at least one mock result")
	}

	if results[0].Source != "google.com" {
		t.Errorf("Expected source to be google.com, got: %s", results[0].Source)
	}
}

// TestDuckDuckGoSearchEngine_New 测试创建 DuckDuckGo 搜索引擎
func TestDuckDuckGoSearchEngine_New(t *testing.T) {
	engine := NewDuckDuckGoSearchEngine()

	if engine == nil {
		t.Error("Expected non-nil engine")
	}
}

// TestDuckDuckGoSearchEngine_Search 测试 DuckDuckGo 搜索引擎的搜索功能
func TestDuckDuckGoSearchEngine_Search(t *testing.T) {
	engine := NewDuckDuckGoSearchEngine()

	results, err := engine.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected at least one mock result")
	}

	if results[0].Source != "duckduckgo.com" {
		t.Errorf("Expected source to be duckduckgo.com, got: %s", results[0].Source)
	}
}

// TestAggregatedSearchEngine_New 测试创建聚合搜索引擎
func TestAggregatedSearchEngine_New(t *testing.T) {
	engine1 := NewMockSearchEngine()
	engine2 := NewMockSearchEngine()

	aggregated := NewAggregatedSearchEngine(engine1, engine2)

	if aggregated == nil {
		t.Error("Expected non-nil aggregated engine")
	}

	if len(aggregated.engines) != 2 {
		t.Errorf("Expected 2 engines, got: %d", len(aggregated.engines))
	}
}

// TestAggregatedSearchEngine_Search 测试聚合搜索引擎的搜索功能
func TestAggregatedSearchEngine_Search(t *testing.T) {
	engine1 := NewMockSearchEngine()
	engine1.AddResponse("test", []SearchResult{
		{Title: "Result 1", URL: "https://example1.com", Score: 0.9},
	})

	engine2 := NewMockSearchEngine()
	engine2.AddResponse("test", []SearchResult{
		{Title: "Result 2", URL: "https://example2.com", Score: 0.8},
	})

	aggregated := NewAggregatedSearchEngine(engine1, engine2)

	results, err := aggregated.Search(context.Background(), "test", 10)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got: %d", len(results))
	}
}

// TestAggregatedSearchEngine_NoEngines 测试没有配置搜索引擎时的错误处理
func TestAggregatedSearchEngine_NoEngines(t *testing.T) {
	aggregated := NewAggregatedSearchEngine()

	_, err := aggregated.Search(context.Background(), "test", 5)
	if err == nil {
		t.Error("Expected error when no engines configured")
	}
}

// TestAggregatedSearchEngine_Deduplication 测试搜索结果去重功能
func TestAggregatedSearchEngine_Deduplication(t *testing.T) {
	engine1 := NewMockSearchEngine()
	engine1.AddResponse("test", []SearchResult{
		{Title: "Result", URL: "https://example.com", Score: 0.9},
	})

	engine2 := NewMockSearchEngine()
	engine2.AddResponse("test", []SearchResult{
		{Title: "Result", URL: "https://example.com", Score: 0.8},
	})

	aggregated := NewAggregatedSearchEngine(engine1, engine2)

	results, err := aggregated.Search(context.Background(), "test", 10)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 deduplicated result, got: %d", len(results))
	}
}

// TestAggregatedSearchEngine_MaxResults 测试聚合搜索引擎的最大结果数限制
func TestAggregatedSearchEngine_MaxResults(t *testing.T) {
	engine1 := NewMockSearchEngine()
	engine1.AddResponse("test", []SearchResult{
		{Title: "R1", URL: "https://ex1.com", Score: 0.9},
		{Title: "R2", URL: "https://ex2.com", Score: 0.8},
		{Title: "R3", URL: "https://ex3.com", Score: 0.7},
	})

	aggregated := NewAggregatedSearchEngine(engine1)

	results, err := aggregated.Search(context.Background(), "test", 2)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results (limited by max_results), got: %d", len(results))
	}
}

// TestDeduplicateResults 测试搜索结果去重辅助函数
func TestDeduplicateResults(t *testing.T) {
	results := []SearchResult{
		{URL: "https://example.com/1"},
		{URL: "https://example.com/2"},
		{URL: "https://example.com/1"}, // Duplicate
		{URL: "https://example.com/3"},
	}

	unique := deduplicateResults(results)

	if len(unique) != 3 {
		t.Errorf("Expected 3 unique results, got: %d", len(unique))
	}
}

// TestSortResultsByScore 测试按分数排序搜索结果
func TestSortResultsByScore(t *testing.T) {
	results := []SearchResult{
		{URL: "a", Score: 0.5},
		{URL: "b", Score: 0.9},
		{URL: "c", Score: 0.7},
	}

	sorted := sortResultsByScore(results)

	if sorted[0].Score != 0.9 {
		t.Errorf("Expected highest score first, got: %f", sorted[0].Score)
	}

	if sorted[2].Score != 0.5 {
		t.Errorf("Expected lowest score last, got: %f", sorted[2].Score)
	}
}

// TestAggregatedSearchEngine_ContextCancellation 测试上下文取消时的处理
func TestAggregatedSearchEngine_ContextCancellation(t *testing.T) {
	engine := NewMockSearchEngine()
	aggregated := NewAggregatedSearchEngine(engine)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := aggregated.Search(ctx, "test", 5)
	if err == nil {
		t.Error("Expected error due to context cancellation")
	}
}

// TestSearchTool_Metadata 测试搜索工具的元数据
func TestSearchTool_Metadata(t *testing.T) {
	engine := NewMockSearchEngine()
	tool := NewSearchTool(engine)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"query":       "test",
			"max_results": float64(10),
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if output.Metadata == nil {
		t.Error("Expected metadata to be present")
	}

	if output.Metadata["query"] != "test" {
		t.Error("Expected query in metadata")
	}

	if output.Metadata["max_results"] != 10 {
		t.Error("Expected max_results in metadata")
	}
}
