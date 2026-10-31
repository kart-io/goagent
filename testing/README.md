# testing 测试工具库

本模块是 goagent 框架的测试支持库，提供 Mock 实现、测试工具和断言辅助函数，简化单元测试和集成测试编写。

## 目录

- [架构设计](#架构设计)
- [Mock 实现](#mock-实现)
- [测试工具](#测试工具)
- [断言函数](#断言函数)
- [使用方法](#使用方法)
- [API 参考](#api-参考)
- [代码结构](#代码结构)

## 架构设计

### 模块结构

```mermaid
graph TB
    subgraph "Mock 层"
        MockTool[MockTool<br/>工具 Mock]
        MockLLM[MockLLMClient<br/>LLM Mock]
        MockState[MockState<br/>状态 Mock]
        MockStore[MockStore<br/>存储 Mock]
    end

    subgraph "工具层"
        TestCtx[TestContext<br/>测试上下文]
        Helpers[断言辅助函数]
    end

    subgraph "被测组件"
        Agent[Agent]
        Tools[Tools]
        Memory[Memory]
    end

    MockTool --> Agent
    MockLLM --> Agent
    MockState --> Agent
    MockStore --> Memory
    TestCtx --> Agent
    TestCtx --> Tools
    Helpers --> Agent

    style TestCtx fill:#e1f5ff
    style MockLLM fill:#e8f5e9
```

### 测试流程

```mermaid
sequenceDiagram
    participant Test as 测试代码
    participant TC as TestContext
    participant Mock as Mock 组件
    participant SUT as 被测系统

    Test->>TC: NewTestContext(t)
    TC->>Mock: 创建 Mock 组件
    TC-->>Test: TestContext

    Test->>Mock: 配置响应/行为
    Test->>SUT: 执行测试
    SUT->>Mock: 调用依赖
    Mock-->>SUT: 返回 Mock 响应
    SUT-->>Test: 返回结果

    Test->>Mock: 验证调用
    Test->>Test: Assert 断言
```

## Mock 实现

### 1. MockTool 工具 Mock

实现 `interfaces.Tool`（`ArgsSchema` 返回 `{"type": "object"}`），可直接注册给 Builder 或 Dispatcher。

**核心功能**：

| 方法 | 说明 |
|------|------|
| `NewMockTool(name, desc)` | 创建 Mock 工具 |
| `SetInvokeFunc(fn)` | 设置自定义执行函数 |
| `SetError(should, msg)` | 配置错误返回 |
| `GetInvokeCount()` | 获取调用次数 |
| `GetLastInput()` | 获取最后输入 |
| `Reset()` | 重置状态 |

### 2. MockLLMClient LLM Mock

**核心功能**：

| 方法 | 说明 |
|------|------|
| `NewMockLLMClient()` | 创建 Mock LLM |
| `SetResponses(responses...)` | 设置预定义响应序列 |
| `SetError(should, msg)` | 配置错误返回 |
| `SetFunctionCallMode(enabled)` | 启用函数调用模式 |
| `SetFunctionResponse(name, resp)` | 设置函数响应 |
| `GetRequestHistory()` | 获取请求历史 |
| `Reset()` | 重置状态 |

### 3. MockStreamingLLMClient 流式 Mock

**核心功能**：

| 方法 | 说明 |
|------|------|
| `NewMockStreamingLLMClient()` | 创建流式 Mock |
| `SetStreamChunks(chunks...)` | 设置流式分块 |
| `Stream(ctx, req)` | 返回流式 channel |

### 4. MockState 状态 Mock

**实现接口**：`state.State`

**核心功能**：

| 方法 | 说明 |
|------|------|
| `Get/Set/Delete/Clear` | 基础 CRUD |
| `Snapshot/Restore/Clone` | 快照管理 |
| `Update/Keys/Size` | 批量更新与查询（`SetUpdateFunc` 的回调只在 `Update` 中触发） |
| `GetString/Int/Bool/Float64/Time` | 类型化获取 |
| `SetUpdateFunc(fn)` | 设置更新回调 |
| `GetStats()` | 获取统计 (get/set 次数) |

### 5. MockStore 存储 Mock

**实现接口**：`store.Store`

**核心功能**：

| 方法 | 说明 |
|------|------|
| `Put/Get/Delete` | 基础存储操作 |
| `List(namespace)` | 列出键 |
| `Search(namespace, filter)` | 搜索（支持 prefix） |
| `Clear(namespace)` | 清空命名空间 |
| `Size/Namespaces` | 统计信息 |
| `SetError(should, msg)` | 配置错误 |
| `GetStats()` | 获取统计 |

### 6. MockToolRegistry 工具注册表 Mock

**核心功能**：

| 方法 | 说明 |
|------|------|
| `Register(tool)` | 注册工具 |
| `Get(name)` | 获取工具 |
| `List()` | 列出所有工具 |
| `Clear()` | 清空注册表 |

### 7. MockToolExecutor 工具执行器 Mock

**核心功能**：

| 方法 | 说明 |
|------|------|
| `ExecuteParallel(ctx, tools, inputs)` | 并行执行 |
| `SetExecuteFunc(fn)` | 设置自定义执行函数 |
| `SetParallelResults(results...)` | 设置并行结果 |
| `SetError(should, msg)` | 配置错误返回 |
| `GetExecuteCalls()` | 获取执行次数 |
| `Reset()` | 重置状态 |

## 测试工具

### TestContext 测试上下文

```go
type TestContext struct {
    Ctx          context.Context
    Cancel       context.CancelFunc
    State        *state.AgentState
    Store        store.Store
    Checkpointer checkpoint.Checkpointer
    MockLLM      *mocks.MockLLMClient
    MockTools    map[string]*mocks.MockTool
    T            *testing.T
}
```

**自动管理**：
- 30 秒超时的 context
- 自动注册 cleanup 回调
- 内存存储后端
- 内存检查点存储

### TestAppContext 应用上下文

```go
type TestAppContext struct {
    UserID   string
    UserName string
    Metadata map[string]interface{}
}
```

### TestAgentConfig 测试配置

```go
type TestAgentConfig struct {
    Name         string   // Agent 名称
    SystemPrompt string   // 系统提示
    Tools        []string // 工具列表
    MaxIter      int      // 最大迭代
}

// 默认配置
func DefaultTestAgentConfig() TestAgentConfig {
    return TestAgentConfig{
        Name:         "test-agent",
        SystemPrompt: "You are a test agent",
        Tools:        []string{"calculator", "search"},
        MaxIter:      5,
    }
}
```

## 断言函数

### 基础断言

| 函数 | 说明 |
|------|------|
| `AssertNoError(t, err, msg...)` | 断言无错误 |
| `AssertError(t, err, msg...)` | 断言有错误 |
| `AssertEqual(t, expected, actual, msg...)` | 断言相等 |
| `AssertNotNil(t, value, msg...)` | 断言非空 |
| `AssertNil(t, value, msg...)` | 断言为空 |
| `AssertTrue(t, value, msg...)` | 断言为真 |
| `AssertFalse(t, value, msg...)` | 断言为假 |
| `AssertContains(t, haystack, needle, msg...)` | 断言包含子串 |

### 异步断言

```go
// 等待条件成立（轮询）
AssertEventually(t, condition func() bool, timeout time.Duration, msg...)

// 等待条件（带 context）
WaitForCondition(ctx context.Context, condition func() bool, interval time.Duration) error
```

### 并行测试

```go
// 并行运行多个测试
RunParallel(t *testing.T, tests map[string]func(t *testing.T))
```

## 使用方法

### 基础测试设置

```go
func TestMyAgent(t *testing.T) {
    // 创建测试上下文
    tc := testutil.NewTestContext(t)

    // 配置 Mock LLM 响应
    tc.MockLLM.SetResponses(
        llm.CompletionResponse{
            Content:    "这是 Mock 响应",
            Model:      "mock-model",
            TokensUsed: 10,
        },
    )

    // 添加 Mock 工具
    calcTool := tc.AddMockTool("calculator", "计算器工具")
    calcTool.SetInvokeFunc(func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
        return &interfaces.ToolOutput{
            Result:  "42",
            Success: true,
        }, nil
    })

    // 创建运行时
    runtime := tc.CreateRuntime("test-session")

    // 执行测试...
}
```

### Mock LLM 多轮对话

```go
func TestMultiTurnConversation(t *testing.T) {
    tc := testutil.NewTestContext(t)

    // 设置多轮响应序列
    tc.MockLLM.SetResponses(
        llm.CompletionResponse{Content: "第一轮响应"},
        llm.CompletionResponse{Content: "第二轮响应"},
        llm.CompletionResponse{Content: "Final Answer: 42"},
    )

    // 执行 Agent...

    // 验证请求历史
    history := tc.MockLLM.GetRequestHistory()
    testutil.AssertEqual(t, 3, len(history), "应该有 3 轮对话")
}
```

### Mock 函数调用

```go
func TestFunctionCalling(t *testing.T) {
    tc := testutil.NewTestContext(t)

    // 启用函数调用模式
    tc.MockLLM.SetFunctionCallMode(true)

    // 设置函数响应
    tc.MockLLM.SetFunctionResponse("calculator", map[string]interface{}{
        "result": 42,
    })

    // 当消息包含 "calculate" 时，自动返回函数调用响应
}
```

### 流式响应测试

```go
func TestStreamingResponse(t *testing.T) {
    streamClient := mocks.NewMockStreamingLLMClient()

    // 设置流式分块
    streamClient.SetStreamChunks(
        "这是",
        "流式",
        "响应",
    )

    // 获取流
    ch, err := streamClient.Stream(ctx, req)
    testutil.AssertNoError(t, err)

    // 消费流
    var content strings.Builder
    for chunk := range ch {
        content.WriteString(chunk.Content)
        if chunk.Done {
            break
        }
    }

    testutil.AssertEqual(t, "这是流式响应", content.String())
}
```

### Mock 状态测试

```go
func TestStateManagement(t *testing.T) {
    state := mocks.NewMockState()

    // 设置更新回调
    var updates []string
    state.SetUpdateFunc(func(key string, value interface{}) error {
        updates = append(updates, key)
        return nil
    })

    // 执行操作
    state.Set("key1", "value1")
    state.Update(map[string]interface{}{
        "key2": "value2",
        "key3": "value3",
    })

    // 验证
    testutil.AssertEqual(t, 2, len(updates))

    // 快照和恢复
    snapshot := state.Snapshot()
    state.Clear()
    state.Restore(snapshot)

    val, ok := state.GetString("key1")
    testutil.AssertTrue(t, ok)
    testutil.AssertEqual(t, "value1", val)
}
```

### Mock 存储测试

```go
func TestStoreOperations(t *testing.T) {
    store := mocks.NewMockStore()

    ctx := context.Background()
    namespace := []string{"test", "namespace"}

    // 存储
    err := store.Put(ctx, namespace, "key1", "value1")
    testutil.AssertNoError(t, err)

    // 获取
    item, err := store.Get(ctx, namespace, "key1")
    testutil.AssertNoError(t, err)
    testutil.AssertEqual(t, "value1", item.Value)

    // 搜索
    results, err := store.Search(ctx, namespace, map[string]interface{}{
        "prefix": "key",
    })
    testutil.AssertNoError(t, err)
    testutil.AssertEqual(t, 1, len(results))

    // 统计
    putCount, getCount := store.GetStats()
    testutil.AssertEqual(t, 1, putCount)
    testutil.AssertEqual(t, 1, getCount)
}
```

### 错误注入测试

```go
func TestErrorHandling(t *testing.T) {
    tc := testutil.NewTestContext(t)

    // 配置 LLM 返回错误
    tc.MockLLM.SetError(true, "API 调用失败")

    // 执行并验证错误处理
    _, err := agent.Execute(tc.Ctx, input)
    testutil.AssertError(t, err)
    testutil.AssertContains(t, err.Error(), "API 调用失败")

    // 重置错误状态
    tc.MockLLM.Reset()
}
```

### 异步条件等待

```go
func TestAsyncOperation(t *testing.T) {
    tc := testutil.NewTestContext(t)

    var completed bool
    go func() {
        time.Sleep(100 * time.Millisecond)
        completed = true
    }()

    // 等待条件成立
    testutil.AssertEventually(t, func() bool {
        return completed
    }, 1*time.Second, "操作应该在 1 秒内完成")
}
```

### 并行测试

```go
func TestParallel(t *testing.T) {
    testutil.RunParallel(t, map[string]func(t *testing.T){
        "TestCase1": func(t *testing.T) {
            // 测试用例 1
        },
        "TestCase2": func(t *testing.T) {
            // 测试用例 2
        },
        "TestCase3": func(t *testing.T) {
            // 测试用例 3
        },
    })
}
```

## API 参考

### mocks 包

```go
// 工具 Mock
NewMockTool(name, description string) *MockTool
NewMockToolRegistry() *MockToolRegistry
NewMockToolExecutor() *MockToolExecutor

// LLM Mock
NewMockLLMClient() *MockLLMClient
NewMockStreamingLLMClient() *MockStreamingLLMClient

// 状态 Mock
NewMockState() *MockState

// 存储 Mock
NewMockStore() *MockStore
```

### testutil 包

```go
// 测试上下文
NewTestContext(t *testing.T) *TestContext
(tc *TestContext) AddMockTool(name, description string) *MockTool
(tc *TestContext) CreateRuntime(sessionID string) *Runtime

// 断言
AssertNoError(t, err, msg...)
AssertError(t, err, msg...)
AssertEqual(t, expected, actual, msg...)
AssertNotNil(t, value, msg...)
AssertNil(t, value, msg...)
AssertTrue(t, value, msg...)
AssertFalse(t, value, msg...)
AssertContains(t, haystack, needle, msg...)
AssertEventually(t, condition, timeout, msg...)

// 工具
WaitForCondition(ctx, condition, interval) error
RunParallel(t, tests)

// 配置
DefaultTestAgentConfig() TestAgentConfig
```

## 代码结构

```text
testing/
├── mocks/
│   ├── mock_tools.go      # 工具相关 Mock
│   │   ├── MockTool
│   │   ├── MockToolRegistry
│   │   └── MockToolExecutor
│   ├── mock_llm.go        # LLM Mock
│   │   ├── MockLLMClient
│   │   └── MockStreamingLLMClient
│   └── mock_state.go      # 状态和存储 Mock
│       ├── MockState
│       └── MockStore
├── testutil/
│   └── helpers.go         # TestContext、断言与工具函数
├── panictest/             # 在子进程中运行可能使进程退出的测试
└── replay/                # 会话日志回放
```

## 最佳实践

### 1. 使用 TestContext

```go
// 推荐：使用 TestContext 统一管理
tc := testutil.NewTestContext(t)
// 自动管理超时、cleanup、Mock 组件

// 不推荐：手动创建每个组件
ctx := context.Background()
mockLLM := mocks.NewMockLLMClient()
// 需要手动管理生命周期
```

### 2. 验证 Mock 调用

```go
// 验证调用次数
count := mockTool.GetInvokeCount()
testutil.AssertEqual(t, 3, count, "工具应该被调用 3 次")

// 验证调用参数
lastInput := mockTool.GetLastInput()
testutil.AssertEqual(t, "expected_arg", lastInput.Args["arg1"])
```

### 3. 测试后重置

```go
// 重置以复用 Mock
mockLLM.Reset()
mockTool.Reset()
mockStore.SetError(false, "")
```

### 4. 使用 t.Cleanup

```go
// TestContext 自动注册 cleanup
tc := testutil.NewTestContext(t)
// context 会在测试结束时自动取消

// 手动添加 cleanup
t.Cleanup(func() {
    // 清理资源
})
```

## 回放会话日志（testing/replay）

把 [`core/session`](../core/session/README.md) 中记录的持久运行转为回归测试（cut-point replay）。重放一轮时，模型回合、工具调用、循环扩展点等持久操作按边界逐个决定：返回记录的结果，或用当前代码实跑。实跑结果与记录不同（分歧）之后的边界一律实跑，因为它们的输入已经改变。重放只读原日志，写入留在内存中。

```go
rp := &replay.Replayer{Source: store, SessionID: "s", Live: replay.Kind("tool")} // 工具用新代码实跑，其余回放
turns, _ := rp.Turns(ctx)                                                         // 日志中已结束的持久运行
ctx, log, report, _ := rp.Prepare(ctx, turns[0])
agent, _ := builder.NewSimpleBuilder(model).WithSessionID("s").WithSessionLog(log).
    WithDurableScope(scope).WithTools(tools...).Build() // 与记录时相同的会话 ID 与 DurableScope
out, _ := agent.AsRunnable().Invoke(ctx, turns[0].Input)
if res, ok := report.Diverged(); ok {
    t.Errorf("%s 的结果变了：记录 %s，现在 %s", res.ID, res.Recorded, res.Value)
}
```

- `Cut`：选中的第一个边界及其之后全部实跑，如 `replay.ModelTurn(1)` 用新模型或新提示词重跑第二次请求之后的部分。
- `Live`：选中的边界单独实跑，结果与记录一致时之后继续回放，如 `replay.Kind("tool")` 检查改过的工具是否仍给出相同结果。
- `Prepare` 返回的 `log` 是该轮开始前的会话副本，历史与记录时相同；重放产生的对话也记在其中，可与原日志比较。
- 改动影响到被回放的边界的输入时（如改了提示词而没有从那里切开），durable 报告 `operation intent or attempt policy changed`；顺序执行时 `report.Results()` 的最后一项就是出错的边界（同一回合并行的工具调用除外），应从那里切开。

## 子进程 panic 测试（testing/panictest）

`panictest.Run(t, fn)` 在子进程中重新执行当前顶层测试并运行 `fn`，父进程断言子进程正常退出且没有未恢复的 panic，用于回归时可能让进程崩溃的场景。`Recorder` 实现全局 panic 指标收集器的 `RecordPanic`，传给 `core.SetGlobalMetricsCollector` 后可用 `Wait(n, timeout)` 取得收到的 panic。`CombinedOutput(cmd, timeout)` 与 `ChildTimeout(t, limit)` 用于自行管理带时限的子进程。

```go
func TestNoCrash(t *testing.T) {
    panictest.Run(t, func(t *testing.T) {
        rec := &panictest.Recorder{}
        core.SetGlobalMetricsCollector(rec)
        // 触发可能 panic 的代码……
        if events := rec.Wait(1, time.Second); len(events) != 1 {
            t.Fatalf("panic 未被恢复上报: %v", events)
        }
    })
}
```

## 扩展阅读

- [core](../core/) - 核心执行引擎
- [llm](../llm/) - LLM 集成
- [interfaces](../interfaces/) - 接口定义
- [store](../store/) - 存储系统
