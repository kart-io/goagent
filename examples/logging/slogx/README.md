# slogx logging

Wires [kart-io/slogx](https://github.com/kart-io/slogx) into goagent: `slogx.Init` configures the process logger, and `Builder.WithLogger` writes every run event as one JSON record.

```bash
cd examples/logging/slogx
go run .                       # offline, scripted model
DEEPSEEK_API_KEY=... go run .  # real DeepSeek
```

Logs go to stdout, the answer to stderr, so `go run . 2>/dev/null | jq` works.

What a run records:

| Record | Level | Fields |
| --- | --- | --- |
| `agent_start`, `model_start`, `tool_start`, `model_usage` | DEBUG | `run_id`, `path`, `agent_id`; `usage.*` token counts on `model_usage` |
| `model_end`, `tool_end`, `agent_end` | INFO | plus `duration` (ns) |
| `*_error` | ERROR (execution, panic) or WARN (canceled, deadline, ...) | plus `error_kind` |

Records carry metadata only, never prompts, model output, tool arguments or results, or provider error text. The host's own records (`客服回答完成` here) go through the same logger.

The example is its own Go module (`replace github.com/kart-io/goagent => ../../..`) so slogx and its dependencies (gorm, redis, OTLP exporters) stay out of goagent's `go.mod`. `make test-example-slogx` runs its offline test. Use slogx v0.2.2 or later; see [Host integration: Logging](../../../docs/guides/HOST_INTEGRATION.md#logging).
