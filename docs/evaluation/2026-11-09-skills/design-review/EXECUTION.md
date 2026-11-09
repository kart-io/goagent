# Skills 执行语义审查

日期：2026-11-09。范围：Core `tools/skill/*.go` 及其实际 Dispatcher / Builder 消费契约；Harness `preset/skills.go`、装配、生命周期与 Manifest 接线。

**最终结论：EXE-01 已由主审修复，并通过本角色新增的永久 Harness 回归关闭；本执行审查没有剩余确认缺陷。Harness 独立发布依赖仍是已知门禁。**

本轮首先读取 `docs/guides/SKILLS.md` 和前轮 `multi-agent-review/SUMMARY.md`。前轮六项关闭发现不重新打开，没有重跑其关闭探针或完整测试套件。两个仓库均未发现 `.claude/skills/` 或 `.agents/skills/` 项目技能目录。本报告不是独立安全审计，没有重试此前被拦截的安全角色任务。

初审仅写本报告及获准的 `/tmp` 探针；随后按用户明确授权，新增 `../goagent-harness/preset/skills_durable_test.go` 并更新本报告。本角色没有修改生产源码或模块文件，没有提交、推送或撤销其他角色编辑。主审同期完成的 YAML `!!binary` 修复由主审负责，不作为本角色新发现。

## 确认缺陷

### EXE-01 — P2，CLOSED：正文加载的已知资源枚举超限被记为执行失败，阻断持久运行中的模型反馈

以下位置和复现记录保留修复前事实；修复后证据见末尾“永久回归与关闭证据”。

**位置**

- Core `tools/skill/tool.go:62`：省略 `file` 时执行 `Catalog.Load`。
- Core `skills/catalog.go:149`：正文返回前枚举资源；`catalog.go:256`、`:268` 分别产生明确的条目数和深度限制错误，代码均为 `CodeInvalidInput`。
- Core `tools/skill/tool.go:100`：已知无效输入/权限类拒绝要求 `file != ""`；因此正文枚举错误经过 `expectedReferenceError` 后仍落入 `:107` 的 `ExecutionError`。
- 消费链：`tools/dispatcher.go:439`、`:461` → `core/durable/operation.go` → `builder/tool_events.go:471` 的 `errorFeedback`。

**触发条件**

有效技能 `demo/SKILL.md` 能正常发现，资源树包含以下任一情况：

1. 33 层子目录，超过 `SkillMaxResourceDepth=32`；或
2. 技能目录内有 `SKILL.md` 加 10000 个空参考文件，共 10001 个条目，超过资源枚举预算。

Harness `Build` 成功，因为发现阶段只读取技能文档，不枚举其整个资源树。随后模型调用 `load_skill({"name":"demo"})`，未指定 `file`。

**本轮实际证据**

使用真实两个 checkout、临时 `go.work`、真实 `preset.Build` / `Assembly.Tools` / 文件日志：

- 深度超限返回 `out == nil` 和包含 `skill resource depth limit exceeded` 的执行错误，日志是 `failed`，不是已完成的拒绝。
- 同一父执行键与 CallID 的三次显式调用，分别留下 `failed/attempts=1`、`2`、`3`；第四次返回 `*durable.OperationError`，需要宿主介入。单次 Invoke 内没有自动重试。
- 条目数超限同样返回执行错误，包含 `skill directory entry limit exceeded`，日志为 `failed/1`。
- 经 `preset.Configure` 的持久 Builder 在第一次模型工具调用后中止；脚本模型只收到一次请求，没有接收到错误工具消息。
- 相同深度超限在非持久 `preset.Configure` 路径中被 `WithToolErrorFeedback` 转为模型可见反馈，脚本模型收到第二次请求并完成。这解释了普通会话测试为何可能掩盖持久路径差异。

这不是前轮 EMFILE / EIO 分类问题的重跑。当前未知 I/O 默认保留执行错误的处理是正确的；本发现针对**已经识别、没有未知 I/O 分支的枚举限制**。Skills 指南承诺已识别的大小限制返回 `Rejected`，修复前正文分支没有兑现这个契约。

**影响**

一个可预期的技能资源限制使持久 Agent 失去告知模型并换用其他技能的机会，重复恢复还会消耗尝试次数。它不会突破资源限制，也不是无限重试问题。

**最小修改建议**

在现有取消、关闭和 `expectedReferenceError` 全错误树检查之后，为 `file == ""` 的已知枚举限制返回带明确原因的 `tools.Rejected`。可复用已识别的 `CodeInvalidInput` 分类，区分正文与引用文件的提示语；不要扩大为所有 `Load` 错误都拒绝，不要吞掉未知 I/O 或混合错误树中的操作故障，也不要放宽 Catalog 限制或返回截断列表。

增加一个经过实际 Dispatcher 日志的永久回归：已知正文枚举超限应为完成的拒绝，持久 Builder 应把它交给模型。深度和条目预算可作为同一分类的用例。生产修复由主审实施；本角色负责永久 Harness 回归与关闭验收。

## 接线与执行契约

**Connected：10 项关键连接；Orphaned：0（限以下检查范围）；Missing：0 个结构连接；原语义缺口 EXE-01 已关闭。** 没有为本次执行范围提供正式 REQ-ID 或对应阶段 SUMMARY，因此以下按实际契约映射，不编造里程碑编号。

| 提供端 | 实际消费端与用途 | 状态 |
|---|---|---|
| `constants.ToolLoadSkill` | Harness 工具名、提示词、默认规则与公共工具 | WIRED |
| `skills.Discover` | `buildSkills` 按来源顺序打开目录后调用 | WIRED |
| `Catalog.List` | Harness 提示词、Manifest 信息；适配器识别未知技能 | WIRED |
| `skill.New` | `a.AddTools` → Registry → Dispatcher | WIRED |
| `Catalog.Load` | `load_skill` 的 `instructions/files` 分支 | WIRED：EXE-01 已关闭 |
| `Catalog.ReadReference` | `load_skill` 的 `file/content` 分支 | WIRED |
| `Catalog.Close` | `a.OnClose` → Build 失败清理或 `Assembly.Close` | WIRED |
| `Dispatcher.Tools` | `Assembly.Tools`，保留模型参数错误恢复与持久能力 | WIRED |
| `preset.Configure` | Builder 系统提示词、工具、循环扩展与错误反馈 | WIRED |
| `Assembly.PinManifest` | 宿主显式入口；后台子任务执行入口实际调用 | WIRED；顶层 Configure 不自动固定 |

HTTP API：不适用。本次消费入口是 `load_skill` 工具。来源信任检查和工具 Pipeline 仍在 Harness 装配链上；没有新增授权测试或安全结论。

### 错误、持久化与重试

- 原始适配器执行失败可同时返回失败 `ToolOutput` 与非 nil error；Dispatcher 接到 error 后返回 nil 输出。这是框架契约，不是遗漏输出。
- `Rejected` 是框架认可的业务结果；经日志编码保存专门标记，恢复后仍可被 `RejectedResult` 识别。Builder 把其 JSON 内容交给模型。
- Dispatcher 默认 `MaxAttempts=3, RetryFailed=true`；再次显式调用才重试失败操作。Harness 配置的 `RetryTransient` 是模型请求重试，不是 Skills 文件读取重试。
- `OnResult` 在已写入日志之后运行。回调失败不撤销完成记录；下一次调用重放结果并再次交付回调。本轮实际验证了此路径。
- 非持久 `preset.Configure` 会把普通执行错误转成模型反馈；持久运行明确不做这种转换，避免模型历史与失败日志不一致。这是既有设计，EXE-01 曾在此边界暴露，现已修复。

### 取消与上下文（源码追踪，本轮没有新增取消探针）

适配器使用 Invoke 的 `ctx`，入口先检查取消；Catalog 在查找、遍历间隔和引用读取后检查。Dispatcher 替换 `ToolInput.Context`，使用派生调用上下文，执行后再次检查取消。取消、超时和关闭错误不会被适配器转为业务拒绝。

Dispatcher 的槽位等待与执行超时、durable 失败记录保存有独立约定；Skills 没有新增超时 goroutine。Catalog 的文件操作与 Close 由读写锁协调。单次本地文件系统调用和锁等待不承诺即时中断；Close 是资源释放，不是取消所有在途 Agent 的入口。这与现有指南的取消边界一致，未发现新的 Skills 专属连接缺口。

### 结果与模型上下文

正文返回 `name/instructions/files`，引用返回 `name/file/content`。适配器不自行添加系统提示词或 `AdditionalContext`。Harness 只在系统提示词展示目录元数据；Builder 将工具 Result 编成 `role=tool` 消息，保留模型 CallID。PostHook 如有附加上下文，由 Builder 在整批工具消息之后追加。

本轮实际验证了完整 Harness → Builder → Dispatcher → 模型工具消息 → 最终答案 → 持久重放。空资源列表在模型消息中保持 `[]`。日志 JSON 边界会把 Go 的具体容器类型转为解码后的通用类型；宿主应依赖结果字段/JSON 契约，不能假定所有执行路径均保持 `[]string`。

### 来源寿命

`buildSkills` 借用来源根目录到 Discover 返回并关闭它们；Catalog 持有选中技能的子目录句柄，cleanup 在空目录提前返回之前已注册。Build 失败路径与 Assembly 正常关闭都调用该 cleanup。Builder 借用工具，不替代宿主关闭 Catalog / Assembly。

本轮新增的持久验证表明：已完成结果在 Assembly 关闭后仍能重放；关闭后的新调用失败并记录为 failed；使用同一配置重建 Assembly 后，原失败调用显式恢复成功，尝试次数从 1 变成 2。这符合资源寿命与日志寿命相互独立的设计。前轮已经关闭的描述符泄漏、Build 各阶段失败清理、目录替换和并发取消探针没有重跑。

## 设计取舍（不是新缺陷）

1. **快照与实时资源并存。** 正文/元数据是 Discover 快照，资源列表和引用内容按新调用读取。日志重放返回原完成结果，不重新读取。已完成的缺失文件拒绝在文件后来创建后仍会重放；新 CallID 才看到新文件。本轮验证成功读同样是旧调用返回 `v1`、新调用返回 `v2`。仅提供 CallID 而没有日志/执行键不会形成跨调用缓存。
2. **Manifest 不冻结资源包。** Harness v3 的技能信息使用原始 SKILL.md SHA-256 前 12 位；参考资源不进入摘要。这与现有文档一致。`skill.New` 本身不声明 Catalog 内容版本，直接使用公共 Dispatcher 的宿主仍须自行固定来源/清单；不能将工具参数意图检查当成整包一致性保证。
3. **持久支持需要装配。** `skill.New` 返回基础 Tool，指南中的直接 `WithTools(tool)` 适用于普通调用。持久 Builder 要求 `Dispatcher.Tools` 等声明持久能力的工具，以及稳定 DurableScope。Harness 已提供前者；顶层宿主仍负责先调用 PinManifest 并管理 Assembly 生命周期。
4. **压缩后重载由宿主决定。** 没有 Skills 专用的加载状态缓存或自动重注入。工具结果作为普通对话消息进入压缩链；宿主可选择保留或再次加载。没有把这一已文档化职责判为丢失接线。

## 可选维护改进

- 给 Skills 指南补一个很短的持久调用说明：Dispatcher、DurableScope、宿主 PinManifest，以及完成拒绝重放/新 CallID 的差别。当前非持久示例有效，这不是阻塞缺陷。
- 将本轮“完成拒绝重放、资源变更后的新调用、失败读取恢复、回调失败后的再次交付”选成一个小型永久集成测试。现有 `tools/skill` 测试主要覆盖适配器，通用 Dispatcher 测试不能单独证明 Harness 的装配组合。后续授权已落实其中的枚举限制拒绝、日志重放与 Builder 反馈回归；其他场景未扩大为新的永久测试。

## 已知发布门禁

Harness 声明的 GoAgent 版本尚不包含新公共 Skills API。发布前仍须选择可获取的 Core 版本、更新 Harness 依赖并通过 `GOWORK=off` 验证。这是前轮既有门禁，本轮没有重复运行独立发布失败检查；临时 workspace 的成功不关闭该门禁。

v3 解析收紧、Manifest 从正文摘要切换为原始文档摘要及旧持久运行迁移要求已记录，不作为新兼容性缺陷。

## 本轮实际验证

临时目录：`/tmp/skills-execution-review-3qakozwn`。所有测试均调用真实仓库代码，不使用替代实现或覆盖源码；模型为确定性的本地脚本模型，没有模型服务请求。

| 新增探针 | 实际结果 |
|---|---|
| `TestDurableReadFreshnessAndRejections` | PASS：拒绝和成功结果按调用重放；新调用读最新资源；完成结果在 Close 后仍能重放 |
| `TestDurableSourceRecoveryAndResultDelivery` | PASS：重建 Assembly 恢复 failed/1 → completed/2；OnResult 失败不撤销 completed/1，回放后再次交付 |
| `TestBuilderInstructionReplayAfterResourceMutation` | PASS：完整 Builder 消息与答案链；资源变更和 Close 后恢复，无新增模型请求 |
| `TestEnumerationLimitExecutionClassification` | PASS：断言并复现 EXE-01，不代表产品契约通过；含非持久/持久 Builder 对照 |
| `TestEnumerationEntryLimitClassification` | PASS：第二种限制来源同样复现 EXE-01，failed/1 |

修复前按用户流程计：4 条新增执行组合完整（读取重放、失败恢复、结果回调恢复、Builder 消息与答案重放），1 条已知限制反馈流程存在断点。两个超限探针属于同一缺陷；修复后该断点由永久回归确认关闭。

命令一（当时文件含前四个顶层探针）：

```sh
env -u GOROOT GOWORK=/tmp/skills-execution-review-3qakozwn/go.work \
  go test -race -count=1 -timeout=60s -v \
  /tmp/skills-execution-review-3qakozwn/execution_test.go
# PASS; ok command-line-arguments 1.038s
```

命令二（只运行新增条目数限制验证）：

```sh
env -u GOROOT GOWORK=/tmp/skills-execution-review-3qakozwn/go.work \
  go test -race -count=1 -timeout=60s -v \
  /tmp/skills-execution-review-3qakozwn/execution_test.go \
  -run '^TestEnumerationEntryLimitClassification$'
# PASS; ok command-line-arguments 1.146s
```

收到主审 YAML `!!binary` 修复通知后，确认最新源码已包含拒绝分支，只复核普通文本的完整 Builder 流程与 EXE-01 条目数复现；未重跑 binary 修复测试或全量：

```sh
env -u GOROOT GOWORK=/tmp/skills-execution-review-3qakozwn/go.work \
  go test -race -count=1 -timeout=60s -v \
  /tmp/skills-execution-review-3qakozwn/execution_test.go \
  -run '^(TestBuilderInstructionReplayAfterResourceMutation|TestEnumerationEntryLimitClassification)$'
# PASS; ok command-line-arguments 1.144s
```

最新复核时 SHA-256：

```text
d184d7f3f8f28dc950c9375698c632ebd3634bdce5e9007cedfe0d0b53027d8d  core/codec/yaml/mapping.go
3636ef245106ce3c6079f3b9e5fcb17e64c607f622fe1d3d21eab1045e2d5e59  core/docs/guides/SKILLS.md
5376764841b9e48c5b7b1ac73dc0dadad48aa891ae282d2910bdeefa9b8e40da  core/tools/skill/tool.go
0b86260e033822e915ebb31bef78eb1be77e594843130ed1340639ccbe3c8ed1  core/skills/catalog.go
b7265272b0c262b9f60fc30fbd1bca249d2919706dacc6ec69186184272a04e8  harness/preset/skills.go
e04255be05166567eb638f19628098d3808ae7a19ac6fb5575ff5a9749f9fb2d  harness/preset/build.go
a087ccce332d6f975bce609f667529d45c05733b7a7274a7ee4ea2d8ff1371e4  temporary/execution_test.go
```

## Requirements Integration Map

正式 REQ-ID 未提供；这里映射本次分派的执行要求。

| 执行要求 | 集成路径 | 状态 | 问题或边界 |
|---|---|---|---|
| 业务拒绝与执行失败区分 | Catalog 错误 → skill 适配器 → Dispatcher → Builder | WIRED | EXE-01 已修复，永久回归通过 |
| 持久化与恢复 | ToolOutput → journal 编解码 → replay → OnResult / 模型消息 | WIRED | 成功与拒绝完成；执行失败可显式恢复 |
| 调用重试与缓存 | CallID/执行键 → durable.Do；新调用 → Catalog | WIRED | 无隐式参数缓存；旧完成结果不会刷新 |
| 取消与超时 | 宿主 ctx → Dispatcher → Catalog 检查 → 执行错误 | WIRED（源码追踪） | 单次文件系统调用不保证即时中断 |
| 结果与上下文形状 | skill Result → Builder tool 消息 → 模型 | WIRED | 没有自动注入或压缩后保留保证 |
| 来源寿命 | roots → Discover → Catalog → OnClose / Assembly | WIRED | 日志重放寿命独立于已关闭来源 |
| 版本一致性 | SKILL.md 摘要 → Manifest → 宿主 PinManifest | WIRED | v3 已文档化；参考文件不固定 |
| 独立发布 | Core 可获取版本 → Harness go.mod → GOWORK=off | PARTIAL | 已知发布门禁，未重跑 |

上述执行要求均有跨层接线，没有本范围内的“仅单阶段、无集成接点”项。解析器内部完整性、安全审计、真实模型/远程文件系统、整个 Dispatcher 的历史缺陷扫描均不属于本轮新增验证。按主审要求结束扩展；后续仅执行用户授权的 EXE-01 永久回归与关闭验收，证据如下。


## 永久回归与关闭证据（后续明确授权）

**EXE-01：CLOSED。** 主审在 `tools/skill/tool.go` 的完整错误树检查之后增加 `file == "" && CodeInvalidInput` 的 `Rejected` 分支，附具体限制原因。本角色读取确认分支位置；取消、关闭和未知/混合错误的前置检查仍位于该分支之前。主审另行负责私有错误分类回归，本角色没有运行或声称独立验收这些私有测试。

本角色新增的唯一仓库测试文件是 Harness `preset/skills_durable_test.go`。它复用已成熟临时探针的装配、文件日志和脚本模型方法，仅包含一个表驱动入口 `TestSkillsEnumerationLimitsDurableRejection`：

| 场景 | 真实 Dispatcher + 文件日志 | 持久 Builder + 模型反馈 |
|---|---|---|
| 深度超过 32 层 | nil error，`RejectedResult=true`，原因正确，`completed/attempts=1`；Close 后重放保持拒绝与次数 | 收到 `role=tool`、CallID `c1`、JSON `error=rejected` 和具体原因；继续得到最终答案；工具日志 `completed/1` |
| SKILL.md 加 10000 个参考文件 | 同上，条目数超限正确分类；Close 后仍可重放 | 同上，收到条目数限制反馈后正常完成 |

Builder 两个场景都在 Assembly Close 后再次 Invoke，确认答案重放、没有第三次模型请求，工具日志仍为 `completed/1`。这同时排除了“把失败转成未入日志的反馈”或“恢复时重新读取已关闭目录”造成的假通过。脚本模型是本地确定性桩，无外部模型调用。

本次永久测试直接针对主审已经落盘的生产修复运行，没有回退或覆盖共享工作区源码。修复前红灯依据为本报告保留的真实 Harness 临时复现；主审另报告私有 `TestInstructionLimitsAreCompletedRejections` 已对旧实现验证 exit 1，这项信息仅归属于主审，不冒称本角色重新运行了永久 Harness 测试的旧版本。

仅运行新增定向测试（从 Harness checkout）：

```sh
env -u GOROOT GOWORK=/tmp/skills-execution-review-3qakozwn/go.work \
  go test -race -count=1 -timeout=60s -v ./preset \
  -run '^TestSkillsEnumerationLimitsDurableRejection$'
```

结果：**PASS，exit 0，`ok github.com/kart-io/goagent-harness/preset 1.190s`**。四个叶子子测试（depth/dispatcher、depth/builder、entries/dispatcher、entries/builder）全部通过；顶层测试执行约 0.18 秒，条目场景约 0.16 秒。未运行完整套件、既有关闭探针或其他历史 Dispatcher 测试。

关闭验收源码 SHA-256（取代上文对应的修复前指纹）：

```text
ee2ac6b13186c2590d577475a1d0524b500e6607535c73544fc77b5dad9868a1  core/tools/skill/tool.go
b6671a6950ecbfbb8af36fe99779ef1e9fba51ef6cdad1ae6e9e0298dee2189f  harness/preset/skills_durable_test.go
```

此次授权后的写入范围严格限定为上述 Harness 测试与本报告；生产修复、私有分类测试及其运行归主审所有。已知发布门禁和文档化设计取舍不因这次关闭而改变。


### 仓库语言惯例与归档日志

按主审反馈，新增测试的注释、断言说明及模型桩诊断均改为简体中文。核对同目录 `mcp_test.go`、`b_test.go`、`background_lifecycle_test.go` 后，继续沿用 Harness 的标准库 `encoding/json`；没有切换 JSON 实现或改变测试逻辑。

仅重跑新增入口 `TestSkillsEnumerationLimitsDurableRejection`（`-race -count=1 -timeout=60s -v`，同一临时 go.work）。四个叶子场景全部通过，exit 0。完整 stdout/stderr 已保存，可供主审直接归档：

`/tmp/skills-execution-review-3qakozwn/harness-skills-durable-final.log`

该文件是实际测试输出日志，非手写摘要；测试内部 `durable.FileStore` 使用 `t.TempDir()`，其操作记录已由断言检查，测试结束后按 Go 测试约定清理。

最终测试输出：

```text
ok  	github.com/kart-io/goagent-harness/preset	1.173s
```

本次语言调整后的最终文件指纹（取代前一节对应测试文件指纹）：

```text
1fff9c72cdcb54624b8b28858adee5fb92d77b67f0b1481a9c73b6474fc0cf2f  harness/preset/skills_durable_test.go
34b075d6dec7633e653d826bbfad6b43d88a59604f52f7d3763e3fec8a6d698e  /tmp/skills-execution-review-3qakozwn/harness-skills-durable-final.log
```

EXE-01 保持 CLOSED；未运行全量或扩展探针范围。
