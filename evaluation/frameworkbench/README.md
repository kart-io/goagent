# Framework performance comparison

[Recorded comparison report](../../docs/evaluation/2026-10-27-framework-performance-v2/REPORT.md)

This is an isolated benchmark module. Its dependencies do not change the main GoAgent module.

The benchmark runs real public framework entry points against deterministic local models. No credentials or paid APIs are used. Python network connections are rejected and external tracing is disabled. This measures warm framework execution plus native model/tool protocol object handling, not model quality, HTTP/SSE parsing, provider inference, distributed durability, or production throughput.

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

## Reproduce

Use a native arm64 Python 3.14 environment on the recorded machine, or record the replacement runtime in a new report. No global environment configuration is modified. `env -u GOROOT` avoids stale local GOROOT settings; it is not needed on a correctly configured installation.

```sh
cd evaluation/frameworkbench
python3.14 -m venv /tmp/goagent-frameworkbench
/tmp/goagent-frameworkbench/bin/python -m pip install -r requirements.lock

env -u GOROOT go test -race ./...
/tmp/goagent-frameworkbench/bin/python -m unittest test_python_runner test_statistics
env -u GOROOT go build -o /tmp/goagent-frameworkbench-runner ./cmd/runner

/tmp/goagent-frameworkbench/bin/python run_comparison.py \
  --go-runner /tmp/goagent-frameworkbench-runner \
  --out /tmp/framework-comparison-new \
  --samples 400 --warmup 50 --repeats 5

GOMAXPROCS=4 env -u GOROOT go test -run '^$' -bench '^BenchmarkFramework$' \
  -benchmem -benchtime=300ms -count=5 > /tmp/framework-go-allocations.log
```

The output directory must not already exist. Jobs run sequentially, with a seeded random order within each repetition, in fresh processes. Do not compile, test or run another benchmark concurrently with timing. Defaults yield 80 jobs and 32,000 measured operations, plus warmups.

`results.jsonl` retains every timing sample. `summary.json` reports the median of five per-process p50 values, median of per-process nearest-rank p95 values, and the min/max p50 across repeats. It does not pool samples from different processes into an apparent single confidence interval. First-delta medians are summarized separately. Go `ns/op`, `B/op`, and `allocs/op` are measured in a separate pass with testing.B, construction outside ResetTimer. Throughput under concurrent load, RSS attribution, and durability require separate workloads.

`manifest.json` records source SHA256, dependency versions in each Python row, Git baseline, runtime, GOMAXPROCS, sample counts and schedule seed. The local GoAgent tree may include uncommitted documentation/benchmark changes; source hashes identify the benchmark code. Reports must state that the local GoAgent implementation under test is the recorded commit, and record any subsequent production changes.
