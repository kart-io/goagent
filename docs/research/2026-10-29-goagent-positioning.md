# GoAgent 定位决策与模块去留清单

日期：2026-10-29  
状态：已确认（2026-10-29，外围模块直接删除）  
代码基线：`c167cf9`（`codex/composable-orchestration-r2`）  
相关：[可组合编排需求](../superpowers/specs/2026-10-27-composable-orchestration-requirements.md)、[多框架调研与复审](2026-10-27-framework-orchestration-review.md)、[执行契约与能力状态](../guides/EXECUTION_CONTRACT.md)、[导入层级](../architecture/IMPORT_LAYERING.md)

## 1. 结论

GoAgent 定位为**嵌入 Go 业务服务的可恢复、可计量、可观测 Agent 编排内核**，不再以“Go 版 LangChain 全家桶”扩展功能广度。

- 核心模块持续投入：执行契约、原生工具循环、Dispatcher、Graph 与持久恢复、预算与计量、OTel 观测、上下文策略、MCP。
- 外围能力直接删除，不拆 `contrib` 模块：本机没有任何宿主导入这些包（见 2.3）。
- 分布式调度、跨主机接管等能力不自建，需要时适配成熟工作流系统。

## 2. 现状证据

### 2.1 优势

| 优势 | 证据 |
| --- | --- |
| 执行语义投入最深 | `core`/`builder`/`tools`/`workflow` 源码约 3.3 万行、测试约 4.2 万行；2026-10-20 以来的提交主要落在这几个包（core 26、builder 27、tools 22、workflow 16）。R2 分支 50 个提交集中修正 panic 边界、取消、恢复、脱敏。 |
| 持久恢复细粒度 | `core/durable`、`CompileDurable` 的节点与并行分支检查点、审批、单机重启恢复；工具调用级日志回放；父子共享根预算，重启不重置（见[R2 整合验证](../evaluation/2026-10-28-r2-integration/REPORT.md)）。 |
| 框架开销低 | [同机对测](../evaluation/2026-10-27-framework-performance-v2/REPORT.md)：8 节点图 P50 0.50µs，Eino 为 10.98µs；两轮模型加一次工具 9.23µs，Eino 为 53.92µs；64 段流的总完成时间 Eino 更低，GoAgent 首段更早。仅适用于该测试的 API 与负载。 |
| 成本控制 | 原生工具流将典型工具任务从 3 次请求降到 2 次；ReturnDirect、根级 MaxModelRequests、按操作键去重的用量登记。 |
| 真实业务验证 | DevOps、文章审核、自动回复、退款案例，覆盖 DeepSeek、Gemini、MiniMax 真实调用及跨进程崩溃恢复。 |
| 复用标准组件 | 观测复用 OTel，MCP 使用官方 go-sdk，未自建 trace 体系。 |

### 2.2 负担

- **外围模块成熟度低**：执行契约将推理策略、自动反思列为“实验”；`reflection` 源码约 1071 行、测试 93 行；`distributed`/`multiagent` 使用 Core NATS，不支持持久化消费；`cache`、`options`、`prompt` 自 2025-12-07 以来没有改动，`prompt` 在主模块中没有被任何包导入。
- **示例过大**：`examples/` 约 5.5 万行（迁出业务案例后仍约 4.8 万行），超过 `core`+`builder`+`tools`+`workflow` 源码总和。
- **单一 go.mod 依赖面宽**：aiplatform、qdrant、cohere、gorm/pgx、mysql、lib/pq、go-sqlite3（cgo）、nats、goquery、pdf、viper 都在主模块。只使用 `builder` 的宿主也会因最小版本选择受这些依赖传递要求的版本约束（如 grpc、otel、protobuf），容易与宿主现有版本冲突；编译本身只涉及被导入的包。
- **采用度与发布**：GitHub 7 star、4 fork；GitHub Release 停在 v0.1.9，git tag 已到 v0.15.0。外部用户少，意味着现在收敛的迁移成本最低。

### 2.3 使用方

2026-10-29 检查本机 `$HOME/code` 下全部 `go.mod`，依赖 goagent 的只有 [goagent-examples](https://github.com/kart-io/goagent-examples) 和仓内子模块 `evaluation/frameworkbench`（`replace` 指向主模块）。goagent-examples 导入的是第 4.1 节的保留包（`interfaces`、`core/durable`、`llm`、`workflow`、`tools`、`core/budget`、`builder` 等），以及 `workflow/refund` 与测试辅助 `testing/panictest`；外围包只被 goagent 自身 `examples/` 中的演示导入。外部开源使用方无法核实，仅有 GitHub star/fork 数可参考。

## 3. 方向比较

| 方向 | 判断 |
| --- | --- |
| A. 维持全家桶，补齐 LangChain 式功能 | 不采用。与 Eino、LangChainGo 比功能广度，投入会流向成熟度最低的实验模块。 |
| **B. 收敛为可恢复编排内核** | **采用**。与已验证的优势、近期投入和 R2 设计结论一致。 |
| C. 改为 Eino 等框架之上的适配层 | 不采用。丢失开销优势和持久语义；R2 调研已否决。 |

## 4. 模块去留清单

依据 `go list` 得到的生产代码导入关系（不含 `examples/` 与测试文件）。“外部依赖”只列非标准库的直接依赖。

### 4.1 保留在主模块

| 包 | 外部依赖 | 说明 |
| --- | --- | --- |
| `interfaces`、`errors`、`utils`、`utils/json`、`utils/httpclient` | zap、sonic、mapstructure、resty | 基础层 |
| `core`、`core/{budget,durable,execution,flow,middleware,observe,state}` | uuid、flock、otel/trace | 执行契约、持久日志、预算、观测事件 |
| `core/checkpoint`（除 `redis.go`） | — | Redis 实现外移，见 4.3 |
| `builder`（除 `reasoning_*.go`）、`builder/contextpolicy` | jsonschema-go、uuid | 推荐入口 AsRunnable |
| `tools`、`tools/middleware`、`tools/{compute,http,search,shell}` | jsonschema-go、x/time | Dispatcher 与轻量内建工具 |
| `workflow` | — | Graph 与 CompileDurable |
| `llm`、`llm/{common,constants,cache}`、`llm/providers` | go-openai、genai、resty | 原生工具流 Provider；后续可按 Provider 再拆，本轮不动 |
| `mcp/{core,remote,toolbox,tools}` | modelcontextprotocol/go-sdk | 官方 SDK 适配 |
| `observability`、`middleware`、`devtools` | otel、prometheus、grpc、logger | OTel 与指标 |
| `store`、`store/memory`、`store/decorator` | — | `core/execution` 与 `builder` 依赖 `store.Store` 接口 |
| `memory`、`cache`、`parsers` | — | `tools` 依赖 `cache`；`parsers` 随 react 移出后再评估是否保留 |

### 4.2 删除

没有宿主使用，保留 contrib 需要多模块 tag 与 CI，维护成本没有收益，因此直接删除。代码保留在 git 历史与 `v0.16.0` tag 中，需要时可从中恢复。

| 包 | 外部依赖 | 删除后效果 |
| --- | --- | --- |
| `agents`、`agents/{base,cot,got,metacot,pot,react,sot,tot,executor}` 与 `builder/reasoning_*.go` | x/sync | 推理策略整体删除；`performance` 只被 `agents` 使用，一并删除 |
| `agents/specialized` | gorm、go-redis、resty、logger | 主模块去掉 gorm 的生产引用 |
| `reflection`、`planning` | logger | 实验能力 |
| `multiagent`、`distributed` | nats、otel/propagation | 主模块去掉 nats；不再暗示提供可靠分布式投递 |
| `retrieval`、`document` | aiplatform、cohere、qdrant、google api、pdf | 去掉最重的一组依赖 |
| `store/{postgres,redis,factory,adapters}`、`options` | gorm、pgx、go-redis、pflag、viper | 存储后端由宿主实现 `store.Store` |
| `core/checkpoint/redis.go` | go-redis | 核心层不再直接依赖 Redis |
| `tools/practical`、`toolkits`、`tools/plugingen` | mysql、lib/pq、go-sqlite3、goquery、yaml.v3 | 主模块去掉 cgo 依赖 |
| `stream` | — | 主模块内无导入方；与 `core.StreamRunnable` 职责重叠。gorilla/websocket 仍经 genai 间接引入 |
| `prompt` | — | 主模块无导入方，2025-12-07 后无改动 |

### 4.3 示例

| 包 | 依据 |
| --- | --- |
| `examples/` 中与核心入口无关的部分 | 业务验证案例（`workflow/business-cases`、`workflow/refund-recovery`、`orchestration`）已于 2026-10-29 迁到 [goagent-examples](https://github.com/kart-io/goagent-examples)；其余目录保留 `workflow`、`builder`、`mcp`、`basic` 等核心入口，只演示 4.2 中包的示例随之删除 |

### 4.4 删除前置改动

保留包对外围包的生产依赖只有三处：

1. `builder/reasoning_builders.go`、`builder/reasoning_presets.go`（共 1285 行）导入 `agents` 及全部推理策略包，随 `agents` 一并删除，Builder 只保留原生工具循环入口。
2. `core/checkpoint/redis.go` 导入 go-redis，删除；`checkpoint.Checkpointer` 接口保留在核心。
3. `testing/test_token_usage.go` 导入 `agents/cot`，改用 mock 或删除。
4. 导入待删包的测试随之删除或改写：`builder/reasoning_presets_test.go`、`tools/toolkits_integration_test.go`、`integration/services_test.go`、`core/checkpoint/redis*_test.go`、`examples/supervisor_agent_example*_test.go`；示例 `examples/basic/14-reasoning-agents`、`examples/advanced/multiagent-patterns` 一并删除。

2026-10-29 在临时工作树中按上述清单删除后，`go build`、`go vet`、`go mod tidy` 通过，`go.mod` 从 125 行降到 81 行，不再依赖：aiplatform、cohere、qdrant、google api、gorm 系列、pgx、lib/pq、mysql、go-sqlite3、nats、goquery、pdf、viper、go-redis。仍残留：pflag（经 `kart-io/logger`）、yaml.v3（经 zap、testify、sonic，均为 indirect）、gorilla/websocket（经 genai）。

`scripts/apicheck` 中针对 `builder/reasoning_*.go` 导入 `agents` 的兼容例外随之删除。

## 5. 能力路线

按与定位的契合度排序，均沿用 R2 的准入原则（需业务证据或故障专项方案）。第 1、2 项的具体需求见[嵌套恢复与失败处理](../superpowers/specs/2026-10-29-nested-resume-and-failure-handling.md)；两者共用操作记录格式升级，实施顺序为记录格式、第 2 项、第 1 项。

1. **嵌套恢复与审批续跑**：补上“不透明复合调用”pending 时不能续跑的缺口，需要独立 continuation 契约。
2. **失败处理**：业务结论作为工具结果交还模型；崩溃后按执行键核对结果再决定是否重跑；临时故障在最内层模型调用上受预算约束地重试。
3. **离线只读运行报告**：基于已有 JSONL 事件，HTML 必须转义不可信内容。
4. **流 API 稳定化**：仅在有业务证明首帧收益后推进。
5. **持久摘要**：后置。

不做：自建分布式调度（需要时适配 Temporal 等）、完整四范式流运行时、反射字段映射、继续扩充推理策略种类。

## 6. 发布

1. R2 的 Breaking 变更作为 v0.16.0 合入 master，CHANGELOG 已有迁移说明。
2. 修复 GitHub Release 流程，使 Release 与 tag 一致。
3. 外围模块删除单独作为 v0.17.0，CHANGELOG 列出删除的包与替代方式（推理策略改用原生工具循环、存储后端由宿主实现 `store.Store`），并注明可从 `v0.16.0` 恢复。
4. 主模块进入 v1 前，至少完成第 5 节第 1、2 项，并有一个内部宿主在生产使用。

## 7. 风险与待确认

| 项 | 说明 |
| --- | --- |
| 外部使用方 | 本机无宿主使用外围包（2.3）；未知的外部用户升级到 v0.17.0 会编译失败，只能停留在 v0.16.0 或自行复制代码。 |
| 缺少生产宿主 | 目前没有内部服务在生产使用 goagent，v1 门槛（第 6 节第 4 条）尚无候选。 |
| 性能结论适用范围 | 对测结论来自单机合成任务，不代表生产吞吐。 |

## 8. 验证方式

本文档的依赖数据可复现：

```bash
go list -f '{{.ImportPath}}|{{join .Imports ","}}' ./... | grep -v '/examples/'
grep -rln 'goagent/\(agents/\(cot\|got\|tot\|metacot\|pot\|sot\)\|reflection\|planning\|multiagent\|distributed\)"' \
  --include='*.go' . | grep -v _test | grep -v '^./\(agents\|reflection\|planning\|multiagent\|distributed\|examples\)/'
grep -rl --include=go.mod 'kart-io/goagent' "$HOME/code" | grep -v '/kart-io/goagent/'
```

第三条命令列出依赖 goagent 的本机模块。第二条命令当前输出 `builder/reasoning_presets.go`、`builder/reasoning_builders.go`、`testing/test_token_usage.go`，即 4.4 列出的前置改动点。
