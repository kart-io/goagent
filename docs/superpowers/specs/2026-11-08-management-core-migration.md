# 统一管理层迁入 GoAgent

## 用户确认的目标

将当前 contrib 中通用管理层直接迁入 GoAgent：Agent、Session、Task、Approval、运行观测、Worker、原生及 Graph 适配、可选 HTTP Handler。保留当前行为和持久协议，不保留旧包兼容转发。

## 分层和交付

- `goagent/management`、`management/adapters`、`management/httpapi` 进入核心单一 Go module，复用现有 execution/core。
- `goagent-contrib/management` 只保留 PostgreSQL 子包、真实数据库集成测试、退款案例、历史验收资料；原有独立 Harness 适配继续保持独立模块。保留扩展 module 名称不意味着保留旧根包。
- 核心不新增 pgx、contrib 或 Harness 依赖；execution、builder、core 等下层不能反向依赖 management。
- JSON 沿用核心 codec/json；领域哨兵迁入 errors 包，命名为 ErrManagementNotFound/Conflict/Invalid/Unauthenticated/Forbidden/Unsupported/Unavailable/ApprovalRequired/Busy/Owner，直接使用新入口，不保留别名。内部停用、用量缺失哨兵同样集中定义。
- 不修改原主工作区未提交文档，不推送或发布。跨仓未发布接口使用显式 GOAGENT_CORE_ROOT 临时 workspace 验证；对外发布必须先发布含 management 的真实核心版本，再更新 contrib 版本，不能声称 v0.50.0 已含新包。

## 验收

核心单 module 的独立 build、vet、race、lint、导入分层、覆盖率门禁、独立 frameworkbench；contrib 全模块真实数据库 race/vet/lint；原 8 组 HTTP 业务案例。已有正常、取消、审批、恢复、Fork、隔离及只读观测用例全部保留。

重点验证大整数和嵌套重复键、未知请求字段、公开响应的业务字段保留、关闭与崩溃恢复、旧根包删除与外部消费者新路径编译。测试跳过和跨仓发布前提必须如实说明。
