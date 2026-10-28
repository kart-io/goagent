# 可组合编排：真实模型案例

八个案例用真实模型（DeepSeek、MiniMax、Gemini）验证 R2 新增能力：根级共享预算、Runnable 工具化与子 Agent 会话、多级嵌套、上下文卸载与持久引用、自定义上下文策略、持久图的循环与审批、观测事件与离线报告、增量流原型。业务数据全部是合成的，不连接真实订单、支付或运维系统。

| 案例 | 验证的能力 | 关键断言 |
| --- | --- | --- |
| [工单分诊](ticket-triage/README.md) | `NewRunnableTool` 包装规则图、`NewRunnableToolFactory` 独立子 Agent、持久根预算、`CleanupTimeout` | 3 张工单结论正确；子会话创建数等于清理数；同目录重跑零请求、计量不变 |
| [长日志排障](log-triage/README.md) | `contextpolicy.Offload` 持久卸载、`read_context_result` 分页、跨进程恢复 | 日志被卸载为引用；读第一页时进程退出，新进程恢复不重发已完成请求；第三次纯回放 |
| [并行诊断与观测报告](parallel-diagnostics/README.md) | `AddParallel` 多 Agent、`observe` + OTel、`devtools.Recorder`、`Describe`、`WriteReport` | 三个分支并发且结论正确；所有 span 在宿主 trace 下；事件不含提示词和正文；报告无脚本 |
| [流式摘要与预算争用](stream-budget/README.md) | `core/flow` 的 Source/Map/Fold、根预算并发准入 | 预览拼接等于终态；逐 token 模型首段早于完成；3 个子 Agent 共享 2 次额度时恰好 1 个在请求前被拒 |
| [文稿审校循环](review-loop/README.md) | 持久图 `AddBranch` 循环、轮次上限、节点级检查点 | 第一稿必然被打回；第 2 轮开始前退出，恢复后不重跑第 1 轮；宿主独立核对终稿事实 |
| [多级委派](delegation/README.md) | 三层嵌套（Agent → 子 Agent → 图工具）、`MaxDepth`、Dispatcher 重入保护 | 答案精确到分；三层请求都计入根预算；深度超限和重入在执行前快速失败 |
| [人工审批暂停](approval/README.md) | `ApprovalNodes` 暂停与恢复、条件路由、外部效果幂等 | 大额暂停、错误审批键被拒；打款后崩溃，恢复时不重复付款也不再请求模型 |
| [本地化](localize/README.md) | 自定义 `contextpolicy.Policy`、配置哈希进入意图、同节点多 Agent 作用域 | 同名工具、相同调用 ID 也不串日志；改术语表后在请求前拒绝复用旧日志 |

## 运行

从仓库根目录执行，建议 Go 1.25。密钥只从环境变量读取：

| 变量 | 说明 |
| --- | --- |
| `GOAGENT_LIVE_PROVIDER` | `deepseek`（默认）、`minimax` 或 `gemini` |
| `DEEPSEEK_API_KEY` / `DEEPSEEK_MODEL` | DeepSeek 密钥；模型默认 `deepseek-flash` |
| `MINIMAX_API_KEY` 或 `MINIMAX_CN_API_KEY` / `MINIMAX_MODEL` | MiniMax 密钥（后者走国内端点）；模型默认 `MiniMax-M3.1-Flash`；`MINIMAX_BASE_URL` 可覆盖地址 |
| `GEMINI_API_KEY` / `GEMINI_MODEL` | Gemini 密钥；模型默认 `gemini-2.5-flash`，走原生 GenAI SDK |
| `GOAGENT_LIVE=1` | 测试才会调用真实模型；未设置时 Live 测试跳过 |

```sh
# 单个案例直接运行
GOAGENT_LIVE_PROVIDER=deepseek go run ./examples/orchestration/live/ticket-triage -dir /tmp/triage -ticket A1001

# 全部测试：离线部分总会运行；设置 GOAGENT_LIVE=1 才调用真实模型
go test ./examples/internal/livemodel/ ./examples/orchestration/live/...
GOAGENT_LIVE=1 GOAGENT_LIVE_PROVIDER=deepseek go test -race -v ./examples/orchestration/live/...
GOAGENT_LIVE=1 GOAGENT_LIVE_PROVIDER=minimax  go test -race -v ./examples/orchestration/live/...
GOAGENT_LIVE=1 GOAGENT_LIVE_PROVIDER=gemini   go test -race -v ./examples/orchestration/live/...
```

一轮全部 Live 测试约 40 次模型请求（随模型翻页次数和审校轮次浮动）。评测结果见 [2026-10-28 真实模型编排案例](../../../docs/evaluation/2026-10-28-orchestration-live/REPORT.md)。

## 模型接入

`examples/internal/livemodel` 统一创建客户端：

- 不做隐式重试。预算按“请求前准入”控制，重试会让实际请求超过准入数。
- 统计真实发出的请求，回放不会进入传输层，所以测试可以断言回放零请求。
- `StreamMode` 声明真实流能力：DeepSeek 逐 token；Gemini 也是流式，但分块很粗（实测 2 段）；MiniMax 一次性返回，预览只有一段。
- `Wrap` 可包装本地模拟模型，离线测试复用同一份案例代码。

MiniMax 适配器从 `examples/workflow/business-cases/minimax.go` 移植，两处暂时各自维护。

## 结论的边界

- 模型结论用 `结论：X` 行解析，只接受预先声明的取值；取不到即失败，不猜测。
- 测试断言的是**按规则应得的结论**和编排不变量（调用次数、预算、回放、清理、trace）。模型补充的处置建议没有被校验，可能包含日志之外的推测。
- 单次 Live 运行只说明该模型在这组合成样例上的表现，不代表线上质量或性能。
