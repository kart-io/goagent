# 多 Agent 预置模式实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在主模块新增顶层包 `multiagent`（Sequential、Loop、RouterSupervisor 图构造器，`ParallelStep`，`SupervisorTools`，文本接力层）与测试替身 `testing/mocks.ScriptedClient`，并补齐导入规则、离线示例与文档。

**Architecture:** 构造器只组合 `workflow.Graph` 的 `AddRunnable`/`AddParallel`/`AddEdge`/`AddBranch`，返回未编译的图，由宿主选择 `Compile` 或 `CompileDurable`，持久语义直接继承 `workflow`。`AgentStep` 把每次新建会话的 `AgentFactory` 包装成固定的会话 Runnable 后交给 `workflow.Adapt`；路由兜底集中在导出的 `GuardRoute`，构造器与手写图共用。`SupervisorTools` 用 `tools.NewRunnableToolFactory` 把成员包装成模型工具。

**Tech Stack:** Go 1.26（`go.mod`：`go 1.26.0`）、`github.com/stretchr/testify/require`、`durable.FileStore`、`budget.NewDurable`、真实 `builder` 与 `mocks.ScriptedClient`；格式化用 `gofumpt` 与 `gci`，静态检查用 golangci-lint v2.15.0。

**Spec:** `docs/superpowers/specs/2026-11-05-multiagent-patterns-design.md`（HEAD `bbfef186`，含 S5 先查回执、登记与判定同事务并限定意图，以及回写本计划核实结果与 `refund.Service.OnRegister` 钩子的修订）

## Global Constraints

- 工作目录：`/home/hellotalk/code/go/src/github.com/kart-io/goagent`，分支 `feat/multiagent-patterns`。
- 包路径 `github.com/kart-io/goagent/multiagent`。非测试代码只导入 `core`、`builder`、`tools`、`workflow`、`interfaces`（及标准库）；测试另用 `core/budget`、`core/durable`、`llm`、`errors`、`testing/mocks`、`workflow/refund`。这是描述性约定，由代码审查把关。
- `scripts/apicheck` 规则：`interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness` 禁止导入 `multiagent`；`multiagent` 加入现有 `execution` 规则与 `plugin` 规则的源目录列表。
- 主模块不新增第三方依赖，`go.mod` 不变。
- `cfg.MaxSteps` 必须为 0，否则构造报错；步数公式：Sequential 为 `n`，Loop 为 `MaxIterations × n`，RouterSupervisor 为 `2 × MaxRounds + 1`；每个 `ParallelStep` 计 1 步；Fallback 不增加步数；乘法与加法先检查 `int` 溢出。
- 持久图运行 ID 与根预算 `budget.Config.RootKey` 必须相同（`builder/agent_usage.go:57-61` 否则报 `agent budget root conflict`），测试与示例都按此装配。
- 文档、注释、测试描述、提交信息使用简体中文；代码标识符用英文并沿用规格中的导出名。错误文本用简体中文（与 `core/durable/interrupt.go`、`workflow/interrupt.go`、`tools/dispatcher.go` 近期代码一致），首字不用大写英文单词（staticcheck ST1005）。
- 导入分三组：标准库 → 第三方 → `github.com/kart-io/goagent/...`（`gci write -s standard -s default -s 'Prefix(github.com/kart-io/goagent)'`），代码用 `gofumpt` 格式化。只格式化本计划新增或修改的 Go 文件，不运行全仓 `make fmt`（`scripts/apicheck/main.go`、`workflow/refund/service.go`、`workflow/refund/service_test.go` 原本就有与本计划无关的 gofumpt/gci 差异，修改它们时只用 `gofmt` 检查，不要顺手重排）。
- 每个任务结束时运行：`go test -race -count=1 ./multiagent/... ./testing/mocks/...`（或该任务涉及的包）与 `go vet` 同一组包；全部通过才提交。
- 最终验证（Task 17）：`go test ./...`、`make lint`（golangci-lint v2.15.0）、`bash scripts/verify_imports.sh`（即 `go run ./scripts/apicheck -mode imports -root .`）、`go test ./scripts/apicheck/`、`python3 scripts/verify_doc_examples.py <本计划改动的文档>`。
- 不修改 `scripts/verify_all_examples.sh`；不提交 `.context/`。

## 与规格的偏差

本计划的代码已在仓库副本中逐任务编译、`go test -race` 与 `golangci-lint` 通过。规格与代码冲突处以代码为准：

1. **错误文本语言**：规格写 `ErrInvalidRoute = errors.New("multiagent: invalid route decision")`，计划用 `errors.New("路由决策无效")`，全部新错误文本使用中文，理由见 Global Constraints。调用方只应依赖 `errors.Is`。
2. **根预算 RootKey 必须等于运行 ID**（已回写规格，`0a6ad270`）：持久图中运行的 Agent 所用根预算的 RootKey 必须与图运行 ID 相同（`builder/agent_usage.go:57-61`，否则报 `agent budget root conflict`）。测试夹具 `env` 以同一字符串作运行 ID 与 RootKey，指南写明这一点。
3. **Agent 工具调用的操作 ID**：持久日志中工具调用的操作 ID 是 `tool/turn/<回合>/call/<调用 ID>`（如 `tool/turn/0/call/c1`），不是 `tool/<调用 ID>`；“工具 pending”测试按真实 ID 注入故障。
4. **ScriptedClient 的规则语义**：规则按顺序各消费一次（`Hits(i)` 为 0 或 1），始终严格（没有可用规则或断言不成立时返回错误）；“按请求内容选择回复”由规则的 `Respond` 函数实现，它也用来模拟阻塞到 ctx 结束的请求。
5. **补充的构造校验**（规格未写、为防误配而加）：`GuardRoute` 的 fallback 也不得为 `FINISH` 或 `workflow.END`；Router 与 Loop 的 Fallback 不得命名为 `FINISH`；`SupervisorTools` 至少一个成员；`TextNext` 拒绝首尾有空白的成员名；`TextStep` 拒绝 nil 选项。
6. **组合流程的步数**（已回写规格，`0a6ad270`）：规格现写明每多一个节点（含整组计 1 步的 `ParallelStep`）就加 1。场景测试在抽取后多放一个 `ParallelStep`，因此 `MaxSteps = 3 + 2N`、停在评审路由位置时步数为 `2 + 2N`；离线示例不含并行查询，沿用 `1 + 2N + 1`。
7. **S5 审核输入的 `reason` 字段**（已回写规格，`0a6ad270`）：输入为 `{"approved","note","approver","reason"}`，`reason` 可选，超时输入为 `{"approved": false, "reason": "timeout"}`；validate 用 `DisallowUnknownFields` 严格解码，`reason == "timeout"` 时按拒绝处理并写回“审核超时，按拒绝处理”。
8. **S5 “登记与判定同事务”的实现方式**：`refund.Service.Register` 原本只写申请，没有挂接额外写入的入口。Task 13 给 `refund.Service` 增加可选钩子 `OnRegister func(ctx context.Context, tx Transaction, req Request) error`（规格 `bbfef186` 已采纳）：仅在新插入申请时、同一事务内、`InsertRequest` 之后调用，出错整体回滚，重复登记不调用。S5 的退款成员每次登记用一份带钩子的 `Service` 副本，在钩子里经账本自己的事务类型写入争议判定与登记序号；真实存储同样在自己的 `Transaction` 实现上写入。
9. **S5 订单变化的判断位置**：`refund.Service` 的错误不是哨兵值，无法可靠区分“订单已变化”。执行操作的 run 在查回执之后、`Approve` 之前比较登记时固定的订单事实与当前事实，不一致时把“订单已变化，需重新申请”作为业务结果返回，效果与“批准失败转业务结果”一致。
10. **离线示例的 S5 账本**：`examples/multi-agent-patterns` 用内存账本演示“登记 → 审核 → 先查回执再执行”的结构，不接 `refund.Service`（场景测试接了）；示例 README 与指南写明真实业务使用 `workflow/refund.Service`。
11. **ParallelStep 的私有类型位置**：`parallelSpec` 定义在 `step.go`（`Step` 的字段需要它），`ParallelStep` 在 `parallel.go`。

## 已核实的现有 API

计划中的代码对照以下真实签名编写（行号为 HEAD `bbfef186`，其后只改动了规格）：

| API | 签名与要点 |
|---|---|
| `workflow.Config` | `struct{ MaxSteps, MaxConcurrency int }`，零值在 `NewGraph` 中取 100 与 4（`workflow/graph.go:29-32,58-74`） |
| `workflow.NewGraph` | `func NewGraph[T any](configs ...Config) *Graph[T]` |
| `(*Graph[T]).AddNode` / `AddRunnable` | `AddNode(name string, run Node[T]) error`；`AddRunnable(name string, run core.Runnable[T, T]) error`，typed nil 被拒（`graph.go:77-95`） |
| `(*Graph[T]).AddEdge` / `AddBranch` | `AddEdge(from, to string) error`；`AddBranch(from string, choose Router[T], targets ...string) error`，目标可含 `END` |
| `workflow.Router` / `Node` | `type Router[T any] func(context.Context, T) (string, error)`；`type Node[T any] func(context.Context, T) (T, error)` |
| `(*Graph[T]).AddParallel` | `AddParallel(name string, config ParallelConfig[T]) error`；`ParallelConfig[T]{Branches []Branch[T]; Clone func(T) T; Merge func(context.Context, T, []T) (T, error)}`，`Branch[T]{Name string; Run Node[T]}`（`parallel.go:14-54`） |
| `(*Graph[T]).Compile` | `Compile(ctx) (*core.StreamRunnable[T, T], error)` |
| `(*Graph[T]).CompileDurable` | `CompileDurable(ctx, DurableConfig, ...DurableOption[T]) (*DurableGraph[T], error)`；`DurableConfig{Store durable.Store; Version string; ApprovalNodes []string}`，计划摘要含 `Config`（`durable.go:96-136`） |
| `(*DurableGraph[T]).Run` / `Resume` / `Snapshot` | `Run(ctx, id string, initial T, approvalKey string) (*GraphSnapshot[T], error)`；`Resume(ctx, id, ResumeRequest{Key, Input json.RawMessage})`；`Snapshot(ctx, id)`；`GraphSnapshot` 有 `State, Node, Steps, Routing, Key, Waiting, Done, Interrupts []InterruptPoint` |
| `workflow.Adapt` | `func Adapt[S, I, O any](runnable core.Runnable[I, O], input func(S) (I, error), output func(S, O) (S, error)) (*core.StreamRunnable[S, S], error)`（`adapter.go:13`） |
| `workflow.DurableKey`、`ErrMaxSteps`、`ResumeRejectedError`、`ErrInterruptAnswered` | `DurableKey(ctx) string`；`ErrMaxSteps` 文本 `workflow maximum steps exceeded` |
| `core.Runnable` / `StreamRunnable` / `RunnableFunc` | `Runnable[I, O]` 含 `Invoke`、`Stream`、`Batch`、`Pipe`、`WithCallbacks`、`WithConfig`；`NewStreamRunnable[I, O any](mode StreamMode, source StreamSource[I, O])`；`NewRunnableFunc[I, O any](fn func(context.Context, I) (O, error))`；`StreamChunk[T]{Data T; Error error; Done bool; Event *StreamEvent}` |
| `tools.OwnedRunnable` | `struct{ Runnable core.Runnable[I, O]; Close func(context.Context) error }` |
| `tools.NewRunnableToolFactory` | `func NewRunnableToolFactory[I, O any](factory func(context.Context) (OwnedRunnable[I, O], error), cfg RunnableToolConfig[I, O]) (interfaces.Tool, error)`；Factory 返回错误但 `Close` 非 nil 时仍关闭（`runnable_tool.go:84-201`） |
| `tools.RunnableToolConfig` | `Name, Description, ArgsSchema, Version string; Decode func(map[string]any) (I, error); Encode func(O) (any, error); ModelInvoking, Resumable bool; MaxDepth int; CleanupTimeout, Timeout time.Duration` |
| `tools.ArgumentError` | `struct{ Message string; Err error }`；Schema 校验在 Decode 前（`runnable_tool.go:164-166`） |
| `tools.NewDispatcher` / `DispatchConfig` | `NewDispatcher(*Registry, DispatchConfig) (*Dispatcher, error)`；`DispatchConfig{Timeout time.Duration; ReturnArgumentErrors bool; ...}`；`(*Dispatcher).Tools() []interfaces.Tool`、`Invoke(ctx, ToolRequest{Name, Input})` |
| `builder.AgentOutput` | `struct{ Result interface{}; State core.State; Metadata map[string]interface{}; ... }` |
| `builder.NewSimpleBuilder` | `func NewSimpleBuilder(llm.Client) *SimpleAgentBuilder`（`= AgentBuilder[any, *core.AgentState]`）；`WithDurableScope`、`WithInterruptedModelResend`、`WithTools`、`WithMaxIterations`、`Build()`；`(*ConfigurableAgent).AsRunnable() *core.StreamRunnable[any, *AgentOutput]`、`Shutdown(ctx) error` |
| `llm.Client` / `ToolStreamClient` | `CompleteStream(ctx, *CompletionRequest) (<-chan *StreamChunk, error)`、`Provider() constants.Provider`；`CompleteToolsStream(ctx, *CompletionRequest, []interfaces.Tool)` |
| `llm.StreamChunk` / `ToolCall` | `StreamChunk{ToolCalls []ToolCall; Content, Delta, FinishReason string; Usage *Usage; Done bool; Error error; ...}`；`ToolCall{ID, Type string; Function struct{ Name, Arguments string }}` |
| `durable.*` | `Do(ctx, id string, input json.RawMessage, policy OperationPolicy, run func(context.Context) (json.RawMessage, error)) (*OperationRecord, error)`；`OperationPolicy{MaxAttempts int; RetryFailed bool; Reconcile func(ctx) (json.RawMessage, bool, error); Resumable, RetryInterrupted bool}`；`AwaitInput(ctx, id string, info json.RawMessage, validate func(json.RawMessage) error) (json.RawMessage, error)`；`InspectOperation(ctx, id)`；`OperationAttempt(ctx) int`；`OperationError{ID, Kind string}`；`ErrApprovalRequired`、`ErrOperationAttention`；`NewFileStore(dir)`；`Store` 含 `Lock/Load/Save/SaveAll`；`Snapshot{Version, Plan, RunID string; Next int; Data json.RawMessage; Waiting, Approved bool; Key string}` |
| `budget.*` | `Config{RootKey, Version string; MaxModelRequests int; DeadlineUnixMilli int64; ...}`；`New(Config)`、`NewDurable(Config, durable.Store)`、`WithController(ctx, c)`、`(*Controller).Snapshot(ctx)` |
| `refund.*` | `Service{Store, Authorizer, Policy, Now}`（Task 13 新增 `OnRegister`）；`Register`、`Approve(ctx, req, actor)`、`Refund(ctx, key, req)`；`Store.Transaction(ctx, func(Transaction) error)` |

## 文件结构

| 文件 | 职责 | 任务 |
|---|---|---|
| `testing/mocks/scripted_client.go`（新） | `ScriptedClient`、`ScriptedRule`、`ScriptedReply`、`TextReply`、`ToolCallReply`、`ToolCall`、`RequestContains` | 1 |
| `multiagent/doc.go`（新） | 包文档（旧包同名不兼容、返回的图只用于编译） | 2 |
| `multiagent/step.go`（新） | `Step`、`parallelSpec`、`AgentFactory`、`AgentStep`、会话 Runnable、名称与 nil 校验 | 2 |
| `multiagent/route.go`（新） | `Finish`、`ErrInvalidRoute`、`GuardRoute` | 3 |
| `multiagent/parallel.go`（新） | `ParallelConfig`、`ParallelStep`、`AddStep` | 4 |
| `multiagent/sequential.go`（新） | `newGraph`、`chain`、`Sequential` | 5 |
| `multiagent/loop.go`（新） | `LoopConfig`、`Loop`、`addFallback` | 6 |
| `multiagent/router.go`（新） | `RouterConfig`、`RouterSupervisor` | 7 |
| `multiagent/text.go`（新） | `Turn`、`Conversation`、`TextOption`、`TextStep`、`WithRender`、`WithLastTurns`、`CloneConversation`、`MergeTurns`、`TextNext` | 8 |
| `multiagent/supervisor_tools.go`（新） | `Member`、`SupervisorTools` | 9 |
| `multiagent/*_test.go`（新） | 夹具（`fixtures_test.go`、`env_test.go`、`refund_fixture_test.go`）与各任务测试 | 2–12、14 |
| `scripts/apicheck/main.go`、`main_test.go`、`docs/architecture/IMPORT_LAYERING.md` | 导入规则 | 10 |
| `workflow/refund/service.go`、`service_test.go`、`README.md` | `Service.OnRegister` 登记钩子 | 13 |
| `examples/multi-agent-patterns/`（新） | 离线示例与验收测试 | 15 |
| `docs/guides/MULTI_AGENT_PATTERNS.md`（新）及 README、组合指南、架构、Eino 对照、定位文档、CHANGELOG、`examples/README.md` | 文档 | 16 |

---

### Task 1: testing/mocks.ScriptedClient

**Files:**
- Create: `testing/mocks/scripted_client.go`
- Test: `testing/mocks/scripted_client_test.go`

**Interfaces:**
- Consumes: `llm.ToolStreamClient`、`llm.CompletionRequest`、`llm.StreamChunk`、`llm.ToolCall`、`constants.ProviderCustom`（均为现有 API）
- Produces: `type ScriptedReply struct{ Text string; ToolCalls []llm.ToolCall }`；`type ScriptedRule struct{ Name string; Match func(*llm.CompletionRequest) bool; Reply ScriptedReply; Respond func(context.Context, *llm.CompletionRequest) (ScriptedReply, error) }`；`func NewScriptedClient(rules ...ScriptedRule) *ScriptedClient`；`func TextReply(name, text string) ScriptedRule`；`func ToolCallReply(name string, calls ...llm.ToolCall) ScriptedRule`；`func ToolCall(id, tool, arguments string) llm.ToolCall`；`func RequestContains(parts ...string) func(*llm.CompletionRequest) bool`；方法 `Calls() int`、`Hits(index int) int`、`Remaining() int`、`Requests() []llm.CompletionRequest`。文本回复发出 `{Delta: text}` 与 `{Done, FinishReason: "stop", Content: text, Usage}`；工具调用回复只发一个 `{Done, FinishReason: "tool_calls", ToolCalls, Usage}`。

- [ ] **Step 1: 写失败测试**

替身必须先于所有场景测试与示例实现。测试覆盖：按顺序回复文本与结构化工具调用、严格模式拒绝额外请求、断言不成立时不消费规则、`Respond` 按请求内容回答、请求历史深拷贝、`Respond` 阻塞到 ctx 结束。

`testing/mocks/scripted_client_test.go`：

```go
package mocks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/llm"
)

func drain(t *testing.T, ch <-chan *llm.StreamChunk) *llm.StreamChunk {
	t.Helper()
	var last *llm.StreamChunk
	for chunk := range ch {
		last = chunk
	}
	require.NotNil(t, last)
	require.True(t, last.Done)
	return last
}

func ask(text string) *llm.CompletionRequest {
	return &llm.CompletionRequest{Messages: []llm.Message{{Role: "user", Content: text}}}
}

func TestScriptedClientRepliesInOrderAndIsStrict(t *testing.T) {
	ctx := context.Background()
	c := NewScriptedClient(
		ScriptedRule{Name: "分流", Match: RequestContains("工单"), Reply: ScriptedReply{Text: "refund"}},
		ToolCallReply("调用", ToolCall("c1", "lookup", `{"task":"x"}`)),
	)
	ch, err := c.CompleteToolsStream(ctx, ask("工单：退款"), nil)
	require.NoError(t, err)
	last := drain(t, ch)
	require.Equal(t, "stop", last.FinishReason)
	require.Equal(t, "refund", last.Content)

	ch, err = c.CompleteStream(ctx, ask("继续"))
	require.NoError(t, err)
	last = drain(t, ch)
	require.Equal(t, "tool_calls", last.FinishReason)
	require.Equal(t, "c1", last.ToolCalls[0].ID)
	require.Equal(t, "lookup", last.ToolCalls[0].Function.Name)

	_, err = c.CompleteStream(ctx, ask("多余"))
	require.ErrorContains(t, err, "没有可用规则")
	require.Equal(t, 3, c.Calls())
	require.Equal(t, 1, c.Hits(0))
	require.Equal(t, 1, c.Hits(1))
	require.Zero(t, c.Remaining())
}

func TestScriptedClientRejectsMismatchedRequest(t *testing.T) {
	c := NewScriptedClient(ScriptedRule{Name: "需要订单号", Match: RequestContains("A1")})
	_, err := c.CompleteStream(context.Background(), ask("没有订单号"))
	require.ErrorContains(t, err, "需要订单号")
	require.Equal(t, 0, c.Hits(0))
	require.Equal(t, 1, c.Remaining())
}

func TestScriptedClientRespondAndDeepCopiedHistory(t *testing.T) {
	c := NewScriptedClient(ScriptedRule{Name: "回声", Respond: func(_ context.Context, req *llm.CompletionRequest) (ScriptedReply, error) {
		return ScriptedReply{Text: "收到：" + req.Messages[0].Content}, nil
	}})
	req := ask("你好")
	ch, err := c.CompleteStream(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "收到：你好", drain(t, ch).Content)
	req.Messages[0].Content = "被改写"
	history := c.Requests()
	require.Equal(t, "你好", history[0].Messages[0].Content)
	history[0].Messages[0].Content = "再次改写"
	require.Equal(t, "你好", c.Requests()[0].Messages[0].Content)
}

func TestScriptedClientRespondCanBlockUntilCanceled(t *testing.T) {
	started := make(chan struct{})
	c := NewScriptedClient(ScriptedRule{Name: "挂起", Respond: func(ctx context.Context, _ *llm.CompletionRequest) (ScriptedReply, error) {
		close(started)
		<-ctx.Done()
		return ScriptedReply{}, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.CompleteStream(ctx, ask("q"))
		done <- err
	}()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 -run '^(TestScriptedClientRepliesInOrderAndIsStrict|TestScriptedClientRejectsMismatchedRequest|TestScriptedClientRespondAndDeepCopiedHistory|TestScriptedClientRespondCanBlockUntilCanceled)$' ./testing/mocks/`
Expected: FAIL，编译错误 `undefined: NewScriptedClient`

- [ ] **Step 3: 写实现**

每个角色使用独立实例；同一实例的请求按到达顺序消费规则。`Respond` 在锁外调用，阻塞的规则不会挡住其他实例。

`testing/mocks/scripted_client.go`：

```go
package mocks

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

var _ llm.ToolStreamClient = (*ScriptedClient)(nil)

// ScriptedReply 是一条规则给出的回复。ToolCalls 非空时回复结构化工具调用（放在成功终止块中），否则回复 Text。
type ScriptedReply struct {
	Text      string
	ToolCalls []llm.ToolCall
}

// ScriptedRule 是一条“请求断言 → 回复”规则。Match 为 nil 时接受任意请求；
// Respond 非 nil 时按请求内容生成回复（可阻塞到 ctx 结束以模拟挂起的请求），否则使用 Reply。
type ScriptedRule struct {
	Name    string
	Match   func(*llm.CompletionRequest) bool
	Reply   ScriptedReply
	Respond func(context.Context, *llm.CompletionRequest) (ScriptedReply, error)
}

// ScriptedClient 按顺序消费规则：每次请求取下一条未用的规则，断言不成立或规则已用完时返回错误（严格模式），
// 测试据此发现意外的重放。请求历史深拷贝保存。可并发调用，但同一实例的请求按到达顺序消费规则，
// 并行分支应各用独立实例。
type ScriptedClient struct {
	mu       sync.Mutex
	rules    []ScriptedRule
	next     int
	hits     []int
	requests []llm.CompletionRequest
}

// NewScriptedClient 创建按 rules 顺序回复的客户端。
func NewScriptedClient(rules ...ScriptedRule) *ScriptedClient {
	return &ScriptedClient{rules: slices.Clone(rules), hits: make([]int, len(rules))}
}

// TextReply 返回回复文本的规则。
func TextReply(name, text string) ScriptedRule {
	return ScriptedRule{Name: name, Reply: ScriptedReply{Text: text}}
}

// ToolCallReply 返回回复结构化工具调用的规则。
func ToolCallReply(name string, calls ...llm.ToolCall) ScriptedRule {
	return ScriptedRule{Name: name, Reply: ScriptedReply{ToolCalls: calls}}
}

// ToolCall 构造带稳定 ID 的工具调用。
func ToolCall(id, tool, arguments string) llm.ToolCall {
	call := llm.ToolCall{ID: id, Type: "function"}
	call.Function.Name = tool
	call.Function.Arguments = arguments
	return call
}

// RequestContains 断言请求的全部消息中依次出现 parts 中的每一段。
func RequestContains(parts ...string) func(*llm.CompletionRequest) bool {
	return func(req *llm.CompletionRequest) bool {
		var all strings.Builder
		for _, m := range req.Messages {
			all.WriteString(m.Content)
			all.WriteString("\n")
		}
		text := all.String()
		for _, p := range parts {
			i := strings.Index(text, p)
			if i < 0 {
				return false
			}
			text = text[i+len(p):]
		}
		return true
	}
}

func (c *ScriptedClient) Provider() constants.Provider { return constants.ProviderCustom }

func (c *ScriptedClient) CompleteStream(ctx context.Context, req *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return c.CompleteToolsStream(ctx, req, nil)
}

func (c *ScriptedClient) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, _ []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	if req == nil {
		return nil, fmt.Errorf("ScriptedClient: 请求为 nil")
	}
	c.mu.Lock()
	c.requests = append(c.requests, copyRequest(req))
	call := len(c.requests)
	if c.next >= len(c.rules) {
		c.mu.Unlock()
		return nil, fmt.Errorf("ScriptedClient: 第 %d 次请求没有可用规则", call)
	}
	index := c.next
	rule := c.rules[index]
	if rule.Match != nil && !rule.Match(req) {
		c.mu.Unlock()
		return nil, fmt.Errorf("ScriptedClient: 第 %d 次请求不满足规则 %d（%s）的断言", call, index, rule.Name)
	}
	c.next++
	c.hits[index]++
	c.mu.Unlock()
	reply := rule.Reply
	if rule.Respond != nil {
		var err error
		if reply, err = rule.Respond(ctx, req); err != nil {
			return nil, err
		}
	}
	return replyStream(ctx, reply), nil
}

func replyStream(ctx context.Context, reply ScriptedReply) <-chan *llm.StreamChunk {
	usage := &llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}
	chunks := []*llm.StreamChunk{{Done: true, FinishReason: "stop", Usage: usage}}
	switch {
	case len(reply.ToolCalls) > 0:
		chunks = []*llm.StreamChunk{{Done: true, FinishReason: "tool_calls", ToolCalls: slices.Clone(reply.ToolCalls), Usage: usage}}
	case reply.Text != "":
		chunks = []*llm.StreamChunk{{Delta: reply.Text}, {Done: true, FinishReason: "stop", Content: reply.Text, Usage: usage}}
	}
	out := make(chan *llm.StreamChunk)
	go func() {
		defer close(out)
		for _, chunk := range chunks {
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func copyRequest(req *llm.CompletionRequest) llm.CompletionRequest {
	out := *req
	out.Stop = slices.Clone(req.Stop)
	out.Messages = make([]llm.Message, len(req.Messages))
	for i, m := range req.Messages {
		m.ToolCalls = slices.Clone(m.ToolCalls)
		if m.ProviderState != nil {
			state := *m.ProviderState
			state.Data = slices.Clone(state.Data)
			m.ProviderState = &state
		}
		out.Messages[i] = m
	}
	if req.Reasoning != nil {
		reasoning := *req.Reasoning
		out.Reasoning = &reasoning
	}
	return out
}

// Calls 返回收到的请求总数（含被拒绝的请求）。
func (c *ScriptedClient) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

// Hits 返回第 index 条规则被匹配的次数。
func (c *ScriptedClient) Hits(index int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[index]
}

// Remaining 返回尚未使用的规则数。
func (c *ScriptedClient) Remaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rules) - c.next
}

// Requests 返回请求历史的深拷贝，应在执行结束后读取。
func (c *ScriptedClient) Requests() []llm.CompletionRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]llm.CompletionRequest, len(c.requests))
	for i := range c.requests {
		out[i] = copyRequest(&c.requests[i])
	}
	return out
}
```
- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -count=1 -run '^(TestScriptedClientRepliesInOrderAndIsStrict|TestScriptedClientRejectsMismatchedRequest|TestScriptedClientRespondAndDeepCopiedHistory|TestScriptedClientRespondCanBlockUntilCanceled)$' ./testing/mocks/`
Expected: PASS

- [ ] **Step 5: 全包回归、vet 与格式检查**

Run: `go test -race -count=1 ./multiagent/... ./testing/mocks/... && go vet ./multiagent/... ./testing/mocks/... && gofumpt -l testing/mocks/scripted_client.go testing/mocks/scripted_client_test.go && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' testing/mocks/scripted_client.go testing/mocks/scripted_client_test.go`
Expected: 测试 PASS，vet 无输出，gofumpt 与 gci 不列出任何文件

- [ ] **Step 6: 提交**

```bash
git add testing/mocks/scripted_client.go testing/mocks/scripted_client_test.go
git commit -m "test(mocks): 新增按脚本回复的 ScriptedClient 替身"
```


### Task 2: Step 与 AgentStep

**Files:**
- Create: `multiagent/doc.go`
- Create: `multiagent/step.go`
- Create: `multiagent/fixtures_test.go`（单元测试共用夹具）
- Test: `multiagent/step_test.go`

**Interfaces:**
- Consumes: Task 1 的 `mocks.NewScriptedClient`、`mocks.TextReply`、`mocks.ScriptedRule`、`mocks.ScriptedReply`、`mocks.RequestContains`；`workflow.Adapt`、`tools.OwnedRunnable`、`builder.AgentOutput`
- Produces: `type Step[T any] struct{ Name string; Run core.Runnable[T, T]; parallel *parallelSpec[T] }`；`type parallelSpec[T any] struct{ steps []Step[T]; clone func(T) T; merge func(context.Context, T, []T) (T, error) }`；`type AgentFactory func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error)`；`func AgentStep[T any](name string, agent AgentFactory, in func(T) (any, error), out func(T, *builder.AgentOutput) (T, error)) (Step[T], error)`；私有 `validName(string) bool`、`isNil(any) bool`、`checkStep[T](Step[T]) error`、`sessionRunnable(AgentFactory)`、常量 `sessionCleanupTimeout = 5s`。测试夹具（同包测试共用）：`type record struct{ Log []string; N int }`、`mark(name) Step[record]`、`cloneRecord(record) record`、`type sessions struct{ opened, closed atomic.Int32 }` 及方法 `fake(func(context.Context, any) (any, error)) AgentFactory`、`scripted(llm.Client, scope string, configure func(*builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder) AgentFactory`，`withTools(t, tools.DispatchConfig, ...interfaces.Tool)`、`countingTool(t, name, result string, *atomic.Int32) interfaces.Tool`；`step_test.go` 中的 `type ticket struct{ Text, Answer string }`、`askText`、`keepAnswer`、`runOne`。

- [ ] **Step 1: 写夹具与失败测试**

夹具供后续所有任务的测试使用（`withTools`、`countingTool` 从 Task 9 起才被用到）。

`multiagent/fixtures_test.go`：

```go
package multiagent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/tools"
)

// record 是单元测试用的类型化状态：Log 记录执行过的 Step，N 供路由读取。
type record struct {
	Log []string
	N   int
}

// mark 返回把自身名字追加到 Log 的 Step。
func mark(name string) Step[record] {
	return Step[record]{Name: name, Run: core.NewRunnableFunc(func(_ context.Context, r record) (record, error) {
		r.Log = append(append([]string(nil), r.Log...), name)
		r.N++
		return r, nil
	})}
}

func cloneRecord(r record) record { r.Log = append([]string(nil), r.Log...); return r }

// sessions 统计会话的创建与关闭次数。
type sessions struct{ opened, closed atomic.Int32 }

// fake 返回不调用模型的会话工厂：answer 根据输入计算 Result。
func (s *sessions) fake(answer func(context.Context, any) (any, error)) AgentFactory {
	return func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		s.opened.Add(1)
		return tools.OwnedRunnable[any, *builder.AgentOutput]{
			Runnable: core.NewRunnableFunc(func(ctx context.Context, in any) (*builder.AgentOutput, error) {
				result, err := answer(ctx, in)
				if err != nil {
					return nil, err
				}
				return &builder.AgentOutput{Result: result}, nil
			}),
			Close: func(context.Context) error { s.closed.Add(1); return nil },
		}, nil
	}
}

// scripted 返回真实 Builder 驱动的会话工厂；同一角色的 ScriptedClient 在各次会话之间共享以保留脚本进度。
func (s *sessions) scripted(client llm.Client, scope string, configure func(*builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder) AgentFactory {
	return func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		b := builder.NewSimpleBuilder(client)
		if scope != "" {
			b = b.WithDurableScope(scope)
		}
		if configure != nil {
			b = configure(b)
		}
		agent, err := b.Build()
		if err != nil {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
		}
		s.opened.Add(1)
		return tools.OwnedRunnable[any, *builder.AgentOutput]{
			Runnable: agent.AsRunnable(),
			Close:    func(ctx context.Context) error { s.closed.Add(1); return agent.Shutdown(ctx) },
		}, nil
	}
}

// withTools 把工具经独立 Dispatcher 包装后交给 Agent，满足持久接入契约。
func withTools(t *testing.T, cfg tools.DispatchConfig, list ...interfaces.Tool) func(*builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder {
	t.Helper()
	reg := tools.NewRegistry()
	for _, tool := range list {
		require.NoError(t, reg.Register(tool))
	}
	d, err := tools.NewDispatcher(reg, cfg)
	require.NoError(t, err)
	return func(b *builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder {
		return b.WithTools(d.Tools()...).WithMaxIterations(4)
	}
}

// countingTool 返回计数的只读查询工具。
func countingTool(t *testing.T, name, result string, calls *atomic.Int32) interfaces.Tool {
	t.Helper()
	tool, err := tools.NewFunctionToolBuilder(name).WithDescription("查询 " + name).
		WithArgsSchema(`{"type":"object","properties":{}}`).
		WithFunction(func(context.Context, map[string]interface{}) (interface{}, error) {
			calls.Add(1)
			return result, nil
		}).Build()
	require.NoError(t, err)
	return tool
}
```

`multiagent/step_test.go`：

```go
package multiagent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

type ticket struct {
	Text   string
	Answer string
}

func askText(t ticket) (any, error) { return t.Text, nil }

func keepAnswer(t ticket, o *builder.AgentOutput) (ticket, error) {
	t.Answer, _ = o.Result.(string)
	return t, nil
}

// runOne 把单个 Step 放进 START → s → END 的图并执行。
func runOne(t *testing.T, s Step[ticket], in ticket) (ticket, error) {
	t.Helper()
	g := workflow.NewGraph[ticket]()
	require.NoError(t, g.AddRunnable(s.Name, s.Run))
	require.NoError(t, g.AddEdge(workflow.START, s.Name))
	require.NoError(t, g.AddEdge(s.Name, workflow.END))
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	return run.Invoke(context.Background(), in)
}

func TestAgentStepOpensAndClosesSessionPerExecution(t *testing.T) {
	var s sessions
	step, err := AgentStep("answer", s.fake(func(_ context.Context, in any) (any, error) { return "答：" + in.(string), nil }), askText, keepAnswer)
	require.NoError(t, err)
	out, err := runOne(t, step, ticket{Text: "退款"})
	require.NoError(t, err)
	require.Equal(t, "答：退款", out.Answer)
	_, err = runOne(t, step, ticket{Text: "物流"})
	require.NoError(t, err)
	require.EqualValues(t, 2, s.opened.Load())
	require.EqualValues(t, 2, s.closed.Load())
}

func TestAgentStepValidation(t *testing.T) {
	var s sessions
	agent := s.fake(func(context.Context, any) (any, error) { return "", nil })
	for name, build := range map[string]func() (Step[ticket], error){
		"空名称":      func() (Step[ticket], error) { return AgentStep("", agent, askText, keepAnswer) },
		"首尾空白":     func() (Step[ticket], error) { return AgentStep(" a", agent, askText, keepAnswer) },
		"保留名":      func() (Step[ticket], error) { return AgentStep(workflow.END, agent, askText, keepAnswer) },
		"agent 为空": func() (Step[ticket], error) { return AgentStep("a", nil, askText, keepAnswer) },
		"in 为空":    func() (Step[ticket], error) { return AgentStep[ticket]("a", agent, nil, keepAnswer) },
		"out 为空":   func() (Step[ticket], error) { return AgentStep("a", agent, askText, nil) },
	} {
		_, err := build()
		require.Error(t, err, name)
	}
}

func TestAgentStepFactoryErrorStillCloses(t *testing.T) {
	closed := 0
	factory := func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		return tools.OwnedRunnable[any, *builder.AgentOutput]{Close: func(context.Context) error { closed++; return nil }}, errors.New("会话创建失败")
	}
	step, err := AgentStep("answer", factory, askText, keepAnswer)
	require.NoError(t, err)
	_, err = runOne(t, step, ticket{Text: "q"})
	require.ErrorContains(t, err, "会话创建失败")
	require.Equal(t, 1, closed)
}

func TestAgentStepRejectsIncompleteSessions(t *testing.T) {
	var typedNil *core.RunnableFunc[any, *builder.AgentOutput]
	nilAnswer := core.NewRunnableFunc(func(context.Context, any) (*builder.AgentOutput, error) { return nil, nil })
	closeOK := func(context.Context) error { return nil }
	for name, owned := range map[string]tools.OwnedRunnable[any, *builder.AgentOutput]{
		"Close 为空":    {Runnable: nilAnswer},
		"typed nil":   {Runnable: typedNil, Close: closeOK},
		"返回 nil, nil": {Runnable: nilAnswer, Close: closeOK},
	} {
		factory := func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) { return owned, nil }
		step, err := AgentStep("answer", factory, askText, keepAnswer)
		require.NoError(t, err)
		_, err = runOne(t, step, ticket{Text: "q"})
		require.Error(t, err, name)
		require.ErrorContains(t, err, `node "answer"`, name)
	}
}

func TestAgentStepJoinsExecutionAndCloseErrors(t *testing.T) {
	factory := func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		return tools.OwnedRunnable[any, *builder.AgentOutput]{
			Runnable: core.NewRunnableFunc(func(context.Context, any) (*builder.AgentOutput, error) { return nil, errors.New("模型失败") }),
			Close:    func(context.Context) error { return errors.New("关闭失败") },
		}, nil
	}
	step, err := AgentStep("answer", factory, askText, keepAnswer)
	require.NoError(t, err)
	_, err = runOne(t, step, ticket{Text: "q"})
	require.ErrorContains(t, err, "模型失败")
	require.ErrorContains(t, err, "关闭失败")
}

func TestAgentStepOutErrorNamesNode(t *testing.T) {
	var s sessions
	reject := func(ticket, *builder.AgentOutput) (ticket, error) {
		return ticket{}, errors.New("合规检查不通过")
	}
	step, err := AgentStep("compliance", s.fake(func(context.Context, any) (any, error) { return "含敏感词", nil }), askText, reject)
	require.NoError(t, err)
	_, err = runOne(t, step, ticket{Text: "q"})
	require.ErrorContains(t, err, `node "compliance"`)
	require.ErrorContains(t, err, "合规检查不通过")
}

func TestAgentStepInErrorSendsNoModelRequest(t *testing.T) {
	client := mocks.NewScriptedClient(mocks.TextReply("不应发出", "x"))
	var s sessions
	needOrder := func(t ticket) (any, error) {
		if t.Text == "" {
			return nil, errors.New("缺少工单内容")
		}
		return t.Text, nil
	}
	step, err := AgentStep("extract", s.scripted(client, "", nil), needOrder, keepAnswer)
	require.NoError(t, err)
	_, err = runOne(t, step, ticket{})
	require.ErrorContains(t, err, "缺少工单内容")
	require.Zero(t, client.Calls())
	require.Zero(t, s.opened.Load(), "映射失败时不创建会话")
}

func TestAgentStepWithRealBuilder(t *testing.T) {
	client := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "回答", Match: mocks.RequestContains("订单 A1"), Reply: mocks.ScriptedReply{Text: "已查询"}})
	var s sessions
	step, err := AgentStep("answer", s.scripted(client, "", nil), askText, keepAnswer)
	require.NoError(t, err)
	out, err := runOne(t, step, ticket{Text: "订单 A1"})
	require.NoError(t, err)
	require.Equal(t, "已查询", out.Answer)
	require.Equal(t, 1, client.Calls())
	require.EqualValues(t, 1, s.closed.Load())
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 ./multiagent/`
Expected: FAIL，编译错误 `undefined: Step`、`undefined: AgentStep`

- [ ] **Step 3: 写实现**

生命周期与 `tools/runnable_tool.go:173-191` 一致：`Close` 非 nil 就在结束时调用（Factory 同时返回错误也调用），用 `context.WithoutCancel` 加 5s；Factory 错误、`Close` 为 nil、Runnable 为 nil（含 typed nil）、Agent 返回 `(nil, nil)` 都使该步失败；执行错误与 `Close` 错误用 `errors.Join` 合并。会话 Runnable 出错后仍读到流关闭再返回，保证 `Close` 在会话结束之后调用；缺少 Done、重复 Done 与取消后的排空交给 `workflow.Adapt` 内的 `consumeNode`。

`multiagent/doc.go`：

```go
// Package multiagent 在 workflow 之上提供多 Agent 预置模式：Sequential、Loop、RouterSupervisor
// 三个图构造器，ParallelStep 组合件，工具型 Supervisor（SupervisorTools）与文本接力便捷层。
//
// 本包与 v0.17 删除的旧 multiagent 包同名，但 API 与语义不兼容，旧代码不能直接迁移；
// 它不引入运行时、调度器或消息投递，所有执行仍走 workflow、builder 与 tools。
//
// 构造器返回的 *workflow.Graph 只应直接交给 Compile 或 CompileDurable：构造器已按拓扑算好 MaxSteps，
// 继续 AddNode 不会报错，但步数上限不再准确。需要组合时用 ParallelStep，
// 或用 workflow.NewGraph 手写拓扑并以 AddStep、GuardRoute 复用本包的 Step 与兜底路由。
package multiagent
```

`multiagent/step.go`：

```go
package multiagent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

// sessionCleanupTimeout 与 tools.RunnableToolConfig.CleanupTimeout 的默认值一致。
const sessionCleanupTimeout = 5 * time.Second

// Step 是图中的一步。Run 必须非 nil；ParallelStep 返回的 Step 由私有字段描述并行分支，忽略 Run。
type Step[T any] struct {
	Name     string
	Run      core.Runnable[T, T]
	parallel *parallelSpec[T]
}

// parallelSpec 是 ParallelStep 复制保存的分支配置，由 AddStep 注册为并行节点。
type parallelSpec[T any] struct {
	steps []Step[T]
	clone func(T) T
	merge func(context.Context, T, []T) (T, error)
}

// AgentFactory 为每次节点执行新建一个 Agent 会话，避免循环与重复调用共享可变记忆。
// 并行分支与并发运行会同时调用它，实现必须并发安全。
type AgentFactory func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error)

// AgentStep 把 Agent 接成一步：in 从状态取任务输入，out 把输出写回状态。
// in 返回 error 时在发出模型请求前失败，可用于拒绝不完整的状态；
// out 返回 error 时该步失败，可用于把“合规检查不通过”变成节点错误。
// in 与 out 必须是纯函数：in 的结果进入模型请求意图，out 会随节点重入再次执行。
func AgentStep[T any](name string, agent AgentFactory,
	in func(T) (any, error), out func(T, *builder.AgentOutput) (T, error),
) (Step[T], error) {
	if !validName(name) {
		return Step[T]{}, fmt.Errorf("Step 名称 %q 无效", name)
	}
	if agent == nil || in == nil || out == nil {
		return Step[T]{}, fmt.Errorf("AgentStep %q 需要 agent、in 与 out", name)
	}
	run, err := workflow.Adapt(sessionRunnable(agent), in, out)
	if err != nil {
		return Step[T]{}, err
	}
	return Step[T]{Name: name, Run: run}, nil
}

// sessionRunnable 把每次返回新会话的 Factory 包装成固定的 Runnable，供 workflow.Adapt 使用。
// 缺少 Done、重复 Done 与取消后的排空由 Adapt 内部的节点消费逻辑处理。
func sessionRunnable(factory AgentFactory) *core.StreamRunnable[any, *builder.AgentOutput] {
	return core.NewStreamRunnable(core.StreamModeSteps, func(ctx context.Context, input any, emit func(core.StreamChunk[*builder.AgentOutput]) error) (err error) {
		owned, err := factory(ctx)
		if owned.Close != nil {
			defer func() {
				cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sessionCleanupTimeout)
				defer cancel()
				err = errors.Join(err, owned.Close(cleanCtx))
			}()
		}
		if err != nil {
			return err
		}
		if owned.Close == nil || isNil(owned.Runnable) {
			return errors.New("会话需要 Runnable 与 Close")
		}
		chunks, err := owned.Runnable.Stream(ctx, input)
		if err != nil {
			return err
		}
		if chunks == nil {
			return errors.New("会话返回了 nil 流")
		}
		var failure error
		// 出错后仍读到流关闭，保证 Close 在会话结束之后调用。
		for chunk := range chunks {
			switch {
			case failure != nil:
			case chunk.Error != nil:
				failure = chunk.Error
			case chunk.Done && chunk.Data == nil:
				failure = errors.New("会话没有返回输出")
			default:
				failure = emit(chunk)
			}
		}
		return failure
	})
}

func validName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name && name != workflow.START && name != workflow.END
}

// isNil 把 typed nil 也视为 nil，与 tools 校验 Runnable 的方式一致。
func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

func checkStep[T any](s Step[T]) error {
	if !validName(s.Name) {
		return fmt.Errorf("Step 名称 %q 无效", s.Name)
	}
	if isNil(s.Run) {
		return fmt.Errorf("Step %q 缺少 Run", s.Name)
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -count=1 ./multiagent/`
Expected: PASS（8 个 `TestAgentStep*`）

- [ ] **Step 5: vet 与格式检查**

Run: `go vet ./multiagent/... && gofumpt -l multiagent && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent`
Expected: 无输出

- [ ] **Step 6: 提交**

```bash
git add multiagent/doc.go multiagent/step.go multiagent/fixtures_test.go multiagent/step_test.go
git commit -m "feat(multiagent): 新增 Step 与按会话执行的 AgentStep"
```


### Task 3: GuardRoute、Finish 与 ErrInvalidRoute

**Files:**
- Create: `multiagent/route.go`
- Test: `multiagent/route_test.go`

**Interfaces:**
- Consumes: Task 2 的 `record`；`workflow.Router`、`workflow.END`、`workflow.ErrMaxSteps`
- Produces: `const Finish = "FINISH"`；`var ErrInvalidRoute = errors.New("路由决策无效")`；`func GuardRoute[T any](next func(context.Context, T) (string, error), members []string, fallback string) (route workflow.Router[T], targets []string, err error)`，targets 依次为成员、`workflow.END`、fallback（非空时）。测试辅助 `fixed(name string, err error) func(context.Context, record) (string, error)` 供 Task 7 复用。

- [ ] **Step 1: 写失败测试**

覆盖构造校验表各条、targets 恰为成员 + END + fallback、运行时四步顺序（含 `Next` 返回 Supervisor 名、兜底名、`"__end__"`），以及 `ErrMaxSteps` 等其他错误即使有兜底也原样返回。

`multiagent/route_test.go`：

```go
package multiagent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/workflow"
)

func fixed(name string, err error) func(context.Context, record) (string, error) {
	return func(context.Context, record) (string, error) { return name, err }
}

func TestGuardRouteValidation(t *testing.T) {
	next := fixed("a", nil)
	for name, tc := range map[string]struct {
		next     func(context.Context, record) (string, error)
		members  []string
		fallback string
	}{
		"next 为空":    {nil, []string{"a"}, ""},
		"没有成员":       {next, nil, ""},
		"成员为空":       {next, []string{""}, ""},
		"成员重复":       {next, []string{"a", "a"}, ""},
		"成员为 FINISH": {next, []string{Finish}, ""},
		"成员为 END":    {next, []string{workflow.END}, ""},
		"兜底与成员同名":    {next, []string{"a"}, "a"},
	} {
		_, _, err := GuardRoute(tc.next, tc.members, tc.fallback)
		require.Error(t, err, name)
	}
}

func TestGuardRouteTargets(t *testing.T) {
	_, targets, err := GuardRoute(fixed("a", nil), []string{"a", "b"}, "")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", workflow.END}, targets)
	_, targets, err = GuardRoute(fixed("a", nil), []string{"a", "b"}, "human")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", workflow.END, "human"}, targets)
}

func TestGuardRouteOrder(t *testing.T) {
	invalid := fmt.Errorf("%w：无法识别", ErrInvalidRoute)
	other := errors.New("存储不可用")
	limit := fmt.Errorf("%w: review loop limit 3", workflow.ErrMaxSteps)
	for _, tc := range []struct {
		name     string
		next     func(context.Context, record) (string, error)
		fallback string
		want     string
		wantErr  error
	}{
		{"无效错误转兜底", fixed("a", invalid), "human", "human", nil},
		{"无效错误无兜底", fixed("a", invalid), "", "", ErrInvalidRoute},
		{"其他错误原样返回", fixed("a", other), "human", "", other},
		{"ErrMaxSteps 原样返回", fixed("", limit), "human", "", workflow.ErrMaxSteps},
		{"FINISH 映射为 END", fixed(Finish, nil), "human", workflow.END, nil},
		{"成员", fixed("b", nil), "human", "b", nil},
		{"未知名转兜底", fixed("x", nil), "human", "human", nil},
		{"未知名无兜底", fixed("x", nil), "", "", ErrInvalidRoute},
		{"END 字面值按无效", fixed(workflow.END, nil), "", "", ErrInvalidRoute},
		{"Supervisor 名按无效", fixed("sup", nil), "", "", ErrInvalidRoute},
		{"兜底名按无效", fixed("human", nil), "human", "human", nil},
	} {
		route, _, err := GuardRoute(tc.next, []string{"a", "b"}, tc.fallback)
		require.NoError(t, err)
		got, err := route(context.Background(), record{})
		if tc.wantErr != nil {
			require.ErrorIs(t, err, tc.wantErr, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, got, tc.name)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 -run '^(TestGuardRouteValidation|TestGuardRouteTargets|TestGuardRouteOrder)$' ./multiagent/`
Expected: FAIL，编译错误 `undefined: GuardRoute`

- [ ] **Step 3: 写实现**

成员集合只含成员，不含 Supervisor 名、兜底名与 `END` 字面值；不能用分支目标列表代替，否则兜底名与 `END` 会被 workflow 放行。

`multiagent/route.go`：

```go
package multiagent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kart-io/goagent/workflow"
)

// Finish 是路由函数表示“结束”的返回值，GuardRoute 把它映射为 workflow.END。
const Finish = "FINISH"

// ErrInvalidRoute 表示路由或结束判断的输入无效（如模型输出无法识别）。
// 配置了 Fallback 时转入兜底 Step，否则运行失败。
var ErrInvalidRoute = errors.New("路由决策无效")

// GuardRoute 把返回成员名或 Finish 的 next 包装成带兜底的 workflow.Router。fallback 为空表示不兜底。
// targets 是应交给 AddBranch 的全部目标（成员、END 与 fallback）。运行时依次：
// next 返回包装 ErrInvalidRoute 的错误且有兜底时转兜底，其他错误原样返回；Finish 映射为 END；
// 不在成员集合中的名字（含 Supervisor 名、兜底名与 END 字面值）按无效处理；否则返回该成员名。
// next 必须是纯函数：持久恢复会对同一份已提交状态重新路由。
func GuardRoute[T any](next func(context.Context, T) (string, error), members []string, fallback string) (route workflow.Router[T], targets []string, err error) {
	if next == nil {
		return nil, nil, errors.New("GuardRoute 需要 next")
	}
	if len(members) == 0 {
		return nil, nil, errors.New("GuardRoute 需要至少一个成员")
	}
	set := make(map[string]bool, len(members))
	for _, m := range members {
		if m == "" || m == Finish || m == workflow.END || set[m] {
			return nil, nil, fmt.Errorf("GuardRoute 的成员名 %q 为空、保留或重复", m)
		}
		set[m] = true
	}
	if fallback != "" && (set[fallback] || fallback == Finish || fallback == workflow.END) {
		return nil, nil, fmt.Errorf("兜底名 %q 不能与成员同名或使用保留名", fallback)
	}
	targets = append(slices.Clone(members), workflow.END)
	if fallback != "" {
		targets = append(targets, fallback)
	}
	route = func(ctx context.Context, state T) (string, error) {
		name, err := next(ctx, state)
		if err != nil {
			if fallback != "" && errors.Is(err, ErrInvalidRoute) {
				return fallback, nil
			}
			return "", err
		}
		if name == Finish {
			return workflow.END, nil
		}
		if !set[name] {
			if fallback != "" {
				return fallback, nil
			}
			return "", fmt.Errorf("%w：%q 不是成员名或 %s", ErrInvalidRoute, name, Finish)
		}
		return name, nil
	}
	return route, targets, nil
}
```
- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -count=1 -run '^(TestGuardRouteValidation|TestGuardRouteTargets|TestGuardRouteOrder)$' ./multiagent/`
Expected: PASS

- [ ] **Step 5: 全包回归、vet 与格式检查**

Run: `go test -race -count=1 ./multiagent/... ./testing/mocks/... && go vet ./multiagent/... ./testing/mocks/... && gofumpt -l multiagent/route.go multiagent/route_test.go && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent/route.go multiagent/route_test.go`
Expected: 测试 PASS，vet 无输出，gofumpt 与 gci 不列出任何文件

- [ ] **Step 6: 提交**

```bash
git add multiagent/route.go multiagent/route_test.go
git commit -m "feat(multiagent): 新增带兜底的路由包装 GuardRoute"
```


### Task 4: ParallelStep 与 AddStep

**Files:**
- Create: `multiagent/parallel.go`
- Create: `multiagent/env_test.go`（持久测试环境与故障注入夹具）
- Test: `multiagent/parallel_test.go`

**Interfaces:**
- Consumes: Task 2 的 `Step`、`parallelSpec`、`checkStep`、`validName`、`record`、`mark`、`cloneRecord`；`workflow.AddParallel`、`workflow.Branch`、`workflow.ParallelConfig`
- Produces: `type ParallelConfig[T any] struct{ Steps []Step[T]; Clone func(T) T; Merge func(context.Context, T, []T) (T, error) }`；`func ParallelStep[T any](name string, p ParallelConfig[T]) (Step[T], error)`；`func AddStep[T any](g *workflow.Graph[T], s Step[T]) error`；`var errParallelRun`。持久夹具（`env_test.go`）：`errInjected`；`type fault` 及 `arm(repeat bool, match func(id string, s *durable.Snapshot) bool)`、`Hits() int`；`faultStore`；`operation(*durable.Snapshot) (durable.OperationRecord, bool)`；`checkpoint(*durable.Snapshot) (checkpointView, bool)`（`checkpointView{Node string; Routing bool}`）；`type env struct{ t; id, graphDir, budgetDir string; budget budget.Config; fault *fault; store durable.Store; controller *budget.Controller }`、`newEnv(t, root string) *env`、`(*env).restart()`、`renew(id string)`、`ctx() context.Context`；`compile[T](t, *env, *workflow.Graph[T], version string, approvals ...string) *workflow.DurableGraph[T]`；`inspect(t, *env, nodeKey, scope, id string) *durable.OperationRecord`；`keyed(AgentFactory, *atomic.Value) AgentFactory`。`parallel_test.go` 中的 `mergeLogs`、`lookups(t) Step[record]`（名为 `lookup`，分支 `kb`、`order`、`ship`）、`single[T](t, workflow.Config, Step[T]) *workflow.Graph[T]` 供后续任务复用。

- [ ] **Step 1: 写夹具与失败测试**

`env` 的运行 ID 与根预算 RootKey 相同（见偏差 2）；“重启”只重建存储对象与预算，复用目录，`fault` 跨重启保留。

`multiagent/env_test.go`：

```go
package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

var errInjected = errors.New("注入的保存失败")

// fault 让匹配的一次（repeat 为真时为每次）Save 失败，用来精确模拟“效果已发生、记录未落盘”时崩溃。
// 它跨“重启”保留，重启只重建被包装的 Store。
type fault struct {
	mu     sync.Mutex
	match  func(id string, s *durable.Snapshot) bool
	repeat bool
	hits   int
}

func (f *fault) arm(repeat bool, match func(id string, s *durable.Snapshot) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.match, f.repeat, f.hits = match, repeat, 0
}

func (f *fault) Hits() int { f.mu.Lock(); defer f.mu.Unlock(); return f.hits }

type faultStore struct {
	durable.Store
	f *fault
}

func (s faultStore) Save(ctx context.Context, id string, snap *durable.Snapshot) error {
	s.f.mu.Lock()
	hit := s.f.match != nil && s.f.match(id, snap)
	if hit {
		s.f.hits++
		if !s.f.repeat {
			s.f.match = nil
		}
	}
	s.f.mu.Unlock()
	if hit {
		return errInjected
	}
	return s.Store.Save(ctx, id, snap)
}

// operation 解码持久操作记录；不是操作记录时返回 false。
func operation(s *durable.Snapshot) (durable.OperationRecord, bool) {
	var r durable.OperationRecord
	if s == nil || s.Version != "goagent-operation-v2" || json.Unmarshal(s.Data, &r) != nil {
		return r, false
	}
	return r, true
}

// checkpoint 解码图检查点的位置字段；不是图检查点时返回 false。
type checkpointView struct {
	Node    string `json:"node"`
	Routing bool   `json:"routing"`
}

func checkpoint(s *durable.Snapshot) (checkpointView, bool) {
	var v checkpointView
	if s == nil || s.Plan == "" || json.Unmarshal(s.Data, &v) != nil {
		return v, false
	}
	return v, true
}

// env 是一次持久运行的存储环境：图存储与预算存储各占一个目录，“重启”时重建全部存储对象与预算。
type env struct {
	t                   *testing.T
	id                  string // 运行 ID，同时是根预算的 RootKey（builder 要求两者一致）
	graphDir, budgetDir string
	budget              budget.Config
	fault               *fault
	store               durable.Store
	controller          *budget.Controller
}

func newEnv(t *testing.T, root string) *env {
	t.Helper()
	e := &env{
		t: t, id: root, graphDir: t.TempDir(), budgetDir: t.TempDir(), fault: &fault{},
		budget: budget.Config{RootKey: root, Version: "v1", MaxModelRequests: 50},
	}
	e.restart()
	return e
}

// restart 重建存储对象与持久根预算，复用同一组目录。
func (e *env) restart() {
	e.t.Helper()
	inner, err := durable.NewFileStore(e.graphDir)
	require.NoError(e.t, err)
	e.store = faultStore{Store: inner, f: e.fault}
	budgetStore, err := durable.NewFileStore(e.budgetDir)
	require.NoError(e.t, err)
	e.controller, err = budget.NewDurable(e.budget, budgetStore)
	require.NoError(e.t, err)
}

// renew 换用新的运行 ID 与新的根预算（RootKey 随之改变），图存储与预算存储目录不变。
func (e *env) renew(id string) {
	e.id, e.budget.RootKey = id, id
	e.restart()
}

// ctx 返回带持久根预算的运行上下文。
func (e *env) ctx() context.Context { return budget.WithController(context.Background(), e.controller) }

// compile 以本环境的存储编译持久图。
func compile[T any](t *testing.T, e *env, g *workflow.Graph[T], version string, approvals ...string) *workflow.DurableGraph[T] {
	t.Helper()
	d, err := g.CompileDurable(context.Background(), workflow.DurableConfig{Store: e.store, Version: version, ApprovalNodes: approvals})
	require.NoError(t, err)
	return d
}

// inspect 读取节点 nodeKey 下 Agent scope 的持久操作记录；先取操作锁，会等仍在运行的 Do 释放。
func inspect(t *testing.T, e *env, nodeKey, scope, id string) *durable.OperationRecord {
	t.Helper()
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), durable.ChildKey(nodeKey, "agent/"+scope)), e.store)
	r, err := durable.InspectOperation(ctx, id)
	require.NoError(t, err)
	return r
}

// keyed 记录工厂被调用时的节点执行键，供 inspect 使用。
func keyed(factory AgentFactory, key *atomic.Value) AgentFactory {
	return func(ctx context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		key.Store(workflow.DurableKey(ctx))
		return factory(ctx)
	}
}
```

`multiagent/parallel_test.go`：

```go
package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

func mergeLogs(_ context.Context, base record, outs []record) (record, error) {
	merged := cloneRecord(base)
	for _, o := range outs {
		merged.Log = append(merged.Log, o.Log[len(base.Log):]...)
	}
	return merged, nil
}

func lookups(t *testing.T) Step[record] {
	t.Helper()
	p, err := ParallelStep("lookup", ParallelConfig[record]{Steps: []Step[record]{mark("kb"), mark("order"), mark("ship")}, Clone: cloneRecord, Merge: mergeLogs})
	require.NoError(t, err)
	return p
}

func TestParallelStepValidation(t *testing.T) {
	inner := lookups(t)
	cfg := func(steps ...Step[record]) ParallelConfig[record] {
		return ParallelConfig[record]{Steps: steps, Clone: cloneRecord, Merge: mergeLogs}
	}
	for name, p := range map[string]struct {
		name string
		cfg  ParallelConfig[record]
	}{
		"名称无效":    {"", cfg(mark("a"))},
		"没有分支":    {"p", cfg()},
		"缺 Clone": {"p", ParallelConfig[record]{Steps: []Step[record]{mark("a")}, Merge: mergeLogs}},
		"缺 Merge": {"p", ParallelConfig[record]{Steps: []Step[record]{mark("a")}, Clone: cloneRecord}},
		"分支重名":    {"p", cfg(mark("a"), mark("a"))},
		"分支缺 Run": {"p", cfg(Step[record]{Name: "a"})},
		"嵌套并行":    {"p", cfg(mark("a"), inner)},
	} {
		_, err := ParallelStep(p.name, p.cfg)
		require.Error(t, err, name)
	}
}

func TestParallelStepRunCannotBeInvokedDirectly(t *testing.T) {
	_, err := lookups(t).Run.Invoke(context.Background(), record{})
	require.ErrorIs(t, err, errParallelRun)
}

// single 用 AddStep 构造 START → s → END。
func single[T any](t *testing.T, cfg workflow.Config, s Step[T]) *workflow.Graph[T] {
	t.Helper()
	g := workflow.NewGraph[T](cfg)
	require.NoError(t, AddStep(g, s))
	require.NoError(t, g.AddEdge(workflow.START, s.Name))
	require.NoError(t, g.AddEdge(s.Name, workflow.END))
	return g
}

func TestParallelStepCopiesSteps(t *testing.T) {
	steps := []Step[record]{mark("kb"), mark("order")}
	p, err := ParallelStep("lookup", ParallelConfig[record]{Steps: steps, Clone: cloneRecord, Merge: mergeLogs})
	require.NoError(t, err)
	steps[0] = mark("changed")
	run, err := single(t, workflow.Config{}, p).Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), record{})
	require.NoError(t, err)
	require.Equal(t, []string{"kb", "order"}, out.Log)
}

// handGraph 用 AddStep 手写 START → triage → lookup → END。
func handGraph(t *testing.T) *workflow.Graph[record] {
	t.Helper()
	g := workflow.NewGraph[record]()
	require.NoError(t, AddStep(g, mark("triage")))
	require.NoError(t, AddStep(g, lookups(t)))
	require.NoError(t, g.AddEdge(workflow.START, "triage"))
	require.NoError(t, g.AddEdge("triage", "lookup"))
	require.NoError(t, g.AddEdge("lookup", workflow.END))
	return g
}

func TestAddStepRegistersPlainAndParallelSteps(t *testing.T) {
	run, err := handGraph(t).Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), record{})
	require.NoError(t, err)
	require.Equal(t, []string{"triage", "kb", "order", "ship"}, out.Log)

	e := newEnv(t, "add-step")
	snap, err := compile(t, e, handGraph(t), "v1").Run(context.Background(), e.id, record{}, "")
	require.NoError(t, err)
	require.Equal(t, out.Log, snap.State.Log)
	require.Equal(t, 2, snap.Steps, "整组并行计 1 步")
}

func TestAddStepValidation(t *testing.T) {
	require.Error(t, AddStep[record](nil, mark("a")))
	g := workflow.NewGraph[record]()
	require.Error(t, AddStep(g, Step[record]{Name: "a"}))
	require.NoError(t, AddStep(g, mark("a")))
	require.ErrorContains(t, AddStep(g, mark("a")), "duplicate node")
	require.ErrorContains(t, AddStep(g, Step[record]{Name: "a", Run: lookups(t).Run, parallel: lookups(t).parallel}), "duplicate node")
}

func parallelGraph(t *testing.T, cfg ParallelConfig[record]) *core.StreamRunnable[record, record] {
	t.Helper()
	p, err := ParallelStep("p", cfg)
	require.NoError(t, err)
	run, err := single(t, workflow.Config{}, p).Compile(context.Background())
	require.NoError(t, err)
	return run
}

func TestParallelMergeErrorFailsNode(t *testing.T) {
	run := parallelGraph(t, ParallelConfig[record]{
		Steps: []Step[record]{mark("a")}, Clone: cloneRecord,
		Merge: func(context.Context, record, []record) (record, error) { return record{}, errors.New("合并失败") },
	})
	_, err := run.Invoke(context.Background(), record{})
	require.ErrorContains(t, err, `node "p"`)
	require.ErrorContains(t, err, "合并失败")
}

func TestParallelClonePanicBecomesError(t *testing.T) {
	run := parallelGraph(t, ParallelConfig[record]{
		Steps: []Step[record]{mark("a")}, Merge: mergeLogs,
		Clone: func(record) record { panic("clone 崩溃") },
	})
	_, err := run.Invoke(context.Background(), record{})
	var panicErr *agentErrors.PanicError
	require.ErrorAs(t, err, &panicErr)
}

func TestParallelBranchFailureCancelsSiblings(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	slow := Step[record]{Name: "slow", Run: core.NewRunnableFunc(func(ctx context.Context, r record) (record, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
			return r, ctx.Err()
		case <-time.After(10 * time.Second):
			return r, nil
		}
	})}
	// broken 等 slow 开始后才失败，保证取消的是正在运行的兄弟分支。
	broken := Step[record]{Name: "broken", Run: core.NewRunnableFunc(func(context.Context, record) (record, error) {
		<-started
		return record{}, errors.New("订单服务不可用")
	})}
	run := parallelGraph(t, ParallelConfig[record]{Steps: []Step[record]{slow, broken}, Clone: cloneRecord, Merge: mergeLogs})
	_, err := run.Invoke(context.Background(), record{})
	require.ErrorContains(t, err, `parallel branch "broken"`)
	require.ErrorContains(t, err, "订单服务不可用")
	select {
	case <-canceled:
	default:
		t.Fatal("失败分支返回后，图应已取消并等待兄弟分支结束")
	}
}

func TestDurableParallelDoesNotRerunCompletedBranches(t *testing.T) {
	e := newEnv(t, "parallel-restart")
	var kb, order atomic.Int32
	failOrder := atomic.Bool{}
	failOrder.Store(true)
	count := func(name string, n *atomic.Int32, fail *atomic.Bool) Step[record] {
		return Step[record]{Name: name, Run: core.NewRunnableFunc(func(_ context.Context, r record) (record, error) {
			n.Add(1)
			if fail != nil && fail.Load() {
				return r, errors.New("暂时失败")
			}
			r.Log = append(r.Log, name)
			return r, nil
		})}
	}
	build := func() *workflow.Graph[record] {
		p, err := ParallelStep("lookup", ParallelConfig[record]{Steps: []Step[record]{count("kb", &kb, nil), count("order", &order, &failOrder)}, Clone: cloneRecord, Merge: mergeLogs})
		require.NoError(t, err)
		return single(t, workflow.Config{MaxConcurrency: 1}, p)
	}
	_, err := compile(t, e, build(), "v1").Run(context.Background(), e.id, record{}, "")
	require.ErrorContains(t, err, "暂时失败")
	failOrder.Store(false)
	e.restart()
	snap, err := compile(t, e, build(), "v1").Run(context.Background(), e.id, record{}, "")
	require.NoError(t, err)
	require.Equal(t, []string{"kb", "order"}, snap.State.Log)
	require.EqualValues(t, 1, kb.Load(), "已保存输出的分支不重跑")
	require.EqualValues(t, 2, order.Load())
}

func TestDurableParallelRejectsBranchOutputThatCannotRoundTrip(t *testing.T) {
	type money struct{ Amount float64 }
	bad := Step[money]{Name: "bad", Run: core.NewRunnableFunc(func(context.Context, money) (money, error) { return money{Amount: math.Inf(1)}, nil })}
	p, err := ParallelStep("p", ParallelConfig[money]{
		Steps: []Step[money]{bad}, Clone: func(m money) money { return m },
		Merge: func(_ context.Context, m money, _ []money) (money, error) { return m, nil },
	})
	require.NoError(t, err)
	g := single(t, workflow.Config{}, p)
	e := newEnv(t, "parallel-json")
	d, err := g.CompileDurable(context.Background(), workflow.DurableConfig{Store: e.store, Version: "v1"})
	require.NoError(t, err)
	_, err = d.Run(context.Background(), e.id, money{}, "")
	var jsonErr *json.UnsupportedValueError
	require.ErrorAs(t, err, &jsonErr)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 ./multiagent/`
Expected: FAIL，编译错误 `undefined: ParallelStep`、`undefined: AddStep`

- [ ] **Step 3: 写实现**

构造时复制 `Steps`；分支不得是 `ParallelStep`；返回值的 `Run` 直接调用即返回 `errParallelRun`，仍满足“Run 非 nil”。`AddStep` 对并行 Step 用 `AddParallel` 注册，分支函数调用分支 Runnable 的 `Invoke`，因此两种编译方式都不转发分支内的中间事件。

`multiagent/parallel.go`：

```go
package multiagent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/workflow"
)

// ParallelConfig 描述一组并行分支。Clone 必须深拷贝分支会修改的切片、映射与指针字段；
// Merge 按分支登记顺序收到各分支输出，必须是纯函数（恢复后可能重复执行）。
type ParallelConfig[T any] struct {
	Steps []Step[T]
	Clone func(T) T
	Merge func(context.Context, T, []T) (T, error)
}

var errParallelRun = errors.New("ParallelStep 只能经 AddStep 或 multiagent 构造器注册，不能直接执行")

// ParallelStep 把一组并行分支包装成名为 name 的一步；整组计 1 步。分支名只需在本节点内唯一，
// 分支不得再是 ParallelStep。返回值的 Run 直接调用即返回错误，构造器与 AddStep 忽略它。
func ParallelStep[T any](name string, p ParallelConfig[T]) (Step[T], error) {
	if !validName(name) {
		return Step[T]{}, fmt.Errorf("Step 名称 %q 无效", name)
	}
	if p.Clone == nil || p.Merge == nil || len(p.Steps) == 0 {
		return Step[T]{}, fmt.Errorf("ParallelStep %q 需要至少一个分支以及 Clone 与 Merge", name)
	}
	seen := map[string]bool{}
	for _, s := range p.Steps {
		if err := checkStep(s); err != nil {
			return Step[T]{}, fmt.Errorf("ParallelStep %q: %w", name, err)
		}
		if s.parallel != nil {
			return Step[T]{}, fmt.Errorf("ParallelStep %q 的分支 %q 不能是 ParallelStep", name, s.Name)
		}
		if seen[s.Name] {
			return Step[T]{}, fmt.Errorf("ParallelStep %q 的分支名 %q 重复", name, s.Name)
		}
		seen[s.Name] = true
	}
	run := core.NewRunnableFunc(func(_ context.Context, in T) (T, error) { return in, errParallelRun })
	return Step[T]{Name: name, Run: run, parallel: &parallelSpec[T]{steps: slices.Clone(p.Steps), clone: p.Clone, merge: p.Merge}}, nil
}

// AddStep 把 Step 加入宿主手写的图：ParallelStep 以 AddParallel 注册，其余以 AddRunnable 注册。
// 三个构造器内部也用它注册节点。
func AddStep[T any](g *workflow.Graph[T], s Step[T]) error {
	if g == nil {
		return errors.New("AddStep 需要非 nil 的图")
	}
	if err := checkStep(s); err != nil {
		return err
	}
	if s.parallel == nil {
		return g.AddRunnable(s.Name, s.Run)
	}
	branches := make([]workflow.Branch[T], len(s.parallel.steps))
	for i, b := range s.parallel.steps {
		run := b.Run
		branches[i] = workflow.Branch[T]{Name: b.Name, Run: func(ctx context.Context, in T) (T, error) { return run.Invoke(ctx, in) }}
	}
	return g.AddParallel(s.Name, workflow.ParallelConfig[T]{Branches: branches, Clone: s.parallel.clone, Merge: s.parallel.merge})
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -count=1 -run '^(TestParallelStepValidation|TestParallelStepRunCannotBeInvokedDirectly|TestParallelStepCopiesSteps|TestAddStepRegistersPlainAndParallelSteps|TestAddStepValidation|TestParallelMergeErrorFailsNode|TestParallelClonePanicBecomesError|TestParallelBranchFailureCancelsSiblings|TestDurableParallelDoesNotRerunCompletedBranches|TestDurableParallelRejectsBranchOutputThatCannotRoundTrip)$' ./multiagent/`
Expected: PASS

- [ ] **Step 5: 全包回归、vet 与格式检查**

Run: `go test -race -count=1 ./multiagent/... && go vet ./multiagent/... && gofumpt -l multiagent && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent`
Expected: 测试 PASS，其余无输出

- [ ] **Step 6: 提交**

```bash
git add multiagent/parallel.go multiagent/env_test.go multiagent/parallel_test.go
git commit -m "feat(multiagent): 新增 ParallelStep 组合件与 AddStep"
```


### Task 5: Sequential 与步数配置

**Files:**
- Create: `multiagent/sequential.go`
- Test: `multiagent/sequential_test.go`

**Interfaces:**
- Consumes: Task 4 的 `AddStep`、`lookups`、`newEnv`、`compile`；Task 2 的 `mark`、`record`
- Produces: `func Sequential[T any](cfg workflow.Config, steps ...Step[T]) (*workflow.Graph[T], error)`；私有 `newGraph[T](cfg workflow.Config, maxSteps int) (*workflow.Graph[T], error)`（`cfg.MaxSteps != 0` 时报错）、`chain[T](g, steps []Step[T]) error`。测试辅助 `count(record, name string) int`、`runBoth(t, build func() (*workflow.Graph[record], error), approvals ...string) (record, error, record, error)`、`requireSameResult(t, build, want []string)` 供 Task 6、7 复用。

- [ ] **Step 1: 写失败测试**

覆盖正常流程（含 `ParallelStep`）普通与持久结果一致，以及校验：无 Step、Run 为空、名称无效、重名、`cfg.MaxSteps` 非零。

`multiagent/sequential_test.go`：

```go
package multiagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/workflow"
)

func count(r record, name string) int {
	n := 0
	for _, s := range r.Log {
		if s == name {
			n++
		}
	}
	return n
}

// runBoth 用普通与持久两种方式各执行一次图，返回两次的结果与错误。
func runBoth(t *testing.T, build func() (*workflow.Graph[record], error), approvals ...string) (record, error, record, error) {
	t.Helper()
	g, err := build()
	require.NoError(t, err)
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	plain, plainErr := run.Invoke(context.Background(), record{})
	e := newEnv(t, "both")
	snap, durableErr := compile(t, e, g, "v1", approvals...).Run(context.Background(), e.id, record{}, "")
	var durableOut record
	if snap != nil {
		durableOut = snap.State
	}
	return plain, plainErr, durableOut, durableErr
}

func requireSameResult(t *testing.T, build func() (*workflow.Graph[record], error), want []string) {
	t.Helper()
	plain, plainErr, durableOut, durableErr := runBoth(t, build)
	require.NoError(t, plainErr)
	require.NoError(t, durableErr)
	require.Equal(t, want, plain.Log)
	require.Equal(t, want, durableOut.Log, "两种编译方式结果一致")
}

func TestSequentialRunsInOrder(t *testing.T) {
	requireSameResult(t, func() (*workflow.Graph[record], error) {
		return Sequential(workflow.Config{}, mark("extract"), lookups(t), mark("answer"))
	}, []string{"extract", "kb", "order", "ship", "answer"})
}

func TestSequentialValidation(t *testing.T) {
	bad := Step[record]{Name: "nil-run"}
	invalidName := Step[record]{Name: " x", Run: mark("x").Run}
	for name, build := range map[string]func() (*workflow.Graph[record], error){
		"Sequential 无 Step":   func() (*workflow.Graph[record], error) { return Sequential[record](workflow.Config{}) },
		"Sequential Run 为空":   func() (*workflow.Graph[record], error) { return Sequential(workflow.Config{}, bad) },
		"Sequential 名称无效":     func() (*workflow.Graph[record], error) { return Sequential(workflow.Config{}, invalidName) },
		"Sequential 重名":       func() (*workflow.Graph[record], error) { return Sequential(workflow.Config{}, mark("a"), mark("a")) },
		"Sequential MaxSteps": func() (*workflow.Graph[record], error) { return Sequential(workflow.Config{MaxSteps: 5}, mark("a")) },
	} {
		g, err := build()
		require.Error(t, err, name)
		require.Nil(t, g, name)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 -run '^(TestSequentialRunsInOrder|TestSequentialValidation)$' ./multiagent/`
Expected: FAIL，编译错误 `undefined: Sequential`

- [ ] **Step 3: 写实现**

`MaxSteps` 为步数 n，`cfg.MaxConcurrency` 原样传给 `workflow.NewGraph`。

`multiagent/sequential.go`：

```go
package multiagent

import (
	"errors"
	"fmt"

	"github.com/kart-io/goagent/workflow"
)

// newGraph 用构造器算出的步数上限创建图；宿主设置的 MaxSteps 会让上限与拓扑不一致，因此拒绝。
func newGraph[T any](cfg workflow.Config, maxSteps int) (*workflow.Graph[T], error) {
	if cfg.MaxSteps != 0 {
		return nil, errors.New("cfg.MaxSteps 由构造器计算，必须为 0")
	}
	cfg.MaxSteps = maxSteps
	return workflow.NewGraph[T](cfg), nil
}

// chain 注册 steps 并按顺序连边：START → s1 → … → sn，不连 sn 的出边。
func chain[T any](g *workflow.Graph[T], steps []Step[T]) error {
	prev := workflow.START
	for i, s := range steps {
		if err := AddStep(g, s); err != nil {
			return fmt.Errorf("第 %d 步: %w", i+1, err)
		}
		if err := g.AddEdge(prev, s.Name); err != nil {
			return err
		}
		prev = s.Name
	}
	return nil
}

// Sequential 构造 START → s1 → … → sn → END，MaxSteps 为 n。
func Sequential[T any](cfg workflow.Config, steps ...Step[T]) (*workflow.Graph[T], error) {
	if len(steps) == 0 {
		return nil, errors.New("Sequential 需要至少一个 Step")
	}
	g, err := newGraph[T](cfg, len(steps))
	if err != nil {
		return nil, err
	}
	if err = chain(g, steps); err != nil {
		return nil, err
	}
	if err = g.AddEdge(steps[len(steps)-1].Name, workflow.END); err != nil {
		return nil, err
	}
	return g, nil
}
```
- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -count=1 -run '^(TestSequentialRunsInOrder|TestSequentialValidation)$' ./multiagent/`
Expected: PASS

- [ ] **Step 5: 全包回归、vet 与格式检查**

Run: `go test -race -count=1 ./multiagent/... ./testing/mocks/... && go vet ./multiagent/... ./testing/mocks/... && gofumpt -l multiagent/sequential.go multiagent/sequential_test.go && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent/sequential.go multiagent/sequential_test.go`
Expected: 测试 PASS，vet 无输出，gofumpt 与 gci 不列出任何文件

- [ ] **Step 6: 提交**

```bash
git add multiagent/sequential.go multiagent/sequential_test.go
git commit -m "feat(multiagent): 新增 Sequential 构造器"
```


### Task 6: Loop（含 Fallback）

**Files:**
- Create: `multiagent/loop.go`
- Test: `multiagent/loop_test.go`

**Interfaces:**
- Consumes: Task 3 的 `GuardRoute`、`Finish`、`ErrInvalidRoute`；Task 5 的 `newGraph`、`chain`、`runBoth`、`requireSameResult`、`count`；Task 4 的 `lookups`
- Produces: `type LoopConfig[T any] struct{ Steps []Step[T]; Until func(context.Context, T) (bool, error); MaxIterations int; Fallback *Step[T] }`；`func Loop[T any](cfg workflow.Config, l LoopConfig[T]) (*workflow.Graph[T], error)`；私有 `addFallback[T](g, fb *Step[T]) (string, error)`（Task 7 复用）。测试辅助 `untilRounds(last string, n int)`、`loopWith(...)`（Task 11 复用 `untilRounds`）。

- [ ] **Step 1: 写失败测试**

覆盖：正常结束（普通与持久一致）、单步自环与并行步、构造后修改调用方切片不影响图、第 N 轮 `Until` 为真正常结束为假返回 `ErrMaxSteps`、`Until` 无效时转兜底或返回 `ErrInvalidRoute`、其他错误即使有兜底也失败、最后一轮无效时有兜底返回 `ErrMaxSteps` 且写作者不多执行，以及校验表（含溢出与 `cfg.MaxSteps`）。

`multiagent/loop_test.go`：

```go
package multiagent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/workflow"
)

// untilRounds 在最后一步执行满 n 次后结束。
func untilRounds(last string, n int) func(context.Context, record) (bool, error) {
	return func(_ context.Context, r record) (bool, error) { return count(r, last) >= n, nil }
}

func TestLoopRunsUntilDone(t *testing.T) {
	requireSameResult(t, func() (*workflow.Graph[record], error) {
		return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("write"), mark("review")}, Until: untilRounds("review", 2), MaxIterations: 3})
	}, []string{"write", "review", "write", "review"})
}

func TestLoopWithSingleStepAndParallel(t *testing.T) {
	requireSameResult(t, func() (*workflow.Graph[record], error) {
		return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{lookups(t)}, Until: untilRounds("kb", 2), MaxIterations: 2})
	}, []string{"kb", "order", "ship", "kb", "order", "ship"})
}

func TestConstructorsCopyCallerSteps(t *testing.T) {
	steps := []Step[record]{mark("write"), mark("review")}
	g, err := Loop(workflow.Config{}, LoopConfig[record]{Steps: steps, Until: untilRounds("review", 1), MaxIterations: 1})
	require.NoError(t, err)
	steps[0], steps[1] = mark("x"), mark("y")
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), record{})
	require.NoError(t, err)
	require.Equal(t, []string{"write", "review"}, out.Log)
}

func TestLoopIterationLimit(t *testing.T) {
	build := func(rounds int) func() (*workflow.Graph[record], error) {
		return func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("write"), mark("review")}, Until: untilRounds("review", rounds), MaxIterations: 2})
		}
	}
	requireSameResult(t, build(2), []string{"write", "review", "write", "review"})
	plain, plainErr, durableOut, durableErr := runBoth(t, build(3))
	require.ErrorIs(t, plainErr, workflow.ErrMaxSteps)
	require.ErrorIs(t, durableErr, workflow.ErrMaxSteps)
	require.Empty(t, plain.Log)
	require.Equal(t, 2, count(durableOut, "review"))
}

func loopWith(until func(context.Context, record) (bool, error), fallback *Step[record], iterations int) func() (*workflow.Graph[record], error) {
	return func() (*workflow.Graph[record], error) {
		return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("write"), mark("review")}, Until: until, MaxIterations: iterations, Fallback: fallback})
	}
}

func TestLoopFallback(t *testing.T) {
	invalidOnce := func(_ context.Context, r record) (bool, error) {
		return false, fmt.Errorf("%w：评审结论无法解析", ErrInvalidRoute)
	}
	fb := mark("human")
	plain, plainErr, durableOut, durableErr := runBoth(t, loopWith(invalidOnce, &fb, 2))
	require.NoError(t, plainErr)
	require.NoError(t, durableErr)
	require.Equal(t, []string{"write", "review", "human"}, plain.Log)
	require.Equal(t, plain.Log, durableOut.Log)

	_, plainErr, _, durableErr = runBoth(t, loopWith(invalidOnce, nil, 2))
	require.ErrorIs(t, plainErr, ErrInvalidRoute)
	require.ErrorIs(t, durableErr, ErrInvalidRoute)

	other := errors.New("评审服务不可用")
	_, plainErr, _, _ = runBoth(t, loopWith(func(context.Context, record) (bool, error) { return false, other }, &fb, 2))
	require.ErrorIs(t, plainErr, other)

	// 最后一轮判断无效：有兜底时返回 ErrMaxSteps，写作者不会多执行一次。
	_, plainErr, durableOut, durableErr = runBoth(t, loopWith(invalidOnce, &fb, 1))
	require.ErrorIs(t, plainErr, workflow.ErrMaxSteps)
	require.ErrorIs(t, durableErr, workflow.ErrMaxSteps)
	require.Equal(t, 1, count(durableOut, "write"))
	_, plainErr, _, durableErr = runBoth(t, loopWith(invalidOnce, nil, 1))
	require.ErrorIs(t, plainErr, ErrInvalidRoute)
	require.ErrorIs(t, durableErr, ErrInvalidRoute)
}

func TestLoopValidation(t *testing.T) {
	until := untilRounds("a", 1)
	bad := Step[record]{Name: "nil-run"}
	for name, build := range map[string]func() (*workflow.Graph[record], error){
		"Loop 无 Step": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Until: until, MaxIterations: 1})
		},
		"Loop 无 Until": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, MaxIterations: 1})
		},
		"Loop MaxIterations 0": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, Until: until})
		},
		"Loop 溢出": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a"), mark("b")}, Until: until, MaxIterations: math.MaxInt/2 + 1})
		},
		"Loop MaxSteps": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{MaxSteps: 3}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, Until: until, MaxIterations: 1})
		},
		"Loop 兜底重名": func() (*workflow.Graph[record], error) {
			fb := mark("a")
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, Until: until, MaxIterations: 1, Fallback: &fb})
		},
		"Loop 兜底 Run 为空": func() (*workflow.Graph[record], error) {
			return Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("a")}, Until: until, MaxIterations: 1, Fallback: &bad})
		},
	} {
		g, err := build()
		require.Error(t, err, name)
		require.Nil(t, g, name)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 -run '^(TestLoopRunsUntilDone|TestLoopWithSingleStepAndParallel|TestConstructorsCopyCallerSteps|TestLoopIterationLimit|TestLoopFallback|TestLoopValidation)$' ./multiagent/`
Expected: FAIL，编译错误 `undefined: Loop`、`undefined: LoopConfig`

- [ ] **Step 3: 写实现**

`Until` 改写成 next（为真返回 `Finish`，为假返回第一步名，错误原样返回），以第一步名为唯一成员、Fallback 为兜底调用 `GuardRoute`，注册在最后一步上；Fallback 连到 `END`。

`multiagent/loop.go`：

```go
package multiagent

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/kart-io/goagent/workflow"
)

// LoopConfig 描述循环：每轮依次执行 Steps，最后一步之后由 Until 判断是否结束。
type LoopConfig[T any] struct {
	Steps         []Step[T]
	Until         func(context.Context, T) (bool, error) // 必填；每轮结束后判断是否结束，必须是纯函数
	MaxIterations int                                    // 必填，> 0；防失控的硬上限，达到后返回 workflow.ErrMaxSteps
	Fallback      *Step[T]                               // 可选；Until 返回包装 ErrInvalidRoute 的错误时执行，之后结束
}

// Loop 构造 START → s1 → … → sn，sn 的分支目标为 s1 与 END（有 Fallback 时再加 fb，fb → END），
// MaxSteps 为 MaxIterations × n。Fallback 不增加步数：最后一轮判断无效时返回 workflow.ErrMaxSteps。
func Loop[T any](cfg workflow.Config, l LoopConfig[T]) (*workflow.Graph[T], error) {
	n := len(l.Steps)
	switch {
	case n == 0:
		return nil, errors.New("Loop 需要至少一个 Step")
	case l.Until == nil:
		return nil, errors.New("Loop 需要 Until")
	case l.MaxIterations <= 0:
		return nil, errors.New("Loop 的 MaxIterations 必须大于 0")
	case l.MaxIterations > math.MaxInt/n:
		return nil, errors.New("Loop 的 MaxIterations × 步数超出 int 范围")
	}
	g, err := newGraph[T](cfg, l.MaxIterations*n)
	if err != nil {
		return nil, err
	}
	if err = chain(g, l.Steps); err != nil {
		return nil, err
	}
	first := l.Steps[0].Name
	until := l.Until
	next := func(ctx context.Context, state T) (string, error) {
		done, err := until(ctx, state)
		if err != nil {
			return "", err
		}
		if done {
			return Finish, nil
		}
		return first, nil
	}
	fallback, err := addFallback(g, l.Fallback)
	if err != nil {
		return nil, err
	}
	route, targets, err := GuardRoute(next, []string{first}, fallback)
	if err != nil {
		return nil, err
	}
	if err = g.AddBranch(l.Steps[n-1].Name, route, targets...); err != nil {
		return nil, err
	}
	return g, nil
}

// addFallback 注册兜底 Step 并连到 END，返回其名称；fb 为 nil 时返回空名。
func addFallback[T any](g *workflow.Graph[T], fb *Step[T]) (string, error) {
	if fb == nil {
		return "", nil
	}
	if fb.Name == Finish {
		return "", fmt.Errorf("兜底 Step 不能命名为 %s", Finish)
	}
	if err := AddStep(g, *fb); err != nil {
		return "", fmt.Errorf("兜底 Step: %w", err)
	}
	if err := g.AddEdge(fb.Name, workflow.END); err != nil {
		return "", err
	}
	return fb.Name, nil
}
```
- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -count=1 -run '^(TestLoopRunsUntilDone|TestLoopWithSingleStepAndParallel|TestConstructorsCopyCallerSteps|TestLoopIterationLimit|TestLoopFallback|TestLoopValidation)$' ./multiagent/`
Expected: PASS

- [ ] **Step 5: 全包回归、vet 与格式检查**

Run: `go test -race -count=1 ./multiagent/... ./testing/mocks/... && go vet ./multiagent/... ./testing/mocks/... && gofumpt -l multiagent/loop.go multiagent/loop_test.go && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent/loop.go multiagent/loop_test.go`
Expected: 测试 PASS，vet 无输出，gofumpt 与 gci 不列出任何文件

- [ ] **Step 6: 提交**

```bash
git add multiagent/loop.go multiagent/loop_test.go
git commit -m "feat(multiagent): 新增带兜底的 Loop 构造器"
```


### Task 7: RouterSupervisor（含 Fallback）

**Files:**
- Create: `multiagent/router.go`
- Test: `multiagent/router_test.go`

**Interfaces:**
- Consumes: Task 3 的 `GuardRoute`、`fixed`；Task 5 的 `newGraph`、`chain`、`runBoth`、`requireSameResult`、`count`；Task 6 的 `addFallback`；Task 4 的 `lookups`、`newEnv`、`compile`
- Produces: `type RouterConfig[T any] struct{ Supervisor Step[T]; Members []Step[T]; Next func(context.Context, T) (string, error); MaxRounds int; Fallback *Step[T] }`；`func RouterSupervisor[T any](cfg workflow.Config, r RouterConfig[T]) (*workflow.Graph[T], error)`。测试辅助 `routeBy(seq ...string)`、`routerWith(...)`。

- [ ] **Step 1: 写失败测试**

覆盖：成员路由（含并行成员）普通与持久一致、第 R 次成员后 Finish 正常结束而再选成员返回 `ErrMaxSteps`、兜底的五种无效输入、其他错误即使有兜底也失败、末轮无效时有兜底返回 `ErrMaxSteps`、Fallback 与并行成员外层名可列入 `ApprovalNodes`、分支名填入被 `CompileDurable` 拒绝，以及校验表。

`multiagent/router_test.go`：

```go
package multiagent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
)

// routeBy 让第 k 次 Supervisor 之后选择 seq[k]，用完后 Finish；只依据状态，是纯函数。
func routeBy(seq ...string) func(context.Context, record) (string, error) {
	return func(_ context.Context, r record) (string, error) {
		k := count(r, "sup") - 1
		if k < len(seq) {
			return seq[k], nil
		}
		return Finish, nil
	}
}

func TestRouterSupervisorRoutesMembers(t *testing.T) {
	requireSameResult(t, func() (*workflow.Graph[record], error) {
		return RouterSupervisor(workflow.Config{}, RouterConfig[record]{
			Supervisor: mark("sup"), Members: []Step[record]{mark("refund"), lookups(t)},
			Next: routeBy("refund", "lookup"), MaxRounds: 2,
		})
	}, []string{"sup", "refund", "sup", "kb", "order", "ship", "sup"})
}

func TestRouterRoundLimit(t *testing.T) {
	build := func(seq ...string) func() (*workflow.Graph[record], error) {
		return func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: routeBy(seq...), MaxRounds: 2})
		}
	}
	requireSameResult(t, build("a", "a"), []string{"sup", "a", "sup", "a", "sup"})
	_, plainErr, durableOut, durableErr := runBoth(t, build("a", "a", "a"))
	require.ErrorIs(t, plainErr, workflow.ErrMaxSteps)
	require.ErrorIs(t, durableErr, workflow.ErrMaxSteps)
	require.Equal(t, 2, count(durableOut, "a"), "第 MaxRounds 次成员后 Supervisor 再执行一次，选成员即超限")
	require.Equal(t, 3, count(durableOut, "sup"))
}

func routerWith(next func(context.Context, record) (string, error), fallback *Step[record], rounds int) func() (*workflow.Graph[record], error) {
	return func() (*workflow.Graph[record], error) {
		return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: rounds, Fallback: fallback})
	}
}

func TestRouterFallback(t *testing.T) {
	invalid := fmt.Errorf("%w：无法识别", ErrInvalidRoute)
	for name, next := range map[string]func(context.Context, record) (string, error){
		"ErrInvalidRoute": fixed("", invalid),
		"未知成员":            fixed("x", nil),
		"END 字面值":         fixed(workflow.END, nil),
		"Supervisor 名":    fixed("sup", nil),
		"兜底名":             fixed("human", nil),
	} {
		fb := mark("human")
		plain, plainErr, durableOut, durableErr := runBoth(t, routerWith(next, &fb, 2))
		require.NoError(t, plainErr, name)
		require.NoError(t, durableErr, name)
		require.Equal(t, []string{"sup", "human"}, plain.Log, name)
		require.Equal(t, plain.Log, durableOut.Log, name)
		if name == "兜底名" {
			continue // 没有兜底时兜底名不存在，等同未知成员
		}
		_, plainErr, _, durableErr = runBoth(t, routerWith(next, nil, 2))
		require.ErrorIs(t, plainErr, ErrInvalidRoute, name)
		require.ErrorIs(t, durableErr, ErrInvalidRoute, name)
	}
	fb := mark("human")
	other := errors.New("分流服务不可用")
	_, plainErr, _, durableErr := runBoth(t, routerWith(fixed("", other), &fb, 2))
	require.ErrorIs(t, plainErr, other, "其他错误即使有兜底也失败")
	require.ErrorIs(t, durableErr, other)
}

func TestRouterFallbackOnLastRoundHitsStepLimit(t *testing.T) {
	// 第 1 次选 a，第 2 次 Supervisor 输出无效：MaxRounds 为 1 时兜底没有步数可用。
	next := func(_ context.Context, r record) (string, error) {
		if count(r, "sup") == 1 {
			return "a", nil
		}
		return "", ErrInvalidRoute
	}
	fb := mark("human")
	_, plainErr, durableOut, durableErr := runBoth(t, routerWith(next, &fb, 1))
	require.ErrorIs(t, plainErr, workflow.ErrMaxSteps)
	require.ErrorIs(t, durableErr, workflow.ErrMaxSteps)
	require.NotContains(t, durableOut.Log, "human")
	_, plainErr, _, durableErr = runBoth(t, routerWith(next, nil, 1))
	require.ErrorIs(t, plainErr, ErrInvalidRoute)
	require.ErrorIs(t, durableErr, ErrInvalidRoute)
}

func TestApprovalNodesAcceptOuterNames(t *testing.T) {
	fb := mark("human")
	g, err := RouterSupervisor(workflow.Config{}, RouterConfig[record]{
		Supervisor: mark("sup"), Members: []Step[record]{lookups(t)},
		Next: fixed("x", nil), MaxRounds: 1, Fallback: &fb,
	})
	require.NoError(t, err)
	e := newEnv(t, "approval")
	_, err = g.CompileDurable(context.Background(), workflow.DurableConfig{Store: e.store, Version: "v1", ApprovalNodes: []string{"kb"}})
	require.ErrorContains(t, err, "unknown or duplicate approval node", "分支名不是图节点")

	d := compile(t, e, g, "v1", "lookup", "human")
	snap, err := d.Run(context.Background(), e.id, record{}, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Equal(t, "human", snap.Node, "兜底可列入审批")
	snap, err = d.Run(context.Background(), e.id, record{}, snap.Key)
	require.NoError(t, err)
	require.Equal(t, []string{"sup", "human"}, snap.State.Log)
}

func TestRouterParallelMemberApprovedByOuterName(t *testing.T) {
	g, err := RouterSupervisor(workflow.Config{}, RouterConfig[record]{
		Supervisor: mark("sup"), Members: []Step[record]{lookups(t)},
		Next: routeBy("lookup"), MaxRounds: 1,
	})
	require.NoError(t, err)
	e := newEnv(t, "parallel-approval")
	d := compile(t, e, g, "v1", "lookup")
	snap, err := d.Run(context.Background(), e.id, record{}, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Equal(t, "lookup", snap.Node)
	snap, err = d.Run(context.Background(), e.id, record{}, snap.Key)
	require.NoError(t, err)
	require.True(t, slices.Equal([]string{"sup", "kb", "order", "ship", "sup"}, snap.State.Log))
	require.Equal(t, 3, snap.Steps, "整组并行计 1 步")
}

func TestRouterValidation(t *testing.T) {
	next := routeBy()
	for name, build := range map[string]func() (*workflow.Graph[record], error){
		"Router MaxRounds 0": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next})
		},
		"Router 溢出": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: math.MaxInt/2 + 1})
		},
		"Router 无 Next": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, MaxRounds: 1})
		},
		"Router 无成员": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Next: next, MaxRounds: 1})
		},
		"Router 成员 FINISH": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark(Finish)}, Next: next, MaxRounds: 1})
		},
		"Router 成员与 Supervisor 同名": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("sup")}, Next: next, MaxRounds: 1})
		},
		"Router 成员重名": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a"), mark("a")}, Next: next, MaxRounds: 1})
		},
		"Router 兜底 FINISH": func() (*workflow.Graph[record], error) {
			fb := mark(Finish)
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: 1, Fallback: &fb})
		},
		"Router 兜底与 Supervisor 同名": func() (*workflow.Graph[record], error) {
			fb := mark("sup")
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: 1, Fallback: &fb})
		},
		"Router 兜底与成员同名": func() (*workflow.Graph[record], error) {
			fb := mark("a")
			return RouterSupervisor(workflow.Config{}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: 1, Fallback: &fb})
		},
		"Router MaxSteps": func() (*workflow.Graph[record], error) {
			return RouterSupervisor(workflow.Config{MaxSteps: 9}, RouterConfig[record]{Supervisor: mark("sup"), Members: []Step[record]{mark("a")}, Next: next, MaxRounds: 1})
		},
	} {
		g, err := build()
		require.Error(t, err, name)
		require.Nil(t, g, name)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 -run '^(TestRouterSupervisorRoutesMembers|TestRouterRoundLimit|TestRouterFallback|TestRouterFallbackOnLastRoundHitsStepLimit|TestApprovalNodesAcceptOuterNames|TestRouterParallelMemberApprovedByOuterName|TestRouterValidation)$' ./multiagent/`
Expected: FAIL，编译错误 `undefined: RouterSupervisor`、`undefined: RouterConfig`

- [ ] **Step 3: 写实现**

`MaxSteps = 2 × MaxRounds + 1`；Supervisor 允许是 `ParallelStep`，成员回到 Supervisor，Fallback 连到 `END`。

`multiagent/router.go`：

```go
package multiagent

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/kart-io/goagent/workflow"
)

// RouterConfig 描述 Supervisor 分流：Supervisor 执行后由 Next 选择成员或 Finish，成员执行后回到 Supervisor。
type RouterConfig[T any] struct {
	Supervisor Step[T]
	Members    []Step[T]
	Next       func(context.Context, T) (string, error) // 返回成员名或 Finish，必须是纯函数
	MaxRounds  int                                      // 必填，> 0：成员最多执行的次数
	Fallback   *Step[T]                                 // 可选；路由无效时执行，之后结束
}

// RouterSupervisor 构造 START → sup，sup 的分支目标为全部成员与 END（有 Fallback 时再加 fb，fb → END），
// 每个成员回 sup，MaxSteps 为 2 × MaxRounds + 1。Fallback 占用一次成员轮数。
func RouterSupervisor[T any](cfg workflow.Config, r RouterConfig[T]) (*workflow.Graph[T], error) {
	switch {
	case r.MaxRounds <= 0:
		return nil, errors.New("RouterSupervisor 的 MaxRounds 必须大于 0")
	case r.MaxRounds > (math.MaxInt-1)/2:
		return nil, errors.New("RouterSupervisor 的 2 × MaxRounds + 1 超出 int 范围")
	case r.Next == nil:
		return nil, errors.New("RouterSupervisor 需要 Next")
	case len(r.Members) == 0:
		return nil, errors.New("RouterSupervisor 需要至少一个成员")
	}
	names := make([]string, len(r.Members))
	for i, m := range r.Members {
		if m.Name == Finish || m.Name == r.Supervisor.Name {
			return nil, fmt.Errorf("成员名 %q 不能是 %s 或 Supervisor 名", m.Name, Finish)
		}
		names[i] = m.Name
	}
	if r.Fallback != nil && r.Fallback.Name == r.Supervisor.Name {
		return nil, fmt.Errorf("兜底名 %q 不能与 Supervisor 同名", r.Fallback.Name)
	}
	g, err := newGraph[T](cfg, 2*r.MaxRounds+1)
	if err != nil {
		return nil, err
	}
	if err = chain(g, []Step[T]{r.Supervisor}); err != nil {
		return nil, fmt.Errorf("注册 Supervisor: %w", err)
	}
	for _, m := range r.Members {
		if err = AddStep(g, m); err != nil {
			return nil, fmt.Errorf("成员: %w", err)
		}
		if err = g.AddEdge(m.Name, r.Supervisor.Name); err != nil {
			return nil, err
		}
	}
	fallback, err := addFallback(g, r.Fallback)
	if err != nil {
		return nil, err
	}
	route, targets, err := GuardRoute(r.Next, names, fallback)
	if err != nil {
		return nil, err
	}
	if err = g.AddBranch(r.Supervisor.Name, route, targets...); err != nil {
		return nil, err
	}
	return g, nil
}
```
- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -count=1 -run '^(TestRouterSupervisorRoutesMembers|TestRouterRoundLimit|TestRouterFallback|TestRouterFallbackOnLastRoundHitsStepLimit|TestApprovalNodesAcceptOuterNames|TestRouterParallelMemberApprovedByOuterName|TestRouterValidation)$' ./multiagent/`
Expected: PASS

- [ ] **Step 5: 全包回归、vet 与格式检查**

Run: `go test -race -count=1 ./multiagent/... ./testing/mocks/... && go vet ./multiagent/... ./testing/mocks/... && gofumpt -l multiagent/router.go multiagent/router_test.go && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent/router.go multiagent/router_test.go`
Expected: 测试 PASS，vet 无输出，gofumpt 与 gci 不列出任何文件

- [ ] **Step 6: 提交**

```bash
git add multiagent/router.go multiagent/router_test.go
git commit -m "feat(multiagent): 新增带兜底的 RouterSupervisor 构造器"
```


### Task 8: 文本接力层

**Files:**
- Create: `multiagent/text.go`
- Test: `multiagent/text_test.go`

**Interfaces:**
- Consumes: Task 2 的 `AgentStep`、`sessions.fake`；Task 3 的 `ErrInvalidRoute`、`Finish`；Task 5 的 `Sequential`；Task 7 的 `RouterSupervisor`
- Produces: `type Turn struct{ Agent, Output string }`；`type Conversation struct{ Input string; Turns []Turn }`；`type TextOption func(*textConfig)`；`func TextStep(name string, agent AgentFactory, opts ...TextOption) (Step[Conversation], error)`；`func WithRender(render func(Conversation) any) TextOption`；`func WithLastTurns(k int) TextOption`；`func CloneConversation(c Conversation) Conversation`；`func MergeTurns(ctx context.Context, base Conversation, branches []Conversation) (Conversation, error)`；`func TextNext(members ...Step[Conversation]) (func(context.Context, Conversation) (string, error), error)`；私有 `renderTurns(Conversation, k int) string`（默认格式：Input 后每轮一行 `\n[Agent]: Output`）、`resultText(any) (string, error)`（Task 9 复用）。

- [ ] **Step 1: 写失败测试**

覆盖默认输入、`WithLastTurns`、`WithRender`、选项互斥与非法值、非字符串结果 JSON 编码、`CloneConversation` 在共享预留容量下互不影响（确定性断言）、`MergeTurns` 顺序与前缀校验、`TextNext` 构造校验与运行时去空白、空记录/空首行/模糊匹配返回 `ErrInvalidRoute`。

`multiagent/text_test.go`：

```go
package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/workflow"
)

// capture 返回记录 Agent 收到的输入并回答 answer 的会话工厂。
func capture(s *sessions, inputs *[]any, answer any) AgentFactory {
	return s.fake(func(_ context.Context, in any) (any, error) {
		*inputs = append(*inputs, in)
		return answer, nil
	})
}

func runText(t *testing.T, s Step[Conversation], c Conversation) Conversation {
	t.Helper()
	g, err := Sequential(workflow.Config{}, s)
	require.NoError(t, err)
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), c)
	require.NoError(t, err)
	return out
}

var history = Conversation{Input: "客户：我要退款", Turns: []Turn{{"triage", "refund"}, {"refund", "已登记"}, {"triage", "FINISH"}}}

func TestTextStepDefaultInputHasAllTurns(t *testing.T) {
	var s sessions
	var inputs []any
	step, err := TextStep("writer", capture(&s, &inputs, "草稿"))
	require.NoError(t, err)
	out := runText(t, step, history)
	require.Equal(t, "客户：我要退款\n[triage]: refund\n[refund]: 已登记\n[triage]: FINISH", inputs[0])
	require.Equal(t, Turn{Agent: "writer", Output: "草稿"}, out.Turns[3])
	require.Len(t, history.Turns, 3, "不修改输入状态")
}

func TestTextStepWindowAndRender(t *testing.T) {
	var s sessions
	var inputs []any
	step, err := TextStep("writer", capture(&s, &inputs, ""), WithLastTurns(1))
	require.NoError(t, err)
	out := runText(t, step, history)
	require.Equal(t, "客户：我要退款\n[triage]: FINISH", inputs[0])
	require.Equal(t, Turn{Agent: "writer"}, out.Turns[3], "空回答也记录")

	step, err = TextStep("writer", capture(&s, &inputs, "ok"), WithRender(func(c Conversation) any {
		data, _ := json.Marshal(c)
		return string(data)
	}))
	require.NoError(t, err)
	runText(t, step, history)
	require.JSONEq(t, `{"Input":"客户：我要退款","Turns":[{"Agent":"triage","Output":"refund"},{"Agent":"refund","Output":"已登记"},{"Agent":"triage","Output":"FINISH"}]}`, inputs[1].(string))
}

func TestTextStepOptionValidation(t *testing.T) {
	var s sessions
	agent := s.fake(func(context.Context, any) (any, error) { return "", nil })
	for name, opts := range map[string][]TextOption{
		"互斥":              {WithLastTurns(1), WithRender(func(Conversation) any { return "" })},
		"k 为 0":           {WithLastTurns(0)},
		"k 为负":            {WithLastTurns(-1)},
		"WithRender(nil)": {WithRender(nil)},
		"nil 选项":          {nil},
	} {
		_, err := TextStep("writer", agent, opts...)
		require.Error(t, err, name)
	}
	_, err := TextStep("writer", nil)
	require.Error(t, err)
}

func TestTextStepEncodesNonStringResult(t *testing.T) {
	var s sessions
	var inputs []any
	step, err := TextStep("extract", capture(&s, &inputs, map[string]any{"order": "A1"}))
	require.NoError(t, err)
	out := runText(t, step, Conversation{Input: "q"})
	require.Equal(t, `{"order":"A1"}`, out.Turns[0].Output)
}

func TestCloneConversationIsolatesSharedCapacity(t *testing.T) {
	base := Conversation{Input: "q", Turns: make([]Turn, 1, 4)}
	a, b := CloneConversation(base), CloneConversation(base)
	a.Turns = append(a.Turns, Turn{Agent: "kb", Output: "知识"})
	b.Turns = append(b.Turns, Turn{Agent: "order", Output: "订单"})
	require.Equal(t, "kb", a.Turns[1].Agent)
	require.Equal(t, "order", b.Turns[1].Agent)
	require.Len(t, base.Turns, 1)
}

func TestMergeTurns(t *testing.T) {
	base := Conversation{Input: "q", Turns: []Turn{{"triage", "lookup"}}}
	kb := CloneConversation(base)
	kb.Turns = append(kb.Turns, Turn{"kb", "知识"})
	order := CloneConversation(base)
	order.Turns = append(order.Turns, Turn{"order", "订单"}, Turn{"order", "补充"})
	out, err := MergeTurns(context.Background(), base, []Conversation{kb, order})
	require.NoError(t, err)
	require.Equal(t, []Turn{{"triage", "lookup"}, {"kb", "知识"}, {"order", "订单"}, {"order", "补充"}}, out.Turns)

	_, err = MergeTurns(context.Background(), base, []Conversation{{Input: "q"}})
	require.Error(t, err, "分支短于 base")
	_, err = MergeTurns(context.Background(), base, []Conversation{{Input: "q", Turns: []Turn{{"x", "y"}}}})
	require.Error(t, err, "前缀不同")
}

func textMember(t *testing.T, name string) Step[Conversation] {
	t.Helper()
	var s sessions
	step, err := TextStep(name, s.fake(func(context.Context, any) (any, error) { return "", nil }))
	require.NoError(t, err)
	return step
}

func TestTextNextValidation(t *testing.T) {
	_, err := TextNext()
	require.Error(t, err)
	for name, members := range map[string][]Step[Conversation]{
		"多行":     {{Name: "a\nb"}},
		"回车":     {{Name: "a\rb"}},
		"重复":     {textMember(t, "a"), textMember(t, "a")},
		"FINISH": {{Name: Finish}},
		"空名":     {{Name: ""}},
	} {
		_, err := TextNext(members...)
		require.Error(t, err, name)
	}
}

func TestTextNextReadsFirstLine(t *testing.T) {
	next, err := TextNext(textMember(t, "refund"), textMember(t, "logistics"))
	require.NoError(t, err)
	last := func(output string) Conversation {
		return Conversation{Input: "q", Turns: []Turn{{"triage", output}}}
	}
	for output, want := range map[string]string{
		"refund":                 "refund",
		"  logistics  \n理由：物流延误": "logistics",
		"FINISH\r\n":             Finish,
	} {
		got, err := next(context.Background(), last(output))
		require.NoError(t, err, output)
		require.Equal(t, want, got, output)
	}
	for name, c := range map[string]Conversation{
		"空记录":  {Input: "q"},
		"空首行":  last("\n refund"),
		"模糊匹配": last("退款（原因：重复扣款）"),
		"大小写":  last("Refund"),
		"前缀":   last("refund please"),
	} {
		_, err := next(context.Background(), c)
		require.ErrorIs(t, err, ErrInvalidRoute, name)
	}
}

func TestTextRouterWithScriptedOutputs(t *testing.T) {
	var s sessions
	decisions := []string{"refund", "FINISH"}
	calls := 0
	triage, err := TextStep("triage", s.fake(func(context.Context, any) (any, error) {
		calls++
		return decisions[calls-1], nil
	}))
	require.NoError(t, err)
	refund, err := TextStep("refund", s.fake(func(context.Context, any) (any, error) { return "已退款", nil }))
	require.NoError(t, err)
	next, err := TextNext(refund)
	require.NoError(t, err)
	g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{Supervisor: triage, Members: []Step[Conversation]{refund}, Next: next, MaxRounds: 2})
	require.NoError(t, err)
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(context.Background(), Conversation{Input: "退款"})
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint([]Turn{{"triage", "refund"}, {"refund", "已退款"}, {"triage", "FINISH"}}), fmt.Sprint(out.Turns))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 -run '^(TestTextStepDefaultInputHasAllTurns|TestTextStepWindowAndRender|TestTextStepOptionValidation|TestTextStepEncodesNonStringResult|TestCloneConversationIsolatesSharedCapacity|TestMergeTurns|TestTextNextValidation|TestTextNextReadsFirstLine|TestTextRouterWithScriptedOutputs)$' ./multiagent/`
Expected: FAIL，编译错误 `undefined: Conversation`、`undefined: TextStep`

- [ ] **Step 3: 写实现**

`out` 用 `slices.Clip` 后追加，保证不改写其他状态共享的底层数组。

`multiagent/text.go`：

```go
package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kart-io/goagent/builder"
)

// Turn 是文本接力中的一轮：执行该轮的 Step 名与其输出。
type Turn struct {
	Agent  string
	Output string
}

// Conversation 是文本接力的状态：原始任务与按顺序追加的各轮输出。
type Conversation struct {
	Input string
	Turns []Turn
}

type textConfig struct {
	render    func(Conversation) any
	lastTurns int
	renderSet bool
	lastSet   bool
}

// TextOption 配置 TextStep 的输入格式。
type TextOption func(*textConfig)

// WithRender 自定义 Agent 输入。render 必须是 Conversation 的纯函数：恢复时输出不同会使模型请求意图不一致；
// 修改 render 等同于修改 Agent 行为，须提升图版本。
func WithRender(render func(Conversation) any) TextOption {
	return func(c *textConfig) { c.render, c.renderSet = render, true }
}

// WithLastTurns 使用固定窗口：输入为 Input 加最近 k 轮。
func WithLastTurns(k int) TextOption {
	return func(c *textConfig) { c.lastTurns, c.lastSet = k, true }
}

// TextStep 把 Agent 接成文本接力的一步：默认输入为 Input 加每一轮的“[Agent]: Output”，按顺序逐行拼接、不截断；
// 输出取 AgentOutput.Result（字符串原样，其他类型按 JSON 编码），追加为 Turn{Agent: name}，空回答也记录。
func TextStep(name string, agent AgentFactory, opts ...TextOption) (Step[Conversation], error) {
	var cfg textConfig
	for _, opt := range opts {
		if opt == nil {
			return Step[Conversation]{}, fmt.Errorf("TextStep %q 收到 nil 选项", name)
		}
		opt(&cfg)
	}
	switch {
	case cfg.renderSet && cfg.lastSet:
		return Step[Conversation]{}, fmt.Errorf("TextStep %q 不能同时使用 WithRender 与 WithLastTurns", name)
	case cfg.renderSet && cfg.render == nil:
		return Step[Conversation]{}, fmt.Errorf("TextStep %q 的 WithRender 需要非 nil 函数", name)
	case cfg.lastSet && cfg.lastTurns <= 0:
		return Step[Conversation]{}, fmt.Errorf("TextStep %q 的 WithLastTurns 需要大于 0 的轮数", name)
	}
	render := cfg.render
	if render == nil {
		k := cfg.lastTurns
		render = func(c Conversation) any { return renderTurns(c, k) }
	}
	in := func(c Conversation) (any, error) { return render(c), nil }
	out := func(c Conversation, o *builder.AgentOutput) (Conversation, error) {
		text, err := resultText(o.Result)
		if err != nil {
			return c, err
		}
		// Clip 保证追加时分配新数组，不改写其他状态共享的底层数组。
		c.Turns = append(slices.Clip(c.Turns), Turn{Agent: name, Output: text})
		return c, nil
	}
	return AgentStep(name, agent, in, out)
}

// renderTurns 是默认输入格式；k > 0 时只保留最近 k 轮。
func renderTurns(c Conversation, k int) string {
	turns := c.Turns
	if k > 0 && len(turns) > k {
		turns = turns[len(turns)-k:]
	}
	var b strings.Builder
	b.WriteString(c.Input)
	for _, t := range turns {
		b.WriteString("\n[")
		b.WriteString(t.Agent)
		b.WriteString("]: ")
		b.WriteString(t.Output)
	}
	return b.String()
}

// resultText 把 Agent 结果转成文本：字符串原样，其他类型按 JSON 编码。
func resultText(result any) (string, error) {
	if s, ok := result.(string); ok {
		return s, nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("编码 Agent 结果: %w", err)
	}
	return string(data), nil
}

// CloneConversation 复制 Turns，供 ParallelConfig.Clone 使用；浅拷贝会让并行分支写同一底层数组。
func CloneConversation(c Conversation) Conversation {
	return Conversation{Input: c.Input, Turns: slices.Clone(c.Turns)}
}

// MergeTurns 按分支登记顺序，追加各分支在 base 之后新增的 Turns。分支的 Turns 短于 base 或前缀不同时返回错误。
func MergeTurns(_ context.Context, base Conversation, branches []Conversation) (Conversation, error) {
	out := CloneConversation(base)
	n := len(base.Turns)
	for i, b := range branches {
		if len(b.Turns) < n || !slices.Equal(b.Turns[:n], base.Turns) {
			return Conversation{}, fmt.Errorf("第 %d 个分支的记录不以合并前的记录为前缀", i+1)
		}
		out.Turns = append(out.Turns, b.Turns[n:]...)
	}
	return out, nil
}

// TextNext 从成员 Step 推导名称，返回供 RouterConfig.Next 使用的路由：取最后一轮输出的第一行并去掉首尾空白，
// 必须恰好等于某个成员名或 Finish；记录为空、首行为空或不匹配时返回包装 ErrInvalidRoute 的错误，不做模糊匹配。
func TextNext(members ...Step[Conversation]) (func(context.Context, Conversation) (string, error), error) {
	if len(members) == 0 {
		return nil, errors.New("TextNext 需要至少一个成员")
	}
	names := make(map[string]bool, len(members))
	for _, m := range members {
		n := m.Name
		if n == "" || strings.TrimSpace(n) != n || strings.ContainsAny(n, "\r\n") || n == Finish || names[n] {
			return nil, fmt.Errorf("TextNext 的成员名 %q 必须非空、单行、不重复且不等于 %s", n, Finish)
		}
		names[n] = true
	}
	return func(_ context.Context, c Conversation) (string, error) {
		if len(c.Turns) == 0 {
			return "", fmt.Errorf("%w：记录为空", ErrInvalidRoute)
		}
		line, _, _ := strings.Cut(c.Turns[len(c.Turns)-1].Output, "\n")
		line = strings.TrimSpace(line)
		if line == "" {
			return "", fmt.Errorf("%w：最后一轮输出的首行为空", ErrInvalidRoute)
		}
		if line != Finish && !names[line] {
			return "", fmt.Errorf("%w：%q 不是成员名或 %s", ErrInvalidRoute, line, Finish)
		}
		return line, nil
	}, nil
}
```
- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -count=1 -run '^(TestTextStepDefaultInputHasAllTurns|TestTextStepWindowAndRender|TestTextStepOptionValidation|TestTextStepEncodesNonStringResult|TestCloneConversationIsolatesSharedCapacity|TestMergeTurns|TestTextNextValidation|TestTextNextReadsFirstLine|TestTextRouterWithScriptedOutputs)$' ./multiagent/`
Expected: PASS

- [ ] **Step 5: 全包回归、vet 与格式检查**

Run: `go test -race -count=1 ./multiagent/... ./testing/mocks/... && go vet ./multiagent/... ./testing/mocks/... && gofumpt -l multiagent/text.go multiagent/text_test.go && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent/text.go multiagent/text_test.go`
Expected: 测试 PASS，vet 无输出，gofumpt 与 gci 不列出任何文件

- [ ] **Step 6: 提交**

```bash
git add multiagent/text.go multiagent/text_test.go
git commit -m "feat(multiagent): 新增文本接力层 TextStep 与 TextNext"
```


### Task 9: SupervisorTools

**Files:**
- Create: `multiagent/supervisor_tools.go`
- Test: `multiagent/supervisor_tools_test.go`

**Interfaces:**
- Consumes: Task 8 的 `resultText`；Task 2 的 `AgentStep`、`sessions`、`withTools`、`askText`、`keepAnswer`、`ticket`；Task 4 的 `newEnv`、`compile`；Task 5 的 `Sequential`；Task 1 的 `ScriptedClient`
- Produces: `type Member struct{ Name, Description, Version string; Factory AgentFactory; Resumable bool; Timeout time.Duration }`；`func SupervisorTools(members ...Member) ([]interfaces.Tool, error)`。测试辅助 `memoryBudget(t) context.Context`、`supervisorAgent(...)`、`runDelegation(t) *sessions`、`runResumableDelegation(t)`、`interruptibleConfirm(t)`（Task 12 的 S7 复用前两个委派辅助）。

- [ ] **Step 1: 写失败测试**

覆盖：校验表；直接 `Invoke`（带根预算、`Args` 为非 nil 空对象）缺 `task` 得 `*tools.ArgumentError`；没有根预算、持久日志下用非持久预算都失败；Factory 出错但返回 Close 时仍关闭；Timeout 为正覆盖 Dispatcher 默认并在超时后仍 Close、为 0 继承默认、直接 `Invoke` 不受约束；Supervisor 一个回合委派两个成员且每次新建会话并 Close；`ReturnArgumentErrors` 关闭时持久回合整体失败、开启时交还模型改正；Resumable 成员放进 `CompileDurable` 单节点图，经可中断工具等待，续跑后 `durable.OperationAttempt` 不增加；无持久日志时 Resumable 成员被拒。

`multiagent/supervisor_tools_test.go`：

```go
package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

func memoryBudget(t *testing.T) context.Context {
	t.Helper()
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 20})
	require.NoError(t, err)
	return budget.WithController(context.Background(), c)
}

func memberTool(t *testing.T, m Member) interfaces.Tool {
	t.Helper()
	list, err := SupervisorTools(m)
	require.NoError(t, err)
	return list[0]
}

func TestSupervisorToolsValidation(t *testing.T) {
	var s sessions
	factory := s.fake(func(context.Context, any) (any, error) { return "", nil })
	ok := Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: factory}
	_, err := SupervisorTools()
	require.Error(t, err)
	for name, m := range map[string]Member{
		"缺 Name":        {Description: "d", Version: "v1", Factory: factory},
		"缺 Description": {Name: "a", Version: "v1", Factory: factory},
		"缺 Version":     {Name: "a", Description: "d", Factory: factory},
		"缺 Factory":     {Name: "a", Description: "d", Version: "v1"},
		"负 Timeout":     {Name: "a", Description: "d", Version: "v1", Factory: factory, Timeout: -time.Second},
	} {
		_, err := SupervisorTools(m)
		require.Error(t, err, name)
	}
	_, err = SupervisorTools(ok, ok)
	require.ErrorContains(t, err, "重复")
}

func TestSupervisorToolsDirectInvoke(t *testing.T) {
	var s sessions
	tool := memberTool(t, Member{
		Name: "billing", Description: "账单专家", Version: "v1",
		Factory: s.fake(func(_ context.Context, in any) (any, error) { return map[string]string{"task": in.(string)}, nil }),
	})
	out, err := tool.Invoke(memoryBudget(t), &interfaces.ToolInput{Args: map[string]any{"task": "查账单"}})
	require.NoError(t, err)
	require.Equal(t, `{"task":"查账单"}`, out.Result, "非字符串结果按 JSON 编码")
	require.EqualValues(t, 1, s.closed.Load())

	_, err = tool.Invoke(memoryBudget(t), &interfaces.ToolInput{Args: map[string]any{}})
	var argErr *tools.ArgumentError
	require.ErrorAs(t, err, &argErr)

	_, err = tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"task": "x"}})
	require.ErrorContains(t, err, "root budget", "没有根预算时失败")

	e := newEnv(t, "nondurable-budget")
	journaled := durable.WithJournal(durable.WithExecutionKey(memoryBudget(t), "node"), e.store)
	_, err = tool.Invoke(journaled, &interfaces.ToolInput{Args: map[string]any{"task": "x"}})
	require.ErrorContains(t, err, "durable root budget", "持久运行中使用非持久预算时失败")
}

func TestSupervisorToolsClosesSessionWhenFactoryFails(t *testing.T) {
	closed := 0
	tool := memberTool(t, Member{
		Name: "billing", Description: "账单专家", Version: "v1",
		Factory: func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{Close: func(context.Context) error { closed++; return nil }}, errors.New("会话创建失败")
		},
	})
	_, err := tool.Invoke(memoryBudget(t), &interfaces.ToolInput{Args: map[string]any{"task": "x"}})
	require.ErrorContains(t, err, "会话创建失败")
	require.Equal(t, 1, closed)
}

// blocking 返回阻塞到 ctx 结束或 d 之后才回答的会话工厂。
func blocking(s *sessions, d time.Duration) AgentFactory {
	return s.fake(func(ctx context.Context, _ any) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d):
			return "完成", nil
		}
	})
}

func dispatch(t *testing.T, cfg tools.DispatchConfig, tool interfaces.Tool) *tools.Dispatcher {
	t.Helper()
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))
	d, err := tools.NewDispatcher(reg, cfg)
	require.NoError(t, err)
	return d
}

func TestSupervisorToolsTimeout(t *testing.T) {
	call := tools.ToolRequest{Name: "billing", Input: &interfaces.ToolInput{Args: map[string]any{"task": "x"}}}
	var own sessions
	timed := memberTool(t, Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: blocking(&own, 10*time.Second), Timeout: 50 * time.Millisecond})
	_, err := dispatch(t, tools.DispatchConfig{}, timed).Invoke(memoryBudget(t), call)
	require.ErrorIs(t, err, context.DeadlineExceeded, "正值覆盖 Dispatcher 默认超时")
	require.EqualValues(t, 1, own.closed.Load(), "超时取消后仍调用 Close")

	var inherited sessions
	tool := memberTool(t, Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: blocking(&inherited, 10*time.Second)})
	_, err = dispatch(t, tools.DispatchConfig{Timeout: 50 * time.Millisecond}, tool).Invoke(memoryBudget(t), call)
	require.ErrorIs(t, err, context.DeadlineExceeded, "0 继承 Dispatcher 默认")

	var direct sessions
	slow := memberTool(t, Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: blocking(&direct, 150*time.Millisecond), Timeout: 50 * time.Millisecond})
	out, err := slow.Invoke(memoryBudget(t), call.Input)
	require.NoError(t, err, "直接 Invoke 不受 Timeout 约束")
	require.Equal(t, "完成", out.Result)
}

// supervisorAgent 构造带成员工具的 Supervisor 会话工厂；Supervisor 使用独立 Dispatcher。
func supervisorAgent(t *testing.T, s *sessions, client *mocks.ScriptedClient, cfg tools.DispatchConfig, members ...Member) AgentFactory {
	t.Helper()
	list, err := SupervisorTools(members...)
	require.NoError(t, err)
	return s.scripted(client, "supervisor", withTools(t, cfg, list...))
}

// runDelegation 让 Supervisor 在一个回合内委派账单与技术两个成员，再汇总回答；返回成员会话统计。
func runDelegation(t *testing.T) *sessions {
	t.Helper()
	var members, sup sessions
	billing := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "账单", Match: mocks.RequestContains("核对扣款"), Reply: mocks.ScriptedReply{Text: "重复扣款 1 次"}})
	tech := mocks.NewScriptedClient(mocks.ScriptedRule{Name: "技术", Match: mocks.RequestContains("登录失败"), Reply: mocks.ScriptedReply{Text: "需重置密码"}})
	supervisor := mocks.NewScriptedClient(
		mocks.ToolCallReply("委派", mocks.ToolCall("c1", "billing", `{"task":"核对扣款"}`), mocks.ToolCall("c2", "tech", `{"task":"登录失败"}`)),
		mocks.ScriptedRule{Name: "汇总", Match: mocks.RequestContains("重复扣款 1 次", "需重置密码"), Reply: mocks.ScriptedReply{Text: "已退款并指导重置密码"}},
	)
	factory := supervisorAgent(t, &sup, supervisor, tools.DispatchConfig{},
		Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: members.scripted(billing, "billing", nil)},
		Member{Name: "tech", Description: "技术专家", Version: "v1", Factory: members.scripted(tech, "tech", nil)})
	step, err := AgentStep("supervisor", factory, askText, keepAnswer)
	require.NoError(t, err)
	g, err := Sequential(workflow.Config{}, step)
	require.NoError(t, err)
	run, err := g.Compile(context.Background())
	require.NoError(t, err)
	out, err := run.Invoke(memoryBudget(t), ticket{Text: "扣款两次且无法登录"})
	require.NoError(t, err)
	require.Equal(t, "已退款并指导重置密码", out.Answer)
	require.Zero(t, supervisor.Remaining())
	return &members
}

func TestSupervisorToolsDelegatesAndClosesEachSession(t *testing.T) {
	members := runDelegation(t)
	require.EqualValues(t, 2, members.opened.Load(), "每次调用新建会话")
	require.EqualValues(t, 2, members.closed.Load())
}

func TestSupervisorToolsArgumentErrors(t *testing.T) {
	for _, feedback := range []bool{false, true} {
		var members, sup sessions
		billing := mocks.NewScriptedClient(mocks.TextReply("账单", "已核对"))
		rules := []mocks.ScriptedRule{mocks.ToolCallReply("缺 task", mocks.ToolCall("c1", "billing", `{}`))}
		if feedback {
			rules = append(rules,
				mocks.ScriptedRule{Name: "改正", Match: mocks.RequestContains("task"), Reply: mocks.ScriptedReply{ToolCalls: []llm.ToolCall{mocks.ToolCall("c2", "billing", `{"task":"核对"}`)}}},
				mocks.TextReply("汇总", "完成"))
		}
		supervisor := mocks.NewScriptedClient(rules...)
		factory := supervisorAgent(t, &sup, supervisor, tools.DispatchConfig{ReturnArgumentErrors: feedback},
			Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: members.scripted(billing, "billing", nil)})
		step, err := AgentStep("supervisor", factory, askText, keepAnswer)
		require.NoError(t, err)
		g, err := Sequential(workflow.Config{}, step)
		require.NoError(t, err)
		e := newEnv(t, "argument-errors")
		snap, err := compile(t, e, g, "v1").Run(e.ctx(), e.id, ticket{Text: "核对扣款"}, "")
		if !feedback {
			require.ErrorContains(t, err, "invalid arguments", "持久运行默认拒绝整个模型回合")
			require.Zero(t, billing.Calls())
			continue
		}
		require.NoError(t, err)
		require.Equal(t, "完成", snap.State.Answer)
		require.Equal(t, 1, billing.Calls())
	}
}

// interruptibleConfirm 是成员 Agent 的可中断工具：等待客户确认后返回确认内容。
func interruptibleConfirm(t *testing.T) interfaces.Tool {
	t.Helper()
	tool, err := tools.NewFunctionToolBuilder("confirm").WithDescription("向客户确认").
		WithArgsSchema(`{"type":"object","properties":{}}`).WithInterruptible("v1").
		WithFunction(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
			raw, err := durable.AwaitInput(ctx, "confirm", json.RawMessage(`{"question":"确认账单？"}`), nil)
			if err != nil {
				return nil, err
			}
			return string(raw), nil
		}).Build()
	require.NoError(t, err)
	return tool
}

// runResumableDelegation 把带 Resumable 成员的 Supervisor 放进 CompileDurable 单节点图：
// 成员经可中断工具等待，续跑后完成，且成员所在的持久操作没有新增尝试。
func runResumableDelegation(t *testing.T) {
	t.Helper()
	e := newEnv(t, "resumable-member")
	member := mocks.NewScriptedClient(
		mocks.ToolCallReply("确认", mocks.ToolCall("m1", "confirm", `{}`)),
		mocks.ScriptedRule{Name: "完成", Match: mocks.RequestContains(`"yes"`), Reply: mocks.ScriptedReply{Text: "账单已核对"}},
	)
	supervisor := mocks.NewScriptedClient(
		mocks.ToolCallReply("委派", mocks.ToolCall("c1", "billing", `{"task":"核对账单"}`)),
		mocks.ScriptedRule{Name: "汇总", Match: mocks.RequestContains("账单已核对"), Reply: mocks.ScriptedReply{Text: "已核对"}},
	)
	var members, sup sessions
	var attempts []int
	inner := members.scripted(member, "billing", withTools(t, tools.DispatchConfig{}, interruptibleConfirm(t)))
	factory := func(ctx context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		attempts = append(attempts, durable.OperationAttempt(ctx))
		return inner(ctx)
	}
	build := func() *workflow.DurableGraph[ticket] {
		step, err := AgentStep("supervisor", supervisorAgent(t, &sup, supervisor, tools.DispatchConfig{},
			Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: factory, Resumable: true}), askText, keepAnswer)
		require.NoError(t, err)
		g, err := Sequential(workflow.Config{}, step)
		require.NoError(t, err)
		return compile(t, e, g, "v1")
	}
	snap, err := build().Run(e.ctx(), e.id, ticket{Text: "核对账单"}, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Len(t, snap.Interrupts, 1)

	e.restart()
	snap, err = build().Resume(e.ctx(), e.id, workflow.ResumeRequest{Key: snap.Interrupts[0].Key, Input: json.RawMessage(`"yes"`)})
	require.NoError(t, err)
	require.Equal(t, "已核对", snap.State.Answer)
	require.Equal(t, []int{1, 1}, attempts, "从等待续跑不计尝试")
	require.Equal(t, 2, supervisor.Calls())
	require.Equal(t, 2, member.Calls())
}

func TestSupervisorToolsResumableMember(t *testing.T) {
	runResumableDelegation(t)

	var s sessions
	tool := memberTool(t, Member{
		Name: "billing", Description: "账单专家", Version: "v1", Resumable: true,
		Factory: s.fake(func(context.Context, any) (any, error) { return "x", nil }),
	})
	_, err := tool.Invoke(memoryBudget(t), &interfaces.ToolInput{Args: map[string]any{"task": "x"}})
	require.ErrorContains(t, err, "requires a journaled dispatcher call", "无持久日志的调用被拒绝")
	require.Zero(t, s.opened.Load())
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 -run '^(TestSupervisorToolsValidation|TestSupervisorToolsDirectInvoke|TestSupervisorToolsClosesSessionWhenFactoryFails|TestSupervisorToolsTimeout|TestSupervisorToolsDelegatesAndClosesEachSession|TestSupervisorToolsArgumentErrors|TestSupervisorToolsResumableMember)$' ./multiagent/`
Expected: FAIL，编译错误 `undefined: SupervisorTools`、`undefined: Member`

- [ ] **Step 3: 写实现**

参数 Schema 固定为 `{"type":"object","properties":{"task":{"type":"string"}},"required":["task"],"additionalProperties":false}`，`ModelInvoking` 固定为 true；`Decode` 只做类型断言。

`multiagent/supervisor_tools.go`：

```go
package multiagent

import (
	"errors"
	"fmt"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Member 是 SupervisorTools 的一个成员 Agent。Version 随成员的行为、提示词或工具集变化而提升。
type Member struct {
	Name, Description, Version string
	Factory                    AgentFactory
	Resumable                  bool
	Timeout                    time.Duration // 0 继承 Dispatcher 默认；< 0 构造报错；只在经 Dispatcher 调用时生效
}

const memberArgsSchema = `{"type":"object","properties":{"task":{"type":"string"}},"required":["task"],"additionalProperties":false}`

// SupervisorTools 把每个成员包装成模型可调用的工具：参数为 {"task": string}，每次调用新建会话。
// 宿主把返回的工具注册到 Supervisor Agent 的 Dispatcher，再经 Dispatcher.Tools() 交给 Agent；
// 调用时 ctx 必须带根预算，持久运行中必须是持久预算。
func SupervisorTools(members ...Member) ([]interfaces.Tool, error) {
	if len(members) == 0 {
		return nil, errors.New("SupervisorTools 需要至少一个成员")
	}
	seen := map[string]bool{}
	out := make([]interfaces.Tool, 0, len(members))
	for _, m := range members {
		switch {
		case m.Name == "" || m.Description == "" || m.Version == "" || m.Factory == nil:
			return nil, fmt.Errorf("成员 %q 需要 Name、Description、Version 与 Factory", m.Name)
		case seen[m.Name]:
			return nil, fmt.Errorf("成员名 %q 重复", m.Name)
		case m.Timeout < 0:
			return nil, fmt.Errorf("成员 %q 的 Timeout 不能为负", m.Name)
		}
		seen[m.Name] = true
		tool, err := tools.NewRunnableToolFactory(m.Factory, tools.RunnableToolConfig[any, *builder.AgentOutput]{
			Name: m.Name, Description: m.Description, ArgsSchema: memberArgsSchema, Version: m.Version,
			Decode: decodeTask, Encode: encodeAgentResult,
			ModelInvoking: true, Resumable: m.Resumable, Timeout: m.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("成员 %q: %w", m.Name, err)
		}
		out = append(out, tool)
	}
	return out, nil
}

// decodeTask 只做类型断言：Schema 已在 Decode 前保证 task 存在且为字符串。
func decodeTask(args map[string]any) (any, error) {
	task, ok := args["task"].(string)
	if !ok {
		return nil, &tools.ArgumentError{Message: "task 必须是字符串"}
	}
	return task, nil
}

func encodeAgentResult(o *builder.AgentOutput) (any, error) {
	if o == nil {
		return nil, errors.New("成员 Agent 没有返回输出")
	}
	return resultText(o.Result)
}
```
- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -count=1 -run '^(TestSupervisorToolsValidation|TestSupervisorToolsDirectInvoke|TestSupervisorToolsClosesSessionWhenFactoryFails|TestSupervisorToolsTimeout|TestSupervisorToolsDelegatesAndClosesEachSession|TestSupervisorToolsArgumentErrors|TestSupervisorToolsResumableMember)$' ./multiagent/`
Expected: PASS

- [ ] **Step 5: 全包回归、vet 与格式检查**

Run: `go test -race -count=1 ./multiagent/... ./testing/mocks/... && go vet ./multiagent/... ./testing/mocks/... && gofumpt -l multiagent/supervisor_tools.go multiagent/supervisor_tools_test.go && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent/supervisor_tools.go multiagent/supervisor_tools_test.go`
Expected: 测试 PASS，vet 无输出，gofumpt 与 gci 不列出任何文件

- [ ] **Step 6: 提交**

```bash
git add multiagent/supervisor_tools.go multiagent/supervisor_tools_test.go
git commit -m "feat(multiagent): 新增工具型 Supervisor 的 SupervisorTools"
```


### Task 10: apicheck 导入规则与层级文档

**Files:**
- Modify: `scripts/apicheck/main.go:36-49`（`forbidden` 中 execution 与 plugin 规则）
- Modify: `scripts/apicheck/main_test.go`（末尾追加用例）
- Modify: `docs/architecture/IMPORT_LAYERING.md`（规则表）

**Interfaces:**
- Consumes: 现有 `forbidden(file, path string) bool`、常量 `module`
- Produces: 新规则：上述 16 个源目录禁止导入 `multiagent`；`multiagent` 加入 execution 与 plugin 规则的源目录。

- [ ] **Step 1: 写失败测试**

在 `scripts/apicheck/main_test.go` 末尾追加：

```go
func TestMultiagentIsAboveMechanismLayers(t *testing.T) {
	for _, file := range []string{"interfaces/x.go", "errors/x.go", "utils/x.go", "cache/x.go", "core/x.go", "builder/x.go", "tools/x.go", "parsers/x.go", "llm/x.go", "workflow/x.go", "memory/x.go", "store/x.go", "middleware/x.go", "observability/x.go", "devtools/x.go", "harness/env/x.go"} {
		require.True(t, forbidden(file, module+"multiagent"), file)
	}
	for _, file := range []string{"examples/multi-agent-patterns/main.go", "execution/scope.go", "plugin/install.go", "evaluation/bench/x.go", "testing/mocks/x.go"} {
		require.False(t, forbidden(file, module+"multiagent"), file)
	}
	for _, dep := range []string{"core", "builder", "tools", "workflow", "interfaces"} {
		require.False(t, forbidden("multiagent/x.go", module+dep), dep)
	}
	require.True(t, forbidden("multiagent/x.go", module+"execution"), "不得上行导入宿主装配层")
	require.True(t, forbidden("multiagent/x.go", module+"plugin"), "不得上行导入能力装配层")
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 -run '^TestMultiagentIsAboveMechanismLayers$' ./scripts/apicheck/`
Expected: FAIL，`interfaces/x.go` 等用例断言 `forbidden` 为 true 失败

- [ ] **Step 3: 修改规则**

在 `scripts/apicheck/main.go` 中把

```go
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability", "devtools", "harness":
			return true
		}
	}
	// plugin
```

替换为

```go
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability", "devtools", "harness", "multiagent":
			return true
		}
	}
	// multiagent 组合 workflow、builder 与 tools 的预置模式，基础与执行机制不得反向依赖它。
	if dest == "multiagent" {
		switch source {
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability", "devtools", "harness":
			return true
		}
	}
	// plugin
```

再把 plugin 规则中的

```go
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability":
			return true
```

替换为

```go
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability", "multiagent":
			return true
```

- [ ] **Step 4: 同步层级文档**

在 `docs/architecture/IMPORT_LAYERING.md` 规则表中把

```markdown
| `interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness` | 顶层 `execution` |
| `interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability` | 顶层 `plugin`（能力装配层） |
```

替换为

```markdown
| `interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness`、`multiagent` | 顶层 `execution` |
| `interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`multiagent` | 顶层 `plugin`（能力装配层） |
| `interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness` | 顶层 `multiagent`（多 Agent 预置模式） |
```

- [ ] **Step 5: 运行测试与检查器**

Run: `go test -count=1 ./scripts/apicheck/ && bash scripts/verify_imports.sh`
Expected: 测试 PASS；检查器输出 `imports: 0 findings`

- [ ] **Step 6: vet**

Run: `go vet ./scripts/apicheck/`
Expected: 无输出（不要对 `main.go` 运行 gofumpt -w，见 Global Constraints）

- [ ] **Step 7: 提交**

```bash
git add scripts/apicheck/main.go scripts/apicheck/main_test.go docs/architecture/IMPORT_LAYERING.md
git commit -m "chore(apicheck): 禁止基础与执行机制包反向导入 multiagent"
```


### Task 11: 持久恢复测试

**Files:**
- Test: `multiagent/durable_test.go`

**Interfaces:**
- Consumes: Task 4 的 `env`、`newEnv`、`restart`、`compile`、`inspect`、`keyed`、`fault.arm`、`operation`、`checkpoint`、`errInjected`；Task 2 的 `sessions.scripted`、`withTools`、`countingTool`、`askText`、`keepAnswer`、`ticket`、`mark`；Task 6 的 `untilRounds`；Task 7 的 `RouterSupervisor`；Task 8 的 `TextStep`、`TextNext`
- Produces: 测试辅助 `agentGraph(t, AgentFactory) *workflow.Graph[ticket]`、`crashBeforeNodeCommit(*env)`、`operationFails(id, status string) func(string, *durable.Snapshot) bool`（Task 14 复用）、`triageGraph(...)`、`routedTo(node string)`。

- [ ] **Step 1: 写测试**

本任务只加测试，验证 Task 2–8 的实现与 workflow、builder 的组合。故障窗口一律由包装 Store 精确定位；`ScriptedClient` 实例跨重启保留，“`Calls()` 不增加”才有意义。逐条对应规格“持久恢复测试”：

| 规格条目 | 测试 |
|---|---|
| 模型回放 | `TestDurableModelReplay`（同时断言根预算 `ReservedRequests == 1`，即恢复后不重复扣减） |
| 工具 completed 回放（`model/turn/1` 的 pending 保存失败且恰好命中一次） | `TestDurableCompletedToolReplay` |
| 工具 pending：默认拒绝；Resumable 第 4 次尝试耗尽 | `TestDurablePendingTool` |
| 模型请求中断四个子测试（pending/canceled × 默认/开启重发） | `TestDurableModelRequestInterrupted` |
| 跨轮隔离 | `TestDurableLoopRoundsUseDistinctKeys` |
| 预算 (a)(b)(c) | `TestDurableBudgetConfiguration` |
| 兜底与无兜底的终态 | `TestDurableFallback` |
| 兜底的重新路由窗口（Router 与 Loop） | `TestDurableFallbackRerouteWindow` |
| render | `TestDurableRenderDeterminism` |
| 契约前提 | `TestDurableContractPreconditions` |
| 版本 | `TestDurableVersionMigration` |

模型中断测试的 Agent 配置了经 `Dispatcher.Tools()` 包装的工具，请求走 `model/turn/0`；pending 子测试必须重新读取存储（`inspect` 经 `durable.InspectOperation` 先取操作锁），因为 `Do` 返回的内存记录已是 completed。deadline 与 execution 两种失败由 `core/durable/operation_test.go:144-180`、`builder/model_resend_test.go:151` 覆盖，重发与 RequestError 重试叠加由 `builder/model_resend_test.go:164-186` 覆盖，本任务不重复。

`multiagent/durable_test.go`：

```go
package multiagent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

func agentGraph(t *testing.T, factory AgentFactory) *workflow.Graph[ticket] {
	t.Helper()
	step, err := AgentStep("answer", factory, askText, keepAnswer)
	require.NoError(t, err)
	g, err := Sequential(workflow.Config{}, step)
	require.NoError(t, err)
	return g
}

// crashBeforeNodeCommit 让第一个节点输出的检查点保存失败一次：节点内的模型操作已提交，节点输出未提交。
func crashBeforeNodeCommit(e *env) {
	e.fault.arm(false, func(_ string, s *durable.Snapshot) bool {
		v, ok := checkpoint(s)
		return ok && v.Routing && s.Next == 1
	})
}

func operationFails(id, status string) func(string, *durable.Snapshot) bool {
	return func(_ string, s *durable.Snapshot) bool {
		r, ok := operation(s)
		return ok && r.ID == id && r.Status == status
	}
}

func TestDurableModelReplay(t *testing.T) {
	e := newEnv(t, "model-replay")
	client := mocks.NewScriptedClient(mocks.TextReply("回答", "已处理"))
	var s sessions
	crashBeforeNodeCommit(e)
	_, err := compile(t, e, agentGraph(t, s.scripted(client, "answer", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.ErrorIs(t, err, errInjected)
	require.Equal(t, 1, e.fault.Hits())

	e.restart()
	snap, err := compile(t, e, agentGraph(t, s.scripted(client, "answer", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.NoError(t, err)
	require.Equal(t, "已处理", snap.State.Answer)
	require.Equal(t, 1, client.Calls(), "恢复后从日志回放模型请求")
	used, err := e.controller.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, used.ReservedRequests, "恢复后根预算不重复扣减")
}

func TestDurableCompletedToolReplay(t *testing.T) {
	e := newEnv(t, "tool-replay")
	var lookups atomic.Int32
	client := mocks.NewScriptedClient(mocks.ToolCallReply("查询", mocks.ToolCall("c1", "lookup", `{}`)), mocks.TextReply("回答", "订单已发货"))
	var s sessions
	build := func() *workflow.DurableGraph[ticket] {
		return compile(t, e, agentGraph(t, s.scripted(client, "answer", withTools(t, tools.DispatchConfig{}, countingTool(t, "lookup", "已发货", &lookups)))), "v1")
	}
	e.fault.arm(false, operationFails("model/turn/1", "pending"))
	_, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.ErrorIs(t, err, errInjected)
	require.Equal(t, 1, e.fault.Hits())

	e.restart()
	snap, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.NoError(t, err)
	require.Equal(t, "订单已发货", snap.State.Answer)
	require.EqualValues(t, 1, lookups.Load(), "已完成的工具不再执行")
	require.Equal(t, 2, client.Calls())
}

func runnableLookup(t *testing.T, resumable bool, calls *atomic.Int32) interfaces.Tool {
	t.Helper()
	tool, err := tools.NewRunnableTool(core.NewRunnableFunc(func(context.Context, string) (string, error) {
		calls.Add(1)
		return "已发货", nil
	}), tools.RunnableToolConfig[string, string]{
		Name: "lookup", Description: "查询订单", Version: "v1", Resumable: resumable,
		ArgsSchema: `{"type":"object","properties":{}}`,
		Decode:     func(map[string]any) (string, error) { return "", nil },
		Encode:     func(s string) (any, error) { return s, nil },
	})
	require.NoError(t, err)
	return tool
}

func TestDurablePendingTool(t *testing.T) {
	t.Run("默认工具拒绝", func(t *testing.T) {
		e := newEnv(t, "tool-pending")
		var calls atomic.Int32
		client := mocks.NewScriptedClient(mocks.ToolCallReply("查询", mocks.ToolCall("c1", "lookup", `{}`)), mocks.TextReply("回答", "x"))
		var s sessions
		build := func() *workflow.DurableGraph[ticket] {
			return compile(t, e, agentGraph(t, s.scripted(client, "answer", withTools(t, tools.DispatchConfig{}, runnableLookup(t, false, &calls)))), "v1")
		}
		e.fault.arm(false, operationFails("tool/turn/0/call/c1", "completed"))
		_, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		require.ErrorIs(t, err, errInjected)
		e.restart()
		_, err = build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		var opErr *durable.OperationError
		require.ErrorAs(t, err, &opErr)
		require.Equal(t, "ambiguous", opErr.Kind)
		require.EqualValues(t, 1, calls.Load())
	})
	t.Run("Resumable 工具重入三次", func(t *testing.T) {
		e := newEnv(t, "tool-pending-resumable")
		var calls atomic.Int32
		client := mocks.NewScriptedClient(mocks.ToolCallReply("查询", mocks.ToolCall("c1", "lookup", `{}`)), mocks.TextReply("回答", "x"))
		var s sessions
		build := func() *workflow.DurableGraph[ticket] {
			return compile(t, e, agentGraph(t, s.scripted(client, "answer", withTools(t, tools.DispatchConfig{}, runnableLookup(t, true, &calls)))), "v1")
		}
		e.fault.arm(true, operationFails("tool/turn/0/call/c1", "completed"))
		for attempt := 1; attempt <= 3; attempt++ {
			_, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
			require.ErrorIs(t, err, errInjected, "第 %d 次尝试", attempt)
			e.restart()
		}
		_, err := build().Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		var opErr *durable.OperationError
		require.ErrorAs(t, err, &opErr, "第 4 次尝试时尝试耗尽")
		require.EqualValues(t, 3, calls.Load())
		require.Equal(t, 1, client.Calls(), "模型回合每次都从日志回放")
	})
}

// interruptedModel 构造模型请求中断测试的 Agent：配置一个经 Dispatcher 包装的工具，使请求走 model/turn/0。
func interruptedModel(t *testing.T, e *env, client *mocks.ScriptedClient, resend bool, key *atomic.Value) *workflow.DurableGraph[ticket] {
	t.Helper()
	var lookups atomic.Int32
	var s sessions
	tool := withTools(t, tools.DispatchConfig{}, countingTool(t, "lookup", "x", &lookups))
	factory := s.scripted(client, "answer", func(b *builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder {
		b = tool(b)
		if resend {
			b = b.WithInterruptedModelResend()
		}
		return b
	})
	return compile(t, e, agentGraph(t, keyed(factory, key)), "v1")
}

func TestDurableModelRequestInterrupted(t *testing.T) {
	for _, resend := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending/resend=%v", resend), func(t *testing.T) {
			e := newEnv(t, "model-pending")
			client := mocks.NewScriptedClient(mocks.TextReply("首次", "丢失"), mocks.TextReply("重发", "已处理"))
			var key atomic.Value
			e.fault.arm(false, operationFails("model/turn/0", "completed"))
			_, err := interruptedModel(t, e, client, resend, &key).Run(e.ctx(), e.id, ticket{Text: "q"}, "")
			require.ErrorIs(t, err, errInjected)
			require.Equal(t, 1, e.fault.Hits())
			// Do 返回的内存记录已是 completed，必须重新读取存储确认落盘的是 pending。
			r := inspect(t, e, key.Load().(string), "answer", "model/turn/0")
			require.Equal(t, "pending", r.Status)
			require.Equal(t, "ambiguous", r.FailureKind)
			require.Equal(t, 1, r.Attempts)
			require.Equal(t, 1, client.Calls())

			e.restart()
			snap, err := interruptedModel(t, e, client, resend, &key).Run(e.ctx(), e.id, ticket{Text: "q"}, "")
			if !resend {
				var opErr *durable.OperationError
				require.ErrorAs(t, err, &opErr)
				require.Equal(t, "ambiguous", opErr.Kind)
				require.Equal(t, 1, client.Calls())
				return
			}
			require.NoError(t, err)
			require.Equal(t, "已处理", snap.State.Answer)
			require.Equal(t, 2, client.Calls())
			require.Equal(t, 2, inspect(t, e, key.Load().(string), "answer", "model/turn/0").Attempts)
		})
		t.Run(fmt.Sprintf("canceled/resend=%v", resend), func(t *testing.T) {
			e := newEnv(t, "model-canceled")
			started := make(chan struct{})
			client := mocks.NewScriptedClient(
				mocks.ScriptedRule{Name: "挂起", Respond: func(ctx context.Context, _ *llm.CompletionRequest) (mocks.ScriptedReply, error) {
					close(started)
					<-ctx.Done()
					return mocks.ScriptedReply{}, ctx.Err()
				}},
				mocks.TextReply("重发", "已处理"),
			)
			var key atomic.Value
			ctx, stop := context.WithCancel(e.ctx())
			done := make(chan error, 1)
			d := interruptedModel(t, e, client, resend, &key)
			go func() {
				_, err := d.Run(ctx, e.id, ticket{Text: "q"}, "")
				done <- err
			}()
			<-started
			stop()
			require.ErrorIs(t, <-done, context.Canceled)
			// Agent 的流在取消时立即返回，内部可能尚未保存终态；InspectOperation 先取操作锁再读。
			r := inspect(t, e, key.Load().(string), "answer", "model/turn/0")
			require.Equal(t, "failed", r.Status)
			require.Equal(t, "canceled", r.FailureKind)

			e.restart()
			snap, err := interruptedModel(t, e, client, resend, &key).Run(e.ctx(), e.id, ticket{Text: "q"}, "")
			if !resend {
				var opErr *durable.OperationError
				require.ErrorAs(t, err, &opErr)
				require.Equal(t, "canceled", opErr.Kind)
				require.Equal(t, 1, client.Calls())
				return
			}
			require.NoError(t, err)
			require.Equal(t, "已处理", snap.State.Answer)
			require.Equal(t, 2, client.Calls())
		})
	}
}

func TestDurableLoopRoundsUseDistinctKeys(t *testing.T) {
	e := newEnv(t, "loop-keys")
	client := mocks.NewScriptedClient(mocks.TextReply("第一稿", "稿一"), mocks.TextReply("第二稿", "稿二"))
	var s sessions
	var keys []string
	inner := s.scripted(client, "writer", nil)
	factory := func(ctx context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		keys = append(keys, workflow.DurableKey(ctx))
		return inner(ctx)
	}
	write, err := TextStep("write", factory)
	require.NoError(t, err)
	g, err := Loop(workflow.Config{}, LoopConfig[Conversation]{
		Steps: []Step[Conversation]{write}, MaxIterations: 2,
		Until: func(_ context.Context, c Conversation) (bool, error) { return len(c.Turns) >= 2, nil },
	})
	require.NoError(t, err)
	snap, err := compile(t, e, g, "v1").Run(e.ctx(), e.id, Conversation{Input: "写回复"}, "")
	require.NoError(t, err)
	require.Len(t, snap.State.Turns, 2)
	require.Len(t, keys, 2)
	require.NotEqual(t, keys[0], keys[1], "执行键包含步数")
	first := inspect(t, e, keys[0], "writer", "model/answer/0")
	second := inspect(t, e, keys[1], "writer", "model/answer/0")
	require.Equal(t, "completed", first.Status)
	require.Equal(t, "completed", second.Status)
	require.NotEqual(t, string(first.Value), string(second.Value), "两轮日志互不覆盖")
}

func TestDurableBudgetConfiguration(t *testing.T) {
	crashed := func(t *testing.T) (*env, *mocks.ScriptedClient) {
		e := newEnv(t, "budget-config")
		client := mocks.NewScriptedClient(mocks.TextReply("回答", "已处理"))
		var s sessions
		crashBeforeNodeCommit(e)
		_, err := compile(t, e, agentGraph(t, s.scripted(client, "answer", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		require.ErrorIs(t, err, errInjected)
		return e, client
	}
	resume := func(t *testing.T, e *env, client *mocks.ScriptedClient) error {
		var s sessions
		e.restart()
		_, err := compile(t, e, agentGraph(t, s.scripted(client, "answer", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
		return err
	}
	t.Run("复用预算存储但配置变化", func(t *testing.T) {
		e, _ := crashed(t)
		budgetStore, err := durable.NewFileStore(e.budgetDir)
		require.NoError(t, err)
		changed := e.budget
		changed.DeadlineUnixMilli = 4102444800000 // 重启时重新计算截止时间
		_, err = budget.NewDurable(changed, budgetStore)
		require.ErrorContains(t, err, "budget configuration or record changed")
	})
	t.Run("新预算存储且配置变化", func(t *testing.T) {
		e, client := crashed(t)
		e.budgetDir = t.TempDir()
		e.budget.MaxModelRequests++
		require.ErrorContains(t, resume(t, e, client), "operation intent or attempt policy changed")
		require.Equal(t, 1, client.Calls())
	})
	t.Run("新预算存储且配置相同", func(t *testing.T) {
		e, client := crashed(t)
		e.budgetDir = t.TempDir()
		require.ErrorContains(t, resume(t, e, client), "usage has no reservation")
		require.Equal(t, 1, client.Calls())
	})
}

// triageGraph 构造文本分流：triage 的输出首行决定成员；fallback 为真时配置转人工兜底。
func triageGraph(t *testing.T, triage, refund *mocks.ScriptedClient, fallback bool, human *[]Conversation) *workflow.Graph[Conversation] {
	t.Helper()
	var s sessions
	sup, err := TextStep("triage", s.scripted(triage, "triage", nil))
	require.NoError(t, err)
	member, err := TextStep("refund", s.scripted(refund, "refund", nil))
	require.NoError(t, err)
	next, err := TextNext(member)
	require.NoError(t, err)
	cfg := RouterConfig[Conversation]{Supervisor: sup, Members: []Step[Conversation]{member}, Next: next, MaxRounds: 3}
	if fallback {
		cfg.Fallback = &Step[Conversation]{Name: "human", Run: core.NewRunnableFunc(func(_ context.Context, c Conversation) (Conversation, error) {
			*human = append(*human, c)
			c.Turns = append(c.Turns, Turn{Agent: "human", Output: "已转人工"})
			return c, nil
		})}
	}
	g, err := RouterSupervisor(workflow.Config{}, cfg)
	require.NoError(t, err)
	return g
}

func TestDurableFallback(t *testing.T) {
	e := newEnv(t, "fallback")
	triage := mocks.NewScriptedClient(mocks.TextReply("无效分流", "退款（原因：重复扣款）"))
	var human []Conversation
	snap, err := compile(t, e, triageGraph(t, triage, mocks.NewScriptedClient(), true, &human), "v1").Run(e.ctx(), e.id, Conversation{Input: "重复扣款"}, "")
	require.NoError(t, err)
	require.Len(t, human, 1)
	require.Equal(t, "已转人工", snap.State.Turns[1].Output)

	e = newEnv(t, "no-fallback")
	triage = mocks.NewScriptedClient(mocks.TextReply("无效分流", "退款（原因：重复扣款）"))
	d := compile(t, e, triageGraph(t, triage, mocks.NewScriptedClient(), false, nil), "v1")
	_, err = d.Run(e.ctx(), e.id, Conversation{Input: "重复扣款"}, "")
	require.ErrorIs(t, err, ErrInvalidRoute)
	_, err = d.Run(e.ctx(), e.id, Conversation{Input: "重复扣款"}, "")
	require.ErrorIs(t, err, ErrInvalidRoute, "无兜底时对该运行 ID 是终态")
	require.Equal(t, 1, triage.Calls())
}

// routedTo 匹配“路由结果为 node”的那次检查点保存：非操作记录、Key 为空、Next > 0、Routing 为假。
func routedTo(node string) func(string, *durable.Snapshot) bool {
	return func(_ string, s *durable.Snapshot) bool {
		v, ok := checkpoint(s)
		return ok && s.Key == "" && s.Next > 0 && !v.Routing && v.Node == node
	}
}

func TestDurableFallbackRerouteWindow(t *testing.T) {
	t.Run("Router", func(t *testing.T) {
		e := newEnv(t, "reroute-router")
		triage := mocks.NewScriptedClient(mocks.TextReply("无效分流", "不知道"))
		var human []Conversation
		e.fault.arm(false, routedTo("human"))
		d := compile(t, e, triageGraph(t, triage, mocks.NewScriptedClient(), true, &human), "v1")
		_, err := d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, errInjected)
		require.Equal(t, 1, e.fault.Hits())
		snap, err := d.Snapshot(context.Background(), e.id)
		require.NoError(t, err)
		require.Equal(t, "triage", snap.Node)
		require.True(t, snap.Routing)

		e.restart()
		_, err = compile(t, e, triageGraph(t, triage, mocks.NewScriptedClient(), true, &human), "v1").Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.NoError(t, err)
		require.Equal(t, 1, triage.Calls(), "重新路由不重新请求分流模型")
		require.Len(t, human, 1)
		require.Equal(t, "不知道", human[0].Turns[0].Output, "兜底收到无效的那次输出")
	})
	t.Run("Loop", func(t *testing.T) {
		e := newEnv(t, "reroute-loop")
		var fallbacks atomic.Int32
		build := func() *workflow.DurableGraph[record] {
			fb := Step[record]{Name: "human", Run: core.NewRunnableFunc(func(_ context.Context, r record) (record, error) {
				fallbacks.Add(1)
				return r, nil
			})}
			g, err := Loop(workflow.Config{}, LoopConfig[record]{
				Steps: []Step[record]{mark("review")}, MaxIterations: 2, Fallback: &fb,
				Until: func(context.Context, record) (bool, error) { return false, ErrInvalidRoute },
			})
			require.NoError(t, err)
			return compile(t, e, g, "v1")
		}
		e.fault.arm(false, routedTo("human"))
		_, err := build().Run(e.ctx(), e.id, record{}, "")
		require.ErrorIs(t, err, errInjected)
		e.restart()
		snap, err := build().Run(e.ctx(), e.id, record{}, "")
		require.NoError(t, err)
		require.Equal(t, []string{"review"}, snap.State.Log)
		require.EqualValues(t, 1, fallbacks.Load())
	})
}

func TestDurableRenderDeterminism(t *testing.T) {
	graph := func(t *testing.T, client *mocks.ScriptedClient, render func(Conversation) any) *workflow.Graph[Conversation] {
		var s sessions
		step, err := TextStep("answer", s.scripted(client, "answer", nil), WithRender(render))
		require.NoError(t, err)
		g, err := Sequential(workflow.Config{}, step)
		require.NoError(t, err)
		return g
	}
	upper := func(c Conversation) any { return strings.ToUpper(c.Input) }
	for name, after := range map[string]func(Conversation) any{
		"相同 render 回放": upper,
		"替换 render 报错": func(c Conversation) any { return "新格式：" + c.Input },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, "render")
			client := mocks.NewScriptedClient(mocks.TextReply("回答", "ok"))
			crashBeforeNodeCommit(e)
			_, err := compile(t, e, graph(t, client, upper), "v1").Run(e.ctx(), e.id, Conversation{Input: "refund"}, "")
			require.ErrorIs(t, err, errInjected)
			e.restart()
			_, err = compile(t, e, graph(t, client, after), "v1").Run(e.ctx(), e.id, Conversation{Input: "refund"}, "")
			if name == "相同 render 回放" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "operation intent or attempt policy changed")
			}
			require.Equal(t, 1, client.Calls())
		})
	}
}

func TestDurableContractPreconditions(t *testing.T) {
	e := newEnv(t, "contract")
	client := mocks.NewScriptedClient(mocks.TextReply("不应发出", "x"))
	var s sessions
	_, err := compile(t, e, agentGraph(t, s.scripted(client, "", nil)), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.ErrorContains(t, err, "WithDurableScope")

	e = newEnv(t, "contract-tool")
	var calls atomic.Int32
	raw := countingTool(t, "lookup", "x", &calls)
	_, err = compile(t, e, agentGraph(t, s.scripted(client, "answer", func(b *builder.SimpleAgentBuilder) *builder.SimpleAgentBuilder {
		return b.WithTools(raw)
	})), "v1").Run(e.ctx(), e.id, ticket{Text: "q"}, "")
	require.ErrorContains(t, err, "Dispatcher.Tools")
	require.Zero(t, client.Calls(), "在模型调用前失败")
}

func TestDurableVersionMigration(t *testing.T) {
	e := newEnv(t, "version")
	loop := func(iterations int) *workflow.Graph[record] {
		g, err := Loop(workflow.Config{}, LoopConfig[record]{Steps: []Step[record]{mark("write"), mark("review")}, Until: untilRounds("review", 2), MaxIterations: iterations})
		require.NoError(t, err)
		return g
	}
	old := compile(t, e, loop(2), "v1", "review")
	snap, err := old.Run(context.Background(), e.id, record{}, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)

	_, err = compile(t, e, loop(3), "v1", "review").Run(context.Background(), e.id, record{}, snap.Key)
	require.ErrorContains(t, err, "checkpoint does not match versioned graph")
	_, err = compile(t, e, loop(3), "v2", "review").Run(context.Background(), e.id, record{}, snap.Key)
	require.ErrorContains(t, err, "checkpoint does not match versioned graph", "提升 Version 也不能恢复旧运行")

	stored, err := e.store.Load(context.Background(), e.id)
	require.NoError(t, err)
	require.Equal(t, "v1", stored.Version, "检查点自带版本，宿主据此选择旧图")
	snap, err = compile(t, e, loop(2), "v1", "review").Run(context.Background(), e.id, record{}, snap.Key)
	require.ErrorIs(t, err, durable.ErrApprovalRequired, "第二轮评审仍需审批")
	snap, err = compile(t, e, loop(2), "v1", "review").Run(context.Background(), e.id, record{}, snap.Key)
	require.NoError(t, err)
	require.Equal(t, []string{"write", "review", "write", "review"}, snap.State.Log)
}
```

- [ ] **Step 2: 运行测试**

Run: `go test -race -count=1 -run '^(TestDurableModelReplay|TestDurableCompletedToolReplay|TestDurablePendingTool|TestDurableModelRequestInterrupted|TestDurableLoopRoundsUseDistinctKeys|TestDurableBudgetConfiguration|TestDurableFallback|TestDurableFallbackRerouteWindow|TestDurableRenderDeterminism|TestDurableContractPreconditions|TestDurableVersionMigration)$' ./multiagent/`
Expected: PASS。若失败，按 superpowers:systematic-debugging 定位：多数是故障匹配条件与真实记录不符（先打印 `operation(s)` 得到的 `ID/Status` 再修正匹配）

- [ ] **Step 3: 全包回归与 vet**

Run: `go test -race -count=1 ./multiagent/... && go vet ./multiagent/... && gofumpt -l multiagent && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent`
Expected: 测试 PASS，其余无输出

- [ ] **Step 4: 提交**

```bash
git add multiagent/durable_test.go
git commit -m "test(multiagent): 覆盖持久恢复、故障窗口、预算与版本迁移"
```


### Task 12: 场景测试 S1–S4、S6–S9 与组合流程

**Files:**
- Test: `multiagent/scenario_test.go`

**Interfaces:**
- Consumes: Task 1–9 的全部导出 API；Task 9 的 `memoryBudget`、`runDelegation`、`runResumableDelegation`；Task 4 的 `env.renew`
- Produces: 测试辅助 `modes`、`execute[T](t, g, durableMode bool, init T) (T, *workflow.GraphSnapshot[T], error)`、`textAgent(t, name, *mocks.ScriptedClient, ...TextOption) Step[Conversation]`、`agents(Conversation) []string`、`parseVerdict(Conversation) (bool, error)`、`untilPassed`、`transferToHuman(*[]Conversation) *Step[Conversation]`、`caseFile`、`reviewRounds`、`reviewExit`、`composedGraph`、`validateOrderID`、`askCustomerStep`（Task 14 复用 `textAgent`）。

- [ ] **Step 1: 写测试**

每个场景一个端到端测试，用真实 Builder 与 `ScriptedClient`，只验证业务链路：

| # | 测试 | 编译方式 |
|---|---|---|
| S1 | `TestScenarioTicketRouting`：依次选中退款、物流后 FINISH；每次 Supervisor 请求含原始工单；分流共享一个脚本实例 | 普通与持久 |
| S2 | `TestScenarioReplyReview`：第一轮不通过、第二轮通过；写作者第二轮请求含上一稿与评审意见 | 普通与持久 |
| S3 | `TestScenarioParallelLookup`：三个分支各用独立脚本，按登记顺序合并，回答 Agent 收到合并结果，整组计 1 步 | 普通与持久 |
| S4 | `TestScenarioPipeline`：订单号写入状态后两步读到；合规不通过由 `out` 报错且带节点名 | 普通与持久 |
| S6 | `TestScenarioAskCustomer`：无效订单号 `Resume` 返回 `*workflow.ResumeRejectedError` 且中断 `Rejected`；有效值续跑完成；两次续跑抽取都不重新请求模型；补充后另一个 scope；截止时间已过以 `context.DeadlineExceeded` 失败、覆盖等待窗口时完成 | 仅持久 |
| S7 | `TestScenarioDelegation`：两个结构化工具调用后汇总，每次新建会话并 Close；Resumable 子测试 | 普通；Resumable 为持久单节点图 |
| S8 | `TestScenarioEscalation`：Loop 与 Router 超限返回 `ErrMaxSteps`；持久模式同一运行 ID 再 `Run` 仍报错，换运行 ID 与新根预算后可处理 | 普通与持久 |
| S9 | `TestScenarioInvalidDecision`：分流首行“退款（原因…）”转兜底，兜底收到该输出；评审结论无法解析时 Loop 同样转兜底 | 普通与持久 |
| 组合 | `TestScenarioComposedPipeline`：手写图 抽取 → 并行查询 → 写作⇄评审 → 合规，`AddStep` 加节点、`GuardRoute` 包装出口路由；N 轮通过时步数恰好够用；始终不通过返回 `ErrMaxSteps`，写作与评审 `Calls()` 为 N、合规为 0，持久模式快照停在评审节点、`Routing` 为真、步数 `2+2N`，再 `Run` 仍报错且不增加请求；评审结论无法解析时转兜底 | 普通与持久 |

S5 在 Task 14 追加到同一文件。

`multiagent/scenario_test.go`：

```go
package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/workflow"
)

var modes = []struct {
	name    string
	durable bool
}{{"普通", false}, {"持久", true}}

// execute 用普通或持久方式执行图，返回最终状态。
func execute[T any](t *testing.T, g *workflow.Graph[T], durableMode bool, init T) (T, *workflow.GraphSnapshot[T], error) {
	t.Helper()
	if !durableMode {
		run, err := g.Compile(context.Background())
		require.NoError(t, err)
		out, err := run.Invoke(memoryBudget(t), init)
		return out, nil, err
	}
	e := newEnv(t, "scenario")
	snap, err := compile(t, e, g, "v1").Run(e.ctx(), e.id, init, "")
	var out T
	if snap != nil {
		out = snap.State
	}
	return out, snap, err
}

func textAgent(t *testing.T, name string, client *mocks.ScriptedClient, opts ...TextOption) Step[Conversation] {
	t.Helper()
	var s sessions
	step, err := TextStep(name, s.scripted(client, name, nil), opts...)
	require.NoError(t, err)
	return step
}

func agents(c Conversation) []string {
	out := make([]string, len(c.Turns))
	for i, turn := range c.Turns {
		out[i] = turn.Agent
	}
	return out
}

// parseVerdict 读取最后一轮评审结论：首行 PASS 为通过，以 REJECT 开头为不通过，其余无法解析。
func parseVerdict(c Conversation) (bool, error) {
	for i := len(c.Turns) - 1; i >= 0; i-- {
		if c.Turns[i].Agent != "review" {
			continue
		}
		line, _, _ := strings.Cut(c.Turns[i].Output, "\n")
		switch line = strings.TrimSpace(line); {
		case line == "PASS":
			return true, nil
		case strings.HasPrefix(line, "REJECT"):
			return false, nil
		}
		return false, fmt.Errorf("%w：评审结论 %q 无法解析", ErrInvalidRoute, line)
	}
	return false, fmt.Errorf("%w：没有评审结论", ErrInvalidRoute)
}

func untilPassed(_ context.Context, c Conversation) (bool, error) { return parseVerdict(c) }

// transferToHuman 是转人工兜底：记录收到的状态并追加交接结果。
func transferToHuman(received *[]Conversation) *Step[Conversation] {
	return &Step[Conversation]{Name: "human", Run: core.NewRunnableFunc(func(_ context.Context, c Conversation) (Conversation, error) {
		*received = append(*received, CloneConversation(c))
		c.Turns = append(c.Turns, Turn{Agent: "human", Output: "已转人工"})
		return c, nil
	})}
}

func TestScenarioTicketRouting(t *testing.T) {
	const ticketText = "工单：重复扣款，且包裹三天未更新"
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			triage := mocks.NewScriptedClient(
				mocks.ScriptedRule{Name: "选退款", Match: mocks.RequestContains(ticketText), Reply: mocks.ScriptedReply{Text: "refund"}},
				mocks.ScriptedRule{Name: "选物流", Match: mocks.RequestContains(ticketText, "[refund]: 已登记退款"), Reply: mocks.ScriptedReply{Text: "logistics"}},
				mocks.ScriptedRule{Name: "结束", Match: mocks.RequestContains(ticketText, "[logistics]: 已催单"), Reply: mocks.ScriptedReply{Text: "FINISH"}},
			)
			refund := textAgent(t, "refund", mocks.NewScriptedClient(mocks.TextReply("退款", "已登记退款")))
			logistics := textAgent(t, "logistics", mocks.NewScriptedClient(mocks.TextReply("物流", "已催单")))
			next, err := TextNext(refund, logistics)
			require.NoError(t, err)
			g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{
				Supervisor: textAgent(t, "triage", triage),
				Members:    []Step[Conversation]{refund, logistics}, Next: next, MaxRounds: 4,
			})
			require.NoError(t, err)
			out, _, err := execute(t, g, mode.durable, Conversation{Input: ticketText})
			require.NoError(t, err)
			require.Equal(t, []string{"triage", "refund", "triage", "logistics", "triage"}, agents(out))
			require.Equal(t, 3, triage.Calls(), "分流角色共享一个脚本实例")
			for _, req := range triage.Requests() {
				require.True(t, mocks.RequestContains(ticketText)(&req), "每次 Supervisor 请求都包含原始工单")
			}
		})
	}
}

func TestScenarioReplyReview(t *testing.T) {
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			writer := mocks.NewScriptedClient(
				mocks.TextReply("初稿", "您好，退款已处理。"),
				mocks.ScriptedRule{Name: "按意见重写", Match: mocks.RequestContains("[write]: 您好，退款已处理。", "[review]: REJECT 缺少到账时间"), Reply: mocks.ScriptedReply{Text: "您好，退款已处理，预计 3 个工作日到账。"}},
			)
			reviewer := mocks.NewScriptedClient(mocks.TextReply("不通过", "REJECT 缺少到账时间"), mocks.TextReply("通过", "PASS"))
			g, err := Loop(workflow.Config{}, LoopConfig[Conversation]{
				Steps: []Step[Conversation]{textAgent(t, "write", writer), textAgent(t, "review", reviewer)},
				Until: untilPassed, MaxIterations: 3,
			})
			require.NoError(t, err)
			out, _, err := execute(t, g, mode.durable, Conversation{Input: "客户询问退款进度"})
			require.NoError(t, err)
			require.Equal(t, []string{"write", "review", "write", "review"}, agents(out))
			require.Equal(t, "PASS", out.Turns[3].Output)
			require.Zero(t, writer.Remaining(), "第二轮请求包含上一稿与评审意见")
		})
	}
}

func TestScenarioParallelLookup(t *testing.T) {
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			lookup, err := ParallelStep("lookup", ParallelConfig[Conversation]{Steps: []Step[Conversation]{
				textAgent(t, "kb", mocks.NewScriptedClient(mocks.TextReply("知识库", "七天无理由"))),
				textAgent(t, "order", mocks.NewScriptedClient(mocks.TextReply("订单", "A1 已签收"))),
				textAgent(t, "shipping", mocks.NewScriptedClient(mocks.TextReply("物流", "签收于 11-01"))),
			}, Clone: CloneConversation, Merge: MergeTurns})
			require.NoError(t, err)
			answer := mocks.NewScriptedClient(mocks.ScriptedRule{
				Name:  "汇总",
				Match: mocks.RequestContains("[kb]: 七天无理由", "[order]: A1 已签收", "[shipping]: 签收于 11-01"),
				Reply: mocks.ScriptedReply{Text: "可在 11-08 前退货"},
			})
			g, err := Sequential(workflow.Config{}, lookup, textAgent(t, "answer", answer))
			require.NoError(t, err)
			out, snap, err := execute(t, g, mode.durable, Conversation{Input: "A1 能退货吗"})
			require.NoError(t, err)
			require.Equal(t, []string{"kb", "order", "shipping", "answer"}, agents(out), "结果按登记顺序合并")
			if snap != nil {
				require.Equal(t, 2, snap.Steps, "整组并行计 1 步")
			}
		})
	}
}

type caseFile struct {
	Ticket  string
	OrderID string
	Answer  string
}

func TestScenarioPipeline(t *testing.T) {
	build := func(t *testing.T, verdict string) *workflow.Graph[caseFile] {
		var s sessions
		extract, err := AgentStep("extract", s.scripted(mocks.NewScriptedClient(mocks.TextReply("抽取", `{"order_id":"A1","type":"refund"}`)), "extract", nil),
			func(c caseFile) (any, error) { return c.Ticket, nil },
			func(c caseFile, o *builder.AgentOutput) (caseFile, error) {
				var fields struct {
					OrderID string `json:"order_id"`
				}
				if err := json.Unmarshal([]byte(o.Result.(string)), &fields); err != nil {
					return c, err
				}
				c.OrderID = fields.OrderID
				return c, nil
			})
		require.NoError(t, err)
		answer, err := AgentStep("answer", s.scripted(mocks.NewScriptedClient(mocks.ScriptedRule{Name: "回答", Match: mocks.RequestContains("订单 A1"), Reply: mocks.ScriptedReply{Text: "A1 已退款"}}), "answer", nil),
			func(c caseFile) (any, error) { return "订单 " + c.OrderID + "：" + c.Ticket, nil },
			func(c caseFile, o *builder.AgentOutput) (caseFile, error) {
				c.Answer = o.Result.(string)
				return c, nil
			})
		require.NoError(t, err)
		compliance, err := AgentStep("compliance", s.scripted(mocks.NewScriptedClient(mocks.ScriptedRule{Name: "合规", Match: mocks.RequestContains("A1", "A1 已退款"), Reply: mocks.ScriptedReply{Text: verdict}}), "compliance", nil),
			func(c caseFile) (any, error) { return c.OrderID + "\n" + c.Answer, nil },
			func(c caseFile, o *builder.AgentOutput) (caseFile, error) {
				if o.Result != "PASS" {
					return c, fmt.Errorf("合规检查不通过：%v", o.Result)
				}
				return c, nil
			})
		require.NoError(t, err)
		g, err := Sequential(workflow.Config{}, extract, answer, compliance)
		require.NoError(t, err)
		return g
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			out, _, err := execute(t, build(t, "PASS"), mode.durable, caseFile{Ticket: "我要退款"})
			require.NoError(t, err)
			require.Equal(t, caseFile{Ticket: "我要退款", OrderID: "A1", Answer: "A1 已退款"}, out)
			_, _, err = execute(t, build(t, "含承诺性措辞"), mode.durable, caseFile{Ticket: "我要退款"})
			require.ErrorContains(t, err, `node "compliance"`)
			require.ErrorContains(t, err, "合规检查不通过")
		})
	}
}

func TestScenarioDelegation(t *testing.T) {
	t.Run("普通成员", func(t *testing.T) {
		members := runDelegation(t)
		require.EqualValues(t, 2, members.closed.Load())
	})
	t.Run("Resumable 成员", runResumableDelegation)
}

func TestScenarioEscalation(t *testing.T) {
	loop := func(t *testing.T, reviewer *mocks.ScriptedClient, writer *mocks.ScriptedClient) *workflow.Graph[Conversation] {
		g, err := Loop(workflow.Config{}, LoopConfig[Conversation]{
			Steps: []Step[Conversation]{textAgent(t, "write", writer), textAgent(t, "review", reviewer)},
			Until: untilPassed, MaxIterations: 2,
		})
		require.NoError(t, err)
		return g
	}
	always := func(text string, n int) *mocks.ScriptedClient {
		rules := make([]mocks.ScriptedRule, n)
		for i := range rules {
			rules[i] = mocks.TextReply(fmt.Sprintf("第 %d 次", i+1), text)
		}
		return mocks.NewScriptedClient(rules...)
	}
	t.Run("Loop 普通", func(t *testing.T) {
		_, _, err := execute(t, loop(t, always("REJECT 仍不合格", 2), always("稿", 2)), false, Conversation{Input: "q"})
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
	})
	t.Run("Router 普通", func(t *testing.T) {
		refund := textAgent(t, "refund", always("处理中", 2))
		next, err := TextNext(refund)
		require.NoError(t, err)
		triage := always("refund", 3)
		g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{Supervisor: textAgent(t, "triage", triage), Members: []Step[Conversation]{refund}, Next: next, MaxRounds: 2})
		require.NoError(t, err)
		_, _, err = execute(t, g, false, Conversation{Input: "q"})
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
		require.Equal(t, 3, triage.Calls(), "第 MaxRounds 次成员后 Supervisor 再请求一次")
	})
	t.Run("持久转人工", func(t *testing.T) {
		e := newEnv(t, "ticket-1")
		reviewer, writer := always("REJECT 仍不合格", 2), always("稿", 2)
		d := compile(t, e, loop(t, reviewer, writer), "v1")
		_, err := d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
		_, err = d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, workflow.ErrMaxSteps, "同一运行 ID 再 Run 仍报错")
		require.Equal(t, 2, reviewer.Calls())

		e.renew("ticket-1-human")
		snap, err := compile(t, e, loop(t, always("PASS", 1), always("人工改写稿", 1)), "v1").Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.NoError(t, err, "换运行 ID 与新根预算后可重新处理")
		require.Equal(t, "PASS", snap.State.Turns[1].Output)
	})
}

func TestScenarioInvalidDecision(t *testing.T) {
	for _, mode := range modes {
		t.Run("Router/"+mode.name, func(t *testing.T) {
			var received []Conversation
			refund := textAgent(t, "refund", mocks.NewScriptedClient())
			next, err := TextNext(refund)
			require.NoError(t, err)
			g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{
				Supervisor: textAgent(t, "triage", mocks.NewScriptedClient(mocks.TextReply("无效", "退款（原因：重复扣款）"))),
				Members:    []Step[Conversation]{refund}, Next: next, MaxRounds: 3, Fallback: transferToHuman(&received),
			})
			require.NoError(t, err)
			out, _, err := execute(t, g, mode.durable, Conversation{Input: "重复扣款"})
			require.NoError(t, err)
			require.Equal(t, []string{"triage", "human"}, agents(out))
			require.Len(t, received, 1)
			require.Equal(t, "退款（原因：重复扣款）", received[0].Turns[0].Output)
		})
		t.Run("Loop/"+mode.name, func(t *testing.T) {
			var received []Conversation
			g, err := Loop(workflow.Config{}, LoopConfig[Conversation]{Steps: []Step[Conversation]{
				textAgent(t, "write", mocks.NewScriptedClient(mocks.TextReply("稿", "回复稿"))),
				textAgent(t, "review", mocks.NewScriptedClient(mocks.TextReply("无法解析", "看起来还行吧"))),
			}, Until: untilPassed, MaxIterations: 3, Fallback: transferToHuman(&received)})
			require.NoError(t, err)
			out, _, err := execute(t, g, mode.durable, Conversation{Input: "q"})
			require.NoError(t, err)
			require.Equal(t, []string{"write", "review", "human"}, agents(out))
			require.Equal(t, "看起来还行吧", received[0].Turns[1].Output)
		})
	}
}

const reviewRounds = 2 // MaxSteps = 3 + 2*reviewRounds：抽取、并行查询、合规各 1 步

// reviewExit 是组合流程中评审后的出口路由：先看解析错误，再看是否通过，最后从状态数出轮数。
func reviewExit(_ context.Context, c Conversation) (string, error) {
	passed, err := parseVerdict(c)
	if err != nil {
		return "", err
	}
	if passed {
		return "compliance", nil
	}
	rounds := 0
	for _, turn := range c.Turns {
		if turn.Agent == "review" {
			rounds++
		}
	}
	if rounds >= reviewRounds {
		return "", fmt.Errorf("%w: review loop limit %d", workflow.ErrMaxSteps, reviewRounds)
	}
	return "write", nil
}

type composedScripts struct{ writer, reviewer, compliance *mocks.ScriptedClient }

func composedGraph(t *testing.T, sc composedScripts, received *[]Conversation) *workflow.Graph[Conversation] {
	t.Helper()
	g := workflow.NewGraph[Conversation](workflow.Config{MaxSteps: 3 + 2*reviewRounds})
	lookup, err := ParallelStep("lookup", ParallelConfig[Conversation]{Steps: []Step[Conversation]{
		textAgent(t, "kb", mocks.NewScriptedClient(mocks.TextReply("知识库", "七天无理由"))),
		textAgent(t, "order", mocks.NewScriptedClient(mocks.TextReply("订单", "A1 已签收"))),
	}, Clone: CloneConversation, Merge: MergeTurns})
	require.NoError(t, err)
	for _, s := range []Step[Conversation]{
		textAgent(t, "extract", mocks.NewScriptedClient(mocks.TextReply("抽取", "A1"))), lookup,
		textAgent(t, "write", sc.writer), textAgent(t, "review", sc.reviewer), textAgent(t, "compliance", sc.compliance), *transferToHuman(received),
	} {
		require.NoError(t, AddStep(g, s))
	}
	for _, edge := range [][2]string{{workflow.START, "extract"}, {"extract", "lookup"}, {"lookup", "write"}, {"write", "review"}, {"compliance", workflow.END}, {"human", workflow.END}} {
		require.NoError(t, g.AddEdge(edge[0], edge[1]))
	}
	route, targets, err := GuardRoute(reviewExit, []string{"write", "compliance"}, "human")
	require.NoError(t, err)
	require.NoError(t, g.AddBranch("review", route, targets...))
	return g
}

func TestScenarioComposedPipeline(t *testing.T) {
	script := func(texts ...string) *mocks.ScriptedClient {
		rules := make([]mocks.ScriptedRule, len(texts))
		for i, text := range texts {
			rules[i] = mocks.TextReply(fmt.Sprintf("第 %d 次", i+1), text)
		}
		return mocks.NewScriptedClient(rules...)
	}
	for _, mode := range modes {
		t.Run("第 N 轮通过/"+mode.name, func(t *testing.T) {
			sc := composedScripts{script("稿一", "稿二"), script("REJECT 太长", "PASS"), script("合规")}
			out, snap, err := execute(t, composedGraph(t, sc, nil), mode.durable, Conversation{Input: "A1 能退货吗"})
			require.NoError(t, err, "MaxSteps 恰好够用")
			require.Equal(t, []string{"extract", "kb", "order", "write", "review", "write", "review", "compliance"}, agents(out))
			if snap != nil {
				require.Equal(t, 3+2*reviewRounds, snap.Steps)
			}
		})
		t.Run("评审结论无法解析/"+mode.name, func(t *testing.T) {
			var received []Conversation
			sc := composedScripts{script("稿一"), script("嗯"), script()}
			out, _, err := execute(t, composedGraph(t, sc, &received), mode.durable, Conversation{Input: "q"})
			require.NoError(t, err)
			require.Equal(t, "human", out.Turns[len(out.Turns)-1].Agent)
			require.Len(t, received, 1)
		})
	}
	t.Run("始终不通过/普通", func(t *testing.T) {
		sc := composedScripts{script("稿一", "稿二"), script("REJECT", "REJECT"), script()}
		_, _, err := execute(t, composedGraph(t, sc, nil), false, Conversation{Input: "q"})
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
		require.Equal(t, reviewRounds, sc.writer.Calls())
		require.Equal(t, reviewRounds, sc.reviewer.Calls())
		require.Zero(t, sc.compliance.Calls())
	})
	t.Run("始终不通过/持久", func(t *testing.T) {
		sc := composedScripts{script("稿一", "稿二"), script("REJECT", "REJECT"), script()}
		e := newEnv(t, "composed")
		d := compile(t, e, composedGraph(t, sc, nil), "v1")
		_, err := d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, workflow.ErrMaxSteps)
		snap, err := d.Snapshot(context.Background(), e.id)
		require.NoError(t, err)
		require.Equal(t, "review", snap.Node)
		require.True(t, snap.Routing)
		require.Equal(t, 2+2*reviewRounds, snap.Steps)
		_, err = d.Run(e.ctx(), e.id, Conversation{Input: "q"}, "")
		require.ErrorIs(t, err, workflow.ErrMaxSteps, "同一运行 ID 再 Run 重新路由，得到同样的错误")
		require.Equal(t, reviewRounds, sc.writer.Calls())
		require.Equal(t, reviewRounds, sc.reviewer.Calls())
		require.Zero(t, sc.compliance.Calls())
	})
}

var orderIDPattern = regexp.MustCompile(`^A\d+$`)

// validateOrderID 是 AwaitInput 的纯函数校验：格式不对时保持等待，宿主用同一中断键提交修正值。
func validateOrderID(raw json.RawMessage) error {
	var id string
	if err := json.Unmarshal(raw, &id); err != nil || !orderIDPattern.MatchString(id) {
		return errors.New("订单号格式应为 A 加数字")
	}
	return nil
}

// askCustomerStep 抽取订单号，缺失时暂停向客户追问；补充后用另一个 scope 再次抽取。
func askCustomerStep(t *testing.T, client *mocks.ScriptedClient) Step[caseFile] {
	t.Helper()
	var s sessions
	invoke := func(ctx context.Context, scope, input string) (string, error) {
		owned, err := s.scripted(client, scope, nil)(ctx)
		if err != nil {
			return "", err
		}
		defer func() { _ = owned.Close(context.WithoutCancel(ctx)) }()
		out, err := owned.Runnable.Invoke(ctx, input)
		if err != nil {
			return "", err
		}
		return out.Result.(string), nil
	}
	return Step[caseFile]{Name: "extract", Run: core.NewRunnableFunc(func(ctx context.Context, c caseFile) (caseFile, error) {
		text, err := invoke(ctx, "extract", c.Ticket)
		if err != nil {
			return c, err
		}
		var fields struct {
			OrderID string `json:"order_id"`
		}
		if err := json.Unmarshal([]byte(text), &fields); err != nil {
			return c, err
		}
		if fields.OrderID == "" {
			raw, err := durable.AwaitInput(ctx, "order-id", json.RawMessage(`{"question":"请提供订单号"}`), validateOrderID)
			if err != nil {
				return c, err
			}
			_ = json.Unmarshal(raw, &fields.OrderID)
			// 同一节点内第二次调用 Agent 必须换 scope，否则与第一次调用的意图冲突。
			if c.Answer, err = invoke(ctx, "extract-confirm", "确认订单 "+fields.OrderID+"："+c.Ticket); err != nil {
				return c, err
			}
		}
		c.OrderID = fields.OrderID
		return c, nil
	})}
}

func TestScenarioAskCustomer(t *testing.T) {
	scripts := func() *mocks.ScriptedClient {
		return mocks.NewScriptedClient(mocks.TextReply("抽取", `{"order_id":""}`),
			mocks.ScriptedRule{Name: "确认", Match: mocks.RequestContains("A123"), Reply: mocks.ScriptedReply{Text: "订单 A123 已确认"}})
	}
	graph := func(t *testing.T, e *env, client *mocks.ScriptedClient) *workflow.DurableGraph[caseFile] {
		g, err := Sequential(workflow.Config{}, askCustomerStep(t, client))
		require.NoError(t, err)
		return compile(t, e, g, "v1")
	}
	t.Run("追问与格式校验", func(t *testing.T) {
		e := newEnv(t, "ask-customer")
		client := scripts()
		snap, err := graph(t, e, client).Run(e.ctx(), e.id, caseFile{Ticket: "我要退款"}, "")
		require.ErrorIs(t, err, durable.ErrApprovalRequired)
		require.Len(t, snap.Interrupts, 1)
		key := snap.Interrupts[0].Key

		e.restart()
		d := graph(t, e, client)
		_, err = d.Resume(e.ctx(), e.id, workflow.ResumeRequest{Key: key, Input: json.RawMessage(`"123"`)})
		var rejected *workflow.ResumeRejectedError
		require.ErrorAs(t, err, &rejected)
		snap, err = d.Snapshot(context.Background(), e.id)
		require.NoError(t, err)
		require.True(t, snap.Waiting)
		require.True(t, snap.Interrupts[0].Rejected)
		require.Equal(t, 1, client.Calls(), "续跑时抽取 Agent 从日志回放")

		snap, err = d.Resume(e.ctx(), e.id, workflow.ResumeRequest{Key: key, Input: json.RawMessage(`"A123"`)})
		require.NoError(t, err)
		require.Equal(t, "A123", snap.State.OrderID)
		require.Equal(t, "订单 A123 已确认", snap.State.Answer)
		require.Equal(t, 2, client.Calls(), "只多出补充后的那次抽取，它使用另一个 scope")
	})
	for name, window := range map[string]time.Duration{"截止时间已过": 300 * time.Millisecond, "截止时间覆盖等待": time.Hour} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, "ask-deadline")
			e.budgetDir = t.TempDir() // 截止时间属于根预算配置，运行开始前确定，之后不可改
			e.budget.DeadlineUnixMilli = time.Now().Add(window).UnixMilli()
			e.restart()
			client := scripts()
			snap, err := graph(t, e, client).Run(e.ctx(), e.id, caseFile{Ticket: "我要退款"}, "")
			require.ErrorIs(t, err, durable.ErrApprovalRequired)
			if window < time.Second {
				time.Sleep(window) // 客户隔了一段时间才回复
			}
			e.restart()
			_, err = graph(t, e, client).Resume(e.ctx(), e.id, workflow.ResumeRequest{Key: snap.Interrupts[0].Key, Input: json.RawMessage(`"A123"`)})
			if window < time.Second {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				return
			}
			require.NoError(t, err)
		})
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `go test -race -count=1 -run '^(TestScenarioTicketRouting|TestScenarioReplyReview|TestScenarioParallelLookup|TestScenarioPipeline|TestScenarioDelegation|TestScenarioEscalation|TestScenarioInvalidDecision|TestScenarioComposedPipeline|TestScenarioAskCustomer)$' ./multiagent/`
Expected: PASS（S6 的“截止时间已过”子测试会等待约 300ms）

- [ ] **Step 3: 全包回归与 vet**

Run: `go test -race -count=1 ./multiagent/... && go vet ./multiagent/... && gofumpt -l multiagent && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent`
Expected: 测试 PASS，其余无输出

- [ ] **Step 4: 提交**

```bash
git add multiagent/scenario_test.go
git commit -m "test(multiagent): 覆盖客服场景 S1–S4、S6–S9 与组合流程"
```


### Task 13: refund.Service 登记钩子 OnRegister

**Files:**
- Modify: `workflow/refund/service.go:80-85`（`Service` 结构体）与 `:153-182`（`Register`）
- Modify: `workflow/refund/service_test.go`（末尾追加测试）
- Modify: `workflow/refund/README.md`
- Modify: `CHANGELOG.md`（`[Unreleased]` 下新建 Added）

**Interfaces:**
- Consumes: 现有 `refund.Service`、`refund.Transaction`、`refund.Request`；`service_test.go` 已有的 `memoryStore`（回调出错不提交）与 `memoryTx`
- Produces: `Service.OnRegister func(ctx context.Context, tx Transaction, req Request) error`：仅在 `Register` 新插入申请时、同一事务内、`InsertRequest` 之后调用；返回错误则事务回滚、`Register` 返回该错误；同一 ID 已存在时不调用；nil 时行为不变。Task 14 用它写入争议判定与登记序号。

- [ ] **Step 1: 写失败测试**

测试：新插入调用一次且钩子内能读到刚插入的申请、重复登记不调用、钩子出错回滚且 `Existing` 查不到申请、nil 钩子行为不变。在 `workflow/refund/service_test.go` 末尾追加：

```go
func hookFixture(hook func(context.Context, refund.Transaction, refund.Request) error) (*memoryStore, *refund.Service, refund.Request) {
	now := time.Now().UTC()
	db := &memoryStore{tx: memoryTx{order: refund.Order{PaidCents: 1000, Currency: "CNY", Version: 1, DeliveryDays: 2}}}
	svc := &refund.Service{
		Store: db, Now: func() time.Time { return now }, OnRegister: hook,
		Authorizer: refund.AuthorizeFunc(func(context.Context, string, refund.Request) error { return nil }),
		Policy:     func(context.Context, refund.Request, refund.Order) error { return nil },
	}
	req := refund.Request{RequestID: "tenant-A/refund-1", OrderID: "tenant-A/order-1", AmountCents: 500, PaidCents: 1000, Currency: "CNY", OrderVersion: 1, DeliveryDays: 2, ValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano)}
	return db, svc, req
}

func TestServiceOnRegisterRunsOnceInsideTransaction(t *testing.T) {
	ctx := context.Background()
	var seen []refund.Request
	db, svc, req := hookFixture(func(ctx context.Context, tx refund.Transaction, r refund.Request) error {
		stored, _, err := tx.Request(ctx, r.RequestID)
		require.NoError(t, err, "钩子在 InsertRequest 之后、同一事务内调用")
		require.Equal(t, r, stored)
		seen = append(seen, r)
		return nil
	})
	_, err := svc.Register(ctx, req)
	require.NoError(t, err)
	_, err = svc.Register(ctx, req)
	require.NoError(t, err)
	require.Equal(t, []refund.Request{req}, seen, "新插入调用一次，重复登记不调用")
	require.Equal(t, req, db.tx.req)
}

func TestServiceOnRegisterErrorRollsBack(t *testing.T) {
	ctx := context.Background()
	hookErr := errors.New("判定写入失败")
	db, svc, req := hookFixture(func(context.Context, refund.Transaction, refund.Request) error { return hookErr })
	_, err := svc.Register(ctx, req)
	require.ErrorIs(t, err, hookErr)
	require.Empty(t, db.tx.req.RequestID, "钩子出错时整个事务回滚，没有申请记录")
	_, found, err := svc.Existing(ctx, req)
	require.NoError(t, err)
	require.False(t, found)
}

func TestServiceWithoutOnRegisterUnchanged(t *testing.T) {
	db, svc, req := hookFixture(nil)
	registered, err := svc.Register(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, req, registered)
	require.Equal(t, req, db.tx.req)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 -run '^(TestServiceOnRegisterRunsOnceInsideTransaction|TestServiceOnRegisterErrorRollsBack|TestServiceWithoutOnRegisterUnchanged)$' ./workflow/refund/`
Expected: FAIL，编译错误 `unknown field OnRegister in struct literal of type refund.Service`

- [ ] **Step 3: 写实现**

在 `workflow/refund/service.go` 中把

```go
	Now        func() time.Time // nil uses time.Now; do not mutate configuration during use.
}
```

替换为

```go
	Now        func() time.Time // nil uses time.Now; do not mutate configuration during use.
	// OnRegister 可选：Register 新插入申请后、在同一事务内调用，用于写入与申请同生共灭的业务记录（如争议判定）。
	// 返回错误时整个事务回滚、Register 返回该错误；同一申请 ID 重复登记时不调用。
	OnRegister func(ctx context.Context, tx Transaction, req Request) error
}
```

再把 `Register` 中的

```go
		if e = tx.InsertRequest(ctx, req); e != nil {
			return e
		}
		out = req
```

替换为

```go
		if e = tx.InsertRequest(ctx, req); e != nil {
			return e
		}
		if s.OnRegister != nil {
			if e = s.OnRegister(ctx, tx, req); e != nil {
				return e
			}
		}
		out = req
```

- [ ] **Step 4: 同步 README 与 CHANGELOG**

`workflow/refund/README.md`：把

````markdown
- `Approve`：
````

替换为

````markdown
- `OnRegister`（可选）：`Register` 新插入申请后在同一事务内调用，用于写入与申请同生共灭的业务记录（如争议判定、登记序号）；返回错误时整个事务回滚、`Register` 返回该错误，同一申请 ID 重复登记时不调用，为 nil 时行为不变。
- `Approve`：
````

`CHANGELOG.md`：把

````markdown
## [Unreleased]
````

替换为

````markdown
## [Unreleased]

### Added

- `workflow/refund.Service` 新增可选 `OnRegister` 钩子：`Register` 新插入申请后在同一事务内调用，可写入争议判定等与申请同生共灭的记录；返回错误时整个事务回滚，重复登记不调用，为 nil 时行为不变。
````

- [ ] **Step 5: 运行测试确认通过**

Run: `go test -race -count=1 ./workflow/refund/... && go vet ./workflow/refund/... && gofmt -l workflow/refund`
Expected: 测试 PASS，其余无输出。这两个文件原本就有 gofumpt/gci 差异，只用 gofmt 检查，不要 gofumpt -w 或 gci write，以免改动无关行

- [ ] **Step 6: 提交**

```bash
git add workflow/refund/service.go workflow/refund/service_test.go workflow/refund/README.md CHANGELOG.md
git commit -m "feat(refund): Service 新增在登记事务内调用的 OnRegister 钩子"
```


### Task 14: S5 高风险操作审批（登记后审核）

**Files:**
- Create: `multiagent/refund_fixture_test.go`（业务侧替身：退款账本与退款成员）
- Modify: `multiagent/scenario_test.go`（末尾追加 `TestScenarioRefundApproval`）

**Interfaces:**
- Consumes: Task 13 的 `refund.Service.OnRegister`；`workflow/refund.Service`、`refund.Store`、`refund.Transaction`、`refund.Request`、`refund.Order`、`refund.ErrNotFound`；`durable.Do`、`durable.OperationPolicy.Reconcile`、`durable.AwaitInput`、`workflow.DurableKey`；Task 12 的 `textAgent`；Task 11 的 `operationFails`；Task 8 的 `renderTurns`、`TextNext`；Task 7 的 `RouterSupervisor`
- Produces: 业务替身：`verdict`、`proposal`、`registration`、`execution`、`reviewInput{Approved bool; Note, Approver, Reason string}`、`validateReview(json.RawMessage) error`、`ledger`（实现 `refund.Store`，`changeOrder()`、`refunds() int`）、`refundDesk`（`register`、`reconcileRegister`、`execute`、`reconcileExecute`，计数 `registers`、`executions`）、`refundMember(*refundDesk, AgentFactory) Step[Conversation]`；场景辅助 `refundCase`、`newRefundCase`、`(*refundCase).graph/start/answer`、`lastOutput`。

- [ ] **Step 1: 写业务替身**

按规格“审批与中断”的 S5 条目（含 HEAD `bbfef186` 的修订）实现退款成员：

1. 调用退款 Agent（scope `refund`，无副作用工具）得到结构化提议。
2. `durable.Do(ctx, "refund/register", …)`：意图只含申请 ID 与提议，不含规则阈值或时间；run 按规则计算判定（金额超过阈值即有争议，Agent 只能升级），用带 `OnRegister` 钩子的 `refund.Service` 副本登记，钩子在同一事务内写入判定与登记序号；`Reconcile` 只有申请与判定都存在时才补记完成。
3. 有争议时 `durable.AwaitInput(ctx, "refund-review", info, validateReview)`：info 含订单号、金额、判定理由与提交人；输入 `{"approved", "note", "approver"}`（另有可选 `reason`，见偏差 7），批准时 `approver` 必须非空；拒绝写回“人工拒绝：…”，超时写回“审核超时，按拒绝处理”。
4. `durable.Do(ctx, "refund/execute", …)`：run 先按申请 ID 查回执，已有回执直接返回；订单事实已变化时返回业务结果“订单已变化，需重新申请”（不是节点错误）；否则 `Approve` 后 `Refund`。`Reconcile` 按回执补记。

`multiagent/refund_fixture_test.go`：

```go
package multiagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/workflow"
	"github.com/kart-io/goagent/workflow/refund"
)

// 本文件是 S5 的业务侧替身：退款账本实现 refund.Store，退款成员是自定义 Step.Run。
// 它们演示业务如何接入，不属于 multiagent 的 API。

// verdict 是按业务规则计算并在登记时固定的争议判定。
type verdict struct {
	Disputed bool   `json:"disputed"`
	Reason   string `json:"reason"`
	Seq      int    `json:"seq"` // 登记序号，由 OnRegister 钩子在登记事务内分配
}

// proposal 是退款 Agent 给出的结构化提议；Agent 只能把无争议升级为有争议。
type proposal struct {
	OrderID     string `json:"order_id"`
	AmountCents int64  `json:"amount_cents"`
	Disputed    bool   `json:"disputed"`
	Reason      string `json:"reason,omitempty"`
}

type registration struct {
	Request refund.Request `json:"request"`
	Verdict verdict        `json:"verdict"`
}

type execution struct {
	Receipt      string `json:"receipt,omitempty"`
	OrderChanged bool   `json:"order_changed,omitempty"`
}

// reviewInput 是审核回应；approver 由宿主认证后填入，reason 为 timeout 表示等待超时按拒绝处理。
type reviewInput struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note"`
	Approver string `json:"approver"`
	Reason   string `json:"reason,omitempty"`
}

// validateReview 是纯函数：格式错误或批准却没有审核人时保持等待。
func validateReview(raw json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var in reviewInput
	if err := dec.Decode(&in); err != nil {
		return errors.New("审核输入格式应为 {approved, note, approver}")
	}
	if in.Approved && in.Approver == "" {
		return errors.New("批准时必须提供审核人")
	}
	return nil
}

type ledgerState struct {
	order    refund.Order
	requests map[string]refund.Request
	actors   map[string]string
	receipts map[string]string
	verdicts map[string]verdict
	seq      int
	total    int64
	refunds  int
}

func (s ledgerState) clone() ledgerState {
	s.requests, s.actors, s.receipts, s.verdicts = maps.Clone(s.requests), maps.Clone(s.actors), maps.Clone(s.receipts), maps.Clone(s.verdicts)
	return s
}

// ledger 是内存退款账本：事务复制状态、成功才提交，回调出错整体回滚。
type ledger struct {
	mu        sync.Mutex
	state     ledgerState
	threshold int64 // 金额超过它即有争议（分）
}

func newLedger(paidCents, threshold int64) *ledger {
	return &ledger{threshold: threshold, state: ledgerState{
		order:    refund.Order{PaidCents: paidCents, Currency: "CNY", DeliveryDays: 2, Version: 1},
		requests: map[string]refund.Request{}, actors: map[string]string{}, receipts: map[string]string{}, verdicts: map[string]verdict{},
	}}
}

func (l *ledger) Transaction(_ context.Context, f func(refund.Transaction) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	next := l.state.clone()
	if err := f(&ledgerTx{s: &next}); err != nil {
		return err
	}
	l.state = next
	return nil
}

func (l *ledger) read(fn func(*ledgerState)) { l.mu.Lock(); defer l.mu.Unlock(); fn(&l.state) }

// changeOrder 模拟审核等待期间订单事实变化。
func (l *ledger) changeOrder() {
	l.read(func(s *ledgerState) { s.order.Version++; s.order.DeliveryDays++ })
}

func (l *ledger) refunds() (n int) { l.read(func(s *ledgerState) { n = s.refunds }); return }

type ledgerTx struct{ s *ledgerState }

func (t *ledgerTx) Order(context.Context, string) (refund.Order, error) { return t.s.order, nil }
func (t *ledgerTx) Request(_ context.Context, id string) (refund.Request, string, error) {
	r, ok := t.s.requests[id]
	if !ok {
		return refund.Request{}, "", refund.ErrNotFound
	}
	return r, t.s.actors[id], nil
}

func (t *ledgerTx) Receipt(_ context.Context, id string) (string, error) {
	r, ok := t.s.receipts[id]
	if !ok {
		return "", refund.ErrNotFound
	}
	return r, nil
}
func (t *ledgerTx) TotalRefunded(context.Context, string) (int64, error) { return t.s.total, nil }
func (t *ledgerTx) InsertRequest(_ context.Context, r refund.Request) error {
	t.s.requests[r.RequestID] = r
	return nil
}

func (t *ledgerTx) Approve(_ context.Context, id, actor string, _ time.Time) error {
	t.s.actors[id] = actor
	return nil
}

func (t *ledgerTx) InsertRefund(_ context.Context, _ string, r refund.Request, receipt string) error {
	t.s.receipts[r.RequestID] = receipt
	t.s.total += r.AmountCents
	t.s.refunds++
	return nil
}

// refundDesk 组合账本、refund.Service 与计数，供退款成员使用。
type refundDesk struct {
	ledger                *ledger
	service               *refund.Service
	registers, executions atomic.Int32
}

func newRefundDesk(paidCents, threshold int64) *refundDesk {
	l := newLedger(paidCents, threshold)
	return &refundDesk{ledger: l, service: &refund.Service{
		Store: l,
		Authorizer: refund.AuthorizeFunc(func(_ context.Context, actor string, _ refund.Request) error {
			if actor == "" {
				return errors.New("审核人未认证")
			}
			return nil
		}),
		Policy: func(context.Context, refund.Request, refund.Order) error { return nil },
	}}
}

// register 登记申请，并经 refund.Service.OnRegister 在同一事务内写入争议判定；判定规则优先，Agent 只能升级为有争议。
func (d *refundDesk) register(ctx context.Context, id string, p proposal) (json.RawMessage, error) {
	d.registers.Add(1)
	var order refund.Order
	var threshold int64
	d.ledger.read(func(s *ledgerState) { order, threshold = s.order, d.ledger.threshold })
	v := verdict{}
	switch {
	case p.AmountCents > threshold:
		v = verdict{Disputed: true, Reason: fmt.Sprintf("金额超过 %d 分", threshold)}
	case p.Disputed:
		v = verdict{Disputed: true, Reason: "Agent 标记：" + p.Reason}
	}
	req := refund.Request{
		RequestID: id, OrderID: p.OrderID, AmountCents: p.AmountCents, Currency: order.Currency, PaidCents: order.PaidCents,
		DeliveryDays: order.DeliveryDays, OrderVersion: order.Version, ValidUntil: time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339Nano),
	}
	// 每次登记用一份带钩子的 Service 副本：钩子在新插入申请的同一事务内写入判定与登记序号。
	svc := *d.service
	svc.OnRegister = func(_ context.Context, tx refund.Transaction, r refund.Request) error {
		lt, ok := tx.(*ledgerTx)
		if !ok {
			return errors.New("账本事务类型不符")
		}
		lt.s.seq++
		v.Seq = lt.s.seq
		lt.s.verdicts[r.RequestID] = v
		return nil
	}
	registered, err := svc.Register(ctx, req)
	if err != nil {
		return nil, err
	}
	d.ledger.read(func(s *ledgerState) { v = s.verdicts[id] })
	return json.Marshal(registration{Request: registered, Verdict: v})
}

// reconcileRegister 只有申请与判定都已存在时才补记完成，避免以零值判定绕过审核。
func (d *refundDesk) reconcileRegister(id string) func(context.Context) (json.RawMessage, bool, error) {
	return func(context.Context) (json.RawMessage, bool, error) {
		var r registration
		var ok bool
		d.ledger.read(func(s *ledgerState) {
			var hasRequest, hasVerdict bool
			r.Request, hasRequest = s.requests[id]
			r.Verdict, hasVerdict = s.verdicts[id]
			ok = hasRequest && hasVerdict
		})
		if !ok {
			return nil, false, nil
		}
		data, err := json.Marshal(r)
		return data, err == nil, err
	}
}

// execute 先按申请 ID 查回执；订单事实已变化时把“需重新申请”作为业务结果返回；否则批准并退款。
func (d *refundDesk) execute(ctx context.Context, req refund.Request, approver string) (json.RawMessage, error) {
	d.executions.Add(1)
	if receipt, ok := d.receipt(req.RequestID); ok {
		return json.Marshal(execution{Receipt: receipt})
	}
	changed := false
	d.ledger.read(func(s *ledgerState) {
		changed = s.order.Version != req.OrderVersion || s.order.PaidCents != req.PaidCents || s.order.DeliveryDays != req.DeliveryDays
	})
	if changed {
		return json.Marshal(execution{OrderChanged: true})
	}
	if err := d.service.Approve(ctx, req, approver); err != nil {
		return nil, err
	}
	receipt, err := d.service.Refund(ctx, workflow.DurableKey(ctx), req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(execution{Receipt: receipt})
}

func (d *refundDesk) receipt(id string) (receipt string, ok bool) {
	d.ledger.read(func(s *ledgerState) { receipt, ok = s.receipts[id] })
	return
}

func (d *refundDesk) reconcileExecute(id string) func(context.Context) (json.RawMessage, bool, error) {
	return func(context.Context) (json.RawMessage, bool, error) {
		receipt, ok := d.receipt(id)
		if !ok {
			return nil, false, nil
		}
		data, err := json.Marshal(execution{Receipt: receipt})
		return data, err == nil, err
	}
}

// refundMember 是 S5 的退款成员：提议 → 登记（固定判定）→ 有争议时等待审核 → 执行（先查回执）。
func refundMember(desk *refundDesk, agent AgentFactory) Step[Conversation] {
	return Step[Conversation]{Name: "refund", Run: core.NewRunnableFunc(func(ctx context.Context, c Conversation) (Conversation, error) {
		reply := func(text string) (Conversation, error) {
			c.Turns = append(c.Turns, Turn{Agent: "refund", Output: text})
			return c, nil
		}
		owned, err := agent(ctx)
		if owned.Close != nil {
			defer func() { _ = owned.Close(context.WithoutCancel(ctx)) }()
		}
		if err != nil {
			return c, err
		}
		out, err := owned.Runnable.Invoke(ctx, renderTurns(c, 0))
		if err != nil {
			return c, err
		}
		var p proposal
		if err := json.Unmarshal([]byte(out.Result.(string)), &p); err != nil {
			return c, fmt.Errorf("退款提议无法解析: %w", err)
		}
		id := "refund-" + p.OrderID
		intent, err := json.Marshal(struct {
			RequestID string   `json:"request_id"`
			Proposal  proposal `json:"proposal"`
		}{id, p})
		if err != nil {
			return c, err
		}
		record, err := durable.Do(ctx, "refund/register", intent, durable.OperationPolicy{Reconcile: desk.reconcileRegister(id)},
			func(ctx context.Context) (json.RawMessage, error) { return desk.register(ctx, id, p) })
		if err != nil {
			return c, err
		}
		var reg registration
		if err := json.Unmarshal(record.Value, &reg); err != nil {
			return c, err
		}
		approver := "rule:auto"
		if reg.Verdict.Disputed {
			info, err := json.Marshal(map[string]any{"order_id": p.OrderID, "amount_cents": p.AmountCents, "reason": reg.Verdict.Reason, "submitter": "agent:refund"})
			if err != nil {
				return c, err
			}
			raw, err := durable.AwaitInput(ctx, "refund-review", info, validateReview)
			if err != nil {
				return c, err
			}
			var in reviewInput
			if err := json.Unmarshal(raw, &in); err != nil {
				return c, err
			}
			switch {
			case !in.Approved && in.Reason == "timeout":
				return reply("审核超时，按拒绝处理")
			case !in.Approved:
				return reply("人工拒绝：" + in.Note)
			}
			approver = in.Approver
		}
		intent, err = json.Marshal(struct {
			RequestID string `json:"request_id"`
			Approver  string `json:"approver"`
		}{id, approver})
		if err != nil {
			return c, err
		}
		record, err = durable.Do(ctx, "refund/execute", intent, durable.OperationPolicy{Reconcile: desk.reconcileExecute(id)},
			func(ctx context.Context) (json.RawMessage, error) { return desk.execute(ctx, reg.Request, approver) })
		if err != nil {
			return c, err
		}
		var result execution
		if err := json.Unmarshal(record.Value, &result); err != nil {
			return c, err
		}
		if result.OrderChanged {
			return reply("订单已变化，需重新申请")
		}
		return reply("已退款：" + result.Receipt)
	})}
}
```

- [ ] **Step 2: 写场景测试**

在 `multiagent/scenario_test.go` 末尾追加（导入已在 Task 12 中齐备）。子测试逐条对应规格 S5 行：等待与 Info、批准后退款恰好一次且退款 Agent 与登记从日志回放、判定与登记序号经钩子写入一次、审批前那次分流规则未再匹配且分流 `Calls()` 恰好加 1、批准缺审核人被拒、拒绝不退款、无争议直接退款、Agent 标记有争议时等待、等待期间改规则阈值与订单事实后判定不变、订单变化导致批准失败写回状态、超时按拒绝、“回执已写入、执行记录 pending”时经 `Reconcile` 补记不重复退款（之后用不带键的 `Run` 续跑）。

```go
// refundCase 装配 S5：分流 → 退款成员（登记、审核、执行）→ 分流 FINISH。
type refundCase struct {
	e              *env
	desk           *refundDesk
	triage, refund *mocks.ScriptedClient
}

func newRefundCase(t *testing.T, root string, amount, threshold int64, agentDisputed bool) *refundCase {
	t.Helper()
	p, err := json.Marshal(proposal{OrderID: "A1", AmountCents: amount, Disputed: agentDisputed, Reason: "客户情绪激动"})
	require.NoError(t, err)
	return &refundCase{
		e: newEnv(t, root), desk: newRefundDesk(100000, threshold),
		triage: mocks.NewScriptedClient(mocks.TextReply("选退款", "refund"),
			mocks.ScriptedRule{Name: "结束", Match: mocks.RequestContains("[refund]: "), Reply: mocks.ScriptedReply{Text: "FINISH"}}),
		refund: mocks.NewScriptedClient(mocks.TextReply("提议", string(p))),
	}
}

func (rc *refundCase) graph(t *testing.T) *workflow.DurableGraph[Conversation] {
	t.Helper()
	var s sessions
	member := refundMember(rc.desk, s.scripted(rc.refund, "refund", nil))
	next, err := TextNext(member)
	require.NoError(t, err)
	g, err := RouterSupervisor(workflow.Config{}, RouterConfig[Conversation]{
		Supervisor: textAgent(t, "triage", rc.triage),
		Members:    []Step[Conversation]{member}, Next: next, MaxRounds: 2,
	})
	require.NoError(t, err)
	return compile(t, rc.e, g, "v1")
}

// start 运行到审核等待，返回中断。
func (rc *refundCase) start(t *testing.T) workflow.InterruptPoint {
	t.Helper()
	snap, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{Input: "订单 A1 重复扣款，申请退款"}, "")
	require.ErrorIs(t, err, durable.ErrApprovalRequired)
	require.Len(t, snap.Interrupts, 1)
	require.Equal(t, "refund-review", snap.Interrupts[0].ID)
	require.Zero(t, rc.desk.ledger.refunds(), "等待期间账本没有退款")
	return snap.Interrupts[0]
}

func (rc *refundCase) answer(t *testing.T, key, input string) (*workflow.GraphSnapshot[Conversation], error) {
	t.Helper()
	rc.e.restart()
	return rc.graph(t).Resume(rc.e.ctx(), rc.e.id, workflow.ResumeRequest{Key: key, Input: json.RawMessage(input)})
}

func lastOutput(c Conversation, agent string) string {
	for i := len(c.Turns) - 1; i >= 0; i-- {
		if c.Turns[i].Agent == agent {
			return c.Turns[i].Output
		}
	}
	return ""
}

func TestScenarioRefundApproval(t *testing.T) {
	const approve = `{"approved":true,"note":"核实无误","approver":"finance:li"}`
	t.Run("有争议时等待审核后退款一次", func(t *testing.T) {
		rc := newRefundCase(t, "refund-approve", 80000, 50000, false)
		point := rc.start(t)
		var info map[string]any
		require.NoError(t, json.Unmarshal(point.Info, &info))
		require.Equal(t, "A1", info["order_id"])
		require.EqualValues(t, 80000, info["amount_cents"])
		require.Contains(t, info["reason"], "金额超过")

		_, err := rc.answer(t, point.Key, `{"approved":true,"note":"x","approver":""}`)
		var rejected *workflow.ResumeRejectedError
		require.ErrorAs(t, err, &rejected, "批准时必须有审核人")

		snap, err := rc.answer(t, point.Key, approve)
		require.NoError(t, err)
		require.True(t, snap.Done)
		require.Contains(t, lastOutput(snap.State, "refund"), "已退款：")
		require.Equal(t, 1, rc.desk.ledger.refunds(), "退款恰好执行一次")
		require.Equal(t, 1, rc.refund.Calls(), "退款 Agent 的模型请求从日志回放")
		require.EqualValues(t, 1, rc.desk.registers.Load(), "登记操作从日志回放")
		rc.desk.ledger.read(func(s *ledgerState) {
			require.Equal(t, 1, s.verdicts["refund-A1"].Seq, "判定与登记序号经 OnRegister 写入一次")
		})
		require.Equal(t, 1, rc.triage.Hits(0), "审批前那次分流请求没有被再次匹配")
		require.Equal(t, 2, rc.triage.Calls(), "分流只多出退款后的那次请求")
	})
	t.Run("拒绝时不退款", func(t *testing.T) {
		rc := newRefundCase(t, "refund-reject", 80000, 50000, false)
		snap, err := rc.answer(t, rc.start(t).Key, `{"approved":false,"note":"重复申请","approver":"finance:li"}`)
		require.NoError(t, err)
		require.Equal(t, "人工拒绝：重复申请", lastOutput(snap.State, "refund"))
		require.Zero(t, rc.desk.ledger.refunds())
	})
	t.Run("无争议时直接退款", func(t *testing.T) {
		rc := newRefundCase(t, "refund-direct", 3000, 50000, false)
		snap, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{Input: "订单 A1 申请退款"}, "")
		require.NoError(t, err)
		require.Contains(t, lastOutput(snap.State, "refund"), "已退款：")
		require.Equal(t, 1, rc.desk.ledger.refunds())
	})
	t.Run("Agent 标记有争议时进入等待", func(t *testing.T) {
		rc := newRefundCase(t, "refund-agent-flag", 3000, 50000, true)
		var info map[string]any
		require.NoError(t, json.Unmarshal(rc.start(t).Info, &info))
		require.Contains(t, info["reason"], "Agent 标记")
	})
	t.Run("等待期间规则或订单变化不改变判定", func(t *testing.T) {
		rc := newRefundCase(t, "refund-rule-change", 80000, 50000, false)
		point := rc.start(t)
		rc.desk.ledger.read(func(*ledgerState) { rc.desk.ledger.threshold = 1000000 }) // 新阈值下本应无争议
		rc.desk.ledger.changeOrder()
		snap, err := rc.answer(t, point.Key, `{"approved":false,"note":"不同意","approver":"finance:li"}`)
		require.NoError(t, err)
		require.Equal(t, "人工拒绝：不同意", lastOutput(snap.State, "refund"), "判定已固定，审核结果仍然生效")
		require.Zero(t, rc.desk.ledger.refunds())
		require.EqualValues(t, 1, rc.desk.registers.Load())
	})
	t.Run("订单变化导致批准失败写回状态", func(t *testing.T) {
		rc := newRefundCase(t, "refund-order-change", 80000, 50000, false)
		point := rc.start(t)
		rc.desk.ledger.changeOrder()
		snap, err := rc.answer(t, point.Key, approve)
		require.NoError(t, err, "业务结果不是节点错误")
		require.Equal(t, "订单已变化，需重新申请", lastOutput(snap.State, "refund"))
		require.Zero(t, rc.desk.ledger.refunds())
	})
	t.Run("等待超时按拒绝处理", func(t *testing.T) {
		rc := newRefundCase(t, "refund-timeout", 80000, 50000, false)
		snap, err := rc.answer(t, rc.start(t).Key, `{"approved":false,"reason":"timeout"}`)
		require.NoError(t, err)
		require.Equal(t, "审核超时，按拒绝处理", lastOutput(snap.State, "refund"))
		require.Zero(t, rc.desk.ledger.refunds())
	})
	t.Run("回执已写入而执行记录停在 pending", func(t *testing.T) {
		rc := newRefundCase(t, "refund-reconcile", 80000, 50000, false)
		point := rc.start(t)
		rc.e.fault.arm(false, operationFails("refund/execute", "completed"))
		_, err := rc.answer(t, point.Key, approve)
		require.ErrorIs(t, err, errInjected)
		require.Equal(t, 1, rc.desk.ledger.refunds())

		rc.e.restart()
		snap, err := rc.graph(t).Run(rc.e.ctx(), rc.e.id, Conversation{}, "")
		require.NoError(t, err, "已回应的中断无需再次提交，用 Run 续跑")
		require.Contains(t, lastOutput(snap.State, "refund"), "已退款：")
		require.Equal(t, 1, rc.desk.ledger.refunds(), "Reconcile 补记完成，不重复退款")
		require.EqualValues(t, 1, rc.desk.executions.Load(), "执行操作没有重跑")
	})
}
```

- [ ] **Step 3: 运行测试**

Run: `go test -race -count=1 -run '^TestScenarioRefundApproval$' ./multiagent/`
Expected: PASS（8 个子测试）

- [ ] **Step 4: 全包回归与 vet**

Run: `go test -race -count=1 ./multiagent/... && go vet ./multiagent/... && gofumpt -l multiagent && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' multiagent`
Expected: 测试 PASS，其余无输出

- [ ] **Step 5: 提交**

```bash
git add multiagent/refund_fixture_test.go multiagent/scenario_test.go
git commit -m "test(multiagent): 覆盖 S5 登记后审核、先查回执与对账补记"
```


### Task 15: 离线示例 examples/multi-agent-patterns

**Files:**
- Create: `examples/multi-agent-patterns/main.go`
- Create: `examples/multi-agent-patterns/main_test.go`
- Create: `examples/multi-agent-patterns/README.md`

**Interfaces:**
- Consumes: `multiagent` 全部导出 API（Task 2–9）、`mocks.ScriptedClient`（Task 1）
- Produces: `func run(ctx context.Context, w io.Writer) error`：依次运行 S1、S2、S3、S5、S7、S9 与组合流程，每个场景结束写一行 `<名称> 完成`。

- [ ] **Step 1: 写失败测试**

验收固化为测试：在 goroutine 中调用 `run`，用独立的 30s 计时器等待（不只依赖 ctx 超时：图会等节点关闭流，见 `workflow/execute.go:96-102`），w 用带锁的 Writer。先例 `examples/integration/human-in-loop/hitl_demo_test.go:28-38`。

`examples/multi-agent-patterns/main_test.go`：

```go
package main

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lockedWriter 让示例写入与测试读取共用一把锁，超时分支读取输出时不触发 -race。
type lockedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// 示例离线运行：每个场景都应写出完成标记。不能只依赖 ctx 超时，图会等节点关闭流，不响应取消的节点会让调用挂住。
func TestMultiAgentPatternsDemo(t *testing.T) {
	out := &lockedWriter{}
	done := make(chan error, 1)
	go func() { done <- run(context.Background(), out) }()
	select {
	case err := <-done:
		require.NoError(t, err, out.String())
	case <-time.After(30 * time.Second):
		t.Fatalf("示例未在 30 秒内结束：\n%s", out.String())
	}
	for _, marker := range []string{"S1 完成", "S2 完成", "S3 完成", "S5 完成", "S7 完成", "S9 完成", "组合流程 完成"} {
		require.Contains(t, out.String(), marker+"\n")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -count=1 ./examples/multi-agent-patterns/`
Expected: FAIL，编译错误 `undefined: run`

- [ ] **Step 3: 写示例**

各场景共用 `kit` 装配辅助；S5 用持久图并以运行 ID 作根预算 RootKey。

`examples/multi-agent-patterns/main.go`：

```go
// multi-agent-patterns 用 multiagent 预置模式离线演示客服场景 S1、S2、S3、S5、S7、S9 与组合流程，
// 模型由 testing/mocks.ScriptedClient 按脚本回答，不需要 API 密钥。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/multiagent"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, w io.Writer) error {
	dir, err := os.MkdirTemp("", "multi-agent-patterns-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	k := &kit{dir: dir}
	for _, s := range []struct {
		name string
		run  func(context.Context, io.Writer, *kit) error
	}{
		{"S1", ticketRouting},
		{"S2", replyReview},
		{"S3", parallelLookup},
		{"S5", refundApproval},
		{"S7", delegation},
		{"S9", invalidDecision},
		{"组合流程", composedPipeline},
	} {
		if err := s.run(ctx, w, k); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		_, _ = fmt.Fprintf(w, "%s 完成\n", s.name)
	}
	return nil
}

// kit 是各场景共用的装配辅助：会话工厂、文本 Step、根预算与持久图。
type kit struct{ dir string }

// agent 每次新建一个 Builder 会话；tools 经独立 Dispatcher 包装，满足持久接入契约。
func (k *kit) agent(client *mocks.ScriptedClient, scope string, list ...interfaces.Tool) (multiagent.AgentFactory, error) {
	var wrapped []interfaces.Tool
	if len(list) > 0 {
		reg := tools.NewRegistry()
		for _, t := range list {
			if err := reg.Register(t); err != nil {
				return nil, err
			}
		}
		d, err := tools.NewDispatcher(reg, tools.DispatchConfig{})
		if err != nil {
			return nil, err
		}
		wrapped = d.Tools()
	}
	return func(context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		b := builder.NewSimpleBuilder(client).WithDurableScope(scope)
		if len(wrapped) > 0 {
			b = b.WithTools(wrapped...).WithMaxIterations(4)
		}
		a, err := b.Build()
		if err != nil {
			return tools.OwnedRunnable[any, *builder.AgentOutput]{}, err
		}
		return tools.OwnedRunnable[any, *builder.AgentOutput]{Runnable: a.AsRunnable(), Close: a.Shutdown}, nil
	}, nil
}

func (k *kit) text(name string, client *mocks.ScriptedClient) (multiagent.Step[multiagent.Conversation], error) {
	factory, err := k.agent(client, name)
	if err != nil {
		return multiagent.Step[multiagent.Conversation]{}, err
	}
	return multiagent.TextStep(name, factory)
}

func (k *kit) texts(clients map[string]*mocks.ScriptedClient, names ...string) ([]multiagent.Step[multiagent.Conversation], error) {
	steps := make([]multiagent.Step[multiagent.Conversation], len(names))
	for i, name := range names {
		var err error
		if steps[i], err = k.text(name, clients[name]); err != nil {
			return nil, err
		}
	}
	return steps, nil
}

// budgetFor 返回根预算上下文；持久运行的预算与图共用存储，RootKey 与运行 ID 一致。
func budgetFor(ctx context.Context, runID string, store durable.Store) (context.Context, error) {
	cfg := budget.Config{RootKey: runID, Version: "v1", MaxModelRequests: 20}
	var c *budget.Controller
	var err error
	if store == nil {
		c, err = budget.New(cfg)
	} else {
		c, err = budget.NewDurable(cfg, store)
	}
	if err != nil {
		return nil, err
	}
	return budget.WithController(ctx, c), nil
}

func invoke[T any](ctx context.Context, g *workflow.Graph[T], in T) (T, error) {
	var zero T
	run, err := g.Compile(ctx)
	if err != nil {
		return zero, err
	}
	ctx, err = budgetFor(ctx, "demo", nil)
	if err != nil {
		return zero, err
	}
	return run.Invoke(ctx, in)
}

func show(w io.Writer, c multiagent.Conversation) {
	for _, t := range c.Turns {
		_, _ = fmt.Fprintf(w, "  [%s] %s\n", t.Agent, strings.ReplaceAll(t.Output, "\n", " "))
	}
}

func reply(name, text string) mocks.ScriptedRule { return mocks.TextReply(name, text) }

// S1 工单分流：分流 Agent 依次交给退款、物流，最后 FINISH。
func ticketRouting(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"triage":    mocks.NewScriptedClient(reply("选退款", "refund"), reply("选物流", "logistics"), reply("结束", "FINISH")),
		"refund":    mocks.NewScriptedClient(reply("退款", "已登记退款")),
		"logistics": mocks.NewScriptedClient(reply("物流", "已催促承运商")),
	}, "triage", "refund", "logistics")
	if err != nil {
		return err
	}
	next, err := multiagent.TextNext(steps[1:]...)
	if err != nil {
		return err
	}
	g, err := multiagent.RouterSupervisor(workflow.Config{}, multiagent.RouterConfig[multiagent.Conversation]{
		Supervisor: steps[0], Members: steps[1:], Next: next, MaxRounds: 4,
	})
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "重复扣款，且包裹三天未更新"})
	show(w, out)
	return err
}

// parseVerdict 读取最后一轮评审：PASS 通过，REJECT 开头不通过，其余包装 ErrInvalidRoute。
func parseVerdict(c multiagent.Conversation) (bool, error) {
	for i := len(c.Turns) - 1; i >= 0; i-- {
		if c.Turns[i].Agent != "review" {
			continue
		}
		line, _, _ := strings.Cut(c.Turns[i].Output, "\n")
		switch line = strings.TrimSpace(line); {
		case line == "PASS":
			return true, nil
		case strings.HasPrefix(line, "REJECT"):
			return false, nil
		}
		return false, fmt.Errorf("%w：评审结论 %q 无法解析", multiagent.ErrInvalidRoute, line)
	}
	return false, fmt.Errorf("%w：没有评审结论", multiagent.ErrInvalidRoute)
}

// S2 回复写作与评审：第一轮不通过，带意见重写后通过。
func replyReview(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"write":  mocks.NewScriptedClient(reply("初稿", "退款已处理。"), reply("二稿", "退款已处理，预计 3 个工作日到账。")),
		"review": mocks.NewScriptedClient(reply("不通过", "REJECT 缺少到账时间"), reply("通过", "PASS")),
	}, "write", "review")
	if err != nil {
		return err
	}
	g, err := multiagent.Loop(workflow.Config{}, multiagent.LoopConfig[multiagent.Conversation]{
		Steps: steps, MaxIterations: 3,
		Until: func(_ context.Context, c multiagent.Conversation) (bool, error) { return parseVerdict(c) },
	})
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "客户询问退款进度"})
	show(w, out)
	return err
}

// S3 多源并行查询：知识库、订单、物流并行，结果按登记顺序合并后交给回答 Agent。
func parallelLookup(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"kb":       mocks.NewScriptedClient(reply("知识库", "七天无理由退货")),
		"order":    mocks.NewScriptedClient(reply("订单", "A1 已签收")),
		"shipping": mocks.NewScriptedClient(reply("物流", "签收于 11-01")),
		"answer":   mocks.NewScriptedClient(reply("回答", "可在 11-08 前申请退货")),
	}, "kb", "order", "shipping", "answer")
	if err != nil {
		return err
	}
	lookup, err := multiagent.ParallelStep("lookup", multiagent.ParallelConfig[multiagent.Conversation]{
		Steps: steps[:3], Clone: multiagent.CloneConversation, Merge: multiagent.MergeTurns,
	})
	if err != nil {
		return err
	}
	g, err := multiagent.Sequential(workflow.Config{}, lookup, steps[3])
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "A1 还能退货吗"})
	show(w, out)
	return err
}

// refundBook 是演示用的退款账本：登记与判定同一次写入，回执按申请 ID 去重。
// 真实业务使用 workflow/refund.Service 与带事务的存储，见客服编排案例。
type refundBook struct {
	mu       sync.Mutex
	verdicts map[string]bool
	receipts map[string]string
}

type reviewInput struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note"`
	Approver string `json:"approver"`
	Reason   string `json:"reason,omitempty"`
}

// refundMember 是 S5 的退款成员：提议 → 登记并固定争议判定 → 有争议时等待审核 → 先查回执再退款。
func (b *refundBook) member(agent multiagent.AgentFactory) multiagent.Step[multiagent.Conversation] {
	return multiagent.Step[multiagent.Conversation]{Name: "refund", Run: core.NewRunnableFunc(func(ctx context.Context, c multiagent.Conversation) (multiagent.Conversation, error) {
		answer := func(text string) (multiagent.Conversation, error) {
			c.Turns = append(c.Turns, multiagent.Turn{Agent: "refund", Output: text})
			return c, nil
		}
		owned, err := agent(ctx)
		if err != nil {
			return c, err
		}
		defer func() { _ = owned.Close(context.WithoutCancel(ctx)) }()
		out, err := owned.Runnable.Invoke(ctx, c.Input)
		if err != nil {
			return c, err
		}
		var p struct {
			OrderID     string `json:"order_id"`
			AmountCents int64  `json:"amount_cents"`
		}
		if err := json.Unmarshal([]byte(out.Result.(string)), &p); err != nil {
			return c, err
		}
		id := "refund-" + p.OrderID
		intent, _ := json.Marshal(map[string]any{"request_id": id, "proposal": p})
		reg, err := durable.Do(ctx, "refund/register", intent, durable.OperationPolicy{Reconcile: b.lookupVerdict(id)}, func(context.Context) (json.RawMessage, error) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.verdicts[id] = p.AmountCents > 50000 // 规则：超过 500 元有争议
			return json.Marshal(b.verdicts[id])
		})
		if err != nil {
			return c, err
		}
		approver := "rule:auto"
		if string(reg.Value) == "true" {
			info, _ := json.Marshal(map[string]any{"order_id": p.OrderID, "amount_cents": p.AmountCents, "reason": "金额超过 500 元"})
			raw, err := durable.AwaitInput(ctx, "refund-review", info, func(raw json.RawMessage) error {
				var in reviewInput
				if json.Unmarshal(raw, &in) != nil || (in.Approved && in.Approver == "") {
					return errors.New("审核输入需要 approved，批准时需要 approver")
				}
				return nil
			})
			if err != nil {
				return c, err
			}
			var in reviewInput
			_ = json.Unmarshal(raw, &in)
			if !in.Approved {
				return answer("人工拒绝：" + in.Note)
			}
			approver = in.Approver
		}
		intent, _ = json.Marshal(map[string]string{"request_id": id, "approver": approver})
		exec, err := durable.Do(ctx, "refund/execute", intent, durable.OperationPolicy{Reconcile: b.lookupReceipt(id)}, func(context.Context) (json.RawMessage, error) {
			b.mu.Lock()
			defer b.mu.Unlock()
			if receipt, ok := b.receipts[id]; ok { // 先查回执，已退款的申请不再批准或退款
				return json.Marshal(receipt)
			}
			b.receipts[id] = "receipt-" + id
			return json.Marshal(b.receipts[id])
		})
		if err != nil {
			return c, err
		}
		return answer("已退款，回执 " + strings.Trim(string(exec.Value), `"`) + "，审核人 " + approver)
	})}
}

func (b *refundBook) lookupVerdict(id string) func(context.Context) (json.RawMessage, bool, error) {
	return func(context.Context) (json.RawMessage, bool, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		v, ok := b.verdicts[id]
		if !ok {
			return nil, false, nil
		}
		data, err := json.Marshal(v)
		return data, err == nil, err
	}
}

func (b *refundBook) lookupReceipt(id string) func(context.Context) (json.RawMessage, bool, error) {
	return func(context.Context) (json.RawMessage, bool, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		r, ok := b.receipts[id]
		if !ok {
			return nil, false, nil
		}
		data, err := json.Marshal(r)
		return data, err == nil, err
	}
}

// S5 高风险操作审批：有争议的退款在执行前停下，审核人批准后从检查点续跑。
func refundApproval(ctx context.Context, w io.Writer, k *kit) error {
	store, err := durable.NewFileStore(k.dir)
	if err != nil {
		return err
	}
	book := &refundBook{verdicts: map[string]bool{}, receipts: map[string]string{}}
	refundAgent, err := k.agent(mocks.NewScriptedClient(reply("提议", `{"order_id":"A1","amount_cents":80000}`)), "refund")
	if err != nil {
		return err
	}
	triage, err := k.text("triage", mocks.NewScriptedClient(reply("选退款", "refund"), reply("结束", "FINISH")))
	if err != nil {
		return err
	}
	member := book.member(refundAgent)
	next, err := multiagent.TextNext(member)
	if err != nil {
		return err
	}
	g, err := multiagent.RouterSupervisor(workflow.Config{}, multiagent.RouterConfig[multiagent.Conversation]{
		Supervisor: triage, Members: []multiagent.Step[multiagent.Conversation]{member}, Next: next, MaxRounds: 2,
	})
	if err != nil {
		return err
	}
	d, err := g.CompileDurable(ctx, workflow.DurableConfig{Store: store, Version: "v1"})
	if err != nil {
		return err
	}
	const runID = "ticket-s5"
	runCtx, err := budgetFor(ctx, runID, store)
	if err != nil {
		return err
	}
	snap, err := d.Run(runCtx, runID, multiagent.Conversation{Input: "订单 A1 申请退款 800 元"}, "")
	if !errors.Is(err, durable.ErrApprovalRequired) || len(snap.Interrupts) != 1 {
		return fmt.Errorf("应等待审核，得到 %v", err)
	}
	_, _ = fmt.Fprintf(w, "  等待审核：%s\n", snap.Interrupts[0].Info)
	// 宿主先认证审核人，再把 approver 填入回应。
	snap, err = d.Resume(runCtx, runID, workflow.ResumeRequest{Key: snap.Interrupts[0].Key, Input: json.RawMessage(`{"approved":true,"note":"核实无误","approver":"finance:li"}`)})
	if err != nil {
		return err
	}
	show(w, snap.State)
	return nil
}

// S7 复杂咨询由模型委派：主 Agent 调用账单与技术两个专家工具后汇总。
func delegation(ctx context.Context, w io.Writer, k *kit) error {
	billing, err := k.agent(mocks.NewScriptedClient(reply("账单", "确认重复扣款 1 次")), "billing")
	if err != nil {
		return err
	}
	tech, err := k.agent(mocks.NewScriptedClient(reply("技术", "需要重置密码")), "tech")
	if err != nil {
		return err
	}
	members, err := multiagent.SupervisorTools(
		multiagent.Member{Name: "billing", Description: "账单专家", Version: "v1", Factory: billing},
		multiagent.Member{Name: "tech", Description: "技术专家", Version: "v1", Factory: tech},
	)
	if err != nil {
		return err
	}
	supervisor, err := k.agent(mocks.NewScriptedClient(
		mocks.ToolCallReply("委派", mocks.ToolCall("c1", "billing", `{"task":"核对扣款"}`), mocks.ToolCall("c2", "tech", `{"task":"排查登录"}`)),
		reply("汇总", "已为您退回重复扣款，并请按指引重置密码"),
	), "supervisor", members...)
	if err != nil {
		return err
	}
	step, err := multiagent.TextStep("supervisor", supervisor)
	if err != nil {
		return err
	}
	g, err := multiagent.Sequential(workflow.Config{}, step)
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "扣款两次，而且登录不了"})
	show(w, out)
	return err
}

func transferToHuman() *multiagent.Step[multiagent.Conversation] {
	return &multiagent.Step[multiagent.Conversation]{Name: "human", Run: core.NewRunnableFunc(func(_ context.Context, c multiagent.Conversation) (multiagent.Conversation, error) {
		c.Turns = append(c.Turns, multiagent.Turn{Agent: "human", Output: "已转人工，交接上下文含 " + fmt.Sprint(len(c.Turns)) + " 轮记录"})
		return c, nil
	})}
}

// S9 判断无效转人工：分流输出无法识别时进入兜底 Step 后结束。
func invalidDecision(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"triage": mocks.NewScriptedClient(reply("无效", "退款（原因：重复扣款）")),
		"refund": mocks.NewScriptedClient(),
	}, "triage", "refund")
	if err != nil {
		return err
	}
	next, err := multiagent.TextNext(steps[1])
	if err != nil {
		return err
	}
	g, err := multiagent.RouterSupervisor(workflow.Config{}, multiagent.RouterConfig[multiagent.Conversation]{
		Supervisor: steps[0], Members: steps[1:], Next: next, MaxRounds: 3, Fallback: transferToHuman(),
	})
	if err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "重复扣款"})
	show(w, out)
	return err
}

const reviewRounds = 3 // MaxSteps = 1 + 2*reviewRounds + 1

// composedPipeline 手写“抽取 → 写作⇄评审 → 合规”，用 AddStep 复用 Step，用 GuardRoute 复用兜底。
func composedPipeline(ctx context.Context, w io.Writer, k *kit) error {
	steps, err := k.texts(map[string]*mocks.ScriptedClient{
		"extract":    mocks.NewScriptedClient(reply("抽取", "订单 A1，退款")),
		"write":      mocks.NewScriptedClient(reply("初稿", "退款已处理。"), reply("二稿", "退款已处理，3 个工作日到账。")),
		"review":     mocks.NewScriptedClient(reply("不通过", "REJECT 缺少到账时间"), reply("通过", "PASS")),
		"compliance": mocks.NewScriptedClient(reply("合规", "合规")),
	}, "extract", "write", "review", "compliance")
	if err != nil {
		return err
	}
	g := workflow.NewGraph[multiagent.Conversation](workflow.Config{MaxSteps: 1 + 2*reviewRounds + 1})
	for _, s := range append(steps, *transferToHuman()) {
		if err := multiagent.AddStep(g, s); err != nil {
			return err
		}
	}
	for _, e := range [][2]string{{workflow.START, "extract"}, {"extract", "write"}, {"write", "review"}, {"compliance", workflow.END}, {"human", workflow.END}} {
		if err := g.AddEdge(e[0], e[1]); err != nil {
			return err
		}
	}
	reviewExit := func(_ context.Context, c multiagent.Conversation) (string, error) {
		passed, err := parseVerdict(c)
		if err != nil {
			return "", err
		}
		if passed {
			return "compliance", nil
		}
		rounds := 0
		for _, t := range c.Turns {
			if t.Agent == "review" {
				rounds++
			}
		}
		if rounds >= reviewRounds {
			return "", fmt.Errorf("%w: review loop limit %d", workflow.ErrMaxSteps, reviewRounds)
		}
		return "write", nil
	}
	route, targets, err := multiagent.GuardRoute(reviewExit, []string{"write", "compliance"}, "human")
	if err != nil {
		return err
	}
	if err := g.AddBranch("review", route, targets...); err != nil {
		return err
	}
	out, err := invoke(ctx, g, multiagent.Conversation{Input: "我要退款"})
	show(w, out)
	return err
}
```

`examples/multi-agent-patterns/README.md`：

````markdown
# multi-agent-patterns

离线演示 `multiagent` 预置模式，模型由 `testing/mocks.ScriptedClient` 按脚本回答，不需要 API 密钥：

| 场景 | 模式 |
| --- | --- |
| S1 工单分流 | `RouterSupervisor` + `TextNext` |
| S2 回复写作与评审 | `Loop` + `Until` |
| S3 多源并行查询 | `Sequential` + `ParallelStep` |
| S5 高风险操作审批 | 持久 `RouterSupervisor`，退款成员内 `durable.Do` 与 `durable.AwaitInput` |
| S7 复杂咨询由模型委派 | `SupervisorTools` |
| S9 判断无效转人工 | `RouterConfig.Fallback` |
| 组合流程 | 手写图 + `AddStep` + `GuardRoute` |

```bash
go run ./examples/multi-agent-patterns
go test ./examples/multi-agent-patterns
```

S5 的账本只为演示；真实业务使用 `workflow/refund.Service` 与带事务的存储。各模式的取舍与持久接入契约见 [多 Agent 预置模式指南](../../docs/guides/MULTI_AGENT_PATTERNS.md)。
````

- [ ] **Step 4: 运行测试与示例**

Run: `go test -race -count=1 ./examples/multi-agent-patterns/ && go run ./examples/multi-agent-patterns`
Expected: 测试 PASS；`go run` 依次打印各场景轮次与 `S1 完成` … `组合流程 完成`，退出码 0

- [ ] **Step 5: vet、lint 与格式检查**

Run: `go vet ./examples/multi-agent-patterns/ && golangci-lint run ./examples/multi-agent-patterns/... && gofumpt -l examples/multi-agent-patterns && gci list -s standard -s default -s 'Prefix(github.com/kart-io/goagent)' examples/multi-agent-patterns`
Expected: `0 issues.`，其余无输出

- [ ] **Step 6: 提交**

```bash
git add examples/multi-agent-patterns
git commit -m "docs(examples): 新增多 Agent 预置模式离线示例"
```


### Task 16: 文档：指南、索引、架构、Eino 对照、定位文档与 CHANGELOG

**Files:**
- Create: `docs/guides/MULTI_AGENT_PATTERNS.md`
- Modify: `README.md`、`README_CN.md`（“Durable Execution and Orchestration”/“持久执行与编排”列表）
- Modify: `docs/guides/AGENT_COMPOSITION.md`（“选择入口”表）
- Modify: `examples/README.md`（目录表）
- Modify: `docs/architecture/ARCHITECTURE.md`（“包依赖”图与“包职责”表）
- Modify: `docs/research/2026-11-04-eino-comparison.md:49,73,78`
- Modify: `docs/research/2026-10-29-goagent-positioning.md` §5
- Modify: `CHANGELOG.md`（在 Task 13 建立的 `[Unreleased]` Added 开头追加三条）

**Interfaces:**
- Consumes: Task 1–15 的 API、行为与示例路径
- Produces: 用户文档；指南链接 `MULTI_AGENT_USAGE.md` 与 `NATIVE_ORCHESTRATION.md#多-agent-作用域与入口约束`。

- [ ] **Step 1: 写指南**

指南以 S1–S9 为线索覆盖规格“文档”一节要求的全部主题：选择模式与两种 Supervisor 的取舍、状态设计、持久接入契约（预算配置不可变、模型 pending 与 `WithInterruptedModelResend`）、轮数上限与超限转人工（含 RootKey 与运行 ID 一致）、路由失败与兜底、版本分流示例、文本层窗口与 render 确定性、内容隔离与登记后审核（S5）、人工等待与截止时间及运行 TTL、并行分支事件、支持的组合与手写组合流程、运营监控；并说明 `examples/advanced/multi-agent-*` 演示手写拓扑。

`docs/guides/MULTI_AGENT_PATTERNS.md`：

````markdown
# 多 Agent 预置模式

`multiagent` 包在 `workflow` 之上提供三个图构造器（`Sequential`、`Loop`、`RouterSupervisor`）、并行组合件 `ParallelStep`、工具型 Supervisor（`SupervisorTools`）与文本接力层（`Conversation`、`TextStep`、`TextNext`）。构造器返回未编译的 `*workflow.Graph[T]`，宿主自己选择 `Compile` 或 `CompileDurable`，持久语义直接继承 `workflow`。本包不新增运行时、调度器或消息投递。

本包与 v0.17 删除的旧 `multiagent` 同名，但 API 与语义不兼容，旧代码不能直接迁移。设计依据见[多 Agent 预置模式设计](../superpowers/specs/2026-11-05-multiagent-patterns-design.md)，离线示例见 [`examples/multi-agent-patterns`](../../examples/multi-agent-patterns/README.md)。`examples/advanced/multi-agent-*` 四个示例演示手写拓扑，本包演示预置模式。

## 选择模式

| 场景 | 模式 | 要点 |
|---|---|---|
| S1 工单分流 | `RouterSupervisor` + `TextNext` | 分流 Agent 输出首行为成员名或 `FINISH`；`MaxRounds` 限制成员执行次数 |
| S2 回复写作与评审 | `Loop` | `Until` 读取评审结论；`MaxIterations` 是硬上限 |
| S3 多源并行查询 | `Sequential` 中放 `ParallelStep` | `Clone` 用 `CloneConversation`，`Merge` 用 `MergeTurns` |
| S4 固定流水线 | `Sequential` + `AgentStep` | 类型化状态；`out` 返回错误即节点失败 |
| S5 高风险操作审批 | `RouterSupervisor`，退款成员为自定义 `Step.Run` | 登记 → 审核 → 执行分段，每段由持久操作固定 |
| S6 向客户追问 | 自定义 `Step.Run` 内 `durable.AwaitInput` | 格式校验放进 validate |
| S7 复杂咨询由模型委派 | `SupervisorTools` | 主 Agent 自行决定调用哪些专家 |
| S8 超限转人工 | `MaxRounds` / `MaxIterations` | 返回 `workflow.ErrMaxSteps`，换运行 ID 与新根预算 |
| S9 判断无效转人工 | `RouterConfig.Fallback`、`LoopConfig.Fallback` | 路由无效时执行兜底 Step 后结束 |

两种 Supervisor 的取舍：`RouterSupervisor` 的每一步都是图节点，有节点检查点、可列入 `ApprovalNodes`、步数有硬上限，路由由宿主代码决定；`SupervisorTools` 把成员做成模型工具，由模型在一个回合内决定调用哪些成员、调用几次，灵活但只有 Agent 的 `MaxIterations` 与根预算约束，成员调用不是图节点，不能单独审批。

## 状态设计

一张图只有一个状态类型 T。持久图首次执行也会对状态做 JSON 往返，并行分支的输出在 `Merge` 前同样经过 JSON 往返，因此 T 只用导出字段，不放 `any` 内的具体类型、依赖指针身份的数据，或数字类型会被改变的值。

文本接力层用 `Conversation{Input, Turns}`；需要订单号这类类型化字段时，全部步骤用 `AgentStep` 加自定义 T。`AgentStep` 的 `in`、`out`，以及 `Until`、`Next`、`Merge` 都必须是纯函数：它们会在恢复后重复执行，`in` 的结果还进入模型请求意图。需要模型判断的逻辑放进 Step，由路由读取 Step 写回状态的结果。

## 步数与轮数上限

| 模式 | `MaxSteps` |
|---|---|
| Sequential | 步数 n |
| Loop | `MaxIterations × n` |
| RouterSupervisor | `2 × MaxRounds + 1` |

每个 `ParallelStep` 计 1 步。`MaxSteps` 由构造器计算，`cfg.MaxSteps` 必须为 0；`cfg.MaxConcurrency` 照常传给并行节点。构造器返回的图只应直接编译：继续 `AddNode` 不会报错，但步数上限不再准确。

执行器只为节点计步，路由不计步，最后一个允许的节点执行后仍会路由，路由到 `END` 即成功：

- Loop 第 `MaxIterations` 轮结束时 `Until` 为真，正常结束；为假时返回 `workflow.ErrMaxSteps`。
- Router 第 `MaxRounds` 次成员执行后 Supervisor 还会再执行一次（再发一次模型请求）：返回 `FINISH` 正常结束，选择成员返回 `ErrMaxSteps`。
- Fallback 不增加步数：Router 的 Fallback 占用一次成员轮数，Loop 的 Fallback 占用下一轮第一步的位置。最后一次判断无效时，配置了 Fallback 返回 `ErrMaxSteps`（与超限同样转人工），没有配置返回 `ErrInvalidRoute`。
- 需要“固定跑 N 轮后正常结束”时，在 T 中记录轮数，由 `Until` 判断。

超限转人工（S8）：持久运行返回 `ErrMaxSteps` 后，检查点停在 `Next == MaxSteps`，同一运行 ID 再 `Run` 仍报错。转人工或重新处理时使用新运行 ID，并为它创建新的根预算：Builder 要求根预算的 `RootKey` 与持久图的运行 ID 一致（否则报 `agent budget root conflict`），而且原根预算的用量不会因换运行 ID 而重置。

```go
runID := ticketID + "/human-1"
c, err := budget.NewDurable(budget.Config{RootKey: runID, Version: "v1", MaxModelRequests: 30}, budgetStore)
if err != nil {
    return err
}
snap, err := graph.Run(budget.WithController(ctx, c), runID, initial, "")
```

## 路由失败与兜底

持久图提交节点输出后，恢复时用同一份已提交状态重新执行路由。`Next`、`Until` 是纯函数，无效输入每次恢复都得到同样的错误，所以兜底由包装路由完成，而不是靠重试：

- `TextNext` 取最后一轮输出的第一行、去掉首尾空白，必须恰好等于某个成员名或 `FINISH`；记录为空、首行为空或不匹配时返回包装 `ErrInvalidRoute` 的错误，不做模糊匹配。用于文本路由的成员名必须是单行。
- 自定义 `Next`、`Until` 判定输入无效时也应包装 `multiagent.ErrInvalidRoute`。
- `GuardRoute` 依次：`Next` 返回包装 `ErrInvalidRoute` 的错误且配置了兜底时转兜底，其他错误原样返回；`FINISH` 映射为 `END`；不在成员集合中的名字（Supervisor 名、兜底名、`"__end__"`）按无效处理；否则返回成员名。
- 兜底 Step 执行后结束，它收到的状态里有无效的那次输出，可据此转人工。没有配置兜底时，`ErrInvalidRoute` 对该运行 ID 是终态，换运行 ID 与新根预算重新处理。
- 本包不做“重问一次”：需要时写进 Supervisor Step 自身。

## 文本接力层

`TextStep` 的默认输入是 Input 加每一轮的 `[Agent]: Output`，逐行拼接、不截断，Router 的 Supervisor 因此能看到原始任务，Loop 的写作者能看到上一稿与评审意见。输出取 `AgentOutput.Result`，字符串原样使用，其他类型按 JSON 编码，追加为一轮，空回答也记录。

- **增长**：单次请求随轮数线性增长，累计输入随轮数平方增长，检查点也反复序列化完整状态。轮数较多时用 `WithLastTurns(k)` 或 `WithRender`；需要摘要时把摘要做成单独的 Step。`WithRender` 与 `WithLastTurns` 互斥。
- **render 的确定性**：render 必须是 `Conversation` 的纯函数。模型请求意图包含完整请求，同一次节点访问恢复时 render 输出不同会报 `operation intent or attempt policy changed`。修改 render 或窗口大小等同于修改 Agent 行为，按“版本与迁移”处理。
- **内容隔离**：默认格式把客户输入与成员输出直接拼接，外部文本可以伪造轮次或诱导分流。高风险动作必须在执行前由人确认具体参数（S5），不能只靠分流判断；对外部输入敏感的流程用结构化 render（如 JSON 编码各轮）。
- **并行**：`ParallelConfig.Clone` 必须用 `CloneConversation`；`MergeTurns` 按分支登记顺序追加各分支新增的轮次，分支记录不以合并前的记录为前缀时报错。

## 持久接入契约

`multiagent` 不能检查 Agent 内部的配置，以下要求由宿主满足。

**Agent 侧**：

- 每个 Agent 用 `WithDurableScope` 设置稳定的非空 scope，日志键为“节点执行键 → `agent/<scope>` → 模型或工具操作键”。每个 Step 是独立节点，循环中每次访问的执行键包含步数，互不冲突。同一节点内第二次调用 Agent 必须换 scope。作用域规则见[原生编排：多 Agent 作用域与入口约束](NATIVE_ORCHESTRATION.md#多-agent-作用域与入口约束)。
- 带工具时，模型客户端必须实现 `llm.ToolStreamClient`，工具必须经 `Dispatcher.Tools()` 包装；否则在模型调用前失败。
- 会话由 `AgentFactory` 每次新建。记忆、检索结果、当前时间、随机示例、render 等任何非确定性内容在恢复时变化，都会使已完成请求的意图不一致而报错，不会静默回放。

**模型请求中断**：模型请求默认只允许一次尝试。恢复时按日志记录处理：

| 发生了什么 | 记录 | 默认 | 开启 `WithInterruptedModelResend` |
|---|---|---|---|
| 请求发出后、终态落盘前崩溃（或终态保存失败） | pending（`ambiguous`） | 拒绝 | 尝试次数 < 3 时重发 |
| 运行 ctx 被取消 | failed（`canceled`） | 拒绝 | 尝试次数 < 3 时重发 |
| 错误链含 `context.DeadlineExceeded` | failed（`deadline`） | 拒绝 | 拒绝 |
| 模型返回其他错误 | failed（`execution`） | 拒绝 | 拒绝 |

“拒绝”指节点以 `*durable.OperationError` 失败（`errors.Is(err, durable.ErrOperationAttention)`），同一运行 ID 无法继续。3 次是持久尝试次数，不是请求次数：每次尝试内 `WithRequestError` 还可原样重试，同一回合最多发出 `3 × (1 + maxRecoveries)` 个请求（`builder/model_resend_test.go` 的 `TestModelResendWithRequestRetries`）。模型客户端把超时包装成取消时，超时也会记为 canceled 并可能被重发。重发可能重复计费；开关在运行的整个生命周期内必须一致，否则恢复时报策略变更。deadline 与 execution 的分类见 `core/durable/operation_test.go`。

**预算**：

- `SupervisorTools` 的成员是模型工具，调用时 ctx 必须带根预算；持久运行必须是 `budget.NewDurable` 创建的持久预算。
- 持久模型请求的意图包含预算配置。恢复时必须复用原来的预算存储与完全相同的配置：复用存储但配置变化时 `budget.NewDurable` 报 `budget configuration or record changed`；换用新存储且配置也变时已完成的请求报意图不一致；换用新存储、配置相同时回放报 `usage has no reservation`；如果续跑时没有需要回放的请求，则完全拦不住，用量从零计数。预算存储必须与图存储一起持久化，恢复时总是复用。
- 成员 Agent 不得使用 Supervisor 或任何祖先调用的 Dispatcher，否则触发 `tools.ErrDispatcherReentry`；成员之间互不嵌套时可以共用一个。

## 人工等待

### 向客户追问（S6）

`AgentStep` 的 `in`、`out` 不接收 ctx，不能暂停。需要在 Agent 输出后向人要输入时写自定义 `Step.Run`：

```go
step := multiagent.Step[Case]{Name: "extract", Run: core.NewRunnableFunc(func(ctx context.Context, c Case) (Case, error) {
    fields, err := extractOnce(ctx, c) // Agent 使用 WithDurableScope("extract")，恢复时从日志回放
    if err != nil || fields.OrderID != "" {
        c.OrderID = fields.OrderID
        return c, err
    }
    raw, err := durable.AwaitInput(ctx, "order-id", json.RawMessage(`{"question":"请提供订单号"}`), validateOrderID)
    if err != nil {
        return c, err // 等待会让图暂停，Snapshot.Interrupts 给出中断键
    }
    return c, json.Unmarshal(raw, &c.OrderID)
})}
```

- 订单号的格式校验必须放进 validate。validate 拒绝时中断保持等待，`Resume` 返回 `*workflow.ResumeRejectedError`，宿主用同一中断键提交修正值；validate 接受的输入就此固定，之后提交不同的值报 `workflow.ErrInterruptAnswered`。
- validate 必须是输入的纯函数，它的错误文本会持久化并对外暴露，不得包含敏感信息。
- 依赖外部数据的核对（如订单是否存在）不能与 `AwaitInput` 放在同一节点，否则重入时外部数据变化会走另一条分支。核对做成下一个节点，结果写进状态，不通过时路由回追问节点；每次访问追问节点都是新的节点执行，中断互不冲突：

```go
g := workflow.NewGraph[Case](workflow.Config{MaxSteps: 1 + 2*maxAsks})
_ = multiagent.AddStep(g, askStep)   // 追问
_ = multiagent.AddStep(g, checkStep) // 核对订单是否存在，写入 c.OrderFound
_ = g.AddEdge(workflow.START, "ask")
_ = g.AddEdge("ask", "check")
_ = g.AddBranch("check", func(_ context.Context, c Case) (string, error) {
    if c.OrderFound {
        return workflow.END, nil
    }
    return "ask", nil
}, "ask", workflow.END)
```

### 登记后审核（S5）

`ApprovalNodes` 的审批发生在节点执行之前，只适合“是否进入某流程”这类粗粒度门禁。要审批 Agent 决定的具体动作（退款金额与订单），在节点内按“提议 → 登记 → 审核 → 执行”分段：

1. 调用退款 Agent 得到结构化提议，Agent 的工具只读。
2. `durable.Do(ctx, "refund/register", intent, policy, run)` 登记申请并计算争议判定。意图只含申请 ID 与提议，不含规则阈值或时间；登记与判定在同一事务内写入（经 `refund.Service.OnRegister` 钩子：仅在新插入申请时、同一事务内调用，出错整体回滚）；`Reconcile` 只有在申请与判定都存在时才补记完成，避免以零值“无争议”补记而绕过审核。判定由业务规则决定，Agent 只能把“无争议”升级为“有争议”。判定随操作记录固定，等待期间规则阈值或订单事实变化也不会翻转。
3. 有争议时 `durable.AwaitInput(ctx, "refund-review", info, validate)`。info 含订单号、金额、判定理由与解析审核人所需的提交人；输入为 `{"approved": bool, "note": string, "approver": string}`，`approver` 由宿主认证后填入，批准时必须非空。拒绝时不执行退款，把“人工拒绝”写回状态。
4. `durable.Do(ctx, "refund/execute", ...)` 的 run 先按申请 ID 查回执，已有回执直接返回；否则批准并退款。`refund.Service.Approve` 在查回执之前就校验有效期与订单事实，而 `Reconcile` 只在记录为 pending 时调用，所以 run 若先 `Approve`，退款已生效、记录为 failed 的运行会卡在批准失败上。审核期间订单事实变化时，把“订单已变化，需重新申请”作为业务结果写回状态，而不是节点错误。执行操作也提供按回执核对的 `Reconcile`。

不要把退款放进可中断工具：可中断工具只允许一次尝试且没有对账钩子，“退款已生效、记录未落盘”时崩溃会留下 pending，恢复被拒；续跑还会整体重跑工具，审核人已拒绝的申请可能被重新判为无争议直接退款。业务实现复用 `workflow/refund.Service`，审核人解析与转人工交接由[客服编排案例](https://github.com/kart-io/goagent-examples/blob/main/docs/superpowers/specs/2026-11-05-customer-service-design.md)定义。

### 等待时限与预算截止时间

每次预留模型请求都会检查根预算的 `DeadlineUnixMilli`，而预算配置在运行生命周期内不能改。有人工等待的流程，截止时间必须覆盖全部等待窗口（业务默认每次等待 48 小时，一个运行可能先追问再审核，设为全部等待时限之和加余量），或者不设截止时间，改用 `MaxModelRequests` 控制花费、由宿主实现运行 TTL：快照 `Waiting` 为真且超过时限即放弃，换运行 ID 转人工。按单次请求时长设截止时间时，客户隔几小时回复，续跑中的模型请求必然以 `context.DeadlineExceeded` 失败。超时由宿主以 `{"approved": false, "reason": "timeout"}` 回应中断，流程按拒绝分支结束。

其他中断规则：Agent 工具内的中断需要工具声明可中断（`WithInterruptible`），或是声明 `Resumable` 的 Runnable 工具；Agent 调用的子图如果是纯审批等待，在外层持久图里按节点失败处理。审批与中断的通用语义见[人工介入](HUMAN_IN_THE_LOOP.md)。

## SupervisorTools

每个成员包装为 `tools.NewRunnableToolFactory`：参数 Schema 固定为 `{"task": string}`；结果为 `AgentOutput.Result`（字符串原样，其他类型按 JSON 编码）；`Version` 必填，随成员的行为、提示词或工具集变化而提升；`Timeout` 只在经 Dispatcher 调用时生效，0 继承 Dispatcher 默认；`Resumable` 的前提与 `RunnableToolConfig.Resumable` 相同，成员内所有效果都记在派生键下，只能在带持久日志的调用中使用。

宿主把返回的工具注册到 Supervisor Agent 的 Dispatcher，再经 `Dispatcher.Tools()` 交给 Agent。参数不合法时持久运行默认拒绝整个模型回合；要把参数错误交还模型改正，在 Supervisor 的 Dispatcher 上开启 `ReturnArgumentErrors`。用量归属与成员身份见[多 Agent 用量](MULTI_AGENT_USAGE.md)。

## 并行分支的事件

两种编译方式都不转发并行分支内 Agent 的中间事件：普通 `Compile` 经分支函数直接执行，持久模式只发出 `branch_start`、`branch_end`，只有合并后的结果。分支名只需在本并行节点内唯一，不是图节点：`ApprovalNodes` 只能填 `ParallelStep` 的外层名，填分支名会被 `CompileDurable` 拒绝。

## 支持的组合

| 组合 | 是否支持 |
|---|---|
| `ParallelStep` 作为 Sequential、Loop、RouterSupervisor 的一步或 Router 成员 | 支持 |
| 普通 `Compile` 的图用 `AddRunnable` 嵌进另一个普通图 | 支持 |
| 持久图经 `AsChildRunnable` 包装成 `Resumable` 工具交给 Agent | 支持 |
| 构造器之间嵌套（如 Sequential 的一步是 Loop） | 不提供，手写图 |
| 已编译的图作为节点放进持久图 | 不支持 |
| `ParallelStep` 的分支再是 `ParallelStep` | 不支持，构造报错 |

“抽取 → 写作⇄评审循环 → 合规”这类组合用 `workflow.NewGraph` 手写拓扑，用 `AddStep` 加入本包的 Step，用 `GuardRoute` 包装自己的路由以复用兜底，并自行设置 `MaxSteps`。循环后面还有节点时，总步数不能约束中间的循环（第 N 轮评审不通过后，留给合规的那一步会被第 N+1 次写作用掉），所以评审出口路由从状态推出轮数：

```go
const reviewRounds = 3 // MaxSteps = 1 + 2*reviewRounds + 1

reviewExit := func(ctx context.Context, c multiagent.Conversation) (string, error) {
    passed, err := parseVerdict(c) // 无法解析时返回包装 multiagent.ErrInvalidRoute 的错误
    if err != nil {
        return "", err
    }
    if passed {
        return "compliance", nil
    }
    rounds := 0
    for _, t := range c.Turns {
        if t.Agent == "review" {
            rounds++
        }
    }
    if rounds >= reviewRounds {
        return "", fmt.Errorf("%w: review loop limit %d", workflow.ErrMaxSteps, reviewRounds)
    }
    return "write", nil
}
g := workflow.NewGraph[multiagent.Conversation](workflow.Config{MaxSteps: 1 + 2*reviewRounds + 1})
// AddStep 加入 extract、write、review、compliance 与兜底 human，连边后：
route, targets, err := multiagent.GuardRoute(reviewExit, []string{"write", "compliance"}, "human")
if err != nil {
    return err
}
err = g.AddBranch("review", route, targets...)
```

`ErrInvalidRoute` 转兜底，`ErrMaxSteps` 原样返回；持久运行停在评审节点的路由位置（`Routing` 为真），同一运行 ID 再 `Run` 仍报错。因为合规节点留有一步，第 N 轮判断无效时兜底能执行，这与 Loop 构造器“末轮无效返回 `ErrMaxSteps`”不同。路由闭包里的 N 不进计划摘要，只有由 N 算出的 `MaxSteps` 进入，所以用同一个常量计算两者。

## 版本与迁移

持久图的计划摘要包含 `DurableConfig.Version` 与整个 `workflow.Config`（含 `MaxSteps`、`MaxConcurrency`），恢复时同时校验。修改 `MaxIterations`、`MaxRounds`、`MaxConcurrency`、步骤列表或审批节点后，旧运行不能交给新图恢复，提升 `Version` 也不行。宿主保留旧版本的构造参数编译旧图，处理未完成的旧运行；新版本只接收新运行。检查点自带版本，按版本分流：

```go
graphs := map[string]*workflow.DurableGraph[multiagent.Conversation]{
    "v1": mustCompile(buildRouter(3), "v1"), // 旧参数：MaxRounds 3
    "v2": mustCompile(buildRouter(5), "v2"),
}
cp, err := store.Load(ctx, runID)
switch {
case errors.Is(err, os.ErrNotExist):
    return graphs["v2"].Run(ctx, runID, initial, "")
case err != nil:
    return nil, err
}
graph, ok := graphs[cp.Version]
if !ok {
    return nil, fmt.Errorf("运行 %s 的版本 %s 已下线", runID, cp.Version)
}
return graph.Run(ctx, runID, initial, approvalKey)
```

## 运营监控

从节点事件统计：Fallback 节点（如 `human`）的执行率、`workflow.ErrMaxSteps` 与 `multiagent.ErrInvalidRoute` 的发生率。用量超过阈值的提示用 `budget.WithUsageHook`，事件含累计统计；`MaxModelRequests` 必须 ≥ 1，设为宽松的防失控上限，触达时与轮数超限一样转人工。
````

- [ ] **Step 2: 修改索引、架构、对照、定位文档与 CHANGELOG**

以下每处原文在文件中恰好出现一次，按原文定位替换：

`README.md`：把

````markdown
- [Composable orchestration](docs/guides/COMPOSABLE_ORCHESTRATION.md)
````

替换为

````markdown
- [Multi-agent patterns](docs/guides/MULTI_AGENT_PATTERNS.md) - `multiagent`: Sequential, Loop, RouterSupervisor, `ParallelStep`, `SupervisorTools` and the text relay layer on top of `workflow`; offline example: `go run ./examples/multi-agent-patterns`.
- [Composable orchestration](docs/guides/COMPOSABLE_ORCHESTRATION.md)
````

`README_CN.md`：把

````markdown
- [可组合编排](docs/guides/COMPOSABLE_ORCHESTRATION.md)
````

替换为

````markdown
- [多 Agent 预置模式](docs/guides/MULTI_AGENT_PATTERNS.md)：`multiagent` 包在 `workflow` 之上提供 Sequential、Loop、RouterSupervisor、`ParallelStep`、`SupervisorTools` 与文本接力层；离线示例：`go run ./examples/multi-agent-patterns`。
- [可组合编排](docs/guides/COMPOSABLE_ORCHESTRATION.md)
````

`docs/guides/AGENT_COMPOSITION.md`：把

````markdown
| 需要人工审批与恢复 |
````

替换为

````markdown
| 多个 Agent 按顺序、循环或分流协作 | 使用 `multiagent` 的 Sequential、Loop、RouterSupervisor，见[多 Agent 预置模式](MULTI_AGENT_PATTERNS.md) |
| 需要人工审批与恢复 |
````

`examples/README.md`：把

````markdown
| [human-in-the-loop-interrupt/](human-in-the-loop-interrupt/) |
````

替换为

````markdown
| [multi-agent-patterns/](multi-agent-patterns/) | `multiagent` presets (router, loop, parallel lookup, refund review, supervisor tools, fallback, composed graph) driven by `mocks.ScriptedClient`, offline |
| [human-in-the-loop-interrupt/](human-in-the-loop-interrupt/) |
````

`docs/architecture/ARCHITECTURE.md`：把

````markdown
    workflow --> core
    workflow --> durable
    workflow --> observe
````

替换为

````markdown
    workflow --> core
    workflow --> durable
    workflow --> observe

    multiagent --> workflow
    multiagent --> builder
    multiagent --> tools
    multiagent --> core
````

`docs/architecture/ARCHITECTURE.md`：把

````markdown
| `workflow/refund` |
````

替换为

````markdown
| `multiagent` | 多 Agent 预置模式：`Sequential`、`Loop`、`RouterSupervisor` 构造器，`ParallelStep`、`AddStep`、`GuardRoute`，`SupervisorTools` 与文本接力层（`Conversation`、`TextStep`、`TextNext`） |
| `workflow/refund` |
````

`docs/research/2026-11-04-eino-comparison.md`：把

````markdown
| 无预置；`tools.NewRunnableToolFactory` 把子 Agent/持久图包装成工具，或用 `workflow.Graph` |
````

替换为

````markdown
| `multiagent` 预置 `Sequential`、`Loop`、`RouterSupervisor` 图构造器与 `ParallelStep`，`SupervisorTools` 把成员 Agent 包成工具；构造器返回 `workflow.Graph`，持久语义随 `CompileDurable` 继承。无 Deep、PlanExecute 与转交（transfer） |
````

`docs/research/2026-11-04-eino-comparison.md`：把

````markdown
| Sequential/Parallel/Loop/Supervisor/Deep/PlanExecute 预置模式 | 不进核心；如需要归 goagent-harness | 定位 §4.2 删除 `multiagent`；[Harness 迁移指南](../guides/HARNESS_MIGRATION.md) |
````

替换为

````markdown
| Sequential/Parallel/Loop/Supervisor 预置模式 | 已进核心：新 `multiagent` 包只组合 `workflow`、`builder`、`tools`，不恢复旧包的分布式投递；Parallel 由只含一个 `ParallelStep` 的 Sequential 表达 | [多 Agent 预置模式设计](../superpowers/specs/2026-11-05-multiagent-patterns-design.md)（内部客服服务的业务证据）；定位 §5 第 6 项 |
| Deep/PlanExecute 预置与 Agent 转交 | 不做 | 无业务证据（定位 §5） |
````

`docs/research/2026-11-04-eino-comparison.md`：把

````markdown
本轮对照没有发现需要进入核心的能力缺口。上一版差距中的“任意节点与工具中途中断、恢复时带数据”已由动态中断（`durable.AwaitInput`）补齐。
````

替换为

````markdown
本轮对照中唯一进入核心的是多 Agent 预置模式，依据是内部客服服务的接入需求，由 `multiagent` 包补齐；其余差距没有业务证据，不进核心。上一版差距中的“任意节点与工具中途中断、恢复时带数据”已由动态中断（`durable.AwaitInput`）补齐。
````

`docs/research/2026-10-29-goagent-positioning.md`：把

````markdown
5. **持久摘要**：后置。
````

替换为

````markdown
5. **持久摘要**：后置。
6. **多 Agent 预置模式**（已完成）：内部客服服务计划接入，业务方提出工单分流与回复评审两个流程，并按其要求补充其余客服流程（[设计](../superpowers/specs/2026-11-05-multiagent-patterns-design.md)）。实现：新 `multiagent` 包只组合 `workflow`、`builder`、`tools`，与 §4.2 删除的旧包同名但不兼容，不恢复 Core NATS 投递。
````

`CHANGELOG.md`：把

````markdown
### Added

- `workflow/refund.Service` 新增可选 `OnRegister` 钩子
````

替换为

````markdown
### Added

- 新增 `multiagent` 包：`Sequential`、`Loop`、`RouterSupervisor` 图构造器返回未编译的 `workflow.Graph`，由宿主选择 `Compile` 或 `CompileDurable`；`ParallelStep`、`AddStep`、`GuardRoute` 供手写组合流程复用；`SupervisorTools` 把成员 Agent 包装为模型工具；文本接力层 `Conversation`、`TextStep`、`TextNext`、`CloneConversation`、`MergeTurns`。路由无效时可转入 `Fallback`，错误满足 `errors.Is(err, multiagent.ErrInvalidRoute)`。与 v0.17 删除的旧 `multiagent` 同名，但 API 与语义不兼容，旧代码不能直接迁移。见[多 Agent 预置模式](docs/guides/MULTI_AGENT_PATTERNS.md)与离线示例 `examples/multi-agent-patterns`。
- 新增 `testing/mocks.ScriptedClient`：实现 `llm.ToolStreamClient` 的脚本化模型替身，按顺序匹配“请求断言 → 回复”，可回复结构化工具调用，收到额外请求时报错，并提供深拷贝的请求历史与按规则的匹配次数。
- `scripts/apicheck` 新增规则：基础与执行机制包禁止导入 `multiagent`，`multiagent` 禁止导入 `execution` 与 `plugin`。
- `workflow/refund.Service` 新增可选 `OnRegister` 钩子
````
- [ ] **Step 3: 检查文档链接**

Run: `python3 scripts/verify_doc_examples.py docs/guides/MULTI_AGENT_PATTERNS.md examples/multi-agent-patterns/README.md README.md README_CN.md docs/guides/AGENT_COMPOSITION.md examples/README.md docs/architecture/ARCHITECTURE.md docs/research/2026-11-04-eino-comparison.md docs/research/2026-10-29-goagent-positioning.md && python3 scripts/verify_doc_examples.py`
Expected: 两次都输出 `问题0项`

- [ ] **Step 4: 提交**

```bash
git add docs/guides/MULTI_AGENT_PATTERNS.md examples/multi-agent-patterns/README.md README.md README_CN.md docs/guides/AGENT_COMPOSITION.md examples/README.md docs/architecture/ARCHITECTURE.md docs/research/2026-11-04-eino-comparison.md docs/research/2026-10-29-goagent-positioning.md CHANGELOG.md
git commit -m "docs: 新增多 Agent 预置模式指南并同步索引、架构与能力对照"
```


### Task 17: 全量验证

**Files:**
- 无新增文件（发现问题时修复并单独提交）

**Interfaces:**
- Consumes: Task 1–16 的全部改动
- Produces: 可交付的分支状态

- [ ] **Step 1: 全量测试**

Run: `go test ./...`
Expected: 全部 PASS（副本中为 125 个包）

- [ ] **Step 2: 新包竞态测试**

Run: `go test -race -count=3 ./multiagent/... ./testing/mocks/... ./examples/multi-agent-patterns/... ./scripts/apicheck/... ./workflow/refund/...`
Expected: PASS，无 DATA RACE

- [ ] **Step 3: vet**

Run: `go vet ./...`
Expected: 无输出

- [ ] **Step 4: lint**

Run: `make lint`
Expected: golangci-lint v2.15.0 `0 issues.`（构建版本须不低于 go.mod 的 go 指令，用官方二进制）

- [ ] **Step 5: 导入检查**

Run: `bash scripts/verify_imports.sh && go test ./scripts/apicheck/`
Expected: `imports: 0 findings`；测试 PASS

- [ ] **Step 6: 文档链接**

Run: `python3 scripts/verify_doc_examples.py`
Expected: `问题0项`

- [ ] **Step 7: 确认工作区**

Run: `git status --short`
Expected: 只剩未跟踪的 `.context/`；没有未提交的计划内改动


## 规格覆盖对照

| 规格章节 | 任务 |
|---|---|
| 目标、包与依赖、旧包同名说明 | Global Constraints；Task 2（`doc.go`）；Task 10；Task 16（CHANGELOG） |
| 与既有决策的关系（Eino 第 49/73/78 行、定位 §5） | Task 16 |
| 底层 API：Step、AgentFactory、AgentStep | Task 2 |
| AgentStep 的执行（生命周期、Adapt、S6 写法与 scope） | Task 2；Task 12（S6）；Task 16 |
| ParallelStep 的表示 | Task 4；Task 7（以外层名审批） |
| 图拓扑与 MaxSteps、溢出、`cfg.MaxSteps` | Task 5、6、7 |
| 轮数上限语义、ErrMaxSteps 检查点位置 | Task 6、7；Task 12（S8、组合） |
| 版本与迁移 | Task 11（`TestDurableVersionMigration`）；Task 16 |
| 返回的图只用于编译 | Task 2（`doc.go`）；Task 16 |
| 路由失败与兜底、GuardRoute | Task 3、6、7；Task 11；Task 12（S9） |
| 路由、合并与映射必须是纯函数 | Task 2–8 的注释；Task 16 |
| 组合流程 | Task 12（`TestScenarioComposedPipeline`）；Task 15；Task 16 |
| 便捷层：文本接力 | Task 8；Task 11（render） |
| SupervisorTools 的工具协议 | Task 9 |
| 持久接入契约（Agent 侧、预算与 Dispatcher、状态 T、支持的组合、审批与中断） | Task 9、11、12、14；Task 16 |
| S5 修订（先查回执、登记与判定同事务经 `OnRegister`、意图限定、approver 与 reason） | Task 13；Task 14；Task 15；Task 16 |
| 校验规则表 | Task 2–9 各校验测试 |
| 测试替身 | Task 1 |
| 单元测试 | Task 2–9 |
| 持久恢复测试 | Task 11 |
| SupervisorTools 测试 | Task 9 |
| 场景覆盖 S1–S9 与组合 | Task 12、14 |
| 离线示例与其他 | Task 15；Task 10（apicheck 回归测试） |
| 文档 | Task 16 |
| 不做、相关问题 | 不实现；不修改 `scripts/verify_all_examples.sh` |
