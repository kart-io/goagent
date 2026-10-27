# Execution Context Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement task by task in the current session.

**Goal:** 收敛推荐入口、补齐可选上下文治理、完善任务评测。

**Architecture:** 保留 Builder 执行源，原生工具循环内管理大结果与摘要预算。评测执行器独立注入，复用统一计量与评分。

**Tech Stack:** Go 1.25，现有 llm/Builder/agentbench。

**Spec:** `docs/superpowers/specs/2026-10-26-execution-context-design.md`

## Global Constraints

- 默认关闭上下文治理；旧 Execute 保持兼容。
- 摘要计入请求预算及用量。
- 离线模型结果只验证协议，不作为质量或性能排名。

## Review Focus

- 多工具回合不破坏调用与结果关联。
- 未知引用与跨执行引用不得读取。
- Unicode 分页与无效 offset/limit 明确处理。
- 摘要失败、取消、无用量及单请求预算不能伪装成功。
- 对照执行失败仍保留计量且不能通过评分。

## Task 1: ContextPolicy

Files: `builder/context_policy.go`, `builder/context_policy_test.go`, `builder/config_options.go`, `builder/builder.go`, `builder/tool_events.go`。

- [x] 写卸载/读取、保留/排除、摘要预算和失败测试，运行并确认缺失行为。
- [x] 实现 WithContextPolicy、执行内存结果分页读取、历史摘要及循环预算接入。
- [x] 运行 Builder 回归与 race。

## Task 2: 任务评测验证

Files: `evaluation/agentbench/run.go`, `evaluation/agentbench/run_test.go`。

- [x] 写可注入执行器计量/评分测试，确认失败。
- [x] 实现 RunWithExecutor，共用提示、题集、计量、评分。

## Task 3: 文档与整体验收

Files: `README.md`, `README_CN.md`, `docs/guides/QUICKSTART.md`, `docs/guides/EXECUTION_CONTRACT.md`, `docs/guides/CONTEXT_POLICY.md`。

- [x] 更新入门到 AsRunnable，并记录迁移、上下文治理及评测命令。
- [x] 主模块短测试、修改包 race、diff 检查。
- [x] 记录已完成与仍需真实业务标签的验收边界。


## 执行记录

- Task 1：ContextPolicy 的缺失行为测试先失败，完成卸载/读取、完整回合与排除、摘要预算/用量/失败以及公共入口记忆保留验证。
- Task 2：RunWithExecutor 计量和评分验证通过；题集导入修正应用于 stress 命令。
- Task 3：入门入口和 RAG 示例已迁移；整仓及修改包分别验证。
- Final review：独立只读审查完成；主模块配置凭据与超时评分问题补充回归。
- Ruling：卸载只保存于当前执行内存 — 无新增持久化生命周期 — 代价是不减进程内存且不跨进程恢复。
- Ruling：最终回合恢复原始证据 — 预算内无法再读取并总结 — 代价是最终请求可能较大。
- Ruling：摘要显式计入现有请求预算并在失败时报错 — 避免隐藏调用 — 代价是应用需增加预算或调整触发阈值。
- Deferred minors：无。
