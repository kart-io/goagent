package coding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

const maxLogCount = 100

// commandTool 经 DispatchTimeoutProvider 向 Dispatcher 声明比命令超时略长的时限，
// 让工具自己先报告超时并带回已产生的输出。
type commandTool struct {
	*tools.BaseTool
	timeout time.Duration
}

func (t *commandTool) DispatchTimeout() time.Duration { return t.timeout + 10*time.Second }

func (w *Workspace) commandTool(name, desc, schema string, run func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error)) interfaces.Tool {
	return &commandTool{BaseTool: tools.NewBaseTool(name, desc, schema, run), timeout: w.commandTimeout}
}

type runResult struct {
	output    string
	truncated bool
	exitCode  int
	timedOut  bool
	duration  time.Duration
}

// execute 以工作区命令超时执行命令。超时记为 timedOut 而非错误；调用方 ctx 结束或命令无法启动时返回错误。
func (w *Workspace) execute(ctx context.Context, argv []string, dir string) (runResult, error) {
	runCtx, cancel := context.WithTimeout(ctx, w.commandTimeout)
	defer cancel()
	start := time.Now()
	res, err := w.exec.Run(runCtx, env.Command{Argv: argv, Dir: dir})
	r := runResult{exitCode: res.ExitCode, duration: time.Since(start)}
	r.output, r.truncated = truncateOutput(res.Output, w.maxOutputBytes)
	if err != nil {
		if ctx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			r.timedOut, r.exitCode = true, -1
			return r, nil
		}
		return r, err
	}
	return r, nil
}

// truncateOutput 超出 limit 时保留首尾各一半：编译错误多在开头，测试结论多在结尾。
func truncateOutput(out []byte, limit int) (string, bool) {
	if len(out) <= limit {
		return strings.ToValidUTF8(string(out), "�"), false
	}
	half := limit / 2
	head := strings.ToValidUTF8(string(out[:half]), "")
	tail := strings.ToValidUTF8(string(out[len(out)-half:]), "")
	return fmt.Sprintf("%s\n... [省略 %d 字节] ...\n%s", head, len(out)-2*half, tail), true
}

func newRunCommandTool(w *Workspace) interfaces.Tool {
	return w.commandTool(ToolRunCommand,
		fmt.Sprintf("在工作区内用 sh -c 执行 shell 命令（如构建、测试、格式化），返回合并后的 stdout/stderr 与退出码。非零退出码是正常结果，请阅读输出后继续。单次超时 %s，输出过长时保留首尾。读写和搜索文件请使用专用工具。", w.commandTimeout),
		`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "要执行的 shell 命令"},
				"work_dir": {"type": "string", "description": "相对工作区根目录的执行目录，默认根目录"}
			},
			"required": ["command"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolRunCommand).WithOperation("run")
			command := stringArg(input.Args, "command")
			if strings.TrimSpace(command) == "" {
				return resp.ValidationError("command is required", "field", "command")
			}
			dir := "."
			if p := stringArg(input.Args, "work_dir"); p != "" {
				rel, err := w.fs.Resolve(p)
				if err != nil {
					return resp.ValidationError(err.Error(), "field", "work_dir")
				}
				if info, err := w.fs.Stat(ctx, rel); err != nil || !info.IsDir() {
					return resp.ValidationError(rel+" is not an existing directory", "field", "work_dir")
				}
				dir = rel
			}
			r, err := w.execute(ctx, []string{"sh", "-c", command}, dir)
			if err != nil {
				return resp.ExecutionError("command failed to run", nil, err)
			}
			return resp.Success(map[string]any{
				"exit_code": r.exitCode,
				"output":    r.output,
				"truncated": r.truncated,
				"timed_out": r.timedOut,
				"duration":  r.duration.Round(time.Millisecond).String(),
			}, nil)
		})
}

// git 在工作区根目录执行 git。git 非零退出（如不是仓库、没有可提交的改动）作为业务拒绝交给模型。
// fail 或 err 非空时调用方应直接返回二者。
func (w *Workspace) git(ctx context.Context, resp *tools.ToolErrorResponse, args ...string) (out string, fail *interfaces.ToolOutput, err error) {
	// 关闭 fsmonitor 与 hooks：仓库配置里的命令不应经由无需审批的 git 工具执行。
	argv := append([]string{"git", "-c", "core.quotepath=false", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}, args...)
	r, err := w.execute(ctx, argv, ".")
	if err != nil {
		fail, err = resp.ExecutionError("git failed to run", nil, err)
		return "", fail, err
	}
	if r.timedOut {
		return "", tools.Rejected(fmt.Sprintf("git %s timed out after %s", args[0], w.commandTimeout), map[string]any{"output": r.output}), nil
	}
	if r.exitCode != 0 {
		return "", tools.Rejected(fmt.Sprintf("git %s exited with code %d: %s", args[0], r.exitCode, strings.TrimSpace(r.output)), nil), nil
	}
	return r.output, nil, nil
}

// gitPaths 把模型给出的路径解析为工作区内的相对路径。
func (w *Workspace) gitPaths(args map[string]any) ([]string, error) {
	paths, err := stringsArg(args, "paths")
	if err != nil {
		return nil, err
	}
	rels := make([]string, len(paths))
	for i, p := range paths {
		rel, err := w.fs.Resolve(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		rels[i] = rel
	}
	return rels, nil
}

const pathsSchema = `"paths": {"type": "array", "items": {"type": "string"}, "description": "限定的文件或目录，相对工作区根目录"}`

func newGitStatusTool(w *Workspace) interfaces.Tool {
	return w.commandTool(ToolGitStatus,
		"查看 git 工作区状态：当前分支以及已暂存、未暂存和未跟踪的文件（git status --short --branch）。",
		`{"type": "object", "properties": {}}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolGitStatus).WithOperation("status")
			out, fail, err := w.git(ctx, resp, "status", "--short", "--branch")
			if fail != nil || err != nil {
				return fail, err
			}
			return resp.Success(map[string]any{"status": out}, nil)
		})
}

func newGitDiffTool(w *Workspace) interfaces.Tool {
	return w.commandTool(ToolGitDiff,
		"查看 git 差异。默认显示未暂存的改动，staged 为 true 时显示已暂存、待提交的改动。",
		`{
			"type": "object",
			"properties": {
				"staged": {"type": "boolean", "description": "是否查看已暂存的改动，默认 false"},
				`+pathsSchema+`
			}
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolGitDiff).WithOperation("diff")
			paths, err := w.gitPaths(input.Args)
			if err != nil {
				return resp.ValidationError(err.Error(), "field", "paths")
			}
			args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv"}
			if boolArg(input.Args, "staged") {
				args = append(args, "--cached")
			}
			out, fail, err := w.git(ctx, resp, append(append(args, "--"), paths...)...)
			if fail != nil || err != nil {
				return fail, err
			}
			return resp.Success(map[string]any{"diff": out}, nil)
		})
}

func newGitLogTool(w *Workspace) interfaces.Tool {
	return w.commandTool(ToolGitLog,
		"查看最近的 git 提交，每行一个：短哈希、日期、作者、标题。",
		`{
			"type": "object",
			"properties": {
				"max_count": {"type": "integer", "minimum": 1, "maximum": 100, "description": "返回的提交数，默认 10"},
				`+pathsSchema+`
			}
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolGitLog).WithOperation("log")
			n, err := intArg(input.Args, "max_count", 10)
			if err != nil || n < 1 || n > maxLogCount {
				return resp.ValidationError("max_count must be an integer between 1 and 100", "field", "max_count")
			}
			paths, err := w.gitPaths(input.Args)
			if err != nil {
				return resp.ValidationError(err.Error(), "field", "paths")
			}
			args := []string{"log", fmt.Sprintf("-n%d", n), "--date=short", "--pretty=format:%h %ad %an %s", "--"}
			out, fail, err := w.git(ctx, resp, append(args, paths...)...)
			if fail != nil || err != nil {
				return fail, err
			}
			return resp.Success(map[string]any{"log": out}, nil)
		})
}

func newGitCommitTool(w *Workspace) interfaces.Tool {
	return w.commandTool(ToolGitCommit,
		"创建 git 提交。给出 paths 时先暂存这些路径；all 为 true 时暂存全部改动（含新文件和删除）；两者都不给时只提交已暂存的改动。返回新提交的哈希。",
		`{
			"type": "object",
			"properties": {
				"message": {"type": "string", "description": "提交信息"},
				"all": {"type": "boolean", "description": "是否暂存全部改动后提交，默认 false"},
				`+pathsSchema+`
			},
			"required": ["message"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolGitCommit).WithOperation("commit")
			message := stringArg(input.Args, "message")
			if strings.TrimSpace(message) == "" {
				return resp.ValidationError("message is required", "field", "message")
			}
			paths, err := w.gitPaths(input.Args)
			if err != nil {
				return resp.ValidationError(err.Error(), "field", "paths")
			}
			all := boolArg(input.Args, "all")
			if all && len(paths) > 0 {
				return resp.ValidationError("set either all or paths, not both")
			}

			var add []string
			switch {
			case all:
				add = []string{"add", "-A"}
			case len(paths) > 0:
				add = append([]string{"add", "--"}, paths...)
			}
			if add != nil {
				if _, fail, err := w.git(ctx, resp, add...); fail != nil || err != nil {
					return fail, err
				}
			}
			out, fail, err := w.git(ctx, resp, "commit", "-m", message)
			if fail != nil || err != nil {
				return fail, err
			}
			hash, fail, err := w.git(ctx, resp, "rev-parse", "HEAD")
			if fail != nil || err != nil {
				return fail, err
			}
			return resp.Success(map[string]any{"commit": strings.TrimSpace(hash), "output": out}, nil)
		})
}
