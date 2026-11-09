import json, os, pathlib, subprocess, time

root = pathlib.Path(__file__).parent
env = dict(os.environ)
env.pop('GOROOT', None)
env['GO_TEST_MODE'] = 'true'
contracts = {
 'github.com/kart-io/goagent-contrib/durable/pgstore': ['TestConformance', 'TestProcessCrashTakeover', 'TestPrimaryRestart', 'TestSyncCommit'],
 'github.com/kart-io/goagent-contrib/management': ['integration/TestHostRegisterPublishSessionTaskApprovalResume', 'integration/TestApprovalSurvivesProcessKillAndRestart', 'integration/TestForkCrashAfterLogCopy'],
 'github.com/kart-io/goagent-harness/integration/postgres': ['TestWaitRestartResumeRelease', 'TestTakeoverFencesOldHolder', 'TestInboxAppendOnceAcrossConnections'],
}
def event(action, pkg, test=''):
    return json.dumps(dict(Action=action, Package=pkg, Test=test)) + '\n'

for mode in ['valid', 'top_skip', 'top_fail_then_pass', 'child_skip', 'child_fail', 'package_fail', 'truncated_tail', 'missing_package_end', 'foreign_json']:
    directory = root / mode
    directory.mkdir(exist_ok=True)
    modules = []
    for i, (module, names) in enumerate(contracts.items()):
        log = ''
        packages = set()
        for name in names:
            pkg, test = (module + '/' + name.rsplit('/', 1)[0], name.rsplit('/', 1)[1]) if '/' in name else (module, name)
            packages.add(pkg)
            log += event('start', pkg) + event('run', pkg, test)
            if mode in ['child_skip', 'child_fail'] and test == 'TestConformance':
                log += event('run', pkg, test + '/durability')
                log += event(mode.split('_')[1], pkg, test + '/durability')
            if mode == 'top_fail_then_pass':
                log += event('fail', pkg, test)
            log += event('skip' if mode == 'top_skip' else 'pass', pkg, test)
        if mode not in ['truncated_tail', 'missing_package_end']:
            for pkg in packages:
                log += event('fail' if mode == 'package_fail' else 'pass', pkg)
        if mode == 'truncated_tail':
            log += '{"Action":"fail","Package":'
        if mode == 'foreign_json':
            log += event('fail', 'example.com/another-run', 'TestFailure')
        name = f'{i}.jsonl'
        (directory / name).write_text(log)
        modules.append(dict(module=module, status='passed', log=name))
    (directory / 'summary.json').write_text(json.dumps(dict(modules=modules)))
    result = subprocess.run([str(root / 'compatcheck'), '-verify-postgres-report', str(directory)], env=env, capture_output=True, text=True, timeout=10)
    print(mode, 'exit=', result.returncode, (result.stdout + result.stderr).strip())

# 覆盖率探针：关键门禁全部达标，但生产总体为 74%。辅助包会影响总体而非独立包门禁。
files = ['core/durable/a.go', 'core/budget/a.go', 'workflow/a.go', 'builder/a.go', 'multiagent/a.go', 'llm/providers/openai.go', 'llm/providers/gemini.go', 'llm/common/base.go', 'management/a.go', 'management/adapters/a.go', 'management/httpapi/a.go']
profile = 'mode: atomic\n' + ''.join(f'github.com/kart-io/goagent/{f}:1.1,2.2 1 1\n' for f in files)
profile += 'github.com/kart-io/goagent/application.go:1.1,2.2 63 1\n'
profile += 'github.com/kart-io/goagent/application.go:3.1,4.2 26 0\n'
for mode, extra in [('production_only', ''), ('with_helper', 'github.com/kart-io/goagent/management/internal/managementtest/store.go:1.1,2.2 20 1\n')]:
    path = root / (mode + '.out')
    path.write_text(profile + extra)
    result = subprocess.run([str(root / 'coveragecheck'), '-profile', str(path)], env=env, capture_output=True, text=True, timeout=10)
    print(mode, 'exit=', result.returncode, result.stdout.splitlines()[0])

# 在发现模块时模拟 go mod edit 阻塞：模块超时 20ms 不约束此阶段。
fake = root / 'fakebin'
fake.mkdir(exist_ok=True)
(fake / 'go').write_text('#!/bin/sh\nexec sleep 30\n')
(fake / 'go').chmod(0o700)
repos = root / 'repos'
for repo in ['goagent', 'goagent-harness', 'goagent-examples', 'goagent-contrib']:
    d = repos / repo
    d.mkdir(parents=True, exist_ok=True)
    (d / 'go.mod').write_text('module github.com/kart-io/' + repo + '\n')
fake_env = dict(env, PATH=str(fake) + os.pathsep + env['PATH'])
started = time.monotonic()
result = subprocess.run(['timeout', '-k', '3s', '1s', str(root / 'compatcheck'), '-root', str(repos), '-out', str(root / 'timeout-report'), '-timeout', '20ms'], env=fake_env, capture_output=True, text=True, timeout=6)
print('discovery_timeout', 'exit=', result.returncode, 'seconds=', round(time.monotonic() - started, 2), 'output=', (result.stdout + result.stderr).strip())
