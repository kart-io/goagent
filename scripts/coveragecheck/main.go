// coveragecheck 按语句数加权检查整体、关键包和关键文件的覆盖率；门槛来自数据文件。
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const module = "github.com/kart-io/goagent/"

type block struct {
	file       string
	statements uint64
	covered    bool
}

// gate.target：「*」为整体；以 .go 结尾为单文件；其余为精确包路径（不含子包）。
type gate struct {
	target  string
	minimum float64
}

var profileLine = regexp.MustCompile(`^(.+):[0-9]+\.[0-9]+,[0-9]+\.[0-9]+ ([0-9]+) ([0-9]+)$`)

func parseProfile(r io.Reader) (map[string]block, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if !scanner.Scan() {
		return nil, errors.New("覆盖率文件缺少模式头")
	}
	switch scanner.Text() {
	case "mode: set", "mode: count", "mode: atomic":
	default:
		return nil, fmt.Errorf("未知覆盖率模式: %q", scanner.Text())
	}
	blocks := map[string]block{}
	line := 1
	for scanner.Scan() {
		line++
		text := scanner.Text()
		m := profileLine.FindStringSubmatch(text)
		if m == nil {
			return nil, fmt.Errorf("覆盖率第 %d 行格式无效", line)
		}
		statements, err := strconv.ParseUint(m[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行语句数无效: %w", line, err)
		}
		count, err := strconv.ParseUint(m[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行执行次数无效: %w", line, err)
		}
		if !strings.HasPrefix(m[1], module) {
			continue
		}
		file := strings.TrimPrefix(m[1], module)
		if strings.HasPrefix(file, "examples/") || strings.Contains(file, "/examples/") || strings.HasPrefix(file, "example/") || strings.Contains(file, "/example/") {
			continue
		}
		key := text[:strings.LastIndex(text[:strings.LastIndex(text, " ")], " ")]
		old, exists := blocks[key]
		if exists && old.statements != statements {
			return nil, fmt.Errorf("第 %d 行覆盖块的语句数冲突", line)
		}
		blocks[key] = block{file: file, statements: statements, covered: count > 0 || old.covered}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, errors.New("缺少主模块非示例覆盖率数据")
	}
	return blocks, nil
}

// readLines 读取去掉注释与空行的条目，供门槛文件与稳定包清单共用。
func readLines(name string) ([][]string, error) {
	data, err := os.ReadFile(name) // #nosec G304 -- 路径来自命令行参数，由仓库脚本与 CI 传入
	if err != nil {
		return nil, err
	}
	var out [][]string
	for _, raw := range strings.Split(string(data), "\n") {
		if i := strings.Index(raw, "#"); i >= 0 {
			raw = raw[:i]
		}
		if fields := strings.Fields(raw); len(fields) > 0 {
			out = append(out, fields)
		}
	}
	return out, nil
}

func loadGates(name string) ([]gate, error) {
	lines, err := readLines(name)
	if err != nil {
		return nil, fmt.Errorf("读取门槛文件: %w", err)
	}
	seen := map[string]bool{}
	gates := make([]gate, 0, len(lines))
	for _, f := range lines {
		if len(f) != 2 {
			return nil, fmt.Errorf("门槛条目须为「目标 最低百分比」: %q", strings.Join(f, " "))
		}
		minimum, err := strconv.ParseFloat(f[1], 64)
		if err != nil || minimum < 0 || minimum > 100 {
			return nil, fmt.Errorf("门槛 %s 的百分比无效: %q", f[0], f[1])
		}
		if seen[f[0]] {
			return nil, fmt.Errorf("门槛 %s 重复声明", f[0])
		}
		seen[f[0]] = true
		gates = append(gates, gate{f[0], minimum})
	}
	if !seen["*"] {
		return nil, errors.New("门槛文件缺少整体条目「*」")
	}
	return gates, nil
}

// requireStable 保证兼容承诺扩大时覆盖率门槛同步扩大。
func requireStable(gates []gate, stableFile string) error {
	lines, err := readLines(stableFile)
	if err != nil {
		return fmt.Errorf("读取稳定包清单: %w", err)
	}
	declared := map[string]bool{}
	for _, g := range gates {
		declared[g.target] = true
	}
	var missing []string
	for _, f := range lines {
		if !declared[f[0]] {
			missing = append(missing, f[0])
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("稳定包缺少覆盖率门槛: %s", strings.Join(missing, ", "))
	}
	return nil
}

func matches(target, file string) bool {
	switch {
	case target == "*":
		return true
	case strings.HasSuffix(target, ".go"):
		return file == target
	default:
		return path.Dir(file) == target
	}
}

func check(blocks map[string]block, rules []gate, report, summary io.Writer) error {
	failed := false
	if _, err := fmt.Fprint(summary, "| 范围 | 覆盖率 | 门槛 | 结果 |\n|---|---|---|---|\n"); err != nil {
		return err
	}
	for _, rule := range rules {
		var total, covered uint64
		for _, b := range blocks {
			if !matches(rule.target, b.file) {
				continue
			}
			total += b.statements
			if b.covered {
				covered += b.statements
			}
		}
		percent, status := 0.0, "通过"
		if total > 0 {
			percent = 100 * float64(covered) / float64(total)
		}
		if total == 0 || percent < rule.minimum {
			status, failed = "失败", true
		}
		name := rule.target
		if name == "*" {
			name = "整体"
		}
		// 行首加「覆盖率 」前缀，避免 setup-go 的问题匹配器把「x.go: ...」当成编译错误注解。
		if _, err := fmt.Fprintf(report, "覆盖率 %s: %.1f%% (%d/%d)，要求 ≥ %g%%，%s\n", name, percent, covered, total, rule.minimum, status); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(summary, "| `%s` | %.1f%% | %g%% | %s |\n", name, percent, rule.minimum, status); err != nil {
			return err
		}
	}
	if failed {
		return errors.New("覆盖率检查失败")
	}
	return nil
}

type options struct{ profile, gates, stable, summary string }

func run(o options, w io.Writer) (err error) {
	gates, err := loadGates(o.gates)
	if err != nil {
		return err
	}
	if err := requireStable(gates, o.stable); err != nil {
		return err
	}
	f, err := os.Open(o.profile)
	if err != nil {
		return fmt.Errorf("打开覆盖率文件: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	blocks, err := parseProfile(f)
	if err != nil {
		return err
	}
	summary := io.Discard
	if o.summary != "" {
		s, err := os.Create(o.summary)
		if err != nil {
			return fmt.Errorf("创建摘要文件: %w", err)
		}
		defer func() { err = errors.Join(err, s.Close()) }()
		summary = s
	}
	return check(blocks, gates, w, summary)
}

func main() {
	var o options
	flag.StringVar(&o.profile, "profile", "coverage/coverage.out", "覆盖率文件路径")
	flag.StringVar(&o.gates, "gates", "scripts/coverage-gates.txt", "覆盖率门槛文件")
	flag.StringVar(&o.stable, "stable", "scripts/stable-packages.txt", "稳定包清单，每个包都必须有门槛")
	flag.StringVar(&o.summary, "summary", "", "可选：写出 Markdown 摘要表（供 PR 评论使用）")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "用法: coveragecheck [-profile 文件] [-gates 文件] [-stable 文件] [-summary 文件]")
		os.Exit(2)
	}
	if err := run(o, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
