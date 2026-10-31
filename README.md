# GoAgent - AI Agent Framework for Go

[![Go Version](https://img.shields.io/badge/go-1.25.0-blue?style=flat-square&logo=go)](https://go.dev/)
[![Build](https://img.shields.io/github/actions/workflow/status/kart-io/goagent/ci.yml?branch=master&style=flat-square&logo=github)](https://github.com/kart-io/goagent/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/kart-io/goagent?style=flat-square)](LICENSE)
[![Documentation](https://img.shields.io/badge/docs-latest-brightgreen?style=flat-square&logo=readthedocs)](docs/)
[![Downloads](https://img.shields.io/endpoint?url=https://api.pinstudios.net/api/badges/downloads/kart-io/goagent/total)](https://github.com/kart-io/goagent/releases/latest)
[![Go Report Card](https://img.shields.io/goreportcard/grade/github.com/kart-io/goagent?style=flat-square)](https://goreportcard.com/report/github.com/kart-io/goagent)
[![Go Reference](https://img.shields.io/badge/go-reference-blue?style=flat-square&logo=go)](https://pkg.go.dev/github.com/kart-io/goagent)
[![Deepwiki](https://img.shields.io/badge/deepwiki-view-blueviolet?style=flat-square)](https://deepwiki.com/kart-io/goagent)

[English](README.md) | [中文](README_CN.md)

GoAgent is a recoverable, budgeted and observable agent execution kernel for Go services: a native tool loop, typed workflow graphs with durable checkpoints and approvals, request budgets, session logs, OpenTelemetry events and MCP.

Related repositories:

- [goagent-contrib](https://github.com/kart-io/goagent-contrib) - retrievers (BM25, hybrid, reranking, Qdrant), document loaders, practical tools and the multi-instance Postgres `durable.Store`.
- [goagent-examples](https://github.com/kart-io/goagent-examples) - business cases (DevOps, content review, auto-reply, refund recovery, live-model orchestration).
- `github.com/kart-io/goagent-harness` - presets, plan mode, compaction, skills, sub-agent components and coding tools. See the [Harness migration guide](docs/guides/HARNESS_MIGRATION.md).

## Features

- **One execution entry** - `agent.AsRunnable()` serves `Invoke`, `Stream`, `Batch` and `Pipe` from the same stream source, so tools, retrieval, middleware and callbacks run on one path.
- **Native tool loop** - the model picks tools or answers in a single streaming request (OpenAI, DeepSeek and Gemini providers); optional parallel tool calls and direct-return tools.
- **Loop extension points** - PreStep, Request, RequestError, TurnStopping hooks, prompt sections/contexts, and a running `Handle` that accepts Steer/Inject/Redirect/Followup.
- **Durable execution** - `core/durable` journals model turns and tool calls; `workflow.Graph` compiles typed graphs with branches, parallel joins, bounded loops and approval resume.
- **Governance** - root request budgets (`core/budget`), `tools.Dispatcher`/`Pipeline` with approvals, guards and admission limits, tool-result offloading, session logs (`core/session`).
- **LLM providers** - OpenAI (and compatible endpoints), DeepSeek and Gemini support `AsRunnable` and the native tool loop; Anthropic, Kimi, SiliconFlow, Ollama, Cohere and Hugging Face are available through the synchronous `Execute` entry. See the [provider table](docs/README.md#llm-提供商).
- **Observability** - read-only execution events (`core/observe`) and OpenTelemetry integration (`observability`).
- **MCP** - official MCP client sessions adapted as tools (`mcp/remote`), plus a local toolbox.

## Quick Start

```bash
go get github.com/kart-io/goagent
export OPENAI_API_KEY=...
export OPENAI_MODEL=...   # optional: OPENAI_BASE_URL for OpenAI-compatible endpoints
```

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/llm/providers"
)

func main() {
    // Reads OPENAI_API_KEY / OPENAI_MODEL / OPENAI_BASE_URL unless set with llm.With* options.
    client, err := providers.NewOpenAIWithOptions()
    if err != nil {
        log.Fatal(err)
    }

    agent, err := builder.NewSimpleBuilder(client).
        WithSystemPrompt("You are a helpful assistant.").
        WithMaxIterations(6).
        WithTimeout(30 * time.Second).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    out, err := agent.AsRunnable().Invoke(context.Background(), "Introduce yourself in one sentence.")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(out.Result)
}
```

### Tools

Tools implement `interfaces.Tool` (`Name`, `Description`, `ArgsSchema`, `Invoke`). `tools.NewFunctionTool` wraps a function:

```go
weather := tools.NewFunctionTool(
    "get_weather",
    "Look up the current weather for a city",
    `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`,
    func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
        city, _ := args["city"].(string)
        return map[string]string{"city": city, "weather": "sunny"}, nil
    },
)

agent, err := builder.NewSimpleBuilder(client).
    WithSystemPrompt("You can look up the weather.").
    WithTools(weather).
    WithMaxIterations(6). // includes the final answer
    WithRequestError(3, builder.RetryTransient(3, func(retry int) time.Duration {
        return time.Duration(retry) * 200 * time.Millisecond
    })).
    Build()
if err != nil {
    return err
}
out, err := agent.AsRunnable().Invoke(ctx, "What's the weather in Paris?")
```

### Streaming

```go
ctx, cancel := context.WithCancel(ctx)
defer cancel() // cancel when you stop consuming early

chunks, err := agent.AsRunnable().Stream(ctx, question)
if err != nil {
    return err
}
for chunk := range chunks {
    if chunk.Error != nil {
        return chunk.Error
    }
    if chunk.Event != nil && chunk.Event.Type == "text" {
        fmt.Print(chunk.Event.Delta) // transient text; may precede a tool call
    }
    if chunk.Done {
        fmt.Println(chunk.Data.Result) // final result after middleware and callbacks
    }
}
return ctx.Err()
```

Other event types are `sources`, `step` (e.g. `tool_start`/`tool_end`) and `usage`; `usage` is a cumulative snapshot. See the [Quick Start guide](docs/guides/QUICKSTART.md) and the [execution contract](docs/guides/EXECUTION_CONTRACT.md).

### Pre-configured Agents

```go
agent, err := builder.QuickAgent(client, "You are a helpful assistant.")
chat, err := builder.ChatAgent(client, "Alice")
rag, err := builder.RAGAgent(client, retriever) // DocumentRetriever, a Retrieve method, or VectorStore
```

`AnalysisAgent`, `WorkflowAgent`, `MonitoringAgent` and `ResearchAgent` preset prompts, state and middleware the same way.

### Grounded RAG

`WithRetriever(retriever, builder.RetrievalConfig{MaxDocuments: 4, MaxContextRunes: 12000})` retrieves evidence for every request and returns provenance in the output metadata key `retrieval_sources`. Retrievers satisfy `interfaces.DocumentRetriever`; implementations live in [goagent-contrib/retrieval](https://github.com/kart-io/goagent-contrib/tree/master/retrieval). See the [RAG and streaming guide](docs/guides/GROUNDED_RAG_AND_STREAMING.md).

### Running Handle and Loop Hooks

With a `llm.ToolStreamClient` and at least one tool, `agent.Start(ctx, input)` runs in the background and returns a `*builder.Handle` (`Steer`, `Inject`, `Redirect`, `Followup`, `Interrupt`, `Wait`). Hooks (`WithPreStep`, `WithIdentifiedPreStep`, `WithRequestHook`, `WithRequestError`, `WithTurnStopping`, `WithPromptSections`, `WithPromptContexts`), tool-result offloading (`WithOffload`) and session logs (`WithSessionLog`) are described in the [builder README](builder/README.md), the [context policy guide](docs/guides/CONTEXT_POLICY.md) and [core/session](core/session/README.md).

### Durable Execution and Orchestration

- [Native orchestration](docs/guides/NATIVE_ORCHESTRATION.md) - `workflow.Graph`, durable graphs via `AsRunnable`, branch-level resume and `tools.Dispatcher`.
- [Workflow](workflow/README.md) - typed graphs, branches, parallel joins and bounded loops.
- [Composable orchestration](docs/guides/COMPOSABLE_ORCHESTRATION.md) - wrap a child agent or `workflow.DurableGraph` as a tool with `tools.NewRunnableToolFactory`; a child waiting for approval suspends the parent with `builder.ErrWaiting`.
- [Host integration](docs/guides/HOST_INTEGRATION.md) - `execution.NewScope` + `execution.ConfigureBuilder` + `BuildContext`; offline example: `go run ./examples/execution-scope`.
- [Extension contract](docs/guides/EXTENSION_CONTRACT.md) and [execution governance](docs/guides/EXECUTION_GOVERNANCE.md) - budgets, diagnostics and resource limits.

## Packages

| Package | Description |
|---------|-------------|
| [builder](builder/) | Fluent agent construction, native tool loop, hooks, `Handle` |
| [builder/contextpolicy](builder/contextpolicy/) | Tool-result offloading (`Offload`) |
| [core](core/) | `Runnable`/`StreamRunnable`, callbacks, state; subpackages `durable`, `budget`, `session`, `observe`, `checkpoint`, `middleware` |
| [execution](execution/) | Host scope: run/session identity, journal, budget, deadline, observer |
| [workflow](workflow/) | Typed graph compilation and durable graphs |
| [llm](llm/) | Client interfaces, options; `llm/providers` implementations |
| [tools](tools/) | Tool registry, function tools, `Dispatcher`, `Pipeline`; `tools/policy`, `tools/spill` and built-in tools |
| [mcp](mcp/) | MCP client adapter and toolbox |
| [memory](memory/) | Conversation and case memory, vector stores |
| [harness/env](harness/env/) | File system and command execution interfaces for tools |
| [observability](observability/) | OpenTelemetry tracing and metrics |
| [evaluation/agentbench](evaluation/agentbench/README.md) | Reproducible task evaluation |
| [testing](testing/) | Mocks, test utilities and session-log replay |

Other packages: `cache`, `errors`, `interfaces`, `parsers`, `store`, `middleware`, `devtools`, `utils`.

## Documentation

- [Quick Start](docs/guides/QUICKSTART.md)
- [User Guide](docs/guides/USER_GUIDE.md)
- [Builder API Reference](docs/guides/BUILDER_API_REFERENCE.md)
- [LLM Providers](docs/guides/LLM_PROVIDERS.md)
- [Execution overhead measurements](docs/evaluation/2026-10-31-execution-overhead/REPORT.md)

See [docs/README.md](docs/README.md) for the full index and [CHANGELOG.md](CHANGELOG.md) for breaking changes.

## Examples

Runnable examples live in [`examples/`](examples/README.md); business cases live in [goagent-examples](https://github.com/kart-io/goagent-examples).

```bash
go run ./examples/basic/01-simple-agent
go run ./examples/workflow
```

## Development

```bash
git clone https://github.com/kart-io/goagent.git
cd goagent
go test ./...
golangci-lint run
bash scripts/verify_imports.sh   # import layering
bash scripts/check_coverage.sh   # coverage threshold
```

## License

Apache License 2.0 - see [LICENSE](LICENSE).

## Support

- **Issues**: [GitHub Issues](https://github.com/kart-io/goagent/issues)
- **Discussions**: [GitHub Discussions](https://github.com/kart-io/goagent/discussions)

## Acknowledgments

GoAgent is inspired by [LangChain](https://github.com/langchain-ai/langchain), [LangGraph](https://github.com/langchain-ai/langgraph) and [AutoGen](https://github.com/microsoft/autogen).
