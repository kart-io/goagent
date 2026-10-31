# 04-search 搜索工具示例

演示 `tools/search` 中的 `SearchTool` 与几种搜索引擎实现。全部使用本地模拟数据，不访问网络，也不调用 LLM。

## 运行

```bash
go run ./examples/tools/04-search
```

## 内容

1. **模拟引擎**：`search.NewMockSearchEngine()`，用 `AddResponse(query, []search.SearchResult{...})` 预置 `golang` 与 `ai agent` 的结果；未预置的查询返回通用模拟结果。
2. **创建工具**：`search.NewSearchTool(engine)`，工具名 `search`。
3. **执行搜索**：参数 `query` 与 `max_results`，`output.Result` 为 `[]search.SearchResult`（`Title`、`URL`、`Snippet`、`Score` 等）。
4. **其他引擎**：`search.NewGoogleSearchEngine(apiKey, cx)` 与 `search.NewDuckDuckGoSearchEngine()`。两者目前都是返回固定结果的模拟实现，示例只创建工具不搜索。
5. **聚合搜索**：`search.NewAggregatedSearchEngine(engine1, engine2)` 并发查询多个引擎，按 URL 去重后按 `Score` 降序排列。
6. **元数据**：`output.Metadata` 中的 `query`、`result_count`、`max_results`。

```go
engine := search.NewMockSearchEngine()
engine.AddResponse("golang", []search.SearchResult{
    {Title: "Go 编程语言官网", URL: "https://golang.org", Score: 0.98},
})
searchTool := search.NewSearchTool(engine)
output, err := searchTool.Invoke(ctx, &interfaces.ToolInput{
    Args:    map[string]interface{}{"query": "golang", "max_results": float64(5)},
    Context: ctx,
})
```
