#!/usr/bin/env bash
# Native function calling, process crash and resume; <=4 paid requests in total.
# All business effects are local simulations. Reports never overwrite old runs.
set -euo pipefail
cd "$(dirname "$0")/.."
provider=${1:?usage: business_tool_loop_live.sh gemini|deepseek|minimax NEW_REPORT_FILE}
report_file=${2:?new report file required}
case "$provider" in
 gemini) : "${GEMINI_API_KEY:?}"; : "${GEMINI_MODEL:?}" ;;
 deepseek) : "${DEEPSEEK_API_KEY:?}"; : "${DEEPSEEK_MODEL:?}" ;;
 minimax)
  if [[ -z "${MINIMAX_API_KEY:-}" && -z "${MINIMAX_CN_API_KEY:-}" ]]; then echo 'MiniMax API key required' >&2; exit 2; fi
  : "${MINIMAX_MODEL:?}" ;;
 *) echo "unsupported provider: $provider" >&2; exit 2 ;;
esac
GOAGENT_NATIVE_LIVE=1 GOAGENT_BUSINESS_PROVIDER="$provider" GOAGENT_BUSINESS_REPORT="$report_file" \
 go test -count=1 -run '^TestLiveNativeToolLoop$' -v -timeout 12m ./examples/workflow/business-cases
