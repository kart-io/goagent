"""通过真实子进程检查验收工具的失败关闭行为。"""

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("verify_agent_acceptance.py")
PACKAGE = "github.com/kart-io/goagent/memory"


class AcceptanceVerifierTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.script = self.root / "verifier.py"
        self.script.write_bytes(SCRIPT.read_bytes())
        ids = ([f"U{i:02}" for i in range(14)] +
               [f"A{i:02}" for i in range(1, 24)] +
               [f"I{i:02}" for i in range(1, 8)])
        self.rows = [dict(acceptance_id=i, repo="goagent", package=PACKAGE,
                          test="TestPassed", log="sample.jsonl") for i in ids]
        self.events = [dict(Action="start", Package=PACKAGE),
                       dict(Action="run", Package=PACKAGE, Test="TestPassed"),
                       dict(Action="pass", Package=PACKAGE, Test="TestPassed"),
                       dict(Action="pass", Package=PACKAGE)]
        (self.root / "sample.exit").write_text("0\n", encoding="utf-8")

    def check(self, passed, raw=None):
        (self.root / "required-tests.json").write_text(json.dumps(self.rows))
        (self.root / "sample.jsonl").write_text(
            "".join(json.dumps(e) + "\n" for e in self.events) if raw is None else raw)
        run = subprocess.run([sys.executable, str(self.script), "--root", str(self.root)],
                             capture_output=True, text=True, timeout=10)
        report = json.loads((self.root / "acceptance-verification.json").read_text())
        self.assertEqual(report["passed"], passed, run.stdout + run.stderr)
        self.assertEqual(run.returncode, 0 if passed else 1, run.stdout + run.stderr)
        return report

    def test_complete_green_log_and_zero_exit_pass(self):
        self.check(True)

    def test_nonzero_exit_fails_even_with_green_log(self):
        (self.root / "sample.exit").write_text("7\n")
        report = self.check(False)
        self.assertTrue(any("退出" in e for e in report["errors"]))

    def test_missing_exit_fails(self):
        (self.root / "sample.exit").unlink()
        self.check(False)

    def test_malformed_exit_fails(self):
        (self.root / "sample.exit").write_text("未知\n")
        self.check(False)

    def test_descendant_skip_fails(self):
        self.events.insert(2, dict(Action="skip", Package=PACKAGE, Test="TestPassed/child"))
        self.check(False)

    def test_test_fail_cannot_be_hidden_by_package_pass(self):
        self.events.insert(2, dict(Action="fail", Package=PACKAGE, Test="TestOther"))
        self.check(False)

    def test_missing_exact_test_pass_fails(self):
        self.events[2]["Test"] = "TestPassed/child"
        self.check(False)

    def test_incomplete_last_package_run_fails(self):
        self.events.append(dict(Action="start", Package=PACKAGE))
        self.events.append(dict(Action="run", Package=PACKAGE, Test="TestPassed"))
        self.check(False)

    def test_repository_and_package_must_match(self):
        self.rows[0]["repo"] = "goagent-harness"
        self.check(False)

    def test_invalid_event_fails(self):
        self.events.append(dict(Action="unknown", Package=PACKAGE))
        self.check(False)

    def test_invalid_action_type_fails(self):
        self.events.append(dict(Action=[], Package=PACKAGE))
        self.check(False)

    def test_malformed_json_fails(self):
        self.check(False, "不是JSON\n")

    def test_missing_acceptance_mapping_fails(self):
        self.rows.pop()
        self.check(False)


if __name__ == "__main__":
    unittest.main()
