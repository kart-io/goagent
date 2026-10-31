#!/usr/bin/env python3
"""检查当前开发入口的本地 Markdown 链接，并编译完整 Go main 示例。

不运行示例、不请求模型、不检查远端 URL 或页面锚点。
接口签名与依赖上下文的片段仍需人工对照源码；本脚本不宣称覆盖它们。
"""

import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_DOCS = [
    "README.md", "README_CN.md", "docs/README.md",
    "docs/api/CORE_API.md", "docs/api/TOOL_API.md",
    "docs/api/LLM_API.md", "docs/api/MIDDLEWARE_API.md",
    "docs/architecture/ARCHITECTURE.md",
    "docs/architecture/COMPONENT_DIAGRAM.md",
    "docs/architecture/IMPORT_LAYERING.md", "docs/design/DESIGN_OVERVIEW.md",
    "docs/guides/BUILDER_API_REFERENCE.md", "docs/guides/QUICKSTART.md",
    "docs/guides/USER_GUIDE.md", "docs/guides/EXECUTION_CONTRACT.md",
    "docs/guides/HARNESS_MIGRATION.md", "docs/guides/LLM_PROVIDERS.md",
    "docs/guides/COMPOSABLE_ORCHESTRATION.md", "docs/guides/HOST_INTEGRATION.md",
    "docs/guides/HTTP_AGENT.md", "docs/design/HTTP_AGENT_DESIGN.md",
]


def check(paths):
    failures = []
    mains = []
    for path in paths:
        source = path.read_text(encoding="utf-8")
        for index, code in enumerate(re.findall(r"```go\s*\n(.*?)```", source, re.S), 1):
            if re.search(r"^package main\s*$", code, re.M) and re.search(r"^func main\(", code, re.M):
                mains.append((path, index, code))
        prose = re.sub(r"```.*?```", "", source, flags=re.S)
        prose = re.sub(r"`[^`\n]*`", "", prose)
        for target in re.findall(r"\[[^\]\n]*\]\(([^)\n]+)\)", prose):
            target = target.strip("<>").split("#", 1)[0]
            if not target or re.match(r"[a-zA-Z][a-zA-Z0-9+.-]*:", target):
                continue
            if not (path.parent / target).exists():
                failures.append(f"{path.relative_to(ROOT)}：本地链接不存在：{target}")

    env = os.environ.copy()
    env["GOWORK"] = "off"
    with tempfile.TemporaryDirectory(prefix="goagent-doc-examples-") as temp:
        for number, (path, index, code) in enumerate(mains):
            snippet = Path(temp) / f"example_{number}.go"
            snippet.write_text(code, encoding="utf-8")
            result = subprocess.run(
                ["go", "build", "-o", str(Path(temp) / f"example_{number}"), str(snippet)],
                cwd=ROOT, env=env, text=True, capture_output=True,
            )
            if result.returncode:
                failures.append(f"{path.relative_to(ROOT)}：Go代码块{index}编译失败：\n{result.stdout}{result.stderr}")
    for failure in failures:
        print(failure, file=sys.stderr)
    print(f"检查文档{len(paths)}份，编译完整Go示例{len(mains)}个，问题{len(failures)}项")
    return 1 if failures else 0


if __name__ == "__main__":
    selected = [ROOT / item for item in (sys.argv[1:] or DEFAULT_DOCS)]
    sys.exit(check(selected))
