# WebSocket 可替换接口验收

日期：2026-10-31。用户要求保留 Gorilla，同时定义接口以便宿主使用其他框架。已实现并本地提交，未推送或发布。

## 交付

| 仓库 | 提交 | 内容 |
| --- | --- | --- |
| goagent-contrib | cfb3e85df18a18307b3cb7e33a965d0b7d5f90a3 | WebSocketConn、WebSocketUpgrader、WebSocketUpgradeOptions、WebSocketUpgraderFunc、Config注入及Handler.ServeWebSocket |
| goagent-examples | 735916ed5842fb3d1ba44a9e8ebb669408790c1d | 宿主升级案例、真实工具/取消与握手策略测试，示例固定新扩展提交 |
| goagent | 本记录所属提交 | 更新设计及验证证据；主库生产代码和依赖不变 |

公开接口不使用 Gorilla 类型；协议运行文件 websocket.go 只操作 WebSocketConn。默认具体实现保留在 websocket_gorilla.go，Config.WebSocketUpgrader为空时继续使用它，因此原调用装配保持可用。没有删除主库由Gemini SDK引入的间接依赖。

两种接入方式：

1. `Config.WebSocketUpgrader`：由宿主提供标准HTTP升级适配器，Handler在升级前检查子协议、Origin与容量并登记连接，升级失败仍归该登记所有者回收。
2. `Handler.ServeWebSocket(ctx, conn)`：宿主框架先完成升级、认证、Origin和goagent.v1检查，再交付连接。Handler接管升级后执行、读写/心跳、取消、容量和停服；拒绝时也关闭交付连接。未交付的握手由宿主负责。

接口要求真实大小/期限控制、文本帧与控制帧处理、Pong通知和并发安全Close；其他框架需要相应包装，不能直接用空方法满足接口。例子用Gorilla作为宿主帧库，但只调用httpagent公开接口；不表示所有框架均已联调。

## 测试与静态检查

全部Go命令使用 `env -u GOROOT GOTOOLCHAIN=go1.25.0`。示例通过项目外GOWORK精确指向 `v0.0.0-20261031151618-cfb3e85df18a`，未在go.mod提交本机replace路径。

| 检查 | 结果与证据 |
| --- | --- |
| httpagent `go test -race -count=3 -json ./...` | 46个顶层测试×3轮，138次通过，0失败，8.591秒；[原始事件](race.jsonl) |
| httpagent vet / golangci-lint v2.6.2 | exit0 / 0 issues；[vet](vet.log)、[lint](lint.log) |
| 示例全模块race | 根包2.050秒，接收器1.748秒，全部通过；[记录](example-race.log) |
| 示例vet / lint /构建 | exit0 / 0 issues /成功；[vet](example-vet.log)、[lint](example-lint.log) |
| JS协议/解析回归 | 6/6通过；[记录](javascript.log) |

新增测试验证：不含Gorilla连接的自定义传输；实际HTTP连接的Upgrader注入；Origin与子协议检查不能被自定义升级器绕过；自定义升级未返回时Shutdown不能假成功；框架连接的context取消、全局关闭、容量拒绝及拒绝后关闭；原有默认Gorilla的真实心跳、慢消费者、取消、唯一终态等测试继续通过。

API先因本地接口尚不存在而编译失败：[api-red.log](api-red.log)。自定义传输漏做上限时，新增回归先因无法及时退出失败，再补共享运行层的消息长度检查：[limit-red.log](limit-red.log)。错误升级器返回nil连接且无error时，回归先得到200再修为500与诊断：[upgrade-red.log](upgrade-red.log)。示例的宿主适配包先缺失再实现：[example-api-red.log](example-api-red.log)。最终完整套件如上通过。

## 进程与浏览器实测

按示例指南开启 `GOAGENT_WS_HOST=1`，在19097运行Agent、19000运行出站回调接收器，使用重新构建的命令行客户端执行全部案例。WS由示例宿主自行握手、包装连接后交给ServeWebSocket；JSON/SSE/Webhook继续走默认HTTP路径。

- [客户端](client.jsonl)：4 completed + 1 cancelled。
- [回调](callbacks.jsonl)：每个CLI run_id匹配唯一终态回调，另有2条浏览器运行的终态回调。
- Chrome实际页面的宿主WS工具执行完成：[快照](adapter-ui-normal.txt)。
- 同页面慢任务显式取消，收到cancelled并恢复发送按钮：[快照](adapter-ui-cancel.txt)。
- 本次导航后console error/warn为零。[服务端日志](server.log)含对应工具前Hook和生命周期事件。

自建Agent与接收器进程均已停止；Agent SIGTERM exit0。默认和宿主模式均使用实际Builder/Plugin/Dispatcher与工具，模型为离线脚本；本次接口改造未扩大真实模型质量结论。

## 使用与边界

示例目录 `orchestration/httpagent`：

```sh
GOAGENT_WS_HOST=1 go run .
# 另一终端，同一workspace
go run ./cmd/client -case ws -input '宿主框架订单'
go run ./cmd/client -case ws-cancel
```

完整适配在 `internal/hostws/handler.go`，运行说明见SCENARIOS.md的“宿主框架自行升级WebSocket”。标准HTTP路径的默认Origin为无Origin允许、带Origin同源；配置CheckOrigin可覆盖。自行升级路径由宿主管理策略。已经升级后的容量拒绝不能再返回升级前HTTP状态，只返回错误并关闭连接。

新扩展尚未推送，发布前运行示例仍需本地workspace。接口化提供替换点，不删除Gorilla依赖，也不为尚未接入的框架承诺期限、关闭和握手正确性。
