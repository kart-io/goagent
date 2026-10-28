# Framework performance comparison

[Recorded comparison report](../../docs/evaluation/2026-10-27-framework-performance-v2/REPORT.md)

This is an isolated benchmark module. Its dependencies do not change the main GoAgent module.

The benchmark runs real public framework entry points against deterministic local models. No credentials or paid APIs are used. The historical Python runs rejected network connections and disabled external tracing. This measures warm framework execution plus native model/tool protocol object handling, not model quality, HTTP/SSE parsing, provider inference, distributed durability, or production throughput.

The table and Python methodology below describe the archived comparison. Only the Go workloads remain executable.

## Compared entry points

| Framework | Entry | Scenarios |
| --- | --- | --- |
| GoAgent (local source) | Graph.Compile().Invoke; Agent.AsRunnable().Invoke/Stream | all four |
| Eino v0.9.22 | compose.Graph.Compile().Invoke; adk.ChatModelAgent + Runner | all four |
| LangGraph | StateGraph.compile().ainvoke | graph, loop |
| LangChain on LangGraph | create_agent().ainvoke/astream | tool, stream |
| OpenAI Agents SDK | Runner.run/run_streamed | tool, stream |
| Google ADK Python | InMemoryRunner.run_async | tool, stream |

Python versions are locked in requirements.lock. LangChain agent results and LangGraph graph results are named separately because they exercise different API layers. Temporal is excluded: a server-backed durable workflow benchmark is not equivalent to these in-process runs. This is not a benchmark of Google ADK Go, Eino legacy ReAct, LangGraph server, or distributed scheduling.

## Workload contracts

- graph_8: eight serial integer increments; input 0, final output 8.
- branch_loop_8: one increment node and a conditional edge, eight visits; final output 8.
- agent_tool: model requests lookup(order_id="DEMO-1001"), tool returns "30", model validates the tool receipt and returns "30"; exactly two model calls and one tool call. No return-direct shortcut.
- stream_64: one local model emits 64 ready text deltas, each "x"; final answer is 64 characters. Partial chunks and the final snapshot are not added together. First-observed-delta latency is recorded separately.

Every measured operation validates output and call/chunk counts. Invalid operations stop the benchmark; failures are retained. Fake model counts are actual adapter entry counts, not token or billing measurements. Framework-native schemas and protocol objects differ; the returned business data and work performed are equal. In particular GoAgent Invoke currently consumes its streaming execution source, while Eino ADK uses Generate for the nonstreaming agent workload.

Frameworks, graphs, agents and tools are constructed before timing. GoAgent has no conversation MemoryManager, Eino has no checkpoint store, LangChain/LangGraph have no checkpointer, OpenAI has no session, and ADK uses a fresh in-memory session. ADK session create/delete occur outside the timed region; runtime session reads/events remain inside. Compilation/import/first-use warmup is excluded. Local models return immediately, so stream results describe handling of already available chunks, not paced network TTFT.

All framework-default state updates, model object construction, tool argument decoding, and completion work remain timed. Tracing exporters are disabled/noop for all. Python GC is enabled. All Python agent/graph entries use one asyncio loop per benchmark job; no per-operation asyncio.run. Python nodes, routes and tools are native async callbacks, avoiding avoidable executor hops for tiny operations. The driver removes PYTHONASYNCIODEBUG (setting it to "0" would still enable debugging), and rejects rows whose event loop reports debug enabled. Go is set to GOMAXPROCS=4. Cross-language numbers include runtime and framework differences; Go allocations are only compared with Go allocations.

## Reproduce the Go comparison

The Python drivers and tests have been removed. The cross-language report above is historical; its JSON results and dependency lock remain as provenance. The runnable module now compares GoAgent and Eino through Go entry points.

```sh
cd evaluation/frameworkbench
env -u GOROOT GOTOOLCHAIN=go1.25.0 go test -race ./...
env -u GOROOT GOTOOLCHAIN=go1.25.0 go build -o /tmp/goagent-frameworkbench-runner ./cmd/runner
GOMAXPROCS=4 /tmp/goagent-frameworkbench-runner \
  -framework goagent -scenario agent_tool -samples 400 -warmup 50
GOMAXPROCS=4 /tmp/goagent-frameworkbench-runner \
  -framework eino -scenario agent_tool -samples 400 -warmup 50
GOMAXPROCS=4 env -u GOROOT GOTOOLCHAIN=go1.25.0 go test -run '^$' \
  -bench '^BenchmarkFramework$' -benchmem -benchtime=300ms -count=5
```

Run benchmarks serially without other tests or compilation competing for CPU. Construction is excluded from Go testing.B timing. Keep generated command output outside the repository; log files are ignored by Git.

Historical `results.jsonl`, `summary.json` and manifests retain the recorded samples, statistics, dependency versions and source hashes. Removed Python sources are historical provenance entries, not current executable paths. These results do not measure concurrent throughput, distributed durability or production model latency.
