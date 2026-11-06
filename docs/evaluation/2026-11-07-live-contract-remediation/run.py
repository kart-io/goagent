import datetime
import json
import os
import pathlib
import secrets
import subprocess

root = pathlib.Path.cwd()
work = root / '.claude/remaining-live-20261107'
runs = work / 'runs'
runs.mkdir(exist_ok=False)
base_env = {k: v for k, v in os.environ.items() if not k.startswith(('ANTHROPIC_', 'DEEPSEEK_', 'GEMINI_', 'MINIMAX_'))}
base_env['GO_TEST_MODE'] = 'false'
minimax_key = json.loads((pathlib.Path.home() / '.claude/env.json').read_text())['minimax_api_key']
deepseek_key = os.environ.get('DEEPSEEK_API_KEY') or os.environ.get('ANTHROPIC_AUTH_TOKEN')
assert minimax_key and deepseek_key
cases = [
    ('deepseek-history', 'deepseek', 'deepseek-flash', 'continuation', '.claude/codec-live-20261106/runs/deepseek/number/resume-call-01-request.json'),
    ('minimax-history', 'minimax-anthropic', 'MiniMax-M3.1-Flash', 'continuation', '.claude/minimax-anthropic-20261107/runs/string/run-call-02-request.json'),
    ('deepseek-number', 'deepseek', 'deepseek-flash', 'number', None),
    ('minimax-string', 'minimax-anthropic', 'MiniMax-M3.1-Flash', 'string', None),
]
results = []
halted = set()
for name, route, model, case, source in cases:
    if route in halted:
        results.append({'name': name, 'status': 'not_attempted_after_upstream_failure', 'passed': False, 'phases': []})
        continue
    directory = runs / name
    directory.mkdir()
    if source is None:
        (directory / 'scenario.json').write_text(json.dumps({'receipt': 'LIVE-CONTRACT-' + secrets.token_hex(8)}))
    env = dict(base_env)
    if route == 'deepseek':
        env['DEEPSEEK_API_KEY'] = deepseek_key
    else:
        env['MINIMAX_CN_API_KEY'] = minimax_key
    phases = []
    for phase in (['run'] if source else ['run', 'replay']):
        cmd = [str(work / 'probe'), '-live', '-route', route, '-model', model, '-case', case, '-phase', phase, '-dir', str(directory)]
        if source:
            cmd += ['-source', str(root / source)]
        with (directory / (phase + '-process.log')).open('w') as log:
            try:
                code = subprocess.run(cmd, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=150).returncode
            except subprocess.TimeoutExpired:
                code = 124
        path = directory / (phase + '-result.json')
        evidence = json.loads(path.read_text()) if path.exists() else {'error': '阶段未产生终态证据'}
        record = {'phase': phase, 'exit_code': code, 'evidence': evidence}
        phases.append(record)
        print(json.dumps({'name': name, 'phase': phase, 'exit_code': code, 'status': evidence.get('status'), 'http': evidence.get('wire_attempts'), 'error': evidence.get('error')}, ensure_ascii=False), flush=True)
        meta = [json.loads(p.read_text()) for p in directory.glob(phase + '-wire-*-meta.json')]
        if any(m.get('status', 0) >= 400 for m in meta):
            halted.add(route)
        if code != 0:
            break
    passed = len(phases) == (1 if source else 2) and all(p['exit_code'] == 0 for p in phases)
    if not source:
        passed = passed and phases[-1]['evidence'].get('effects') == 1 and not (directory / 'duplicate-attempts.jsonl').exists()
    results.append({'name': name, 'route': route, 'model': model, 'passed': passed, 'phases': phases})
    (work / 'results.json').write_text(json.dumps(results, ensure_ascii=False, indent=2) + '\n')
attempts = sum(p['evidence'].get('wire_attempts', 0) for r in results for p in r['phases'])
assert attempts <= 8
summary = {'source_revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
           'completed_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'cases': len(results), 'passed': sum(r['passed'] for r in results),
           'generation_http_attempts': attempts, 'request_cap': 8, 'automatic_retries': 0, 'gemini_requests': 0, 'global_configuration_modified': False}
(work / 'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
print(json.dumps(summary, ensure_ascii=False), flush=True)
raise SystemExit(0 if all(r['passed'] for r in results) else 1)
