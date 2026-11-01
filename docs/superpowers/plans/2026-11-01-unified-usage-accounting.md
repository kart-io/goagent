# Unified Usage Accounting Plan

目标：按用户要求将多 Agent 计量收敛为单一实现，不维护按格式切换的兼容模式。

约束：复用现有预算、Observer 和持久 Store；不新建调度/计费系统。旧格式明确拒绝，不转换、不猜归属、不重新发送模型请求。Config.Version 仅是宿主策略指纹。

## 范围

- [x] 默认预算保存 partial 字段证据的回归先 RED。
- [x] 移除 AttributionVersion、AttributionEnabled 和双格式选择；唯一格式为 goagent-model-budget。
- [x] Entries 单条记录包含 Estimate、Evidence、AgentCallKey，删除重复的估计/证据/归属映射。
- [x] Reserve/ReserveEstimated/RecordUsage 只转发统一证据实现，继承 context 归属。
- [x] Builder 绑定预算即建立调用身份；无角色名用 anonymous，无 Agent 上下文的直接调用明确未归属。
- [x] Snapshot 与 Attribution.Root 共用同一求和函数。
- [x] 回归并发、重试、partial、未知字段、错误及子进程恢复，运行根模块和 race。
- [x] 更新当前指南/设计/示例，历史验证报告注明已被本次收敛取代。
- [x] 独立审核、归档验证并本地提交；不推送或合并。

## 实施选择

MaxAgentCalls 为 0 时归一为 DefaultMaxAgentCalls=1000，避免为零模型调用另设启用模式；负值拒绝。没有归属的低层调用也使用相同证据类型，但不凭空创建 Agent 身份。

这是明确的接口/存储收敛，不提供旧 AttributionVersion 的别名或自动迁移。旧持久运行保留原数据，需要原实现或宿主明确处置；不能通过清理日志或换键来掩盖已有副作用。

## 验证结果

默认partial与便利API链路回归RED→GREEN。根模块、相关包race、五轮聚焦回归、跨进程恢复与离线示例通过；独立只读审核无实质未解决项。日志见[报告](../../evaluation/2026-11-01-unified-usage/REPORT.md)。
