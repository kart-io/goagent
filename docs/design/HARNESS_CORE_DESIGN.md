# Harness 核心化设计（草案）

状态：待评审　日期：2026-10-30　参考：DeepSeek Harness（dsh）、Hermes Agent（NousResearch）、第 12 节文献

## 0. 背景与结论

`tools/coding` 已实现权限策略、人工审批、先读后改、检查点回滚、Docker 沙箱、待办清单，但这些能力被绑死在一个工具包里：

- MCP 工具、HTTP 工具、子 Agent 无法复用审批与策略；
- 文件系统与命令执行没有抽象，换远程沙箱要改工具本身；
- 循环没有"运行中插话、注入上下文、停止前续跑"这类生命周期扩展点；
- 扩展机制分散在 `core.Callback`、`core/middleware`、`contextpolicy.Policy`、`DispatchConfig`、`AgentEvent` 五处，语义各不相同。

结论：把 harness 下沉为核心设计。**Agent 循环和工具执行只定义扩展点，策略、审批、沙箱、压缩、待办、记忆都作为挂在扩展点上的组件**，`tools/coding` 退化为消费能力接口的普通工具集。

## 1. 两个参考的取舍

| 主题 | dsh | Hermes | goagent 取舍 |
|---|---|---|---|
| 内核形态 | Cordis 微内核，一切皆插件，可热替换 | 单进程 Python，facade + 阶段文件 | 不引入插件内核；用 **接口 + 有序 hook 链**，Go 静态组合 |
| 工具管线 | pre → 单调 guard → approval → execute(around) → post → result | `pre_tool_call` hook + 中间件洋葱 | 采用 dsh 四段管线 + 单调 guard |
| 审批 | 闭合四值结果，无应答者 fail-closed，审计事件 | 分层：硬线 → allowlist → 危险模式 → 会话缓存 → 人工/辅助 LLM；网关队列 + 合并重复请求 | 采用 dsh 的闭合结果 + Hermes 的 once/session 缓存与请求合并 |
| 执行环境 | fs / shell / subprocess / sandbox 四个 seam，换实现搬走全部工具 | `BaseEnvironment{RunBash, Cleanup}` + 能力位（is_remote、uses_host_filesystem…） | 采用 seam 思路，接口按 Hermes 收敛成最小集 + 能力位 |
| 先读后改 | `fs/write-intent` CAS（createIfAbsent / replaceIfVersion），按 actor 记录观察 | — | 采用 dsh：下沉到 FileSystem 观察策略 |
| 插话 | inbox 两条队列：followup / steer / inject | interrupt / steer / redirect，按会话隔离中断 | 合并两者：followup、steer、inject、redirect、interrupt |
| 循环扩展点 | pre-step / request / request-error / turn-stopping | 阶段函数返回 verdict；错误分类器给恢复提示 | 采用 dsh 四个扩展点 + Hermes 错误分类 |
| 上下文 | system 段落稳定，易变状态作 context 追加 | system prompt 全程字节不变，注入只走 user/tool；大结果落盘留预览 | 采用：**提示词缓存不变量** + 结果溢出落盘 |
| 会话 | append-only 事件日志是模型历史唯一来源，surface replace 实现压缩 | SQLite 追加写、谱系、FTS5、输入 FIFO、CAS rewind | 第三阶段采用事件日志；与 durable 日志统一 |
| 扩展 API | 事件三种分发：waterfall / serial / emit | observer（只读、fail-open）与 middleware（可改行为）分开，统一关联 ID | 采用：观察者与拦截器分开，事件带关联 ID |
| 训练数据 | — | ShareGPT 轨迹、批量运行、轨迹压缩 | 从会话日志导出轨迹，服务"模型训练"环节 |

不采用：Cordis 热重载与 profile/patch 组装（Go 无运行时插件需求）；Hermes 的 AST 自动发现工具、约 250 行正则的危险命令检测（可作为可选 guard 组件后续提供）。

## 2. 设计原则

1. **扩展点优先**：新行为挂到已有扩展点，不改循环本身；循环改动需同步更新扩展点文档。
2. **观察与拦截分离**：观察者只读、出错不影响运行；拦截器可改变行为、出错按规则处理。所有事件携带 `SessionID/TurnID/StepID/CallID/ParentID`。
3. **失败默认关闭**：需要审批而无人应答、超时、应答者缺失，一律拒绝，并把拒绝作为工具结果交给模型而非中止运行。
4. **模型可见即已记录**（第三阶段起强制）：每次模型请求都能从会话日志重建。
5. **提示词缓存不变量**：一次会话内 system 段落字节稳定；cwd、审批模式、待办等易变状态作为 context 追加到 user 消息；压缩是唯一允许打破前缀的操作。
6. **能力接口化**：工具只依赖能力接口，替换实现即可整体迁移到容器/远程沙箱。

## 3. 分层

```text
宿主（CLI / LarkBot / HTTP 服务）
  │  Agent 句柄：Run / Followup / Steer / Inject / Redirect / Interrupt
  ▼
builder.ConfigurableAgent（默认驱动器）
  turn → [PreStep] → 组装提示词 → [Request] → 模型流 → [RequestError]
       → 工具批次 ──► tools.Dispatcher 管线
       → [TurnStopping]
  │                         │ Pre → Guard → Approver → Around → 工具体 → Post → Result
  ▼                         ▼
session（第三阶段）     能力接口 harness/env：FileSystem · Executor · Sandbox
                              ▲
                  tools/coding、MCP、HTTP 等工具只消费接口
```

组件（全部挂在扩展点上，可选）：权限规则、审批传输（CLI、飞书卡片）、结果溢出、压缩、待办、计划模式、目标续跑、Claude Code 兼容 hooks、记忆、后台反思、轨迹导出。

## 4. 第一阶段：工具管线与能力接口

### 4.1 工具管线（`tools` 包，替换 `DispatchConfig.Authorize`）

```go
// 管线不修改参数：参数已写入日志并展示给人，改写会让审计与审批失真（dsh 同一取舍）。
type PreDecision struct {
	Kind   PreKind // PreAllow | PreDeny | PreAsk
	Reason string  // Deny/Ask 时交给模型或审批界面
}

// PreHook 全部执行，裁决按 Stop > Deny > Substitute > Ask > Allow 聚合（见 14.6）；都放行则进入 Guard。
type PreHook func(ctx context.Context, call *Call) (PreDecision, error)

// Guard 只能拒绝不能放行，注册顺序无法把拒绝翻成允许，用于不可绕过的硬线（工作区越界、.git 写保护）。
type Guard func(ctx context.Context, call *Call) (denyReason string)

// Around 包住工具体执行：超时、重试、指标。洋葱式，必须调用 next。
type Around func(ctx context.Context, call *Call, next func(context.Context) (*interfaces.ToolOutput, error)) (*interfaces.ToolOutput, error)

// PostHook 可接受、替换结果或阻断（以反馈替换结果），并追加下一步注入的上下文。
type PostHook func(ctx context.Context, call *Call, out *interfaces.ToolOutput) (PostDecision, error)

type PostDecision struct {
	Replace           *interfaces.ToolOutput // nil 表示接受
	AdditionalContext []string               // 本批工具结果之后作为 user 消息注入
}

// ResultObserver 在结果冻结后同步通知，只读。
type ResultObserver func(ctx context.Context, call *Call, out *interfaces.ToolOutput)

type Call struct {
	Tool         string
	CallID       string
	ExecutionKey string
	Args         map[string]any // 只读
	Scope        Scope          // SessionID、TurnID、AgentID、委派深度等关联信息
}

type Pipeline struct {
	Pre      []PreHook
	Guards   []Guard
	Approver Approver
	Around   []Around
	Post     []PostHook
	Results  []ResultObserver
}
```

执行顺序：参数校验 → Pre → （Ask 时）Approver → Guard → Around → 工具体 → Post → Results。被拒绝的调用也走 Post 与 Results，结果统一为 `tools.Rejected`，不中止运行。现有 `OnResult` 并入 `Results`；`Authorize` 删除（其中止语义由 Pre 返回 error 保留）。

### 4.2 审批（`tools` 包）

```go
type ApprovalOutcome int // AllowedOnce | AllowedSession | Rejected | Unavailable | Cancelled

type ApprovalRequest struct {
	Call          *Call
	Reason        string
	DisplayReason string
}

type Approver interface {
	Request(ctx context.Context, req ApprovalRequest) (ApprovalOutcome, string, error)
}
```

- `AllowedSession` 由管线按 `(Scope.SessionID, 工具, 匹配键)` 缓存，同会话同类调用不再询问（Hermes）。
- 同一批并发调用的相同请求合并为一次（Hermes coalesce）。
- 审批的发起与裁决写入观察事件，第三阶段落入会话日志（dsh `approval/asked|decided`）。
- 实现：`approval.Func`、命令行提示、飞书卡片（LarkBot 阶段），宿主可组合"先查规则，再问人"。

`tools/coding.Policy` 迁移为通用的 `Pre` 组件 `policy.Rules`，对任何工具生效；命令前缀匹配保留为 `run_command` 专用匹配器。

### 4.3 能力接口（新包 `harness/env`）

```go
type Capabilities struct {
	Remote    bool // 命令不在宿主机执行
	Container bool // 已有容器隔离，审批可据此放宽
	HostFS    bool // 文件工具可直接访问宿主机路径
}

type Executor interface {
	Run(ctx context.Context, cmd Command) (Result, error) // 现 coding.Runner
	Capabilities() Capabilities
}

type WriteIntent struct {
	CreateIfAbsent bool
	Version        Version // 非零时仅当当前版本一致才写入（CAS）
}

type FileSystem interface {
	Resolve(path string) (Path, error)            // 工作区限定
	Stat(ctx context.Context, p Path) (Info, error)
	ReadText(ctx context.Context, p Path) ([]byte, Version, error)
	WriteText(ctx context.Context, p Path, data []byte, intent WriteIntent) (Version, error)
	List(ctx context.Context, p Path) ([]Info, error)
}
```

- 先读后改：`Observed` 包装器按 Agent 记录读到的 `Version`，写入时转成 `WriteIntent` CAS；未读或版本变化返回类型化错误，工具转为 `Rejected`（替代 `coding.Workspace` 内的摘要表）。
- 检查点：`Checkpointed` 包装器在首次写入前保存原状态，提供 `Changes/Revert`（迁移 `coding` 现有实现）。
- 实现：`LocalFS` + `LocalExecutor`（进程组清理）、`DockerExecutor`、后续 SSH/远程沙箱。容器执行器配套的 FileSystem 通过挂载目录访问，`HostFS` 为真。
- `tools/coding` 只依赖 `FileSystem` 与 `Executor`，不再持有路径解析、摘要表、检查点。

### 4.4 结果溢出（Post 组件，Hermes）

单个结果超过阈值时写入工作区缓存目录，返回预览加文件引用，模型可用 `read_file` 分段读取；与 `contextpolicy` 的结果截断二选一，避免重复处理。阈值按模型上下文窗口比例计算。

### 4.5 并行规划

工具声明 `ConcurrencySafe(args) bool`；文件工具额外声明读写路径，Dispatcher 按"屏障 + 路径读写冲突"把一批调用切成可并行段（Hermes `_plan_tool_batch_segments`），替代 `coding` 内的全局写锁。

## 5. 第二阶段：轮次生命周期

### 5.1 Agent 句柄与收件箱

```go
type Handle interface {
	Followup(msg string) // 排到下一轮，空闲时唤醒
	Steer(msg string)    // 排到下一步：当前工具批次结束后作为 user 消息追加，唤醒
	Inject(msg string)   // 排到下一步，不唤醒；随下一次获准请求发送
	Redirect(msg string) // 取消进行中的模型请求，保留已流出的文本，追加纠正后重试；工具执行中降级为 Steer
	Interrupt(cause error)
	Wait(ctx context.Context) (*AgentOutput, error)
}
```

`assistant(tool_calls) → tool → user` 的顺序在各 provider 上都合法，Steer 因此不打断工具。LarkBot 中用户在任务进行时发来的新消息按配置走 Steer、Redirect 或 Followup（Hermes `busy_input_mode`）。

### 5.2 循环扩展点

| 扩展点 | 时机 | 能力 |
|---|---|---|
| `PreStep` | 领取收件箱输入、组装提示词之后 | 改写或拒绝输入（拒绝则无步骤结束轮次）；压缩压力检查 |
| `Request` | 发送模型请求前 | 替换模型/路由、调整参数 |
| `RequestError` | 模型请求失败 | 依据错误分类返回 Retry / Compress / Fallback / Fail |
| `TurnStopping` | 无待办工作、即将结束轮次 | 调用 Steer 续跑（目标驱动、Claude Code Stop hook） |

错误分类：`llm.ClassifyError(err) ClassifiedError{Reason, Retryable, ShouldCompress, ShouldFallback}`，复用现有 `ProviderError`；重试逻辑只读提示位（Hermes）。现有 `contextpolicy.BeforeTurn` 并入 `PreStep`；`WithModelRetry` 并入 `RequestError`。

### 5.3 提示词组装

```go
type Section struct{ Name string; Order int; Text func(Scope) string } // 进入 system，会话内稳定
type Context struct{ Name string; Order int; Text func(Scope) string } // 每步重算，作为 user 消息追加
```

工作目录、审批模式、沙箱状态、待办摘要走 `Context`；身份、规则、项目说明（`AGENTS.md`）走 `Section`。计划模式等切换只改 Context，不增删工具，保持前缀稳定（dsh `plan`）。

## 6. 第三阶段：会话事件日志

- `session.Event{Seq, Type, Time, Data, Surface}`，核心类型：`turn/start|end`、`step/start|end`、`user|system|assistant/message`、`assistant/attempt`、`tool/call|result`、`inbox/spliced`、`approval/*`、`compaction/summary`、`todo/write`。
- `DeriveMessages()` 是模型历史唯一来源；压缩以 `Surface: Replace{Start, End}` 遮蔽区间。
- 与 `core/durable` 统一：durable 的操作日志成为会话日志的投影或其一部分，恢复、回放、fork、rewind（CAS 校验期望事件序号，Hermes）都基于同一份日志。
- 持久化接口 `Store{Create, Open, Append, List, Fork}`，提供文件 JSONL 与 PostgreSQL 实现（contrib/pgstore 扩展）。
- **轨迹导出**：从日志导出 ShareGPT/OpenAI 消息格式，附工具统计，供 SFT/RL 使用；对应目标链路中的"模型训练"环节。

## 7. 现有扩展机制的归并

| 现有 | 去向 |
|---|---|
| `core.Callback` | 观察者事件（只读）；后续删除 |
| `core/middleware` 用于模型请求的部分 | `Request` / `RequestError` 扩展点 |
| `contextpolicy.Policy` | `PreStep` 组件（结果截断、历史摘要保留为组件） |
| `DispatchConfig.Authorize` / `OnResult` | 管线 `Pre` / `Results` |
| `AgentEvent`（字符串类型） | 带关联 ID 的类型化观察事件；第三阶段与会话事件对齐 |
| `coding.Policy` / `Approver` / 摘要表 / 检查点 / Runner | `policy.Rules`、`tools.Approver`、`env.Observed`、`env.Checkpointed`、`env.Executor` |

## 8. 破坏性变更与迁移

| 变更 | 迁移 |
|---|---|
| 删除 `DispatchConfig.Authorize` | 改为 `Pipeline.Pre`；需中止运行时返回 error，需交给模型时返回 `PreDeny` |
| `DispatchConfig.OnResult` 并入 `Pipeline.Results` | 签名改为 `(ctx, *Call, *ToolOutput)`，回放语义不变 |
| `coding.NewWorkspace` 改为接收 `env.FileSystem`、`env.Executor` | `coding.NewWorkspace(root)` 保留为本地实现的便捷构造 |
| `coding.Policy/Rule/Approver` 移至 `tools/policy` 与 `tools` | 类型改名，规则语义不变 |
| `WithModelRetry`、`WithContextPolicies` 重组为扩展点组件 | 第二阶段给出等价组件 |

按项目约定不保留兼容层，CHANGELOG 给出逐项迁移说明；第一阶段为 v0.22.0，二、三阶段各一个 minor。

## 9. 分阶段计划与验收

| 阶段 | 内容 | 验收 |
|---|---|---|
| 1 | 工具管线、审批、`harness/env` 能力接口、`tools/coding` 改为消费者、结果溢出、并行规划 | MCP 工具与 coding 工具共用同一策略和审批；替换为 Docker 执行器后 coding 全部测试通过；现有 builder/tools 测试通过；实测示例通过 |
| 2 | Agent 句柄与收件箱、四个循环扩展点、错误分类、提示词 Section/Context | 运行中 Steer/Redirect 的端到端测试；TurnStopping 续跑测试；system 前缀跨步骤字节不变的测试 |
| 3 | 会话事件日志、与 durable 统一、fork/rewind、轨迹导出 | 由日志重建每次请求的测试；崩溃恢复与回放一致；导出轨迹可被训练脚本读取 |

后续组件（不阻塞核心）：飞书审批卡片、Claude Code hooks 兼容、危险命令检测 guard、后台反思记忆、子 Agent fork/spawn 与深度预算、后台任务。

## 10. 待决问题的调研结论

以下结论来自文献调研（见第 12 节）。标注"建议"的条目仍待评审确认。

### 10.1 Pre 是否允许改写参数 → 建议：不允许原地改写，提供"替代调用"

| 来源 | 机制 |
|---|---|
| dsh | pre 只有 allow/deny/ask/cancel；参数已入日志并展示给人，不改写 |
| AgentSpec [1] | 执行动作为 `stop`、`user_inspection`、`invoke_action(tool, input)`、`llm_self_examine`；替换是执行一个**另行声明的安全动作**，不是修改原调用 |
| Progent [2] | forbid 后的回退：终止、交给用户决定、以消息代替工具输出（默认），均不改参数 |
| TRIAD [3]、GuardianAgentBench [4] | proceed/refuse/update：update 以反馈让 Agent 自己修订计划后重试 |

一致做法是"拒绝并反馈，由模型修订"，或"执行另一个显式记录的动作"，没有静默改写参数的。决定：

```go
type PreKind int // PreAllow | PreDeny | PreAsk | PreStop | PreSubstitute

type PreDecision struct {
	Kind       PreKind
	Feedback   string // Deny：交给模型的修订提示（对应 llm_self_examine / Progent 默认回退）
	Substitute *Call  // Substitute：以新 CallID 记录的替代调用，重新经过完整管线
}
```

`PreStop` 对应终止运行（Progent 回退一、AgentSpec `stop`）。替代调用作为独立调用记录并重新走管线，审计链完整。

### 10.2 能力接口的位置 → 建议：独立包 `harness/env`

- OpenHands [5]：Agent 抽象、事件流、运行时（Docker 沙箱）三者分离，运行时经事件流与 Agent 解耦。
- SWE-agent [6]：面向 Agent 的接口（ACI：查看、搜索、编辑、上下文管理）是独立设计对象；消融显示**带 lint 校验的编辑动作**最关键。
- CodeAct [7]：代码作为动作，比 JSON 工具调用成功率最高提升 20%、动作数最多减少 30%。执行环境需要同时服务工具调用和代码动作（Hermes `execute_code`、dsh PTC）。

运行时（执行环境）与面向模型的工具是两个层次，放进 `tools` 会让工具包依赖执行细节。决定新建 `harness/env`，`tools` 与 `tools/coding` 只依赖接口。由 SWE-agent 结论新增一个 Post 组件：编辑类工具写入后运行 linter/格式化，失败时撤销写入并把错误片段交给模型。

### 10.3 会话日志与 durable 日志 → 建议：一份存储，两种视图

- OpenHands [5]：事件流是用户界面、Agent、环境交流的唯一枢纽。
- Chronicle [8]：在非确定性边界（模型调用、工具调用、路由决策）记录不可变信封，按"边界名 + 第几次"寻址；cut-point replay 让部分边界取记录、其余用新代码实跑，把线上事故转成回归测试。论文报告每次记录中位开销 23μs。
- 持久执行（Temporal/Inngest 一类）：记住已完成的非确定性步骤，恢复时返回记录值。

会话日志回答"模型看到了什么"，durable 日志回答"哪些非确定性结果已经发生"，两者是同一事件序列的不同投影。决定：底层只有一份追加写日志；durable 的操作记录成为其中的 `boundary/*` 事件，以 ExecutionKey 作为"边界 + 次数"键；会话视图与回放视图分别投影。在 `testing` 包提供 cut-point replay，可直接把会话日志转为回归测试，也供训练轨迹回放校验。

### 10.4 "本会话允许"的缓存粒度 → 建议：缓存为参数级规则，不按工具名

- Progent [2]：策略按"工具 + 参数条件合取"匹配，强调最小权限；标识类参数用精确枚举；默认拒绝；策略带 `update` 字段，生效时向策略集追加新策略。
- Conseca [9]：按任务即时生成**可被人核对**的上下文策略，确定性执行。
- 审批疲劳：常被引用的"约 93% 的权限提示被批准"等数据都来自二手资料，没找到同行评审研究；从业者普遍建议按风险分级、批准一类而不是逐条批准、并给人展示解析后的动作。

决定：`AllowedSession` 不缓存"工具名"，而是由审批请求附带一条**拟定规则**（工具 + 参数约束，如 `run_command` 的不含 shell 控制符的命令前若干词，文件工具的路径前缀），审批界面以人可读形式展示该规则，批准后按 Progent `update` 语义追加到会话策略。低风险只读工具允许整工具放行；`run_command` 等高风险工具禁止整工具放行。

### 10.5 调研中发现的其他设计修正

| 主题 | 依据 | 修正 |
|---|---|---|
| 上下文压缩 | Complexity Trap [10]：在 SWE-agent / SWE-bench Verified 上，遮蔽旧观察与 LLM 摘要解决率相当、成本约减半，二者混合再降 7%–11%；ACON [11]：用失败样本迭代压缩指南，峰值 token 降 26%–54%，代价是额外延迟 | 第二阶段默认压缩改为**观察遮蔽**（旧工具结果替换为占位与溢出文件引用，可再读取），LLM 摘要作为超阈值后的第二级；压缩指南设计成可配置文本，便于按 ACON 做离线优化 |
| 提示词缓存 | Don't Break the Cache [12]：只缓存 system 提示最稳定，全上下文缓存可能反而增加延迟；避免动态工具定义，动态内容放末尾 | 强化第 2 节原则 5：会话内工具集合不变（模式切换只改 Context），工具 schema 排序固定 |
| 并行调用 | LLMCompiler [13]：DAG 规划并行执行，比 ReAct 延迟最多降至 1/3.7 | 核心只做模型给出的同批调用的冲突分段；DAG 规划作为可选组件，不进核心 |
| 不可信数据 | CaMeL [14]、FIDES [15]：给数据打来源/保密性标签，策略按标签确定性判定；FIDES 指出关闭策略检查后所有规划器都会被注入攻破 | `Call` 与 `ToolOutput` 预留 `Labels`（来源、可信度），Pre 组件可按标签判定；默认不启用，作为后续组件 |
| 代码动作 | CodeAct [7]、Hermes `execute_code`、dsh PTC | 后续组件：代码中的工具子调用必须逐个经过同一管线，并记录父调用 |

## 11. 修订后的第一阶段范围

在第 4 节基础上：Pre 决策采用 10.1 的五种；能力接口为 `harness/env`；审批结果 `AllowedSession` 携带拟定规则；新增编辑后 lint 校验 Post 组件；`Call`/`ToolOutput` 预留 `Labels` 字段（不实现标签传播）。

## 12. 参考文献

1. Wang, Poskitt, Sun. AgentSpec: Customizable Runtime Enforcement for Safe and Reliable LLM Agents. arXiv:2503.18666，ICSE 2026。规则语法与动作见 github.com/haoyuwang99/AgentSpec
2. Shi et al. Progent: Programmable Privilege Control for LLM Agents. arXiv:2504.11703
3. Sun et al. From Risk Classification to Action Plan Remediation: A Guardrail Feedback Driven Framework for LLM Agents（TRIAD）. arXiv:2606.05805
4. GuardianAgentBench: Where Agents Fail and How to Guard Them. arXiv:2607.20982（仅读到检索摘要，未读原文）
5. Wang et al. OpenHands: An Open Platform for AI Software Developers as Generalist Agents. arXiv:2407.16741
6. Yang et al. SWE-agent: Agent-Computer Interfaces Enable Automated Software Engineering. arXiv:2405.15793
7. Wang et al. Executable Code Actions Elicit Better LLM Agents（CodeAct）. arXiv:2402.01030，ICML 2024
8. Chawla, Koul. Chronicle: Cut-Point Replay for Regression Testing of LLM Agents. arXiv:2609.20625
9. Tsai, Bagdasarian. Contextual Agent Security: A Policy for Every Purpose（Conseca）. arXiv:2501.17070，HotOS 2025
10. Lindenbauer et al. The Complexity Trap: Simple Observation Masking Is as Efficient as LLM Summarization for Agent Context Management. arXiv:2508.21433，NeurIPS 2025
11. Kang et al. ACON: Optimizing Context Compression for Long-horizon LLM Agents. arXiv:2510.00615
12. Don't Break the Cache: An Evaluation of Prompt Caching for Long-Horizon Agentic Tasks. arXiv:2601.06007
13. Kim et al. An LLM Compiler for Parallel Function Calling. arXiv:2312.04511，ICML 2024
14. Debenedetti et al. Defeating Prompt Injections by Design（CaMeL）. arXiv:2503.18813
15. Costa et al. Securing AI Agents with Information-Flow Control（FIDES）. arXiv:2505.23643
16. Shi, Zhang, Cui. A Programming Paradigm for Spatiotemporal Composability（Cordis，dsh 底层框架）. arXiv:2608.25512

## 13. 第一阶段实施记录（2026-10-30）

已实现：`tools.Pipeline`（第 4.1、4.2、10.1、10.4 节）、`harness/env`（4.3、10.2）、`tools/policy`、`tools/coding` 改为消费者、结果溢出（4.4，`coding.SpillHook`，A1 起移至 `tools/spill`）、编辑后检查（10.2，`coding.WithLinter`）、`ToolOutput.AdditionalContext` 注入与预留 `Labels`、Builder 设置会话 ID。

与设计的差异：

| 设计 | 实施 | 原因 |
|---|---|---|
| 删除 `DispatchConfig.Authorize` | 保留 | 它在参数不合法时也执行、拒绝即中止、不写入日志，承担宿主层面的调用方授权；Pipeline 承担面向模型的策略，两者职责不同 |
| `OnResult` 并入 `Pipeline.Results` | 保留 `OnResult` | 其回放与推迟交付语义（`DeferResults`）已完备，改名没有收益 |
| 替代调用以新 CallID 独立记录 | 在原调用的日志操作内执行，结果元数据记录 `substituted_for`/`substitute` | 独立记录依赖第三阶段会话日志 |
| `Call` 与 `ToolOutput` 都预留 `Labels` | 只在 `ToolOutput` 预留 | `Call` 的标签来自参数来源追踪，第三阶段再定 |
| lint 作为调用后钩子 | 作为 `coding.WithLinter` 工具选项 | 撤销写入需要写入前的内容，只有工具内部持有 |
| 4.5 按路径冲突规划并行 | 未实现 | Builder 现按整批判断能否并发；正确性由编辑串行与条件写入保证，规划器是优化，留到第二阶段 |
| 等待审批不占并发槽位 | 等待审批时占用槽位 | 释放再获取槽位会改变 Dispatcher 的排队语义，先在文档中注明 |
| — | 准入阶段在日志操作之外，执行前先查日志：已完成的调用直接回放 | 准入在日志内会让终止与审批中取消计为失败尝试，耗尽 `MaxAttempts` 后调用永久失败，且等待审批期间持有日志锁 |
| — | 上次执行中断（pending）的调用恢复时重新经过准入，可能再次询问 | 核对（Reconcile）在日志操作内执行，先于准入核对需要拆开 `durable.Do`，收益小 |

## 14. 声明式自定义 Agent（参考 dsh preset，草案 v2）

v1 经 Codex 审核（2026-10-30，13 项，结论"修改后推进"），v2 逐项修订；对应关系见 14.13。

### 14.1 借鉴与取舍

dsh 中一个自定义 Agent 就是一行 `dsh-agent-preset`：`id`、`name`、`description` 加 `plugins` 列表，**列表即该 Agent 的全部能力**；配置在加载时按 schema 校验；多层 YAML patch 叠加，`--dump-config` 输出合并结果并标注来源；会话只保存 preset `id`。

| dsh | goagent 取舍 |
|---|---|
| Cordis 插件行，npm 包名定位插件 | 组件行 `use: <组件名>`，组件在 Go 中注册（`preset.Register`，带实现版本），不做运行时插件加载 |
| Schemastery 加载期校验 | 组件配置严格解码（未知字段即报错）；需要连接才能确定的错误（MCP 工具列表）在 Build 阶段报告 |
| patch 进不了 preset 的 `plugins`，也改不了 `disabled` | preset 按 id 合并，组件按组件 id 合并，语义见 14.3 |
| 各层同等可信 | **覆盖顺序与权限上限分离**：项目层只能收紧，扩权需宿主授权（14.2） |
| `!!js` 表达式 | 不支持；秘密只能经类型化引用注入（14.4） |
| 沙箱模式只管文件写入 | 沙箱模式是可验证的能力要求，执行环境做不到即 Build 失败（14.7） |
| 钩子按 deny > ask > allow 合并 | 合并下沉到 Pipeline：全部调用前钩子的裁决统一聚合（14.6） |
| 会话只存 preset id | 会话固定解析后的装配清单摘要，恢复时核验（14.9） |
| 模型只在子 Agent 的 `agentOptions` | 模型完全由宿主提供，preset 不声明模型 |

### 14.2 信任模型

| 层 | 文件 | 信任 |
|---|---|---|
| 内置 | 编译进二进制 | 可信 |
| 用户 | `~/.goagent/agents.yml` | 可信 |
| 项目 | `<root>/.goagent/agents.yml` | 不可信 |
| 本地 | `<root>/.goagent/agents.local.yml` | 不可信（位于仓库目录内，可被仓库中的脚本写入） |
| 宿主 | `preset.Layers.Host`（代码传入） | 可信 |

覆盖顺序为 内置 → 用户 → 项目 → 本地 → 宿主，但**权限上限只由可信层决定**：

- 可信层可声明 `limits`：沙箱模式上限、可用执行器与镜像、是否允许网络、允许的 MCP 服务与钩子命令、可引用的秘密名单。未声明时上限为 `workspace-write`、禁止网络、禁止启动宿主进程、不可引用秘密。
- **扩权项**：项目层或本地层中的下列内容需要授权才生效——启动宿主进程的组件（stdio MCP、命令钩子）、联网、引用秘密、`allow` 规则、放宽沙箱模式、删除或替换来自可信层的组件。收紧（加 `deny`/`ask` 规则、降低沙箱模式、删除自己层的组件）无需授权。
- **授权**：宿主实现 `preset.TrustStore`，按"规范化后的项目根路径 + 扩权项的规范化摘要"记录授权。Build 发现未授权的扩权项时返回 `*TrustRequiredError`（列出每一项及其来源文件），由宿主询问用户后调用 `TrustStore.Grant` 再重试；摘要变化即失效，需重新授权。不提供"跳过未授权项继续"的隐式降级。
- **锁定**：可信层组件可标 `locked: true`，项目层与本地层不能替换或删除它（尝试即 Load 报错）。
- **宿主基线**：Build 总是注入不在组件列表中、配置无法删除的安全基线——沙箱守卫（14.7）、`.git` 写保护、缺省权限 Ask（没有 permissions 组件时不会全放行）。
- **无副作用加载**：`Load`、`Dump`、信任检查都是纯函数，不启动进程、不连网络、不读取秘密的值。

### 14.3 配置格式与合并语义

```yaml
# <root>/.goagent/agents.yml
- id: coder
  name: 编码助手
  description: 在仓库中修复缺陷并提交
  components:
    - {id: persona, use: persona, config: {prefix: "你是一个严谨的编码 Agent……"}}
    - {id: instructions, use: instructions, config: {files: [AGENTS.md, CLAUDE.md], maxBytes: 32768}}
    - {id: workspace, use: workspace, config: {sandbox: workspace-write, executor: docker, image: golang:1.25, commandTimeout: 2m}}
    - {id: coding, use: coding-tools}
    - id: permissions
      use: permissions
      config:
        approval: ask
        rules:
          - {tool: run_command, command: "go test", decision: allow}   # 扩权项，需授权
          - {tool: git_commit, decision: ask}
    - {id: spill, use: spill, config: {maxBytes: 100000}}
    - id: github
      use: mcp
      config: {transport: stdio, command: github-mcp, env: {GITHUB_TOKEN: {secret: GITHUB_TOKEN}}}   # 扩权项
```

合并规则（确定性，无隐式深合并）：

- **preset**：按 `id` 合并。后层出现的标量字段替换前层，未出现保持不变，写空串即设为空；`remove: true` 删除整个 preset。
- **组件**：按组件 `id` 合并。
  - 同 id 整行替换并**保留原位置**；`config` 整体替换，不深合并。
  - 替换不得改变 `use`，否则 Load 报错；确需换实现时写 `replace: true`。
  - `remove: true` 删除；删除后在更后的层重新出现时追加到末尾。
  - 新 id 按 层顺序、文件内顺序 追加。
  - 同一层同一 preset 内重复 id 报错。
- **顺序即语义**：提示词段落、调用前/后钩子、执行包装的顺序就是最终组件列表顺序；依赖关系只用于初始化顺序，不改变钩子顺序。
- **Dump**：输出最终组件列表，每行标注来源层与文件，并列出被覆盖、被删除的行及删除者；秘密只显示引用。

### 14.4 秘密

- 只在组件配置中声明为秘密类型的字段（如 MCP 的 `env`、`headers` 的值）接受 `{secret: NAME}` 引用；提示词、规则等普通字符串字段不做任何插值，因此无需转义规则。
- 引用在 Build 阶段解析，Load 只检查语法与名单；名称必须在可信层 `limits.secrets` 中，项目层引用名单外的秘密属于扩权项。
- 缺失的秘密 Build 报错；Dump、错误信息与装配清单只出现 `secret:NAME`，不出现值。

### 14.5 组件与装配契约

组件注册信息：名称、实现版本、是否单例（`workspace`、`permissions`、`spill` 为单例）、依赖（`coding-tools`、`spill` 依赖 `workspace`）、能力需求（启动进程、网络、秘密、写入），能力需求同时用于信任检查和沙箱校验。

Build 分阶段执行，任一阶段失败都按初始化的逆序关闭已创建的资源：

1. **静态校验**（Load 已完成）：严格解码、合并、id 唯一、单例数量、依赖存在、引用的工具名可解析。
2. **信任检查**：计算扩权项并比对 `TrustStore`。
3. **沙箱校验**：执行环境能否兑现所选模式（14.7）。
4. **资源初始化**：按依赖拓扑序创建工作区、执行器、MCP 会话。
5. **工具发现**：MCP 列出工具并加命名空间（14.10）。
6. **最终校验**：工具名全局唯一、规则引用的工具存在、组合约束成立（如 spill 自带读取工具）。
7. **发布装配结果**。

`preset.Build(ctx, def, preset.Host{Root, Approver, TrustStore, ArtifactDir})` 返回 `*Assembly`，而不是 Agent：

- `SystemPrompt`：按组件顺序拼接的段落。
- `Tools`：已经过 Dispatcher 与 Pipeline 的工具，交给 `builder.WithTools`。
- `Workspace`、`Manifest`（14.9）、幂等的 `Close()`。
- 组件可以贡献：提示词段落、工具、`PreHook`、`Guard`、`Around`、`PostHook`、执行器、需关闭的资源。

宿主负责：模型、稳定的 `SessionID`、持久化作用域、预算与并发上限，以及 `Approver`。`approval: never` 时 Ask 自动拒绝；`approval: ask` 而宿主未提供 `Approver` 时结果为 Unavailable，同样按拒绝处理。

### 14.6 权限

**管线级聚合（Pipeline 破坏性变更）**：现行 `gate` 在第一个非 Allow 裁决处停止，Ask 获批后后续钩子的 Deny 不再执行，钩子顺序会改变权限结果。改为：

- 全部 `PreHook` 都执行，裁决按 Stop > Deny > Ask > Allow 聚合。
- `Substitute` 只在没有 Stop/Deny 且只有一个钩子提出时生效，多个钩子提出时报错。
- 多个 Ask 合并为**一次**审批，理由逐条列出；只有每个提出 Ask 的钩子都给出 `SessionGrant` 时才提供"本会话允许"，记录的授权是这些 Grant 的组合，必须全部匹配才算命中。因此某一来源的授权不会满足另一来源的 Ask。
- 会话授权属于一个 Pipeline 实例；每次 Build 创建新实例，配置变化后旧授权不迁移。
- 不可删除的硬限制放在宿主基线的 `Guard` 与执行环境中，不依赖钩子。

**`tools/policy` 求值改为 deny > ask > allow**：在所有匹配的规则中取最严格的裁决，`Default` 只在没有规则匹配时使用。现有 `coding.DefaultPolicy` 的规则互不重叠，行为不变，由测试锁定。可信层与项目层的规则合并为同一策略；由于 deny 优先，项目层的 `allow` 无法越过可信层的 `ask`/`deny`，但能放行可信层未覆盖的调用，所以属于扩权项。

### 14.7 沙箱

沙箱模式写在 `workspace` 组件上，是对执行环境的**可验证要求**：

| 模式 | 文件工具 | 命令（含 git、lint） |
|---|---|---|
| `read-only` | 文件系统包装为只读，写入与删除一律拒绝 | 只能在以只读方式挂载工作区的容器中执行；执行器做不到则禁用命令工具 |
| `workspace-write` | 写入限于工作区（`LocalFS` 路径限定） | 只能在只挂载工作区与临时目录的容器中执行 |
| `danger-full-access` | 同上 | 可在宿主机执行 |

- `harness/env` 补充能力：`DockerExecutor` 增加只读挂载；`Capabilities` 增加 `ReadOnlyRoot` 等字段，由执行器如实声明。
- 网络按 `limits` 与组件配置决定，执行器声明的 `Network` 不符即 Build 失败。
- `LocalExecutor` 不提供隔离：选它而模式低于 `danger-full-access` 时 Build 失败，**不静默降级**。
- 命令前缀规则不是隔离手段：放行 `go test` 等于允许执行仓库代码，文档明确说明。
- stdio MCP 与命令钩子在宿主机运行、不受沙箱约束，因此列为扩权项，并标注"不受沙箱保护"。

### 14.8 结果溢出

- 溢出文件写入宿主提供的 `ArtifactDir`（默认在用户缓存目录），按会话与执行键分目录，不写工作区，因此不受只读模式影响，也不计入 `Changes()`。
- spill 组件自带只读工具 `read_spill`，不依赖 `read_file` 是否被裁掉。
- 写入失败时返回截断的原结果并附说明，不报错，避免已产生副作用的工具被重新执行。
- 保留期由 `retention` 配置，宿主负责清理；恢复时文件缺失，`read_spill` 返回明确的拒绝结果。

### 14.9 恢复与装配清单

`Assembly.Manifest` 是解析后执行语义的规范化 JSON 及其摘要：

- 最终组件列表（含配置与顺序，秘密只存引用）与各组件的实现版本；
- 工具清单：名称、schema 摘要、`DurableIntentVersion`、MCP 工具来源（服务与工具列表快照摘要）；
- 提示词全文摘要（含指令文件内容）；
- 工作区根路径、执行器种类与镜像、沙箱模式、权限规则。

持久运行开始时把清单摘要记入日志；恢复时摘要不一致即拒绝（`ErrManifestChanged`），由宿主决定新建会话。不提供自动迁移。摘要只保证执行语义一致，外部副作用仍依赖各工具的核对（Reconcile）。

### 14.10 MCP

- **工具名**：工具名为 `mcp__<组件id>__<工具名>`；可配别名，别名同样须全局唯一，权限规则按最终名称匹配。
- **超时**：启动和工具发现各有超时。
- **工具列表**：Build 时取快照，服务端之后的变更在下次 Build 才生效。
- **失败处理**：
  - 远端返回的错误结果（`IsError`）作为 `tools.Rejected` 交给模型，不中止运行。
  - 传输错误中止本次调用；MCP 工具的日志策略不自动重试，因为副作用无法确认。
- **关闭**：`Close` 幂等，取消在途调用，按初始化的逆序关闭会话与进程。

### 14.11 子 Agent（B 期）

- Load 检查 preset 引用图，有环即报错；嵌套深度经 ctx 传递，受 `maxDepth` 约束。
- 子 Agent 的权限取三者交集：父 Agent 的有效权限、目标 preset 的权限、宿主上限。
- `toolFilter` 在资源初始化之前生效，工具全部被过滤掉的组件不初始化，也不启动进程。
- 子 Agent 使用独立的 Dispatcher 与并发槽位，避免父调用占满槽位造成死锁；继承父调用的取消与总预算。

### 14.12 分期与验收

| 期 | 内容 | 阻断验收 |
|---|---|---|
| A0 | Pipeline 裁决聚合与组合授权；`tools/policy` 改为 deny > ask > allow。独立发布（破坏性） | 钩子顺序不影响结果；获批 Ask 之后的 Deny 生效；单一来源的授权不满足另一来源的 Ask；`DefaultPolicy` 行为不变 |
| A1 | `harness/preset`：格式、合并、严格校验、无副作用 Dump、信任模型、宿主基线、沙箱校验（含 Docker 只读挂载）、组件 `persona`、`instructions`、`workspace`、`coding-tools`、`permissions`、`spill`；`Assembly` 与 `Manifest`；示例改为配置驱动 | 恶意项目配置（扩权、删除锁定组件、引用秘密）被拦截且 Load 无副作用；执行器做不到所选模式时 Build 失败；合并语义逐条有测试；部分初始化失败时资源被关闭 |
| A2 | `mcp` 组件（命名空间、生命周期、失败契约）；持久运行记录并核验清单摘要 | 工具重名报错；秘密在 Dump、错误、清单中脱敏；配置变化后恢复被拒绝 |
| B | `claude-code-hooks`（PreToolUse/PostToolUse，扩权项）、`skills`（`SKILL.md` frontmatter）、`subagent` | 子 Agent 不能扩大父 Agent 的权限；引用有环时报错 |
| C | 依赖第二阶段循环扩展点：UserPromptSubmit/Stop/SessionStart 钩子、计划模式、压缩组件 | — |

### 14.13 Codex 审核意见处理

| # | 意见 | 处理 |
|---|---|---|
| 1 | 项目配置被当作授权来源 | 14.2 信任模型：项目层只能收紧，扩权需授权并绑定摘要，Load/Dump 无副作用 |
| 2 | 沙箱承诺超出实现 | 14.7：模式即可验证要求，做不到即 Build 失败；补 Docker 只读挂载 |
| 3 | 改规则优先级不能保证全管线 deny 优先 | 14.6：Pipeline 聚合全部裁决，组合授权（A0） |
| 4 | 只绑定 preset id 不足以保证恢复 | 14.9：装配清单摘要，不一致拒绝恢复 |
| 5 | 规则优先级是独立的破坏性变更 | 拆为 A0 单独交付；`Default` 只在无匹配时使用 |
| 6 | 合并规则不完整 | 14.3：位置、删除、重加、改 `use`、重复 id、标量清除逐条规定 |
| 7 | 装配契约不完整 | 14.5：单例与依赖、分阶段 Build、失败逆序清理、补上 `Around`、缺省权限 Ask |
| 8 | MCP 名称空间与生命周期 | 14.10 |
| 9 | `${ENV}` 与 Dump 泄漏秘密 | 14.4：取消文本插值，只接受类型化秘密引用并脱敏 |
| 10 | Build 返回值、模型、审批不明确 | 14.5：返回 `Assembly`；模型由宿主提供；`never`/无 Approver 的语义 |
| 11 | spill 与只读、裁剪、恢复冲突 | 14.8：写入宿主产物目录，自带 `read_spill`，失败不重跑 |
| 12 | 子 Agent 授权与资源约束 | 14.11 |
| 13 | A 期范围过宽，缺少阻断验收 | 14.12：拆为 A0/A1/A2，每期列出阻断验收 |

### 14.14 A0、A1 实施记录（2026-10-30）

A0（`2fc040b`）：Pipeline 裁决聚合与按钩子记录授权；`tools/policy` 取最严格者。A1：`harness/preset`、`tools/spill`、Docker 只读挂载与 `env.ReadOnly`，示例改为配置驱动。

与 14.2–14.8 的差异：

| 设计 | 实施 | 原因 |
|---|---|---|
| 配置文件为 preset 列表 | 顶层为 `{limits, presets}` | 权限上限需要与 preset 并列声明，且只允许出现在可信层 |
| permissions 为单例 | 可多行，全部规则合并为一个策略 | 不可信层新增一行 deny/ask 即可收紧，无需替换可信层的行（替换属于扩权项） |
| 沙箱模式写在 permissions 上 | 写在 workspace 上 | 沙箱约束的是执行环境，与工作区一起创建、一起校验 |
| 规则引用的工具须存在 | 只检查用户书写的规则 | `base: coding` 等内置规则在只读模式下会引用被裁掉的写入类工具 |
| 宿主基线含 `.git` 写保护守卫 | 由 coding 工具自身拒绝写入 `.git` | 已有实现，重复的守卫没有收益 |
| 删除不存在的组件给出警告 | 报错 | 合并结果须确定，静默忽略会掩盖拼写错误 |
| 秘密名单外的引用 | 来自可信层的引用不受名单约束 | 名单是对不可信层的上限 |

未在 A1 验证：`workspace-write` 加 Docker 执行器的端到端运行（本机无 Go 镜像）；只读挂载由 `harness/env` 的 Docker 测试覆盖。

### 14.15 A2 实施记录（2026-10-30）

`mcp` 组件与 `Assembly.PinManifest`。与 14.9、14.10 的差异：

| 设计 | 实施 | 原因 |
|---|---|---|
| 持久运行开始时由 Builder 记录清单摘要 | 宿主在调用 Agent 前执行 `Assembly.PinManifest(ctx)` | 不需要改动 Builder：摘要作为一条持久操作记在宿主的运行键下，与 Agent 自己的日志相邻 |
| 恢复时摘要不一致即拒绝 | 只比对已完成的记录；记录不存在或未完成时重新写入 | 首次运行在写入前中断时，没有可比对的记录 |
| MCP 工具"不自动重试" | 工具声明 `MaxAttempts: 1` 的持久日志策略 | 中断或失败的调用在恢复时返回需要宿主处理的错误，而不是重跑 |
| 清单记录服务身份与工具列表快照 | 记录服务名称与版本、每个工具的 schema 与描述摘要及来源组件 | 服务名称与版本来自握手结果，工具摘要覆盖列表快照 |
| — | stdio 服务只继承 PATH、HOME、USER、LANG、TMPDIR | 避免把宿主的全部环境（含凭据）交给第三方进程，其余变量须在 `env` 中显式列出 |
