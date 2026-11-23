#!/usr/bin/env bash
# 发版时把 CHANGELOG.md 的 [Unreleased] 条目归入新版本段，并补比较链接，供 release.yml 按 "## [x.y.z]" 抽取说明。
# 用法：scripts/finalize_changelog.sh NEW_TAG PREV_TAG [DATE]，DATE 默认为当天（UTC）。
# 版本段已存在或 Unreleased 为空时不改文件；退出码 0 表示已改，3 表示无需改。
set -euo pipefail
cd "$(dirname "$0")/.."

new_tag=${1:?缺少 NEW_TAG}
prev_tag=${2:?缺少 PREV_TAG}
date=${3:-$(date -u +%F)}
ver=${new_tag#v}
file=CHANGELOG.md

if grep -qF "## [$ver]" "$file"; then
	echo "CHANGELOG 已有 [$ver] 段，跳过"
	exit 3
fi

body=$(awk '/^## \[Unreleased\]/ {f=1; next} /^## \[/ && f {exit} f' "$file")
if [ -z "${body//[[:space:]]/}" ]; then
	echo "Unreleased 没有条目，跳过"
	exit 3
fi

repo_url=$(grep -m1 -oE '^\[Unreleased\]: https://[^ ]+/compare/' "$file" | sed -E 's|^\[Unreleased\]: ||; s|/compare/$||')
if [ -z "$repo_url" ]; then
	echo "找不到 [Unreleased] 比较链接" >&2
	exit 1
fi

tmp=$(mktemp)
awk -v ver="$ver" -v date="$date" -v url="$repo_url" -v new="$new_tag" -v prev="$prev_tag" '
	/^## \[Unreleased\]$/ && !h { print; print ""; print "## [" ver "] - " date; h=1; next }
	/^\[Unreleased\]: / && !l {
		print "[Unreleased]: " url "/compare/" new "...HEAD"
		print "[" ver "]: " url "/compare/" prev "..." new
		l=1; next
	}
	{ print }
	END { if (!h || !l) exit 1 }
' "$file" >"$tmp"
mv "$tmp" "$file"
echo "已将 Unreleased 归入 [$ver] - $date"
