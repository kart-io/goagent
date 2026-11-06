"""核对归档完整性及本轮结果，不调用模型、网络或写入存储。"""
import gzip
import hashlib
import json
import pathlib

base=pathlib.Path(__file__).resolve().parent
packed=(base/'evidence.json.gz').read_bytes()
manifest=json.loads((base/'manifest.json').read_text())
assert hashlib.sha256(packed).hexdigest()==manifest['archive_sha256']
files=json.loads(gzip.decompress(packed))
assert len(files)==len(manifest['files'])
for item in manifest['files']:
 raw=files[item['path']].encode()
 assert hashlib.sha256(raw).hexdigest()==item['sha256'] and len(raw)==item['bytes']
summary=json.loads((base/'summary.json').read_text())
assert summary['real_model_requests']==0 and summary['full_race']=='passed'
assert summary['lint_findings']==summary['import_findings']==0
assert 'FAIL' not in files['full-final.log'] and '\nok ' in files['full-final.log']
assert '0 issues.' in files['checks-final.log']
assert '整体覆盖率: 80.0%' in files['coverage-gates-final.log']
assert 'imports: 0 findings' in files['apicheck.log']
compat=json.loads(files['compatibility-final/summary.json'])
assert compat['core_directory']=='<worktree>'
assert len(compat['modules'])==15
assert sum(m['status']=='passed' for m in compat['modules'])==7
for module in compat['modules']:
 if module['status']=='failed':
  assert module['passed_tests']==0
  assert 'utils/json' in files['compatibility-final/'+module['log']]
bench=json.loads((base/'benchmarks.json').read_text())
assert len(bench)==summary['benchmark_cases'] and all(len(r['samples'])==3 for r in bench)
assert all('ns_per_op' in row['samples'][0] for row in bench)
print(f'核对通过：{len(files)}份证据、{len(bench)}组基准；核心检查通过，跨仓7通过/8既有旧导入失败如实保留。')
