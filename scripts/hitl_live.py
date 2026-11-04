#!/usr/bin/env python3
"""Bounded real-model HITL validation; each phase starts an independent process."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time


def check(condition, message):
    if not condition:
        raise AssertionError(message)


def redact(text):
    for name in ("GEMINI_API_KEY", "DEEPSEEK_API_KEY"):
        value = os.environ.get(name)
        if value:
            text = text.replace(value, "[REDACTED]")
    return text


def review_results(directory):
    """Re-score saved synthetic notices without making model calls."""
    results = json.loads((directory / "results.json").read_text())
    reviews = []
    for item in results["providers"]:
        execution = item.get("execution_passed", item.get("passed", False))
        notice = item.get("final_notice", {}).get("notice", "")
        # Fixed-fixture content check, not a general natural-language verifier.
        forbidden = ("已完成退款", "退款已完成", "已退款", "已退还", "已支付", "已打款", "已到账", "退款成功", "已退回")
        claims = [phrase for phrase in forbidden if phrase in notice]
        reviews.append({"provider": item["provider"], "model": item["model"], "execution_passed": execution,
                        "content_passed": bool(notice) and not claims,
                        "unsupported_payment_claims": claims, "notice": notice})
    review = {"basis": "saved results only; no new model calls; payment was never executed",
              "providers": reviews, "passed": bool(reviews) and all(r["execution_passed"] and r["content_passed"] for r in reviews)}
    (directory / "review.json").write_text(json.dumps(review, ensure_ascii=False, indent=2) + "\n")
    return review


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true")
    parser.add_argument("--out", type=Path)
    parser.add_argument("--review", type=Path, help="review an existing results directory without model calls")
    parser.add_argument("--providers", nargs="+", choices=("gemini", "deepseek"), default=["gemini", "deepseek"])
    args = parser.parse_args()
    if args.review:
        if args.live or args.out:
            parser.error("--review cannot be combined with --live or --out")
        review = review_results(args.review)
        print(json.dumps(review, ensure_ascii=False, indent=2))
        return 0 if review["passed"] else 1
    if not args.live:
        parser.error("--live is required; this sends paid model requests")
    if not args.out:
        parser.error("--out is required for a new live run")
    if len(set(args.providers)) != len(args.providers):
        parser.error("providers must be unique")
    repo = Path(__file__).resolve().parent.parent
    args.out = args.out.resolve()
    args.out.mkdir(parents=True, exist_ok=False)
    results = {"started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "max_model_calls": 3 * len(args.providers),
               "max_output_tokens_per_call": 512, "automatic_retries": 0, "providers": [], "phases": []}
    defaults = {"gemini": "gemini-2.5-flash", "deepseek": "deepseek-flash"}

    def save():
        (args.out / "results.json").write_text(json.dumps(results, ensure_ascii=False, indent=2) + "\n")

    save()
    with tempfile.TemporaryDirectory(prefix="goagent-hitl-live-") as temp:
        binary = str(Path(temp) / "hitl")
        compiled = subprocess.run(["go", "build", "-o", binary, "./evaluation/hitlbench/cmd/run"], cwd=repo, capture_output=True, text=True)
        (args.out / "build.log").write_text(redact(compiled.stdout + compiled.stderr))
        check(compiled.returncode == 0, "probe build failed; see build.log")
        for provider in args.providers:
            model = os.environ.get(provider.upper() + "_MODEL") or defaults[provider]
            case = {"provider": provider, "model": model, "execution_passed": False}
            results["providers"].append(case)
            run_dir = args.out / provider
            run_dir.mkdir()

            def phase(label, scenario="approve", expected_code=0, calls=0, **flags):
                cmd = [binary, "-live", "-provider", provider, "-model", model, "-dir", str(run_dir / "checkpoints"),
                       "-run", provider + "/" + scenario]
                for key, value in flags.items():
                    cmd.append("-" + key.replace("_", "-"))
                    if value is not True:
                        cmd.append(str(value))
                try:
                    completed = subprocess.run(cmd, cwd=repo, capture_output=True, text=True, timeout=100)
                    stdout, stderr = redact(completed.stdout), redact(completed.stderr)
                    code = completed.returncode
                except subprocess.TimeoutExpired as exc:
                    def text(value):
                        return value.decode(errors="replace") if isinstance(value, bytes) else (value or "")
                    stdout, stderr = redact(text(exc.stdout)), redact(text(exc.stderr))
                    code = -1
                (run_dir / (label + ".stdout.log")).write_text(stdout)
                (run_dir / (label + ".stderr.log")).write_text(stderr)
                entry = {"provider": provider, "scenario": scenario, "phase": label, "exit_code": code}
                results["phases"].append(entry)
                save()  # retain failed output before assertions or JSON parsing
                check(code == expected_code, f"{label}: exit {code}, expected {expected_code}")
                if expected_code == 86:
                    entry["intentional_crash"] = True
                    save()
                    return None
                out = json.loads(stdout)
                entry["result"] = out
                save()
                check(out["new_model_calls"] == calls, f"{label}: unexpected model calls {out['new_model_calls']}")
                print(f"{provider} {label}: calls={out['new_model_calls']} reserved={out['budget']['ReservedRequests']}", flush=True)
                return out

            try:
                check(bool(os.environ.get(provider.upper() + "_API_KEY")), "provider credential is missing")
                waiting = phase("01-draft", calls=1)
                snap = waiting["snapshot"]
                check(snap["Waiting"] and not snap["Done"], "draft must pause for review")
                check("88" in snap["State"]["draft"] and "DEMO-HITL-01" in snap["State"]["draft"], "model draft lost source facts")
                check(waiting["budget"]["ReservedRequests"] == 1, "draft budget must be one")
                key = snap["Key"]
                for label, flags in [
                    ("02-wait-replay", {}),
                    ("03-status", {"phase": "status"}),
                    ("04-wrong-key", {"phase": "resume", "key": "forged"}),
                    ("05-wrong-actor", {"phase": "resume", "key": key, "actor": "attacker"}),
                    ("06-invalid-input", {"phase": "resume", "key": key, "amount": -1}),
                ]:
                    denied = label.startswith(("04", "05", "06"))
                    checked = phase(label, expected_code=1 if denied else 0, **flags)
                    check(checked["snapshot"] == snap, f"{label}: waiting checkpoint changed")
                    check(checked["budget"] == waiting["budget"], f"{label}: budget changed")
                phase("07-accept-and-crash", expected_code=86, phase="resume", key=key, amount=66, crash_after_accept=True)
                accepted = phase("08-status-after-crash", phase="status")
                check(not accepted["snapshot"]["Waiting"] and not accepted["snapshot"]["Done"], "accepted checkpoint not persisted")
                check(accepted["snapshot"]["State"]["approved_amount"] == 66, "human edit lost after exit")
                check(accepted["budget"] == waiting["budget"], "crash before model must not spend budget")
                done = phase("09-recover-and-generate", calls=1)
                check(done["snapshot"]["Done"] and done["snapshot"]["State"]["status"] == "accepted", "recovery did not finish")
                final = json.loads(done["snapshot"]["State"]["final"])
                check(final["amount_yuan"] == 66 and final["order_id"] == "DEMO-HITL-01", "model ignored human amendment")
                check("66" in final["notice"] and "88" not in final["notice"] and "DEMO-HITL-01" in final["notice"], "final notice contains stale or missing facts")
                check(done["budget"]["ReservedRequests"] == 2, "model requests must total two")
                replay = phase("10-completed-replay")
                check(replay["snapshot"] == done["snapshot"] and replay["budget"] == done["budget"], "completion replay changed state or usage")
                phase("11-stale-response", phase="resume", key=key, expected_code=1)
                reject_wait = phase("12-reject-draft", scenario="reject", calls=1)
                reject_key = reject_wait["snapshot"]["Key"]
                rejected = phase("13-human-reject", scenario="reject", phase="resume", key=reject_key, decision="reject")
                check(rejected["snapshot"]["Done"] and rejected["snapshot"]["State"]["status"] == "rejected", "human rejection did not finish")
                check(not rejected["snapshot"]["State"].get("final"), "rejected run generated a final notice")
                check(rejected["budget"] == reject_wait["budget"], "human rejection made a model call")
                reject_replay = phase("14-rejected-replay", scenario="reject")
                check(reject_replay["snapshot"] == rejected["snapshot"] and reject_replay["budget"] == rejected["budget"], "rejection replay changed state or usage")
                case.update(execution_passed=True, approved_budget=done["budget"], rejected_budget=rejected["budget"], final_notice=final)
            except Exception as exc:
                case["error"] = redact(str(exc))
                print(f"{provider} FAILED: {case['error']}", flush=True)
            save()
    results["finished_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    results["execution_passed"] = all(p["execution_passed"] for p in results["providers"])
    save()
    review = review_results(args.out)
    checksums = {str(p.relative_to(args.out)): hashlib.sha256(p.read_bytes()).hexdigest()
                 for p in sorted(args.out.rglob("*")) if p.is_file()}
    (args.out / "checksums.json").write_text(json.dumps(checksums, indent=2) + "\n")
    return 0 if review["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
