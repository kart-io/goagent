---
phase: skills-extraction-design-review
reviewed: 2026-11-09
depth: deep
role: architecture-public-api
files_reviewed: 20
files_reviewed_list:
  - skills/parse.go
  - skills/catalog.go
  - constants/skills.go
  - skills/catalog_test.go
  - skills/parse_test.go
  - tools/skill/tool.go
  - examples/skills/03-catalog/main.go
  - go.mod
  - docs/guides/SKILLS.md
  - docs/guides/HARNESS_MIGRATION.md
  - docs/architecture/ARCHITECTURE.md
  - docs/design/HARNESS_CORE_DESIGN.md
  - docs/evaluation/2026-11-09-skills/REPORT.md
  - docs/evaluation/2026-11-09-skills/multi-agent-review/SUMMARY.md
  - docs/evaluation/2026-11-09-skills/multi-agent-review/ARCHITECTURE.md
  - docs/evaluation/2026-11-09-skills/multi-agent-review/RELEASE.md
  - ../goagent-harness/preset/skills.go
  - ../goagent-harness/preset/build.go
  - ../goagent-harness/preset/README.md
  - ../goagent-harness/go.mod
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
optional_improvements: 3
known_release_gates: 1
status: clean
---

# Skills 架构与公共 API 续审

本角色本轮未确认新增缺陷，建议通过所分配的架构/API 审查。`clean` 仅表示本角色没有尚待处理的确认问题，不表示独立发布或安全审查通过。主审提供的 `!!binary` 格式问题及修复状态单列，不重复计算为本角色独立发现。

审查对象是当前两个 checkout 的未提交实现，重点为 `skills/parse.go`、`skills/catalog.go`、`constants/skills.go`；另沿实际消费者追踪所有权和装配。没有修改源码、提交或推送，没有运行测试、关闭项探针或全量套件。本报告是唯一写入文件。

## 范围与证据

先读 `docs/guides/SKILLS.md` 和上一轮 `multi-agent-review/SUMMARY.md`。前一轮六项关闭结论作为历史状态，不重新验证。没有重新启动此前失败的安全审查，也没有进行解析器对抗输入研究；codec 设计及规范核对由主审负责。

上方清单包含 20 个主要文件。阅读深度如下，避免将局部追踪描述为整仓复审：

| 文件 | 本轮范围 |
|---|---|
| `skills/parse.go`、`skills/catalog.go`、`constants/skills.go` | 全文，导出 API、返回副本、持有句柄、错误与关闭路径、容量边界 |
| `skills/catalog_test.go` | 全文静态阅读；采信所有权、快照、并发关闭和容量的既有断言；未执行 |
| `skills/parse_test.go` | 正常元数据、摘要和返回结构断言；不评审对抗解析用例 |
| `tools/skill/tool.go` | 导入、`New` 的借用契约和调用链、错误入口；不重做已关闭错误树验证 |
| `examples/skills/03-catalog/main.go` | 全文，独立应用与 Catalog/工具消费关系；不重跑示例关闭项 |
| 双方 `go.mod` | 工具链与模块依赖声明 |
| `docs/guides/SKILLS.md` | 初始全文，作为当前产品契约；主审后续 binary 修改不冒充已独立复核 |
| `docs/guides/HARNESS_MIGRATION.md`、`docs/architecture/ARCHITECTURE.md` | Skills 职责、模块迁移和发布相关段落 |
| `docs/design/HARNESS_CORE_DESIGN.md` | 历史职责分离、B 期组件实施段落 |
| `REPORT.md`、上一轮 `SUMMARY.md`、`ARCHITECTURE.md`、`RELEASE.md` | 当前状态、关闭项、已有验证和发布条件；不是本轮新执行结果 |
| Harness `preset/skills.go` | 当前全文；另全文读取最初迁出 `0af7f76` 和当前 HEAD 的历史版本 |
| Harness `preset/build.go` | `OnClose`、`Assembly.Close`、`closeAll`、失败清理、成功移交 closers 的相关片段 |
| Harness `preset/README.md` | Skills、v3 解析/结果兼容性、开发验证段落 |

补充读取或检索的上下文逐项列明：Core `CLAUDE.md`、双方 `.gitignore`；双方 `README.md`、Core `README_CN.md` 的平台声明检索；Harness `docs/MIGRATION.md` 全文及 `docs/design/HARNESS_CORE_DESIGN.md` 的相关段落检索。两仓均未发现 `.claude/skills/`、`.agents/skills/` 或 `.claudeignore`；审查目标未被 `git check-ignore` 排除。没有读取完整 `AGENTS.md`。

为回答清理错误及平台问题，读取本地 Go 1.27.3 标准库片段：`src/os/root.go`、`root_openat.go`、`root_noopenat.go`、`stat_unix.go`、`file.go`、`file_posix.go`、`file_unix.go`、`src/internal/poll/fd_unix.go`；并检索 `src/syscall` 中 Windows、Plan 9、JS 文件的 `O_NONBLOCK` 定义。这些均位于 `/home/hellotalk/.gvm/gos/go1.27.3/`，不是仓库修改对象。

## 策略与机制：对照初始 Harness 抽取

最初迁出提交 `0af7f76:preset/skills.go` 的 v1 将来源解释、`env.LocalFS`、解析、缓存正文、资源读取、提示词和 Manifest 放在同一组件。迁移前 HEAD（`912bca0a05c11c204b5cad695f0bd85fc0b10b5b`）已是 v2，使用持有的 `os.Root`。不能把 v2 已有的句柄语义全部归为本次 v3 的新设计。

| 职责 | 当前归属与判断 |
|---|---|
| 默认 `.agents/skills`、`.claude/skills`；相对工作区、绝对路径与 `~` | Harness `preset/skills.go:24–59,107–127`；合理的宿主策略，没有进入公共加载器 |
| 授权需求、提示词隐藏和 `load_skill` 权限装配 | Harness `:46–53,89–101`；`DisableModelInvocation` 是提示词策略，不自动撤销读取能力，当前文档明确 |
| 来源优先顺序 | 宿主传入顺序；Core 执行首个同名胜出，单来源按名称排序。公共 API 无默认来源 |
| 解析、目录获取、正文快照、资源读取 | Core `Parse/Discover/Catalog`；不依赖 Harness、模型、Builder 或执行器 |
| 工具结果封装 | 可选 `tools/skill.New(catalog)`；借用 Catalog，普通应用可以不使用工具适配器 |
| 运行清单与版本 | Harness v3 保留 `name@digest` 形式，改用原始文档摘要；作者 Version 不是组件版本 |

当前 `skills` 的直接项目导入只有 `codec/yaml`、`constants`、`errors`。这是一份主模块内可单独消费的包，不是单独发布的 Go module。本轮没有重跑全仓导入门禁，不扩大此前门禁通过结论。

## 合理设计与普通正确性

1. **公开面足够小。** `Metadata`、`Skill`、`Catalog` 加 `Parse/Discover/List/Load/ReadReference/Close` 覆盖解析及三级消费。没有引入来源注册器、后台刷新、授权接口或执行抽象。`AllowedTools` 保留文本，避免公共包替宿主解释工具语法。`Attributes` 和便利字段 `Version` 的重复只存在于返回值，不形成内部双写状态。
2. **输入根借用、子根自有。** `catalog.go:43–121` 在 Discover 成功后只持有选中技能子根；重复项立即关闭，失败清理 Catalog。调用方关闭来源根不会关闭 Catalog 自有根。Harness `skills.go:61–84` 关闭来源根，并在后续装配前登记 Catalog 清理；`build.go:335–338,483–494` 分别覆盖失败和成功移交。未发现清理登记晚于失败点的问题。
3. **返回值隔离。** `parse.go:151–153` 复制属性 map；`catalog.go:126–153` 的 List、Load 都使用该副本，Files 每次独立创建。调用者可以编辑返回结构，不会改变目录快照。没有必要把这些数据结构改成大量 getter。
4. **关闭与并发语义明确。** Load/ReadReference 在使用句柄期间持读锁，Close 持写锁且幂等；List 在关闭后仍查看元数据。取消检查先于关闭检查，所以同时取消且关闭时可返回 context 错误，不属于错误身份混乱。Catalog 不可复制已在类型注释说明；宿主需传递指针。
5. **快照边界真实且透明。** 元数据、正文、Digest 在 Discover 时确定；Files 和引用内容实时读取。Source 仅用于追踪，Version 仅为作者声明，Digest 仅覆盖原始 SKILL.md。`ReadReference(name, "SKILL.md")` 同样会读取当前原始文件，可能不同于 Load 的缓存正文；这是当前所有普通文件均可读取的结果，不等于快照失效。
6. **升级归属正确。** 更新正文应重建 Catalog，等旧使用者结束后关闭旧实例；新发现不会自动迁移工具闭包持有的旧 Catalog。指南已有此职责说明。v3 原始文档摘要和严格字段规则属于明确记录的迁移变化，不宜伪装成 v2 无行为变化。
7. **复杂度与需求相称。** `entry`、map 加稳定 order、少量遍历辅助函数、单个 RWMutex 足够表达当前需求。固定深度和数量限制使递归边界可审查。没有证据需要拆出泛型仓储、插件式 loader 或另一个生命周期管理框架。通用 YAML 实现留在 codec，而非复制到 Skills；codec 自身复杂度由主审评估。

## 设计取舍与限制（不计缺陷）

| 取舍 | 具体影响与边界 |
|---|---|
| 预读全部正文 | Discover 为所有有效技能读取正文，三级加载是调用方/模型的消费层次，并非只读 frontmatter 的磁盘惰性加载。原 Harness 已在 Build 缓存正文，符合当前指南 |
| Load 同时枚举资源 | `catalog.go:145–153` 的资源目录不可读、超过条目/深度上限时，整个 Load 失败，即使正文已缓存。旧工具也先遍历再返回正文，当前没有“枚举失败仍交付正文”的承诺 |
| 全量校验遮蔽项 | 后续同名技能仍会解析，坏文档会中止发现。旧 Harness 已是先发现再去重，当前文档明确；若产品要求坏的备用来源不影响选中技能，应另外变更契约 |
| 固定容量 | 1 MiB 文档、4 MiB 引用、10000 条扫描预算、2000 个选中技能、32 层资源深度集中在 constants。发现预算跨所有来源累计，资源预算每次 Load 重新累计；目录、非技能条目也参与对应扫描计数。固定限制是受支持容量，不是 token 预算 |
| 持有目录句柄 | 选中技能持续占有子根，2000 是算法上限，不保证进程能够打开 2000 个目录。实际容量还受宿主现有句柄、系统限制和同时打开的来源根约束。错误向上传递并清理，不构成静默丢技能 |
| 热更新并存 | 构造新 Catalog 与旧 Catalog 暂时共存需要额外容量。资源可变意味着两次引用读取不保证一致；需要可重复整包内容时，宿主须提供不可变目录或额外资源校验 |
| context 与关闭 | `RLock/Lock` 等待本身不接受 context；Close 等待在途文件操作结束。上下文不能保证立即打断本地系统调用，当前不是严格时限关闭 API |
| 平台契约 | 实现以 `os.Root` 和 OS 文件系统为基础，不能直接接收 `embed.FS`、对象存储或任意 `fs.FS`。本地 Go `root.go` 明确 JS/Plan 9 不追踪重命名；不能把当前目录句柄说明扩大为所有 GOOS 保证。既有 REPORT 仅报告 Windows/macOS 交叉编译，不等于目标平台运行验证 |

这些取舍尚未提供足以要求新增 API 的具体业务失败。尤其不建议为容量猜想增加配置矩阵，或为资源枚举失败直接新增第二套加载接口。

## 清理错误疑点：独立判断

**位置：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/skills/catalog.go:69–71,103–106,220–224`。

**逻辑事实：** Discover 对 `discoverSkill` 返回值只用 `errors.Is(err, fs.ErrNotExist)` 判断跳过。如果收到 `errors.Join(fs.ErrNotExist, 未知错误)`，确实会吞掉未知分支。这与此前 `tools/skill` 分类器是不同调用点；工具层 AR-02 已关闭，不代表这一处已做错误树分类。

**实际可达性核对：**

- `readFile` 在 `root.OpenFile` 失败时于第 221–222 行直接返回；第 224 行的 `f.Close` defer 尚未登记。因此通常的 SKILL.md 缺失路径不能产生“打开缺失 + 文件关闭失败”。
- `discoverSkill` 确实会关闭已打开的技能根。但当前 Go 1.27.3 的 `os/root_openat.go:32–41`（Unix、Windows、WASI）和 `os/root_noopenat.go:59–64`（JS、Plan 9）中 `Root.Close` 都返回 nil；前者甚至不向上传递底层 close 返回值。因此不能用这个 defer 直接证明当前工具链会合并非 nil 清理错误。
- 文件成功打开后，`readFile` 的 Stat/Read 与 File.Close 可以有联合错误。已经查到本地 `File.Stat` 使用 Fstat、Read 使用已有描述符、File.Close 会保留关闭错误；但本轮没有具体文件系统证据证明此处 Stat/Read 返回 `fs.ErrNotExist`，同时 Close 返回另一个未知错误。仅合成一个 joined error 或直接替换返回值只能验证判定表达式，不能证明实际触发。

**结论：未确认可达缺陷，不列 Warning，不重开 AR-02。** 不声称所有文件系统永远不可能发生；若后续有真实文件系统或真实操作序列证据，可按新的 Catalog 缺陷立项。

**可选最小加固（AI-03）：** 仅当错误树所有非 nil 叶子均属于 `fs.ErrNotExist` 时才跳过，其余保留完整错误。不要用字符串分类，也不要让基础 skills 反向依赖工具层分类器。若实施，再补单错误、wrapped 缺失、纯缺失 join 和缺失加未知错误的局部分类回归；当前没有修改或执行该建议。

## 主审 binary 修复的 API 评估

主审提供复现：UTF-8 YAML 文本 `description: !!binary /w==` 经 DecodeMapping 返回替换字符，随后 Parse 成功。该证据由主审产生，本角色没有重复探针。主审随后告知已完成 codec 修复与定向验证；本角色只认可方案，不声称独立复核最终补丁或验证日志。

**API 判断：赞成在新 `DecodeMapping` 的转换前 guard 拒绝 `!!binary` 标量，并保留旧 `Unmarshal` 语义。** `Metadata.Description` 与 `Attributes` 的契约是文本字符串；YAML binary 是另一种类型，即使某份 binary 解码结果恰好是合法 UTF-8，也没有必要在这个文本映射 API 中接受。不能等 JSON 已替换无效字节后再判断，届时原始内容身份已经丢失。

该方案不要求扩展 Skill 为二进制联合类型，也不改变 `Catalog.ReadReference` 返回原始资源字节的职责。主审计划的 codec/skills 回归应覆盖拒绝而不是接受替换字符；旧解码 API 行为保持不变。

文档应说明“未知扩展字段不解释”仍以映射中类型被支持为前提，未知字段中的 binary 也会拒绝；需要 base64 文本的作者使用普通字符串或 `!!str`。这与当前已存在的全映射类型约束一致。此项为主审确认并处理的问题，不列入本角色新增缺陷数。

## 可选改进与已知发布门禁

- **AI-01，平台与容量说明：** 在 `docs/guides/SKILLS.md` 的文件边界/容量段落补充目标平台验证范围、每技能持有句柄及进程容量约束。依据为 `catalog.go:21–28,82–85` 和本地 `os.Root` 平台说明。最小改动是文档，无需引入新配置或改变读取机制。
- **AI-02，Load 与取消说明：** 在 `catalog.go:136–138` 的公开注释或指南补一句“资源枚举失败时 Load 整体失败；等待关闭/锁及单次文件操作不保证及时取消”。让使用者理解缓存正文可用性及延迟边界，不要求改变锁或公开接口。
- **AI-03，纯缺失错误识别：** 见上节清理错误判断；防御性改进，当前未证实真实失败。
- **已知发布门禁：** Harness `go.mod` 仍指向 `v0.49.3-0.20261107151110-dae60f4779d4`，不含新公共包。上一轮 `RELEASE.md` 已记录独立构建失败，本轮未重跑。必须先有可获取的 Core 版本，再更新 Harness 依赖并以 `GOWORK=off` 完成独立验证。本地 workspace 成功不能替代它。

## 验证与交付边界

本轮证据是逐文件阅读、历史版本比较、调用链与标准库实现追踪。未执行新测试，未将之前的 race、跨平台编译或覆盖率报告算作本轮结果。没有确认问题需要新增失败探针，也遵守了“不重跑关闭项和全量套件”的要求。

本轮审查时三个主文件 SHA-256 为：

```text
b80263773c4beb93b7803ab784bc586451d06f5a38788fb5e864d50a7676c50b  skills/parse.go
0b86260e033822e915ebb31bef78eb1be77e594843130ed1340639ccbe3c8ed1  skills/catalog.go
a3d80851091fac655d9e903977706b4a468832fa02b5840052e8aeb8e1e99d16  constants/skills.go
```

源码只读；工具目录没有独立 Read/Write、desktop-commander、sequential-thinking 或 shrimp-task-manager，本轮使用只读 shell、显式分析及结构化补丁工具写入本指定报告。用户限定交付路径优先于项目要求的其他日志路径，没有另写 `.claude/` 报告。

技术评价：代码质量 92/100、既有测试契约可读性 90/100、职责规范一致性 93/100；需求匹配 94/100、架构一致性 94/100、限制透明度 90/100；综合 92/100。评分仅为本轮静态架构判断，不代表重新测得覆盖率或完成发布验收。建议：架构/API 范围通过，保留上述可选改进和独立发布门禁。

_审查者：Codex（架构/API 角色）；本轮未开展新的独立安全审计。_
