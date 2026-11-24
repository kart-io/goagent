# Calling MiniMax over the Anthropic Protocol

[English](MINIMAX_ANTHROPIC.md) | [中文](MINIMAX_ANTHROPIC_CN.md)

MiniMax is integrated through `providers.NewAnthropicWithOptions`. The mainland China base URL is `https://api.minimax.cn/anthropic`, and the actual request path is `/anthropic/v1/messages`. The model is explicitly set to `MiniMax-M3.1-Flash`. For endpoint, model, and authentication configuration, see the [official MiniMax documentation](https://platform.minimax.cn/docs/api-reference/text-anthropic-api).

The function below can be placed in a host service and reads the credential from `MINIMAX_CN_API_KEY`; the caller can also obtain the key from its own credential management component and pass it to `llm.WithAPIKey`.

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

The returned client implements `llm.ToolStreamClient` and can be passed to `builder.NewSimpleBuilder(client)`. Once the model, base URL, and key are passed explicitly, local `ANTHROPIC_*` default settings used for other providers do not replace these values. The `Provider()` identifier is still `anthropic`; the provider and model identity should be determined from the actual endpoint, the requested model, and the response model.

The tool loop must fully preserve the assistant message's `ProviderState`, which contains native thinking, signatures, and tool_use. The Builder preserves it automatically; when continuing manually, put the final block's `ToolCalls` and `ProviderState` together into the next assistant message, then append the corresponding tool results. Thinking tokens of `MiniMax-M3.1-Flash` count toward the output limit; do not send `thinking.type=disabled`. The official documentation is authoritative for the provider's specific limits.

If a tool result is itself the complete answer, use `WithReturnDirectTools` explicitly. Returning complete JSON directly and requiring the model to output only a receipt are different contracts; callers should choose according to business needs.


## Exact Arguments and Receipt Acceptance

Exact identifiers and decimals should be declared as `type: "string"`, and the host validates them field by field against trusted business reference values; keeping `json.Number` only on the decoding side cannot recover digits already truncated upstream.

When the model must answer in a specified format, reuse `WithTurnStopping` to check the final text, correct it at most once, and return a failure if it is still wrong. The evaluation reference implementation is [`agentbench.ToolContract`](../../evaluation/agentbench/tool_contract.go); see the [regression test](../../evaluation/agentbench/tool_contract_test.go) for complete assembly and replay. `CheckArguments` should be wired into both Dispatcher authorization and tool execution; `CheckAnswer` should be called again before returning the authoritative final state. This acceptance checker targets this set of synthetic fields; real business should replace it with its own schema, trusted data, and permission rules.

You can use `WithMaxIterations(5)` to reserve steps for the stop hook, while limiting the root budget with `MaxModelRequests: 3` and `MaxToolCalls: 1`, and then set a request timeout. A second malformed answer should terminate the run; do not extract JSON fields to stand in for a successful model answer; intermediate streamed text may already have been output. When changing acceptance rules, update the durable scope and budget version to avoid reusing final states from the old policy directly.

## Bounded Periodic Regression

After setting a dedicated `MINIMAX_CN_API_KEY`, run `bash scripts/provider_live.sh minimax`. This entry point always uses the mainland China Anthropic URL; the default model is `MiniMax-M3.1-Flash` and can be overridden with `MINIMAX_MODEL`. It makes at most three model requests and one synthetic tool effect; it checks exact string arguments, at most one receipt correction, and zero-request, zero-effect replay after rebuilding. Network errors are not retried, any acceptance failure returns a non-zero exit code, temporary checkpoints are deleted automatically, and no report file is generated.

The periodic workflow keeps the explicit `ENABLE_PROVIDER_LIVE` switch and the `provider-live` environment. Before enabling it, configure the `MINIMAX_CN_API_KEY` secret in that environment, and optionally the `MINIMAX_MODEL` variable; credentials do not reuse other providers' `ANTHROPIC_*` settings.
