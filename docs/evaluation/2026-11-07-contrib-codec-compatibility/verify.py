"""离线核对兼容修复证据，不调用外部服务。"""
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
 raw=files[item['path']].encode();assert hashlib.sha256(raw).hexdigest()==item['sha256'] and len(raw)==item['bytes']
summary=json.loads((base/'summary.json').read_text())
assert summary['before']['failed']==8 and summary['after']['failed']==0
report=json.loads(files['compatibility/summary.json'])
assert report['core_directory']=='<core-worktree>'
assert len(report['modules'])==15 and all(m['status']=='passed' for m in report['modules'])
assert all(m['directory'].startswith('<contrib-worktree>/') for m in report['modules'] if 'goagent-contrib/' in m['module'])
assert summary['after']['passed_tests']==sum(m['passed_tests'] for m in report['modules'])
assert summary['after']['skipped_tests']==sum(len(m.get('skipped_tests',[])) for m in report['modules'])
assert json.loads(files['workspace-published-core.json'])['Version']=='v0.49.2'
assert 'Replace' not in json.loads(files['workspace-published-core.json'])
assert files['all-published-core-lint.log'].count('0 issues.')==11
assert 'FAIL' not in files['core-full-test.log']
for source,text in files.items():
 if source.startswith('source/contrib/') and source.endswith('.go'):assert 'github.com/kart-io/goagent/utils/json' not in text
print(f'核对通过：15/15模块通过，原8项失败已消除；{len(files)}份证据，跳过项独立保留。')
