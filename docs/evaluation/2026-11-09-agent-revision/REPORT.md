# 不可变 AgentRevision 验收

日期：2026-11-09。范围为 GoAgent 核心仓库，未修改 Harness、Contrib 或案例仓库。

## 需求与实现

| 需求 | 实现 | 验证 |
| --- | --- | --- |
| 保存旧配置、提示词、工具定义、模型/策略版本 | `revision.Definition`、`Seal`、`Verify`、内容摘要 | 规范 JSON、大数、修改检测、引用约束、深复制测试 |
| 保存旧 Skills 与附属资源 | `Files` 归档字节、`Skills` 显式目录 | 技能校验、加载正文/参考文件、未声明技能隔离 |
| 保存程序身份并实际加载 | Runtime native/executable/oci，精确注册表，私有物化目录 | 工件 SHA 校验；示例真实编译并执行 v1/v2，删除原文件后重载 v1 |
| 新发布不漂移旧会话 | 复用不可变 `management.Version`，Session/Task 固定 RevisionID | 发布 v2 后旧任务、旧 Session 新任务、Fork 继续 v1，新 Session 使用 v2 |
| 审批与输入跨版本恢复 | 真实 Worker、持久管理命令与归档 | input/approval 等待后停 Worker、发布新版、重开归档和重建 Service，再恢复原任务 |
| 缺旧程序时拒绝替换 | 精确 Runtime registry，预检阻塞 | 只装 v2 时旧命令 blocked，等待点/执行键保留，补回 v1 后恢复 |
| 防止错误归档发布 | 保存、发布及执行前统一校验 | 普通 Factory、错误 ID、篡改、错误 Config/程序版本均被拒；发布依赖不可用不改变当前发布版本 |
| 追溯同程序资源变更 | ConfigHash 纳入 RevisionID | 仅提示词变化也产生不同恢复身份；幂等请求换归档冲突 |
| HTTP 可观测绑定 | DTO/OpenAPI 增加 revision_id | 公开投影保留 RevisionID，继续隐藏内部执行键 |

## 设计取舍

复用现有 Version、Session、Task、Factory、Worker、Store 和会话日志；无新数据库表、无新发布状态机。
`revision` 只依赖技能解析、格式编解码与叶子常量，管理适配器负责宿主运行环境。
内核禁止反向导入 revision，依赖门禁已有回归用例。

FileArchive 写入使用同步临时文件、原子硬链接创建和目录同步；相同摘要幂等，不覆盖损坏对象。
读取有字节、文件数、路径、JSON 深度限制，验证整个规范对象。宿主文件系统须支持相应操作。

专业分工包括归档实现与测试、管理适配实现与窄范围独立复核、主审的绑定链与发布门禁测试。
独立复核发现的 Build 前置失败恢复缺口及关闭证据见 [REVIEW.md](REVIEW.md)。不把本次窄范围复核宣称为完整安全审计。
文档与测试的第二视角复核发现配置未产生 v1/v2 差异，现已补上归档 Config、精确大数、工具定义及模型/策略完整引用的实际读取断言；授权与重启证据的范围也已收紧。模型/策略引用隔离不等于外部解析器行为验证。

## 验证记录

执行原始输出保留在本机 `/tmp/goagent-revision-*.log`。

| 命令 | 结果 |
| --- | --- |
| `env -u GOROOT go test -race -count=1 -timeout=120s ./...` | PASS，74 个有测试包；最终恢复边界变更另由管理层完整回归复验 |
| `env -u GOROOT go test -race ./management -run '^TestAgentRevision' -count=1 -v` | PASS，含配置/工具/完整引用隔离、发布门禁、缺失旧程序恢复 |
| `env -u GOROOT go test -race -count=1 -cover ./revision` | PASS，85.9% 语句覆盖 |
| `env -u GOROOT go build ./...` | PASS |
| `env -u GOROOT go vet ./...` | PASS |
| `env -u GOROOT golangci-lint run ./...` | PASS，0 issues；首次两处 De Morgan 表达式提示已修复 |
| `env -u GOROOT go run ./scripts/apicheck` | PASS，imports: 0 findings |

最终生产代码落盘后再次执行：

```sh
env -u GOROOT go test -race -count=1 -timeout=120s -coverprofile=/tmp/goagent-revision-management.cover ./management/... ./revision ./examples/agent-revisions ./scripts/apicheck
```

全部 PASS。覆盖率：management 80.1%、adapters 84.8%、httpapi 97.3%，超过项目门槛 75%/75%/80%；同次运行 revision 87.5%、protocol 75.9%、apicheck 92.4%。a2a/mcpserver 的独立包没有自身测试文件，协议集成测试在 protocol，不能将单包 0% 解读为该协议未运行或已实现完整覆盖。

最终再次通过 build、vet、lint（0 issues）与 apicheck（0 findings）；gofumpt 与 `git diff --check` 通过。完整 race 与最终相关回归的原始输出归档于 [checks.txt](checks.txt)。
初次并行测试时碰到另一 worker 尚未完成的 `RetryableBuildError` 编译状态；归档与 apicheck 已单独通过，示例待完整代码落盘后统一重跑，不把该轮记为全通过。

## 恢复复核后的修正

- 仅安全且已清理的 Build 失败恢复 claim 前状态，原输入/审批命令变为 blocked，Attempt 结束，Session 仍由原任务占用；使用持久 AvailableAt 延迟重试。
- 不清除并发取消，不跳过旧 attempt 或 Attention 的 Inspect。未知结果且无恢复命令时拒绝取消，避免关闭人工核对入口。
- 失败适配保留宿主是否交出 Runtime 的事实，防止包装错误时抹去 Worker 的重试否决条件；普通错误、清理失败、panic 和 Run 返回的 Build 标记均不会因此自动重跑。
- Attempt 序号按分页历史最大值递增，不再在 10000 条截断后重复。清理错误继续上报。

## 已验证范围与部署边界

- 管理端到端使用内存 Store/Session 夹具、真实 Worker 和磁盘归档的关闭重开，验证内容与身份链，不能等同于数据库或操作系统进程崩溃耐久性验收。既有 PostgreSQL 生产适配未在本轮实库重跑。
- 可执行文件示例真实编译、归档、删除原产物并运行两版 Go 程序；不调用模型，不证明外部模型行为可复现。
- native 程序仍须由宿主保留并注册旧实现。OCI 仅固定 image@digest，镜像层保留、拉取及容器生命周期由宿主负责。
- Models/Policy 是固定版本引用，外部内容的保留、精确解析、凭据和实时权限由宿主负责；不归档模型权重或密钥。
- 未实现自动 GC、跨平台二进制转换、动态 Go 插件加载或 Kubernetes 编排。旧 revision 与其依赖须在被会话和审计引用期间保留。
- 磁盘硬件掉电故障注入、超大并发容量与生产文件系统耐久性未在本轮测试；这些仍应由部署环境验证。

## 工具与复用记录

编码前复核 management 版本发布/Worker、workflow RecoveryManifest、Harness Manifest/PinManifest、skills Catalog、codec/json 精确解析和 durable 文件存储模式。
环境没有 desktop-commander、sequential-thinking、shrimp-task-manager、context7 或 GitHub 代码搜索工具；使用 rg、本地源码、Go 工具链、官方 kagent 文档及专业代理分工补足。设计与范围保存在 `.claude/context-summary-agent-revision.md`。

产品 API、装配步骤和来源链接见 [AgentRevision 指南](../../guides/AGENT_REVISIONS.md)。
