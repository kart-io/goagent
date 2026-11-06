import json
import os
import pathlib
import secrets
import subprocess

root = pathlib.Path.cwd()
work = root / '.claude/codec-live-20261106'
initial = json.loads((work / 'summary.json').read_text())
assert initial['generation_http_attempts'] == 13
env = os.environ.copy()
env['GO_TEST_MODE'] = 'false'
phases = []

def execute(directory, model, phase, expected):
    cmd = [str(work / 'probe'), '-live', '-route', 'deepseek', '-model', model,
           '-case', 'number', '-phase', phase, '-dir', str(directory)]
    with (directory / (phase + '-process.log')).open('w') as log:
        p = subprocess.run(cmd, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=150)
    out = json.loads((directory / (phase + '-result.json')).read_text())
    row = {'phase': phase, 'exit_code': p.returncode, 'expected_exit_code': expected,
           'matched_exit': p.returncode == expected, 'evidence': out}
    print(json.dumps({'directory': directory.parent.name, 'phase': phase,
                      'exit_code': p.returncode, 'http': out['wire_attempts'],
                      'error': out.get('error')}, ensure_ascii=False), flush=True)
    return row

# 即使答案格式不合格，仍检查已完成日志的零请求回放；保留同一个格式失败。
d = work / 'runs/deepseek/number'
assert not (d / 'replay-result.json').exists()
replay = execute(d, 'deepseek-flash', 'replay', 1)
assert replay['evidence']['new_model_calls'] == 0
assert replay['evidence']['wire_attempts'] == 0
assert replay['evidence']['effects'] == 1
assert replay['evidence']['answer'] == json.loads((d / 'resume-result.json').read_text())['answer']

# 预算内增加同模型、不同协议的数值工具对照，不重试首次 Flash 用例。
d = work / 'runs/deepseek-v4-native/number'
d.mkdir(parents=True, exist_ok=False)
(d / 'scenario.json').write_text(json.dumps({'receipt': 'LIVE-CODEC-' + secrets.token_hex(8)}))
first = execute(d, 'deepseek-v4-pro', 'crash', 86)
phases.append(first)
if first['matched_exit']:
    second = execute(d, 'deepseek-v4-pro', 'resume', 0)
    phases.append(second)
    if second['matched_exit']:
        phases.append(execute(d, 'deepseek-v4-pro', 'replay', 0))

extra = {'flash_numeric_replay': replay,
         'same_model_protocol_control': {'route': 'deepseek-v4-native', 'probe_route': 'deepseek',
                                         'model': 'deepseek-v4-pro', 'case': 'number',
                                         'passed': len(phases) == 3 and all(p['matched_exit'] for p in phases),
                                         'phases': phases}}
total = initial['generation_http_attempts'] + sum(p['evidence']['wire_attempts'] for p in phases)
assert total <= 15
extra['cumulative_generation_http_attempts'] = total
(work / 'extra-results.json').write_text(json.dumps(extra, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'cumulative_generation_http_attempts': total,
                  'protocol_control_passed': extra['same_model_protocol_control']['passed'],
                  'flash_zero_request_replay': True}, ensure_ascii=False), flush=True)
