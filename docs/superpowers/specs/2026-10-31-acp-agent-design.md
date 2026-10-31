# ACP Agent 端设计

基线 goagent v0.32.0（ecde482）。2026-10-31 修订：落实[调研发现](../../research/2026-10-31-new-plugin-acp-review.md)，替换对 SDK 自动取消、资源自动释放及编辑器条件写入的错误假设。

状态：主仓维护设计规格；ACP 已有实现位于 goagent-contrib/acp。本轮已在其 fix/acp-contracts-20261031 分支落实契约修复，执行与验证记录见 [执行记录](../../../../goagent-contrib/docs/ACP_CONTRACT_FIXES.md)；新修复尚未发布。

## 目标与范围

在 goagent-contrib 独立模块提供 Zed Agent Client Protocol（ACP）的 Agent 端，使普通 builder Agent 可经 stdio 被编辑器驱动，支持流式答复、工具通知与审批、取消、会话恢复，以及可选的文件/终端转发。

GoAgent 核心继续提供 Agent、工具和状态执行能力；ACP 只负责协议适配。Harness 可提供工具集合，但不是 Factory 的必经入口。ACP 不等于沙盒，也不承诺命令运行位置或网络隔离。

不包含 ACP Client、编码工具、认证/登出、模式与配置设置、unstable 方法、图片/音频输入及推理/计划通知。文件适配要求客户端与 Agent 共享同一工作区磁盘视图；远程编辑器使用不同文件系统的场景本版不支持，不得以本地磁盘操作冒充远端操作。

## 位置与依赖

- 模块：`github.com/kart-io/goagent-contrib/acp`，Go 1.25.0。
- 固定：`github.com/kart-io/goagent v0.32.0`、`github.com/coder/acp-go-sdk v0.13.5`、`github.com/google/uuid v1.6.0`；测试使用 `github.com/stretchr/testify v1.11.1`。
- SDK 是社区实现，负责 JSON-RPC 编解码与方法分派。v0.13.5 的取消登记缺陷已由[独立探针](../../evaluation/2026-10-31-acp-contract-review/result.log)复现，本模块必须自己维护取消与关闭状态，不能只依赖 SDK 的 sessionCancels。
- 不修改 goagent 主模块，不复制或修改 SDK 私有字段。SDK 升级单独验证，不能让“升级后可能修复”替代本模块的契约测试。
- examples 当前已引用发布版 acp v0.1.1；本轮新增接口尚未发布，先通过本地工作区验证，发布新版本后再更新消费者依赖。

## 对外 API

```go
package acp

type Factory func(ctx context.Context, s *Session) (Runner, error)

type Runner interface {
    ExecuteStream(ctx context.Context, input any) (<-chan builder.AgentEvent, error)
}

type Session struct { /* 未导出字段 */ }
func (s *Session) ID() string
func (s *Session) Cwd() string
func (s *Session) MCPServers() []acpsdk.McpServer
func (s *Session) Pipeline() *tools.Pipeline
func (s *Session) FileSystem() env.FileSystem
func (s *Session) Executor() env.Executor
// Factory 获得资源后立即注册；回调按注册逆序最多执行一次。
func (s *Session) OnClose(cleanup func(context.Context) error) error

type Server struct { /* 未导出字段 */ }
func NewServer(f Factory, opts ...Option) *Server
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error
func WithAgentInfo(name, version string) Option
func WithSessionStore(store session.Store) Option
func WithToolKind(kind func(tool string) acpsdk.ToolKind) Option
// 使用客户端终端时必须显式配置，能力由宿主核对实际部署后声明。
func WithTerminalCapabilities(caps env.Capabilities) Option
```

`acpsdk` 为固定 SDK 别名。Factory 必须 WithSessionID(s.ID())，工具经 s.Pipeline() 所在 Dispatcher 注册；启用恢复时还须通过 WithSessionLog 使用相同 Store。MCPServers 返回副本；只传递声明，不自动连接。宿主连接 MCP、启动 plugin.Host 后立即用 OnClose 注册释放；注册失败须由宿主立即释放该资源。Factory 返回失败或 nil Runner 时也运行已注册清理。既有 Runner io.Closer 约定保留，在 OnClose 回调前调用；同一资源不得再重复注册 OnClose。关闭错误必须上报，不能忽略。

## 协议映射

| ACP 方法 | 行为 |
| --- | --- |
| initialize | 协议版本 1；agentInfo；prompt 仅声明 embeddedContext；close 能力开启；配置 Store 时声明 loadSession 与 list；记录客户端文件/终端能力 |
| session/new | UUID；构造 Session，调用 Factory，保存元数据，成功后登记；失败不登记并清理 |
| session/prompt | 校验内容、登记本轮执行、串行运行 Runner、转换事件与终态 |
| session/cancel | 主动取消该 Session 中运行及等待的轮次；SDK 的取消仅为附加机制 |
| session/close | 取消、等待在途执行与终端清理、逆序释放资源；完成后才移除会话并响应 |
| session/load | 同连接已有对象先安全关闭再重载；正在加载/清理时拒绝；读取最终日志，Factory 成功后按 Derive 回放并发布 |
| session/list | 读取持久元数据，严格按 cwd 筛选、稳定排序与分页；没有元数据的旧日志不以空 cwd 返回 |
| 其他 | method-not-found；authenticate 无认证方式时返回空响应 |

未配置 Store 时 load/list 返回 method-not-found。不存在或已关闭的 Session 返回 invalid-params；重复 close 返回 invalid-params。stdout 专用于协议，日志写 stderr。

## 执行、取消与会话关闭

每个 Session 有独立根 context、状态（open/closing/closed）、执行代次与轮次登记表。每轮在等待执行权之前登记唯一 token 和 cancel；新轮次取消此前运行/等待轮次，同 Session 最多一个 Runner 在执行。等待执行权时须 select context，不使用不可取消的裸 Mutex.Lock 等待。

本轮 context 从 Session 根 context 派生，同时传播 SDK 请求 context 的取消；session/cancel 直接调用适配层登记的 cancel。轮次返回时只删除自身 token，不能清除其他代次。SDK 旧请求返回删除其内部表项时，不影响适配层的独立登记。

关闭流程：先原子地进入 closing、拒绝新执行/资源登记，取消全部轮次，再等待 ExecuteStream 生产者结束及终端回收，然后执行 OnClose，最后进入 closed。每次释放资源时创建独立 5 秒 context，不继承 prompt 的取消；没有在关闭阶段开始前消耗期限。清理按逆序逐个尝试，错误 errors.Join；任何超时或清理错误都返回可见错误，不能当作释放成功。

关闭等待也以独立 5 秒期限为限；Runner 忽略取消而未停止时，不并发释放其仍可能使用的资源，保持 closing，返回错误并保留清理责任。生产者真正结束后继续释放；close 重试可等待同一个清理结果，不重复调用回调。Factory 和清理回调必须遵守 context，Go 无法强制结束不配合的 goroutine；这类对象保留未完成责任，不得被报告为已释放。同连接可直接 load 已打开 ID，适配层先关闭旧对象；回滚后清理尚未完成的同 ID 仍保留占用，不能提前发布新对象。

连接断开、Serve context 结束时，对所有 Session 运行相同关闭流程；完成关闭后才返回。连接正常结束且清理成功返回 nil；context 结束返回与 ctx.Err() 可匹配的错误；清理失败保留错误链。Serve 的 io.Reader 不必可关闭，调用方负责关闭阻塞的输入以结束 SDK 读循环；实际 stdio 进程由宿主管理。

Factory 本身也属于可取消的构造阶段：连接关闭不能漏掉“正在构造、尚未发布”的 Session。新建/加载的构造和同 ID 替换必须纳入连接生命周期登记，失败和取消都释放已获得资源。

## Prompt 与最终答案

输入支持 text、resource_link、嵌入文本 resource，按原顺序用双换行连接；资源链接写 URI，嵌入资源用 `<uri>` 加文本。其他块 invalid-params。

| GoAgent 事件 | 转换 |
| --- | --- |
| text.Delta | agent_message_chunk，记录本轮已发送文本 |
| step | 不重复发工具状态；由 Pipeline 发，事件本身仍可用于本地记录 |
| sources/usage | 本版不转发 |
| error | 优先检查取消；否则返回内部错误，保留原错误文本 |
| done | 优先检查取消，再按下面规则处理 done.Output，然后 end_turn |

最终 Result 为字符串时原样；nil 为空；其他可 JSON 编码对象编码为 JSON，编码失败返回内部错误。设已发送文本为 T、最终答案为 F：F 为空或 T 以 F 结尾时不再发送；T 是 F 的前缀时仅补发剩余后缀（T 为空即补全答案）；否则发 `\n\n最终答复：\n` 加 F，明确呈现与流式草稿不同的最终结果，不能直接把两个答案拼成一句。正常流不重复；只有 done.Output 的缓存/PreStep 答案仍能显示。

ctx 取消无论发生在同步 ExecuteStream、等待事件、发送通知或处理 done 阶段，都返回 cancelled。读取事件必须 select ctx，不能无限 range 自定义 Runner 的未关闭通道。返回前等待实际流结束以避免旧任务尚未释放执行权新任务就启动；遵守上述关闭期限和资源等待规则。

## 工具通知与审批

Pipeline 保留以下顺序：

1. Pre[0] 校验 SessionID；CallID 为空时生成会话内唯一 ID 并写回 Call；发 pending，带工具名、kind、参数。
2. Approver 只在 PreAsk 时请求允许一次、允许本会话（Grant 非空才提供）、拒绝；cancelled 映射 ApprovalCancelled。不追加 PreAsk 则直接执行。
3. Around[0] 发 in_progress，在 context 中关联 CallID；工具 error 或 ArgumentErrorResult 发 failed。
4. Post[0] 发 completed 或被拒的 failed，附终端引用、结果内容与 rawOutput。

宿主可追加 Guards/Pre/Around/Post，不替换适配器已有项。宿主后续 Post 改写的结果不进入先前通知，文档须明确。准入中止、审批失败、PreStop 与替代调用等没有终态的 pending，在本轮退出前补发 failed；通知用新建的独立 5 秒 context。每个 CallID 仅发送一次终态；同一调用允许多次参数/终端更新。SessionID 不匹配报错；工具绕过该 Dispatcher 时没有通知/审批，不能宣称自动覆盖。

## FileSystem 转发与协调范围

仅客户端同时声明 readTextFile 和 writeTextFile 时启用文本转发；否则全部使用 LocalFS，不能将一侧编辑器缓冲区与另一侧磁盘写入混用。

- 路径、符号链接限定沿用以 Cwd 为根的 LocalFS；向客户端发解析后的绝对路径。Stat/Walk/Remove 作用于共享磁盘；尚未落盘的新缓冲区不属于该工作区文件视图。
- ReadFile 先 Stat，目录报错，不存在保留 fs.ErrNotExist；随后经编辑器读文本，Version 为 SHA-256 十六进制。
- WriteFile 在副作用前校验：非 UTF-8 数据或 Perm 非零返回包装 errors.ErrUnsupported 的错误，不能静默丢弃二进制或权限要求。Perm=0 时按编辑器的默认权限语义工作；本版不承诺编辑器之外的权限变更。
- CreateIfAbsent 存在时 ErrExists；IfVersion 不存在或不匹配均 ErrStale；其他磁盘/传输错误原样包装，目录目标报错。组合意图按 LocalFS 的检查顺序处理。
- 写入前按需在共享磁盘创建父目录，不让测试预建目录替代实现；客户端 RPC 失败时文件可能已更新，调用方须重新读取确认。
- 同一 Server 实例内所有启用转发的 clientFS，通过 Server 级可取消的写协调器串行执行“检查版本→创建父目录→RPC 写入”，Remove 参与同一协调；等锁时支持 context 取消。重叠工作区也在此协调范围内。锁不覆盖编辑器手工改动、终端写入、其他 Server 进程或直接 LocalFS 调用，明确不承诺这些外部写者的 CAS。
- MCP 或其他代码绕过 clientFS 时不参与该协调。纯 LocalFS 路线仍遵循 LocalFS 自身的全进程协调范围，不与转发路线承诺跨实现原子性。

## Executor 与终端生命周期

客户端声明 terminal 且显式配置 WithTerminalCapabilities 后启用转发。客户端支持终端但宿主未声明能力时，会话创建失败，不猜测 Remote/Network/Container/ReadOnlyRoot；未声明 terminal 时使用本地 LocalExecutor，其能力按实际实现返回。

Run 验证非空 Argv，经 LocalFS 限定 cwd；terminal/create 分别传 command 与 args，不做 shell 拼接。等待退出后读取合并输出，非零退出仍 Result 与 nil；信号退出 ExitCode=-1；缺少退出码且没有 signal 是协议错误，不能默认为成功。保留客户端截断后的输出，文档说明无法恢复被截断部分。

终端在执行期间即附入对应工具调用。Create 成功后不启动清理计时；每个 terminal 在退出、取消或任何错误路径进入回收后，各回收阶段开始时才生成独立 5 秒 context。单次关闭等待仍有期限，但阶段分别计时可能使总回收时间超过 5 秒。取消或终态未知时 kill，随后 wait 确认退出、尝试读取已有输出、最后 release；正常结束只 release。kill/wait/output/release 的错误分别记录、合并且继续尝试后续步骤；保留 ctx.Err()，不能吞掉清理失败。取消的 Prompt 仍按协议返回 cancelled，清理错误通过工具失败通知与 session/close/Serve 的清理结果保留。

创建请求结果不明、没有 terminal ID 时不能假定未启动；返回可识别错误并登记未确认的远端状态。连接断开无法 RPC 清理时保留错误，不宣称资源已释放。

## 会话持久元数据与回放

使用同一个 session.Store，新增适配器私有事件类型 `acp/session_metadata`，Data 包含 `schema=1`、绝对且清理后的 cwd、UTC RFC3339Nano updatedAt。该类型不修改 core/session：Derive 忽略未知类型。每次新建成功、load 成功准备发布及 prompt 收尾记录元数据；首轮之前也能列出已成功创建的空会话。

写元数据用 Store.Lock(ctx, id, "acp-metadata") 协调适配层自身，再 Read 得到 LastSeq，Append 使用期望序号；与 Builder 并发追加遇 ErrConflict 最多尝试 3 次，其他错误直接返回。新会话 Read 的 ErrNotFound 视为期望序号 0；不能将任意读错误当空日志。失败/取消日志写入使用新建独立 5 秒 context，且不掩盖原执行结果。

列表从 Store.List/Read 还原最新有效元数据，不依赖连接 map；严格相等筛选已规范化 cwd。按 updatedAt 降序、SessionID 升序打破并列；每页 100 项。游标编码最后一条的 updatedAt/ID，按该键继续，不以不稳定数组下标分页；游标格式错误 invalid-params，并发更新不承诺快照分页。没有元数据的旧会话不出现在列表，显式 load 成功后补建元数据；损坏元数据报错，不能偷偷返回空 cwd。

load 的对话按 session.Derive 回放：user 文本、assistant 文本及 pending 工具调用、tool completed/failed。rewind 与 aborted turn 的撤销由 Derive 处理。Factory 失败不发回放；回放失败清理新 Session，不发布部分成功状态。回放结束后才响应，不重新执行历史工具。事件流中的更新不是会话资源快照，恢复不能保证外部沙盒或业务副作用自动恢复。

## 验收与失败条件

必须以 io.Pipe 假编辑器和 MockStreamClient 覆盖握手、文本、工具、审批、会话恢复与环境路径；整体 go test -race。必须额外覆盖：

- 旧 prompt 返回之后新 prompt 仍可取消；第三个替换、等待执行权时取消、多会话互不影响。
- 执行/审批中 close，重复 load，Factory 部分失败，连接在构造中断开；每个 OnClose 恰好一次，资源释放前没有在途使用。
- 命令运行超过 5 秒后取消及正常退出；kill/release 失败、断线、缺少退出码、输出截断均不被误报成功。
- 读写四种能力组合；版本冲突、缺失版本目标、目录、UTF-8/Perm 拒绝、副作用前校验、新建多层目录；同 Server 并发条件写一成一败。
- done-only、普通增量、前缀补齐、最终答案改写、取消与 done 竞争。
- 重新连接/新 Server 加载同一 Store 后的 cwd、严格过滤、旧日志迁移、分页及追加冲突。

真实编辑器联调在假编辑器测试后进行；stdio 配置示例放 goagent-examples/orchestration/acp。未通过本地全部契约测试，不进行合并发布。

## 真实验证后的约定修正

已通过 Neovim/CodeCompanion + Ollama 的实际进程链路及官方 TCK。TCK 证实同连接 session/load 不应要求客户端先 close，现已按上面的安全交接实现；不再保留旧限制。CodeCompanion v20.0.1 实测 read_text_file 只读磁盘，未声明 terminal；缓冲区可见性和终端转发必须以具体客户端能力及实现为准。完整证据见[实测报告](../../../../goagent-contrib/docs/evaluation/acp-live-2026-10-31/REPORT.md)。
