package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestGatePropagatesReportWriteFailure(t *testing.T) {
	want := errors.New("报告写入失败")
	for _, blocks := range []map[string]block{{}, {"block": {file: "a.go", statements: 1, covered: true}}} {
		if err := check(blocks, []gate{{"整体", "", 45}}, failingWriter{want}); !errors.Is(err, want) {
			t.Fatalf("需要报告写入错误，实际: %v", err)
		}
	}
}

// 每个关键包或文件必须有独立门禁，不能被其他包的高覆盖率掩盖。
func TestRunEnforcesEveryCriticalCoverageGate(t *testing.T) {
	files := []string{"core/durable/a.go", "core/budget/a.go", "workflow/a.go", "builder/a.go", "multiagent/a.go", "llm/providers/openai.go", "llm/providers/gemini.go", "llm/common/base.go", "management/a.go", "management/adapters/a.go", "management/httpapi/a.go"}
	profile := filepath.Join(t.TempDir(), "coverage.out")
	write := func(target, mode string) {
		t.Helper()
		var data strings.Builder
		data.WriteString("mode: atomic\n")
		for _, file := range files {
			if file == target && mode == "missing" {
				continue
			}
			count := "1"
			if file == target && mode == "uncovered" {
				count = "0"
			}
			data.WriteString(module + file + ":1.1,2.2 100 " + count + "\n")
		}
		if err := os.WriteFile(profile, []byte(data.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("", "")
	if err := run(profile, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		for _, mode := range []string{"missing", "uncovered"} {
			t.Run(file+"/"+mode, func(t *testing.T) {
				write(file, mode)
				if run(profile, &bytes.Buffer{}) == nil {
					t.Fatalf("整体覆盖率通过时仍须拒绝关键数据 %s: %s", mode, file)
				}
			})
		}
	}
}

func TestWeightedCoverageAndExampleExclusion(t *testing.T) {
	p, err := parseProfile(strings.NewReader("mode: atomic\n" +
		"github.com/kart-io/goagent/core/durable/a.go:1.1,2.2 9 1\n" +
		"github.com/kart-io/goagent/core/durable/a.go:3.1,4.2 1 0\n" +
		"github.com/kart-io/goagent/examples/demo/main.go:1.1,2.2 100 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := check(p, []gate{{"整体", "", 90}, {"持久化", "core/durable/", 90}}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "90.0% (9/10)") {
		t.Fatal(out.String())
	}
}

func TestGateRejectsMissingAndUndercoveredModules(t *testing.T) {
	p, err := parseProfile(strings.NewReader("mode: set\ngithub.com/kart-io/goagent/core/durable/a.go:1.1,2.2 10 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []gate{{"预算", "core/budget/", 75}, {"持久化", "core/durable/", 70}, {"相邻包", "core/durablex/", 70}} {
		var out bytes.Buffer
		if check(p, []gate{rule}, &out) == nil {
			t.Fatalf("门禁错误放行: %+v", rule)
		}
	}
}

// 测试辅助包由调用方的测试执行，不能混入被测生产包的独立门禁。
func TestPackageGateDoesNotMixDescendants(t *testing.T) {
	p, err := parseProfile(strings.NewReader("mode: set\n" +
		"github.com/kart-io/goagent/core/durable/a.go:1.1,2.2 9 1\n" +
		"github.com/kart-io/goagent/core/durable/a.go:3.1,4.2 1 0\n" +
		"github.com/kart-io/goagent/core/durable/storetest/a.go:1.1,2.2 100 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := check(p, []gate{{"持久化", "core/durable/", 90}}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "90.0% (9/10)") {
		t.Fatal(out.String())
	}
}

func TestManagementCoverageBoundaries(t *testing.T) {
	for _, target := range []gate{{"management", "management/", 75}, {"management/adapters", "management/adapters/", 75}, {"management/httpapi", "management/httpapi/", 80}} {
		for _, covered := range []int{int(target.minimum) - 1, int(target.minimum)} {
			t.Run(fmt.Sprintf("%s/%d", target.name, covered), func(t *testing.T) {
				var data strings.Builder
				data.WriteString("mode: atomic\n")
				for _, rule := range gates {
					if rule.path == "" {
						continue
					}
					file := rule.path
					if strings.HasSuffix(file, "/") {
						file += "a.go"
					}
					if rule.path == target.path {
						fmt.Fprintf(&data, "%s%s:1.1,2.2 %d 1\n%s%s:3.1,4.2 %d 0\n", module, file, covered, module, file, 100-covered)
					} else {
						fmt.Fprintf(&data, "%s%s:1.1,2.2 100 1\n", module, file)
					}
				}
				profile := filepath.Join(t.TempDir(), "coverage.out")
				if err := os.WriteFile(profile, []byte(data.String()), 0o600); err != nil {
					t.Fatal(err)
				}
				err := run(profile, &bytes.Buffer{})
				if (err != nil) != (covered < int(target.minimum)) {
					t.Fatalf("门禁边界错误：%v", err)
				}
			})
		}
	}
}

func TestProfileRejectsMalformedOrEmptyData(t *testing.T) {
	for _, data := range []string{"", "mode: nope\n", "mode: set\n", "mode: set\nbad\n", "mode: set\nx.go:1.1,2.2 -1 0\n", "mode: set\nx.go:1.1,2.2 1 -1\n", "mode: set\nx.go:1.1,2.2 1 0\n"} {
		if _, err := parseProfile(strings.NewReader(data)); err == nil {
			t.Fatalf("非法或无模块数据被接受: %q", data)
		}
	}
}

func TestDuplicateBlocksDoNotInflateStatementCounts(t *testing.T) {
	p, err := parseProfile(strings.NewReader("mode: count\n" +
		"github.com/kart-io/goagent/core/durable/a.go:1.1,2.2 4 0\n" +
		"github.com/kart-io/goagent/core/durable/a.go:1.1,2.2 4 1\n" +
		"github.com/kart-io/goagent/core/durable/a.go:3.1,4.2 4 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := check(p, []gate{{"整体", "", 50}}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "50.0% (4/8)") {
		t.Fatal(out.String())
	}
	_, err = parseProfile(strings.NewReader("mode: count\ngithub.com/kart-io/goagent/a.go:1.1,2.2 4 1\ngithub.com/kart-io/goagent/a.go:1.1,2.2 5 1\n"))
	if err == nil {
		t.Fatal("冲突的语句数必须失败")
	}
}

func TestRunEnforcesOverall75PercentBoundary(t *testing.T) {
	files := []string{"core/durable/a.go", "core/budget/a.go", "workflow/a.go", "builder/a.go", "multiagent/a.go", "llm/providers/openai.go", "llm/providers/gemini.go", "llm/common/base.go", "management/a.go", "management/adapters/a.go", "management/httpapi/a.go"}
	for _, covered := range []int{74, 75} {
		t.Run(fmt.Sprint(covered), func(t *testing.T) {
			var profile strings.Builder
			profile.WriteString("mode: atomic\n")
			for _, file := range files {
				fmt.Fprintf(&profile, "%s%s:1.1,2.2 1 1\n", module, file)
			}
			fmt.Fprintf(&profile, "%sapplication.go:1.1,2.2 %d 1\n", module, covered-len(files))
			fmt.Fprintf(&profile, "%sapplication.go:3.1,4.2 %d 0\n", module, 100-covered)
			path := filepath.Join(t.TempDir(), "coverage.out")
			if err := os.WriteFile(path, []byte(profile.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			err := run(path, &bytes.Buffer{})
			if (err != nil) != (covered < 75) {
				t.Fatalf("整体 %d%% 的门禁结果错误: %v", covered, err)
			}
		})
	}
}

func TestRunRequiresMultiagentCoverageIndependently(t *testing.T) {
	for _, covered := range []int{-1, 84, 85} {
		t.Run(fmt.Sprint(covered), func(t *testing.T) {
			var profile strings.Builder
			profile.WriteString("mode: atomic\n")
			for _, file := range []string{"core/durable/a.go", "core/budget/a.go", "workflow/a.go", "builder/a.go", "llm/providers/openai.go", "llm/providers/gemini.go", "llm/common/base.go", "management/a.go", "management/adapters/a.go", "management/httpapi/a.go"} {
				fmt.Fprintf(&profile, "%s%s:1.1,2.2 100 1\n", module, file)
			}
			if covered >= 0 {
				fmt.Fprintf(&profile, "%smultiagent/a.go:1.1,2.2 %d 1\n", module, covered)
				fmt.Fprintf(&profile, "%smultiagent/a.go:3.1,4.2 %d 0\n", module, 100-covered)
			}
			path := filepath.Join(t.TempDir(), "coverage.out")
			if err := os.WriteFile(path, []byte(profile.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			err := run(path, &bytes.Buffer{})
			if (err != nil) != (covered < 85) {
				t.Fatalf("其他模块高覆盖率不能掩盖 multiagent %d%%: %v", covered, err)
			}
		})
	}
}
