---
phase: 2026-11-09-management-hardening
reviewed: 2026-11-09T03:12:00Z
reviewer: Codex
depth: deep
status: issues_found
diff_base: f2e0b0b449fcfda416169356291689248e5f455b
files_reviewed: 8
files_reviewed_list:
  - builder/capabilities.go
  - builder/events.go
  - builder/session_log.go
  - builder/capabilities_test.go
  - builder/session_log_test.go
  - builder/plain_session_test.go
  - management/worker.go
  - management/worker_recovery_paging_test.go
findings:
  critical: 1
  warning: 0
  info: 0
  total: 1
priorities:
  P0: 0
  P1: 1
  P2: 0
  P3: 0
---

# 核心运行语义独立深度审查

## 结论

**建议退回修复一个新增 P1：普通子 Agent 会误用父 Agent 的 SessionLog，提前结束父轮次，污染中断历史并阻断持久恢复。**

本报告只将同一根因计为一个发现。启动恢复分页没有发现有实证的新增缺陷。原有定向测试通过不构成无缺陷证明：新增的三个独立反例在当前代码上全部失败，而仅通过 Go overlay 将 `builder/events.go` 替换为指定基线内容后，三个反例全部通过。

审查对象为给定基线至当前工作树（包括未跟踪测试文件），不是仅查看提交间差异。报告完成前再次核对八个范围文件的 SHA-256，均与父目录 `source-manifest.json` 相符。未修改生产源码、已有测试或其他审查者文件，未提交。

## P1 / CORE-01：隔离普通子 Agent 的 sessionTurn，避免写坏父轮次

**类别：** 正确性／会话数据完整性／持久恢复，新增回归。  
**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/builder/events.go`  
**行号：** 210–214、234–236；入口上下文依据为同文件 43–46。

### 触发条件

父 Agent 配置 `WithSessionLog`，在工具中以工具收到的 `ctx` 调用一个**没有配置 SessionLog、没有工具的普通子 Agent**：

```go
out, err := child.AsRunnable().Invoke(ctx, "子任务")
```

这属于已经支持的嵌套调用方式。子 Agent 的普通聊天路径在本轮变更前也能运行；触发本问题不要求给子 Agent 新增任何日志配置。

### 代码证据与调用链

入口只清除收件箱及历史，没有清除父轮次的日志引用：

```go
// builder/events.go:45
ctx = context.WithValue(context.WithValue(ctx, inboxKey{}, (*inbox)(nil)), historyKey{}, nil)
```

新代码不检查此日志是否属于当前 Agent，直接取继承值并同步：

```go
// builder/events.go:210–214
slog := sessionTurnFrom(ctx)
if err := slog.begin(ctx, messages, nil); err != nil {
    return nil, fmt.Errorf("会话日志: %w", err)
}
if err := slog.sync(ctx, messages); err != nil {
    // ...
}
```

随后只凭继承值非空就结束轮次：

```go
// builder/events.go:234–236
if slog != nil {
    final := llm.Message{Role: "assistant", Content: turn.Content,
        ReasoningContent: turn.ReasoningContent, ProviderState: turn.ProviderState}
    if err := slog.end(ctx, append(slices.Clone(messages), final), turn.Content, usage); err != nil {
        // ...
    }
}
```

父 `runEvents` 在 192–200 行将 `*sessionTurn` 放进 ctx；`runToolEvents → invokeToolCall → Tool.Invoke → child.AsRunnable().Invoke → ExecuteStream → runEvents` 保留该引用。子 Agent 未配置日志，因此不会执行 192–200 行建立自己的轮次，但 210 行仍取得父轮次对象。

`session_log.go:207–226` 的 `end` 将子对话同步到该 Recorder，并追加普通的 `turn/end`，没有独立子轮次身份。核心依赖的直接后果为：

- `core/session/event.go:187–198` 的 `OpenTurn` 从末尾遇到 `turn/end` 即判定不存在未完成轮次。
- `builder/session_log.go:142–153` 将匹配父执行键的 `turn/start` 之后的非中止 `turn/end` 视为父轮次完成。
- `builder/events.go:197–198` 随之开启 `durable.WithReplayOnly`，禁止恢复过程中执行尚不存在的父模型操作。

### 实际影响与复现结果

临时反例位于 [nested_test.go](/tmp/goagent-core-review-UOQQnt/nested_test.go)，通过 overlay 注入同包测试，没有写入仓库测试目录。

| 反例 | 正确行为 | 当前代码实测 |
| --- | --- | --- |
| `TestCoreReviewPlainChildMustNotCloseParentTurn` | 子调用后父轮次仍打开，父最终完成后共一轮 | 子调用关闭父轮次；最终轨迹记为两轮 |
| `TestCoreReviewPlainChildFailureMustAbortParentHistory` | 子调用后父模型失败，下一次输入应中止上一轮；请求仅一条新用户消息 | 下一轮请求含四条消息，保留失败父输入、父工具调用、工具结果和新输入 |
| `TestCoreReviewPlainChildMustNotBlockDurableResume` | 子工具完成、父第二次模型请求前中断后，同键恢复可继续执行缺失模型请求 | 重建父 Agent 后报“只读重放需要已完成的持久记录”，无法完成父任务 |

第三个反例使用 journaled Dispatcher 工具、父子各自的 DurableScope、同一会话 DurableStore；在父 `RequestHook` 的 `Step == 1` 处仅失败一次，第二次构建不再注入故障。第一次父模型及子模型的结果均已持久化，因此恢复失败不是模拟模型缺少答复造成的。

**新增与既有问题的区分：** 未清理 `sessionLogKey` 的入口和原生工具循环引用继承日志的行为在基线已经存在，但基线的普通聊天路径完全不读取该引用。本轮新增 `slog.begin/sync/end` 将问题扩展到原先正常工作的“有日志的工具父 Agent → 无日志的普通子 Agent”。三个反例均仅替换基线 `events.go` 就通过，故本发现明确定位于新增普通路径，不把既有的工具子 Agent 问题重复计为新增缺陷。

### 修复方向

在每个 Agent 执行边界屏蔽继承的 `sessionLogKey`，仅允许当前 Agent 的 `openSessionTurn` 安装其拥有的日志对象；同时覆盖 `ExecuteStream` 与 Handle 的内部入口，避免只修普通入口后仍有旁路。可以在共同的 `executeStream` 入口绑定空 `*sessionTurn`，再由本次 `runEvents` 根据配置创建轮次。不要通过修改 `completedSessionTurn` 忽略多余结束事件来掩盖日志归属错误。

补充上述三个嵌套反例作为回归测试，并验证父日志的 system／工具定义仍归父所有。子 Agent 显式配置自身日志时，其会话身份也应明确隔离；本报告未将该既有身份设计另列为新增缺陷。

## 已检查的语义边界与未列为发现的结果

### 普通聊天

逐个阅读了三个 Builder 源文件及相关测试；调用链向下检查 `runnable.go`、`execution_gate.go`、`tool_events.go`、`model_call.go`、`durable_turn.go`、`loop_hooks.go`、`agent_usage.go`、`memory_policy.go`，以及 `core/session` 的事件推导、Recorder、DurableStore 和导出实现。

- 历史来源：`openSessionTurn` 推导历史后覆盖 `historyKey`，`streamMessages` 优先采用该历史；已通过无工具的普通 Client／ToolStreamClient、多轮、文件存储重建及 Fork 的现有定向测试。
- 中断：普通非持久失败保持轮次打开，下一次执行通过中止事件撤销；模型快照已提交而会话 `turn/end` 提交失败时，补日志不会重发模型。上述正常单 Agent 场景通过。
- 重放与 usage：追加 overlay 正向测试，第一次模型结果落盘故障、开启中断重发、重建 Agent 后成功，再次回放；实际调用两次、预留请求数为 2、已知 token 为 14，完成回放不追加会话事件。
- 上下文修改：上述完成轮次改用不同 system prompt 重放时，意图校验拒绝请求，没有第三次模型调用、没有修改会话事件；`PreStep/RequestHook/PromptContexts` 等仍按能力校验限制在工具循环。
- 空输出校验、RequestError 原地修改消息、记忆写入及最终验收位于会话完成之后等通用行为并非本轮新增，未据此另列发现。

### 启动分页恢复

完整阅读 `management/worker.go`，并沿 `Run → owner → recover → poll/start → execute/finish` 以及 `Store.WithinTx → all/List → update/CAS` 追踪。

- 两类记录均处于同一个事务；状态修改使记录离开筛选集合，每次重新从集合头读取，没有偏移跳项。
- `Running/Canceling → Queued` 保留 `CancelRequested`，设置 `RecoveryNeeded`；启动恢复后的执行先检查完成证据，再处理取消。定向完成证据优先／取消竞态测试通过。
- 新增 overlay 验证 0、1、199、200、201、400、401 条边界，查询次数与更新次数均符合排空逻辑；原有 10001 条跨租户测试及恢复幂等性测试通过。
- 新增 overlay 在任务第二页、命令第二页读取失败，命令写入 CAS 失败，以及两类记录分页中取消时断言全部任务和命令回滚，均通过。
- 并发保护沿用已有 Worker owner 和 CAS 协议；生产路径先获得唯一 owner，再扫描恢复，尚未进入本 Worker 的任务派发。owner 身份、代次及审计原子性的定向测试通过。本轮没有引入第二个并发恢复生产者。
- 验证使用事务替身；没有运行 PostgreSQL 集成、真实跨进程并发或全量矩阵，因此这里不声称证明数据库隔离级别或生产锁等待行为。未用父级 REPORT.md 的绿色结果替代本次验证。

## 全部有效定向验证命令

工作目录：`/home/hellotalk/code/go/src/github.com/kart-io/goagent`。所有命令均实际运行，未调用真实模型。

### 新增缺陷：当前失败、基线通过

```sh
env -u GOROOT GO_TEST_MODE=true go test -overlay=/tmp/goagent-core-review-UOQQnt/overlay.json ./builder -run '^TestCoreReviewPlainChild' -count=1 -timeout=45s
env -u GOROOT GO_TEST_MODE=true go test -overlay=/tmp/goagent-core-review-UOQQnt/base-overlay.json ./builder -run '^TestCoreReviewPlainChild' -count=1 -timeout=45s
```

第一条退出 1，三个反例全部失败；第二条退出 0，全部通过。单独运行 `-run '^TestCoreReviewPlainChildMustNotBlockDurableResume$'` 也分别失败／通过。

[当前 overlay](/tmp/goagent-core-review-UOQQnt/overlay.json) 仅注入额外测试文件；[基线 overlay](/tmp/goagent-core-review-UOQQnt/base-overlay.json) 额外映射 `builder/events.go` 到 [base_events.go](/tmp/goagent-core-review-UOQQnt/base_events.go)，该文件内容来自 `git show f2e0b0b449fcfda416169356291689248e5f455b:builder/events.go`。未还原工作树。

### 正向边界：全部通过

```sh
env -u GOROOT GO_TEST_MODE=true go test ./builder -run '^(TestPlainSession|TestSessionLog|TestBuildRejectsUnavailableLoopFeatures|TestBuildAllowsPlainRetryAndFinalValidation)' -count=1 -timeout=60s
env -u GOROOT GO_TEST_MODE=true go test ./management -run '^TestWorkerRecovery' -count=1 -timeout=60s
env -u GOROOT GO_TEST_MODE=true go test -overlay=/tmp/goagent-core-review-UOQQnt/positive-overlay.json ./builder -run '^TestCoreReviewPlainUsage' -count=1 -timeout=45s
env -u GOROOT GO_TEST_MODE=true go test -overlay=/tmp/goagent-core-review-UOQQnt/recovery-overlay.json ./management -run '^TestCoreReviewRecovery' -count=1 -timeout=45s
env -u GOROOT GO_TEST_MODE=true go test ./management -run '^(TestOfflineRecoverOwnerRequiresConfirmedIdentityAndAtomicAudit|TestCompletedEvidenceWinsRecoveredCancel|TestReviewCancelWaitingRace)$' -count=1 -timeout=45s
```

独立正向测试源码为 [positive_test.go](/tmp/goagent-core-review-UOQQnt/positive_test.go)、[recovery_test.go](/tmp/goagent-core-review-UOQQnt/recovery_test.go)。临时目录可能被系统清理，故报告同时给出上述具体触发步骤、断言与源码调用链。

## 审查边界与工具记录

已读取项目 `CLAUDE.md`；项目 `.claude/skills/`、`.agents/skills/` 不存在，无需加载项目技能。八个范围文件均未被 Git ignore 排除，未发现 `.claudeignore`。当前工具环境没有项目约定的 desktop-commander、sequential-thinking、shrimp-task-manager 或专用 Read/Write；使用只读 shell 检索与文件读取、显式调用链分析、临时目录 overlay 和 apply_patch 完成替代。按用户指定的唯一仓库写入路径集中记录，不生成其他项目工作文件。

报告只包含一项实证新增缺陷；没有将测试通过扩张为全面正确保证，也没有将性能、风格偏好或既有缺陷计入发现数量。
