package search

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

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

func TestDuckDuckGoSearchEngine_New(t *testing.T) {
	engine := NewDuckDuckGoSearchEngine()

	if engine == nil {
		t.Error("Expected non-nil engine")
	}
}

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

func TestAggregatedSearchEngine_NoEngines(t *testing.T) {
	aggregated := NewAggregatedSearchEngine()

	_, err := aggregated.Search(context.Background(), "test", 5)
	if err == nil {
		t.Error("Expected error when no engines configured")
	}
}

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
