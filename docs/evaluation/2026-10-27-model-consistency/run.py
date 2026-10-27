import hashlib, json, os, subprocess, time
from pathlib import Path
from datetime import datetime, timezone
root = Path(__file__).resolve().parent
providers = [("gemini", "gemini-2.5-flash"), ("deepseek", "deepseek-flash"), ("minimax", "MiniMax-M3.1-Flash")]
files = sorted(Path("examples/workflow/business-cases").glob("*.go")) + [Path("scripts/business_cases_live.sh")]
def hashes():
    return {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
initial_hashes = hashes()
manifest = {"started_at": datetime.now(timezone.utc).isoformat(), "repeats": 3, "maximum_model_requests": 54, "models": dict(providers), "source_sha256": initial_hashes, "runs": []}
(root / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
for round_number in range(1, 4):
    for provider, model in providers:
        name = f"round-{round_number}-{provider}"
        output = root / (name + ".json")
        log = root / (name + ".log")
        env = os.environ.copy()
        env[provider.upper() + "_MODEL"] = model
        print(f"Starting {name}: {model}", flush=True)
        start = time.monotonic()
        with log.open("w") as handle:
            try:
                result = subprocess.run(["bash", "scripts/business_cases_live.sh", provider, str(output)], env=env, stdout=handle, stderr=subprocess.STDOUT, timeout=750)
                code = result.returncode
            except subprocess.TimeoutExpired:
                code = 124
        entry = {"round": round_number, "provider": provider, "model": model, "exit_code": code, "duration_seconds": round(time.monotonic()-start, 3), "results": output.name, "log": log.name}
        manifest["runs"].append(entry)
        manifest["source_unchanged"] = hashes() == initial_hashes
        (root / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        print(f"Finished {name}: exit={code}", flush=True)
manifest["finished_at"] = datetime.now(timezone.utc).isoformat()
(root / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
print("All nine runs finished; failures retained without retries.", flush=True)
