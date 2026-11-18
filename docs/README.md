# GoAgent Documentation

[English](README.md) | [中文](README_CN.md)

GoAgent is an agent execution kernel embedded in Go services: a native tool loop, typed workflow graphs, durable execution with approval and resume, request budgets, session logs, read-only observation and MCP. Assembly features such as presets and coding tools live in the separate goagent-harness project (see the [migration guide](guides/HARNESS_MIGRATION.md), Chinese), retrievers and multi-instance durable stores live in [goagent-contrib](https://github.com/kart-io/goagent-contrib), and business examples live in [goagent-examples](https://github.com/kart-io/goagent-examples).

Pages marked *(Chinese)* are not translated yet.

## Getting started

- [Quick start](guides/QUICKSTART.md): the `AsRunnable` entry point, tools, RAG, streaming and migrating from older interfaces
- [User guide](guides/USER_GUIDE.md): tools, memory, middleware, error handling
- [Builder API reference](guides/BUILDER_API_REFERENCE.md): Builder methods, defaults and loop extension points
- [Execution contract](guides/EXECUTION_CONTRACT.md): what each execution entry point can and cannot do

- [Unified run management](../management/README.md) *(Chinese)*: Agent, Session, Task, Approval, Worker and read-only run observation; [migration guide](guides/MANAGEMENT_MIGRATION.md) *(Chinese)*

## Orchestration and durable execution

- [Human in the loop: pause, resume and checkpoints](guides/HUMAN_IN_THE_LOOP.md): human input, state edits, approval branches and cross-process resume

- [Native orchestration and tools](guides/NATIVE_ORCHESTRATION.md): `workflow.Graph`, durable graphs, Dispatcher and approval resume
- [Workflow orchestration](../workflow/README.md): graph compilation, branches, parallel joins, bounded loops and agent adapters
- [Composable orchestration](guides/COMPOSABLE_ORCHESTRATION.md): root budgets, sub-agent tools, durable context references and read-only observation
- [Session log](../core/session/README.md): event log, forks, trajectory export
- [Context policy](guides/CONTEXT_POLICY.md): tool result offloading and custom PreStep
- [Grounded RAG and streaming](guides/GROUNDED_RAG_AND_STREAMING.md) *(Chinese)*

## Host integration and governance

- [AgentRevision and session binding](guides/AGENT_REVISIONS.md) *(Chinese)*: immutable configuration and artifact archives, loading old programs, version isolation between old and new sessions

- [Skills loading](guides/SKILLS.md) *(Chinese)*: reusable Catalog, on-demand bodies and resources, optional tool adapter and host boundaries
- [Host integration](guides/HOST_INTEGRATION.md): Scope, BuildContext, resume identity and resource lifecycle
- [HTTP/SSE/WebSocket, webhooks and hooks](guides/HTTP_AGENT.md) *(Chinese)*: installation, page and client examples, host assembly, replaceable connection interfaces
- [Extension contract](guides/EXTENSION_CONTRACT.md) *(Chinese)*
- [Execution governance](guides/EXECUTION_GOVERNANCE.md): budgets, diagnostics and resource governance
- [MCP and resumable steps](guides/PRIORITY_CAPABILITIES.md) *(Chinese)*
- [Tool middleware](guides/TOOL_MIDDLEWARE.md), [caching](guides/CACHING_GUIDE.md) *(Chinese)*, [plugins: agent capability packs and the Runnable registry](guides/PLUGIN_SYSTEM_GUIDE.md) *(Chinese)*
- [Option pattern](guides/OPTION_PATTERN_BEST_PRACTICES.md) *(Chinese)*

## LLM providers

- [LLM provider guide](guides/LLM_PROVIDERS.md), [capability matrix](guides/PROVIDER_CAPABILITIES.md), [best practices](guides/PROVIDER_BEST_PRACTICES.md) *(Chinese)*, [continuous regression](guides/PROVIDER_REGRESSION.md) *(Chinese)*
- [MiniMax via the Anthropic protocol](guides/MINIMAX_ANTHROPIC.md) *(Chinese)*: China endpoints, explicit credentials, native tool continuation and verification limits
- [Embedder providers](https://github.com/kart-io/goagent-contrib/blob/master/retrieval/EMBEDDER_PROVIDERS.md) (goagent-contrib)

| Provider | Constructor (`llm/providers`) | `AsRunnable` and native tool streaming |
|---|---|---|
| OpenAI and compatible endpoints | `NewOpenAIWithOptions` | Supported |
| DeepSeek | `NewDeepSeekWithOptions` | Supported |
| Gemini | `NewGeminiWithOptions` | Supported |
| Anthropic | `NewAnthropicWithOptions` | Supported (official SDK) |
| Kimi | `NewKimiWithOptions` | Supported (OpenAI-compatible preset) |
| SiliconFlow | `NewSiliconFlowWithOptions` | Supported (OpenAI-compatible preset) |
| Ollama | `NewOllamaWithOptions` | Supported (OpenAI-compatible preset) |

`AsRunnable` and `ExecuteStream` accept any `llm.Client`; the native tool loop, `Start` handles and loop extension points need `llm.ToolStreamClient`. Every provider marked "Supported" implements both.

## Architecture and design *(Chinese)*

- [Architecture overview](architecture/ARCHITECTURE.md), [component diagram](architecture/COMPONENT_DIAGRAM.md), [import layering](architecture/IMPORT_LAYERING.md)
- [Design overview](design/DESIGN_OVERVIEW.md), [sequence diagrams](design/SEQUENCE_DIAGRAMS.md), [flow diagrams](design/FLOW_DIAGRAMS.md)
- [Harness core design](design/HARNESS_CORE_DESIGN.md)
- [Runtime readiness](guides/RUNTIME_READINESS.md): implemented capabilities, deployment limits, memory migration and pending evaluations
- [Memory design](design/MEMORY_DESIGN.md), [deletion first for long-term memory](design/MEMORY_GOVERNANCE.md), [memory administration](design/MEMORY_ADMIN.md)
- [HTTP agent design](design/HTTP_AGENT_DESIGN.md): boundary between the kernel and the network module
- [Multi-framework survey](research/2026-10-27-framework-orchestration-review.md), [Eino comparison](research/2026-11-04-eino-comparison.md)

## API reference

- [Core API](api/CORE_API.md), [Tool API](api/TOOL_API.md), [LLM API](api/LLM_API.md), [Middleware API](api/MIDDLEWARE_API.md) *(Chinese)*
- Signatures on [pkg.go.dev](https://pkg.go.dev/github.com/kart-io/goagent) or `go doc` are authoritative
- [API stability policy](API_STABILITY.md): which packages are covered, the deprecation process and the road to v1.0

## Evaluation *(Chinese)*

- [Execution overhead](evaluation/2026-10-31-execution-overhead/REPORT.md): three budget modes with reproducible raw results
- [Framework performance](evaluation/2026-10-27-framework-performance-v2/REPORT.md): GoAgent/Eino baseline and rerun code
- [GoAgent evaluation](../evaluation/agentbench/README.md)

## Development *(Chinese)*

- [Development guide](development/DEVELOPMENT_GUIDE.md), [testing best practices](development/TESTING_BEST_PRACTICES.md), [panic handling](development/PANIC_HANDLING.md)
- [Business integration and failure recovery acceptance](development/BUSINESS_ACCEPTANCE.md)

## Related links

- [Project README](../README.md)
- [Changelog](../CHANGELOG.md)
- [Examples](../examples/README.md)
