import json
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parent
CASES = ["devops-outage", "devops-conflicting-signals", "article-educational", "article-medical-misinformation", "reply-paraphrased-hours", "reply-unsupported-injection"]

def signature(row):
    evidence = row.get("model_review") or {}
    if row.get("error") or not evidence or not row.get("outcome"):
        return None
    return (evidence.get("decision"), row["outcome"], evidence.get("source"), row.get("approval_required"))

def agreement(rows, count):
    keys = [signature(row) for row in rows]
    return len(keys) == count and all(key is not None for key in keys) and len(set(keys)) == 1

def compare(root=ROOT):
    manifest = json.loads((root / "manifest.json").read_text())
    providers = list(manifest["models"])
    repeats = manifest["repeats"]
    entries, errors = [], []
    for run in manifest["runs"]:
        try:
            report = json.loads((root / run["results"]).read_text())
            rows = report["results"]
            if report["provider"] != run["provider"] or report["model"] != run["model"]:
                raise ValueError("provider/model mismatch")
            if Counter(row["name"] for row in rows) != Counter(CASES):
                errors.append({"run": run["results"], "error": "missing or duplicate case names"})
            for row in rows:
                entries.append(dict(row, provider=run["provider"], round=run["round"]))
        except (OSError, ValueError, KeyError, TypeError) as exc:
            errors.append({"run": run["results"], "error": str(exc)})
    summary = {"expected_runs": len(providers)*repeats, "completed_runs": len(manifest["runs"]), "expected_case_executions": len(CASES)*len(providers)*repeats, "observed_case_executions": len(entries), "passed": sum(row.get("passed", False) for row in entries), "observed_model_requests": sum(row.get("model_calls", 0) for row in entries), "source_unchanged": manifest.get("source_unchanged", False), "process_failures": [run for run in manifest["runs"] if run["exit_code"] != 0], "report_errors": errors, "providers": {}, "cases": [], "rounds": []}
    for provider in providers:
        rows = [row for row in entries if row["provider"] == provider]
        usage = [(row.get("model_review") or {}).get("usage") for row in rows]
        summary["providers"][provider] = {"model": manifest["models"][provider], "expected": repeats*len(CASES), "observed": len(rows), "passed": sum(row.get("passed", False) for row in rows), "model_requests": sum(row.get("model_calls", 0) for row in rows), "reported_total_tokens": sum(item.get("total_tokens", 0) for item in usage if item), "usage_reported_executions": sum(item is not None for item in usage), "consistent_cases_across_repeats": sum(agreement([row for row in rows if row["name"] == name], repeats) for name in CASES)}
    for name in CASES:
        rows = [row for row in entries if row["name"] == name]
        variants = Counter(signature(row) for row in rows)
        reason_values = [(row.get("model_review") or {}).get("reason") for row in rows]
        explanations = {reason for reason in reason_values if isinstance(reason, str) and reason}
        observed_reasons = sum(isinstance(reason, str) and bool(reason) for reason in reason_values)
        summary["cases"].append({"name": name, "observed": len(rows), "passed": sum(row.get("passed", False) for row in rows), "business_consistent": agreement(rows, repeats*len(providers)), "business_variants": [{"decision": key[0], "outcome": key[1], "source": key[2], "approval_required": key[3], "count": value} if key is not None else {"invalid_or_error": True, "count": value} for key,value in variants.items()], "observed_explanations": observed_reasons, "explanation_variants": len(explanations), "explanation_identical": observed_reasons==repeats*len(providers) and len(explanations)==1, "all_replays_unchanged": len(rows)==repeats*len(providers) and all(row.get("repeat_unchanged", False) for row in rows)})
    for round_number in range(1,repeats+1):
        rows = [row for row in entries if row["round"] == round_number]
        summary["rounds"].append({"round": round_number, "cross_model_consistent_cases": sum(agreement([row for row in rows if row["name"] == name], len(providers)) for name in CASES)})
    return summary

if __name__ == "__main__":
    summary = compare()
    (ROOT / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
