package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kart-io/goagent/codec/json"
)

// 实库门禁要求关键场景有明确通过事件，编译成功或跳过不能作为恢复验收。
var postgresContracts = map[string][]string{
	"github.com/kart-io/goagent-contrib/durable/pgstore": {
		"TestConformance", "TestProcessCrashTakeover", "TestPrimaryRestart", "TestSyncCommit",
	},
	"github.com/kart-io/goagent-contrib/management": {
		"integration/TestHostRegisterPublishSessionTaskApprovalResume",
		"integration/TestApprovalSurvivesProcessKillAndRestart",
		"integration/TestForkCrashAfterLogCopy",
	},
	"github.com/kart-io/goagent-harness/integration/postgres": {
		"TestWaitRestartResumeRelease", "TestTakeoverFencesOldHolder", "TestInboxAppendOnceAcrossConnections",
	},
}

func checkPostgresEvidence(directory string) error {
	raw, err := os.ReadFile(filepath.Join(directory, "summary.json"))
	if err != nil {
		return err
	}
	var report summary
	if err := json.Unmarshal(raw, &report); err != nil {
		return err
	}
	var failures []error
	for module, required := range postgresContracts {
		var result *moduleResult
		for i := range report.Modules {
			if report.Modules[i].Path == module {
				if result != nil {
					return fmt.Errorf("实库验收模块重复：%s", module)
				}
				result = &report.Modules[i]
			}
		}
		if result == nil || result.Status != "passed" || result.Error != "" || len(result.FailedTests) != 0 {
			failures = append(failures, fmt.Errorf("实库验收缺少通过的模块：%s", module))
			continue
		}
		if result.Log == "" || filepath.Base(result.Log) != result.Log {
			return fmt.Errorf("无效的模块日志路径：%s", module)
		}
		states, err := testEvidence(filepath.Join(directory, result.Log), result)
		if err != nil {
			return err
		}
		for _, name := range required {
			key := module + "/" + name
			if states[key] != "pass" {
				failures = append(failures, fmt.Errorf("实库场景未通过（缺失、失败或跳过）：%s/%s", module, name))
			}
			for test, state := range states {
				if strings.HasPrefix(test, key+"/") && state != "pass" {
					failures = append(failures, fmt.Errorf("实库场景后代未通过：%s（%s）", test, state))
				}
			}
		}
	}
	return errors.Join(failures...)
}

// 包和测试都必须有开始与终态；只检查顶层 pass 无法证明日志完整。
func testEvidence(path string, result *moduleResult) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	states := map[string]string{}
	type packageState struct {
		ended       bool
		noTestFiles bool
		tests       map[string]string
	}
	packages := map[string]*packageState{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 16*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		invalid := func(reason string) error {
			return fmt.Errorf("%s 第%d行：%s", path, lineNumber, reason)
		}
		var event struct{ Action, Package, Test, Output, ImportPath string }
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// 旧 Go 的下载、编译诊断与 JSON 共用日志；疑似事件的损坏行不能当诊断吞掉。
			if strings.HasPrefix(line, "{") || strings.HasPrefix(line, "[") || strings.Contains(line, `"Action"`) {
				return nil, invalid("测试事件 JSON 损坏")
			}
			continue
		}
		if event.Action == "fail" || event.Action == "build-fail" {
			return nil, invalid("日志包含失败事件")
		}
		if event.Action == "build-output" && event.ImportPath != "" && event.Output != "" && event.Package == "" && event.Test == "" {
			// 依赖包的编译诊断可以来自模块外，不是该模块的测试生命周期。
			continue
		}
		if event.Package != result.Path && !strings.HasPrefix(event.Package, result.Path+"/") {
			return nil, invalid("测试事件不属于模块：" + event.Package)
		}
		pkg := packages[event.Package]
		if event.Action == "start" && event.Test == "" && pkg == nil {
			packages[event.Package] = &packageState{tests: map[string]string{}}
			continue
		}
		if pkg == nil || pkg.ended {
			return nil, invalid("包未开始或已经结束：" + event.Package)
		}
		if event.Action == "output" {
			if event.Test == "" && strings.TrimSpace(event.Output) == "?   \t"+event.Package+"\t[no test files]" {
				pkg.noTestFiles = true
			}
			continue
		}
		if event.Test == "" {
			noTestPackage := event.Action == "skip" && len(pkg.tests) == 0 && pkg.noTestFiles
			if event.Action != "pass" && !noTestPackage {
				return nil, invalid("包没有成功结束或不是无测试包：" + event.Package)
			}
			for test, state := range pkg.tests {
				if state == "run" {
					return nil, invalid("测试缺少结束事件：" + event.Package + "/" + test)
				}
			}
			pkg.ended = true
			continue
		}
		switch event.Action {
		case "run":
			if pkg.tests[event.Test] != "" {
				return nil, invalid("测试重复开始：" + event.Test)
			}
			pkg.tests[event.Test] = "run"
		case "pause", "cont", "pass", "skip":
			if pkg.tests[event.Test] != "run" {
				return nil, invalid("测试未开始或已经结束：" + event.Test)
			}
			if event.Action == "pass" || event.Action == "skip" {
				pkg.tests[event.Test] = event.Action
				states[event.Package+"/"+event.Test] = event.Action
			}
		default:
			return nil, invalid("未知测试事件：" + event.Action)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("日志没有测试包：%s", path)
	}
	for name, pkg := range packages {
		if !pkg.ended {
			return nil, fmt.Errorf("日志缺少包结束事件：%s", name)
		}
	}
	// 复用实跑的统计口径核对摘要，摘要中的 passed 不能替代事件证据。
	var actual moduleResult
	if err := readTestResults(path, &actual); err != nil {
		return nil, err
	}
	sameNames := func(a, b []string) bool {
		a, b = slices.Clone(a), slices.Clone(b)
		slices.Sort(a)
		slices.Sort(b)
		return slices.Equal(a, b)
	}
	if actual.PassedTests != result.PassedTests || actual.PassedPackages != result.PassedPackages ||
		!sameNames(actual.SkippedTests, result.SkippedTests) || !sameNames(actual.SkippedPackages, result.SkippedPackages) {
		return nil, fmt.Errorf("模块摘要与测试日志统计不符：%s", result.Path)
	}
	return states, nil
}
