# Skills 抽取：可维护性与验证审查

日期：2026-11-09。审查者：Codex（本轮验证与可维护性视角）。

## 授权补测后的最终结论

**VAL-01 已由主审修复并验证；COV-01 已由本视角补充永久测试并通过定向 race；COV-02 的空数组契约已由双方补齐。未发现需要额外修改 Catalog 生产实现的缺陷。剩余精确字节／深度接受边界及生命周期测试强化为可选后续项，不再把本轮已补齐项目列为开放缺口。**

初审之后，用户授权本视角只写 `skills/catalog_budget_test.go` 与本报告。实际仅新增该测试文件、对其 gofmt，并更新本报告；没有修改生产源码、其他测试、go.mod 或他人文件，没有提交。主审的并行修改不属于本视角的改动。

### 本视角完成的永久测试

复用现有 `skills_test` 的 `writeSkill`、`openRoot`、`discover`，新增四个测试及文件内非公开辅助函数，没有新增公共接口：

| 新测试 | 真实夹具与决定性断言 | 本轮结果 |
|---|---|---|
| `TestCatalogBudgetDiscoverEntriesAcrossSources` | 两个来源合计 10000 条目成功，包含普通文件、无 SKILL.md 的目录和两个有效技能；第二来源增加一个文件后合计 10001，要求 InvalidInput 和明确条目超限错误，返回 Catalog 为 nil | PASS，0.16 秒 |
| `TestCatalogBudgetResourceEntriesAcrossDirectories` | SKILL.md、两个子目录及 9997 个资源合计 10000 条目，完整比较全部 Files；连续两次 Load 成功；后一个子目录增加文件后合计 10001，要求条目超限与零值 Skill；移除新增文件后恢复完整列表 | PASS，0.22 秒 |
| `TestCatalogBudgetUniqueSkillsAcrossSources` | 两个来源合计 2000 个唯一技能，另含一个重复技能；比对全部名称和描述，确认第一来源正文优先；关闭成功 Catalog 后增加第 2001 个唯一技能，要求 InvalidInput 和技能数量超限，返回 nil Catalog | PASS，0.86 秒 |
| `TestCatalogBudgetEmptyFilesNonNil` | 只有 SKILL.md，Load 的名称、正文正确，Files 同时满足非 nil 和长度为零 | PASS，0.00 秒 |

失败场景都发生在此前已产生可用技能或资源之后，断言不会返回部分结果；数量测试的总扫描条目远低于 10000，避免由其他预算提前拒绝。资源测试也直接保护每次 Load 重置预算以及失败后不污染下一次枚举。

宿主 `ulimit -n` 为 **524288**，本轮真实执行数量边界，没有任何 skip。测试在其他宿主遇到 `errors.Is(err, syscall.EMFILE/ENFILE)` 时会明确 skip，说明无法验证同时持有 2001 个技能目录；不会把系统句柄耗尽断言为数量限制通过。其他错误仍失败。

本视角唯一执行的 Go 测试命令：

```sh
env -u GOROOT go test -race -count=1 -timeout=90s ./skills -run '^TestCatalogBudget' -v
```

退出码 **0**；四项均 PASS；包级输出 `ok github.com/kart-io/goagent/skills 2.249s`。没有重跑原有测试、全量套件、工具包、Harness、模型或安全探针，也没有重新计算覆盖率。

### 关闭项、验收归属与剩余范围

| 项目 | 最终状态 | 证据与责任归属 |
|---|---|---|
| VAL-01 验证命令掩盖失败 | **已关闭，主审验收** | 主审报告迁移命令加入子 shell、`set -euo pipefail`、`trap`，对文档原块注入测试退出 23 后，整体实际退出 23；证据为 [command-exit-check.json](command-exit-check.json)。本视角未重复执行该模拟 |
| COV-01 三类预算及无部分结果 | **已关闭，本视角验收** | 本文件上表前三个永久测试，真实阈值、跨来源／跨子目录、重复不增加唯一数量，定向 race/count=1 全部通过 |
| COV-02 只含 SKILL.md 的空数组契约 | **已关闭，双方分层验收** | 本视角证明 Catalog Files 非 nil 空切片；主审补入工具 `TestEmptyResourcesEncodeAsArray` 并报告工具整包 race 通过，覆盖实际序列化。工具用例及其执行由主审负责，本视角未重跑 |
| COV-02 精确字节／深度接受边界 | **可选剩余** | 恰好 1 MiB 有效 SKILL.md、4 MiB 引用、32 层接受尚未由本文件补测；超限拒绝仍有既有测试。不是本轮授权的优先预算范围 |
| COV-03 生命周期断言强化 | **可选剩余** | 本视角未补确定性读/Close 重叠及 Harness 关闭原因测试，没有生产故障证据；另一 agent 的 Harness durable 回归由主审汇总，不在此重复认领 |
| codec `!!binary` 修复 | **主审完成并验收** | 保持下文记录的静态核对与验收归属，本视角未重跑 |
| Harness 独立发布依赖 | **已知发布门禁仍独立管理** | 不因本次 Catalog 测试通过而改变发布结论 |

本次测试盘点在初审清单基础上增加 **1 个 Core 测试文件、4 个 Test 函数**；主审在工具测试中增加的空数组用例另行归属，不混入本视角数量。没有提出新的生产修改请求，无阻塞。下文保留初审证据和当时缺口，开放状态以本节为准。

---

## 初审记录（补测前）

**结论：未确认新增生产代码缺陷；确认 1 项验证文档缺陷，并发现永久测试覆盖缺口。不能以已有语句覆盖率认定所有验收边界已覆盖。建议修改验证文档并补充定向测试；本报告没有提出必须修改生产实现的结论。**

本轮只写本文件，没有修改源码、测试、依赖或其他报告，没有提交或推送。没有重跑已关闭探针、全量测试、模型调用或安全探针，也没有重新开展独立安全审计。主审最后要求立即收敛后，未再搜索或测试。

## 范围与证据有效期

首先读取 `docs/guides/SKILLS.md` 和上轮 `multi-agent-review/SUMMARY.md`，再核对当前源码、测试断言、文档差异及存档覆盖率。重点为 Core `skills/`、`tools/skill/`、`codec/yaml/mapping*`、`examples/skills/03-catalog/`、导入门禁，以及 Harness `preset` 的 Skills 测试与迁移文档。

- Core 基线：`d010f039610aecbc57c0b0c261555fa2765c5f76`；Harness 基线：上轮记录的 `912bca0a05c11c204b5cad695f0bd85fc0b10b5b`。
- **主审本轮 codec 修复前**，实际逐文件 SHA-256 校验 `multi-agent-review/FINAL_SOURCES.json`：Core 27/27、Harness 8/8 匹配。因此可将存档覆盖率用于当时源码的覆盖分析，不能把它当作本轮新增代码的测试结果。
- 存档 `statements.coverage.gz` 解压后 SHA-256 为 `3611593cfc2cff104a4ae4269f233d614f0ba8e3658c31a9caedf320d83c32f9`，与 `final-checks.json` 相符。
- 主审告知 codec 修复及受影响五包 race/vet/import 检查已完成后，本视角读取了新增 `!!binary` 拒绝逻辑、codec 测试、Parse 两个负例和指南更新。**下述 codec 测试盘点包含这些新增内容；没有独立重跑或声称旧哈希仍覆盖修复后文件。**
- 两仓均未发现 `.claude/skills/` 或 `.agents/skills/` 项目技能。已读 Core `CLAUDE.md`；其指定的专用本地分析工具当前不可用，使用只读 shell、文本检索和 Python 读取存档补偿。未生成额外流程报告。

## 确认缺陷

### VAL-01／P3：Core 迁移指南的验证命令块可能失败后仍退出 0

**分类：确认的验证文档缺陷，不是 Skills 运行时缺陷。**

**位置：** `docs/guides/HARNESS_MIGRATION.md:32` 的 Bash 代码块，特别是第 37 行测试、第 38 行构建和第 39 行清理。

**触发：** 使用默认未启用 `errexit` 的 Bash，将代码块作为一个脚本执行；测试失败，而后面的构建和清理成功。命令块没有 `set -e`、显式状态检查或失败传播，最终状态取自 `rm`。

**证据：** 原文连续执行 `go test -race ./...`、`go build ./...`、`rm -rf "$workspace_dir"`。本轮唯一行为模拟使用 Bash 函数替代 `go` 和 `rm`，不运行 Go、不写文件、不执行删除：模拟测试返回 23，构建返回 0，清理返回 0；整体实测退出码为 0。该模拟确认命令序列的状态传播问题，不代表实际 Go 测试失败。

**最小修改：** 复用 Harness `README.md` 已有的括号子 shell、`set -euo pipefail` 和 `trap` 清理模式，或显式保存并返回失败状态。保持临时 workspace 的验证范围不变。

**是否需改源码：** 否，只需改文档命令。本轮未修改。

## 已确认的覆盖缺口

以下是测试证据缺口，**没有据此推断生产实现已失效**。

### COV-01／优先补齐：三个条目／数量预算没有永久边界测试

**位置：** `constants/skills.go:7`、`skills/catalog.go:50`、`:84`、`:148`、`:245`；当前限额测试为 `skills/catalog_test.go:185`。

**契约：** 发现过程总共最多 10000 条目，每次资源枚举最多 10000 条目，最多 2000 个选中技能；超限报错，不截断。来源之间共享发现预算，资源子目录之间共享单次枚举预算。

**证据：** 盘点全部相关测试后，仅发现文档字节、引用字节和深度超限用例，没有 `SkillMaxEntries`、`SkillMaxCount` 对应的测试或等价大目录夹具。与该源码匹配的存档覆盖率中，`catalog.go:85` 的数量超限返回和 `catalog.go:256` 的条目超限返回执行次数均为 **0**。

**会漏掉的具体回归：** 删除数量检查、把预算按来源／子目录重置、在恰好达到上限时误拒绝，或静默截断，都缺少直接验收断言。现有 93.1% 包语句覆盖率不能排除这些回归。

**最小补测：**

1. 发现 10000 条目成功、10001 条目报错；跨两个来源合计超限，确认预算没有重置。普通文件及无 SKILL.md 的目录也计入扫描预算。
2. 资源树合计 10000 条目成功、10001 条目报错；分散到多个子目录，确认按整个树累计。计数包含目录和 SKILL.md，不能把“返回的文件数”误当扫描条目数。
3. 2000 个唯一有效技能成功、2001 个报错；同名遮蔽不增加选中数量。若环境句柄上限不足，应明确标记环境前提，不能以 EMFILE 代替数量超限断言。
4. 错误时断言不返回部分 Catalog／部分资源结果。无需为此引入新的公开配置接口。

**是否需改源码：** 当前没有生产修改依据；需新增永久测试。尚未执行这些补测。

### COV-02：空资源数组及若干接受边界缺少直接断言

**位置：** `docs/guides/SKILLS.md:80`、`skills/catalog.go:147`、`tools/skill/tool_test.go:46`、Harness `preset/b_test.go:175`。

只含 SKILL.md 时，当前实现初始化非 nil 空切片，符合 `files: []`。但 Core 工具结果形状测试使用有两个资源的夹具；Harness 无资源的隐藏技能只断言 `instructions`。Parse 的 `require.Empty(s.Files)` 不覆盖 Catalog 或工具序列化，也不能区分 nil 和空切片。上轮临时消费者探针已经观察过 `[]`，因此不是重新打开旧缺陷；缺的是永久回归。

**最小补测：** 一个仅含 SKILL.md 的技能，经工具 Invoke 后序列化，断言 `files` 是 `[]` 而不是 `null`。可同时验证 Load 返回非 nil 空切片。

此外，现有字节与深度测试只有超限拒绝，没有恰好 1 MiB 的有效 SKILL.md、恰好 4 MiB 引用和恰好 32 层的接受用例；可在现有表驱动测试中补充，避免边界比较符回归。

**是否需改源码：** 否，当前行为从源码可见；需要补测试。

### COV-03：并发与关闭断言不足以证明所有生命周期声明

**位置：** Core `skills/catalog_test.go:149`；Harness `preset/boundary_audit_test.go:81`。

Core 的 `TestCatalogContextAndConcurrentClose` 启动工作线程后立刻 Close，线程允许每次都得到 `fs.ErrClosed`。存在所有线程在关闭后才开始的合法调度，因此测试通过不能证明读操作与 Close 实际重叠，也没有验证成功读取时的正文／资源内容。这是非确定性的覆盖限制，未发现实际锁错误。

Harness 关闭后断言为 `if err == nil { require.True(rejected) }`，任何非 nil 错误均通过，不能区分预期关闭原因与无关 I/O 错误；这段弱断言是修改文件中保留的既有代码，不是本轮抽取新增的生产回归。通用 `TestBuildFailureClosesResources` 使用测试组件，不能单独证明 Skills 在后续组件失败时释放具体文件句柄。

**最小补强：** 并发测试至少同步确认关闭前发生成功读取并校验内容；若要声明覆盖真正重叠，需进一步同步到读操作已持有资源的阶段，启动屏障本身不足。Harness 在现有 Dispatcher 错误传播契约允许时断言 `errors.Is(err, fs.ErrClosed)`，并检查没有内容返回；需要证明装配失败后的具体资源释放时，再加一条 Skills + 后续失败组件的定向测试。

**是否需改源码：** 没有证据要求修改生命周期实现；属于测试强化。

## 新增／修改测试完整盘点

Core 本次抽取涉及 **10 个测试文件**：9 个新增、1 个修改。包括主审刚加入的 codec 回归后，新增文件中共有 **34 个 Test 函数及 1 个 Fuzz 函数**；修改文件只变更一个现有测试中的导入规则表。Harness 为 **2 个新增、2 个修改测试文件**。函数数量不是覆盖率。

| 文件 | 新增／修改内容与实际断言 | 评价 |
|---|---|---|
| Core `skills/parse_test.go` | 4 Test + FuzzParse；字段、原文 digest、BOM/CRLF、类型/null/长度拒绝、小别名、展开拒绝、边界长度、正文分隔；新增二进制说明和 metadata 负例 | 正反例有价值；Fuzz 只断言成功结果名称、描述非空及 digest，不能证明语义完整性；没有把 fuzz 次数当边界验收 |
| Core `skills/catalog_test.go` | 5 Test；来源优先级、借用 root、深拷贝、缓存正文/实时引用、关闭、未知技能、目录保持、无效元数据、取消、并发、字节/深度限制 | 主要契约覆盖；缺口见 COV-01～03；资源列表新增/删除后的刷新、单目录完整排序尚无专门断言 |
| Core `skills/catalog_linux_test.go` | 2 Test；FIFO 无写入者拒绝、发现失败前后 fd 数量 | 保留；失败清理夹具有效，首个 `a-valid` 的名称与目录一致，辅助函数第四参数是 description。本轮排除了最初的夹具误读 |
| Core `skills/discovery_linux_test.go` | 1 Test；列举之后目录被替换，调用实际 discoverSkill 获取路径 | 确定性控制变更时点，比随机竞态测试更有价值；本轮只读，没有运行 |
| Core `tools/skill/tool_test.go` | 6 Test；完整结果形状、良性路径拼写、脚本不执行、参数类型、未知/缺失、取消/关闭、nil Catalog、原始二进制读取与工具拒绝、超限原因 | 上轮关闭项已有永久断言，未重跑；缺空资源序列化用例 |
| Core `tools/skill/errors_test.go` | 2 Test；EMFILE/ENFILE/EIO/未知原因；请求错误与 EIO 两种 Join 顺序及包装 | 直接保护错误树分类，值得保留；不能代替真实系统错误链测试 |
| Core `tools/skill/resource_linux_test.go` | 1 Test；隔离子进程耗尽描述符，确认确实 EMFILE，恢复后成功 | 与纯分类测试层次不同，不是无意义重复；本轮未执行 |
| Core `codec/yaml/mapping_test.go` | 原 10 Test + 新增 RejectsBinaryScalars；类型、各层键、循环/重复/单文档、别名预算、指数预算共享、普通数值兼容、精确预算；新增普通/非 UTF-8 binary、嵌套、序列、别名及旧 Unmarshal 行为 | codec 的预算边界断言比 Catalog 完整；新增 binary 用例已读，执行结果归主审 |
| Core `examples/skills/03-catalog/main_test.go` | 2 Test；缺失引用使 run 返回拒绝错误、仓内 fixture 的 run 成功 | 足以保护 run 的错误返回；不是 stdout 内容或进程退出码的永久端到端断言 |
| Core `scripts/apicheck/main_test.go` | 修改 TestImportRulesUseCurrentModuleAndLayeredExceptions；合法 codec/errors/constants/标准库、禁止 framework/第三方、core→skills 禁止、tool→skills 允许 | 与实现边界相符；未把上轮可选 Builder 禁入门禁再报为缺陷 |
| Harness `preset/skills_test.go` | 新增 TestSkillsManifestTracksRawMetadata；组件 v3、实际原文 SHA 前 12 位、不变重建可 Pin、metadata-only 改变使 Pin 失败 | 真实 durable store 与实际 Manifest，永久覆盖较强 |
| Harness `preset/skills_linux_test.go` | 新增来源 FIFO 测试；默认工作区来源、绝对路径与 home 路径 | 平台特定测试，保留；本轮未运行 |
| Harness `preset/b_test.go` | 修改 TestSkills；新增隐藏技能仍按名称可调用，更新路径拒绝原因；已有默认来源、覆盖、提示词、正文/引用、无效文档和授权需求断言 | 共享工具通过实际 Assembly 使用；没有为每个 Core parser 负例重复 Build 测试的必要 |
| Harness `preset/boundary_audit_test.go` | 修改越界引用错误断言，检查 PathError 与无内容；其余 Skills 目录、文件与生命周期测试保留 | 本轮只检查断言质量；关闭后断言仍较弱，见 COV-03 |

未修改的 `codec/yaml/yaml_test.go`、`fuzz_test.go` 与既存 fuzz corpus 是共享 codec 的辅助兼容证据，不算本次新增测试。Harness `preset/preset_test.go` 的通用清理测试也是既有辅助证据。

## 验收契约与永久覆盖对照

| 契约 | 永久证据 | 尚缺／限制 |
|---|---|---|
| 名称 64、描述 1024、compatibility 500 边界 | ParseBoundariesAndBodySeparators + malformed 表 | 描述有 Unicode 边界；compatibility 的等长接受用例是 ASCII |
| 1 MiB SKILL.md、4 MiB 引用、32 层 | CatalogFileAndTraversalLimits，Parse over byte limit，工具超限拒绝 | 只有超限，缺等于上限成功 |
| 发现条目、资源条目、唯一技能数量上限 | 当前源码存在检查 | 三类边界无永久用例；存档对应超限分支计数 0 |
| YAML 输入/展开预算 | DecodeMappingByteBudgets、FloatExponentBudgetIsShared 等 | 精确相邻预算、小别名成功、累计超限均有断言；不能移用为文件枚举预算证据 |
| 原文 digest 与 metadata-only Manifest 改变 | ParseMetadataAndExactDigest、Harness SkillsManifestTracksRawMetadata | 已形成永久证据；无需重跑旧 probe |
| 正文缓存、引用实时读、来源根借用、深拷贝 | CatalogPrecedenceSnapshotsAndOwnership | 未专门验证新增/删除资源后的 Files 刷新 |
| 无技能、空文件列表、只有隐藏技能 | 部分空目录与隐藏技能路径存在 | 缺明确的空工具 JSON 形状；Harness 全部来源缺失／全部隐藏的完整装配契约可选补充 |
| 已取消／截止日期／已关闭 | Core Catalog 与工具测试 | 中途取消、读取完成后取消分支缺定向测试；存档 catalog.go:185、:265、:276 为 0 |
| 工具业务拒绝与执行错误分离 | 工具表驱动、错误树、实际 EMFILE 子进程 | 不把未模拟的权限故障、所有系统 I/O 组合声称为已覆盖 |
| 示例三级展示与失败传播 | 两个 run 测试，历史实际示例日志 | 删除 fmt 输出仍可能通过 run 测试；当前应称冒烟测试，若要求输出契约再增加 writer 捕获断言 |

## 文档、示例及验收资料盘点

| 文件／文件组 | 变更与审查结论 |
|---|---|
| Core `README.md`、`README_CN.md` | 修改能力入口与 Harness 装配定位；链接到 Skills 指南，与实际拆分一致 |
| Core `docs/README.md`、`docs/architecture/ARCHITECTURE.md` | 增加指南索引与组件职责行；没有把 Catalog 接入执行内核 |
| Core `docs/guides/SKILLS.md` | 新公共指南：所有权、快照/实时资源、限额、错误、宿主职责、v3 迁移、发布限制；最后已加入 binary 拒绝说明。契约总体与源码一致，但不能把契约陈述当测试证明 |
| Core `docs/guides/HARNESS_MIGRATION.md` | 新 workspace 方法与独立发布条件；VAL-01 的失败状态传播需修正文档 |
| Core `examples/README.md` | 新案例入口；01-demo 和 02-weather-skill 属既有例子，未算入新增实现 |
| Core `examples/skills/03-catalog/README.md` | 明确无模型、无 Harness、固定 faq、宿主生命周期；可选补充资源名也固定为 references/refunds.md |
| Core `examples/skills/03-catalog/main.go` | 新可运行例子；真实 Discover→List→Load→工具 ReadReference，检查 Rejected 与 Success 后再输出 JSON，无占位调用 |
| Core `examples/skills/03-catalog/fixtures/faq/SKILL.md`、`fixtures/faq/references/refunds.md` | 新合成说明与引用，version 为字符串、相对引用匹配；合成内容已明示，不是伪生产数据 |
| Harness `README.md`、`docs/MIGRATION.md` | 修改实际依赖、Go 工具链、临时 workspace 与发布门禁；README 的 fail-fast 子 shell 可直接供 VAL-01 复用 |
| Harness `preset/README.md` | 更新组件 v3、原文摘要、类型/长度迁移、工具结果及宿主边界；空数组变化已披露，不能再报“未记录变更” |
| 验收 `REPORT.md` | 区分本地组合与独立发布，交叉编译没有冒充平台运行；覆盖率数字是历史语句覆盖率，不是全部接受边界比例 |
| 上轮 `multi-agent-review/ARCHITECTURE.md`、`INTEGRATION.md`、`VERIFICATION.md`、`SUMMARY.md` | 记录关闭项及历史过程；仅将其用于排除重复问题和定位证据，没有替代源码核对 |
| 上轮 `multi-agent-review/RELEASE.md`、`SECURITY.md` | 前者记录未通过的独立依赖门禁，后者记录安全视角未完成。本轮未改变其结论，也未重试安全任务 |

配套非文档证据也纳入盘点：根目录 `SOURCE_SHA256SUMS`、`HARNESS_SOURCE_SHA256SUMS`、`checks.json`、`harness-checks.json`、`core-race.log`、`harness-race.log`、`example.log`、`fuzz.txt`；上轮目录 `BASELINE.json`、`FINAL_SOURCES.json`、`ci-checks.json`、`final-checks.json`、`harness-checks.json`、`release-check.json`、`core-race-coverage.log`、`harness-race.log`、`coverage-gates.txt`、`statements.coverage.gz`、`frameworkbench.txt`、`example.log`。本轮读取了其中与结论相关的清单、最终结果、日志包数和压缩覆盖率；没有逐条重新执行这些命令。

存档日志包数与汇总相符：Core 72、Harness 11。`coverage-gates.txt` 的门槛列表没有 Skills 专属门槛；“全部覆盖率门禁通过”确实只表示已有门禁通过，不等于所有 Skills 验收边界通过。无需追求 100% 语句覆盖来解决 COV-01，优先补足实际边界断言。

## 设计取舍、可选改进与已知发布门禁

**设计取舍，不列新增缺陷：** 主审已核对官方规范与集成指南，三层披露契合，允许发现时缓存正文；ASCII 名称范围已明确披露；codec 两次解析是复用精确数字转换的维护权衡，本轮不建议重构。正文快照与实时资源、资源不计入 digest、隐藏提示词不等于禁止加载等均已披露，并有对应实现。

**测试可维护性：** 没有足够证据建议删除任何新增测试。Core 单元、工具契约、Harness 装配测试验证不同层次，不因都读同一文件就算重复。codec 的 59/60 与 2489/2490 精确预算用例关联公开注释中的计费算法，不是无意义地照抄实现。EMFILE 子进程和纯错误树测试也分别证明真实错误来源与组合分类。

**可选改进：** 若示例 stdout 是验收契约，使用 io.Writer 捕获而不是全局 stdout 或新模型测试；若需永久保护 Builder 消费者路径，将上轮无模型消费者场景压缩成一个最小用例。不要复制整套 parser 负例到 Harness，也不要为每个文档句子加机械测试。

**已知发布门禁：** Harness 声明依赖尚不含新公共包，需 Core 可获取版本、Harness 依赖更新及 `GOWORK=off` 独立验证。沿用上轮已确认结论，本轮没有重跑，也不算新增缺陷。Windows/macOS 交叉编译不等同运行验证；Docker/Postgres 与独立安全视角的限制保持原状。

## 主审 codec 修复与未完成项

主审本轮确认并修复 `!!binary` 经共享 JSON 转换的问题，本报告不重复认领该发现。源码核对可见 `mapping.go` 在标量转换前拒绝 binary 标签；新增测试包含 UTF-8 binary、非 UTF-8 binary、嵌套值、序列、别名，并检查通用 Unmarshal 保持旧语义；Parse 加入说明和 metadata 两个回归。断言与“严格入口拒绝、通用入口不变”的修复目标一致。受影响五包 race/vet/import 通过由主审报告，本视角没有独立执行这些检查。

未完成：COV-01～03 和上述可选用例尚未补写或运行；没有以 mutation testing 实测删除保护后的结果，没有做目标平台运行验证，也没有独立复核主审新测试的执行日志。因此本报告不宣称覆盖完整、全部阶段验收通过或最新全仓通过。没有阻塞本报告交付的工具或资料问题。

**交给主审的动作：** 修正 VAL-01 的文档命令；优先补 COV-01 的数量／条目边界，再补空数组与精确接受边界。生命周期断言按实际承诺加强。当前证据不要求额外修改生产源码，不需要重跑已关闭探针或重复全量测试。
