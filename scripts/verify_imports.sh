#!/usr/bin/env bash
# Parse actual Go imports from the repository root, including nested packages.
set -euo pipefail
cd "$(dirname "$0")/.."
go run ./scripts/apicheck -mode imports -root . "$@"
