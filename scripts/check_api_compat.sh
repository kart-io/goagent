#!/usr/bin/env bash
# 比较基线版本与当前工作区的导出 API，稳定包出现不兼容变更即失败。
# 用法：scripts/check_api_compat.sh [BASE_REF]，BASE_REF 默认为最近的 tag。
# ALLOW_BREAKING=1 时只报告不失败（用于已声明破坏性变更的 PR）。
set -euo pipefail
cd "$(dirname "$0")/.."

APIDIFF="golang.org/x/exp/cmd/apidiff@v0.0.0-20261014143641-f0251eb4aa3e"
MODULE="$(go list -m)"
BASE="${1:-$(git describe --tags --abbrev=0)}"
STABLE="scripts/stable-packages.txt"

work="$(mktemp -d)"
trap 'git worktree remove --force "$work/base" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

git worktree add --quiet --detach "$work/base" "$BASE"
(cd "$work/base" && go run "$APIDIFF" -m -w "$work/base.exp" "$MODULE")
go run "$APIDIFF" -m -w "$work/head.exp" "$MODULE"
go run "$APIDIFF" -m -incompatible "$work/base.exp" "$work/head.exp" 2>/dev/null >"$work/diff.txt" || true

# 行格式：- ./<包路径>.<标识符>: <变更>；根包为 - ./.<标识符>
grep -v '^\s*#' "$STABLE" | sed '/^\s*$/d' >"$work/stable.txt"
awk -v list="$work/stable.txt" '
  BEGIN { while ((getline p < list) > 0) stable[p] = 1 }
  /^- \.\// {
    sym = substr($2, 3); sub(/:$/, "", sym)
    n = split(sym, parts, "/"); last = parts[n]; dot = index(last, ".")
    pkg = (n > 1 ? substr(sym, 1, length(sym) - length(last)) : "") substr(last, 1, dot - 1)
    if (pkg in stable) print
  }' "$work/diff.txt" >"$work/breaking.txt"

if [ ! -s "$work/breaking.txt" ]; then
  echo "稳定包相对 $BASE 无不兼容 API 变更。"
  exit 0
fi
echo "稳定包相对 $BASE 的不兼容 API 变更："
cat "$work/breaking.txt"
if [ "${ALLOW_BREAKING:-0}" = "1" ]; then
  echo "ALLOW_BREAKING=1：已声明破坏性变更，仅报告。"
  exit 0
fi
echo "稳定包的破坏性变更须先弃用（见 docs/API_STABILITY.md）；确需破坏时给 PR 加 breaking-change 标签并在 CHANGELOG 写迁移说明。" >&2
exit 1
