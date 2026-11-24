#!/usr/bin/env bash
# 兼容系统 Bash；统计逻辑由 Go 实现，门槛见 scripts/coverage-gates.txt。
# 用法：check_coverage.sh [PROFILE] [SUMMARY_MD]
set -euo pipefail
cd "$(dirname "$0")/.."
profile=${1:-coverage/coverage.out}
summary=${2:-}
if [[ ! -f "$profile" ]]; then
  echo "覆盖率文件不存在: $profile；请先运行 make coverage，或传入已有文件路径。" >&2
  exit 1
fi
# 保留过滤版 profile，供 go tool cover 与既有报告消费。
awk '!/^github[.]com\/kart-io\/goagent\/(.*\/)?examples?\//' "$profile" > "${profile%.out}.lib.out"
go run ./scripts/coveragecheck -profile "$profile" ${summary:+-summary "$summary"}
