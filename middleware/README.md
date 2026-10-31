# middleware 中间件

本包提供两类组件：

- `ObservabilityMiddleware`：实现 `core/middleware.Middleware`（`OnBefore`/`OnAfter`/`OnError`），挂在 `coremiddleware.MiddlewareChain` 上记录 OpenTelemetry 追踪与指标。
- 基于状态的处理器：`LLMToolSelectorMiddleware`、`DynamicPromptMiddleware`、`LLMToolEmulatorMiddleware`、`AdaptiveMiddleware`、`ContextEnrichmentMiddleware`。它们的逻辑在 `Process(ctx, state core.State) (core.State, error)` 中，读写约定的状态键；嵌入的 `BaseMiddleware` 的 `OnBefore`/`OnAfter` 原样透传，因此加入 `MiddlewareChain` 不会触发其逻辑，需由调用方显式调用 `Process`。

`Middleware` 接口、`MiddlewareChain`、请求/响应对象池以及缓存、限流、熔断等通用中间件位于 [core/middleware](../core/middleware/)。

## ObservabilityMiddleware

```go
obs, err := middleware.NewObservabilityMiddleware(provider) // provider: *observability.TelemetryProvider
if err != nil {
    return err
}

chain := coremiddleware.NewMiddlewareChain(handler).Use(obs)

req := coremiddleware.GetMiddlewareRequest()
defer coremiddleware.PutMiddlewareRequest(req)
req.Input = userInput
req.Timestamp = time.Now()
req.Metadata["agent.name"] = "assistant"

resp, err := chain.Execute(ctx, req)
```

- `OnBefore`：以 `Metadata["agent.name"]` 为名启动 Agent span 并存入请求元数据，活跃计数加一。
- `OnAfter`：从响应元数据取回 span（处理器需把请求元数据带入响应，Builder 即如此），`response.Error` 非空时记为错误、否则记为成功，然后结束 span；记录执行耗时与成功与否（`Metadata["agent.name"]`、`["agent.type"]`），活跃计数减一。
- `OnError`：把错误记入 `ctx` 中的 span 与错误指标，活跃计数减一。处理器返回错误时链只调用 `OnError`，它取不到 `OnBefore` 启动的 span，该 span 不会结束、不会导出。
- `RecordToolCall(ctx, toolName, duration, err)`、`RecordLLMCall(...)`：供调用方手动记录工具与 LLM 调用。

## 基于状态的处理器

| 类型 | 读取的状态键 | 写入的状态键 |
|---|---|---|
| `LLMToolSelectorMiddleware` | `tools`（`[]interfaces.Tool`）、`query`（string） | `tools`（筛选后）、`tool_selection_metadata` |
| `DynamicPromptMiddleware` | 生成器自行读取 | `system_prompt`（追加）、`prompt_enhancements` |
| `LLMToolEmulatorMiddleware` | `pending_tool_call`（含 `name`、`input` 的 map） | `tool_result`、`tool_emulated` |
| `AdaptiveMiddleware` | `error` | `CurrentConfig` 中的全部键 |
| `ContextEnrichmentMiddleware` | `context` | `context`（合并）、`context_enriched`、`enrichment_errors` |

### LLMToolSelectorMiddleware

用一个（通常较便宜的）模型从 `tools` 中挑选与 `query` 相关的最多 `maxTools` 个工具。`WithAlwaysInclude(...)` 指定始终保留的工具；选择结果按 `query` 与工具数量缓存 5 分钟（`CacheTTL`）；模型调用失败时保留全部工具。

```go
selector := middleware.NewLLMToolSelectorMiddleware(cheapModel, 5).
    WithAlwaysInclude("help_tool")

st := core.NewAgentState()
st.Set("tools", allTools)
st.Set("query", "查询北京天气")
out, err := selector.Process(ctx, st) // out.Get("tools") 为筛选后的工具
```

### DynamicPromptMiddleware

条件满足的 `PromptGenerator` 生成的文本追加到 `system_prompt`：

```go
prompts := middleware.NewDynamicPromptMiddleware().
    AddGenerator(middleware.PromptGenerator{
        Name:     "expert_mode",
        Priority: 100,
        Condition: func(s core.State) bool {
            v, _ := s.Get("user_expertise")
            return v == "advanced"
        },
        Generate: func(s core.State) string { return "你是专家助手，提供详细的技术回答。" },
    })
```

### LLMToolEmulatorMiddleware

用模型模拟工具执行（测试用）。`SetEmulatedTools(names...)` 指定要模拟的工具，`SetEmulateAll(true)` 模拟全部。

### AdaptiveMiddleware

每次 `Process` 把 `CurrentConfig` 写入状态，记录本次耗时（保留最近 100 次计算 `AverageLatency`）与基于 `error` 键的成功率，再依次检查 `Adaptation`，条件成立时调用 `Apply(CurrentConfig)`：

```go
adaptive := middleware.NewAdaptiveMiddleware().
    AddAdaptation(middleware.Adaptation{
        Name:      "reduce_tokens_on_slow",
        Condition: func(m *middleware.PerformanceMetrics) bool { return m.AverageLatency > 2*time.Second },
        Apply:     func(cfg map[string]interface{}) { cfg["max_tokens"] = 100 },
    })
```

### ContextEnrichmentMiddleware

依次运行 `ContextEnricher`，把返回的数据合并进状态的 `context` map。`Async: true` 的增强器并发执行，`Timeout` 限制单个增强器；增强器的错误与（异步增强器的）panic 记入 `enrichment_errors`，不使 `Process` 失败。

```go
enrich := middleware.NewContextEnrichmentMiddleware().
    AddEnricher(middleware.ContextEnricher{
        Name: "user_profile",
        Enrich: func(ctx context.Context, s core.State) (map[string]interface{}, error) {
            return map[string]interface{}{"tier": "premium"}, nil
        },
        Async:   true,
        Timeout: 500 * time.Millisecond,
    })
```

## 代码结构

```text
middleware/
├── observability.go           # ObservabilityMiddleware
└── tool_selector_advanced.go  # 基于状态的处理器
```

## 扩展阅读

- [core/middleware](../core/middleware/) - Middleware 接口与中间件链
- [observability](../observability/README.md) - TelemetryProvider、AgentTracer、AgentMetrics
