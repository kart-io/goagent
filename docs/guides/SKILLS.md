# 通用 Skills 加载

`github.com/kart-io/goagent/skills` 是主仓中可单独使用的只读加载包。
它不依赖 Builder、模型、工作流、插件或 Harness，执行内核也不依赖它。
可选的 `github.com/kart-io/goagent/tools/skill` 把 Catalog 包装为 `load_skill` 工具。

## 基本用法

```go
root, err := os.OpenRoot("/srv/approved-skills")
if err != nil { return err }
defer root.Close()

catalog, err := skills.Discover(ctx, root)
if err != nil { return err }
defer catalog.Close()

metadata := catalog.List()                         // 名称、描述、来源、版本、摘要
loaded, err := catalog.Load(ctx, "faq")             // 正文、资源文件名
if err != nil { return err }
data, err := catalog.ReadReference(ctx, "faq", "references/refunds.md")
if err != nil { return err }
// 调用方消费 metadata、loaded.Body 和 data。
```

独立解析内存中的文件可用 `skills.Parse(data)`。可运行案例：

```sh
env -u GOROOT go run ./examples/skills/03-catalog
```

它演示[元数据、完整说明、附属资源三个加载层次](https://agentskills.io/integrate-skills)，无需模型密钥。
解析遵循 [Agent Skills 格式](https://agentskills.io/specification)，支持 UTF-8 BOM、LF、CRLF、
完整行的 frontmatter 分隔符及标准可选字段；名称、描述、兼容性说明进行严格校验。
名称限制为原 Harness 支持的 ASCII 小写字母、数字和单连字符，目录名须匹配名称。
YAML 使用 GoAgent 的 `codec/yaml.DecodeMapping`，在展开别名前检查大小并保留字段类型；
所有映射键和 `metadata` 的值必须是字符串，数字版本应写作 `version: "1"`，显式 null 不代替可选字段的省略。
小型别名可用；重复键（含别名键）、循环别名、展开超限、YAML 合并键 `<<` 和二进制标量 `!!binary` 会被拒绝。
frontmatter 是文本元数据，二进制素材应放在附属文件中，避免 YAML 到 JSON 的转换改变字节。
数值的科学计数指数也计入解码预算，避免短数字文本触发巨大有理数转换。
通过上述 YAML 类型与预算检查后，未知扩展字段会被忽略；`disable-model-invocation` 保留为可供宿主解释的扩展字段。

## 来源与所有权

- Discover 只扫描传入的目录句柄中的 `<name>/SKILL.md`，不选择默认目录或解释 `~`。
- 调用方决定来源顺序。多个目录按参数顺序处理，同名保留第一个；单个目录内按名称排序。
- 无 SKILL.md 的目录被跳过。格式错误、名字不匹配或不可读的技能使整次发现失败，包含被覆盖的技能。
- 来源目录仅被借用到 Discover 返回；调用方负责关闭。Catalog 自己持有每个已选技能的子目录句柄，失败时清理，成功时由调用方 Close。
- List 返回独立副本。Close 可重复调用，之后仍可 List 查看元数据，但 Load/ReadReference 返回 `fs.ErrClosed`。未知技能支持 `errors.Is(err, fs.ErrNotExist)`。
- Source 是发现时的 SKILL.md 路径，仅用于追踪，不能用它重新打开文件代替 Catalog 的受限读取。

元数据和正文是发现时的快照，资源列表和资源内容按需读取。`Version` 来自作者声明的
`metadata.version`，`Digest` 是原始 SKILL.md 全文的 SHA-256，包含 frontmatter、BOM 与换行差异。
摘要不包含资源文件，不代表整包签名或不可变版本。需要整包一致性时，宿主应提供不可变目录或另行校验资源；
需要更新正文时应重新 Discover 并在旧使用者结束后关闭旧 Catalog。

## 文件边界

技能目录使用 `os.Root` 持有句柄，目录被重命名或原路径被替换不会把读取重定向到新目录。
上述目录身份语义依赖平台的 `os.Root` 实现；Go 的 JS、Plan 9 实现不跟踪目录重命名。
本次运行验证在 Linux，Windows/macOS 仅做交叉编译，未验证其运行行为。
资源路径使用斜线相对路径；兼容 `./references/a.md`、`references/./a.md` 和重复斜线。
在归一化之前拒绝任何 `..` 分量、绝对路径、反斜线和空路径，文件参数不接受末尾 `/` 或 `/.`。
内部文件链接可读，越界链接被拒绝；发现时不跟随技能目录链接，资源枚举不递归跟随目录链接。
读取只允许普通文件；FIFO 不会等待写入者。受限根目录不等于沙箱：目录内已有的硬链接、挂载及来源授权由宿主管理。

资源上限位于 `constants`：SKILL.md 最大 1 MiB，frontmatter 展开后的 JSON 大小保守估算最多 1 MiB，
单个引用文件最大 4 MiB，发现扫描和每次资源枚举
分别最多 10000 条目，最多 2000 个选中技能，资源枚举深度最多 32 层。超限返回错误，不静默截断。
每个选中技能持续持有一个子目录句柄，实际容量还受进程句柄上限约束；更新时新旧 Catalog 并存也会占用额外资源。
Load 会同时枚举资源，枚举失败时整体返回错误，不单独返回已缓存正文。
带 context 的操作在扫描间隔和读文件前后检查取消；锁等待和普通本地文件的单次系统调用不承诺即时中断，Close 会等待在途读取结束。

## 工具与宿主职责

```go
tool := skilltool.New(catalog)
agent, err := builder.NewSimpleBuilder(model).
    WithSystemPrompt(promptSelectedByHost).
    WithTools(tool).
    Build()
```

`load_skill` 接受 `name` 和可选 `file`。省略 file 时返回 `name/instructions/files`；指定 file 时返回
`name/file/content`。没有附属文件时 `files` 是空数组 `[]`。公共 Catalog 返回原始字节，工具的 content
仅输出可无损编码的 UTF-8 文本，非 UTF-8 内容会明确拒绝；二进制素材应由宿主直接读取。
工具不拥有 Catalog，不自动注入提示词，不解释授权，不执行脚本。

工具区分业务拒绝和执行失败：未知技能、缺失文件、已识别的无效路径/权限/大小限制返回 `Rejected` 并给出原因。
正文加载时已识别的资源枚举条目数或深度超限同样返回 `Rejected`，完整说明与文件列表不会部分交付。
未分类的 I/O 故障返回非 nil 执行错误，保留 `errors.Is` 身份，避免将临时故障记录为已完成的拒绝。
Go `os.Root` 未公开部分边界错误的标识，这些错误也原样进入执行错误链，读取仍被阻止，不通过错误文本猜测分类。

上述直接 `WithTools(tool)` 示例用于普通调用。持久 Builder 应使用 Dispatcher 装配后的工具和稳定的 DurableScope；
使用 Harness 时，宿主还需显式固定 Manifest 并管理 Assembly 生命周期。已完成的成功或拒绝按原调用身份重放，
不会因文件后来改变而重新读取；需要读取最新资源时应发起新调用。

宿主决定哪些来源/技能可供调用者使用、来源覆盖顺序、目录如何展示、上下文压缩后是否重新加载，
以及是否提供技能声明的工具。`AllowedTools` 不会自动授权；`DisableModelInvocation` 不会自动过滤公共 Catalog。
隐藏目录提示不能代替授权，若用户不可读取某技能，应在来源选择或工具调用策略层阻止访问。
Git/OCI 下载、安装更新与脚本执行继续使用各自的来源管理或执行环境。

Harness `preset/skills.go` 保留配置入口、默认目录、工作区路径解释、信任授权、提示词和 Manifest 装配，
复用这个 Catalog 和工具。组件版本升级为 3，Manifest 从正文摘要改为原始 SKILL.md 全文摘要，
因此仅修改元数据也会导致已固定的 Manifest 不匹配；旧版本的持久运行需要由宿主处理版本迁移。

v3 也收紧了旧 Harness 的解析规则：布尔值使用 `true/false`（不再接受 `yes/no`），纯数字名称需要引号，
新增元数据读取要求 `metadata.version` 等值是字符串，不再忽略它们的类型；名称、描述和兼容性说明分别限制
为 64、1024、500 个字符。无效文档包括被覆盖的技能均会使 Build 失败，迁移前应修正对应文件。

这批新公共 API 尚未发布到版本依赖。Harness 当前开发验证需用临时 `go.work` 组合两个 checkout；
独立发布 Harness 前须把其 `go.mod` 更新到包含新 API 的可获取 GoAgent 版本，并以 `GOWORK=off` 验证。
