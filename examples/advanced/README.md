# Advanced Examples

Multi-feature examples. Run from the repository root with `go run ./examples/advanced/<dir>`.

| Directory | Contents |
| --- | --- |
| [multi-agent-collaboration](multi-agent-collaboration/README.md) | Analysis, strategy and execution agents built with `builder`, each with its own tools; needs Ollama, `OPENAI_API_KEY` or `GEMINI_API_KEY` |
| `multi-agent-simple` | The same three-stage flow with plain prompts on an `llm.Client`; falls back to a mock client when no provider is configured |
| `multi-agent-modular` | The three agents and a coordinator split into the `agents` package; same provider fallback |
| `multi-agent-demo` | Printed walkthrough of the multi-agent flow, no LLM |
| `observability` | `observability` telemetry provider, tracer and metrics around an agent, with a mock LLM |
| `parallel-execution` | `tools.ToolExecutor`: parallel vs. sequential, concurrency limit, errors, retries, timeouts |
| `tool-runtime` | `tools.NewToolRuntime`: tools reading/writing agent state and a `store/memory` store, stream writer, restricted runtime, cloning |
| `tool-selector` | `middleware` tool selector: always-include, max limit, caching, token comparison |
