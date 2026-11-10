# Integration Examples

Examples that combine several packages. Run from the repository root with `go run ./examples/integration/<dir>`; none of them call a real LLM.

| Directory | Contents |
| --- | --- |
| `langchain-inspired` | `core` Runnable pipes, callbacks, `cache` and `parsers` |
| `langchain-phase2` | `core/middleware` request/response interceptor chains |
| `preconfig-agents` | Preconfigured agent templates from `builder`, with a mock LLM |

For human-in-the-loop pause and resume, see [human-in-the-loop](../human-in-the-loop/README.md) and [human-in-the-loop-interrupt](../human-in-the-loop-interrupt/README.md).

Business cases (DevOps, content review, auto-reply, refund approval with crash recovery, composable orchestration) live in [goagent-examples](https://github.com/kart-io/goagent-examples).
