import datetime
import json
import os
import pathlib
import secrets
import subprocess
import sys

root = pathlib.Path.cwd()
work = root / '.claude/minimax-anthropic-20261107'
runs = work / 'runs'
runs.mkdir(exist_ok=False)
config = json.loads((pathlib.Path.home() / '.claude/env.json').read_text())
key = config.get('minimax_api_key')
if not isinstance(key, str) or not key:
    raise SystemExit('缺少既有 MiniMax 凭据')
env = {k: v for k, v in os.environ.items() if not k.startswith(('ANTHROPIC_', 'DEEPSEEK_', 'GEMINI_', 'MINIMAX_'))}
env.update(MINIMAX_CN_API_KEY=key, GO_TEST_MODE='false')
results = []
halt = False

def execute(case, phase, directory, expected):
    cmd = [str(work / 'probe'), '-live', '-route', 'minimax-anthropic',
           '-model', 'MiniMax-M3.1-Flash', '-case', case, '-phase', phase, '-dir', str(directory)]
    with (directory / (phase + '-process.log')).open('w') as log:
        try:
            code = subprocess.run(cmd, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=150).returncode
        except subprocess.TimeoutExpired:
            code = 124
    path = directory / (phase + '-result.json')
    evidence = json.loads(path.read_text()) if path.exists() else {'error': '阶段未生成结果'}
    print(json.dumps({'case': case, 'phase': phase, 'exit_code': code,
                      'status': evidence.get('status'), 'http': evidence.get('wire_attempts'),
                      'error': evidence.get('error')}, ensure_ascii=False), flush=True)
    global halt
    meta = [json.loads(p.read_text()) for p in directory.glob(phase + '-wire-*-meta.json')]
    if any(e.get('status', 0) >= 400 for e in meta) or 'max_tokens' in evidence.get('error', ''):
        halt = True
    return {'phase': phase, 'exit_code': code, 'expected_exit_code': expected,
            'matched_exit': code == expected, 'evidence': evidence}

for case in ['text', 'number', 'string']:
    if halt:
        results.append({'case': case, 'passed': False, 'status': 'not_attempted_after_upstream_failure', 'phases': []})
        continue
    directory = runs / case
    directory.mkdir()
    (directory / 'scenario.json').write_text(json.dumps({'receipt': 'LIVE-MINIMAX-ANTHROPIC-' + secrets.token_hex(8)}))
    phases = []
    if case == 'number':
        first = execute(case, 'crash', directory, 86)
        phases.append(first)
        if first['matched_exit']:
            second = execute(case, 'resume', directory, 0)
            phases.append(second)
            if second['matched_exit']:
                phases.append(execute(case, 'replay', directory, 0))
    else:
        first = execute(case, 'run', directory, 0)
        phases.append(first)
        if case == 'string' and first['matched_exit']:
            phases.append(execute(case, 'replay', directory, 0))
    expected_phases = 3 if case == 'number' else 2 if case == 'string' else 1
    passed = len(phases) == expected_phases and all(p['matched_exit'] for p in phases)
    if passed and case != 'text':
        passed = phases[-1]['evidence'].get('wire_attempts') == 0 and phases[-1]['evidence'].get('effects') == 1
        passed = passed and not (directory / 'duplicate-attempts.jsonl').exists()
    results.append({'case': case, 'passed': passed, 'status': 'passed' if passed else 'failed', 'phases': phases})
    (work / 'results.json').write_text(json.dumps(results, ensure_ascii=False, indent=2) + '\n')

attempts = sum(p['evidence'].get('wire_attempts', 0) for r in results for p in r['phases'])
assert attempts <= 5
summary = {'source_revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
           'protocol': 'Anthropic Messages SSE', 'base_url': 'https://api.minimax.cn/anthropic',
           'model': 'MiniMax-M3.1-Flash', 'completed_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
           'credential_source': '~/.claude/env.json:minimax_api_key；仅映射至子进程环境',
           'cases': len(results), 'passed': sum(r['passed'] for r in results),
           'generation_http_attempts': attempts, 'request_cap': 5,
           'global_configuration_modified': False, 'automatic_retries': 0}
(work / 'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
print(json.dumps(summary, ensure_ascii=False), flush=True)
sys.exit(0 if all(r['passed'] for r in results) else 1)
