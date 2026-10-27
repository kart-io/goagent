#!/usr/bin/env bash
# Deterministic offline contract suite; no retries or real API credentials needed.
set -euo pipefail
cd "$(dirname "$0")/.."
report=${1:-provider-contracts.jsonl}
# pipefail preserves go test's exit status while retaining failure evidence.
go test -json -race -short -count=1 -timeout=5m \
  ./llm/... ./builder ./evaluation/agentbench/... | tee "$report"
