import os, pathlib, subprocess, json
root = pathlib.Path(__file__).parent
workspace = root / 'child-skip-workspace'
core = workspace / 'goagent'
core.mkdir(parents=True, exist_ok=True)
(core / 'go.mod').write_text('module github.com/kart-io/goagent\n\ngo 1.26.0\n')
(core / 'value.go').write_text('package goagent\nconst Value = 42\n')
modules = {
 'goagent-examples': ('', ['TestFixture']),
 'goagent-contrib/durable/pgstore': ('', ['TestConformance', 'TestProcessCrashTakeover', 'TestPrimaryRestart', 'TestSyncCommit']),
 'goagent-contrib/management': ('integration', ['TestHostRegisterPublishSessionTaskApprovalResume', 'TestApprovalSurvivesProcessKillAndRestart', 'TestForkCrashAfterLogCopy']),
 'goagent-harness/integration/postgres': ('', ['TestWaitRestartResumeRelease', 'TestTakeoverFencesOldHolder', 'TestInboxAppendOnceAcrossConnections']),
}
for name, (pkg, tests) in modules.items():
    directory = workspace / name
    directory.mkdir(parents=True, exist_ok=True)
    (directory / 'go.mod').write_text('module github.com/kart-io/' + name + '\n\ngo 1.26.0\n\nrequire github.com/kart-io/goagent v0.0.0\n')
    testdir = directory / pkg
    testdir.mkdir(exist_ok=True)
    source = 'package contract\nimport ("testing"; core "github.com/kart-io/goagent")\n'
    for test in tests:
        body = 'if core.Value != 42 { t.Fatal("wrong source") }'
        if test == 'TestConformance':
            body += '; t.Run("durability", func(t *testing.T) { t.Skip("prerequisite unavailable") })'
        source += 'func ' + test + '(t *testing.T) { ' + body + ' }\n'
    (testdir / 'contract_test.go').write_text(source)
env = dict(os.environ, GO_TEST_MODE='true', GOPROXY='off', GOSUMDB='off', GOAGENT_PGSTORE_DSN='fixture-unused', GOAGENT_PGSTORE_CONTAINER='fixture-unused')
env.pop('GOROOT', None)
report = root / 'child-skip-report'
result = subprocess.run([str(root / 'compatcheck'), '-root', str(workspace), '-out', str(report), '-timeout', '20s', '-require-postgres'], env=env, capture_output=True, text=True, timeout=100)
(root / 'child-skip-result.txt').write_text(f'exit={result.returncode}\n' + result.stdout + result.stderr)
print('full_require_postgres_child_skip', result.returncode, result.stdout, result.stderr)
summary = json.loads((report / 'summary.json').read_text())
for module in summary['modules']:
    print(module['module'], module['status'], module.get('skipped_tests', []))
