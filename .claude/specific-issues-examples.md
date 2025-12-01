# goagent 代码审查 - 具体问题示例

## 问题 1: 英文注释违规

### 文件: `interfaces/reasoning.go` (行 7-15)

当前代码（违规）:
```go
// ReasoningPattern defines the interface for different reasoning strategies.
//
// This interface allows agents to implement various reasoning patterns such as:
// - Chain-of-Thought (CoT): Linear step-by-step reasoning
// - Tree-of-Thought (ToT): Tree-based search with multiple reasoning paths
// - Graph-of-Thought (GoT): Graph-based reasoning with complex dependencies
// - Program-of-Thought (PoT): Code generation and execution for reasoning
// - Skeleton-of-Thought (SoT): Parallel reasoning with skeleton structure
// - Meta-CoT/Self-Ask: Self-questioning and meta-reasoning
type ReasoningPattern interface {
	// Name returns the name of the reasoning pattern.
	Name() string

	// Description returns a description of the reasoning pattern.
	Description() string
```

修正代码（符合规范）:
```go
// ReasoningPattern 定义推理策略接口
//
// 此接口允许 Agent 实现各种推理模式，如：
// - 链式思考 (CoT): 线性逐步推理
// - 思维树搜索 (ToT): 多路径树形搜索
// - 思维图推理 (GoT): 复杂依赖关系的图形推理
// - 程序化思考 (PoT): 代码生成和执行推理
// - 骨架化思考 (SoT): 并行推理与骨架结构
// - 元认知推理 (Meta-CoT/Self-Ask): 自我质疑和元推理
type ReasoningPattern interface {
	// Name 返回推理策略的名称
	Name() string

	// Description 返回推理策略的描述
	Description() string
```

### 文件: `core/plugin_bridge.go` (行 1-3)

当前代码（违规）:
```go
// Package core provides plugin bridge utilities for dynamic component loading.
//
// This file addresses the fundamental tension between:
```

修正代码：
```go
// Package core 提供动态组件加载的插件桥接工具
//
// 此文件解决以下核心矛盾：
```

---

## 问题 2: TODO 标记未完成

### 文件: `tools/search/search_tool.go`

当前代码（未完成）:
```go
// GoogleSearchTool 谷歌搜索工具
type GoogleSearchTool struct {
	apiKey    string
	searchID  string
	httpClient *http.Client
}

// Search 执行搜索
func (s *GoogleSearchTool) Search(ctx context.Context, query string) ([]SearchResult, error) {
	// TODO: 实现真实的 Google Custom Search API 调用
	return nil, errors.New("not implemented")
}

// DuckDuckGoTool DuckDuckGo 搜索工具
func (d *DuckDuckGoTool) Search(ctx context.Context, query string) ([]SearchResult, error) {
	// TODO: 实现真实的 DuckDuckGo API 调用
	return nil, errors.New("not implemented")
}
```

需要选择以下之一：

**选项A: 完成实现** (推荐用于 v1.0)
```go
func (s *GoogleSearchTool) Search(ctx context.Context, query string) ([]SearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://www.googleapis.com/customsearch/v1?q=%s&key=%s&cx=%s",
			url.QueryEscape(query), s.apiKey, s.searchID),
		nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()
	
	// 解析响应...
	return results, nil
}
```

**选项B: 标记为 Future** (用于 MVP 阶段)
```go
// GoogleSearchTool 谷歌搜索工具
//
// 注意: 此工具当前为 Mock 实现，生产使用需要完成以下步骤：
//   1. 配置 Google Custom Search API
//   2. 实现真实 API 调用 (搜索关键词: GoogleSearchTool.Search)
//   3. 添加集成测试
//   4. 文档：见 docs/tools/search-tool.md
//
// Future: v1.1+ (计划于 Q2 2025)
type GoogleSearchTool struct {
	apiKey    string
	searchID  string
	httpClient *http.Client
}

func (s *GoogleSearchTool) Search(ctx context.Context, query string) ([]SearchResult, error) {
	// 返回 Mock 数据用于演示
	return []SearchResult{
		{
			Title: "Mock result: " + query,
			URL:   "https://example.com/search?q=" + url.QueryEscape(query),
			Description: "This is a mock search result. To enable real search, " +
				"implement the Google Custom Search API integration.",
		},
	}, nil
}
```

**选项C: 删除** (如果不是核心功能)
```go
// 删除此文件和相关测试
```

---

## 问题 3: 低测试覆盖率

### 模块: `tools/practical` (39.9%)

当前测试不足:
```go
// tools/practical/practical_test.go (不完整)
func TestAPICaller(t *testing.T) {
	caller := NewAPICaller()
	// 缺少错误场景测试
}

func TestFileOperations(t *testing.T) {
	// 缺少权限错误、路径遍历等边界条件
}

func TestDatabaseQuery(t *testing.T) {
	// 缺少 SQL 注入防护、连接池等测试
}
```

应补充的测试:
```go
// 1. 错误处理
func TestAPICallerErrorHandling(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want error
	}{
		{"invalid URL", "not-a-url", ErrInvalidURL},
		{"timeout", "http://httpbin.org/delay/10", context.DeadlineExceeded},
		{"not found", "http://httpbin.org/status/404", ErrHTTPError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := caller.Call(ctx, tt.url)
			if !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}
}

// 2. 并发安全
func TestFileOperationsConcurrency(t *testing.T) {
	// 测试并发文件操作
	var wg sync.WaitGroup
	results := make([]error, 10)
	
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, results[idx] = ReadFile(tmpFile)
		}(i)
	}
	wg.Wait()
	
	// 验证结果一致
}

// 3. 边界条件
func TestDatabaseQueryBoundary(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  error
	}{
		{"empty query", "", ErrEmptyQuery},
		{"sql injection", "'; DROP TABLE users; --", ErrSQLInjection},
		{"huge result set", "SELECT * FROM large_table", nil}, // 应正常处理
	}
	// ...
}
```

---

## 问题 4: 过度使用内联优化

### 文件: `core/agent.go` (行 158-161)

当前代码（过度优化）:
```go
// Name 返回 Agent 名称
//
//go:inline
func (a *BaseAgent) Name() string {
	return a.name
}

// Description 返回 Agent 描述
//
//go:inline
func (a *BaseAgent) Description() string {
	return a.description
}

// Capabilities 返回 Agent 能力列表
func (a *BaseAgent) Capabilities() []string {
	return a.capabilities
}

// InvokeFast 快速调用（绕过中间件）
//go:inline
func (a *BaseAgent) InvokeFast(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	// ... 100+ 行实现
}
```

改进建议:
```go
// Name 返回 Agent 名称
func (a *BaseAgent) Name() string {
	return a.name
}

// Description 返回 Agent 描述
func (a *BaseAgent) Description() string {
	return a.description
}

// Capabilities 返回 Agent 能力列表
func (a *BaseAgent) Capabilities() []string {
	return a.capabilities
}

// InvokeFast 快速调用（绕过中间件）
// 
// 性能关键路径: 注意此方法避免了 Runnable 回调开销，
// 适用于嵌套调用和性能敏感场景。
// 基准测试表明性能提升 ~30%。
//
//go:inline
func (a *BaseAgent) InvokeFast(ctx context.Context, input *AgentInput) (*AgentOutput, error) {
	// ... 实现
}
```

理由：
- Getter 方法（`return a.name`）编译器已自动内联，显式标注无需
- `Capabilities` 返回切片，编译器可能不会内联，标注无益
- `InvokeFast` 是性能关键路径且有 ~100 行代码，标注有意义但要加文档

---

## 问题 5: 不规范的日志调用

### 文件: `core/middleware/middleware.go` (行 184)

当前代码（不规范）:
```go
// GetDefaultLogger 获取默认日志记录器
func GetDefaultLogger() func(string) {
	logger := func(msg string) { fmt.Println(msg) }
	return logger
}
```

改进代码（规范）:
```go
import "log/slog"

// GetDefaultLogger 获取默认日志记录器
func GetDefaultLogger() func(string) {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})
	logger := slog.New(handler)
	
	return func(msg string) {
		logger.Info(msg)
	}
}

// 或直接返回 slog.Logger
func NewStructuredLogger() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return slog.New(handler)
}
```

优势：
- 支持日志级别过滤（DEBUG, INFO, WARN, ERROR）
- JSON 格式便于解析和分析
- 添加源码位置便于调试
- 结构化字段便于日志聚合（ELK, Splunk 等）

---

## 问题 6: 内存管理策略混乱

### 文件: `core/agent.go` (行 634-648)

当前代码（混乱）:
```go
// Copy metadata from output to context
if output.Metadata != nil {
	// Reuse existing context map if possible, otherwise create new
	if pooledInput.Context == nil {
		pooledInput.Context = make(map[string]interface{}, len(output.Metadata))
	} else {
		// 清理 Context map
		// 策略：如果 map 过大，直接丢弃重建，避免长期持有大内存
		if len(pooledInput.Context) > maxContextMapSize {
			pooledInput.Context = make(map[string]interface{}, len(output.Metadata))
		} else {
			// Go 1.21+ 使用 clear() 内置函数
			clear(pooledInput.Context)
		}
	}
	for k, v := range output.Metadata {
		pooledInput.Context[k] = v
	}
}
```

改进方案（统一策略）:
```go
const maxContextMapSize = 1000

// Copy metadata from output to context
if output.Metadata != nil {
	if pooledInput.Context == nil {
		// 首次创建，预分配合理容量
		pooledInput.Context = make(map[string]interface{}, len(output.Metadata))
	} else if len(pooledInput.Context) > maxContextMapSize {
		// 策略: map 过大时重建，避免内存驻留
		// 理由: 重建成本低于长期持有大内存成本
		pooledInput.Context = make(map[string]interface{}, len(output.Metadata))
	} else {
		// 策略: 清空复用，利用预分配的容量
		clear(pooledInput.Context)
	}
	
	// 复制元数据
	for k, v := range output.Metadata {
		pooledInput.Context[k] = v
	}
}
```

性能验证:
```go
func BenchmarkContextClearing(b *testing.B) {
	ctx := make(map[string]interface{}, 100)
	
	b.Run("clear", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			clear(ctx)
		}
	})
	
	b.Run("rebuild", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ctx = make(map[string]interface{}, 100)
		}
	})
}
```

---

## 总结

这些问题需要在合并前逐一修复。关键优先级：

1. **英文注释** - 快速全项目搜索替换
2. **TODO 标记** - 评估优先级后处理
3. **低覆盖率** - 添加测试用例
4. **其他优化** - 在第 2 阶段处理

