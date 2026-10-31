<!-- generated-by: gsd-doc-writer -->
# GoAgent Tool API 参考

模型可见工具和持久调用推荐通过 `tools.Dispatcher`。普通 `Tool.Invoke` 只是一次函数调用，不自动提供校验、超时、审批、预算或恢复。

源码：[接口](../../interfaces/tool.go)、[基础工具](../../tools/tool.go)、[函数工具](../../tools/function_tool.go)、[Registry](../../tools/registry.go)、[Dispatcher](../../tools/dispatcher.go)、[Pipeline](../../tools/pipeline.go)、[Runnable 工具](../../tools/runnable_tool.go)。

## 接口与数据

```go
type Tool interface {
    Name() string
    Description() string
    Invoke(context.Context, *ToolInput) (*ToolOutput, error)
    ArgsSchema() string
}
```

名称在注册表中唯一且不能有首尾空白。ArgsSchema 是 JSON Schema 字符串；Dispatcher 构造时解析并固定工具定义。

| ToolInput 字段 | 类型 | 含义 |
| --- | --- | --- |
| CallID | string | 当前父执行下稳定的子操作 ID；同一节点多个效果须不同 |
| ExecutionKey | string | 宿主执行身份，不能从模型 Args 取值 |
| Args | map[string]interface{} | JSON 对象参数 |
| Context | context.Context | 不序列化的调用上下文 |
| CallerID、TraceID | string | 可选调用者与追踪标识 |

Dispatcher 从宿主 context 绑定执行键，并在提供 CallID 时派生工具子键。外部服务须按该键或更强业务 ID 去重，字段本身不保证幂等。

| ToolOutput 字段 | 类型 | 含义 |
| --- | --- | --- |
| Result | interface{} | 业务结果；持久调用须可 JSON 编码 |
| Success | bool | 是否成功 |
| Error | string | 工具错误文本 |
| Metadata | map[string]interface{} | 结果元数据 |
| AdditionalContext | []string | Builder 在本批工具结果后作为 user 消息追加的上下文 |
| Labels | map[string]string | 预留标签，目前不会自动传播 |

`ToolResult` 是旧 ToolExecutor 接口结果信封（ToolName、Output、ExecutionTime），不同于 Dispatcher 的 DispatchResult。

可选接口位于 [interfaces/tool.go](../../interfaces/tool.go)：

- `ValidatableTool.Validate(ctx, input) error`：自定义参数验证；Dispatcher 先验证 Schema，再调用它，并在成功后重新检查 Schema。
- `DurableCallTool.SupportsDurableCalls() bool`：声明保留 CallID、宿主日志和派生执行键。
- `ArgumentErrorTool.ReturnsArgumentErrors() bool`：声明参数错误可以结果形式交还模型。
- `ConcurrentCallTool.SupportsConcurrentCalls() bool`：声明同轮可并发且自行限流；Builder 仍须配置 WithParallelToolCalls(n>=2)。

Dispatcher.Tools 返回的包装工具实现后三项契约。自行包装时须显式保留需要的能力声明。

## 创建工具

```go
func NewBaseTool(
    name, description, argsSchema string,
    runFunc func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error),
) *BaseTool

func NewFunctionTool(
    name, description, argsSchema string,
    fn func(context.Context, map[string]interface{}) (interface{}, error),
) *FunctionTool
```

两种基础构造不会自动执行 Schema 校验。`NewFunctionToolBuilder(name)` 支持 WithDescription、WithArgsSchema、WithArgsSchemaFromStruct、WithFunction；Build 返回 (*FunctionTool,error)，MustBuild 失败则 panic。需要严格参数边界时，显式提供 Schema。

### 无网络完整示例

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/tools"
)

func main() {
    if err := run(); err != nil {
        panic(err)
    }
}

func run() error {
    tool := tools.NewBaseTool("echo", "返回输入文本",
        `{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`,
        func(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
            if err := ctx.Err(); err != nil {
                return nil, err
            }
            return &interfaces.ToolOutput{Success: true, Result: in.Args["text"]}, nil
        })
    registry := tools.NewRegistry()
    if err := registry.Register(tool); err != nil {
        return err
    }
    dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{
        MaxConcurrency: 1,
        Timeout: 2 * time.Second,
    })
    if err != nil {
        return err
    }
    out, err := dispatcher.Invoke(context.Background(), tools.ToolRequest{
        Name: "echo",
        Input: &interfaces.ToolInput{Args: map[string]interface{}{"text": "你好"}},
    })
    if err != nil {
        return err
    }
    fmt.Println(out.Result)
    return nil
}
```

预期输出：你好。Builder 使用 `WithTools(dispatcher.Tools()...)`，而不是注册表中的裸工具。

## 注册与执行

```go
func NewRegistry() *Registry
func (r *Registry) Register(tool interfaces.Tool) error
func (r *Registry) Get(name string) interfaces.Tool
func (r *Registry) List() []interfaces.Tool
func (r *Registry) Names() []string
func (r *Registry) Clear()
func (r *Registry) Size() int

func NewDispatcher(registry *Registry, cfg DispatchConfig) (*Dispatcher, error)
func (d *Dispatcher) Invoke(ctx context.Context, req ToolRequest) (*interfaces.ToolOutput, error)
func (d *Dispatcher) Tools() []interfaces.Tool
func (d *Dispatcher) AsRunnable() *core.StreamRunnable[ToolRequest, *interfaces.ToolOutput]
```

Get 未找到返回 nil；List/Names 不保证顺序。Dispatcher 快照 Registry；后续注册变更不影响既有 Dispatcher。底层工具须能承受配置并发，否则设 MaxConcurrency=1。ToolRequest 包含 Name 和 Input。

| DispatchConfig | 默认值与作用 |
| --- | --- |
| MaxConcurrency | 4；执行并发，审批等待不占此槽 |
| MaxAdmissionConcurrency | 0；零值不限准入并发，正值单独限制校验和管线准入 |
| MaxQueued | 0；零值不限排队，正值限制两阶段合计等待，超限返回 ErrDispatcherBusy |
| Timeout | 30秒；校验、宿主检查、日志与执行累计计时，准入/审批和排队暂停单次计时 |
| MaxAttempts | 3；持久操作的显式恢复上限，不是一次 Invoke 中自动重试 |
| ReturnArgumentErrors | false；只影响 Tools() 模型调用路径 |
| Authorize | 宿主检查，可能收到不合 Schema 的参数；error 中止 |
| OnResult | 首次与完成回放均调用，宿主应据稳定 ExecutionKey 幂等重建状态 |
| Pipeline | 可选调用管线 |

实现 DispatchTimeoutProvider 的工具可用正值覆盖默认单次超时，零值沿用默认；直接 Tool.Invoke 仍只受宿主 context 约束。宿主及根预算期限始终有效。OnResult 不受工具单次 Timeout 限制，应合作处理宿主 context。框架不能强杀忽略 context 的同步工具。

## 参数错误、拒绝与恢复

- error 非 nil 表示调用未正常完成，例如网络中断、取消或内部失败。
- `tools.Rejected(message, data)` 是明确业务拒绝，作为结果交给模型；持久日志记为 completed，恢复只回放。用 RejectedResult 判断，自行构造同形状 map 不会被识别为拒绝。
- ReturnArgumentErrors=true 时，Tools() 把 Schema 错误或本层 *tools.ArgumentError 转为 Success=false、Error为空的结果，让模型修正；不转换授权、超时、取消和子流程执行错误。
- 直接 Dispatcher.Invoke/AsRunnable 不启用上述参数结果恢复，仍返回 error。参数修复不等于向用户自动澄清。

持久调用要求宿主日志、执行键、稳定 CallID 和支持持久调用的入口。completed 回放不重新调用工具，也跳过 Pipeline 准入；Schema/宿主检查与 OnResult 有各自调用契约。首次与回放均经过 JSON 边界，数字解码为 json.Number，不能依赖原 Go 指针类型。

pending/failed 不证明外部效果未发生。服务须实现幂等，或由 ReconcilableTool.Reconcile 核对效果；框架不承诺任意工具 exactly-once。见 [执行治理](../guides/EXECUTION_GOVERNANCE.md)和[拒绝/核对源码](../../tools/rejected.go)。

## Pipeline

Pipeline 是工具调用扩展点，不是 Agent 外围中间件：

```text
Schema/自定义验证与宿主检查 → PreHook → 审批 → Guard → Around → 工具 → PostHook
```

- Pre：[]PreHook，返回 PreDecision；优先级 Stop > Deny > Substitute > Ask > Allow。
- Approver：Approve(ctx, ApprovalRequest) (ApprovalResult,error)；ApprovalTimeout 默认10分钟。
- Guards：[]Guard，非空原因即拒绝；人的批准不能覆盖 Guard。
- Around：[]Around，包装执行；Post：[]PostHook，处理成功及 Rejected，可替换输出、追加上下文。
- GrantTTL：会话批准有效期，非正值不设过期；Grants、RevokeGrants、PruneGrants 查询和回收范围。

参数非法不进入 Pipeline。Pre/审批/Guard 在操作日志之外；准入错误不留下 pending，明确拒绝结果会写日志。completed 回放跳过准入。Pipeline 审批不等价于可恢复工作流暂停。

## Runnable、Workflow 与子 Agent

```go
func NewRunnableTool[I, O any](
    run core.Runnable[I, O], cfg RunnableToolConfig[I, O],
) (interfaces.Tool, error)

type OwnedRunnable[I, O any] struct {
    Runnable core.Runnable[I, O]
    Close func(context.Context) error
}

func NewRunnableToolFactory[I, O any](
    factory func(context.Context) (OwnedRunnable[I, O], error),
    cfg RunnableToolConfig[I, O],
) (interfaces.Tool, error)
```

Config 必填 Name、Version、对象 ArgsSchema、Decode、Encode，Description 描述业务用途。共享无状态 Runnable 用 NewRunnableTool；有状态子 Agent 用工厂，宿主分配独立会话、记忆命名空间及 Dispatcher。

| Config 字段 | 契约 |
| --- | --- |
| ModelInvoking | 模型型工具设 true；缺根预算时在工厂前拒绝，不自动检测隐藏模型请求 |
| MaxDepth | 默认8，祖先更小限制继续生效 |
| CleanupTimeout | 默认5秒，Close 必填（可为 no-op），成功、失败和取消均清理 |
| Timeout | 经 Dispatcher 的单次超时声明；直接调用受宿主 context 约束 |
| Resumable | 默认 false；开启须经带日志 Dispatcher，子步骤在同一 Store 和稳定派生键下记录 |

默认不透明复合工具只尝试一次：completed 可回放，未完成不能假定自动进入子检查点。嵌套审批/恢复显式使用 Resumable 与 DurableGraph.AsChildRunnable；等待经 WaitError 传播，不能 Encode 为成功。

工厂部分失败仍应提供已取得资源的 Close。清理 context 不继承父取消，受 CleanupTimeout 约束；不合作的清理不能强制终止，清理失败阻止工具完成。见 [编排指南](../guides/COMPOSABLE_ORCHESTRATION.md)。

## 其他入口与工具位置

`NewToolExecutor(opts ...ExecutorOption) *ToolExecutor` 仍提供 ExecuteParallel、ExecuteSequential、ExecuteWithDependencies 和 ExecuteBatch，见 [executor_tool.go](../../tools/executor_tool.go)。批量/依赖执行不自动获得 Dispatcher.Tools 的持久模型调用契约。

| 核心子包 | 当前构造器 |
| --- | --- |
| tools/compute | NewCalculatorTool() *CalculatorTool |
| tools/http | NewAPITool(baseURL string, timeout time.Duration, headers map[string]string) *APITool |
| tools/search | NewSearchTool(engine SearchEngine) *SearchTool |
| tools/shell | NewShellTool(allowedCommands []string, timeout time.Duration) *ShellTool |

编码工具已移至 github.com/kart-io/goagent-harness/tools/coding，不再从核心导入。参见 [Harness 迁移](../guides/HARNESS_MIGRATION.md)。外部模块、示例层业务集成与核心工具应分别说明。
