# HTTP、SSE、WebSocket、Webhook 与 Hooks 接入

GoAgent的网络入口由独立扩展 `github.com/kart-io/goagent-contrib/httpagent` 提供。主库继续负责Agent/工具/图/持久执行；网络适配器负责入口、协议与运行资源责任，Web页面在goagent-examples。

## 文档与版本

本文最初按 contrib 提交 `cfb3e85`、GoAgent v0.32.2 和 Go 1.25 编写。接入当前 GoAgent 时要求 Go 1.27+；扩展与示例的具体依赖版本以各自 `go.mod` 为准，接口以对应版本的下列文档为准。

| 需要了解 | 文档 |
| --- | --- |
| 从零运行、复制完整宿主程序 | [使用指南](https://github.com/kart-io/goagent-contrib/blob/master/httpagent/docs/USER_GUIDE.md) |
| 所有者、终态、取消、接纳与通知的实现取舍 | [扩展设计](https://github.com/kart-io/goagent-contrib/blob/master/httpagent/docs/DESIGN.md) |
| Config全部字段、默认值、公开接口、协议与错误 | [API参考](https://github.com/kart-io/goagent-contrib/blob/master/httpagent/docs/API_REFERENCE.md) |
| GoAgent内核与扩展如何分工 | [本仓设计入口](../design/HTTP_AGENT_DESIGN.md) |
| Web页面、CLI、双向回调与宿主WS案例 | [案例指南](https://github.com/kart-io/goagent-examples/blob/main/orchestration/httpagent/SCENARIOS.md) |

## 运行现成案例

```sh
git clone -b main https://github.com/kart-io/goagent-examples.git
cd goagent-examples/orchestration/httpagent
GOWORK=off go run .
```

打开 http://127.0.0.1:8097 。另一个终端在同目录执行：

```sh
GOWORK=off go run ./cmd/client -case all -input '查询订单 E100'
```

预期4条completed和1条cancelled；默认使用离线脚本模型，实际执行Plugin工具和网络传输。无需克隆本地contrib工作树，固定提交已可远端下载。

需要出站Webhook时，先在同目录的另一个终端启动接收器，再带WEBHOOK_URL启动案例服务：

```sh
GOWORK=off go run ./cmd/webhook-receiver -listen 127.0.0.1:9000
# 另一个终端；替换上面的普通服务启动命令
GOWORK=off WEBHOOK_URL=http://127.0.0.1:9000/events go run .
```

## 在业务服务中安装

```sh
go get github.com/kart-io/goagent-contrib/httpagent@v0.0.0-20261031151618-cfb3e85df18a
```

实现Factory，在请求context下装配Builder、Plugin和Dispatcher并返回Native(agent.ExecuteStream)。按真实模型能力检查llm.HasToolStreaming，不能凭ExecuteStream方法存在性判断。Factory每次新建请求级Agent，Plugin Host等共享资源由服务生命周期管理。完整可编译程序在扩展使用指南。

| 功能 | 入口/配置 | 主要边界 |
| --- | --- | --- |
| 同步结果、流式工具事件 | POST /invoke、POST /stream | 两者保留相同原生执行；Answer显式不支持SSE |
| WebSocket | GET /ws，goagent.v1 | 一连接一个活动运行，终态后等ready再重用 |
| 自有同步事件 | Receiver.Mode=inline | 等业务完成再应答 |
| 持久接纳后worker | Receiver.Mode=queued、Acceptor、Handler.Run | 持久队列、去重和worker由宿主提供 |
| 出站通知 | Delivery、NewSender | 默认有界单次尝试；可靠重投需宿主outbox |
| 原生Hook与运行观察 | Plugin Hooks、Config.Observer | Hook参与执行循环，Observer观察生命周期 |
| 其他框架WS | WebSocketUpgrader或ServeWebSocket | 接口不含Gorilla类型，默认实现仍保留Gorilla |

Handler路由没有/api前缀，案例宿主通过StripPrefix挂载。完整HTTP/WS报文以扩展API参考为准。

## 生命周期与持久执行

run_id为当前运行身份，不等于业务会话锁、事件去重键或已完成结果缓存。共享SessionID需要宿主协调；需要预算/日志/持久恢复时，Factory应组合 [宿主标准接入](HOST_INTEGRATION.md) 与 [执行治理](EXECUTION_GOVERNANCE.md)。

网络断开或退出未确认不能证明工具副作用没发生。客户端没有收到业务终态时，应按run_id诊断，不能盲目换ID重试。停服先BeginShutdown取消，再等HTTP Shutdown与Handler.Shutdown，成功后关闭共享Host。具体超时与资源保留设计见扩展文档。

文档代码块及源码对照范围见 [扩展文档验证](https://github.com/kart-io/goagent-contrib/blob/master/httpagent/docs/VERIFICATION.md)。实测证据见 [完整实现验收](../evaluation/2026-10-31-httpagent-implementation/REPORT.md) 与 [可替换WS接口验收](../evaluation/2026-10-31-websocket-interfaces/REPORT.md)。

合并到主分支后的检查见 [合并验收](../evaluation/2026-10-31-httpagent-merge/REPORT.md)。
