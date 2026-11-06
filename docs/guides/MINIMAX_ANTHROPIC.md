# 使用 Anthropic 协议调用 MiniMax

MiniMax 通过 `providers.NewAnthropicWithOptions` 接入，国内基地址为 `https://api.minimax.cn/anthropic`，实际请求路径为 `/anthropic/v1/messages`。模型显式选择 `MiniMax-M3.1-Flash`。端点、模型与认证配置参见 [MiniMax 官方说明](https://platform.minimax.cn/docs/api-reference/text-anthropic-api)。

以下函数可放入宿主服务，凭据从 `MINIMAX_CN_API_KEY` 读取；调用方也可以从自己的凭据管理组件取得 key，再传给 `llm.WithAPIKey`。

```go
package minimaxexample

import (
	"fmt"
	"os"
	"time"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
)

func NewClient() (*providers.AnthropicProvider, error) {
	key := os.Getenv("MINIMAX_CN_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("未设置 MINIMAX_CN_API_KEY")
	}
	return providers.NewAnthropicWithOptions(
		llm.WithAPIKey(key),
		llm.WithBaseURL("https://api.minimax.cn/anthropic"),
		llm.WithModel("MiniMax-M3.1-Flash"),
		llm.WithMaxTokens(4096),
		llm.WithTimeout(80*time.Second),
		llm.WithStreamIdleTimeout(40*time.Second),
		llm.WithStreamTimeout(100*time.Second),
		llm.WithCustomHeaders(map[string]string{"Authorization": "Bearer " + key}),
	)
}
```

返回的客户端实现 `llm.ToolStreamClient`，可交给 `builder.NewSimpleBuilder(client)` 使用。显式传入模型、基地址和 key 后，本机用于其他供应商的 `ANTHROPIC_*` 默认配置不会替换这些值。`Provider()` 标识仍是 `anthropic`，供应商和模型身份需结合实际端点、请求模型与响应模型判断。

工具循环需要完整保留 assistant 消息的 `ProviderState`，其中包含原生 thinking、签名和 tool_use。Builder 会自动保留；手工续接时应把终态块的 `ToolCalls`、`ProviderState` 一并放入下一轮 assistant 消息，再追加对应工具结果。`MiniMax-M3.1-Flash` 的思考 Token 计入输出上限；不要发送 `thinking.type=disabled`。供应商的具体限制以官方说明为准。

2026-11-07 的真实验证使用本接口完成流式响应、原生内容回传，以及工具效果落盘后的进程退出、恢复和零请求重放，详见[完整报告](../evaluation/2026-11-07-minimax-anthropic/REPORT.md)。同时保留两项失败：上游原始数值参数把高精度小数缩短；一次模型回答没有遵守只返回回执的格式。精确业务字段可在工具 Schema 中声明为字符串，并由宿主校验原文。若工具结果本身就是完整答案，可显式使用 `WithReturnDirectTools`，其完整 JSON 直返与“要求模型只输出回执”是不同契约。


## 精确参数与回执验收

[后续修复实测](../evaluation/2026-11-07-live-contract-remediation/REPORT.md)完成了字符串精度、模型最终回执与持久化重放验证。精确标识和小数应声明为 `type: "string"`，宿主按可信业务参考值逐字段验证；仅在解码端保留 `json.Number` 无法找回上游已截短的位数。原数值模式失败记录继续保留。

需要模型按指定格式回答时，复用 `WithTurnStopping` 检查最终文本，最多纠正一次，仍错误就返回失败。评测参考实现为 [`agentbench.ToolContract`](../../evaluation/agentbench/tool_contract.go)，完整装配与重放见[回归测试](../../evaluation/agentbench/tool_contract_test.go)。`CheckArguments` 应接入 Dispatcher 授权和工具执行两处；`CheckAnswer` 应在返回权威终态前再次调用。该验收器针对本组合成字段，实际业务应替换为自己的 Schema、可信数据和权限规则。

本组工具循环采用 `WithMaxIterations(5)` 为停止钩子预留步骤，同时通过根预算限制 `MaxModelRequests: 3`、`MaxToolCalls: 1`，再设置请求超时。第二次错误格式会终止，不提取 JSON 字段充当模型回答成功；中间流文本仍可能已经输出。改变验收规则时，更新持久化 scope 和预算版本，避免直接复用旧策略终态。
