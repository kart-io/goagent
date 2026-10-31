# 插件

Agent 能力包见第一节；`core` 的 Runnable 注册表见最后一节。

## Agent 能力包

插件把工具、提示词片段、循环钩子、中间件、回调与进程级资源打包，经 `plugin.Install` 装进 Agent。`builder` 不依赖 `plugin`；goagent-harness 的 preset 组件可以直接包装插件。

```go
type Orders struct{ cfg OrdersConfig }

func (*Orders) Name() string    { return "orders" }
func (*Orders) Version() string { return "1.0.0" }
func (o *Orders) Contribute(ctx context.Context, env *plugin.Env) (plugin.Contribution, error) {
    db, err := plugin.Lookup[*sql.DB](env.Resources(), "database.primary")
    if err != nil { return plugin.Contribution{}, err }
    return plugin.Contribution{
        Tools:          []interfaces.Tool{newLookupOrder(db)},
        PromptSections: []builder.PromptSection{{Name: "orders", Text: func(builder.PromptScope) string { return "退款前先查询订单" }}},
    }, nil
}
func (*Orders) Requires() []string { return []string{"database"} }

host, err := plugin.NewHost(database, &Orders{cfg: cfg}, redact)
if err != nil { return err }
if err := host.Start(ctx); err != nil { return err }
defer host.Stop(context.Background())

b := builder.NewSimpleBuilder(client).WithDurableScope("support")
if err := plugin.Install(ctx, b, host, plugin.Dispatch(tools.DispatchConfig{MaxConcurrency: 4})); err != nil { return err }
agent, err := b.Build()
```

规则（以代码为准）：

- 插件名在 Host 内唯一：非空、无首尾空白、不含 `/` 与 `@`；版本非空、无首尾空白、不含 `/`。`NewHost` 一次报告名称、版本、重名与依赖未注册的全部问题；这些都通过后再检查循环依赖。
- `Host.Start` 按依赖拓扑顺序（同层保持注册顺序）调用 `Starter.Start`；任一失败或 ctx 结束时逆序停止已启动的插件，失败的插件自身不调用 `Stop`。Host 不可重启，`Stop` 逆序停止并合并错误，重复调用返回 nil。
- 资源只能在本插件 `Start` 中经 `env.Provide("<插件名>.<名称>", v)` 发布；其他插件用 `plugin.Lookup[T]` 取出。Host 停止后资源清空，须先停用 Agent 再停止 Host。
- `Install` 要求 Host 已启动；先调用所选插件的 `Contribute` 并校验（工具、提示词片段、提示词上下文、中间件重名，PreStep 缺少 Identity，nil 钩子），全部通过后才写入 Builder。`Only(names...)` 选择插件；`Dispatch(cfg)` 把插件工具统一经一个 Dispatcher 包装，持久运行必须使用。
- 插件 PreStep 以 `plugin/<名称>@<版本>/<Identity>` 进入持久日志，升级版本后恢复旧运行以 `operation intent or attempt policy changed` 失败。
- 所有插件的 `RequestErrors` 合并后注册一次，恢复次数取各插件 `MaxRecoveries` 的最大值；宿主之后再调用 `WithRequestError` 会覆盖它。
- 同一个 Builder 只应 Install 一次。

案例见 goagent-examples 的 `orchestration/plugin/`。

## Runnable 注册表

`core` 另提供进程内的 Runnable 注册表：把泛型 `core.Runnable[I, O]` 包装为动态类型的 `core.DynamicRunnable`，按名称注册、查找。它是独立的组件容器，**不与 Builder 或 Dispatcher 集成**：注册表里的条目是 `DynamicRunnable`，不是模型可调用的 `interfaces.Tool`。给 Agent 提供工具用 `WithTools` 或上面的能力包（见下文 MCP 一节与[原生编排](NATIVE_ORCHESTRATION.md)）。

| 类型 | 职责 |
|---|---|
| `TypedToDynamicAdapter[I, O]` / `DynamicToTypedAdapter[I, O]` | 泛型与动态调用互转；输入先直接断言，失败时按结构解码；`TypedToDynamicAdapter` 的调用带 panic 保护 |
| `PluginRegistry` | 名称 → `DynamicRunnable` 与 `PluginMetadata`；`RegisterTyped`/`GetTyped` 免手写适配器 |

进程级单例：`GlobalPluginRegistry()`。

### 使用

```go
reg := core.NewPluginRegistry()
length := core.NewRunnableFunc(func(ctx context.Context, s string) (int, error) { return len(s), nil })
if err := core.RegisterTyped(reg, "length", length, &core.PluginMetadata{Version: "1.0.0"}); err != nil {
    return err
}
fn, err := core.GetTyped[string, int](reg, "length")
if err != nil { return err }
n, err := fn.Invoke(ctx, "hello")
```

`Get` 返回 `DynamicRunnable`（`InvokeDynamic`/`StreamDynamic`/`BatchDynamic`/`TypeInfo`）；另有 `Unregister`、`List`、`GetMetadata`。

## 接入 MCP 服务器

MCP 服务器的工具不需要包装为插件。`mcp/remote` 基于官方 SDK 建立会话，`remote.Tools` 返回的 `interfaces.Tool` 可直接传给 Builder：

```go
import (
    "github.com/kart-io/goagent/mcp/remote"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

session, err := remote.Connect(ctx, &mcp.CommandTransport{Command: exec.Command("./my-mcp-server")})
if err != nil {
    return err
}
defer session.Close()

mcpTools, err := remote.Tools(ctx, session)
if err != nil {
    return err
}
agent, err := builder.NewSimpleBuilder(client).WithTools(mcpTools...).Build()
```

HTTP 传输与行为说明见[官方 MCP 客户端适配](PRIORITY_CAPABILITIES.md#官方-mcp-客户端适配)。
