// compatcheck 用临时依赖覆盖验证本地关联模块，不改写下游仓库。
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/internal/processgroup"
)

const coreModule = "github.com/kart-io/goagent"

type (
	moduleRef struct{ Path, Version string }
	module    struct {
		Path string
		Dir  string
		mods struct {
			Module  moduleRef
			Replace []struct{ Old, New moduleRef }
		}
	}
)

type moduleResult struct {
	Path            string   `json:"module"`
	Dir             string   `json:"directory"`
	Status          string   `json:"status"`
	CoreDir         string   `json:"core_directory,omitempty"`
	Log             string   `json:"log,omitempty"`
	PassedTests     int      `json:"passed_tests"`
	PassedPackages  int      `json:"passed_packages"`
	SkippedTests    []string `json:"skipped_tests,omitempty"`
	SkippedPackages []string `json:"skipped_packages,omitempty"`
	FailedTests     []string `json:"failed_tests,omitempty"`
	Error           string   `json:"error,omitempty"`
}

type summary struct {
	CoreDir      string         `json:"core_directory"`
	CoreRevision string         `json:"core_revision,omitempty"`
	GoVersion    string         `json:"go_version"`
	Modules      []moduleResult `json:"modules"`
}

type goProcess struct{ *exec.Cmd }

func (c *goProcess) Run() error {
	defer cleanupProcess(c.Cmd)
	return c.Cmd.Run()
}

func (c *goProcess) Output() ([]byte, error) {
	defer cleanupProcess(c.Cmd)
	return c.Cmd.Output()
}

func cleanupProcess(cmd *exec.Cmd) { _ = processgroup.Kill(cmd) }

func goCommand(ctx context.Context, dir string, args ...string) *goProcess {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	// 清除旧标准库路径和外部 workspace/GOFLAGS，保留代理、工具链及显式测试服务配置。
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GOROOT=") && !strings.HasPrefix(entry, "GOWORK=") && !strings.HasPrefix(entry, "GOFLAGS=") && !strings.HasPrefix(entry, "GO_TEST_MODE=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GOWORK=off", "GOFLAGS=", "GO_TEST_MODE=true")
	cmd.WaitDelay = 2 * time.Second
	processgroup.Configure(cmd)
	return &goProcess{cmd}
}

func inheritedGoFlags(modfile string) (string, error) {
	arg := "-modfile=" + modfile
	// Go 的 GOFLAGS 按 cmd/internal/quoted 解析，引用内不处理反斜线转义。
	if !strings.ContainsAny(arg, " \t\r\n") {
		return "-mod=mod " + arg, nil
	}
	if !strings.Contains(arg, "'") {
		return "-mod=mod '" + arg + "'", nil
	}
	if !strings.Contains(arg, `"`) {
		return `-mod=mod "` + arg + `"`, nil
	}
	return "", fmt.Errorf("临时依赖路径同时包含空白和两种引号，无法传入 GOFLAGS")
}

func discover(ctx context.Context, root string, coreDir ...string) ([]module, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if len(coreDir) > 1 {
		return nil, fmt.Errorf("最多指定一个核心工作树")
	}
	selected := ""
	if len(coreDir) == 1 && coreDir[0] != "" {
		selected, err = filepath.Abs(coreDir[0])
		if err != nil {
			return nil, err
		}
	}
	var modules []module
	seen := map[string]bool{}
	for _, repo := range []string{"goagent", "goagent-harness", "goagent-examples", "goagent-contrib"} {
		count := len(modules)
		dir := filepath.Join(root, repo)
		if repo == "goagent" && selected != "" {
			dir = selected
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() && path != dir && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if d.IsDir() || d.Name() != "go.mod" || (repo == "goagent" && filepath.Dir(path) != dir) {
				return nil
			}
			m := module{Dir: filepath.Dir(path)}
			data, err := goCommand(ctx, m.Dir, "mod", "edit", "-json").Output()
			if err != nil {
				return fmt.Errorf("读取 %s: %w", path, err)
			}
			if err := json.Unmarshal(data, &m.mods); err != nil {
				return err
			}
			m.Path = m.mods.Module.Path
			if repo == "goagent" && m.Path != coreModule {
				return fmt.Errorf("核心工作树模块身份不符: %s", m.Path)
			}
			if m.Path == "" || seen[m.Path] {
				return fmt.Errorf("模块路径为空或重复: %s (%s)", m.Path, path)
			}
			seen[m.Path] = true
			modules = append(modules, m)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("发现仓库 %s: %w", repo, err)
		}
		if count == len(modules) {
			return nil, fmt.Errorf("仓库 %s 中没有 Go 模块", repo)
		}
	}
	if !seen[coreModule] {
		return nil, fmt.Errorf("缺少核心模块 %s", coreModule)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	return modules, nil
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value, json.Pretty)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func verify(ctx context.Context, root, out string, timeout time.Duration, coreDir ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if timeout <= 0 || out == "" {
		return fmt.Errorf("必须指定新报告目录和正数超时")
	}
	modules, err := discover(ctx, root, coreDir...)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(out, 0o700); err != nil {
		return fmt.Errorf("报告目录必须不存在: %w", err)
	}
	report := summary{}
	for _, m := range modules {
		if m.Path == coreModule {
			report.CoreDir = m.Dir
			cmd := exec.CommandContext(ctx, "git", "-C", m.Dir, "rev-parse", "HEAD")
			if revision, err := cmd.Output(); err == nil {
				report.CoreRevision = strings.TrimSpace(string(revision))
			}
			continue
		}
		report.Modules = append(report.Modules, moduleResult{Path: m.Path, Dir: m.Dir, Status: "not_run"})
	}
	version, err := goCommand(ctx, report.CoreDir, "version").Output()
	if err != nil {
		return err
	}
	report.GoVersion = strings.TrimSpace(string(version))
	var failures []error
	index := 0
	for _, m := range modules {
		if m.Path == coreModule {
			continue
		}
		result := &report.Modules[index]
		index++
		if ctx.Err() == nil {
			moduleCtx, cancel := context.WithTimeout(ctx, timeout)
			err := verifyModule(moduleCtx, m, modules, report.CoreDir, out, index, timeout, result)
			cancel()
			result.Status = "passed"
			if err != nil {
				result.Status, result.Error = "failed", err.Error()
				failures = append(failures, fmt.Errorf("%s: %w", m.Path, err))
			}
		}
		if err := writeJSON(filepath.Join(out, "summary.json"), report); err != nil {
			return errors.Join(errors.Join(failures...), err)
		}
	}
	return errors.Join(append(failures, ctx.Err())...)
}

func verifyModule(ctx context.Context, m module, modules []module, coreDir, out string, index int, timeout time.Duration, result *moduleResult) error {
	tmp, err := os.MkdirTemp("", "goagent-compat-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	modfile := filepath.Join(tmp, "go.mod")
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(m.Dir, name))
		if errors.Is(err, os.ErrNotExist) && name == "go.sum" {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o600); err != nil {
			return err
		}
	}
	result.Log = fmt.Sprintf("%02d-tests.jsonl", index)
	log, err := os.OpenFile(filepath.Join(out, result.Log), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	args := []string{"mod", "edit", "-modfile=" + modfile}
	local := map[string]bool{}
	for _, current := range modules {
		local[current.Path] = true
	}
	for _, replace := range m.mods.Replace {
		if local[replace.Old.Path] {
			old := replace.Old.Path
			if replace.Old.Version != "" {
				old += "@" + replace.Old.Version
			}
			args = append(args, "-dropreplace="+old)
		}
	}
	for _, current := range modules {
		if current.Path != m.Path {
			args = append(args, "-replace="+current.Path+"="+current.Dir)
		}
	}
	cmd := goCommand(ctx, m.Dir, args...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Run(); err != nil {
		return err
	}
	metadata, err := goCommand(ctx, m.Dir, "list", "-mod=mod", "-modfile="+modfile, "-m", "-json", coreModule).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			_, _ = log.Write(exit.Stderr)
		}
		return err
	}
	var resolved struct{ Replace *struct{ Dir string } }
	if err := json.Unmarshal(metadata, &resolved); err != nil {
		return err
	}
	if resolved.Replace == nil || resolved.Replace.Dir != coreDir {
		return fmt.Errorf("核心依赖没有指向当前源码 %s", coreDir)
	}
	result.CoreDir = resolved.Replace.Dir
	if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("%02d-core.json", index)), metadata, 0o600); err != nil {
		return err
	}
	cmd = goCommand(ctx, m.Dir, "test", "-json", "-mod=mod", "-modfile="+modfile, "-race", "-count=1", "-timeout="+timeout.String(), "./...")
	flags, err := inheritedGoFlags(modfile)
	if err != nil {
		return err
	}
	// 测试内的 go build/run 同样绑定当前源码，不能退回下游原始依赖。
	cmd.Env = append(cmd.Env, "GOFLAGS="+flags)
	cmd.Stdout, cmd.Stderr = log, log
	testErr := cmd.Run()
	if err := log.Close(); err != nil {
		return errors.Join(testErr, err)
	}
	return errors.Join(testErr, ctx.Err(), readTestResults(filepath.Join(out, result.Log), result))
}

func readTestResults(path string, result *moduleResult) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 16*1024*1024)
	for scanner.Scan() {
		var event struct{ Action, Package, Test string }
		// go 的下载和编译诊断也保留在同一日志中，它们不是测试事件。
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch event.Action {
		case "pass":
			if event.Test == "" {
				result.PassedPackages++
			} else {
				result.PassedTests++
			}
		case "skip":
			if event.Test == "" {
				result.SkippedPackages = append(result.SkippedPackages, event.Package)
			} else {
				result.SkippedTests = append(result.SkippedTests, event.Package+"/"+event.Test)
			}
		case "fail":
			if event.Test != "" {
				result.FailedTests = append(result.FailedTests, event.Package+"/"+event.Test)
			}
		}
	}
	return scanner.Err()
}

func main() {
	root := flag.String("root", "..", "包含 goagent 及三个关联仓库的目录")
	coreDir := flag.String("core-dir", "", "显式选择核心工作树，空值使用root/goagent")
	out := flag.String("out", "", "必须不存在的报告目录")
	timeout := flag.Duration("timeout", 5*time.Minute, "每个模块的总时限")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), stopSignals()...)
	defer cancel()
	if err := verify(ctx, *root, *out, *timeout, *coreDir); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, _ = io.WriteString(os.Stdout, "跨仓验证通过，测试及跳过项见 "+filepath.Join(*out, "summary.json")+"\n")
}
