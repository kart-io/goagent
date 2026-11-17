# goagent 长期记忆可靠性验证设计

日期：2026-11-17

## 意图

验证 goagent 长期记忆系统（`memory` 包及 contrib 的 Postgres、Qdrant 后端）的可靠性。现有验证只覆盖一条线：客服示例中同一客户依次开几张工单（记住偏好、改偏好、整理）。这次扩到业界常用的记忆测试场景，并补上模型测试难以碰到的故障与并发路径。

“可靠”分三问，各由一部分回答：

| 部分 | 回答的问题 | 判定方式 |
|---|---|---|
| A 真实场景回归 | 真实使用中记得准不准、该忘的忘没忘、会不会串 | 记忆库状态（确定）+ 模型评审答复，按通过率统计 |
| B 故障与并发 | 出故障或并发时会不会丢、重、串、坏 | 确定性断言，必须 100% 通过 |
| C 基准评测 | 与业界数据集和本项目历史结果相比水平如何、有无退化 | `scripts/memeval` 在 LoCoMo / LongMemEval 上的准确率 |

## 目标

1. A：在客服示例上，用真实 DeepSeek、Gemini 嵌入与本地 docker 存储，跑 9 个场景，每个场景重复 10 次，报告每个场景的通过率与 95% 置信区间，并把失败归类到出错环节。
2. B：在同一套真实存储上，用离线脚本模型跑 6 个故障与并发场景，全部通过；不通过即为缺陷。
3. C：用 `scripts/memeval` 跑 LoCoMo 分层 200 题（与 2026-11-13 G2 评测同一批题）和 LongMemEval oracle 分层抽样，与历史结果对比。
4. 结果写入 `docs/evaluation/2026-11-17-memory-reliability/`，包含命令、代码版本、原始结果与结论；可在本机重复执行。

## 非目标

- 不修复本次发现的缺陷。发现即记录（复现步骤、所属环节、影响），修复另行确认后单独进行。
- 不发布任何版本、不打 tag、不推送。
- 不调优提示词或检索参数来提高分数；本次只测量现状。
- 不做性能压测（吞吐、延迟只记录不设门槛）。

## 反目标

- 为了让场景通过而改动断言、放宽判定或挑选有利的运行结果。
- 测试之间共用客户 ID 或记忆命名空间，使一次运行的残留影响另一次。
- 把 API Key、连接串写入文件、日志、提交或报告。
- 评审答复时把模型的自评当作结论；评审只依据场景给定的事实。

## 约束

- 被测代码：goagent 本地代码，经 `go.work` 使用，不发版。基线为 master，叠加尚未合并的 `feat/memory-save-replaces`（memory_replace）与 `fix/deepseek-dsml-toolcalls`；本分支 `test/memory-reliability` 即为该组合（实现第一步把 fix 分支合入）。报告写明确切提交。
- 客服示例为 goagent-examples 的 `feat/cs-memory` worktree（`goagent-examples-cs-mem`），客服图 `graphVersion` 为 v3、general 留三步。
- 存储全部来自本地 docker，经 `orchestration/customer-service/scripts/with-stores.sh` 启动专用容器、用后删除；不碰已在运行的其他容器。
- 模型：对话 DeepSeek；嵌入 Gemini（`gemini-embedding-001`）。本机没有 `OPENAI_API_KEY`，所以 C 的评审不能用历史评测所用的 gpt-4o（见 C）。
- 测试命令统一加 `TMPDIR=/tmp taskset -c 2,3,6-27`，`go test -p 1`（核 0、8 有故障；/mnt/data 是机械盘）。
- 不使用 `-mod=mod`，不执行 `go mod tidy`，不改 go.mod/go.sum。
- 测试写成表驱动；文档、注释、报告用简体中文。

## 做法比较

| 做法 | 取舍 | 结论 |
|---|---|---|
| 只跑 memeval 基准 | 有可比分数，但只测“灌历史→作答”，不测工具写入、钉住、整理、隔离、遗忘，也不碰故障路径 | 不足以证明可靠，作为 C |
| 只做客服场景回归 | 贴近真实使用，但模型路径很难稳定触发并发、崩溃、存储故障 | 作为 A |
| 故障注入 | 确定、可重复，覆盖历史缺陷所在的路径（钉住丢失、同轮否定 panic、槽位泄漏），但不反映模型行为 | 作为 B |

用户选择三者都做。

## A 真实场景回归

### 运行方式

- 放在客服示例包中，形式为 Go live 测试 `TestLiveMemoryScenarios`，门控与现有 `TestLiveMemory` 相同（需要 `DEEPSEEK_API_KEY`、`GEMINI_API_KEY`、`CS_MEMORY_PG_URL`、`CS_MEMORY_QDRANT_ADDR`，经 `with-stores.sh` 运行），另加环境变量 `CS_SCENARIO_REPEATS`（默认 10）与 `CS_SCENARIO_ONLY`（按场景名筛选）。
- 进程内装配宿主（与 `serve` 相同的图、记忆、存储），不经 HTTP 与浏览器；浏览器端到端已由 `/tmp/cs-e2e` 覆盖，不重复。
- 每个场景的每次重复使用一个新客户 ID，互不影响；跨工单的步骤之间按场景需要调用宿主的 sweep（处理记忆任务与整理）。
- 测试本身不因个别重复失败而失败：它输出逐次结果（JSONL）与汇总表，只在“确定性不变量”被破坏时（见下）判失败；目前只有 isolate 与 pinchain 含不变量。是否达标由报告中的通过率体现。
- 并发与预算（我的决定）：场景之间并发 3 个，同一场景的各次重复串行。先以 `CS_SCENARIO_REPEATS=2` 试跑全部场景，确认话术与判定可用后再跑 10 次。粗估全量约 90 次场景、约 1500 次 DeepSeek 调用与数百次 Gemini 嵌入调用，耗时约 1～2 小时。限流、5xx 等提供方错误归为 `infra`，不计入通过率分母，在报告中列出次数；占比超过 10% 时降低并发重跑。

### 判定

每个场景有两层检查，汇总表分别统计两层通过率，并给出“两层都通过”的综合通过率（我的决定）；失败归类按先失败的那一层与环节记。

1. **记忆库状态（确定）**：直接读记忆存储，例如旧记录的 `invalid_reason`、`superseded_by`、钉住标记、命名空间内是否存在某事实。其中标为“不变量”的一旦违反即为缺陷，测试失败。
2. **答复（模型评审）**：DeepSeek 作为评审，输入场景给定的标准事实、问题与客服最终答复，输出“对/错”与一句理由。评审提示词参照 `memory/memeval.JudgePrompt` 的 LongMemEval 模板（知识更新、时间、弃答各有对应模板），中文化后固定在代码中。评审与被测是同一模型，存在偏向风险：所有判为“错”的答复，以及每个场景随机抽 2 条判为“对”的答复，人工复核并在报告中记录评审误判率。

### 场景

LongMemEval 五类能力与 LoCoMo 对抗题在客服语境下的改写，加上客服场景特有的四类：

| 名称 | 来源类别 | 步骤（工单之间都执行 sweep） | 记忆库检查 | 答复检查 |
|---|---|---|---|---|
| recall | 信息提取 | 工单1：“我的设备型号是 X200，买了两年了”；工单2：“我这台设备怎么升级固件？” | 命名空间内有含 X200 的有效记录 | 答复针对 X200，未再询问型号 |
| multi | 跨会话综合 | 工单1 提订单 A1002 是耳机；工单2 提订单 A1003 是充电器；工单3：“我这两个订单分别买了什么？” | 两条事实都在 | 同时答出耳机与充电器 |
| update | 知识更新 | 工单1：收货地址在北京朝阳；工单2：已搬到上海浦东；工单3：“我的包裹会寄到哪？” | 北京的记录已失效（取代或否定），上海的有效 | 答上海，不答北京 |
| temporal | 时间推理 | 工单1 反馈耳机断连；工单2 反馈充电器发烫；工单3：“我最早反馈的是什么问题？” | 两条事实都在且带时间 | 答耳机断连 |
| abstain | 弃答 | 新客户首个工单：“我上次说的设备型号是什么？” | 命名空间为空或无型号 | 表示不知道或请客户提供，不编造 |
| premise | 对抗（错误前提） | 工单1：耳机是蓝色；工单2：“我那个红色耳机的保修期是多久？” | — | 指出记录中是蓝色或确认颜色，不顺着红色作答 |
| isolate | 客户隔离 | 客户 P 工单：“请记住我的会员号是 M-778899”；客户 Q 工单：“我的会员号是多少？” | **不变量**：Q 的命名空间内没有 M-778899 | Q 的答复不含 M-778899 |
| forget | 遗忘请求 | 工单1：“请记住我的备用电话是 13800001111”；工单2：“把我的备用电话删掉，以后别用了”；工单3：“我的备用电话是多少？” | 含该号码的记录已失效（遗忘或取代为“无备用电话”） | 答复不含该号码 |
| pinchain | 钉住穿越整理 | 工单1：“请记住：只用邮件联系我”；工单2：“改一下：邮件或短信都行”；工单3：“改回只用邮件”；每步后 sweep 两次 | **不变量**：取代链上每条记录都钉住、末端有效；钉住集合中至少一条联系偏好 | 工单4 问“该怎么联系我”，答只用邮件 |

### 失败归类

每次失败归入一个环节，便于定位：`tool`（工具未执行，含 DSML 文本）、`extract`（后台抽取没写入）、`consolidate`（整理把事实合错、丢失或钉住丢失）、`recall`（记录在但没进召回块）、`answer`（召回块里有但答错）、`judge`（人工复核认为评审误判，只在报告中标注）。归类依据运行记录（`data/runs/*.json` 的 `model/turn/N`）、记忆库状态与召回块。

## B 故障与并发

- 放在客服示例包中，形式为 Go 测试 `TestMemoryFaults`，门控只需 `CS_MEMORY_PG_URL` 与 `CS_MEMORY_QDRANT_ADDR`（经 `with-stores.sh`），对话用离线脚本模型（`demomodel`），嵌入用示例已有的哈希嵌入，结果确定。
- 不改 `model.go` 的离线话术表与任何生产代码（我的决定）：B 需要的抽取与整理话术放在测试文件里的专用脚本模型中。
- `with-stores.sh` 新增导出 `CS_MEMORY_PG_CONTAINER` 与 `CS_MEMORY_QDRANT_CONTAINER`（容器名），供 pg-outage、index-loss 只操作脚本自己启动的容器；未设置时这两个场景跳过。
- 全部为不变量，任何一次不满足即测试失败。

| 名称 | 操作 | 必须满足 |
|---|---|---|
| concurrent | 同一客户同时提交两张工单，各自写入一条不同事实，同时 sweep | 两条事实都存在，无重复记录，无错误 |
| replay | 同一批记忆任务连续 sweep 三次；同一消息重复投递 | 记录数与只处理一次时相同 |
| crash | 整理进行中终止宿主（取消上下文并丢弃进程内状态，模拟 kill），重启后再 sweep | 任务最终完成；不存在“旧记录已失效但无取代者”或“取代者不存在”的悬空记录；钉住不丢 |
| pg-outage | sweep 期间 `docker pause` Postgres 若干秒后恢复 | sweep 返回错误而非静默成功；恢复后重试完成，状态与无故障时一致 |
| index-loss | 删除 Qdrant 集合（或停用后恢复）后召回 | 宿主不崩溃；用 `Memory.Reindex` 重建索引后，召回结果与故障前一致 |
| ns-concurrent | 多个客户并发提交工单与 sweep | 每个命名空间只含自己的事实 |

`crash` 用子进程方式（我的决定）：测试编译并启动 `cs serve` 子进程（离线模型、同一套存储），提交工单后在 sweep 进行中发送 SIGKILL，再启动新进程完成 sweep；不为此给生产代码加测试挂钩。`index-loss` 的“一致”指召回命中的记录 ID 集合相同，不要求顺序与分数。

## C 基准评测

- 工具：goagent `scripts/memeval`，不改代码。
- 数据：LoCoMo `locomo10.json`（`snap-research/locomo`），LongMemEval oracle（`xiaowu0162/longmemeval`），下载到 `/tmp` 下的专用目录，报告记 sha256；不提交数据集。
- 配置：
  - LoCoMo：`-sample 200 -repeats 2 -systems memory,graph -chat deepseek -embed gemini:gemini-embedding-001`，与 2026-11-13 G2 评测同一批题。
  - LongMemEval oracle：`-sample 100 -repeats 1`，同样的方案与模型。
- 评审：本机没有 OpenAI Key，评审用 DeepSeek（`-judge deepseek`）。
- 基线：`docs/evaluation/2026-11-13-memory-g2/` 的 `locomo-200.jsonl` 与 `locomo-200-r2.jsonl`（G2 两次独立运行，memory 与 graph 方案）。
- 跨运行对比（我的决定）：memeval 没有“只评审”入口，其配对检验也只比较同一运行内的两个方案，所以另写一个只读的评测辅助程序（不改 memeval，放在评测目录，按以往做法存为 `.go.txt`，运行时复制到 /tmp）：它用 `memeval.JudgePrompt` 与 DeepSeek 重新判定基线 JSONL 中的答复，再按“同一方案、同一题”把本次与基线逐题配对，做符号检验。p < 0.05 的下降视为退化。LongMemEval 没有历史结果，只报告本次分数作为今后的基线。

## 交付物

- goagent-examples `feat/cs-memory`：`TestLiveMemoryScenarios`、`TestMemoryFaults` 及其辅助代码；README 补运行方式。
- goagent `test/memory-reliability`：本设计、实施计划、`docs/evaluation/2026-11-17-memory-reliability/`（README 结论、A 的 JSONL 与汇总、B 的测试输出、C 的 Markdown 与 JSONL）。
- 发现的缺陷清单（在评测 README 中），每条含复现步骤与所属环节。

## 留给实现者

- 场景的确切话术、sweep 次数与等待方式，只要满足上表的意图与检查。
- 评审提示词的中文措辞，只要沿用 LongMemEval 的判定口径。
- `crash`、`index-loss` 的具体注入手段。
- A 的汇总表格式（至少含场景、次数、通过数、通过率、置信区间、失败归类计数）。
