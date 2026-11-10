# GoAgent 导入层级与检查范围

## 检查器

`scripts/verify_imports.sh` 调用 `go run ./scripts/apicheck -mode imports -root .`：用 Go AST 读取仓库内 `.go` 文件（跳过隐藏目录、`vendor`、`node_modules`，包含 `examples/`），检查导入、错误定义和业务错误构造；任一违规即以非零码退出。分层与错误规则只检查非测试代码，测试文件只检查格式库导入。规则在 `scripts/apicheck/main.go` 中，回归测试在 `scripts/apicheck/main_test.go`。CI（`ci.yml`、`pr.yml`、`release.yml`）都会运行该脚本，任一违规即失败。

检查器禁止外层包通过纯 `const` 转发为中央 `constants` / `errors` 再建入口（包括带括号的值）；领域枚举、自定义 Provider 字面量和派生策略表达式保留。

检查器同时禁止已迁移的50项共享协议/默认策略按旧包符号重新声明，字面量和转发都不能回流，也不能改用包级 `var` 重建旧入口；并拒绝框架生产包重新声明未绑定领域类型的包级 `Default*` 默认常量。显式领域定义类型枚举、持久格式、算法参数及独立 `codec`/`harness/env` 边界保留。扫描按目录和包名分组，先读取全部非测试源码，再沿本文件及跨文件的透明类型别名解析；别名使用其声明文件的导入，不能借 `type Wait = time.Duration` 或 `type Count = int` 获得领域类型豁免。标准库 `time.Duration` 的别名导入及点导入仍视为带单位默认值，真正定义的同名领域类型不误判；循环别名不会获得豁免。检查只使用 AST，不依赖完整项目类型检查；测试声明不进入生产类型上下文，测试文件仍只检查格式库导入。旧符号清单与规则位于 `scripts/apicheck/constants_ownership.go`，不以禁止所有非中央 `const` 代替归属判断。

未显式写类型的领域枚举也可经本包类型转换、具名常量引用、括号及保留类型的运算识别；支持跨文件绑定和 const 块的隐式继承，显式声明类型优先，引用循环会终止。定义的泛型领域类型实例仍保留领域身份，透明泛型别名继续解析其目标。常量比较结果不继承领域类型，移位只继承左操作数类型。此规则不解析任意外部依赖的常量或透明别名、不验证泛型参数约束和表达式合法性；外部常量绑定不明确时应显式标注领域类型，由 Go 编译器另行检查合法性。

检查器还约束 `constants/` 内的错误常量归属：`Err*`、`ErrorField*`、`Code*` 及已知 LLM 失败提示须移入 `errors/`；HTTP 状态码、错误字段和错误事件等协议约定可保留。

## 错误构造与独立边界

框架生产代码的业务错误使用 `github.com/kart-io/goagent/errors` 的 `AgentError` 构造函数，提供错误码及组件、操作或参数信息。稳定控制信号由该包集中定义为不可变的 `SentinelError`，调用方直接引用中央哨兵并使用标准库 `errors.Is/As`；同码的不同哨兵仍保持独立身份。原错误入口迁移后删除，不保留兼容变量。

检查器按实际导入路径识别标准库 `errors`，拒绝框架代码中的 `errors.New` 引用，包含显式导入别名及把构造函数赋给变量的写法；同时拒绝标准库 `errors` 的点导入。局部同名变量、框架包自己的 `errors.New`、标准库 `errors.Is/As/Join` 不受影响。

无真实 `%w` 动词的业务 `fmt.Errorf` 使用框架 `Newf` 或专用构造器，补齐分类和组件/操作信息。标准 `fmt.Errorf` 只保留明确的错误链包装（包括给构造函数加括号的直接调用），支持索引参数和多个 `%w`；`%%w` 是文本转义，不算包装；超长宽度/精度按标准 fmt 的数字解析终止规则处理，不能把无实际包装的格式放行。检查器按实际 `fmt` 导入路径识别别名，拒绝点导入、构造函数间接引用和不能静态确认包装格式的写法。已有 `%w` 不额外改成新业务类别，保留错误链顺序、哨兵身份与等待叶子。

以下目录保留独立错误模型，允许标准库错误构造：

| 范围 | 原因 |
|---|---|
| `errors/` | 中央错误模型的实现位置 |
| `codec/` | 独立格式转换入口，不反向依赖框架错误实现 |
| `harness/env/` | 无框架内部依赖的通用环境接口 |
| `examples/`、`scripts/`、顶层 `cmd/` | 示例和命令行的本地参数、运行错误 |
| `evaluation/` 内含 `cmd/` 目录段的命令行 | 基准运行器的命令行错误；`evaluation/budget.go` 等框架源码仍检查 |
| `testing/`、`core/durable/storetest/`、`*_test.go` | 独立测试工具和故障注入；测试文件继续执行格式库导入检查 |

允许范围按目录段判断：`harness/env_extra/` 和 `core/cmd/` 不因名称相近而豁免。此规则不改变标准库 context、fs、io 错误的身份或错误链语义。

## 分层

非测试代码只能导入同层或更低层的 GoAgent 包。包按路径最长前缀归层（`core/usage` 优先于 `core`），未在表中声明所在层的包本身就是一条发现，新增顶层目录须先在 `scripts/apicheck/main.go` 的 `layers` 中声明。该表是分层的唯一来源，下表与其保持一致：

| 层 | 路径前缀 |
|---|---|
| 0 | `constants`、`errors`、`interfaces`、`codec` |
| 1 | `internal`、`utils`、`cache`、`parsers`、`skills`、`core/usage`、`core/state`、`core/internal`、`tools/toolargs` |
| 2 | `llm`、`store`、`revision`、`harness`、`core/durable`、`core/middleware`、`core/session`、`core/budget`、`core/observe` |
| 3 | `core`（其余子包） |
| 4 | `tools`、`workflow`、`memory`、`observability` |
| 5 | `builder`、`a2a`、`mcp`、`middleware`、`devtools` |
| 6 | `execution`、`multiagent`、`plugin`、`evaluation` |
| 7 | `management`、`testing`、`scripts`、`cmd`、`examples` |

## 专门规则

以下规则在分层之外进一步收紧个别包，按“源文件所在的顶层目录”与“被导入的 GoAgent 包”判断；循环依赖由 Go 编译器拒绝。

| 源目录 | 禁止导入 |
|--------|----------|
| 任意（含 `examples/`） | `github.com/kart-io/goagent-harness` 及其子包 |
| `constants/` | 任何 GoAgent 内部包及第三方包（只依赖标准库） |
| `interfaces/`、`errors/` | 除 `constants` 以外的 GoAgent 内部包 |
| `utils/` | 除 `utils/...`、`errors`、`codec/...` 与 `constants` 以外的内部包 |
| `cache/` | 除 `cache/...`、`codec/...`、`constants` 与根 `errors` 以外的内部包；不允许 `errors/...` 子包 |
| `codec/` | 除 `codec/...` 以外的 GoAgent 内部包（统一格式转换入口，只依赖标准库、YAML 库、mapstructure 与彼此） |
| 除 `codec/` 外的全部代码（含测试与 `examples/`） | `encoding/json`、`encoding/json/v2`、`encoding/json/jsontext`、`encoding/xml`、YAML 库（`go.yaml.in/yaml/...`、`gopkg.in/yaml.*`、`sigs.k8s.io/yaml`、`goccy/go-yaml`）、mapstructure，以及 sonic、json-iterator、go-json、easyjson、gjson、sjson、jsonparser、go-toml、BurntSushi/toml。数据格式转换统一用 `codec/json`、`codec/yaml` 的 `Marshal`/`Unmarshal`，`json.RawMessage`、`json.Number` 等类型与 `json.Strict`、`json.Indent` 等选项，格式运行时才确定时用 `codec`；测试文件只检查此条，其余分层规则只约束非测试代码 |
| `core/` | `tools`、`middleware`、`parsers`、`builder`、`skills`、`revision`（顶层目录，`core/middleware` 不受影响） |
| `revision/` | 除 `revision/...`、`skills/...`、`codec/...`、`errors` 与 `constants` 以外的内部包 |
| `builder/` | `middleware` |
| `parsers/` | `middleware` |
| `interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness`、`multiagent` | 顶层 `execution` |
| `interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`multiagent` | 顶层 `plugin`（能力装配层） |
| `interfaces`、`errors`、`utils`、`cache`、`core`、`builder`、`tools`、`parsers`、`llm`、`workflow`、`memory`、`store`、`middleware`、`observability`、`devtools`、`harness` | 顶层 `multiagent`（多 Agent 预置模式） |
| 非 `examples/` | `examples/...` |

说明：

- 顶层 `execution/` 是宿主装配薄层（依赖 `builder`、`core`、`core/budget`、`core/durable`、`core/observe`、`tools`），只有第 6 层及以上的包（如 `evaluation`、`management`、`testing`、`examples`）可以导入。
- Harness 已迁到独立仓库 `github.com/kart-io/goagent-harness`，它依赖 GoAgent，反向导入被禁止。本仓库的 `harness/env` 是不依赖内部包的通用环境接口。迁移见 [Harness 迁移指南](../guides/HARNESS_MIGRATION.md)。

## 当前依赖图

`go list` 得到的实际内部依赖（不含 `examples/`、测试与 `cmd/`）见 [架构概述](ARCHITECTURE.md#包依赖)。基础常量叶子层为 `constants`，它是共享协议值、默认参数和 Provider 类型的唯一入口；原包转发声明及 `llm/constants` 包已删除，不保留兼容别名。其他领域内部类型仍在所属包。迁移方式见[常量迁移指南](../guides/CONSTANTS_MIGRATION.md)。错误码、分类、提示、上下文字段及框架哨兵由 `errors` 统一维护，调用方直接导入该包；`constants` 不导入 `errors`。`cache` 可依赖根 `errors`，`errors` 不反向依赖 `cache` 或其他框架实现；`harness/env` 保持无内部依赖的独立接口。

## 运行

```bash
bash scripts/verify_imports.sh      # 期望输出：imports: 0 findings
go test ./scripts/apicheck/         # 规则回归测试
```

新增或调整边界时，同时修改 `forbidden` 与 `main_test.go` 的用例，再更新本表。

## 退役接口与配置

`apicheck` 同时拒绝重建已退役的旧流、工具调用别名、缓存实现、中间件旧钩子、Runtime 空配置/指标及 Runnable 空重试配置。`WithMiddleware` 必须声明为强类型函数装配入口，`NewRuntimeManager` 不接收已删除的配置；这些函数名不能改成变量或类型来接回旧调用形状。

无效配置字段的检查沿本包结构体别名、底层定义和匿名嵌入追踪；旧方法接收者只沿透明别名追踪，真正的新定义类型保留自己的方法身份。类型上下文按目录和包名隔离，不让测试声明污染生产检查。在线 durable 包不允许 operation-v1 分派；历史版本只允许在独立离线迁移组件中处理。正常协议 facade、当前业务类型及崩溃恢复不属于退役入口。

此规则检查明确退役清单和本包 AST，不代替编译器完整解析外部类型语义。接口与调用迁移见[兼容入口迁移指南](../guides/COMPATIBILITY_CLEANUP.md)。

## 导入顺序

`make fmt` 用 `gofumpt` 格式化，并用 `gci` 按“标准库 → 第三方 → `github.com/kart-io/goagent`”分组排序。

## 相关文档

- [架构概述](ARCHITECTURE.md)
- [测试最佳实践](../development/TESTING_BEST_PRACTICES.md)
