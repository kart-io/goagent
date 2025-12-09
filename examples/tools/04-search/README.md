# 04-search 搜索工具示例

本示例演示 `SearchTool` 的使用方法，展示搜索引擎的创建、搜索查询执行和聚合搜索功能。

## 目录

- [架构设计](#架构设计)
- [核心组件](#核心组件)
- [执行流程](#执行流程)
- [使用方法](#使用方法)
- [代码结构](#代码结构)

## 架构设计

### 搜索工具架构

```mermaid
graph TB
    subgraph SearchTool["搜索工具"]
        Tool["SearchTool"]
    end

    subgraph Engines["搜索引擎"]
        Mock["MockSearchEngine<br/>模拟引擎"]
        Google["GoogleSearchEngine<br/>Google 搜索"]
        DDG["DuckDuckGoSearchEngine<br/>DuckDuckGo"]
        Aggregated["AggregatedSearchEngine<br/>聚合引擎"]
    end

    subgraph Results["搜索结果"]
        Title["标题"]
        URL["URL"]
        Snippet["摘要"]
        Score["评分"]
        Source["来源"]
        Date["发布日期"]
    end

    Tool --> Engines
    Engines --> Results
    Aggregated --> Google
    Aggregated --> DDG

    style Tool fill:#c8e6c9
    style Aggregated fill:#e3f2fd
```

### 类图

```mermaid
classDiagram
    class SearchEngine {
        <<interface>>
        +Search(ctx, query, maxResults) []SearchResult
    }

    class SearchTool {
        -engine SearchEngine
        +Name() string
        +Description() string
        +Invoke(ctx, input) ToolOutput
    }

    class SearchResult {
        +Title string
        +URL string
        +Snippet string
        +Source string
        +PublishDate time.Time
        +Score float64
    }

    class MockSearchEngine {
        -responses map[string][]SearchResult
        +AddResponse(query, results)
        +Search(ctx, query, maxResults) []SearchResult
    }

    class AggregatedSearchEngine {
        -engines []SearchEngine
        +Search(ctx, query, maxResults) []SearchResult
        -mergeAndSort(results) []SearchResult
    }

    SearchEngine <|.. MockSearchEngine : 实现
    SearchEngine <|.. AggregatedSearchEngine : 实现
    SearchTool --> SearchEngine : 使用
    SearchTool --> SearchResult : 返回
    AggregatedSearchEngine --> SearchEngine : 聚合
```

## 核心组件

### 1. 搜索引擎类型

| 引擎 | 说明 | 适用场景 |
|------|------|----------|
| `MockSearchEngine` | 模拟搜索引擎 | 测试和演示 |
| `GoogleSearchEngine` | Google 搜索 | 生产环境 |
| `DuckDuckGoSearchEngine` | DuckDuckGo 搜索 | 隐私优先 |
| `AggregatedSearchEngine` | 聚合搜索引擎 | 多源合并 |

### 2. 搜索结果字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `Title` | string | 结果标题 |
| `URL` | string | 结果链接 |
| `Snippet` | string | 内容摘要 |
| `Source` | string | 来源网站 |
| `PublishDate` | time.Time | 发布日期 |
| `Score` | float64 | 相关性评分 |

### 3. 聚合搜索特性

```mermaid
flowchart LR
    Query["搜索查询"] --> Engine1["引擎 1"]
    Query --> Engine2["引擎 2"]
    Query --> Engine3["引擎 N"]

    Engine1 --> Merge["合并结果"]
    Engine2 --> Merge
    Engine3 --> Merge

    Merge --> Dedup["去重"]
    Dedup --> Sort["按评分排序"]
    Sort --> Result["最终结果"]

    style Query fill:#e3f2fd
    style Result fill:#c8e6c9
```

## 执行流程

### 搜索执行流程

```mermaid
sequenceDiagram
    participant User as 用户
    participant Tool as SearchTool
    participant Engine as SearchEngine
    participant Source as 数据源

    User->>Tool: Invoke(query: "golang", max_results: 5)
    Tool->>Engine: Search(ctx, "golang", 5)
    Engine->>Source: 执行搜索
    Source-->>Engine: 原始结果
    Engine->>Engine: 解析和评分
    Engine-->>Tool: []SearchResult
    Tool->>Tool: 添加元数据
    Tool-->>User: ToolOutput{Result: results}
```

### 聚合搜索流程

```mermaid
sequenceDiagram
    participant Tool as SearchTool
    participant Agg as AggregatedEngine
    participant E1 as Engine 1
    participant E2 as Engine 2

    Tool->>Agg: Search("golang", 5)

    par 并行搜索
        Agg->>E1: Search("golang", 5)
        E1-->>Agg: Results 1
    and
        Agg->>E2: Search("golang", 5)
        E2-->>Agg: Results 2
    end

    Agg->>Agg: 合并结果
    Agg->>Agg: 去重
    Agg->>Agg: 按评分排序
    Agg-->>Tool: Merged Results
```

## 使用方法

### 运行示例

```bash
cd examples/tools/04-search
go run main.go
```

### 预期输出

```text
╔════════════════════════════════════════════════════════════════╗
║              搜索工具 (SearchTool) 示例                        ║
╚════════════════════════════════════════════════════════════════╝

【步骤 1】创建模拟搜索引擎
────────────────────────────────────────
✓ 模拟搜索引擎创建成功

【步骤 3】执行搜索
────────────────────────────────────────
搜索: 'golang' (最多 5 条结果)
✓ 搜索成功
  找到 3 条结果:
  1. Go 编程语言官网
     URL: https://golang.org
     评分: 0.98

【步骤 5】聚合搜索
────────────────────────────────────────
✓ 聚合搜索成功
  合并并排序后得到 4 条结果
```

### 关键代码片段

#### 创建模拟搜索引擎

```go
import "github.com/kart-io/goagent/tools/search"

mockEngine := search.NewMockSearchEngine()

// 添加预设响应
mockEngine.AddResponse("golang", []search.SearchResult{
    {
        Title:   "Go 编程语言官网",
        URL:     "https://golang.org",
        Snippet: "Go 是一门开源的编程语言...",
        Score:   0.98,
    },
})
```

#### 创建搜索工具

```go
searchTool := search.NewSearchTool(mockEngine)

output, err := searchTool.Invoke(ctx, &interfaces.ToolInput{
    Args: map[string]interface{}{
        "query":       "golang",
        "max_results": float64(5),
    },
    Context: ctx,
})
```

#### 使用聚合搜索

```go
engine1 := search.NewMockSearchEngine()
engine2 := search.NewMockSearchEngine()

// 创建聚合搜索引擎
aggregatedEngine := search.NewAggregatedSearchEngine(engine1, engine2)
aggregatedTool := search.NewSearchTool(aggregatedEngine)
```

#### 使用 Google 搜索引擎

```go
googleEngine := search.NewGoogleSearchEngine("your-api-key", "your-cx")
googleTool := search.NewSearchTool(googleEngine)
```

## 代码结构

```text
04-search/
├── main.go          # 示例入口
└── README.md        # 本文档
```

## 生产环境提示

- 需要集成真实的搜索 API（如 Google Custom Search）
- 注意 API 调用限制和费用
- 考虑添加搜索结果缓存
- 实现搜索结果过滤和排序

## 扩展阅读

- [06-web-scraper](../06-web-scraper/) - 网页抓取工具示例
- [tools/search 包](../../../tools/search/) - 搜索工具实现
