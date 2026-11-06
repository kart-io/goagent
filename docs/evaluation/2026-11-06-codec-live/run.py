import datetime
import json
import os
import pathlib
import secrets
import subprocess
import sys

root = pathlib.Path.cwd()
work = root / '.claude/codec-live-20261106'
runs = work / 'runs'
runs.mkdir(exist_ok=False)
env = os.environ.copy()
env.pop('GOROOT', None)
env['GO_TEST_MODE'] = 'false'
results = []

def execute(route, model, case, phase, directory, expected):
    cmd = [str(work / 'probe'), '-live', '-route', route, '-model', model,
           '-case', case, '-phase', phase, '-dir', str(directory)]
    with (directory / (phase + '-process.log')).open('w') as log:
        try:
            p = subprocess.run(cmd, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=150)
            code = p.returncode
        except subprocess.TimeoutExpired:
            code = 124
    path = directory / (phase + '-result.json')
    evidence = json.loads(path.read_text()) if path.exists() else {'error': '探针未产出阶段结果'}
    row = {'phase': phase, 'exit_code': code, 'expected_exit_code': expected,
           'matched_exit': code == expected, 'evidence': evidence}
    print(json.dumps({'route': route, 'case': case, 'phase': phase, 'exit_code': code,
                      'status': evidence.get('status'), 'http': evidence.get('wire_attempts'),
                      'error': evidence.get('error')}, ensure_ascii=False), flush=True)
    return row

for route, model in [('deepseek', 'deepseek-flash'), ('gemini', 'gemini-2.5-flash'),
                     ('anthropic-deepseek', 'deepseek-v4-pro')]:
    for case in ['text', 'number', 'string']:
        directory = runs / route / case
        directory.mkdir(parents=True)
        (directory / 'scenario.json').write_text(json.dumps({'receipt': 'LIVE-CODEC-' + secrets.token_hex(8)}))
        phases = []
        if case == 'number':
            p = execute(route, model, case, 'crash', directory, 86)
            phases.append(p)
            if p['matched_exit'] and p['evidence'].get('effects') == 1:
                p = execute(route, model, case, 'resume', directory, 0)
                phases.append(p)
                if p['matched_exit']:
                    phases.append(execute(route, model, case, 'replay', directory, 0))
        else:
            p = execute(route, model, case, 'run', directory, 0)
            phases.append(p)
            if case == 'string' and p['matched_exit']:
                phases.append(execute(route, model, case, 'replay', directory, 0))
        expected_phases = 3 if case == 'number' else 2 if case == 'string' else 1
        passed = len(phases) == expected_phases and all(p['matched_exit'] for p in phases)
        if case != 'text' and passed:
            passed = phases[-1]['evidence'].get('effects') == 1
            passed = passed and phases[-1]['evidence'].get('wire_attempts') == 0
            passed = passed and not (directory / 'duplicate-attempts.jsonl').exists()
        results.append({'route': route, 'model': model, 'case': case, 'passed': passed, 'phases': phases})
        (work / 'results.json').write_text(json.dumps({'started_source': '0e9e54d8', 'cases': results}, ensure_ascii=False, indent=2) + '\n')

summary = {'source_revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
           'completed_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
           'cases': len(results), 'passed': sum(r['passed'] for r in results),
           'failed': sum(not r['passed'] for r in results),
           'generation_http_attempts': sum(p['evidence'].get('wire_attempts', 0) for r in results for p in r['phases']),
           'metadata_requests': 2, 'generation_request_cap': 15,
           'note': 'Anthropic 路由为真实 DeepSeek 兼容服务，不是 Claude；没有自动重试。'}
assert summary['generation_http_attempts'] <= summary['generation_request_cap']
(work / 'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
print(json.dumps(summary, ensure_ascii=False), flush=True)
sys.exit(1 if summary['failed'] else 0)
