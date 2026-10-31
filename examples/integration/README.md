# Integration Examples

Examples that combine several packages. Run from the repository root with `go run ./examples/integration/<dir>`; none of them call a real LLM.

| Directory | Contents |
| --- | --- |
| `human-in-loop` | Interrupts, priorities, human input, rule-based interrupts, persistence via `core/checkpoint`, hooks |
| `langchain-inspired` | `core` Runnable pipes, callbacks, `cache` and `parsers` |
| `langchain-phase1` | `core/state`, `core/execution` runtime, `store/memory` and checkpointer |
| `langchain-phase2` | `core/middleware` request/response interceptor chains |
| `langchain-complete` | An agent assembled with `builder` using the features above |
| `preconfig-agents` | Preconfigured agent templates from `builder`, with a mock LLM |

Business cases (DevOps, content review, auto-reply, refund approval with crash recovery, composable orchestration) live in [goagent-examples](https://github.com/kart-io/goagent-examples).
