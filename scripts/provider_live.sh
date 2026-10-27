#!/usr/bin/env bash
# Explicitly opted-in, bounded synthetic live checks. No retries.
set -euo pipefail
cd "$(dirname "$0")/.."
provider=${1:?usage: provider_live.sh gemini|deepseek|openai NEW_OUTPUT_DIRECTORY}
report_dir=${2:?new output directory required}
case "$provider" in
  gemini)
    : "${GEMINI_API_KEY:?GEMINI_API_KEY required}"
    : "${GEMINI_MODEL:?GEMINI_MODEL required}"
    : "${GEMINI_EMBEDDING_MODEL:?GEMINI_EMBEDDING_MODEL required}"
    # Six cases, <= 10 upstream requests: 8 generation + 2 embedding.
    mkdir "$report_dir"
    go run ./examples/llm/gemini-verify -live -suite native -repeats 1 \
      -model "$GEMINI_MODEL" -embedding-model "$GEMINI_EMBEDDING_MODEL" \
      -out "$report_dir/results.json"
    ;;
  deepseek|openai)
    if [[ "$provider" == deepseek ]]; then
      : "${DEEPSEEK_API_KEY:?DEEPSEEK_API_KEY required}"
      model=${DEEPSEEK_MODEL:?DEEPSEEK_MODEL required}
    else
      : "${OPENAI_API_KEY:?OPENAI_API_KEY required}"
      model=${OPENAI_MODEL:?OPENAI_MODEL required}
    fi
    # Six tenant-isolated cases, <= 12 generation requests, sequential execution.
    go run ./evaluation/agentbench/cmd/stress -live -provider "$provider" \
      -model "$model" -dataset evaluation/agentbench/testdata/tenant-order-cases.json \
      -mode contract -repeats 1 -workers 1 -max-calls 2 \
      -max-output-tokens 2048 -token-stop 8000 -timeout 90s -out "$report_dir"
    ;;
  *) echo "unknown provider: $provider" >&2; exit 2 ;;
esac
