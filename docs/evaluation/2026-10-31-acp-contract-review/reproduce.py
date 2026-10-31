#!/usr/bin/env python3
"""独立模块复现 SDK 行为并核验会话元数据前提，不改变主项目依赖。"""
import json
import os
from pathlib import Path
import subprocess
import tempfile

env = os.environ.copy()
env.pop("GOROOT", None)
env["GOWORK"] = "off"
core = Path(__file__).resolve().parents[3]
with tempfile.TemporaryDirectory(prefix="goagent-acp-review-") as folder:
    root = Path(folder)
    root.joinpath("go.mod").write_text(
        "module acp-contract-review\n\ngo 1.25.0\n\nrequire (\n"
        "github.com/coder/acp-go-sdk v0.13.5\n"
        "github.com/kart-io/goagent v0.32.0\n)\n"
        "replace github.com/kart-io/goagent => " + json.dumps(str(core)) + "\n")
    for name in ("sdk_probe_test.go", "metadata_probe_test.go"):
        root.joinpath(name).write_bytes(Path(__file__).with_name(name + ".txt").read_bytes())
    subprocess.run(["go", "mod", "tidy"], cwd=root, env=env, check=True)
    subprocess.run(["go", "test", "-race", "-v", "-count=1", "-timeout=30s"], cwd=root, env=env, check=True)
