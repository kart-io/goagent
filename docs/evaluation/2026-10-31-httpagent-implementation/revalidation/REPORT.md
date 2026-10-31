# 用户要求的重新验证

2026-10-31 22:55–22:57（Asia/Singapore）。实际验证 contrib `d35f054`、examples `d21bf52`，均无未提交产品改动。本次重新执行，不复用原验收通过记录。

## 自动检查

Go 命令统一使用 `env -u GOROOT GOTOOLCHAIN=go1.25.0`；示例通过项目外 GOWORK 精确指向当前新模块。

| 检查 | 本轮结果 |
| --- | --- |
| contrib/httpagent `go test -race -count=3 -json ./...` | 37个顶层测试×3轮，111次通过，0失败事件，8.267秒；原始 [race.jsonl](race.jsonl) |
| contrib/httpagent `go vet ./...` | exit 0 |
| contrib/httpagent golangci-lint v2.6.2 | 0 issues，exit 0 |
| 示例 `go test -race -count=1 ./...` | PASS 4.912秒 |
| 示例 `go vet ./...`、构建新二进制 | exit 0 |
| `node --test web/*.test.mjs` | 6 passed，0 failed |

竞态测试覆盖五入口原生工具/持久预算/日志/Hook、重复接纳和冲突、通知隔离、构造与停服、慢消费者、容量上限、心跳及六个审阅回归。111为顶层测试执行次数，不包含子用例数，不是111个不同测试。

## 进程、网络与浏览器实测

使用本轮重新构建的二进制 `/tmp/goagent-httpagent-reverify`：8097浏览器离线模型示例；8098真实本地模型；8099独立HTTP回调探针。

- Chrome实际页面中文SSE执行工具并完成：[快照](reverify-sse.txt)。
- Chrome实际页面中文WS执行工具并完成：[快照](reverify-ws.txt)。
- WS慢工具cancel后收到已取消状态，发送按钮恢复：[快照](reverify-ws-cancel.txt)。
- SSE慢工具停止后页面如实显示未确认；服务端相同run_id `c30efa02-00cc-40a8-8fb1-89cfeb1d1707`记录run.cancelled：[快照](reverify-sse-cancel.txt)、[服务日志](browser-server.log)。
- 本次页面导航后的console error/warn为零。
- 独立Python探针真实运行JSON调用、inline入站Webhook和出站HTTP回调；逐个核对结果marker、run_id及run.completed，非法输入400、非法方法405、未知接收器404未产生通知；SIGTERM后进程exit 0。[探针源码](probe.py)、[结果](probe-result.json)、[服务日志](callback-server.log)。
- 实际Ollama llama3.1:8b工具流：run_id `e5caf98c-9fc6-4df0-93ab-0ae4759d63f2`，echo_input tool_start、15个事件、唯一done，结果含REVERIFY_LIVE_3103，2次模型请求、360tokens、1129ms。脚本断言已通过。[原始流](live-sse.txt)、[模型服务日志](live-server.log)。

自有8097、8098进程SIGTERM后均exit 0；8099探针进程也exit 0，共享Ollama未停止。

## 交付前提与结论

通过GOWORK=off并指定公共Go代理实际查询当前示例固定的新模块版本，返回404/unknown revision d35f054145fd。此提交仅在本地，未推送：直接独立下载示例依赖尚不可用。该限制与前次报告一致，发布后仍须重新验证无workspace的依赖获取。

本轮未发现新的功能问题，无产品代码修改。queued/worker持久契约经完整测试验证；生产队列/outbox后端、平台签名和共享会话锁仍需宿主集成。本次本地模型结果不推断多供应商或生产部署行为。
