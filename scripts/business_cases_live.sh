#!/usr/bin/env bash
# Explicit real-model evaluation. Six synthetic cases, <= 6 requests, no retries.
# Restart/publish/send effects remain local simulations.
set -euo pipefail
cd "$(dirname "$0")/.."
provider=${1:?usage: business_cases_live.sh gemini|deepseek|minimax NEW_REPORT_FILE}
report_file=${2:?new report file required; parent directory must exist}
case "$provider" in
  gemini)
    : "${GEMINI_API_KEY:?GEMINI_API_KEY required}"
    : "${GEMINI_MODEL:?GEMINI_MODEL required}"
    ;;
  deepseek)
    : "${DEEPSEEK_API_KEY:?DEEPSEEK_API_KEY required}"
    : "${DEEPSEEK_MODEL:?DEEPSEEK_MODEL required}"
    ;;
  minimax)
    if [[ -z "${MINIMAX_API_KEY:-}" && -z "${MINIMAX_CN_API_KEY:-}" ]]; then
      echo "MINIMAX_API_KEY or MINIMAX_CN_API_KEY required" >&2; exit 2
    fi
    : "${MINIMAX_MODEL:?MINIMAX_MODEL required}"
    ;;
  *) echo "unsupported provider: $provider" >&2; exit 2 ;;
esac
GOAGENT_BUSINESS_LIVE=1 GOAGENT_BUSINESS_PROVIDER="$provider" \
  GOAGENT_BUSINESS_REPORT="$report_file" \
  go test -count=1 -run '^TestLiveBusinessCases$' -v -timeout 12m ./examples/workflow/business-cases
