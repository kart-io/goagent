# 统一 Agent 运行管理

`github.com/kart-io/goagent/management` 是可嵌入宿主的管理服务，统一 Agent 注册与配置发布、Session 与 Fork、Task 接纳/取消/恢复、Approval 决定及运行观测。使用单个活动 worker 持续处理已持久接纳的命令；执行复用现有 Builder、Graph、durable、budget 和 execution.Scope。

| 包 | 职责 |
| --- | --- |
| `management` | 实体、Store/Runtime/Factory 合约、管理服务、Worker、Outbox、只读运行查询 |
| `management/adapters` | 原生 Agent 与 Graph 的执行适配 |
| `management/protocol` | A2A/MCP 公共任务身份、回复与状态观测 |
| `management/a2a` | 官方 A2A JSON-RPC/SSE、能力发现、取消与恢复 |
| `management/mcpserver` | Agent 工具、长任务、MCP 人工输入与审批续接 |
| `management/httpapi` | 可挂载的标准库 HTTP Handler 与 [OpenAPI](httpapi/openapi.yaml) |

宿主向 `management.New(Config)` 注入管理 Store、Session Store、HistoryReader、Authorizer 和固定版本 Factory；显式启动 `NewWorker(service).Run(ctx)`，通过 `httpapi.New` 生成 Handler。构造器不建表、不监听端口，也不自动启动后台工作。宿主负责认证、数据库迁移、资源关闭和确认旧进程停止后的 owner 回收。

PostgreSQL 参考实现、真实数据库集成测试、退款业务案例位于 [contrib/management](https://github.com/kart-io/goagent-contrib/tree/master/management)。可选 Harness 适配保持独立；核心管理包不依赖 pgx、contrib 或 Harness。

## 请求与执行合约

- 创建和命令使用 Idempotency-Key；内容相同复用原响应，内容不同拒绝。修订更新采用 expected_revision。
- 业务配置版本为不可变记录。发布影响新 Session，已有 Session 和 Fork 继续绑定原版本；这不属于软件旧接口兼容。
- Agent 停用后拒绝新 Session（包括 Fork）和新 Task；重新启用后可重试先前被拒绝的请求。
- 任务取消在生产者实际退出后确认。未知外部效果保持 attention，须提供可信核对证据，不能直接重新执行。
- 审批必须经 DecideApproval；决定和续跑命令原子持久化，普通 ResumeTask 不能绕过审批。
- JSON 使用 codec/json，精确保留数字并拒绝重复键；HTTP DTO 使用 Strict 规则，拒绝未知字段、非法 UTF-8 和不匹配大小写的字段名。
- 查询只读取已存在的日志/用量证据，未知消耗保留未知；运行汇总不把查询页大小当作总量限制。

领域错误直接使用 `github.com/kart-io/goagent/errors` 中的 `ErrManagement*`，不在本包维护旧错误别名。迁移说明和完整对应关系见 [核心迁移指南](../docs/guides/MANAGEMENT_MIGRATION.md)。

## 本地验证

在 GoAgent 根目录运行：

```bash
env -u GOROOT GOTOOLCHAIN=go1.27.3 GOWORK=off go test -race ./management/...
env -u GOROOT GOTOOLCHAIN=go1.27.3 GOWORK=off go build ./...
```

核心单测不需要数据库。持久恢复、进程故障和 HTTP 退款案例继续在 contrib 使用专用 Postgres 验证，不能用内存单测或数据库 SKIP 代替。

`make test-management` 执行默认离线回归；`make coverage` 额外要求 management ≥75%、adapters ≥75%、httpapi ≥80%，三个包分别检查，不能用其他包的覆盖率抵消。

## 纯聊天与恢复边界

`adapters.NativeFactory` 支持只有 `llm.Client` 的纯聊天 Builder，无需添加占位工具。适配器绑定的会话日志记录完整轮次，重建后可按原执行键回放，Fork 继续绑定原配置版本。循环钩子和 Handle 仍要求原生工具循环。

单个活动 Worker 可按 `Limits.Workers` 并发执行不同会话；这不提供多实例分片或心跳超时自动接管。异常退出后，宿主必须通过 `ConfirmWorkerStopped` 确认旧进程停止，再执行带 owner、generation 和原因的回收。`Shutdown` 超时不代表旧生产者已退出。

启动恢复每批读取200条 running/canceling 任务和 applying 命令，修改状态后继续读取，直到集合排空，不再截断于10000条；全部批次仍在同一事务内，失败或取消时回滚。数据库必须支持事务内读到自身写入和原子 CAS；超大积压的事务时长、锁等待及恢复目标仍由实际容量测试确定。

跨仓实库验收使用 `make compatibility-postgres`，须配置专用测试数据库和允许重启的容器。它要求数据库存储契约、崩溃接管、重启、同步提交、管理审批进程恢复、Fork 崩溃恢复，以及 Harness 跨连接恢复、旧持有者失效和 Inbox 去重具有实际通过事件；缺失或跳过即失败。

新增20组HTTP与4项数据库业务案例的功能映射、红绿修复和执行结果见 [扩展案例验收](../docs/evaluation/2026-11-08-management-expanded-cases/REPORT.md)。

DeepSeek与Gemini的真实模型、审批续跑、多轮会话、Fork及实际用量结果见 [真实模型验收](../docs/evaluation/2026-11-08-management-live-model/REPORT.md)。

真实模型验收工具的失败退出、请求增量证据和配置前检修复见 [可靠性修复](../docs/evaluation/2026-11-08-management-live-reliability/REPORT.md)。

纯聊天支持、恢复分页、覆盖门禁及跨仓数据库复验见 [管理层稳定化验收](../docs/evaluation/2026-11-09-management-hardening/REPORT.md)。

A2A、HTTP 与 MCP 共用任务的装配及互操作约束见 [协议接入指南](../docs/guides/PROTOCOL_ADAPTERS.md)。
