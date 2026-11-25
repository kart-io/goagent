# Provider Continuous Regression

[English](PROVIDER_REGRESSION.md) | [中文](PROVIDER_REGRESSION_CN.md)

Offline entry point:

```sh
bash scripts/provider_regression.sh /tmp/provider-contracts.jsonl
```

`.github/workflows/provider-contracts.yml` runs on pushes and PRs to master and develop (it can also be triggered manually), with Go 1.27, race, short, count=1, and no retry on failure. Native OpenAI/DeepSeek and Gemini protocols are covered by a standalone HTTP test server for tool result continuation, signature preservation, truncation and missing terminal states, cancellation, usage, and capability degradation; the script also runs the contract tests of `./builder` and `./evaluation/agentbench/...`. The import layering check is run by the CI/PR workflow, and leftover calls to removed APIs are caught by the compile check. The JSONL is uploaded even on failure. This step uses no real API keys.

The live entry point specifies the provider, model, and a new output directory explicitly:

```sh
GEMINI_MODEL=gemini-2.5-flash GEMINI_EMBEDDING_MODEL=gemini-embedding-001 \
  bash scripts/provider_live.sh gemini /tmp/gemini-new-results
DEEPSEEK_MODEL=deepseek-flash \
  bash scripts/provider_live.sh deepseek /tmp/deepseek-new-results
```

MiniMax uses the China-region Anthropic protocol: run `bash scripts/provider_live.sh minimax`, which reads the dedicated `MINIMAX_CN_API_KEY`; the default model is `MiniMax-M3.1-Flash`, and `MINIMAX_MODEL` is optional. No output directory is needed; temporary checkpoints are deleted automatically, and no report is written.

The other keys are read from the corresponding `GEMINI_API_KEY` / `DEEPSEEK_API_KEY` environment variables. The script also supports `openai`, which needs `OPENAI_MODEL` and `OPENAI_API_KEY` and is not part of the default paid matrix.

| Suite | Cases | Max upstream requests | Limits |
|---|---:|---:|---|
| MiniMax Anthropic | 1 full tool flow plus rebuild replay | 3 | One tool effect, at most one format correction, 180-second total deadline, at most 4096 output tokens per request |
| Gemini native | 6 | 10 (8 generation + 2 embedding) | 90 seconds per case, at most 2048 output tokens for generation |
| DeepSeek tenant orders | 6 | 12 | Sequential, at most 2 requests and 90 seconds per case, at most 2048 output tokens per request |

There is no automatic retry; failures save results and return non-zero. DeepSeek's 8000-token stop threshold is a per-task observed threshold, not a global cost cap. The Gemini Embed API does not report usage, so the known tokens in the report exclude embedding requests; a request cap is not a monetary cap. The data is locally synthesized read-only orders, with no real orders or payment writes.

`provider-live.yml` is scheduled every Monday at 03:30 UTC and can also be triggered manually. Before enabling it, set `ENABLE_PROVIDER_LIVE=true` in the repository variables, and configure `MINIMAX_CN_API_KEY` and the two keys above, plus the `GEMINI_MODEL`, `GEMINI_EMBEDDING_MODEL`, and `DEEPSEEK_MODEL` variables, in the `provider-live` environment. When not enabled, the whole job is skipped; once enabled, missing configuration fails the job, so missing keys are never reported as passing tests. PRs do not trigger paid calls and do not receive keys. The workflow has read-only repository permissions and a 20-minute timeout, and Gemini/DeepSeek results are kept for 14 days; MiniMax only outputs the acceptance status and uploads no report.

For the latest local live run see the regression results.
