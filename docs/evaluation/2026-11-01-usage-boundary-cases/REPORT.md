# Token 边界案例补充与真实验证

日期：2026-11-01。新增 [cases 案例](https://github.com/kart-io/goagent-examples/blob/main/agent-usage/cases/README.md)，覆盖跨平台同任务、持久回放、工具错误和取消。仅补充案例与共享示例客户端工厂，未修改核心计量实现。

## 真实执行结果

加载 `source ~/.zshrc` 后执行三个独立命令，全部退出码为 0；没有自动重试，没有模拟回退。

| 验证场景 | 已知总 Token | 客户端调用 | 普通工具执行 | 结果 |
| --- | --- | --- | --- | --- |
| 三平台混合任务 + 持久回放 | 1826 | 首轮 6，回放后仍 6 | 首轮 1，回放后仍 1 | 通过；回放通知 3 次 |
| 工具主动报错 | 173 | 1 | 1 | 通过；业务按预期失败 |
| 执行中取消 | 189 | 1 | 1 | 通过；业务按预期取消 |

合计 8 次真实模型客户端调用、2188 已知 TotalTokens；回放没有新增模型调用或普通工具副作用。调用次数是案例对结构化客户端入口的计数，所用 Provider 没有隐式重试，不是对供应商后台账单条目的承诺。

mixed-replay 中 Gemini 父 Agent 调用 DeepSeek researcher 与 MiniMax reviewer；researcher 再调用 lookup_order。Gemini 本轮选择分两轮委派，因此父 Agent 3 次请求、DeepSeek 2 次、MiniMax 1 次，共 6 次。不是额外失败重试。

| 平台 / Agent | 请求 | Prompt | Completion | Total | 已知 Reasoning |
| --- | --- | --- | --- | --- | --- |
| Gemini / coordinator | 3 | 616 | 44 | 755 | 95 |
| DeepSeek / researcher | 2 | 763 | 77 | 840 | 33 |
| MiniMax / reviewer | 1 | 212 | 19 | 231 | 15 |
| 任务 | 6 | 1591 | 140 | 1826 | 143 |

同一任务根账本按字段准确汇总，父 Inclusive 包含全部 1826，父 Self 只含 Gemini 的 755。缓存已知累计 256。可选细项只统计已知值，不宣称完整明细；平台供应商的 TotalTokens 保持原值，推理子项不能再次加入总量。

平台归属使用宿主的 Agent→平台路由表，避免把 MiniMax 的兼容 Provider 标识 openai 误当作实际厂商。结构化证据保留 requested/response model、显式推理设置、字段位图、前后账本与逐次通知。

## 校验内容

- 首轮实际使用三个平台、两个子 Agent 和普通工具；模型输出文字但没调用工具不会通过。
- 已知用量六字段的逐尝试之和 = 平台之和 = Agent Self 之和 = Root；请求预留数与客户端进入次数一致。
- 使用同一输入、作用域和持久日志再次 Invoke；要求执行成功、完整归属报告前后相等、模型调用数不增加、工具只执行一次且收到回放通知。
- 回放通知单独标记，在平台汇总前跳过，不再增加消费。
- tool-error 要求 errors.Is 命中受控哨兵错误；cancel 要求命中 context.Canceled。两者均要求工具确实执行一次、之前已知用量留在账本。其它原因导致的失败不会被当作预期错误通过。
- 取消后的查询使用脱离取消信号的有界 context，不把已消费 Token 丢成零。

Passed 表示验证预期成立。工具错误和取消案例的业务结果仍失败；报告保留 ExpectedErrorObserved，不能解读为业务成功。

## 复现

```sh
source ~/.zshrc
MINIMAX_BASE_URL=https://api.minimax.cn/v1 go run ./examples/agent-usage/cases -case mixed-replay -out /tmp/goagent-usage-cases-20261101-mixed-r1
go run ./examples/agent-usage/cases -case tool-error -out /tmp/goagent-usage-cases-20261101-tool-error-r1
go run ./examples/agent-usage/cases -case cancel -out /tmp/goagent-usage-cases-20261101-cancel-r1
```

原目录已经存在，复现需改成新目录。模型由对应 MODEL 环境变量选择，未配置时使用 gemini-2.5-flash、deepseek-chat、MiniMax-M3.1-Flash。Gemini 每请求思考预算 128，其余使用 effort=low；不承诺不同平台的推理策略相同。

每场景最多 10 次模型请求、8 次工具调用、单次输出上限 2048、180 秒运行时限。临时持久目录在结束后清理，报告不包含对话或推理正文。

## 测试与证据

`go test -race ./examples/agent-usage/...` 通过，包括并行子 Agent、重复调用、错误、取消、细项聚合和既有案例，见 [tests.log](tests.log)。常规测试注入离线响应，不冒充真实模型结果；初始 API 未实现的 RED 记录见 [red.log](red.log)。

独立只读审核未发现阻断问题，复核了失败是否按预期识别、回放是否重复计数、可信平台归属和客户端工厂提取；审核未运行收费请求或修改文件。

[混合任务及回放](mixed.json)、[工具错误](tool-error.json)、[取消](cancel.json)与同名日志均已归档。归档前按加载后的环境密钥精确值检查，无凭据输出。文件和源码摘要见 [checksums.json](checksums.json)。

边界：这是同进程、同存储的持久回放验证，未模拟进程崩溃、网络中断或供应商部分流错误。工具错误与取消触发是案例主动控制，模型选择工具和之前的 usage 来自真实服务。本轮没有遇到额外模型/供应商失败，未做金额核对。
