// coveragecheck 按语句数加权检查整体、关键模块和关键文件的覆盖率。
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
type gate struct {
	name, path string
	minimum    float64
}

var profileLine = regexp.MustCompile(`^(.+):[0-9]+\.[0-9]+,[0-9]+\.[0-9]+ ([0-9]+) ([0-9]+)$`)
var gates = []gate{
	{"整体覆盖率", "", 45},
	{"core/durable", "core/durable/", 70},
	{"core/budget", "core/budget/", 75},
	{"workflow", "workflow/", 75},
	{"builder", "builder/", 75},
	{"llm/providers/openai.go", "llm/providers/openai.go", 70},
	{"llm/providers/gemini.go", "llm/providers/gemini.go", 65},
	{"llm/common/base.go", "llm/common/base.go", 50},
}

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

func check(blocks map[string]block, rules []gate, w io.Writer) error {
	failed := false
	for _, rule := range rules {
		var total, covered uint64
		for _, b := range blocks {
			if rule.path != "" && b.file != rule.path && (!strings.HasSuffix(rule.path, "/") || path.Dir(b.file) != strings.TrimSuffix(rule.path, "/")) {
				continue
			}
			total += b.statements
			if b.covered {
				covered += b.statements
			}
		}
		if total == 0 {
			if _, err := fmt.Fprintf(w, "%s: 缺少覆盖率数据（要求 ≥ %.0f%%）\n", rule.name, rule.minimum); err != nil {
				return err
			}
			failed = true
			continue
		}
		percent := 100 * float64(covered) / float64(total)
		status := "通过"
		if percent < rule.minimum {
			status = "失败"
			failed = true
		}
		if _, err := fmt.Fprintf(w, "%s: %.1f%% (%d/%d)，要求 ≥ %.0f%%，%s\n", rule.name, percent, covered, total, rule.minimum, status); err != nil {
			return err
		}
	}
	if failed {
		return errors.New("覆盖率检查失败")
	}
	return nil
}

func run(path string, w io.Writer) (err error) {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开覆盖率文件: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	blocks, err := parseProfile(f)
	if err != nil {
		return err
	}
	return check(blocks, gates, w)
}

func main() {
	path := flag.String("profile", "coverage/coverage.out", "覆盖率文件路径")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "用法: coveragecheck -profile 文件")
		os.Exit(2)
	}
	if err := run(*path, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
