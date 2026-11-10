# GoAgent Examples

Runnable examples for the `github.com/kart-io/goagent` module. Run any of them from the repository root, e.g.:

```bash
go run ./examples/workflow
```

Examples that call a real LLM read the provider's API key from the environment (see each example); the rest run offline with local or mock models.

## Directories

| Directory | Contents |
| --- | --- |
| [basic/](basic/) | Single-feature examples: Ollama, providers, DeepSeek/Kimi with the builder |
| [builder/](builder/) | `builder` API layers (simple, core, advanced) and configuration serialization |
| [tools/](tools/) | Built-in tools (calculator, HTTP API, shell, search), function tools, tools with an agent |
| [skills/](skills/) | Skill registration, routing, a layered weather skill and [standalone SKILL.md loading](skills/03-catalog/) |
| [advanced/](advanced/) | Multi-agent collaboration, observability, tool selector |
| [integration/](integration/) | Human-in-the-loop, LangChain-style runnables/middleware/checkpoints, preconfigured agents |
| [llm/](llm/) | Provider examples: Anthropic, Kimi, and the Gemini native API verifier ([gemini-verify](llm/gemini-verify/README.md)) |
| [Token 统计案例](https://github.com/kart-io/goagent-examples/tree/main/agent-usage) | 已迁至 goagent-examples：任务、单 Agent／子 Agent、多平台及错误边界 |
| [workflow/](workflow/) | `workflow` graph with branching and streaming events, offline (`go run ./examples/workflow [-stream]`) |
| [human-in-the-loop/](human-in-the-loop/) | Persisted review gates: inspect checkpoints, approve/reject, edit state and resume in another process, offline |
| [multi-agent-patterns/](multi-agent-patterns/) | `multiagent` presets (router, loop, parallel lookup, refund review, supervisor tools, fallback, composed graph) driven by `mocks.ScriptedClient`, offline |
| [human-in-the-loop-interrupt/](human-in-the-loop-interrupt/) | Node pauses mid-execution for human input and resumes across processes, offline |
| [execution-scope/](execution-scope/) | `execution.NewScope` + `execution.ConfigureBuilder`: shared run/session ID, request budget and timeout, with a built-in mock model, offline |
| [error_handling/](error_handling/) | `errors` package: codes, context, wrapping, retry and fallback patterns |
| [translate/](translate/) | DeepSeek translation agent, batch and interactive |
| `testhelpers/` | Assertions and environment helpers shared by example tests |

## Elsewhere

- Business cases (DevOps, content review, auto-reply, refund approval with crash recovery, composable orchestration and ops AI cases) live in [goagent-examples](https://github.com/kart-io/goagent-examples).
- Harness presets, plan mode and coding tools live in `github.com/kart-io/goagent-harness`.

## Prerequisites

- Go version as declared in the root `go.mod`
- API keys only for the examples that call a real LLM

## 不可变版本与旧程序加载

[agent-revisions](agent-revisions/README.md) 编译两个 Go 程序，归档程序与提示词，删除原工件后分别重载执行 v1、v2、v1，验证旧修订仍可运行。执行 `go run ./examples/agent-revisions`，不需要模型或 Docker。
