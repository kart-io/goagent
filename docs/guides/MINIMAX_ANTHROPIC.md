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

若工具结果本身就是完整答案，可显式使用 `WithReturnDirectTools`。完整 JSON 直返与要求模型只输出回执是不同契约，调用方应按业务需求选择。


## 精确参数与回执验收

精确标识和小数应声明为 `type: "string"`，宿主按可信业务参考值逐字段验证；仅在解码端保留 `json.Number` 无法找回上游已截短的位数。

需要模型按指定格式回答时，复用 `WithTurnStopping` 检查最终文本，最多纠正一次，仍错误就返回失败。评测参考实现为 [`agentbench.ToolContract`](../../evaluation/agentbench/tool_contract.go)，完整装配与重放见[回归测试](../../evaluation/agentbench/tool_contract_test.go)。`CheckArguments` 应接入 Dispatcher 授权和工具执行两处；`CheckAnswer` 应在返回权威终态前再次调用。该验收器针对本组合成字段，实际业务应替换为自己的 Schema、可信数据和权限规则。

可采用 `WithMaxIterations(5)` 为停止钩子预留步骤，同时通过根预算限制 `MaxModelRequests: 3`、`MaxToolCalls: 1`，再设置请求超时。第二次错误格式应终止，不提取 JSON 字段充当模型回答成功；中间流文本仍可能已经输出。改变验收规则时，更新持久化 scope 和预算版本，避免直接复用旧策略终态。

## 有界定期回归

设置专用 `MINIMAX_CN_API_KEY` 后运行 `bash scripts/provider_live.sh minimax`。该入口固定使用国内 Anthropic 地址，默认模型为 `MiniMax-M3.1-Flash`，可通过 `MINIMAX_MODEL` 覆盖。最多三个模型请求、一次合成工具效果；检查精确字符串参数、最多一次回执纠正，以及重建后的零请求、零效果重放。网络错误不重试，任何验收失败返回非零退出码，临时检查点自动删除，不生成报告文件。

定期工作流沿用 `ENABLE_PROVIDER_LIVE` 显式开关及 `provider-live` 环境。启用前在该环境配置 `MINIMAX_CN_API_KEY` Secret，可选配置 `MINIMAX_MODEL` Variable；凭据不复用其他供应商的 `ANTHROPIC_*` 设置。
