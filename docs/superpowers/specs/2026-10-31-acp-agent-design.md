# ACP Agent 端设计

基线 goagent v0.32.0（ecde482）。协议、范围、位置由用户确认：Zed 的 Agent Client Protocol（ACP），完整 Agent 端，放在 goagent-contrib 的独立模块，基于社区 SDK。

## 目标与非目标

让任意 `builder` 构建的 Agent 以 ACP Agent 身份经 stdio 运行，被 Zed、JetBrains、Neovim 等 ACP 客户端（下称"编辑器"）驱动：流式回答、工具调用通知、工具审批、取消、会话恢复，以及把文件读写与命令执行交给编辑器。

非目标：

- ACP Client 端（goagent 调用外部 ACP Agent）。
- 编码工具（读写文件、执行命令、搜索）。它们在 goagent-harness，本模块只提供它们依赖的 `env.FileSystem` 与 `env.Executor`。
- 认证（`authenticate`、`logout`）、会话模式与配置项（`session/set_mode`、`session/set_config_option`）、unstable 方法、图片与音频输入、`thought` 与 `plan` 通知（goagent 的流式事件没有推理与计划）。
- goagent 主模块不做改动。

## 位置与依赖

新模块 `github.com/kart-io/goagent-contrib/acp`，独立 `go.mod`（Go 1.25），依赖：

- `github.com/kart-io/goagent` v0.32.0：`builder`、`tools`、`core/session`、`harness/env`、`llm`。
- `github.com/coder/acp-go-sdk` v0.13.5：Apache-2.0，零依赖，协议版本 1。它是 ACP 官网社区库中最成熟的 Go 实现，ACP 没有官方 Go SDK。它仍是 v0，升级可能有破坏性变更，放在独立模块使这种变更不波及主模块。

SDK 的 `AgentSideConnection` 负责 JSON-RPC 编解码与请求分派，并在 `session/cancel` 时取消对应 `session/prompt` 的 ctx，本模块不重复实现。

发布：tag `acp/v0.1.0`；`scripts/test-all.sh` 与 `lint-all.sh` 按 `go.mod` 枚举模块，自动覆盖新模块；README 模块表新增一行。

## 对外 API

```go
package acp

// Factory 为每个 ACP 会话构建一个 Agent。goagent 的 SessionID 是 Builder 配置，所以一个会话对应一个 Agent。
// 构建时必须 WithSessionID(s.ID())，工具经 s.Pipeline() 所在的 Dispatcher 注册；
// 配置了会话存储时还须 WithSessionLog 使用同一个 Store。
type Factory func(ctx context.Context, s *Session) (Runner, error)

// Runner 是本模块对 Agent 的最小要求，*builder.ConfigurableAgent[C, S] 满足它。
type Runner interface {
    ExecuteStream(ctx context.Context, input any) (<-chan builder.AgentEvent, error)
}

type Session struct{ /* 未导出字段，只读 */ }

func (s *Session) ID() string
func (s *Session) Cwd() string                      // 编辑器给出的工作区绝对路径
func (s *Session) MCPServers() []acpsdk.McpServer   // 原样转交，宿主可用 mcp/remote 连接
func (s *Session) Pipeline() *tools.Pipeline        // 已装好通知与审批钩子，交给宿主的 tools.Dispatcher
func (s *Session) FileSystem() env.FileSystem
func (s *Session) Executor() env.Executor

type Server struct{ /* … */ }

func NewServer(f Factory, opts ...Option) *Server
// Serve 处理一条连接直到对端断开或 ctx 结束，通常传 os.Stdin 与 os.Stdout。
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error

func WithAgentInfo(name, version string) Option
func WithSessionStore(store session.Store) Option              // 开启 session/load 与 session/list
func WithToolKind(kind func(tool string) acpsdk.ToolKind) Option // 默认全部为 other
```

`acpsdk` 指 `github.com/coder/acp-go-sdk`。stdout 被协议占用，日志须写 stderr，README 写明。

## 方法映射

| ACP | 行为 |
|---|---|
| `initialize` | 回报 `ProtocolVersionNumber`（1）、`agentInfo`、`loadSession`（配置了 Store 时为真）、prompt 能力只声明 `embeddedContext`；记下客户端的 `fs.readTextFile`、`fs.writeTextFile`、`terminal` 能力 |
| `session/new` | 生成 ID（UUID），构造 `Session`，调用 Factory；失败返回错误且不登记会话 |
| `session/load` | 未配置 Store 返回 method-not-found；会话不存在返回 invalid-params。否则 `Read` 全部事件，以同一 ID 调用 Factory（失败时直接报错、不发回放），再按 `session.Derive` 推导的顺序回放（见下） |
| `session/list` | 未配置 Store 返回 method-not-found；否则返回 `Store.List` 的会话，`cwd` 取当前连接 `session/new` 时记录的值，未知时取空 |
| `session/prompt` | 见下节 |
| `session/cancel` | 由 SDK 取消 prompt 的 ctx |
| `session/close` | 丢弃会话的 Runner 与 Session |
| 其他方法 | method-not-found；`authenticate` 返回空响应（`initialize` 不声明认证方式） |

同一会话同时只允许一个 prompt：SDK 在新 prompt 到达时取消旧 prompt，本模块在旧 prompt 返回后才开始新的执行。

### session/prompt

1. 把 prompt 内容块拼成输入字符串：`text` 原样；`resource`（嵌入文本资源）以 `<资源 uri>` 加内容的形式附在后面；`resource_link` 只写 URI；其他类型返回 invalid-params。
2. 调用 `Runner.ExecuteStream`，按事件发 `session/update`：

   | goagent 事件 | ACP |
   |---|---|
   | `text`（Delta） | `agent_message_chunk` |
   | `step` tool_start / tool_end | 不发；工具通知由 Pipeline 钩子发出，它们带 CallID 与参数 |
   | `sources`、`usage` | 不发 |
   | `error` | 结束执行，返回错误 |
   | `done` | 结束，`stopReason` 为 `end_turn` |

3. ctx 被取消时（`session/cancel` 或连接断开）停止读取事件，返回 `stopReason: cancelled`，不返回错误，这是协议要求。
4. `ExecuteStream` 同步返回错误或流中出现 `error` 事件时，返回 JSON-RPC 内部错误，消息为中文并保留原错误文本。

### session/load 回放

按 `session.Derive` 的顺序（它比 `DeriveMessages` 多带工具结果的成败）：用户消息发 `user_message_chunk`；助手消息的文本发 `agent_message_chunk`，其中每个工具调用发 `tool_call`（状态 pending，带参数）；`tool/result` 消息发 `tool_call_update`（completed 或 failed，附结果文本）。被 `session/rewind` 撤销与中止轮次中的事件不回放，这由 `DeriveMessages` 保证。回放完成后才返回 `session/load` 响应。

## 工具调用与审批

`Session.Pipeline()` 返回的 `tools.Pipeline`：

| 阶段 | 行为 |
|---|---|
| `Pre[0]` | 发 `tool_call`：`toolCallId` 为 `Call.CallID`（goagent 只在持久运行中设置它；为空时生成会话内唯一的 `call-<n>` 并写回 `Call.CallID`，供后续阶段沿用），`title` 为工具名，`kind` 由 `WithToolKind` 决定，`rawInput` 为 `Call.Args`，状态 pending；返回 PreAllow |
| `Approver` | 只在某个 PreHook 返回 `PreAsk` 时由 Pipeline 调用。发 `session/request_permission`，选项：允许一次（allow_once）、本会话允许（allow_always，仅当 `ApprovalRequest.Grant` 非空时提供，名称附 `Grant.Describe()`）、拒绝（reject_once），分别映射为 `ApprovalAllowedOnce`、`ApprovalAllowedSession`、`ApprovalRejected`；响应为 cancelled 或请求因 ctx 结束失败时返回 `ApprovalCancelled` |
| `Around[0]` | 执行前发 `tool_call_update` 状态 in_progress，并把 CallID 放入 ctx 供 Executor 关联终端；工具返回 error 时发 failed |
| `Post[0]` | Pipeline 对成功与被拒（`tools.RejectedResult`）结果都运行 Post。发 `tool_call_update`：成功为 completed（`rawOutput` 为 `Result`），被拒为 failed（内容为拒绝原因）；`content` 为该调用的终端引用加结果文本 |

宿主通过向返回的 Pipeline 追加返回 `PreAsk` 的 PreHook 来要求审批（带 `Grant` 时编辑器可选"本会话允许"），不追加则所有调用直接执行；也可追加 `Guards`、`Around`、`Post`，但不应替换本模块装好的第 0 项与 `Approver`。本模块的 Post 先于宿主追加的 Post 运行，宿主 Post 替换的结果不反映在通知中。

准入阶段出错或终止（PreHook 返回 error 或 PreStop、审批请求失败、ctx 结束）时 Around 与 Post 都不运行。参数错误的结果不经过 Post，由 Around[0] 识别（`tools.ArgumentErrorResult`）并立即发 failed。非持久运行中宿主 PreHook 用 `PreSubstitute` 替代调用时，Dispatcher 以空 CallID 新建替代调用，本模块无法把它与原调用关联：编辑器中显示为两个调用，原调用在轮末标为"调用未执行（可能已被替代或中止）"。本模块按会话记录已发出 `tool_call` 但未结束的调用，在该次 prompt 返回前为它们补发 failed，避免编辑器中留下悬挂的调用。

通知经 `Call.SessionID` 定位会话；ID 与 `Session.ID()` 不一致时钩子返回错误，工具调用失败，错误说明须 `WithSessionID(s.ID())`。工具不经 `s.Pipeline()` 所在的 Dispatcher 注册时不会有工具通知与审批，本模块无法检测，文档写明。

## env 转发

### FileSystem

客户端同时声明 `fs.readTextFile` 与 `fs.writeTextFile` 时（只声明其一时读写的版本来源会不一致，因此全部走本地）：

- `ReadFile` 先经 `LocalFS.Stat` 判断存在（ACP 没有标准的"不存在"错误码，不存在时返回 LocalFS 的错误，`errors.Is(err, fs.ErrNotExist)` 成立），再经 `fs/read_text_file` 读取，能读到编辑器中未保存的内容；版本为内容的 SHA-256 十六进制，与 `LocalFS` 一致。只存在于编辑器、尚未落盘的新缓冲区读不到。
- `WriteFile` 经 `fs/write_text_file`，编辑器可展示改动。`CreateIfAbsent` 在转发前经 `LocalFS.Stat` 检查（存在时返回 `env.ErrExists`），`IfVersion` 经上述 `ReadFile` 比对版本（不同时返回 `env.ErrStale`），编辑器不参与协调，检查与写入之间的修改无法发现，文档写明为尽力而为。`Perm` 不转发。父目录不存在时先在磁盘上创建（`env.FileSystem` 要求按需创建父目录，ACP 未规定编辑器会这样做）；`IfVersion` 而文件不存在时返回 `env.ErrStale`，目标是目录时报错，均与 `LocalFS` 一致。
- 路径：`Resolve` 及路径限定沿用以 `Cwd` 为根的 `env.LocalFS`，转发时使用解析后的绝对路径（ACP 要求绝对路径）。
- `Stat`、`Walk`、`Remove` 是 ACP 没有的操作，委托给该 `LocalFS`。

客户端未声明时全部委托给 `LocalFS`。

### Executor

客户端声明了 `terminal` 时，`Run` 依次调用 `terminal/create`（`command` 为 `Argv[0]`，`args` 为其余，`cwd` 为解析后的工作目录）、`terminal/wait_for_exit`、`terminal/output`、`terminal/release`。ctx 结束时先 `terminal/kill` 再 `release`，返回 ctx 错误并带已取得的输出。非零退出返回 `Result` 与 nil，与 `env` 约定一致；输出被客户端截断时在 `Result` 中照实保留截断后的内容。工具调用中的终端会以 `terminal` 内容块附到对应的 `tool_call_update`，使编辑器内嵌显示终端（调用经 ctx 关联到当前 CallID；关联不上时不附加）。`Capabilities` 为 `{Remote: false, Network: true}`。

未声明时委托给 `env.LocalExecutor{Root: Cwd}`。

## 并发与生命周期

- 一条连接可有多个会话，会话间并发；每个会话的通知按产生顺序发送（SDK 写出串行）。
- 连接断开时取消全部进行中的 prompt 与审批，`Serve` 返回 nil；`ctx` 结束时返回 `ctx.Err()`。
- 会话与 Runner 在 `session/close` 或连接结束时释放；Factory 构建的资源由宿主在 Factory 中自行管理（如用 `plugin.Host`），本模块不提供关闭钩子。

## 错误处理

错误消息为中文，以 `errors.Is/As` 保留原错误链；返回给编辑器的 JSON-RPC 错误用 SDK 的 `NewInvalidParams`、`NewMethodNotFound`、`NewInternalError`。不恢复宿主代码的 panic（Runner 内部已转为错误）。

## 测试

单元与集成测试（`-race`），用 `io.Pipe` 连接本模块与 SDK 的 `ClientSideConnection`（测试中的假编辑器），模型用 `llm.MockStreamClient`：

- 握手：能力回报随 `WithSessionStore` 变化；记录客户端能力。
- prompt：文本增量按序到达，`end_turn`；内容块拼接；不支持的块返回 invalid-params；Factory 与执行错误返回内部错误。
- 工具：pending → in_progress → completed/failed 的顺序与 CallID、参数；审批的允许一次、总是允许（同会话第二次不再请求）、拒绝、取消；被拒调用补发 failed；SessionID 不一致报错。
- 取消：执行与审批等待中取消均返回 `cancelled`；连接断开后 `Serve` 返回。
- session/load：回放顺序与内容，rewind 与中止轮次不回放，之后继续对话；未配置 Store 与会话不存在。
- FileSystem 与 Executor：转发与回退两条路径，条件写入的冲突，终端 kill 与 release。

## 案例（goagent-examples）

`orchestration/acp`（放在 `orchestration/` 下才能复用 `orchestration/internal/demomodel`；`internal/livemodel` 在仓库根，任意目录可用）：

- `main.go`：stdio ACP Agent，Factory 中用 `s.FileSystem()` 与 `s.Executor()` 构造两个小工具（读文件、执行命令），工具经 `tools.NewDispatcher(reg, tools.DispatchConfig{Pipeline: s.Pipeline()})` 注册；离线模型为 `demomodel`，`GOAGENT_LIVE=1` 与 `DEEPSEEK_API_KEY` 时用 `livemodel`。
- `main_test.go`：离线，用 SDK 的 Client 端驱动，验证一轮含审批与工具调用的对话。
- `README.md`：Zed `settings.json` 的 `agent_servers` 配置与运行方式。

## 风险

- SDK 仍是 v0，升级时须跟随其破坏性变更；只影响本模块。
- 条件写入在编辑器侧无法原子保证，见 env 转发。
- 宿主若不按约定装配（未用 `s.Pipeline()`、未设 `WithSessionID`、未用同一 Store），工具通知、审批或会话恢复将缺失，只能部分检测（SessionID 不一致、Store 中无该会话）。
