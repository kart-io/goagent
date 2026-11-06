"""离线核对真实记录与摘要，不调用模型或读取凭据。"""
import decimal
import gzip
import hashlib
import json
import pathlib

base = pathlib.Path(__file__).resolve().parent
packed = (base / 'raw-evidence.json.gz').read_bytes()
manifest = json.loads((base / 'manifest.json').read_text())
assert hashlib.sha256(packed).hexdigest() == manifest['archive_sha256']
files = json.loads(gzip.decompress(packed))
for item in manifest['files']:
    assert hashlib.sha256(files[item['path']].encode()).hexdigest() == item['sanitized_sha256']
assert len(files) == len(manifest['files'])
D = decimal.Decimal

def load(name):
    return json.loads(files[name], parse_float=D)

results = load('results.json')
assert len(results) == 4 and all(r['passed'] for r in results)
summary = load('summary.json')
assert summary == json.loads((base / 'summary.json').read_text(), parse_float=D)
assert summary['gemini_requests'] == 0 and summary['automatic_retries'] == 0
assert summary['generation_http_attempts'] == 7 <= summary['request_cap'] == 8
metas = [load(p) for p in files if p.startswith('runs/') and '-wire-' in p and p.endswith('-meta.json')]
assert len(metas) == 7 and all(m['status'] == 200 and m['method'] == 'POST' for m in metas)
assert {m['endpoint'] for m in metas} == {'https://api.minimax.cn/anthropic/v1/messages', 'https://api.deepseek.com/chat/completions'}
usage = [load(p)['chunk']['usage'] for p in files if p.startswith('runs/') and '-call-' in p and p.endswith('-result.json')]
assert len(usage) == 7 and sum(u['total_tokens'] for u in usage) == 4582
for name, route, prefix in [('deepseek-history', 'deepseek', 'resume-call-01'), ('minimax-history', 'minimax', 'run-call-02')]:
    path = 'runs/' + name + '/'
    original = load('historical/' + route + '/' + prefix + '-request.json')['messages']
    bad = load('historical/' + route + '/' + prefix + '-result.json')['chunk']
    request = load(path + 'run-call-01-request.json')['messages']
    result = load(path + 'run-result.json')
    receipt = load('historical/' + route + '/scenario.json')['receipt']
    assert request[:-2] == original
    assert request[-2]['content'] == bad['content'] and bad['content'].strip() != receipt
    for key in ['provider_state', 'reasoning_content', 'tool_calls']:
        assert request[-2].get(key) == bad.get(key)
    assert request[-1]['content'] == result['correction']
    assert result['terminal']['content'].strip() == receipt and not result['terminal'].get('tool_calls')
    assert result['wire_attempts'] == 1 and result['effects'] == 0
for name, strings_only, calls in [('deepseek-number', False, 3), ('minimax-string', True, 2)]:
    path = 'runs/' + name + '/'
    run, replay = load(path + 'run-result.json'), load(path + 'replay-result.json')
    receipt = load(path + 'scenario.json')['receipt']
    assert run['answer'].strip() == replay['answer'].strip() == receipt
    assert run['wire_attempts'] == run['new_model_calls'] == calls
    assert replay['wire_attempts'] == replay['new_model_calls'] == 0
    assert run['effects'] == replay['effects'] == 1
    assert run['budget'] == replay['budget']
    assert run['budget']['ReservedRequests'] == calls and run['budget']['ReservedToolCalls'] == 1
    assert run['metadata'].get('stopping_continued', 0) == (1 if calls == 3 else 0)
    assert not run['metadata'].get('turn_stopping_skipped')
    assert path + 'duplicate-attempts.jsonl' not in files
    effects = [json.loads(line, parse_float=D) for line in files[path + 'effects.jsonl'].splitlines()]
    assert len(effects) == 1
    want = {'id': '9007199254740993' if strings_only else 9007199254740993,
            'rate': '0.1234567890123456789' if strings_only else D('0.1234567890123456789'), 'label': '<测试&>'}
    args = json.loads(load(path + 'run-call-01-result.json')['chunk']['tool_calls'][0]['function']['arguments'], parse_float=D)
    assert args == effects[0] == want
    fragments = []
    for line in files[path + 'run-wire-01-response.txt'].splitlines():
        if not line.startswith('data: ') or line[6:] == '[DONE]':
            continue
        data = json.loads(line[6:])
        if strings_only:
            delta = data.get('delta', {})
            if delta.get('type') == 'input_json_delta':
                fragments.append(delta['partial_json'])
        else:
            for choice in data.get('choices', []):
                for call in choice.get('delta', {}).get('tool_calls', []):
                    fragments.append(call.get('function', {}).get('arguments', ''))
    assert json.loads(''.join(fragments), parse_float=D) == want
    wire_request = load(path + 'run-wire-01-request.json')
    tool = wire_request['tools'][0]
    schema = tool['input_schema'] if strings_only else tool['function']['parameters']
    assert schema['properties']['id']['type'] == ('string' if strings_only else 'integer')
    assert schema['properties']['rate']['type'] == ('string' if strings_only else 'number')
    assert receipt not in load(path + 'run-call-01-request.json')['messages'][-1]['content']
# 每一轮 MiniMax 的 assistant 原生内容必须原样送回，包括思考和签名。
for path in files:
    if path.startswith('runs/minimax') and '-call-' in path and path.endswith('-request.json'):
        request = load(path)
        wire = load(path.replace('-call-', '-wire-'))
        native = [m['provider_state']['data']['content'] for m in request['messages'] if 'provider_state' in m]
        sent = [m['content'] for m in wire['messages'] if m['role'] == 'assistant']
        assert native == sent
        assert wire['stream'] is True
# 原始数值模式仍是失败负对照；不能用新字符串通过覆盖旧失败。
fragments = []
for line in files['historical/minimax/number-wire-response.txt'].splitlines():
    if line.startswith('data: '):
        data = json.loads(line[6:])
        if data.get('delta', {}).get('type') == 'input_json_delta':
            fragments.append(data['delta']['partial_json'])
original_args = json.loads(''.join(fragments), parse_float=D)
assert original_args['rate'] == D('0.12345678901234568') != D('0.1234567890123456789')
print('核对通过：4 项实测、7 次 HTTP 200、4582 Token、原精度与回执保持、零请求重放、历史数值失败保留。')
