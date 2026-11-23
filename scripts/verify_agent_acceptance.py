#!/usr/bin/env python3
"""核对 Agent 验收映射、完整 Go 测试日志及同名 .exit 文件。"""

import argparse
import json
from pathlib import Path


REQUIRED = ({f"U{i:02}" for i in range(14)} |
            {f"A{i:02}" for i in range(1, 24)} |
            {f"I{i:02}" for i in range(1, 8)})
ACTIONS = {"start", "run", "pause", "cont", "pass", "fail", "output", "skip", "bench",
           "build-start", "build-output", "build-fail"}
REPOS = {"goagent", "goagent-harness"}


def read_log(root, filename):
    errors = []
    events = []
    path = (root / filename).resolve()
    exit_path = path.with_suffix(".exit")
    if not path.is_relative_to(root) or not exit_path.resolve().is_relative_to(root):
        return events, [f"日志或退出状态位于验收目录之外: {filename}"]
    try:
        exit_code = int(exit_path.read_text(encoding="utf-8").strip())
        if exit_code != 0:
            errors.append(f"命令非零退出: {filename}: {exit_code}")
    except (OSError, ValueError):
        errors.append(f"退出状态缺失或非法: {exit_path.name}")
    try:
        for line in path.read_text(encoding="utf-8").splitlines():
            event = json.loads(line)
            if (not isinstance(event, dict) or not isinstance(event.get("Action"), str) or
                    event["Action"] not in ACTIONS or
                    not isinstance(event.get("Package"), str) or not event["Package"] or
                    ("Test" in event and not isinstance(event["Test"], str))):
                raise ValueError("非法Go测试事件")
            events.append(event)
        if not events:
            raise ValueError("空Go测试日志")
    except (OSError, ValueError) as err:
        errors.append(f"日志解析失败: {filename}: {err}")
    if any(e["Action"] in ("fail", "build-fail") for e in events):
        errors.append(f"日志包含失败: {filename}")
    return events, errors


def verify(root):
    root = root.resolve()
    errors = []
    covered = set()
    logs = {}
    try:
        rows = json.loads((root / "required-tests.json").read_text(encoding="utf-8"))
        if not isinstance(rows, list) or not rows:
            raise ValueError("验收清单必须是非空数组")
    except (OSError, ValueError) as err:
        rows = []
        errors.append(f"验收清单读取失败: {err}")
    for number, row in enumerate(rows, 1):
        fields = ("acceptance_id", "repo", "package", "test", "log")
        if not isinstance(row, dict) or any(not isinstance(row.get(f), str) or not row[f] for f in fields):
            errors.append(f"验收项字段缺失或非法: 第{number}项")
            continue
        covered.add(row["acceptance_id"])
        repo, pkg, test, filename = (row[f] for f in ("repo", "package", "test", "log"))
        module = "github.com/kart-io/" + repo
        if repo not in REPOS or (pkg != module and not pkg.startswith(module + "/")):
            errors.append(f"仓库与Package不匹配: {repo}: {pkg}")
            continue
        if filename not in logs:
            events, log_errors = read_log(root, filename)
            errors.extend(log_errors)
            packages, tests = {}, {}
            for event in events:
                action, package, name = event["Action"], event["Package"], event.get("Test", "")
                if action == "start":
                    packages[package] = None
                elif not name and action in ("pass", "fail", "skip"):
                    packages[package] = action
                if name and action in ("run", "pass", "fail", "skip"):
                    tests[(package, name)] = action
            logs[filename] = events, packages, tests
        events, packages, tests = logs[filename]
        if tests.get((pkg, test)) != "pass":
            errors.append(f"没有完整Test pass: {row['acceptance_id']} {pkg}/{test}")
        if any(e["Action"] in ("fail", "skip") and e["Package"] == pkg and
               (e.get("Test") == test or e.get("Test", "").startswith(test + "/")) for e in events):
            errors.append(f"测试或子测试跳过/失败: {pkg}/{test}")
        if packages.get(pkg) != "pass":
            errors.append(f"所属包最后一次运行未完整通过: {pkg}: {filename}")
    errors.extend(f"缺验收映射: {name}" for name in sorted(REQUIRED - covered))
    return {"scope": "local_engineering_tests_only", "formal_dependency_gate": "not_checked",
            "mapped_rows": len(rows), "acceptance_ids": sorted(covered),
            "errors": sorted(set(errors)), "passed": not errors}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path,
                        default=Path(__file__).resolve().parents[1] / ".claude/agent-system-checks",
                        help="包含required-tests.json、JSONL日志及同名.exit的目录")
    root = parser.parse_args().root
    result = verify(root)
    try:
        (root / "acceptance-verification.json").write_text(
            json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    except OSError as err:
        result["errors"].append(f"验收报告写入失败: {err}")
        result["passed"] = False
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
