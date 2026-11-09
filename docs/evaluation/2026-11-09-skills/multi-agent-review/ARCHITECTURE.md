---
phase: skills-extraction
reviewed: 2026-11-09T07:50:32Z
resolved_findings:
  - AR-01
  - AR-02
open_findings: []
depth: deep
role: architecture-public-api
files_reviewed: 15
files_reviewed_list:
  - skills/parse.go
  - skills/catalog.go
  - skills/parse_test.go
  - skills/catalog_test.go
  - tools/skill/tool.go
  - tools/skill/tool_test.go
  - tools/skill/errors_test.go
  - tools/skill/resource_linux_test.go
  - ../goagent-harness/preset/boundary_audit_test.go
  - constants/skills.go
  - scripts/apicheck/main.go
  - scripts/apicheck/main_test.go
  - examples/skills/03-catalog/main.go
  - docs/guides/SKILLS.md
  - ../goagent-harness/preset/skills.go
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
defects:
  P1: 0
  P2: 0
  P3: 0
optional_improvements: 1
status: clean
---

# Skills 架构与公共 API 审查

审查当前未提交源码；没有沿用旧报告的“通过”结论。复核已关闭 **AR-01（EMFILE）** 和 **AR-02（联合错误）**，当前无剩余真实缺陷；另列 **1 个 P3 可选门禁加固建议（未实施）**。原 REPORT.md 的九个已关闭问题不重复列报。独立 Harness 的版本缺包属于已知发布门禁，单独列在限制部分。

用户指定的交付物为本文件；没有修改生产源码、测试、模块文件或 Git 状态。临时探针仅位于 /tmp。没有运行全量测试或真实模型。

## 一、已关闭缺陷与复核证据

### AR-01 — P2：读取引用文件时把可恢复系统错误伪装成文件不存在（已解决；以下保留初审证据）

**文件：** /home/hellotalk/code/go/src/github.com/kart-io/goagent/tools/skill/tool.go:89–90。

**具体触发：** Catalog 已正常发现技能及存在的 ref.txt；宿主进程达到文件描述符上限。通过可选工具调用 load_skill，参数为 name=demo、file=ref.txt。

**实际行为：**

- Catalog.ReadReference 保留底层 EMFILE，errors.Is(err, syscall.EMFILE) 为 true。
- 适配器在排除取消和关闭后，只判断 file 非空，返回 tools.Rejected("skill demo has no file ref.txt", nil)、nil。
- 原始 error 被丢弃，宿主无法识别 EMFILE；模型收到错误的“不存在”结论。
- 同条件下省略 file、加载正文时，却返回正确的执行错误并保留 EMFILE。
- 恢复描述符后，同一 Catalog、同一引用文件立即读取成功，证明不是缺失资源或非法参数。

**预期行为：** 系统资源耗尽等未完成的读取应通过 ExecutionError 返回非 nil error，保留 errors.Is 的底层错误身份。只有明确的缺失、路径拒绝及约定的业务拒绝才转换为 Rejected。

**影响：** tools/rejected.go:12–15 明确规定业务拒绝按已完成结果记入 Dispatcher 日志，恢复时回放而不重试。因此这里不仅改变报错文字，也把临时系统故障改变为终局业务结论。探针直接验证适配器分类；日志恢复影响依据共享 Rejected 契约及 Dispatcher 调用链，没有另跑持久化端到端测试。

**可重复证据：** Linux、Go 1.27.3，普通用户运行；探针仅修改自身子进程的 RLIMIT_NOFILE，不改变 shell 或其他审查进程。命令：

```sh
env -u GOROOT go run -mod=readonly /tmp/skills-architecture-E8DW8g/emfile.go
```

实测输出：

```text
catalog: EMFILE=true; err=openat ref.txt: too many open files
tool reference: err=<nil>; rejected=true; message="skill demo has no file ref.txt"
tool body: EMFILE=true; rejected=false
after recovery: "reference"
```

完整探针见附录；可保存到任意 /tmp 路径后从 core 仓库根目录执行。

**最小修复：** 收窄第 89 行的兜底分支：按已明确分类的缺失/参数/边界拒绝生成 Rejected；剩余错误统一走现有 resp.ExecutionError("skill loading failed", nil, err)。不要仅按 file 是否为空决定错误类别，也不要只特判 EMFILE 而继续吞掉其他未知 I/O 错误。不需要新增公开 Catalog 接口或注入机制。补充一个子进程故障回归，断言引用文件和正文两条路径都保留 EMFILE，且都不是 Rejected。

该问题不属于旧报告九个关闭项，使用当前源码获得了新的失败复现。


### AR-01 修复复核 — 已解决原始单一系统错误场景

**复核时间：** 2026-11-09T07:46:04Z。工具源码 SHA-256：5c862a659c3b2e717f5fe9ba2bfe8c06e65bf317ea6a3448421d4e64aa99e91b。

检查 tools/skill/tool.go:82–104：取消/超时/关闭优先作为执行错误；已知缺失及预期参数/权限错误按正向名单拒绝；未知错误默认 ExecutionError 保留 cause；没有错误字符串匹配。正文路径没有被正向名单扩大为业务拒绝。原始 AR-01 的 EMFILE 触发已修复；当时发现的联合错误残留及后续关闭证据见 AR-02。

**原探针原样重跑，退出 0：**

```sh
env -u GOROOT go run -mod=readonly /tmp/skills-architecture-E8DW8g/emfile.go
```

```text
catalog: EMFILE=true; err=openat ref.txt: too many open files
tool reference: err=[TOOL_EXECUTION] [load_skill] operation=invoke: skill loading failed: openat ref.txt: too many open files; rejected=false; message=""
tool body: EMFILE=true; rejected=false
after recovery: "reference"
```

**定向永久回归：** 以下命令通过，tools/skill 2.024s。子进程 RLIMIT 回归明确断言引用和正文两条路径的 errors.Is(err, syscall.EMFILE)，并验证恢复后读取成功；纯分类器回归覆盖 EMFILE、ENFILE、EIO 和不透明错误的原因身份。

```sh
env -u GOROOT go test -mod=readonly -race -count=1 -timeout=60s ./tools/skill -run 'Test(DescriptorExhaustionIsAnExecutionFailure|UnclassifiedReadErrorsPreserveCause|UnknownSkillsAndInvalidReferencesAreRejected|CanceledAndClosedCatalogRemainErrors)$'
```

已阅读 Harness preset/boundary_audit_test.go:47–83：越界链接要求非 nil error、失败且 Result=nil；词法非法路径仍要求 Rejected。本轮没有重复运行 Harness 套件。指南的返回契约更新由主流程进行，复核当时尚未在 docs/guides/SKILLS.md 看到完整新错误分类描述；不声称文档已全部更新。

新增回归源码摘要：

```text
28c12854d23507c3513c845c8fab968e2fcf144e8ac0ea60c178c950630eec56  tools/skill/errors_test.go
6d15c1a8a0985afd5606e3f3d7bc415906350fe6f5dbcd2cb8723a50f3d8ed29  tools/skill/resource_linux_test.go
```

### AR-02 — P2：联合错误中的业务错误会掩盖同时发生的 I/O 错误（已解决；以下保留原复现证据）

**文件：** /home/hellotalk/code/go/src/github.com/kart-io/goagent/tools/skill/tool.go:97–99；相关合并点 /home/hellotalk/code/go/src/github.com/kart-io/goagent/skills/catalog.go:213。

**触发：** ReadReference 的读取/检查产生 CodeInvalidInput（例如非普通文件或超限），关闭句柄同时返回 EIO；readFile 的 defer 使用 errors.Join 合并二者。分类器通过 errors.Is / IsCode 匹配整个树时，任一业务码即可进入 Rejected。第 94 行针对 fs.ErrNotExist 的匹配也采用相同的“存在匹配分支即拒绝”规则，修复时应统一考虑。

**实际：** 将 errors.Join(CodeInvalidInput错误, syscall.EIO) 及反向顺序直接交给当前真实 loadError，两者均返回 got=nil、rejected=true，EIO 被丢弃。

**预期：** 联合错误包含任何未分类的系统失败，就必须保留整个错误树并返回 ExecutionError。只有每个非 nil 分支都能明确归类为预期业务拒绝，才允许整体 Rejected。

**证据边界：** 这是当前私有分类函数的确定性失败复现，加上 Catalog 的真实合并调用点；未声称已在本地文件系统制造 close(EIO)。本轮没有增加广泛审查，仅复核前一轮已经指出的分类边界。

**最小修复：** 在正向名单前保守处理多错误树；对 Unwrap() []error 的全部分支递归分类，未知分支优先作为执行错误，保留原始 err。不能先对整棵树使用 errors.Is 或 GetCode 决定拒绝，否则仍会提前掩盖其他分支。添加两个顺序的 InvalidInput + EIO 回归；同理验证缺失/权限与未知 I/O 的组合。无需扩大公开 API。

**复现：** 仅在 /tmp 创建了现有 errors_test.go 的副本及 overlay 映射，未修改仓库测试文件。向副本追加下列测试并导入框架 agenterrors：

```go
func TestArchitectureJoinedFailure(t *testing.T) {
    for _, cause := range []error{
        errors.Join(agenterrors.New(agenterrors.CodeInvalidInput, "invalid resource"), syscall.EIO),
        errors.Join(syscall.EIO, agenterrors.New(agenterrors.CodeInvalidInput, "invalid resource")),
    } {
        out, got := loadError(context.Background(), &skills.Catalog{},
            tools.NewToolErrorResponse(constants.ToolLoadSkill), "demo", "ref.txt", cause)
        _, rejected := tools.RejectedResult(out)
        t.Logf("cause=%v got=%v rejected=%v", cause, got, rejected)
        if !errors.Is(got, syscall.EIO) || rejected {
            t.Error("joined I/O failure lost")
        }
    }
}
```

```sh
env -u GOROOT go test -mod=readonly -count=1 -timeout=30s -overlay=/tmp/skills-architecture-recheck-5JqvNS/overlay.json ./tools/skill -run '^TestArchitectureJoinedFailure$' -v
```

实测测试失败；两种顺序均输出 got=<nil> rejected=true、joined I/O failure lost。永久 TestUnclassifiedReadErrorsPreserveCause 目前只覆盖单一原因，没有覆盖此联合错误。AR-01 原始场景关闭不代表该残留已经解决。



### AR-02 修复复核 — 已解决

**复核时间：** 2026-11-09T07:50:32Z。此次仅复核分类器、原 overlay 用例、定向永久回归及已更新的错误契约，没有扩展审查范围。

检查当前 loadError：在未知技能、缺失文件和其他正向名单分支之前，先调用 expectedReferenceError。该函数先遍历全部 Unwrap() []error 分支、或递归单一 Unwrap() error 原因；只有原子错误叶子才允许匹配预期错误。任何未知原因均使整个错误作为 ExecutionError 返回，原始错误树保留。因而混合错误不能再被外层 CodeInvalidInput 或提前的未知技能判断遮蔽。不存在错误文本匹配。

**原失败 overlay 原样重跑：**

```sh
env -u GOROOT go test -mod=readonly -count=1 -timeout=30s -overlay=/tmp/skills-architecture-recheck-5JqvNS/overlay.json ./tools/skill -run '^TestArchitectureJoinedFailure$' -v
```

退出 0，TestArchitectureJoinedFailure PASS，包耗时 0.003s。InvalidInput + EIO 的两种顺序均返回非 nil TOOL_EXECUTION 错误、rejected=false，测试中的 errors.Is(got, syscall.EIO) 均通过。此 overlay 保持原复现文件不变，不包含后加的永久测试；永久测试另外直接使用当前仓库文件执行。

**当前定向永久回归：**

```sh
env -u GOROOT go test -mod=readonly -race -count=1 -timeout=60s ./tools/skill -run 'Test(DescriptorExhaustionIsAnExecutionFailure|UnclassifiedReadErrorsPreserveCause|JoinedRequestAndIOErrorsRemainExecutionFailures|UnknownSkillsAndInvalidReferencesAreRejected|CanceledAndClosedCatalogRemainErrors)$'
```

退出 0，tools/skill 通过，2.028s。覆盖 EMFILE 子进程实际耗尽、单一未知错误、InvalidInput/NotExist/Permission/Invalid 与 EIO 的两种组合顺序、外层 InvalidInput 包装 EIO；同时覆盖正文/引用路径和未知技能名，以及既有拒绝、取消、关闭契约。

docs/guides/SKILLS.md:84–86 已明确记录业务拒绝、未知 I/O 执行错误、保留 errors.Is 身份及不透明 os.Root 边界错误的处理，与本次修复一致。

**结论：** AR-01、AR-02 均已解决，本角色无剩余真实缺陷；AI-01 Builder 门禁建议仍为独立、未实施的可选改进。独立 Harness 发布门禁仍单独记录，不由本次定向通过替代。仅更新本报告，没有修改生产源码、测试、模块或 Git 状态。

本轮源码摘要：

```text
5376764841b9e48c5b7b1ac73dc0dadad48aa891ae282d2910bdeefa9b8e40da  tools/skill/tool.go
d29120807ba990a488f7800ce4763627f32f99fe3f39351a9da61e00da00137b  tools/skill/errors_test.go
```


## 二、可选改进（不计入真实缺陷；AI-01 未实施）

### AI-01 — P3：可进一步固化 Builder 不内建 Skills 的边界

**文件：** /home/hellotalk/code/go/src/github.com/kart-io/goagent/scripts/apicheck/main.go:90–93。

**现状/触发：** 当前 Builder 本身没有导入 skills 或 tools/skill；但把仅含该导入的 builder/probe.go 放入临时扫描根目录，apicheck 仍给出 0 findings。与之对照，core → skills 和 skills → builder 都正确被拒绝。

```sh
env -u GOROOT go run -mod=readonly ./scripts/apicheck -root /tmp/skills-architecture-E8DW8g/gate-builder
# imports: 0 findings；退出 0

env -u GOROOT go run -mod=readonly ./scripts/apicheck -root /tmp/skills-architecture-E8DW8g/gate-core
# disallowed import github.com/kart-io/goagent/skills；退出 1

env -u GOROOT go run -mod=readonly ./scripts/apicheck -root /tmp/skills-architecture-E8DW8g/gate-skills
# disallowed import github.com/kart-io/goagent/builder；退出 1
```

三个临时根目录分别包含 builder/probe.go、core/probe.go、skills/probe.go；内容都是相应 package 声明加空白导入目标包。可用此最小文件重建 Builder 探针：

```go
package builder
import _ "github.com/kart-io/goagent/skills"
```

**建议的预期/最小措施：** 若要把“Builder 不提供专门 Skills 装配、调用方通过通用 WithTools 接入”固化成长期门禁，在 builder 分支额外拒绝 skills 和 tools/skill，并补充扫描级回归；不应拒绝所有 tools 依赖，因为 Builder 现有通用工具支持需要它。

**为何仅为建议：** 当前依赖图符合独立库设计；现有文档明确承诺 skills 不依赖 Builder、core 不依赖 skills，并未明确承诺 apicheck 必须拒绝 Builder 未来导入 skills。没有把一个尚未发生的源码依赖错误当作运行时缺陷。

## 三、已核实的架构与 API 契约

| 检查项 | 当前证据与结论 |
|---|---|
| 独立顶层包 | skills 的直接项目依赖仅 codec/yaml、errors、constants；go list -deps ./skills 没有 core、builder 或 Harness。codec 和 errors 的传递格式/日志依赖不等于引入 Agent 执行内核。它是主模块内独立包，不宣称独立 Go module。 |
| 可选适配器 | tools/skill 通过 interfaces.Tool 与通用 tools 包接入。当前 core 和 builder 不导入 skills；普通应用可直接 Parse/Discover，或把 New(catalog) 交给 WithTools，无需 Harness 或真实模型。 |
| 最小 API | Metadata、Skill、Catalog；Parse、Discover；List、Load、ReadReference、Close 足以覆盖三级加载。没有泄漏 Harness 配置、授权策略、提示词或执行器到基础 API；不建议为未来功能增加抽象。 |
| 来源与优先级 | Discover 只接收调用方已打开的 os.Root；参数顺序决定跨来源优先级，每个来源内部排序，同名首个胜出。被覆盖文档仍验证，错误使发现失败，符合文档。 |
| 授权与提示词 | 公共包/工具不选择默认目录、不注入提示词、不执行 allowed-tools。DisableModelInvocation 不自动隐藏或授权；Harness buildSkills 保留默认来源、目录解释、提示词和策略装配。隐藏提示不等于授权已在指南说明。 |
| 元数据隔离 | List 与 Load 均经 maps.Clone 复制 Attributes；字符串为值，Files 每次 Load 新建。现有回归实际修改返回副本后再读，原快照不变。 |
| 生命周期/并发 | Catalog 使用 RWMutex，Close 排他、可重复；Load/ReadReference 的句柄使用处于读锁内。借用根与持有子目录的所有权独立；Close 后 List 可读、文件操作返回 fs.ErrClosed。定向 race 回归通过。 |
| 错误身份 | Catalog 未知技能支持 fs.ErrNotExist；关闭支持 fs.ErrClosed；context 取消保持身份。工具取消/关闭也保留身份。AR-01 单一系统错误和 AR-02 联合错误身份均已修复并经定向复核。Parse 使用框架 CodeInvalidInput。 |
| 来源证明 | Source 保存发现时的路径，仅供追踪；Version 为作者声明；Digest 为 SKILL.md 原文 SHA-256。正文/元数据是发现快照、资源实时读取，三者语义不混淆。不把 Source 当权限或重新打开入口，也不把 Digest 当整包身份。 |
| 限制与常量 | 文件/条目/深度上限集中在 constants/skills.go，工具名也单点定义。上限是固定安全预算而非 token 策略，文档没有承诺可配置。 |
| 导入门禁 | 当前全仓扫描 0 findings；定向测试覆盖 skills 禁止 Builder/core/tools/interfaces/plugin/第三方直接实现依赖，允许 codec/errors/constants/标准库；core 禁止 skills，核心对外部 Harness 依赖禁止。测试文件的导入例外是既有政策。 |

## 四、声明限制与独立发布门禁

- **发布门禁，非新增 API 缺陷：** 独立 Harness 当前版本依赖没有新公共包。主流程已复现并写入 release-check.json；本角色没有重复跑该发布检查，也不将其计入新缺陷。发布前仍需更新到可获取的 GoAgent 版本，并通过 GOWORK=off。本地 workspace 通过不能代替独立发布验证。
- 主流程告知 CI coverage gates 全部通过、overall 79.4%，benchmark contracts 通过。这是主流程提供的结果，本角色未重复执行。该覆盖率是初审时主流程提供的数据；修复复核结果另见 AR-01/AR-02。
- 文件安全与 codec 对抗探针由另一审查者负责；本角色未重复 FIFO、链接竞态、路径穿越或 YAML 资源放大检查。未重新打开旧报告已关闭事项。
- 并发验证只针对现有定向回归和代码锁范围，不宣称证明所有调度；不测试取消对阻塞系统调用的即时打断，指南已明确不保证。
- 只核实本地源码、导入图及公共接口契约；没有调用真实模型，没有执行全仓或 Harness 全量套件，没有外部发布。
- 已读取 CLAUDE.md，检查了项目技能目录：不存在 .claude/skills、.agents/skills；没有 .claudeignore，目标源码未被 git check-ignore 排除。当前工具没有独立 Read/Write、desktop-commander、sequential-thinking、shrimp-task-manager；使用只读 shell、显式分析及 Node fs 写入指定报告和 /tmp 探针。用户限定写入范围优先于 CLAUDE.md 的 .claude 日志路径要求。

## 五、初审时本角色实际执行的验证命令（修复复核命令见上文）

从 core 仓库根目录执行；全部带 -mod=readonly 的 Go 命令不修改模块文件。

```sh
env -u GOROOT go test -mod=readonly -race -count=1 -timeout=60s ./skills ./tools/skill ./scripts/apicheck -run 'Test(CatalogPrecedenceSnapshotsAndOwnership|CatalogContextAndConcurrentClose|ParseMetadataAndExactDigest|LoadAndReferenceContracts|CanceledAndClosedCatalogRemainErrors|NilCatalogFailsWithoutPanic|ImportRulesUseCurrentModuleAndLayeredExceptions|CoreDoesNotDependOnHarness|ConstantsAreAnIndependentLeaf)$'
# 三个包通过：1.019s、1.018s、1.014s

env -u GOROOT go run -mod=readonly /tmp/skills-architecture-E8DW8g/emfile.go
# 退出 0；输出确认 AR-01（该探针打印观测，不把缺陷转为非零退出码）

env -u GOROOT go run -mod=readonly ./scripts/apicheck -root .
# imports: 0 findings

env -u GOROOT go list -mod=readonly -f '{{.ImportPath}} -> {{join .Imports " "}}' ./skills ./tools/skill ./core ./builder
env -u GOROOT go list -mod=readonly -deps ./skills
# 成功，依赖边界如上表

# 三个隔离门禁命令见 AI-01，分别退出 0、1、1。
```

执行说明：EMFILE 探针首次生成时字符串换行转义错误，编译失败；只修正 /tmp 探针后重跑获得上述结果，没有修改被测源码。

**初审评分（历史记录）：** 代码质量 88/100、测试覆盖针对本范围 86/100、规范遵循 90/100；需求匹配 94/100、架构一致 94/100、风险控制 84/100，综合 89/100。初审建议修复 AR-01 后完成定向复核；当前 AR-01、AR-02 均已关闭；独立发布另受上述版本门禁约束。评分是本角色的定性判断，不替代测试或其他审查角色结论。

## 六、初审关键源码 SHA-256（修复后摘要见 AR-01 复核）

```text
b80263773c4beb93b7803ab784bc586451d06f5a38788fb5e864d50a7676c50b  skills/parse.go
e178447af644ad655ca33e38e04abc1166a553d13a1c88eab6420beace126991  skills/catalog.go
c230a82ecf3b12ceaa715619dcc2799b09cd7fdfe29686c2352d1536a1cffff8  tools/skill/tool.go
a3d80851091fac655d9e903977706b4a468832fa02b5840052e8aeb8e1e99d16  constants/skills.go
f6f0a9c88511d4b77c6cabb8afce76ec4a9faa1c85df538182934646feee9c10  scripts/apicheck/main.go
fbccd3532ef309a6d479442c5ab4722963d87ada105e238ff38edeeaefdd9539  scripts/apicheck/main_test.go
b7265272b0c262b9f60fc30fbd1bca249d2919706dacc6ec69186184272a04e8  ../goagent-harness/preset/skills.go
```

## 附录：EMFILE 完整复现程序

以下程序在 Linux 子进程内创建正常技能、耗尽自身文件描述符、比较 Catalog 与工具的错误身份，再恢复限制并读取同一文件。只依赖当前公开 API。

```go
package main
import (
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "syscall"
 "github.com/kart-io/goagent/interfaces"
 "github.com/kart-io/goagent/skills"
 "github.com/kart-io/goagent/tools"
 skilltool "github.com/kart-io/goagent/tools/skill"
)
func must(err error) { if err != nil { panic(err) } }
func main() {
 base, err := os.MkdirTemp("", "skills-api-emfile-"); must(err); defer os.RemoveAll(base)
 must(os.Mkdir(filepath.Join(base,"demo"),0700))
 must(os.WriteFile(filepath.Join(base,"demo","SKILL.md"),[]byte("---\nname: demo\ndescription: useful\n---\nbody"),0600))
 must(os.WriteFile(filepath.Join(base,"demo","ref.txt"),[]byte("reference"),0600))
 root, err := os.OpenRoot(base); must(err); defer root.Close()
 c, err := skills.Discover(context.Background(),root); must(err); defer c.Close()
 tool := skilltool.New(c)
 var old syscall.Rlimit; must(syscall.Getrlimit(syscall.RLIMIT_NOFILE,&old))
 limit := old; limit.Cur = 64; must(syscall.Setrlimit(syscall.RLIMIT_NOFILE,&limit))
 var held []*os.File
 for { f, err := os.Open("/dev/null"); if err != nil { if !errors.Is(err,syscall.EMFILE) { panic(err) }; break }; held = append(held,f) }
 _, directErr := c.ReadReference(context.Background(),"demo","ref.txt")
 out, toolErr := tool.Invoke(context.Background(),&interfaces.ToolInput{Args:map[string]any{"name":"demo","file":"ref.txt"}})
 bodyOut, bodyErr := tool.Invoke(context.Background(),&interfaces.ToolInput{Args:map[string]any{"name":"demo"}})
 must(syscall.Setrlimit(syscall.RLIMIT_NOFILE,&old)); for _, f := range held { must(f.Close()) }
 message, rejected := tools.RejectedResult(out)
 _, bodyRejected := tools.RejectedResult(bodyOut)
 fmt.Printf("catalog: EMFILE=%v; err=%v\n",errors.Is(directErr,syscall.EMFILE),directErr)
 fmt.Printf("tool reference: err=%v; rejected=%v; message=%q\n",toolErr,rejected,message)
 fmt.Printf("tool body: EMFILE=%v; rejected=%v\n",errors.Is(bodyErr,syscall.EMFILE),bodyRejected)
 data, err := c.ReadReference(context.Background(),"demo","ref.txt"); must(err)
 fmt.Printf("after recovery: %q\n",data)
}
```
