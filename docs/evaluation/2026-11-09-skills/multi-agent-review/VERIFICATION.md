---
scope: skills-extraction-verification-devex
verified: 2026-11-09T07:47:06Z
status: passed
score: 7/7 behavioral contracts verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 5/8
  gaps_closed:
    - "V-01: 非 UTF-8 资源明确拒绝，Catalog 原始字节保持不变"
    - "V-02: 超限资源报告 byte limit，不再误报不存在"
    - "V-03: 示例业务拒绝返回错误，命令非零退出"
  gaps_remaining: []
  regressions: []
artifact_refresh: "待主审在全部修复与审查结束后更新最终源码哈希和日志；历史产物不代表修复后快照"
---

# Skills 提取：验证与开发体验审查

复验结论：初审的 **2 项 P2、1 项 P3 已全部修复并关闭**。外部消费者的 4 个探针、工具与示例永久测试全部通过，正常示例退出 0，缺少参考资料的示例退出 1。当前无未关闭的本角色产品缺陷。源码哈希与完整日志待主审最终刷新；本结论不是独立 Harness 发布门禁或最终全套测试完成声明。

## 修复复验（当前结论）

| 发现 | 修复位置与检查 | 运行证据 | 状态 |
|---|---|---|---|
| V-01 / P2 | `tools/skill/tool.go:75` 在字符串转换前检查 `utf8.Valid(data)`；无效数据返回业务拒绝 | Catalog 仍返回 `89504e47ff00`；直接工具及 Builder 模型消息均返回 `not UTF-8 text`，没有 content 字段或替换字符 | 已关闭 |
| V-02 / P2 | `tools/skill/tool.go:94` 只对 ErrNotExist 使用不存在提示；`:97`–`:99` 将 CodeInvalidInput 等错误作为有原因的拒绝；其他执行错误保留错误链 | 已枚举的 large.txt 返回 `cannot read skill demo file large.txt: [INVALID_INPUT]: skill file byte limit exceeded`；nil Go error、rejected=true，未出现 `has no file` | 已关闭 |
| V-03 / P3 | `examples/skills/03-catalog/main.go:53`–`:57` 检查 RejectedResult 和 Success 后才序列化输出 | 缺少参考文件的实际 `go run` 退出 1，打印 `load_skill rejected`；完整默认 fixture 退出 0 | 已关闭 |

新增永久回归 `tools/skill/tool_test.go:170` 同时断言 Catalog 二进制字节保持不变、工具明确拒绝和超限错误不伪装为不存在；`examples/skills/03-catalog/main_test.go:11`、`:20` 覆盖失败与成功路径。本角色额外运行了真实 CLI，补足函数级测试没有直接检查进程退出码的部分。

临时探针仅在 /tmp 调整：原二进制日志语句假定成功结果一定是 map，现在改为断言合法的拒绝分支，**没有放宽内容完整性要求**；新增断言原始字节一致、拒绝消息正确、wire 不含 content。Builder 消息探针从打印观察强化为解析实际模型请求并断言拒绝类型、消息和无损坏内容。大小探针同时断言文件被枚举及准确的 byte-limit 原因。

复验实际运行的命令：

| 工作目录与命令 | 结果 |
|---|---|
| `/tmp/goagent-skills-verification-iMkElb`：`env -u GOROOT GOWORK=off go test -mod=readonly -count=1 -timeout=8s -v .` | 退出 0；TestBuilderThreeLevels、TestBinaryToolContract、TestOversizedReferenceMessage、TestBuilderBinaryWire 全通过 |
| Core：`env -u GOROOT go test -mod=readonly -count=1 -timeout=8s -v ./tools/skill ./examples/skills/03-catalog` | 退出 0；两包所有测试通过，包括新增内容/大小限制和示例正反例 |
| Core：`env -u GOROOT go run ./examples/skills/03-catalog -dir /tmp/goagent-skills-verification-iMkElb/missing-ref` | 预期失败：退出 1；`load_skill rejected: skill faq has no file references/refunds.md` |
| Core：`env -u GOROOT go run ./examples/skills/03-catalog` | 退出 0；文本参考内容及三级输出正常 |
| Core：`env -u GOROOT go test -mod=readonly -count=1 -timeout=8s ./skills -run '^TestCatalogPrecedenceSnapshotsAndOwnership$'` | 通过；针对 Catalog 变动快速复核优先级、快照、资源实时读取及生命周期 |

复验没有运行全套、Harness 测试、安全 fuzz 或真实模型，也未修改仓库源码、测试、模块或 git 状态。工具包命令顺带执行了当前已有的错误身份及文件描述符耗尽回归，结果通过；未另行扩展该方向探针。BASELINE 抽查确认 Parse 源码和测试未变；Catalog 源码、测试及 Skills 指南已有父任务修复，不能沿用旧哈希。已快速阅读 Catalog 的路径归一化和原有快照/读取接线，并单独复跑优先级/快照/所有权测试。当前指南明确非 UTF-8 拒绝和错误分类，与本次观测一致。

当前计分为 **7/7 行为契约通过**。初审第 8 项是产物快照一致性，现按用户明确安排移到最终产物刷新事项，未冒称已通过，也不计为新的产品缺陷。旧 full-race、覆盖率与哈希结果仅代表各自运行时快照；最终结果由主审刷新。

## 范围、方法与限制

- 审查当前未提交的 `skills/catalog.go`、`skills/parse.go`、`tools/skill` 及其测试，`docs/guides/SKILLS.md`、`examples/skills/03-catalog` 和验收产物。
- 从对外行为向上核验 Parse → Discover/Catalog → 工具 → Builder → 模型请求消息；没有把文件存在、覆盖率或旧报告结论视为行为证明。
- 仅写本报告；探针与临时模块位于 `/tmp/goagent-skills-verification-iMkElb`。未修改生产代码、仓库测试、模块文件或 git 状态，未提交、推送或调用真实模型。外部模块的 `go.mod/go.sum` 只写在 /tmp。
- 未重复全套测试、安全 fuzz 或 Harness 测试。正常 Builder 集成使用现有 `testing/mocks.ScriptedClient`。
- 初审没有既有的本角色 `VERIFICATION.md`；本次按其中三项失败进行复验。这不是某个规划阶段的验收，未将仓库无关的 protocol-adapters 阶段要求套用到本任务。
- 已读取项目指南与验证参考。专用 Read/Write、desktop-commander、sequential-thinking 工具不可用，以只读 shell、结构化检查和 apply_patch 替代；遵循用户指定的唯一仓库输出路径。

## 初审缺陷记录（均已关闭；以下保留修复前证据）

### V-01 · P2 · 已关闭：非文本资源被标记成功，实际传给模型的内容已改变

**位置：** `tools/skill/tool.go:73`；模型可见描述为 `tools/skill/tool.go:22`、`:27`。相关文档：`docs/guides/SKILLS.md:78`。

**具体触发：** 一个有效技能目录中包含普通文件 `asset.bin`，内容为十六进制 `89 50 4e 47 ff 00`。模型从资源列表得到文件名后调用 `load_skill(name="demo", file="asset.bin")`。

**观察：**

```text
Catalog=89504e47ff00
success=true
contentUTF8=false
wire={"content":"�PNG�\u0000","file":"asset.bin","name":"demo"}
decoded=efbfbd504e47efbfbd00
模型实际收到: {"content":"�PNG�\u0000","file":"asset.bin","name":"demo"}
```

Catalog 正确保留原始字节；适配器无条件转换为字符串并返回成功。框架 JSON 序列化将无效 UTF-8 替换为 U+FFFD，外部 Builder 集成确认模型实际收到替换后的内容。磁盘文件本身未被修改。

**预期：** 既然文档明确工具仅输出文本、二进制交由宿主读取，工具应明确拒绝不支持的内容并说明应走宿主资源接口，而不是将不可表示的内容标记为成功。这里不要求新增二进制/多模态功能。

**最小修复：** 在成功返回前至少检查 UTF-8 有效性，返回安全、明确的业务拒绝；同步模型可见工具描述中的文本限制。若要支持二进制，应另行定义带编码标识的契约，不应默默改变字节。

**决定性回归：** Catalog 原始字节相等；工具不会成功返回不可无损编码的文本；经过实际 Builder/JSON 后仍满足该契约。现有 `TestLoadAndReferenceContracts` 仅使用文本和 shell 文本，`parse_test.go` 的无效 UTF-8 测试只验证 SKILL.md，不覆盖资源适配器。

**复现证据：** 临时模块 `probe_test.go` 中 `TestBinaryToolContract` 失败；`TestBuilderBinaryWire` 将实际模型请求中的替换内容打印出来。后者通过只代表探针观察成功，不代表产品契约通过。

### V-02 · P2 · 已关闭：资源读取错误被无差别描述成“文件不存在”

**位置：** `tools/skill/tool.go:89`–`:90`；底层准确错误位于 `skills/catalog.go:221`–`:222`。

**具体触发：** 已发现技能中有一个 4 MiB+1 字节的普通文本文件 `large.txt`。先 Load 获取资源列表，再通过工具读取该文件。

**观察：**

```text
files=[asset.bin large.txt reference.txt]
CatalogErr=[INVALID_INPUT]: skill file byte limit exceeded
toolErr=<nil>
rejected=true
message="skill demo has no file large.txt"
```

适配器丢弃底层超限原因，把已经列出的资源否认为不存在。模型无法根据结果正确调整读取方案，宿主也拿不到该次读取的原始错误。该分支并非只处理 fs.ErrNotExist；除取消/关闭外，所有指定 file 的读取错误均进入相同分支。其他操作性错误的运行时影响未另外注入验证，不据此扩大本发现。

**预期：** 不存在、路径不允许、超限与操作失败应有适当区分；至少对于可合法访问但超限的文件，返回“超过资源读取上限”的安全业务拒绝，而不是“不存在”。不需要向模型泄漏绝对路径或底层系统细节。

**最小修复：** 对可识别的输入/上限错误返回明确、脱敏的拒绝原因；真实不存在可保留当前形状；其余执行故障保留框架错误链和必要的恢复语义。不要简单把所有原始错误文本暴露给模型。

**决定性回归：** 先断言 `Load.Files` 含该文件，再调用工具，验证超限提示且不能出现 `has no file`。现有 `skills/catalog_test.go:180` 验证了底层限制，却未贯穿适配器；`tools/skill/tool_test.go:90`–`:96` 将多种输入错误统一断言为 `has no file`，证明了现状而未检验原因表达。

**复现证据：** 临时模块 `TestOversizedReferenceMessage` 因上述错误信息失败。

### V-03 · P3 · 已关闭：示例资源读取被拒绝时仍退出成功

**位置：** `examples/skills/03-catalog/main.go:48`–`:57`。

**具体触发：** 使用 README 允许的 `-dir`，提供合法 `faq/SKILL.md`，但不提供 `references/refunds.md`。

**运行命令：**

```sh
env -u GOROOT go run ./examples/skills/03-catalog -dir /tmp/goagent-skills-verification-iMkElb/missing-ref
```

**观察：** 命令退出码 **0**，最后一行是：

```json
{"error":"rejected","message":"skill faq has no file references/refunds.md"}
```

`tools.Rejected` 按框架设计返回 nil Go error；示例只检查 err，随后序列化失败结果并返回 nil。使用本示例学习工具调用的消费者容易复制同样的误判；以退出码判断三级读取成功的验证也无法识别缺少资源。

**预期：** 示例要么明确说明它只展示任意工具响应、不保证三级加载成功，要么在参考读取未成功时返回错误。当前 README 宣称展示参考资料读取，推荐后者。

**最小修复：** 在 Marshal 前检查输出存在且 `out.Success` 为真；将业务拒绝转为有说明的示例错误。不要改变工具的业务拒绝契约。

**决定性回归：** 完整 FAQ fixture 退出 0；缺少参考文件的 fixture 非零退出。原样默认示例本次确实成功，因此这不是对旧示例正常输出的重复否定。

## 可观察契约核验

| 契约 | 结果 | 实际证据 |
|---|---|---|
| Parse 独立解析、校验字段、保留原文摘要 | 通过 | Parse 源码及元数据/摘要/边界测试；摘要直接对原始输入计算 |
| Catalog 维护来源优先级、独立副本、生命周期与实时资源读取 | 通过 | catalog 测试实际改写正文和资源、关闭借用根目录，并检查快照/实时读取差异 |
| 工具遵循文本成功形状、参数验证和宿主职责分离 | 通过 | 工具测试检查结果 map、空 AdditionalContext、脚本不执行、隐藏标记不自动过滤、Catalog 不被关闭 |
| 普通外部消费者能接入 Builder | 通过 | /tmp 独立模块、本地 replace，真实 Builder 工具循环完成三次脚本化模型请求 |
| 不支持的二进制资源被诚实表达 | 通过（修复后） | V-01：明确拒绝非 UTF-8；Builder 实际消息验证通过，Catalog 原始字节不变 |
| 资源错误原因准确且可采取后续行动 | 通过（修复后） | V-02：超限返回 byte limit，未知执行错误保留链路 |
| 可运行示例能区分三级读取成功与失败 | 通过（修复后） | 默认示例退出 0；缺少参考资源退出 1，永久正反例通过 |
| 当前审查快照与源码哈希、主要日志一致 | 待最终刷新（不计行为评分） | 初审校验通过；修复后旧摘要不再代表当前源码，主审将在全部审查结束后更新 |

**当前计分：7/7 行为契约通过；初审为 5/8（包括产物一致性项）。** 最终产物刷新单独跟踪，当前计分不是全项目测试通过率或发布就绪声明。

### 实际接线与数据流

`Discover` 调用 Parse 并保存正文、元数据和目录句柄；`Catalog.Load` 读取实时资源列表；`ReadReference` 读取文件字节；`skill.New` 的闭包调用这两个公开 API；`Builder.WithTools` 将其注册后执行，`builder/tool_events.go:511` 使用框架 JSON 将结构化结果放入模型消息。不存在孤立的占位实现；失败发生在已经接通的数据转换和错误适配层。

外部模块 `TestBuilderThreeLevels` 的首轮规则禁止出现未主动注入的目录描述和正文，第二轮要求收到 `INSTRUCTIONS_MARKER`，第三轮要求收到 `REFERENCE_MARKER`，并断言只发生三次请求、规则耗尽、Catalog 仍可用。技能同时声明 `disable-model-invocation: true` 和不可用的 allowed-tools；加载仍按宿主明确注册的工具进行，符合本包职责，而非授权缺陷。

## 可选改进（不计为新增产品缺陷）

- 为正常外部 Builder 三级加载保留一条永久集成回归。本次已验证接线正确；当前示例仅直接 Invoke 工具，不能替代 Builder 请求消息断言。
- `skills/catalog_test.go:144` 的并发关闭测试允许所有 goroutine 在 Close 之后才读取，因此可能全返回 ErrClosed 仍通过。可增加同步屏障，证明关闭前至少发生一次成功读取，同时保留关闭后拒绝断言。这是测试证明力不足，不是已复现的数据竞争。
- 当前源码中有 10000 条目和 2000 技能上限判断，但现有测试未覆盖各自恰好上限/超过一条的行为。深度、文件字节上限已有测试。建议按风险补齐，不将缺测本身宣称为实际超限漏洞。
- 历史 SOURCE_SHA256SUMS 覆盖生产源及测试，但不覆盖 Skills 指南和所有迁移文档；本轮 BASELINE 补充了这些文件。历史 checks.json 的覆盖率只是汇总数字，没有对应的持久 coverage profile；新的 CI 汇总记录了 /tmp profile 路径。若需要长期审计，可归档 profile 与工具链、命令和源码快照关联。

## 初审日志与源码产物一致性（历史记录，修复后须刷新）

- 原报告 Core race 日志包含 71 个 `ok` 包，Harness 包含 11 个，与 checks.json 一致；Core 多数为缓存命中，日志如实标注 cached。
- 原样示例输出与 example.log 的可见内容一致。
- 本角色定向测试测得 skills **92.9%**、tools/skill **94.7%**，与旧汇总及新 full-race 日志对应包一致。高覆盖率没有覆盖 V-01/V-02 所需的不同输入与语义断言。
- 已读取主审提供的 `ci-checks.json`、`coverage-gates.txt`、frameworkbench 结果和相关包日志：full race、覆盖率门禁及 frameworkbench 通过，整体 **79.4%**；不是本角色重新执行。新日志 codec/yaml 为 88.4%，旧汇总写 88.0%；这里不将不同运行的覆盖率差异认定为代码缺陷。
- 哈希审计：Core SOURCE 清单 17 项及 Harness SOURCE 清单 4 项全部匹配；本轮 BASELINE 的 40 项文件也均匹配。首次误在 Core 根目录运行 Harness 清单得到路径缺失，随后在 Harness 根目录纠正，4 项全部通过；这属于审查命令工作目录错误，不是项目缺陷。
- 未发现本次范围中的占位实现或 TODO/FIXME 阻断；发现的是正常分支下的契约不完整。

## 已声明限制

- Catalog 支持原始二进制资源；工具按文本输出。该功能边界合理，V-01 针对的是没有在工具边界诚实处理不支持输入。
- 正文/元数据是快照，资源是实时读取；Digest 不覆盖资源，不代表整包签名。文档与实现一致。
- Catalog/工具不承担来源授权、默认目录、自动提示词、脚本执行、安装更新或工具权限授予；宿主负责这些职责。
- 独立 Harness 发布门禁仍未满足，已由主审在 `release-check.json` 单独证明；不重复记为新问题。本次外部消费者用本地 replace 证明源码可集成，不证明已发布版本可获取。
- 未验证真实模型选择技能的效果、Windows/macOS 运行行为、Docker/Postgres 或远程 CI。旧报告交叉编译声明没有冒充目标平台运行验证。本次缺陷无需人工或外部服务即可复现。

## 初审实际运行的验证命令（修复前，复验命令见报告顶部）

| 命令 | 结果 |
|---|---|
| Core：`env -u GOROOT go run ./examples/skills/03-catalog` | 退出 0；三级输出正确 |
| Core：`env -u GOROOT go test -mod=readonly -count=1 -timeout=8s -cover ./skills ./tools/skill` | 通过；92.9% / 94.7% |
| /tmp 临时模块：`env -u GOROOT GOWORK=off go test -mod=mod -count=1 -timeout=8s -v .` | 退出 1；正常 Builder 与消息观察探针通过，二进制契约和超限提示两项断言失败 |
| Core：`env -u GOROOT go run ./examples/skills/03-catalog -dir /tmp/goagent-skills-verification-iMkElb/missing-ref` | 退出 0，却返回 rejected，复现 V-03 |
| Core：`sha256sum -c docs/evaluation/2026-11-09-skills/SOURCE_SHA256SUMS` | 17/17 通过 |
| Harness 根目录：`sha256sum -c /home/hellotalk/code/go/src/github.com/kart-io/goagent/docs/evaluation/2026-11-09-skills/HARNESS_SOURCE_SHA256SUMS` | 4/4 通过，仅校验文件，未执行 Harness 测试 |
| Python 读取 BASELINE 并逐文件 SHA-256 对比；计数两个 race 日志的 ok 行 | Core 33/33、Harness 7/7；日志 71/11 |
| `env -u GOROOT go version` | go1.27.3 linux/amd64 |

上述检查及探针运行均短于 10 秒。未执行全套、race 全套或新 fuzz。临时探针文件包含完整夹具和断言，可直接在其目录重复运行；不要把断言预期错误地改为接受当前行为。

## 持久复现说明

即使 /tmp 被清理，也可在新的临时模块中复现：使用 `module verification.local/skillsconsumer`、`go 1.27.0`、`require github.com/kart-io/goagent v0.0.0`，并 `replace github.com/kart-io/goagent => /home/hellotalk/code/go/src/github.com/kart-io/goagent`。

1. 创建临时 `demo/SKILL.md`，内容为 `---\nname: demo\ndescription: demo\n---\nread resources`。
2. 写 `demo/asset.bin` 为 `[]byte{0x89, 0x50, 0x4e, 0x47, 0xff, 0x00}`；写 `demo/large.txt` 为 `bytes.Repeat([]byte("x"), constants.SkillMaxReferenceBytes+1)`。
3. `root := os.OpenRoot(...)` 后调用 `skills.Discover(ctx, root)`；通过 `skilltool.New(catalog).Invoke` 依次传入两个文件名。
4. 二进制结果用 `github.com/kart-io/goagent/codec/json.Marshal(out.Result)` 再 Unmarshal；初审为结果字节不同且 Success=true，修复后应断言明确拒绝、没有 content 字段，Catalog.ReadReference 原始字节仍保持不变。
5. 大文件先确认 `catalog.Load` 的 Files 含 large.txt，再对比 Catalog.ReadReference 的超限错误与 `tools.RejectedResult(out)` 的不存在提示。
6. V-03 只需一个临时 `faq/SKILL.md`（name: faq、任意非空 description），省略参考文件，然后按上表 -dir 命令运行。

三项关闭依据是修复后的决定性回归与外部消费者实际观察均符合预期；没有用旧 CI 通过结果代替复验。

---

审查者：Codex（verification / developer experience）。初审：2026-11-09T07:32:10Z；修复复验：2026-11-09T07:47:06Z。最终源码哈希和完整日志由主审在审查结束后更新。
