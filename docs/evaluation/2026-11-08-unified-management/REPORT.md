# 统一管理服务实施结果

> 后续验收：本报告记录首轮交付。扩展功能、真实模型及可靠性修复的后续结果分别见[扩展案例](../2026-11-08-management-expanded-cases/REPORT.md)、[真实模型验收](../2026-11-08-management-live-model/REPORT.md)和[可靠性验收](../2026-11-08-management-live-reliability/REPORT.md)。

> 状态：下文的数字和“未推送”描述属于首轮验收时点；请以上述后续报告及当前发布状态为准。

日期：2026-11-08。

> 后续决策：用户已取消旧版兼容要求。实现统一使用 GoAgent 管理接口；原双依赖基线只作历史记录。

首批功能在 `goagent-contrib` 的 `management` 模块实现，另有独立可选的 `management/harness`；GoAgent 核心执行代码没有增加管理数据库或 Harness 依赖。核心统一管理改动已合并并发布为 GoAgent `v0.51.0`。

交付覆盖 Agent 版本发布/启停、Session/历史/Fork、持久 Task/Command、审批审计与续跑、运行和预算查询、24 条管理 HTTP 路由、Postgres 后端、通知 outbox 和可运行退款案例。

实际验证：

- management race 测试 43 条通过，0 失败、0 测试跳过；vet 通过，golangci-lint v2.15.0 为 0 问题。
- 当前核心源码的临时 workspace 与可下载依赖基线分别验证。
- 17 个关联模块兼容验证通过。跨模块另有 102 项条件跳过，主要是外部模型/服务与特定故障环境，不算作这些场景通过。
- 真实 HTTP＋Postgres 退款案例：退款 1 次、幂等提交同一 Task、Token 10、旧会话/分支 v1、新会话 v2。
- 等待审批时强制终止 worker，批准后由新 OS 进程恢复，SQL 计数为模型请求 2 次、退款 1 次。
- Fork 底层复制完成而 SQL 元数据未 ready 时子进程以 42 退出；恢复比对前缀并补 ready，未重复写日志。
- 并发决定单赢家、拒绝效果为 0、真实退出后取消、同会话串行和通知重投不重跑业务均有实际用例。

独立审阅发现的 7 项 Important 已逐项补失败测试并修复，随后完整回归通过。详细报告、机器可读结果、全部规划文档与案例源码都随实现分支保存于 `management/docs` 和 `management/examples/host`。

复跑入口（在该分支的 contrib 根目录，并设置本地 `GOAGENT_PGSTORE_DSN`）：

```bash
bash scripts/with-local-modules.sh go -C management test -race -count=1 -timeout 180s ./...
bash scripts/with-local-modules.sh go -C management run ./examples/host -migrate -verify -listen 127.0.0.1:0
```

边界：一个活动执行 worker，没有心跳超时自动抢占、多 worker 分片或管理界面。attention 恢复需要业务专用核对协议；缺失时明确 unsupported。线上模型质量、长期容量和数据库主备切换不由这些离线案例覆盖。
