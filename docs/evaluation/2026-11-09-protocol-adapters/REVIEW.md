---
phase: 01-protocol-adapters
reviewed: 2026-11-09T04:17:02Z
depth: deep
diff_base: 0f9f9698aeae84ec921dcdcbfddc08acd4fea4bb
files_reviewed: 8
files_reviewed_list:
  - management/protocol/service.go
  - management/protocol/integration_test.go
  - management/a2a/handler.go
  - management/mcpserver/server.go
  - a2a/remote/client.go
  - mcp/remote/client.go
  - mcp/remote/input.go
  - go.mod
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
resolved_findings:
  critical: 1
  warning: 8
  info: 0
  total: 9
verification: static_recheck_after_fixes
final_runtime_gates: pending_orchestrator
commit_docs: false
---

# 阶段 01：A2A/MCP 独立深度审查报告

**审查时间：** 2026-11-09T04:17:02Z
**深度：** deep
**审查文件：** 8
**状态：** clean（当前快照没有未关闭的审查发现）

## 结论

本次逐文件审查和跨模块追踪共发现 9 项问题，已即时通知主代理。主代理修改后，审查者逐项静态复核，确认相应触发路径已修正或明确拒绝不支持的操作。当前无未解决的 Critical/Warning/Info；历史发现保留在下文，不能因修复而省略审计记录。

此结论是**指定范围最新快照的静态复核通过**，不代表完成最终运行验收。独立局部反例在修复前执行；按照任务分工，未重复全量测试，修复后的永久回归、race 和最终门禁由主代理继续完成。下文明确区分实际执行证据、静态推理和上游报告。

## 范围、方法与依赖

- 使用显式 8 文件范围，基线提交已核实存在；包含尚未跟踪的新源码，没有使用任意 HEAD~N 推断范围。
- 首先读取 `CLAUDE.md`、`.claude/context-summary-a2a-mcp.md`，并读取 gsd-code-review 技能及其 workflow。未发现项目 `.claude/skills/`、`.agents/skills/`；不存在 `.claudeignore`，8 个目标均未被 `.gitignore` 排除。忽略目录中的上下文是用户明确要求读取的指导材料，没有作为源码审查。
- 逐文件阅读后追踪 `management/{service,auth,query,task,session,worker}.go`、`management/httpapi/handler.go`、管理测试夹具、`tools/tool.go`、统一 JSON codec。三类复用模式为管理服务授权/事务幂等、HTTP 公开字段投影、官方 SDK 客户端/传输。
- 核对本机 `github.com/a2aproject/a2a-go/v2@v2.7.0` 的 `a2a/core.go`、`a2aclient`、`a2asrv/jsonrpc.go`、Agent Card resolver；核对 `github.com/modelcontextprotocol/go-sdk@v1.8.0/mcp` 的 `client.go`、`server.go`、`shared.go`、`mrtr.go`、`protocol.go`、`streamable.go`。没有依据记忆猜测 SDK 行为。
- Read/Write、desktop-commander、context7、sequential-thinking 等专用工具在此会话不可用；采用可用的 shell/rg 读取、分项人工推理、官方规范与 SDK 源码核验，用 `apply_patch` 写报告。仓库内只写本报告，未改源码、回退、提交或推送；修复均由主代理完成。临时反例位于 `/tmp`，通过 Go overlay 加入编译，不占用源码路径。

调用关系：A2A/MCP 服务端 → `management/protocol` → 同一个 `management.Service` → Authorizer、事务 Store、持久命令与 Worker；既有 HTTP 入口直接使用同一管理服务。外部 A2A/MCP 工具 → 官方客户端 → 宿主提供的传输。未发现新建的平行任务状态库或循环依赖。

## 已关闭的严重问题

### CR-01：A2A 入站大整数舍入破坏业务输入与幂等比较

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/a2a/handler.go:79-103`、`:256-273`（原问题位于 `messageInput` 的 Data → Marshal 路径）。
**严重级别：** Critical，静默数据损坏风险。
**触发：** 同一个消息 ID 先发送 `{"id":9007199254740993}`，再发送 `{"id":9007199254740992}`。官方 SDK 的 `Part.UnmarshalJSON` 先将 Data.Value 解为 float64；管理层收到的首个输入已变成后者，第二次竟被当成成功的相同输入重试。

**实测：** `TestReviewA2ANumericCollision` 通过真实 A2A SDK/httptest 观察到持久输入 `{"id":9007199254740992}`，第二次调用 `error=<nil>` 且复用同一任务。

**修复建议及已落盘结果：** 在官方 SDK 解码前，以有界 HTTP 请求体保留原始 Data JSON；先通过 CanonicalJSON 拒绝歧义 JSON，再把原始 Data 用于管理输入和幂等比较。最新代码已保留 raw parts，并在 `messageInput` 中优先使用原文。**静态复核关闭**；此结论仅覆盖服务端入站，客户端结果精度见残余边界。

## 已关闭的警告

### WR-01：恢复任务等待整轮执行结束，阻塞流式首事件与立即返回

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/protocol/service.go:133-149`；调用方 `/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/a2a/handler.go:328`。
**严重级别：** Warning。
**触发：** 任务先进入 waiting_input；回复后执行器持续运行。原 WaitCommand 等到 applied 才返回，但 Worker 在运行结束时才设置 applied。

**实测：** `TestReviewA2AResumeFirstResponse` 的 immediate 和 streaming 两个子用例均在 350ms 后超时，持久状态已经 running，流式事件数为 0。

**修复：** 接受 applying 或 applied 为命令已开始应用的边界。已核对 Worker 在同一事务内发布 task.running 与 command.applying，避免先读到旧等待快照，也不再等整轮结束。**静态复核关闭**。

### WR-02：错误拒绝包含可支持类型的输出格式列表

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/a2a/handler.go:293-295`。
**严重级别：** Warning。
**触发：** `acceptedOutputModes=["text/plain","application/json"]` 原先因含 text/plain 被拒绝，尽管可返回 JSON。

**修复：** 非空列表按是否包含 application/json 判断交集；空列表采用默认输出。当前代码已实现。**静态复核关闭**。依据：[A2A SendMessageConfiguration](https://a2a-protocol.org/latest/specification/#322-sendmessageconfiguration)。

### WR-03：终态任务重新订阅返回成功，测试固化了错误行为

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/a2a/handler.go:389-409`；`/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/protocol/integration_test.go:153-161`。
**严重级别：** Warning。
**触发：** 对已完成任务 SubscribeToTask，原实现返回终态 Task 并正常结束；原集成测试还要求这种行为。

**修复：** 先授权查询任务，对订阅前已经终态的任务返回 UnsupportedOperation；最终结果改用 GetTask。实现和测试已同步修改。**静态复核关闭**。依据：[A2A Subscribe to Task](https://a2a-protocol.org/latest/specification/#316-subscribe-to-task)。

### WR-04：MCP 人工输入缺少能力协商，并曾错误拒绝空 elicitation 声明

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/mcp/remote/client.go:21-40`；`/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/mcpserver/server.go:229-235`、`:292-294`、`:322-329`。
**严重级别：** Warning。
**触发：** 原 Connect 没有 ElicitationHandler 或显式 capability，SDK 不宣告 elicitation；服务端仍返回 form InputRequests。第一次补丁又把新协议 `elicitation:{}` 当作不支持。

**修复：** ConnectWithOptions 默认声明 form，并允许宿主明确配置；服务端只在需要收发人工输入时检查每请求能力。缺少 elicitation 或仅支持 URL 时拒绝，空 elicitation 对象按 form 支持处理；保留缺能力的 JSON-RPC 错误。已核对 SDK 的 ClientCapabilities、Elicit 和 MRTR 处理，最新实现符合这条边界。**静态复核关闭**。依据：[MCP 2026-07-28 Elicitation 能力规则](https://modelcontextprotocol.io/specification/2026-07-28/client/elicitation#capabilities)。

### WR-05：忽略 Part 显式声明的不支持媒体类型

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/a2a/handler.go:256-275`。
**严重级别：** Warning。
**触发与实测：** `TestReviewA2AMediaTypeIgnored` 发送 Data part 且 `mediaType=image/png`，旧实现返回成功，尽管 Agent Card 只支持 JSON/text。

**修复：** Text 仅接受省略媒体类型或 text/plain；Data 仅接受省略媒体类型或 application/json；其余返回 UnsupportedContentType。最新代码已实现。**静态复核关闭**。

### WR-06：内部分页固定 200，破坏合法的管理服务限制配置

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/protocol/service.go:238-254`；`/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/a2a/handler.go:413-415`（原 ListTasks 总量扫描固定 Limit=200）。
**严重级别：** Warning。
**触发与实测：** 配置 `PageSize=20, MaxPageSize=50` 可正常创建 management.Service，但 `ListTasks(PageSize=1)` 和进入 waiting_approval 的 SendMessage 均返回 invalid params。`TestReviewConfiguredPageLimit` 实际复现；MCP 审批投影也调用同一 Describe。

**修复：** Describe 内部查询 Limit=0，采用管理服务默认并沿返回游标分页；A2A 列表明确拒绝，移除不兼容的总量扫描。最新代码已实现。**静态复核关闭**。

### WR-07：ListTasks 成功结果不满足排序及分页范围契约

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/a2a/handler.go:413-415`。
**严重级别：** Warning。
**触发：** 原实现直接复用按 CreatedAt 升序分页的管理列表。两个先后创建/更新的任务即可能按旧任务在前返回，而协议要求按状态更新时间降序；原参数校验还接受 101–200 的 pageSize。

**修复：** 管理层尚无相应持久更新时间排序契约，不能仅在单页内排序来伪装符合协议。已改为返回 UnsupportedOperation，并修正集成测试。任务列表不是本次核心功能的必需项，因此没有要求为此扩大持久模型改动。**静态复核关闭**。依据：[A2A List Tasks](https://a2a-protocol.org/latest/specification/#314-list-tasks)。

### WR-08：不同轮次人工提示复用同一个消息身份

**文件：** `/home/hellotalk/code/go/src/github.com/kart-io/goagent/management/a2a/handler.go:176-178`。
**严重级别：** Warning。
**触发：** 同一任务连续两次等待不同输入，原 Status.Message.ID 均为 `taskID/wait`；按消息 ID 去重的客户端可能忽略第二轮提示。

**修复：** 使用 `taskID/wait/revision`，同一状态快照查询身份稳定，后续状态修订得到新的消息身份。最新代码已实现。**静态复核关闭**。

## 重点风险检查结果

| 检查点 | 追踪结果 |
| --- | --- |
| 身份与租户 | A2A 由宿主 ActorFromRequest 注入私有 context；Tenant 参数只校验不覆盖宿主身份。MCP 每次工具请求重新取 Actor。management.Get/Mutate 都按 Actor.TenantID 访问。 |
| Agent 隔离 | protocol.Start 检查会话 AgentID；Task 检查任务 AgentID；MCP get 检查配置允许的 Agent 集合。 |
| 授权传播 | GetTask、GetSession、GetApproval、DecideApproval、ResumeTask、CancelTask 进入既有 Authorizer；Worker 应用输入/审批时再次核验权限。未发现适配层旁路。 |
| RequestState 信任 | 严格解码、绑定当前 task_id、限定一个响应；审批仍按真实 Approval.TaskID 和修订判断。状态可被客户端修改，但不是授权凭据；伪造普通输入不能代替审批。 |
| 幂等回复 | Key 纳入 Tenant/Subject/Agent；管理服务按操作、请求内容和任务/审批身份保存幂等响应。先验证授权再复用结果；已决定回复的重试不会重新执行决定。 |
| 取消与连接断开 | 协议观测使用调用方 ctx；后台任务由 Worker 独立执行。A2A cancel 等持久 canceled，其他终态返回不可取消；MCP 如实返回 canceling。elicitation 的 cancel 仅取消本次交互，不等同取消任务。 |
| 外部调用与续接 | A2A Tool 的 send/get/cancel 传 caller ctx；durable policy 最大尝试 1。MCP 无输入处理器或达到轮数上限时返回可保存 InputRequiredError；Continue 校验响应 ID/类型并携带原 Arguments/RequestState，不无状态重发初始调用。 |
| 公开投影 | TaskView 保留业务 input/result，未直接暴露执行键、会话日志或中断键；审批仅投影 ID、WaitID、Revision。 |
| 能力边界 | Agent Card 不宣告推送通知/文件传输/扩展卡；原生 MCP tasks 扩展不属于本次实现，长任务使用明确 tools 契约。HTTP 无状态 transport 支持新协议 MRTR；ListTasks、history 和不支持的内容有明确拒绝路径。 |

授权结论依赖宿主正确实现 ActorFromRequest/Authorizer。现有 TenantAuthorizer 本来就只适用于宿主已完成权限校验的接入，不应被误认为逐用户所有权策略。本次没有据此误报同租户必然泄漏。

## 实际验证记录

独立执行以下局部命令，未运行 `go test ./...` 或重复全量 race：

```sh
go test -overlay=/tmp/goagent-review-overlay.json ./management/protocol -run '^TestReviewA2A' -count=1 -v
go test -overlay=/tmp/goagent-review-overlay.json ./management/protocol -run '^TestReviewConfiguredPageLimit$' -count=1 -v
git diff --check
```

首条在修复前执行，实际用例为大整数/幂等碰撞、恢复 immediate、恢复 streaming、未知 MediaType，共 4 个场景；第二条验证自定义分页限制下的列表和审批投影失败。两条测试返回 PASS 的含义是**成功断言并复现原缺陷**，不能当作修复后的通过证据。临时文件为 `/tmp/goagent-review-probe_test.go`、`/tmp/goagent-review-overlay.json`；修复后不应继续使用“断言缺陷存在”的测试作为门禁。

最终静态复核读取了全部修复位置及修正的集成测试断言；`git diff --check` 未报告问题。主代理报告既有跨 HTTP/A2A/MCP SDK 回归 race 通过，此为上游提供的信息，本审查者没有重复执行，也不将其表述为本次独立运行结果。

主代理继续补充的永久负面、数值、异步恢复、无能力声明、remote 检查点回归和文档不属于此快照已完成的运行验证；最终门禁仍需覆盖这些新增代码。

## 残余边界

1. **SDK 客户端结果中的大数精度。** 本次修复保护 A2A 服务端入站原始 Data，但官方 v2.7.0 客户端仍将 Task.Metadata、Data.Value 等解为 any/float64。`a2a/remote/client.go:73-98` 返回 SDK 结果，外部 Agent 的大整数结果仍可能在该解码边界舍入；不能宣称端到端任意 JSON 数值完全保真。本轮按明确分工记录为 SDK 边界。精确业务 ID 应采用双方约定的字符串编码；若后续要求任意数值保真，应另行增加原文保留或明确拒绝策略及外部 SDK 回归。
2. **持久性和并发证据范围。** 夹具 Store 使用串行内存事务，局部 httptest 证明真实协议与服务映射，不证明生产数据库隔离、故障恢复、跨进程 Worker 竞争；本次未重复重启 Worker 等既有全量场景。
3. **流式粒度。** Watch 读取持久任务修订，可能合并快于轮询的中间状态；这是状态/产物快照流，不是 token 级流或完整事件回放。断线后的新订阅读取当前快照；已终态用 GetTask。
4. **远端续接契约。** 检查点不包含新的授权凭据；重新连接仍需宿主提供正确的认证和原远端。跨 Session 是否有效取决于远端契约。旧协议的宿主人工输入处理器需通过 ConnectWithOptions 配置；不能把任意旧服务器行为当作新 MRTR 保证。
5. **明确未实现项。** 不把推送通知、文件传输、原生 MCP tasks 扩展、A2A 列表排序或人工核对 reconciliation 扩展误报为本次必需能力；只审查已有成功路径的正确性和拒绝行为。

## 项目要求的评分与交付映射

| 维度 | 分数 | 依据 |
| --- | ---: | --- |
| 代码质量 | 92 | 共用服务和官方 SDK；9 项发现已在快照中关闭。 |
| 测试覆盖 | 86 | 已有跨入口场景，独立反例明确；修复后永久负面及最终门禁仍由主代理完成。 |
| 规范遵循 | 92 | 已修正模式协商、终态订阅、输入能力、多轮身份；不支持的列表如实拒绝。 |
| 需求匹配 | 94 | 发现、消息/任务映射、状态流、取消、人工输入、恢复和外部调用均有实现路径。 |
| 架构一致 | 96 | 共享 management.Service，不另存任务；认证与部署交由宿主。 |
| 风险控制 | 90 | 已复核身份、审批、幂等与取消；保留 SDK 精度和持久后端边界。 |

**综合：92/100；建议：通过本轮独立静态审查。** 最终交付以主代理完成永久测试、文档和运行门禁为准。源码映射为 frontmatter 的 8 文件；独立交付物仅此报告，未另写 `.claude/verification-report.md`，遵守用户指定的唯一仓库写入范围。

## 审查快照 SHA-256

```text
879965cc94626bfaaa74e486028e91d2509dae5a2ec26624625d6ba6da406015  management/protocol/service.go
c7e0eacd2b713898b302b45e86f9890478de4dc295dcfc2a49413f8b64dd2ec0  management/protocol/integration_test.go
0f214b537d4714b76f24498802d3219328d4744fff7db132a03fdabeec0bdbcf  management/a2a/handler.go
16cb1fb57bfab47801b061a3efab5abc8d4413b0cbe58129ea9f0341280ba082  management/mcpserver/server.go
97198ab2b187dff94cf22bde80c30ff6195eae1be86719840f9a16445d47e395  a2a/remote/client.go
8845024fc3dc6fbc2dc141e96bf4bfe3b7fb339e3b6aa61dc07f7fc8a25a0819  mcp/remote/client.go
a11af19a1fd83e90e080d2a39a42d00dac7a16b6145b6f4583a26339e98dcd25  mcp/remote/input.go
a47d47f13fd995e84ad5944d2d32a66692f8c8e7c17e777002137ac86f1a42ed  go.mod
```

---

_审查者：Codex（gsd-code-reviewer）；深度：deep；未提交或推送。_
