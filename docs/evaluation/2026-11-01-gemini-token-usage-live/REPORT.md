# Gemini 真实 Token 统计验证：被账号额度阻塞

日期：2026-11-01。模型：`gemini-2.5-flash`，项目原生 Gemini Provider，使用环境变量 `GEMINI_API_KEY`，默认官方端点。

## 实际执行

```sh
go run ./examples/agent-usage/live -provider gemini -model gemini-2.5-flash -out /tmp/goagent-gemini-token-live-20261101-run1
```

该命令退出码为 1。先执行 single，失败后停止，没有进入 child 和 task。单 Agent 已预留一次模型请求；没有模型 usage，Root 已知 Token 为 0、unknown=1。这个零值不能理解为无消费，也不能作为统计成功的证据。

为定位首次错误，额外执行了一次相同 Provider/模型的独立最小诊断请求，生成上限 256、30 秒时限；这次诊断不属于首轮账本。返回：

```text
HTTP 402
Status: RESOURCE_EXHAUSTED
Your prepayment credits are depleted.
```

两次执行均未收到 usage；没有自动重试，没有尝试充值或切换账号。诊断日志保留错误状态、原因与官方账务页面，不包含凭据。归档前按当前环境中的密钥值检查证据文件。

## 结论与后续

真实端点调用已执行，但未验证实际 Token 数值、父子归属或普通工具路径。需要为现有 Gemini 项目补充额度，或提供另一个有可用额度的 Gemini 配置，再以新输出目录重跑三种场景。重跑需保留本次失败证据。

[首轮账本和用量证据](results.json)、[执行日志](execution.log)、[诊断日志](diagnostic.log)。

真实入口的离线测试覆盖三种场景校验，以及 Gemini/DeepSeek/MiniMax 缺少凭据时拒绝回退；race 测试结果见 [tests.log](tests.log)。离线测试不能替代真实 usage 验证。

[运行说明](https://github.com/kart-io/goagent-examples/blob/main/agent-usage/live/README.md)。
