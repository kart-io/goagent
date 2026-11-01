# Pi Core Contracts Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task. User has requested direct execution of the previously recommended scope.

**Goal:** 补齐 GoAgent 的输入终态可见性与 Pi 设计对照，保持通用编排内核定位。

**Architecture:** 复用 Handle 的锁和队列，只增加快照查询并修复失败清空 Followup；不增加调度器或执行循环。

**Tech Stack:** Go 1.25，现有 Builder 测试工具，无新增依赖。

**Spec:** [设计](../../design/PI_CORE_ADOPTION.md)

## Global Constraints

- 不修改默认并发、迭代预算、恢复策略或权限；不引入供应商与 TypeScript 运行时。
- 保留用户现有未跟踪的 Sandbox 调研材料。
- 只读快照不消费、不承诺已领取输入的处理结果、不持久接收消息。

## Review Focus

- 输入在 TurnStopping 之后到达，不能无声消失。
- 第一轮失败时 Followup 未被执行，也不能从快照消失。
- Wait 超时只是调用方退出等待，不能误报运行完成。
- 快照修改不能改变运行队列；并发查询不产生数据竞争。
- 持久恢复与进程内队列边界必须写清，不从队列推导副作用状态。

### Task 1: Handle 输入边界

**Files:** `builder/handle.go`、`builder/handle_pending_test.go`。

**Interfaces:** 新增 `PendingInput{Kind string, Text string}` 与 `(*Handle).PendingInputs() []PendingInput`；Kind 固定四个小写字符串。

- [x] 写实际 Builder/Handle 回归，先单独用现有内部队列证明失败清空 Followup 的缺陷，再写公开快照测试。
- [x] 运行失败回归，预期 Followup 长度错误；新增接口测试最初预期缺少接口。
- [x] 在锁内复制待处理队列；只在决定续跑时领取 Followup。
- [x] 运行 Handle/loop hooks 测试和 race；预期通过。

### Task 2: 设计对照、组合指南与验证

**Files:** `docs/research/2026-11-01-pi-core-gap-analysis.md`、`docs/guides/AGENT_COMPOSITION.md`、`README.md`、`docs/design/DESIGN_OVERVIEW.md`、`docs/evaluation/2026-11-01-pi-core/REPORT.md`。

**Interfaces:** 文档引用现有 Runnable/Dispatcher/Graph/Session API，不新增业务路由或后台调度 API。

- [x] 补固定官方源码来源、已支持/本轮补齐/外围待评估能力表及对应代码证据。
- [x] 补输入状态、完成事件、可恢复子流程、意图路由与会话分支指南；使用编译运行的 Example 展示通用组合。
- [x] 执行所涉及包 race 与根模块测试，记录结果与明确未验证项；检查文档链接及 diff。
- [x] 独立审查最终差异，处理实质问题；提交本地分支，不推送或合并。

## 执行记录

用户已授权按上一轮建议直接执行，沿用核心/外围分层。Task 1 已完成 RED→GREEN；Task 2 的 Example、相关包 race、根模块测试与文档链接检查均通过。独立审查无实质发现，无待处理审查项。默认工具链版本错配通过显式 Go 1.25.0 解决。完整命令和日志见[验证报告](../../evaluation/2026-11-01-pi-core/REPORT.md)。
