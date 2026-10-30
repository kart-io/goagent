# harness/preset — 声明式自定义 Agent

一个 preset 就是一个 Agent：由若干组件行组成，组件列表即该 Agent 的全部能力（提示词、工作区与沙箱、工具、权限、结果溢出）。配置分层叠加，但权限上限只由可信层决定，项目里的配置只能收紧权限。设计与取舍见 [设计文档第 14 节](../../docs/design/HARNESS_CORE_DESIGN.md)。

```go
sources, _ := preset.FileSources(root, userHome)         // 用户层（可信）、项目层与本地层（不可信）
set, err := preset.Load(append(sources, hostLayer)...)  // 合并并严格校验；不启动进程、不读秘密
coder, _ := set.Get("coder")
asm, err := preset.Build(ctx, coder, preset.Host{Root: root, Approver: approver, Trust: &preset.FileTrustStore{Path: trustFile}})
var trustErr *preset.TrustRequiredError
if errors.As(err, &trustErr) {
    // 向用户展示 trustErr.Items，同意后授权再重新 Build
    _ = host.Trust.Grant(trustErr.Root, trustErr.Digest)
}
defer asm.Close()

agent, _ := builder.NewSimpleBuilder(client).
    WithSessionID(sessionID).
    WithSystemPrompt(asm.SystemPrompt).
    WithTools(asm.Tools...).
    Build()
```

完整示例见 [examples/tools/09-coding-agent](../../examples/tools/09-coding-agent)。

## 配置格式

```yaml
limits:                       # 只能出现在可信层
  sandbox: workspace-write    # 不可信层的沙箱模式上限
  executors: [docker]         # 缺省不限
  images: [golang:1.25]       # 缺省不限
  network: false
  hostProcesses: []           # 允许在宿主机启动的进程
  secrets: [GITHUB_TOKEN]     # 允许引用的秘密
presets:
  - id: coder
    name: 编码助手
    components:
      - {id: persona, use: persona, config: {prompt: "你是一个严谨的编码 Agent……"}}
      - {id: workspace, use: workspace, config: {sandbox: workspace-write, executor: docker, image: golang:1.25}}
      - {id: instructions, use: instructions}
      - {id: coding, use: coding-tools}
      - {id: permissions, use: permissions, config: {base: coding, rules: [{tool: git_commit, decision: ask}]}}
      - {id: spill, use: spill}
```

| 层 | 位置 | 信任 |
|---|---|---|
| 内置、宿主 | 代码传入的 `Source{Trusted: true}` | 可信 |
| 用户 | `~/.goagent/agents.yml` | 可信 |
| 项目 | `<root>/.goagent/agents.yml` | 不可信 |
| 本地 | `<root>/.goagent/agents.local.yml` | 不可信（位于仓库目录内） |

### 合并

- preset 按 `id` 合并：后层出现的 `name`/`description` 替换前层，写空串即清空；`remove: true` 删除整个 preset。
- 组件按 `id` 合并：同 id 整行替换并保留位置，`config` 整体替换不深合并；替换不得改变 `use`（确需更换实现写 `replace: true`）；`remove: true` 删除，之后重新出现时追加到末尾；新 id 按层、文件内顺序追加；同一层重复 id 报错。
- 提示词段落与钩子按最终组件顺序排列；依赖只影响初始化顺序。
- 未知字段、未知组件、单例重复、缺少依赖都在 `Load` 时报错。`set.Dump()` 输出合并结果、每行来源、被覆盖与被删除的行，以及需要授权的扩权项。

### 信任

不可信层中的下列内容是**扩权项**，`Build` 返回 `*TrustRequiredError`，经 `TrustStore.Grant` 授权后才生效：

- 超出 `limits` 的沙箱模式、执行器、镜像、联网、宿主进程、秘密引用；
- `allow` 规则、`base: coding`、`sessionTools`；
- 替换或删除来自可信层的组件（可信层可用 `locked: true` 直接禁止）。

新增 `deny`/`ask` 规则、降低沙箱模式、删除自己层的组件无需授权。授权绑定项目根目录与扩权项（含所在组件的完整配置）的摘要，配置变化后需重新授权。

## 内置组件

| 组件 | 配置 | 说明 |
|---|---|---|
| `persona` | `prompt` | 一段系统提示词 |
| `instructions` | `files`（默认 AGENTS.md、CLAUDE.md）、`maxBytes`（默认 32768） | 读取工作区内的项目说明，依赖 `workspace` |
| `workspace` | `sandbox`、`executor`（docker/local）、`image`、`network`、`commandTimeout`、`maxOutputBytes`、`linters` | 单例。沙箱模式是硬性要求，见下表 |
| `coding-tools` | `tools`（缺省为沙箱下可用的全部） | 依赖 `workspace`；列出不可用的工具时 Build 失败 |
| `permissions` | `approval`（ask/never）、`base`（coding）、`rules`、`sessionTools` | 可多行，全部规则合并为一个策略，按 Deny > Ask > Allow 取最严格者 |
| `spill` | `maxBytes`（默认 100000）、`retention`（默认 168h） | 超长结果写入 `Host.ArtifactDir`，提供 `read_spill` |

| 沙箱模式 | 文件工具 | 命令（含 git、lint） |
|---|---|---|
| `read-only` | 只读，不提供写入类工具 | 只在以只读方式挂载工作区的容器中执行，否则不提供 |
| `workspace-write` | 写入限于工作区 | 必须在容器中执行，本地执行器 Build 失败 |
| `danger-full-access` | 写入限于工作区 | 可在宿主机执行 |

执行器的网络能力必须与 `network` 一致（本地执行器总能联网，须写 `network: true`），做不到即 Build 失败，不静默降级。

宿主基线：没有任何规则匹配的调用需要审批；`approval: never` 时 Ask 自动拒绝；未提供 `Approver` 时 Ask 的结果为 Unavailable，同样按拒绝处理。

## 自定义组件

```go
type MyConfig struct {
    Endpoint string        `yaml:"endpoint"`
    Token    preset.Secret `yaml:"token"` // 配置中写作 {secret: NAME}
}

func init() {
    preset.Register(preset.Spec[MyConfig]{
        Name: "my-tool", Version: "1",
        Needs: func(c *MyConfig) preset.Needs { return preset.Needs{Network: true} },
        Build: func(ctx context.Context, a *preset.Assembler, c *MyConfig) error {
            token, err := a.Secret(c.Token)
            if err != nil {
                return err
            }
            a.AddTools(newMyTool(c.Endpoint, token))
            a.OnClose(func() error { return nil })
            return nil
        },
    })
}
```

秘密只能经 `Secret` 类型的字段引用，Build 时解析；`Dump`、错误与 `Assembly.Manifest` 中只出现引用。`Manifest.Digest()` 覆盖组件配置与版本、工具 schema、提示词与工作区，可用于在恢复持久运行前核对执行语义。

## 测试

```bash
go test ./harness/preset/ ./tools/spill/
```
