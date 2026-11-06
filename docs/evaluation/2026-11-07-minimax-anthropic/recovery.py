import json
import os
import pathlib
import secrets
import subprocess

root = pathlib.Path.cwd()
work = root / '.claude/minimax-anthropic-20261107'
assert json.loads((work / 'summary.json').read_text())['generation_http_attempts'] == 4
key = json.loads((pathlib.Path.home() / '.claude/env.json').read_text())['minimax_api_key']
env = {k: v for k, v in os.environ.items() if not k.startswith(('ANTHROPIC_', 'DEEPSEEK_', 'GEMINI_', 'MINIMAX_'))}
env.update(MINIMAX_CN_API_KEY=key, GO_TEST_MODE='false')

def execute(case, phase, directory, expected):
    cmd = [str(work / 'probe-direct'), '-live', '-route', 'minimax-anthropic', '-model', 'MiniMax-M3.1-Flash',
           '-case', case, '-phase', phase, '-dir', str(directory)]
    with (directory / (phase + '-process.log')).open('w') as log:
        p = subprocess.run(cmd, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=150)
    result = json.loads((directory / (phase + '-result.json')).read_text())
    row = {'phase': phase, 'exit_code': p.returncode, 'expected_exit_code': expected,
           'matched_exit': p.returncode == expected, 'evidence': result}
    print(json.dumps({'case': case, 'phase': phase, 'code': p.returncode,
                      'http': result['wire_attempts'], 'effects': result['effects'],
                      'error': result.get('error')}, ensure_ascii=False), flush=True)
    return row

# 原格式失败的完成记录仍应零请求回放，保持相同失败答案。
directory = work / 'runs/string'
assert not (directory / 'replay-result.json').exists()
replay = execute('string', 'replay', directory, 1)
assert replay['evidence']['wire_attempts'] == replay['evidence']['new_model_calls'] == 0
assert replay['evidence']['effects'] == 1
assert replay['evidence']['answer'] == json.loads((directory / 'run-result.json').read_text())['answer']

directory = work / 'runs/direct-string'
directory.mkdir(exist_ok=False)
(directory / 'scenario.json').write_text(json.dumps({'receipt': 'LIVE-MINIMAX-DIRECT-' + secrets.token_hex(8)}))
phases = [execute('direct-string', 'crash', directory, 86)]
if phases[-1]['matched_exit']:
    phases.append(execute('direct-string', 'resume', directory, 0))
    if phases[-1]['matched_exit']:
        phases.append(execute('direct-string', 'replay', directory, 0))
extra = {'original_string_zero_request_replay': replay,
         'direct_recovery': {'passed': len(phases) == 3 and all(p['matched_exit'] for p in phases), 'phases': phases},
         'cumulative_generation_http_attempts': 4 + sum(p['evidence']['wire_attempts'] for p in phases)}
assert extra['cumulative_generation_http_attempts'] <= 5
(work / 'recovery.json').write_text(json.dumps(extra, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'direct_recovery_passed': extra['direct_recovery']['passed'],
                  'cumulative_generation_http_attempts': extra['cumulative_generation_http_attempts']}, ensure_ascii=False), flush=True)
