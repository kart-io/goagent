# 多级委派：三层嵌套与共享根预算

```text
经理 Agent ──finance_expert──▶ 财务专家子 Agent ──fx_convert──▶ 规则图（查汇率 → 换算）
                                                └─tax_calc───▶ 函数工具
```

- `finance_expert` 由 `NewRunnableToolFactory` 创建。每次调用新建一个专家，带唯一 `SessionID` 和独立的 Dispatcher。
- `fx_convert` 是用 `NewRunnableTool` 包装的两节点图，位于嵌套第 2 层。
- 三层共用一份持久根预算（额度 10）。换算和计税都由工具完成，模型不做算术，最终答案可以精确校验：1200 × 7.10 × 1.06 = 9031.20 元。

```sh
GOAGENT_LIVE_PROVIDER=gemini go run ./examples/orchestration/live/delegation -dir /tmp/procure-1
```

离线测试用脚本化模型验证两条宿主配置错误，结果是确定的：

| 测试 | 配置 | 预期 |
| --- | --- | --- |
| `TestScriptedDepthLimit` | 专家工具 `MaxDepth=1` | 第 2 层 `fx_convert` 在执行前被拒，返回 `tools.ErrNestedExecutionLimit` |
| `TestScriptedDispatcherReentry` | 专家复用经理正在占用的 Dispatcher | 在排队等槽位之前被拒，返回 `tools.ErrDispatcherReentry`，不会死锁 |

两种情况都是整条调用链快速失败，错误不交给模型，避免模型绕过限制继续执行。出错路径上子会话同样会被清理。`TestScriptedDelegation` 覆盖正常路径与回放。

`TestLiveDelegation` 用真实模型检查四件事：答案精确到分；每层都执行了；三层请求数都计入根预算；同目录再次运行时 0 次请求，也不新建专家。
