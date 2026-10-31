# GoAgent Examples

Runnable examples for the `github.com/kart-io/goagent` module. Run any of them from the repository root, e.g.:

```bash
go run ./examples/basic/01-simple-agent
```

Examples that call a real LLM read the provider's API key from the environment (see each example); the rest run offline with local or mock models.

## Directories

| Directory | Contents |
| --- | --- |
| [basic/](basic/) | Single-feature examples: simple agent, memory, Ollama, providers, DeepSeek/Kimi with the builder |
| [builder/](builder/) | `builder` API layers (simple, core, advanced) and configuration serialization |
| [tools/](tools/) | Built-in tools (calculator, HTTP API, shell, search), function tools, tools with an agent |
| [mcp/](mcp/) | MCP toolbox: basic tools, custom tools, tool chains |
| [skills/](skills/) | Skill registration, routing and a layered weather skill |
| [advanced/](advanced/) | Multi-agent collaboration, observability, parallel tool execution, tool runtime, tool selector |
| [integration/](integration/) | Human-in-the-loop, LangChain-style runnables/middleware/checkpoints, preconfigured agents |
| [llm/](llm/) | Provider examples: Anthropic, Cohere, Hugging Face, Kimi, and the Gemini native API verifier ([gemini-verify](llm/gemini-verify/README.md)) |
| [workflow/](workflow/) | `workflow` graph with branching and streaming events, offline (`go run ./examples/workflow [-stream]`) |
| [execution-scope/](execution-scope/) | `execution.NewScope` + `execution.ConfigureBuilder`: shared run/session ID, request budget and timeout, with a built-in mock model, offline |
| [generator/](generator/) | Iterating a `core.BaseAgent` stream as a generator vs. a channel (`basic`); Take/Filter and other helpers (`advanced`) |
| [error_handling/](error_handling/) | `errors` package: codes, context, wrapping, retry and fallback patterns |
| [retry-jitter/](retry-jitter/) | `ToolExecutor` retry backoff with jitter |
| [optimization/](optimization/) | `ShardedToolCache` presets |
| [translate/](translate/) | DeepSeek translation agent, batch and interactive |
| `testhelpers/` | Mock agent and assertions shared by example tests |

## Elsewhere

- Business cases (DevOps, content review, auto-reply, refund approval with crash recovery, composable orchestration and ops AI cases) live in [goagent-examples](https://github.com/kart-io/goagent-examples).
- Harness presets, plan mode and coding tools live in `github.com/kart-io/goagent-harness`.

## Prerequisites

- Go version as declared in the root `go.mod`
- API keys only for the examples that call a real LLM
