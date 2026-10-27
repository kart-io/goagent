"""Run isolated, randomized framework benchmark processes; retain every sample."""
import argparse
import datetime
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import random
import statistics
import subprocess
import sys
import time

HERE=Path(__file__).resolve().parent
ROOT=HERE.parent.parent

def percentile(values,p):
    return sorted(values)[max(0,math.ceil(len(values)*p)-1)]

def summarize(rows):
    groups={}
    for row in rows:groups.setdefault((row["framework"],row["scenario"]),[]).append(row)
    result=[]
    for (framework,scenario),runs in sorted(groups.items()):
        medians=[statistics.median(r["durations_ns"]) for r in runs]
        p95s=[percentile(r["durations_ns"],.95) for r in runs]
        first=[statistics.median(r["first_ns"]) for r in runs if r["first_ns"]]
        result.append(dict(framework=framework,scenario=scenario,repeats=len(runs),samples=sum(len(r["durations_ns"]) for r in runs),p50_ns=statistics.median(medians),p95_ns=statistics.median(p95s),p50_min_ns=min(medians),p50_max_ns=max(medians),first_p50_ns=statistics.median(first) if first else None,all_contracts_passed=True))
    return result

def command_output(cmd,env):
    return subprocess.check_output(cmd,cwd=ROOT,env=env,text=True).strip()

def benchmark_env():
    env=os.environ.copy()
    env.pop("GOROOT",None)
    env.pop("PYTHONASYNCIODEBUG",None)
    env.update(GOMAXPROCS="4",PYTHONHASHSEED="0",LANGSMITH_TRACING="false",LANGCHAIN_TRACING_V2="false",OTEL_SDK_DISABLED="true")
    return env

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument("--out",type=Path,required=True)
    parser.add_argument("--go-runner",type=Path,required=True)
    parser.add_argument("--samples",type=int,default=400)
    parser.add_argument("--warmup",type=int,default=50)
    parser.add_argument("--repeats",type=int,default=5)
    parser.add_argument("--seed",type=int,default=20261027)
    args=parser.parse_args()
    if min(args.samples,args.repeats)<1 or args.warmup<0:parser.error("invalid counts")
    out=args.out.resolve();out.mkdir(parents=True,exist_ok=False)
    env=benchmark_env()
    sources={str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in HERE.rglob("*") if p.is_file() and p.suffix in (".go",".py",".mod",".sum",".lock")}
    manifest=dict(started_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),git_head=command_output(["git","rev-parse","HEAD"],env),platform=platform.platform(),machine=platform.machine(),cpu_count=os.cpu_count(),python=platform.python_version(),go=command_output(["go","version"],env),gomaxprocs=4,asyncio_debug=False,samples=args.samples,warmup=args.warmup,repeats=args.repeats,seed=args.seed,network="local fake models; Python socket connect disabled; no credentials used",timed_scope="compiled warm execution; excludes construction/import/session prepare+cleanup; includes framework execution and model protocol objects",source_sha256=sources)
    (out/"manifest.json").write_text(json.dumps(manifest,indent=2)+"\n")
    jobs=[]
    for repeat in range(args.repeats):
        round_jobs=[]
        for framework in ("goagent","eino"):
            for scenario in ("graph_8","branch_loop_8","agent_tool","stream_64"):
                round_jobs.append((repeat,framework,scenario))
        for framework in ("langchain","openai_agents","google_adk"):
            for scenario in ("agent_tool","stream_64"):round_jobs.append((repeat,framework,scenario))
        for scenario in ("graph_8","branch_loop_8"):round_jobs.append((repeat,"langgraph",scenario))
        random.Random(args.seed+repeat).shuffle(round_jobs);jobs.extend(round_jobs)
    (out/"schedule.json").write_text(json.dumps(jobs,indent=2)+"\n")
    rows=[]
    with (out/"results.jsonl").open("w") as raw:
        for index,(repeat,framework,scenario) in enumerate(jobs):
            cmd=[str(args.go_runner.resolve())] if framework in ("goagent","eino") else [sys.executable,str(HERE/"python_runner.py")]
            cmd += ["--framework",framework,"--scenario",scenario,"--samples",str(args.samples),"--warmup",str(args.warmup)]
            started=time.monotonic()
            result=subprocess.run(cmd,cwd=HERE,env=env,text=True,capture_output=True,timeout=120)
            stem=f"{repeat}-{framework}-{scenario}"
            (out/f"{stem}.stderr.log").write_text(result.stderr)
            if result.returncode:
                failure=dict(repeat=repeat,framework=framework,scenario=scenario,exit_code=result.returncode,stderr_file=f"{stem}.stderr.log")
                (out/"failure.json").write_text(json.dumps(failure,indent=2)+"\n")
                raise RuntimeError(f"benchmark failed: {failure}")
            row=json.loads(result.stdout);row.update(repeat=repeat,wall_seconds=time.monotonic()-started)
            if framework not in ("goagent","eino") and row.get("asyncio_debug") is not False:raise ValueError("asyncio debug enabled")
            if len(row["durations_ns"])!=args.samples:raise ValueError("missing samples")
            rows.append(row);raw.write(json.dumps(row)+"\n");raw.flush()
            print(f"{index+1}/{len(jobs)} {framework}/{scenario}: p50={statistics.median(row['durations_ns'])/1000:.1f} us",flush=True)
    summary=summarize(rows)
    (out/"summary.json").write_text(json.dumps(summary,indent=2)+"\n")
    manifest["completed_utc"]=datetime.datetime.now(datetime.timezone.utc).isoformat()
    manifest["successful_jobs"]=len(rows);manifest["measured_operations"]=len(rows)*args.samples
    (out/"manifest.json").write_text(json.dumps(manifest,indent=2)+"\n")

if __name__=="__main__":main()
