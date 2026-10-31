# HTTP Agent 实施验收记录

日期：2026-10-31。根据已审核设计与“开始执行”指令完成六项实施任务，仅本地提交，未推送、合并或发布。GoAgent 主库生产代码未改变；原有沙盒研究与 examples 主工作区修改保留。

## 交付与源码

三个仓库均使用 `feat/httpagent-webhooks-20261031` 分支。

| 仓库 | 交付 | 最终代码提交 |
| --- | --- | --- |
| goagent-contrib | 独立 httpagent 模块，JSON/SSE/WS、双向 Webhook、生命周期 Observer、取消/停服与资源跟踪 | d35f054145fd39145cbadbd2eafa60d6ea1e3cee |
| goagent-examples | orchestration/httpagent 独立模块、嵌入 Web 页面、真实 Plugin 工具、SSE/WS 切换及停止 | d21bf5285f7fe0e3f7c6e5a0fcb95ba015afcc15 |
| goagent | 设计、计划和本验收记录 | 见本文件所属提交 |

本地工作树分别为 `~/.codex/worktrees/goagent-contrib-httpagent` 和 `~/.codex/worktrees/goagent-examples-acp-contracts`。核心依赖为已发布 v0.32.2；新模块精确固定 `v0.0.0-20261031145228-d35f054145fd`，尚未推送，不能宣称外部用户已可直接下载。开发验证使用项目外 `/tmp/goagent-httpagent-implementation.work` 精确版本 replace；仓库内没有机器路径 replace。发布后需使用 GOWORK=off 再验证依赖获取。

接口：`POST /invoke`、`POST /stream`、`GET /ws`（goagent.v1）、`POST /webhooks/{name}`；宿主 worker 使用同一 `Handler.Run`。示例挂载 `/api/`，同时提供静态页面。Native 使用原有 ExecuteStream 工具循环；Answer 显式不承诺流式工具能力。Factory 在登记后构建，最终结果、通知和资源释放归同一所有者管理。

## 验证结果

所有 Go 命令通过 `env -u GOROOT GOTOOLCHAIN=go1.25.0` 执行。lint 使用 golangci-lint v2.6.2。

| 验证 | 结果与证据 |
| --- | --- |
| httpagent 全模块竞态测试，37 个顶层 Test（含子用例） | PASS 3.592s，[race.log](race.log) |
| go vet / golangci-lint | 成功 / 0 issues，[vet.log](vet.log)、[lint.log](lint.log) |
| 示例全模块竞态测试、vet、二进制构建 | 成功；race 1.963s，[example-race.log](example-race.log) |
| JS SSE parser 与 WSProtocol | 6/6 PASS，[javascript.log](javascript.log) |
| contrib 11 模块基线 race/vet | 全通过；其中 httpagent 随最终修复已单独重跑，[contrib-baseline.log](contrib-baseline.log) |
| 主库导入边界 | 0 findings，[imports.log](imports.log) |

五入口集成测试使用实际 Builder、Plugin、Dispatcher、脚本 HTTP 模型服务、durable budget FileStore 和 session FileStore。JSON、SSE、WS、inline Webhook、worker 各执行一次工具并保留两个模型请求预算、工具预算、PreStep Hook；重新打开持久文件核对预算和日志。模型脚本用于确定性验证执行契约，不代表线上模型质量。

入站接纳测试使用测试专用磁盘 Acceptor，原子写入事件、原始 payload hash 和稳定 RunID；16 个并发重试仅接纳一条，相同事件复用 ID，payload 冲突返回409，租户/接收器隔离，HTTP 请求 ctx 退出后独立 worker 仍执行。它不是交付的生产队列后端。

其他回归包括：构造中停服、迟到 Factory/cleanup、生产者退出未确认、唯一终态、编码/容量错误、WS cleanup→ready 窗口、真实 TCP 慢消费者写期限、首事件前 SSE 心跳、通知多目标同一事件 ID、Observer 修改副本不污染投递，以及不支持写期限的中间件在执行/接纳前501。

## 实际浏览器与模型

使用 Chrome DevTools 连接器操作真实页面；原生 CUA 无可用 surface，改用可工作的浏览器连接器。最终二进制由上列代码提交构建，端口8097，无浏览器 console error/warn。

| 操作 | 结果 |
| --- | --- |
| SSE 中文任务 | echo_input tool_start/tool_end、已完成，[final-sse.txt](final-sse.txt) |
| WebSocket 中文任务 | 工具执行、已完成且可再次发送，[final-ws.txt](final-ws.txt) |
| WS 慢工具显式 cancel | cancelled 终态、ready 后按钮可用，[final-ws-cancel.txt](final-ws-cancel.txt) |
| SSE 慢工具中断连接 | 页面明确显示“服务端实际退出状态未确认”，服务端同一 run_id 记录 run.cancelled，[final-sse-cancel.txt](final-sse-cancel.txt)、[browser-server.log](browser-server.log) |

另在8098使用已安装并运行的 Ollama llama3.1:8b，经实际 OpenAI 兼容 HTTP API 执行 SSE 请求。run_id `f8097a7b-168c-4fa9-8838-f09fa74bc7dd`，出现 echo_input tool_start、两个模型请求、唯一 done，结果含 `LIVE_SSE_73126`，耗时5011ms、总374tokens。[原始流](live-model-sse.txt)、[日志](live-model-server.log)。未下载模型；不据此推断其他模型、平台或生产代理行为。两个自有示例进程在验收后停止，共享 Ollama 保留。

## 一次独立审阅与修复

最终独立审阅针对 contrib b9d39c7..9040c72 与 examples 93ae6cb..3404282，初始结论需修复，报告4 Important + 2 Minor。两个 Minor 分别会造成代理首包前断连及无效终态，按实际功能影响重新定为 Important。六项均先复现再修复；未委派第二次复审，最终以完整竞态测试验证。

| 发现 | RED→GREEN 对应回归 |
| --- | --- |
| WS 包装后超限，丢终态仍 ready | TestReviewWSFrameBoundary；冻结前验证完整帧，传输失败关闭连接 |
| worker 返回 Output 可改写冻结通知 | TestReviewWorkerOutputCannotMutateFrozenNotice；终态先快照，返回值深拷贝 |
| cleanup 阻塞使通知无限等待 | TestReviewNotificationBeforeCleanupExit；通知与 cleanup 独立受管推进，JSON 等待有界投递 |
| 超时 Report 回调活动时 Shutdown 错误成功 | TestReviewShutdownTracksTimeoutReporter；回调完成纳入所有者退出 |
| 首事件前无 SSE 心跳 | TestReviewSSEHeartbeatBeforeFirstEvent；能力检查后允许注释心跳 |
| 极小事件上限使错误终态也无法编码 | TestReviewSmallLimitProducesTerminal；构造校验至少512字节，不发送空 fallback |

[六项 RED 记录](review-red.log)、[修复后完整测试](review-green.log)。额外页面协议回归拒绝无 done/error 的 ready，[web-protocol-red.log](web-protocol-red.log)；不支持写控制时拒绝 Webhook 接纳，[webhook-writer-red.log](webhook-writer-red.log)。最终完整测试见前表，无延期 Minor。

## 执行决策与保证边界

以下保留执行 ledger 中所有 Ruling，含其代价；完整过程见 [EXECUTION_LEDGER.md](EXECUTION_LEDGER.md)。

1. 用户已明确批准执行，按计划直接实施，不再增加设计确认；代价是依据当前已审核范围推进。
2. HTTP 示例采用独立嵌套模块，避免未发布扩展影响其他案例；发布前需要同时检出 contrib。
3. 原计划固定核心 f364a20 以保留零温修复，go get 确认该提交已发布为 v0.32.2，最终使用此正式版本；无核心伪版本负担。
4. 新 httpagent 模块暂以外部 workspace 验证，保留未发布约束；外部下载须等推送/发布，不能作为本轮已通过项。
5. 队列/outbox 后端按批准范围由宿主注入；部署者仍需选择和验收持久后端及平台签名/challenge。
6. 任意不配合取消的 Go 函数无法强停；保留所有者及容量、报告未确认，代价是停服可能超时。
7. WS writer 在 ready 写入前清除 active；单 writer 保序且必要 cleanup 已完成，保持单活动运行语义，已有窗口回归。
8. 自定义 Binding 的能力声明、跨实例共享 SessionID 的锁/租约由宿主负责；五入口测试证明默认隔离组合，未验证外部宿主锁实现。
9. 模型、平台和部署结论限于现有证据；已做本地真实模型调用，未声称多供应商、生产代理或平台完整适配验收。

初始示例 Go RED 是未发布依赖解析失败，明确不作为功能 TDD 证据；JS parser/protocol 和六个审阅问题有实际行为 RED。第一次早期浏览器 SSE 取消错过窗口，未计作成功，最终版本已重新验证。

本轮按计划保留本地分支和工作树。未推送/发布意味着示例直接独立下载尚有交付前提；可靠平台后台服务、自动重试、生产队列、跨实例会话锁和断线恢复均需宿主后续集成。
