# GoAgent 测试最佳实践

## 覆盖率与门禁

- PR 流程运行 `./scripts/check_coverage.sh`：整体覆盖率（排除 `examples/`）不低于 45%，`llm/providers/openai.go`、`llm/providers/gemini.go`、`llm/common/base.go` 另有单文件阈值。
- 新行为必须带测试，覆盖正常流程、边界与错误恢复；修 bug 先写能复现的测试。
- 默认测试不访问外部服务；真实模型测试见 [Provider 持续回归](../guides/PROVIDER_REGRESSION.md)。

## 运行

```bash
make test                                   # GO_TEST_MODE=true go test -v -race -timeout 120s ./...
make test-short                             # -short
make test-integration                       # -tags=integration
go test -race -run TestName ./builder/      # 单个包或用例
make coverage                               # coverage/coverage.out 与 coverage.html
go test -coverprofile=coverage.out ./... && ./scripts/check_coverage.sh
bash scripts/provider_regression.sh         # 离线 Provider/Builder 契约与竞态回归
```

## 测试工具

| 位置 | 内容 |
|------|------|
| `llm.NewMockStreamClient()` | 实现 `llm.StreamClient` 的离线客户端，适合不带工具的 Agent |
| `testing/mocks` | `MockLLMClient`（`SetResponses`、`SetError`、`GetRequestHistory`）、`MockStreamingLLMClient`、`MockState`、`MockStore`、`MockTool` 等 |
| `testing/testutil` | `TestContext`、断言与等待辅助函数 |
| `testing/replay` | 把会话日志中的持久运行转为回归测试，按边界选择回放或实跑 |
| `testing/panictest` | 在子进程中运行可能让进程退出的测试 |
| `core/durable/storetest`、`core/session/sessiontest` | `durable.Store` / `session.Store` 实现的一致性测试，自定义存储后端须通过 |

`mocks.MockTool` 没有 `ArgsSchema` 方法，不满足 `interfaces.Tool`；给 Builder 或 Dispatcher 的测试工具用 `tools.NewFunctionTool` 构造。

## 原生工具循环的测试模型

Builder 的原生工具循环要求 `llm.ToolStreamClient`。用一个按顺序回放预设回合的模型即可离线测试工具调用、事件与持久恢复（`builder/tool_events_test.go` 中的 `nativeEventModel` 是同样的做法）：

```go
// scriptedModel 按顺序回放预设的回合，每次 CompleteToolsStream 消费一回合。
type scriptedModel struct {
	*llm.MockStreamClient
	mu       sync.Mutex
	turns    [][]*llm.StreamChunk
	requests []*llm.CompletionRequest
}

func newScriptedModel(turns ...[]*llm.StreamChunk) *scriptedModel {
	return &scriptedModel{MockStreamClient: llm.NewMockStreamClient(), turns: turns}
}

func (m *scriptedModel) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) >= len(m.turns) {
		return nil, errors.New("多余的模型请求")
	}
	m.requests = append(m.requests, req)
	turn := m.turns[len(m.requests)-1]
	ch := make(chan *llm.StreamChunk, len(turn))
	for _, c := range turn {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func toolCallTurn(id, name, args string) []*llm.StreamChunk {
	call := llm.ToolCall{ID: id, Type: "function"}
	call.Function.Name, call.Function.Arguments = name, args
	return []*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{call}}}
}

func answerTurn(text string) []*llm.StreamChunk {
	return []*llm.StreamChunk{{Delta: text, Content: text}, {Done: true, FinishReason: "stop", Content: text}}
}

func newAgent(t *testing.T, model *scriptedModel, calls *atomic.Int32) *builder.SimpleAgent {
	t.Helper()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewFunctionTool("lookup", "查询",
		`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
		func(ctx context.Context, args map[string]any) (any, error) {
			calls.Add(1)
			return "shipped", nil
		})))
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
	require.NoError(t, err)
	agent, err := builder.NewSimpleBuilder(model).
		WithTools(d.Tools()...).
		WithDurableScope("test").
		BuildContext(context.Background())
	require.NoError(t, err)
	return agent
}
```

### 事件顺序

```go
func TestToolEvents(t *testing.T) {
	var calls atomic.Int32
	model := newScriptedModel(toolCallTurn("c1", "lookup", `{"id":"A-1"}`), answerTurn("已发货"))
	agent := newAgent(t, model, &calls)

	events, err := agent.ExecuteStream(context.Background(), "A-1 到哪了")
	require.NoError(t, err)
	var steps []string
	for e := range events {
		require.NoError(t, e.Error)
		if e.Type == "step" {
			steps = append(steps, e.Step)
		}
	}
	require.Equal(t, []string{"model_start", "tool_start", "tool_end", "model_start"}, steps)
	require.Len(t, model.requests, 2)
}
```

### 持久恢复与回放

```go
func TestDurableRunReplays(t *testing.T) {
	store := session.NewMemoryStore()
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "run-1"), session.DurableStore(store, "s1"))
	var calls atomic.Int32

	first := newScriptedModel(toolCallTurn("c1", "lookup", `{"id":"A-1"}`), answerTurn("已发货"))
	out, err := newAgent(t, first, &calls).AsRunnable().Invoke(ctx, "A-1 到哪了")
	require.NoError(t, err)
	require.Equal(t, "已发货", out.Result)

	// 同一执行键重新执行：模型回合与工具调用全部回放，模型与工具都不再被调用。
	replay := newScriptedModel()
	out, err = newAgent(t, replay, &calls).AsRunnable().Invoke(ctx, "A-1 到哪了")
	require.NoError(t, err)
	require.Equal(t, "已发货", out.Result)
	require.Empty(t, replay.requests)
	require.Equal(t, int32(1), calls.Load())
}
```

以上三段代码在本仓库当前 API 下编译并通过。崩溃恢复类测试的常见做法：在工具或模型中途取消 ctx，再用新建的 Agent 与存储实例以同一执行键恢复，断言副作用与预算不重复（参考 `builder/durable_entry_test.go`、`execution/integration_test.go`）。

## 约定

- 表驱动测试 + `t.Run` 子测试；断言用 `testify` 的 `require`（失败即停）与 `assert`。
- 修改全局状态（如 `core.SetGlobalPanicHandler`）后用 `t.Cleanup` 恢复。
- 等待异步结果用通道或 `testutil.WaitForCondition` 加超时，不用固定 `time.Sleep`。
- 并发相关改动必须在 `-race` 下通过；取消与超时路径要单独覆盖。
- 自定义 `durable.Store` / `session.Store` 后端在自己的测试中调用 `storetest.Run` / `sessiontest.Run`。
- 真实模型或外部服务的测试用 `-tags=integration` 或环境变量控制，缺少凭据时 `t.Skip`。

## 相关文档

- [开发指南](DEVELOPMENT_GUIDE.md)
- [Panic 处理指南](PANIC_HANDLING.md)
- [架构概述](../architecture/ARCHITECTURE.md)
