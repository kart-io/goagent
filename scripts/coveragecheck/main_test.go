package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func writeFile(t *testing.T, name, data string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGatePropagatesReportWriteFailure(t *testing.T) {
	want := errors.New("报告写入失败")
	for _, blocks := range []map[string]block{{}, {"block": {file: "a.go", statements: 1, covered: true}}} {
		if err := check(blocks, []gate{{"*", 45}}, failingWriter{want}, io.Discard); !errors.Is(err, want) {
			t.Fatalf("需要报告写入错误，实际: %v", err)
		}
		if err := check(blocks, []gate{{"*", 45}}, io.Discard, failingWriter{want}); !errors.Is(err, want) {
			t.Fatalf("需要摘要写入错误，实际: %v", err)
		}
	}
}

// 门槛来自数据文件；稳定包缺门槛、门槛文件非法、覆盖不足都必须失败。
func TestRunGatesFromDataFile(t *testing.T) {
	profile := "mode: atomic\n" +
		module + "core/a.go:1.1,2.2 8 1\n" + module + "core/a.go:3.1,4.2 2 0\n" +
		module + "core/sub/a.go:1.1,2.2 10 0\n" +
		module + "llm/providers/openai.go:1.1,2.2 5 1\n" +
		module + "examples/x/main.go:1.1,2.2 100 0\n"
	cases := []struct {
		name, gates, stable string
		wantErr             string
		wantOut             string
		wantSummary         string
	}{
		{"通过", "* 40\ncore 80\nllm/providers/openai.go 100\n", "core\n", "", "覆盖率 core: 80.0% (8/10)", "| `core` | 80.0% | 80% | 通过 |"},
		{"包门槛不含子包且低于门槛失败", "* 0\ncore 81\n", "core\n", "覆盖率检查失败", "失败", "| `core` | 80.0% | 81% | 失败 |"},
		{"整体按语句加权且排除 examples", "* 52\n", "", "", "覆盖率 整体: 52.0% (13/25)", "| `整体` | 52.0% |"},
		{"稳定包缺门槛", "* 0\n", "# 注释\ncore\ncore/sub\n", "稳定包缺少覆盖率门槛: core, core/sub", "", ""},
		{"缺整体条目", "core 1\n", "", "缺少整体条目", "", ""},
		{"百分比越界", "* 101\n", "", "百分比无效", "", ""},
		{"条目字段数错误", "* 1 2\n", "", "门槛条目须为", "", ""},
		{"重复声明", "* 1\ncore 1\ncore 2\n", "", "重复声明", "", ""},
		{"门槛目标无数据", "* 0\ntools 1\n", "", "覆盖率检查失败", "(0/0)", ""},
		{"无语句目标确无语句", "* 0\ntools -\n", "tools\n", "", "覆盖率 tools: 0 条语句，要求无可执行语句，通过", "| `tools` | — | 无语句 | 通过 |"},
		{"无语句目标出现语句", "* 0\ncore -\n", "", "覆盖率检查失败", "覆盖率 core: 10 条语句，要求无可执行语句，失败", ""},
		{"整体不能声明无语句", "* -\n", "", "百分比无效", "", ""},
		{"不计的包移出整体分母", "* 80\ncore/sub 不计\n", "", "", "覆盖率 整体: 86.7% (13/15)", "| `core/sub` | 0.0% | 不计入整体 | — |"},
		{"不计只排除精确包", "* 87\ncore 不计\n", "", "覆盖率检查失败", "覆盖率 整体: 33.3% (5/15)", ""},
		{"稳定包不能不计", "* 0\ncore 不计\n", "core\n", "稳定包不能标记为不计: core", "", ""},
		{"整体不能不计", "* 不计\n", "", "百分比无效", "", ""},
		{"单文件不能不计", "* 0\ncore/a.go 不计\n", "", "百分比无效", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			summary := filepath.Join(t.TempDir(), "summary.md")
			err := run(options{
				profile: writeFile(t, "coverage.out", profile),
				gates:   writeFile(t, "gates.txt", c.gates),
				stable:  writeFile(t, "stable.txt", c.stable),
				summary: summary,
			}, &out)
			if c.wantErr == "" && err != nil || c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("错误 = %v，期望包含 %q", err, c.wantErr)
			}
			if !strings.Contains(out.String(), c.wantOut) {
				t.Fatalf("报告缺少 %q:\n%s", c.wantOut, out.String())
			}
			if c.wantSummary != "" {
				data, err := os.ReadFile(summary)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(data), c.wantSummary) {
					t.Fatalf("摘要缺少 %q:\n%s", c.wantSummary, data)
				}
			}
		})
	}
}

// 仓库门槛文件必须覆盖全部稳定包；每条门槛都要独立生效，不能被其他目标的高覆盖率掩盖。
func TestRepositoryGatesAreEnforcedIndependently(t *testing.T) {
	const gatesFile, stableFile = "../coverage-gates.txt", "../stable-packages.txt"
	gates, err := loadGates(gatesFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireStable(gates, stableFile); err != nil {
		t.Fatal(err)
	}
	fileOf := func(target string) string {
		if strings.HasSuffix(target, ".go") {
			return target
		}
		return target + "/gate_probe.go"
	}
	// 被测目标 100 条语句、覆盖 covered 条；其余目标各 10000 条全覆盖。
	// 测包门槛时，包内单文件探针按同一比例覆盖；测单文件门槛时，所在包由大权重探针托住。
	profileFor := func(target string, covered int) string {
		var data strings.Builder
		data.WriteString("mode: atomic\n")
		for _, g := range gates {
			if g.target == "*" || g.minimum == noCode {
				continue
			}
			file := fileOf(g.target)
			if g.target == target || path.Dir(file) == target {
				fmt.Fprintf(&data, "%s%s:1.1,2.2 %d 1\n%s%s:3.1,4.2 %d 0\n", module, file, covered, module, file, 100-covered)
			} else {
				fmt.Fprintf(&data, "%s%s:1.1,2.2 10000 1\n", module, file)
			}
		}
		return data.String()
	}
	runWith := func(profile string) error {
		return run(options{profile: writeFile(t, "coverage.out", profile), gates: gatesFile, stable: stableFile}, io.Discard)
	}
	if err := runWith(profileFor("", 0)); err != nil {
		t.Fatalf("全部覆盖时应通过: %v", err)
	}
	for _, g := range gates {
		if g.target == "*" || g.minimum <= 0 {
			continue
		}
		at := int(g.minimum)
		if float64(at) < g.minimum {
			at++
		}
		t.Run(g.target, func(t *testing.T) {
			if err := runWith(profileFor(g.target, at)); err != nil {
				t.Fatalf("覆盖 %d%% 应满足门槛 %g%%: %v", at, g.minimum, err)
			}
			if runWith(profileFor(g.target, at-1)) == nil {
				t.Fatalf("覆盖 %d%% 低于门槛 %g%% 仍被放行", at-1, g.minimum)
			}
		})
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
	if err := check(p, []gate{{"*", 90}, {"core/durable", 90}}, &out, io.Discard); err != nil {
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
	for _, rule := range []gate{{"core/budget", 75}, {"core/durable", 70}, {"core/durablex", 70}} {
		if check(p, []gate{rule}, io.Discard, io.Discard) == nil {
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
	if err := check(p, []gate{{"core/durable", 90}}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "90.0% (9/10)") {
		t.Fatal(out.String())
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
	if err := check(p, []gate{{"*", 50}}, &out, io.Discard); err != nil {
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
