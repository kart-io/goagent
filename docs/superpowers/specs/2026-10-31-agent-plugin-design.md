# Agent 能力包（plugin）设计

基线 cbe4f51（v0.31.0）。定位、贡献范围、包位置、案例场景已由用户确认，采用"插件返回贡献清单，由 plugin 包安装"的方案。

## 目标与非目标

插件是在 Go 中静态组合的 Agent 能力包：一个插件把工具、提示词片段、循环钩子、中间件与回调，以及需要启停的进程级资源打包，经 `plugin.Install` 装进任意 `builder.AgentBuilder[C, S]`。

非目标：

- 运行时加载、热替换、按配置文件启停插件、语义化版本约束。组件装配仍由 goagent-harness 的 preset 负责（见 HARNESS_CORE_DESIGN 14.1），preset 组件可以直接包装插件。
- `core/plugin_bridge.go` 的 Runnable 注册表（`PluginRegistry`、`TypedToDynamicAdapter`）与 Agent 能力包无关，本次不改。
- `builder` 不导入 `plugin`，也不新增 `WithPlugins`。

## 包结构与接口

新建 `goagent/plugin`，依赖方向为 `plugin → builder、core、interfaces`。删除 `core/plugin_registry_enhanced.go` 及其测试（`Plugin`、`BasePlugin`、`EnhancedPluginRegistry`、`PluginConfig`、`Container`、`ResolveTyped`、`GlobalEnhancedPluginRegistry`）；`core` 的通用 `LifecycleManager` 保留。

```go
type Plugin interface {
    Name() string    // Host 内唯一；非空、无首尾空白、不含 "/" 与 "@"
    Version() string // 非空、无首尾空白、不含 "/"；进入持久运行的身份
    Contribute(ctx context.Context, env *Env) (Contribution, error)
}

type Starter interface { // 可选
    Start(ctx context.Context, env *Env) error
    Stop(ctx context.Context) error
}

type Dependent interface { // 可选
    Requires() []string // 必须先启动的插件名
}

type PreStep struct {
    Identity string // 必填，标识钩子配置；最终身份见"持久运行身份"
    Hook     builder.PreStepHook
}

type Contribution struct {
    Tools          []interfaces.Tool
    PromptSections []builder.PromptSection
    PromptContexts []builder.PromptContext
    PreSteps       []PreStep
    RequestHooks   []builder.RequestHook
    RequestErrors  []builder.RequestErrorHook
    MaxRecoveries  int // 只对 RequestErrors 生效
    TurnStopping   []builder.TurnStoppingHook
    Middleware     []middleware.Middleware // core/middleware
    Callbacks      []core.Callback
}
```

`Env` 是插件可见的宿主环境：`Plugin()` 返回当前插件名，`Provide(key, value)` 发布资源，`Resources()` 返回 Host 共享的 `*Resources`。

插件配置由插件构造函数接收自己的类型化结构体（如 `NewOrders(OrdersConfig{...})`），Host 不传递无类型配置。

## 共享资源

`Resources` 替代原 `Container`，只保存实例，不提供工厂：

- `Env.Provide(key, value) error`：只能在该插件的 `Start` 执行期间调用；`key` 必须以 `"<插件名>."` 开头，防止插件冒用他人命名空间；重复 key 或 nil 值报错。
- `Resolve(key) (any, bool)` 与泛型 `Lookup[T](r, key) (T, error)`：未找到与类型不符分别报错。
- 并发安全；`Stop` 之后清空。插件只应解析自己 `Requires` 中插件提供的资源，Host 不强制。

## Host 生命周期与依赖

`Host` 状态为 新建 → 已启动 → 已停止，不可重启。

- `NewHost(plugins ...Plugin) (*Host, error)`：校验名称与版本、名称唯一、`Requires` 指向已注册插件、无环；按依赖拓扑排序，同层保持注册顺序。错误一次性列出全部问题（`errors.Join`）。
- `Start(ctx)`：仅新建状态可调用；按拓扑顺序调用各 `Starter.Start`。某个失败时，按逆序停止已启动的插件，返回原错误与停止错误的合并；Host 进入已停止状态。ctx 已取消时不启动任何插件。
- `Stop(ctx)`：逆序调用 `Starter.Stop`，单个失败不中断其余，合并返回；重复调用返回 nil。
- `Plugins()` 返回拓扑顺序的 `名称@版本` 列表，供宿主记录。

## 安装与冲突规则

```go
func Install[C any, S core.State](ctx context.Context, b *builder.AgentBuilder[C, S], h *Host, opts ...InstallOption) error
func Only(names ...string) InstallOption          // 只安装列出的插件
func Dispatch(cfg tools.DispatchConfig) InstallOption // 插件工具统一经一个 Dispatcher 包装，持久运行需要
```

- Host 必须处于已启动状态（即使没有 Starter，也要求先 `Start`，保证资源在任何 Contribute 之前就绪）。
- 不带 `Only` 时安装全部插件；带 `Only` 时只安装列出的插件，未知名称报错；按 Host 拓扑顺序处理，与书写顺序无关。
- 持久运行要求工具经 Dispatcher（否则执行时报 `requires a journaled adapter`）。带 `Dispatch` 时，所选插件的全部工具注册进一个 `tools.Registry` 并经 `tools.NewDispatcher(reg, cfg)` 包装后再 `WithTools`；宿主自己的工具仍由宿主自行包装。
- 先对所有选中插件调用 `Contribute` 并完成全部校验，全部通过后才写入 Builder；任一失败时 Builder 不变。
- 校验（跨所选插件）：工具名重复、PromptSection 名重复、PromptContext 名重复、中间件 `Name()` 重复、PreStep `Identity` 为空、贡献中的 nil 钩子或 nil 工具。错误指明冲突双方插件名。与宿主自己经 `WithTools` 加入的工具重名，由 Build 时 Dispatcher 的 `duplicate tool` 报告。
- 写入顺序：插件按拓扑顺序，每个插件内按切片顺序，依次调用对应 `With*`。
- `RequestErrors`：所有插件的钩子合并后只调用一次 `WithRequestError`，`maxRecoveries` 取各插件 `MaxRecoveries` 的最大值（全为 0 时用 Builder 默认的 3）。宿主之后再调用 `WithRequestError` 会覆盖该上限，文档写明。
- 同一个 Builder 只应 Install 一次。Builder 没有可供外部读取的状态，`plugin` 不做检测；重复安装时重名工具会在 Build 时由 Dispatcher 报错，钩子则会重复执行，文档写明。

## 持久运行身份

插件 PreStep 以 `plugin/<名称>@<版本>/<Identity>` 调用 `WithIdentifiedPreStep`。插件升级版本后，恢复旧运行在请求模型前以 `intent changed` 失败，与现有 PreStep 身份规则一致。其他钩子不进入日志，行为与宿主直接调用 `With*` 相同。

## 错误处理

错误为中文、带插件名（`插件 "orders" 贡献无效：…`），保留 `errors.Is/As` 链。不对插件代码做 panic 恢复，与 Builder 钩子一致。

## 测试

`plugin` 包单元测试（`-race`）：

- NewHost：空名、非法字符、重复名、缺失依赖、环、拓扑顺序稳定。
- Start/Stop：顺序、失败回滚与错误合并、已取消 ctx、重复 Start、Stop 幂等、Stop 后资源清空。
- Resources：命名空间限制、只能在 Start 中 Provide、重复 key、Lookup 类型不符、并发访问。
- Install：未启动、未知名称、子集安装、各类冲突、失败时 Builder 不变、`MaxRecoveries` 取最大值。
- 集成：用 `llm.MockStreamClient` 驱动 Agent，验证工具可被调用、PromptSection 进入 system、PreStep/RequestError/TurnStopping/中间件/回调生效；持久运行中插件版本变化导致 `intent changed`。

`scripts/apicheck` 增加导入规则：`builder`、`core`、`interfaces`、`llm` 不得导入 `plugin`。

## 案例（goagent-examples）

放在 `orchestration/plugin/`，复用 `orchestration/internal/demomodel` 做离线确定性模型；设置 `GOAGENT_LIVE=1` 与 `DEEPSEEK_API_KEY` 时经 `internal/livemodel` 调用真实模型。每个案例有 `main.go`、离线 `main_test.go` 与 README。

| 目录 | 场景 |
|---|---|
| `basic` | 订单查询插件（工具 + 使用说明 PromptSection）；与物流插件组合进同一 Agent；两个插件提供同名工具时 Install 报错 |
| `hooks` | 手机号脱敏（PreStep）、限流重试（RequestError + `RetryTransient`）、答案缺少订单号时续跑（TurnStopping）、审计中间件与回调 |
| `resources` | 库存插件在 Start 中打开内存库存并 Provide；定价插件 Requires 库存并 Lookup；第二个插件 Start 失败时库存插件被回滚停止 |
| `support-desk` | 客服端到端：订单 + 退款策略 + 脱敏插件，持久运行中途崩溃后恢复且不重复调用工具；升级脱敏插件版本后恢复旧运行报 `intent changed` |

## 文档与发布

重写 `docs/guides/PLUGIN_SYSTEM_GUIDE.md`（能力包为主，Runnable 注册表一节保留）；更新 `docs/README.md`、`docs/architecture/ARCHITECTURE.md` 的包说明与 CHANGELOG（`Added` 与 `Removed` 含迁移说明）。提交为 `feat!`，发布 0.32.0 后升级 goagent-examples 的依赖并提交案例。

## 风险

- 重复 Install 同一个 Builder 不被检测（见安装规则），只能依靠文档约定与 Build 时的工具重名检查。
- 插件可在 Contribute 中返回闭包持有资源；Host Stop 后继续使用旧 Agent 会访问已释放资源，文档要求先停用 Agent 再 Stop。
