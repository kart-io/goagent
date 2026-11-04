#!/usr/bin/env bash
# 兼容系统 Bash；统计逻辑由 Go 实现，不依赖关联数组或 bc。
set -euo pipefail
cd "$(dirname "$0")/.."
profile=${1:-coverage/coverage.out}
# 保留 CI 使用的过滤版 profile，方便 go tool cover 与报告生成。
if [[ ! -f "$profile" ]]; then
  echo "覆盖率文件不存在: $profile；请先运行 make coverage，或传入已有文件路径。" >&2
  exit 1
fi
awk '!/^github[.]com\/kart-io\/goagent\/(.*\/)?examples?\//' "$profile" > "${profile%.out}.lib.out"
go run ./scripts/coveragecheck -profile "$profile"
