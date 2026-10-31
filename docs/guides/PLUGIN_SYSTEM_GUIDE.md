# 插件注册中心

`core` 包提供进程内的插件注册中心：把泛型 `core.Runnable[I, O]` 包装为动态类型的 `core.DynamicRunnable`，按名称（和版本）注册、查找，并统一管理生命周期。它是独立的组件容器，**不与 Builder 或 Dispatcher 集成**：注册中心里的工具是 `DynamicRunnable`，不是模型可调用的 `interfaces.Tool`。给 Agent 提供工具用 `WithTools`（见下文 MCP 一节与[原生编排](NATIVE_ORCHESTRATION.md)）。

| 类型 | 职责 |
|---|---|
| `TypedToDynamicAdapter[I, O]` / `DynamicToTypedAdapter[I, O]` | 泛型与动态调用互转；输入先直接断言，失败时按结构解码；`TypedToDynamicAdapter` 的调用带 panic 保护 |
| `PluginRegistry` | 名称 → `DynamicRunnable` 与 `PluginMetadata`；`RegisterTyped`/`GetTyped` 免手写适配器 |
| `EnhancedPluginRegistry` | 名称 → 版本 → `Plugin`，附带 `Container` 与 `DefaultLifecycleManager` |
| `Plugin` / `BasePlugin` | `interfaces.Lifecycle` 加 `Name`、`Version`、`GetTools`、`GetAgents`、`GetMiddleware` |
| `Container` | 按名称共享实例或延迟工厂 |

两个注册中心各有进程级单例：`GlobalPluginRegistry()`、`GlobalEnhancedPluginRegistry()`。

## 简单注册中心

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

## 带版本与生命周期的插件

```go
type SearchPlugin struct {
    *core.BasePlugin
    logger Logger
    apiKey string
}

// 插件不会自动拿到 Container，由宿主在构造时传入。
func NewSearchPlugin(container *core.Container) (*SearchPlugin, error) {
    logger, err := core.ResolveTyped[Logger](container, "logger")
    if err != nil { return nil, err }
    p := &SearchPlugin{BasePlugin: core.NewBasePlugin("search", "1.0.0"), logger: logger}
    search := core.NewRunnableFunc(func(ctx context.Context, query string) (string, error) {
        return "results for " + query, nil
    })
    p.AddTool(core.NewTypedToDynamicAdapter[string, string](search, "search"))
    return p, nil
}

// config 是 PluginConfig.Config。
func (p *SearchPlugin) Init(ctx context.Context, config interface{}) error {
    if err := p.BasePlugin.Init(ctx, config); err != nil { return err }
    if cfg, ok := config.(map[string]interface{}); ok {
        p.apiKey, _ = cfg["api_key"].(string)
    }
    return nil
}
```

```go
registry := core.NewEnhancedPluginRegistry()
registry.Container().Register("logger", logger)

plugin, err := NewSearchPlugin(registry.Container())
if err != nil { return err }
if err := registry.RegisterPlugin(plugin, &core.PluginConfig{
    Config: map[string]interface{}{"api_key": "..."},
}); err != nil {
    return err
}
if err := registry.InitializeAll(ctx); err != nil { return err }
if err := registry.StartAll(ctx); err != nil { return err }
defer registry.StopAll(ctx)

for key, tools := range registry.GetAllTools() { // key 形如 "search@1.0.0"
    for _, tool := range tools {
        out, err := tool.InvokeDynamic(ctx, "退款政策")
        fmt.Println(key, tool.TypeInfo().Name, out, err)
    }
}
for name, status := range registry.Lifecycle().HealthCheckAll(ctx) {
    fmt.Println(name, status.State)
}
```

行为要点（以代码为准）：

- 插件以 `Name()`、`Version()` 为键；同名同版本重复注册返回错误，不同版本可共存。
- 只有传入非 nil `PluginConfig` 时，插件才以 `name@version`、优先级 100 登记到生命周期管理器，`Init` 收到 `PluginConfig.Config`；传 nil 只登记到注册表，`InitializeAll`/`StartAll` 不会调用它。`PluginConfig` 的 `Name`、`Version`、`Dependencies`、`Enabled` 当前不被注册中心读取。
- `InitializeAll` 按优先级升序初始化；`StartAll` 另按组件实现的 `interfaces.DependencyAware.Dependencies()`（填写生命周期名，即 `name@version`）排序；`StopAll` 按优先级逆序停止。单个操作默认超时 30 秒，默认遇错即停。
- `GetPlugin(name, version)` 取指定版本；`GetLatestPlugin(name)` 按版本**字符串**比较取最大值（`"1.10.0"` 小于 `"1.9.0"`），不是语义化版本比较。另有 `ListPlugins`、`ListVersions`、`GetAllAgents`。
- `BasePlugin.GetMiddleware` 返回 nil；需要时在自定义插件中覆盖。
- `Container.Resolve` 先查实例，再调用工厂并缓存成功的结果；`MustResolve` 失败时 panic。

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
