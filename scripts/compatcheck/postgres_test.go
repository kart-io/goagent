package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
)

func TestPostgresEvidenceRequiresExecutedContracts(t *testing.T) {
	for _, mode := range []string{"通过", "跳过", "缺失", "失败", "缺少模块", "只有子测试", "先失败再通过"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			report := summary{}
			index := 0
			for module, required := range postgresContracts {
				index++
				name := fmt.Sprintf("%d.jsonl", index)
				var log strings.Builder
				packages := map[string]bool{}
				for _, contract := range required {
					pkg, test := module, contract
					if i := strings.LastIndex(contract, "/"); i >= 0 {
						pkg += "/" + contract[:i]
						test = contract[i+1:]
					}
					if !packages[pkg] {
						log.WriteString(evidenceEvent("start", pkg, ""))
						packages[pkg] = true
					}
					action := "pass"
					if mode == "跳过" {
						action = "skip"
					}
					if mode == "失败" {
						action = "fail"
					}
					if mode == "只有子测试" {
						test += "/child"
					}
					if mode != "缺失" {
						log.WriteString(evidenceEvent("run", pkg, test))
					}
					if mode == "先失败再通过" {
						raw, _ := json.Marshal(map[string]string{"Action": "fail", "Package": pkg, "Test": test})
						log.Write(raw)
						log.WriteByte('\n')
					}
					if mode != "缺失" {
						raw, _ := json.Marshal(map[string]string{"Action": action, "Package": pkg, "Test": test})
						log.Write(raw)
						log.WriteByte('\n')
					}
				}
				for pkg := range packages {
					log.WriteString(evidenceEvent("pass", pkg, ""))
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(log.String()), 0o600); err != nil {
					t.Fatal(err)
				}
				if mode != "缺少模块" {
					result := moduleResult{Path: module, Status: "passed", Log: name}
					if err := readTestResults(filepath.Join(dir, name), &result); err != nil {
						t.Fatal(err)
					}
					report.Modules = append(report.Modules, result)
				}
			}
			if err := writeJSON(filepath.Join(dir, "summary.json"), report); err != nil {
				t.Fatal(err)
			}
			err := checkPostgresEvidence(dir)
			if (err == nil) != (mode == "通过") {
				t.Fatalf("模式%s，门禁结果%v", mode, err)
			}
		})
	}
}

func evidenceEvent(action, pkg, test string) string {
	raw, err := json.Marshal(map[string]string{"Action": action, "Package": pkg, "Test": test})
	if err != nil {
		panic(err)
	}
	return string(raw) + "\n"
}

// 使用真实 Go 事件作为有效基准，变异仅改变所测试的证据边界。
func realPostgresEvidence(t *testing.T) (string, summary) {
	t.Helper()
	dir := t.TempDir()
	report := summary{}
	for module, contracts := range postgresContracts {
		root := t.TempDir()
		writeFixture(t, root, "go.mod", "module "+module+"\n\ngo 1.26.0\n")
		sources := map[string]string{}
		for _, contract := range contracts {
			pkg, name := filepath.Dir(contract), filepath.Base(contract)
			if sources[pkg] == "" {
				sources[pkg] = "package contract\nimport (\"testing\"; \"os\")\nvar _ = os.Getenv\n"
			}
			body := ""
			if name == "TestConformance" {
				body = `t.Run("存储", func(t *testing.T) {
 t.Parallel()
 t.Run("持久化", func(t *testing.T) {
  switch os.Getenv("COMPATCHECK_CONTRACT_MODE") {
  case "skip": t.Skip("关键场景没有执行")
  case "fail": t.Fatal("关键场景失败")
  }
 })
})`
			}
			sources[pkg] += fmt.Sprintf("func %s(t *testing.T) { %s }\n", name, body)
		}
		for pkg, source := range sources {
			writeFixture(t, root, filepath.Join(pkg, "contract_test.go"), source)
		}
		writeFixture(t, root, "empty/value.go", "package empty\n")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		output, err := goCommand(ctx, root, "test", "-json", "-race", "-count=1", "./...").Output()
		cancel()
		if err != nil {
			t.Fatalf("生成真实通过事件失败: %v\n%s", err, output)
		}
		name := fmt.Sprintf("%d.jsonl", len(report.Modules))
		writeFixture(t, dir, name, string(output))
		result := moduleResult{Path: module, Status: "passed", Log: name, Dir: root}
		if err := readTestResults(filepath.Join(dir, name), &result); err != nil {
			t.Fatal(err)
		}
		report.Modules = append(report.Modules, result)
	}
	if err := writeJSON(filepath.Join(dir, "summary.json"), report); err != nil {
		t.Fatal(err)
	}
	return dir, report
}

func TestPostgresEvidenceRealEventsAndIntegrity(t *testing.T) {
	t.Setenv("COMPATCHECK_CONTRACT_MODE", "")
	dir, report := realPostgresEvidence(t)
	var target moduleResult
	for _, result := range report.Modules {
		if strings.HasSuffix(result.Path, "/pgstore") {
			target = result
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, target.Log))
	if err != nil {
		t.Fatal(err)
	}
	original := string(raw)
	// 按事件字段查找，避免依赖 Go 版本的时间、字段顺序或输出格式。
	change := func(action, test, replacement string) string {
		lines := strings.SplitAfter(original, "\n")
		for i, line := range lines {
			var event struct{ Action, Package, Test string }
			if json.Unmarshal([]byte(line), &event) == nil && event.Package == target.Path && event.Action == action && event.Test == test {
				lines[i] = replacement
				return strings.Join(lines, "")
			}
		}
		t.Fatalf("真实事件未包含 %s/%s", action, test)
		return ""
	}
	for _, tc := range []struct {
		name, log string
		valid     bool
	}{
		{"完整真实流及无测试包", original, true},
		{"普通构建诊断", "go: downloading example.com/dependency v1.0.0\n# runtime/cgo\nwarning: 仅供诊断\n" + original, true},
		{"依赖构建事件", "{\"Action\":\"build-output\",\"ImportPath\":\"runtime/cgo\",\"Output\":\"warning: 仅供诊断\\n\"}\n" + original, true},
		{"包失败", change("pass", "", evidenceEvent("fail", target.Path, "")), false},
		{"缺少包结束", change("pass", "", ""), false},
		{"缺少包开始", change("start", "", ""), false},
		{"缺少测试结束", change("pass", "TestSyncCommit", ""), false},
		{"缺少测试开始", change("run", "TestSyncCommit", ""), false},
		{"损坏尾部", original + `{"Action":"fail","Package":`, false},
		{"损坏中间", "{损坏的JSON}\n" + original, false},
		{"无关JSON", original + "{}\n", false},
		{"构建失败", original + "{\"Action\":\"build-fail\",\"ImportPath\":\"runtime/cgo\"}\n", false},
		{"异模块失败", original + evidenceEvent("fail", "example.com/other", "TestFailure"), false},
		{"异模块通过", original + evidenceEvent("start", target.Path+"-other", "") + evidenceEvent("pass", target.Path+"-other", ""), false},
		{"模块内辅助包未结束", original + evidenceEvent("start", target.Path+"/unfinished", ""), false},
		{"模块内辅助测试失败", original + evidenceEvent("start", target.Path+"/auxiliary", "") + evidenceEvent("run", target.Path+"/auxiliary", "TestOther") + evidenceEvent("fail", target.Path+"/auxiliary", "TestOther"), false},
		{"执行过测试的包跳过", change("pass", "", evidenceEvent("skip", target.Path, "")), false},
		{"重复完整流", original + original, false},
		{"重复包结束", original + evidenceEvent("pass", target.Path, ""), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFixture(t, dir, target.Log, tc.log)
			if err := checkPostgresEvidence(dir); (err == nil) != tc.valid {
				t.Fatalf("门禁结果不符，期望通过=%v，实际=%v", tc.valid, err)
			}
		})
	}
	writeFixture(t, dir, target.Log, original)
	for _, mode := range []string{"skip", "fail"} {
		t.Run("真实后代"+mode, func(t *testing.T) {
			t.Setenv("COMPATCHECK_CONTRACT_MODE", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			output, err := goCommand(ctx, target.Dir, "test", "-json", "-race", "-count=1", "./...").Output()
			if (err == nil) != (mode == "skip") || !strings.Contains(string(output), `"Action":"`+mode+`"`) {
				t.Fatalf("未产生预期真实事件: %v\n%s", err, output)
			}
			writeFixture(t, dir, target.Log, string(output))
			// 摘要如实统计 skip，仍声称模块通过；门禁必须独立判定关键后代。
			altered := report
			altered.Modules = append([]moduleResult(nil), report.Modules...)
			for i, result := range altered.Modules {
				if result.Path == target.Path {
					result.PassedTests, result.PassedPackages = 0, 0
					result.SkippedTests, result.SkippedPackages = nil, nil
					if err := readTestResults(filepath.Join(dir, target.Log), &result); err != nil {
						t.Fatal(err)
					}
					altered.Modules[i] = result
				}
			}
			if err := writeJSON(filepath.Join(dir, "summary.json"), altered); err != nil {
				t.Fatal(err)
			}
			if err := checkPostgresEvidence(dir); err == nil {
				t.Fatal("关键契约后代未通过却被放行")
			}
		})
	}
	writeFixture(t, dir, target.Log, original)
	for _, mode := range []string{"重复模块", "摘要计数不符", "摘要失败不符"} {
		t.Run(mode, func(t *testing.T) {
			altered := report
			altered.Modules = append([]moduleResult(nil), report.Modules...)
			switch mode {
			case "重复模块":
				altered.Modules = append(altered.Modules, target)
			case "摘要计数不符":
				altered.Modules[0].PassedTests++
			case "摘要失败不符":
				altered.Modules[0].Error = "测试失败"
			}
			if err := writeJSON(filepath.Join(dir, "summary.json"), altered); err != nil {
				t.Fatal(err)
			}
			if err := checkPostgresEvidence(dir); err == nil {
				t.Fatal("摘要与日志矛盾却被放行")
			}
		})
	}
}
