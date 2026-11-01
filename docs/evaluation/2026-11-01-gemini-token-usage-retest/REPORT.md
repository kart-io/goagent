# Gemini 真实 Token 统计复测通过

日期：2026-11-01。按用户要求执行 `source ~/.zshrc` 后，使用 Gemini 原生 Provider、`gemini-2.5-flash`、默认官方端点，运行全部三个场景，退出码为 0。真实模型入口没有回退到离线模型，也没有自动重试。

```sh
source ~/.zshrc
env -u GOROOT GOTOOLCHAIN=go1.25.0 go run ./examples/agent-usage/live \
  -provider gemini -model "${GEMINI_MODEL:-gemini-2.5-flash}" \
  -out /tmp/goagent-gemini-token-live-20261101-run2
```

`run2` 目录已存在，重新执行需指定新的输出目录。

| 场景 | 模型请求 | 工具调用 | 输入 | 输出候选 | TotalTokens | 结果 |
| --- | --- | --- | --- | --- | --- | --- |
| 单 Agent | 1 | 0 | 15 | 1 | 44 | 通过 |
| 父 Agent + writer 子 Agent | 3 | 1 | 187 | 10 | 256 | 通过 |
| 多 Agent + 普通工具完整任务 | 6 | 3 | 911 | 39 | 1273 | 通过 |

三个独立任务合计 10 次模型请求、1573 TotalTokens。各任务的全部基本 usage 完整，unknown=0、partial=0。TotalTokens 直接使用供应商报告，不以输入加输出候选重新计算。Gemini 的总量还包含思考和工具相关 Token，参见 [官方 UsageMetadata 定义](https://ai.google.dev/api/generate-content#UsageMetadata)；当前通用接口未单独输出思考字段。

## Agent 归属

| 场景 | Agent | Self | Inclusive |
| --- | --- | --- | --- |
| single | writer | 44 | 44 |
| child | coordinator | 214 | 256 |
| child | writer | 42 | 42 |
| task | coordinator | 905 | 1273 |
| task | researcher | 291 | 291 |
| task | reviewer | 77 | 77 |

完整任务真实执行了 research、review 两个子 Agent 和 lookup_order 普通工具，各一次。工具本身不伪造模型 Token，其选择及结果处理归属对应模型请求。父 Inclusive 已包含子 Agent，不重复相加。

所有场景通过以下程序校验：逐尝试钩子 Token 之和 = Agent Self 之和 = 任务 Root；通知次数 = 模型请求预留次数；尝试键不重复、证据已记账；子调用父关系正确；父 Inclusive = Root。归属结果来自统一账本，没有额外统计模式。

## 证据与边界

[结构化证据](results.json)包含每次尝试的用量和身份、每个调用的 Self/Inclusive、根汇总与工具计数；[执行日志](execution.log)保存结果摘要。归档前按 source 后环境中密钥的精确值检查文件，未保存凭据或业务对话。

本轮没有执行持久回放验证；供应商最终货币账单未核对。上一轮充值额度错误的[失败记录](../2026-11-01-gemini-token-usage-live/REPORT.md)保持独立，不计入本轮 1573 Token，也不能据其缺失 usage 认定无消费。

[真实模型运行指南](https://github.com/kart-io/goagent-examples/blob/main/agent-usage/live/README.md)。文件及测试入口源码摘要见 [checksums.json](checksums.json)。
