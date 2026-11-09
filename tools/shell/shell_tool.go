package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	shared "github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/internal/processgroup"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/toolargs"
)

// ShellTool Shell 命令执行工具
//
// 提供安全的 shell 命令执行能力，支持命令白名单
type ShellTool struct {
	*tools.BaseTool
	allowedCommands map[string]bool // 命令白名单
	timeout         time.Duration   // 默认超时时间
	rootDir         string          // 工作目录根，空表示不限制
}

// NewShellTool 创建 Shell 工具
//
// 白名单只校验命令名，命令的全部参数由模型决定：find -exec、git -c、curl -o、env、xargs、解释器等
// 可经参数执行任意程序，列入白名单即等于放行任意执行。需要按参数收窄时配合 tools/policy。
//
// Parameters:
//   - allowedCommands: 允许执行的命令列表（白名单）
//   - timeout: 超时时间，也是调用方可指定超时的上限
func NewShellTool(allowedCommands []string, timeout time.Duration) *ShellTool {
	whitelist := make(map[string]bool)
	for _, cmd := range allowedCommands {
		whitelist[cmd] = true
	}

	if timeout == 0 {
		timeout = 30 * time.Second
	}

	tool := &ShellTool{
		allowedCommands: whitelist,
		timeout:         timeout,
	}

	tool.BaseTool = tools.NewBaseTool(
		shared.ToolShell,
		shared.DescShell,
		`{
			"type": "object",
			"properties": {
				"command": {
					"type": "string",
					"description": "Command to execute (must be in whitelist)"
				},
				"args": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Command arguments"
				},
				"work_dir": {
					"type": "string",
					"description": "Working directory (optional)"
				},
				"timeout": {
					"type": "integer",
					"description": "Timeout in seconds (optional, default: 30)"
				}
			},
			"required": ["command"]
		}`,
		tool.run,
	)

	return tool
}

// shellMetachars 是 shell 会解释的字符，换行在 bash -c 中等同于命令分隔符
const shellMetachars = ";|&`$><\n\r"

// maxOutputBytes 是保留的命令输出上限，超出部分丢弃。
const maxOutputBytes = 1 << 20

// limitedBuffer 只保留前 maxOutputBytes 字节，其余照常消费，避免子进程因管道写满而阻塞。
type limitedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxOutputBytes - b.buf.Len(); len(p) > room {
		b.buf.Write(p[:room])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

// run 执行 shell 命令
func (s *ShellTool) run(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	resp := tools.NewToolErrorResponse(s.Name()).WithOperation("run")

	// 解析参数
	command, ok := input.Args["command"].(string)
	if !ok || command == "" {
		return resp.ValidationError("command is required and must be a non-empty string", "field", "command")
	}

	// 安全检查：命令白名单
	if !s.allowedCommands[command] {
		output, err := resp.ValidationError("command not allowed: "+command,
			"command", command,
			"allowed_commands", s.GetAllowedCommands())
		// 添加 Metadata 以保持向后兼容性
		output.Metadata = map[string]interface{}{
			"allowed_commands": s.GetAllowedCommands(),
		}
		return output, err
	}

	// 解析参数
	// 模型经 JSON 传入 []interface{}，Go 调用方（ExecuteScript 等）传入 []string
	var args []string
	switch v := input.Args["args"].(type) {
	case []string:
		args = v
	case []interface{}:
		args = make([]string, len(v))
		for i, arg := range v {
			args[i] = fmt.Sprint(arg)
		}
	}

	workDir, _ := input.Args["work_dir"].(string)
	dir, err := s.resolveWorkDir(workDir)
	if err != nil {
		return resp.ValidationError(err.Error(), "work_dir", workDir)
	}

	// 解析超时：只能缩短，不能超过工具配置的超时
	timeout := s.timeout
	if v, ok := toolargs.Float64(input.Args["timeout"]); ok {
		timeout = time.Duration(v * float64(time.Second))
	} else if v, ok := input.Args["timeout"].(int); ok {
		timeout = time.Duration(v) * time.Second
	}
	if timeout <= 0 || timeout > s.timeout {
		timeout = s.timeout
	}

	// 应用超时
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Validate command to prevent shell injection
	if strings.ContainsAny(command, shellMetachars) {
		return resp.ValidationError("command contains potentially dangerous characters",
			"command", command,
			"dangerous_chars", strings.Split(shellMetachars, ""))
	}

	// 构建命令
	cmd := exec.CommandContext(cmdCtx, command, args...)
	processgroup.Configure(cmd)
	cmd.WaitDelay = time.Second
	defer func() { _ = processgroup.Kill(cmd) }()
	cmd.Dir = dir

	// 执行命令
	var output limitedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	// 父命令已退出但后台进程仍持有输出管道时，按父命令退出码处理，再清理后代。
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil {
		err = nil
		if exitCode != 0 {
			err = &exec.ExitError{ProcessState: cmd.ProcessState}
		}
	}
	if ctxErr := cmdCtx.Err(); ctxErr != nil && !errors.Is(err, ctxErr) {
		err = errors.Join(err, ctxErr)
	}

	outputStr := output.buf.String()
	success := err == nil && exitCode == 0

	result := map[string]interface{}{
		"command":   command,
		"args":      args,
		"output":    outputStr,
		"exit_code": exitCode,
		"duration":  duration.String(),
		"truncated": output.truncated,
	}

	if !success {
		// 命令执行失败但有输出，返回部分结果
		errorMsg := fmt.Sprintf("command failed with exit code %d", exitCode)
		if err != nil && exitCode == 0 {
			errorMsg = err.Error()
		}
		return resp.ExecutionError(errorMsg, result, err)
	}

	return resp.Success(result, map[string]interface{}{
		"work_dir": workDir,
		"timeout":  timeout.String(),
	})
}

// resolveWorkDir 返回命令的工作目录：未设根目录时原样使用 work_dir；设了根目录时相对路径基于根目录解析，
// 解析符号链接后必须仍在根目录内。
func (s *ShellTool) resolveWorkDir(workDir string) (string, error) {
	if s.rootDir == "" {
		return workDir, nil
	}
	dir := workDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(s.rootDir, dir)
	}
	root, err := filepath.EvalSymlinks(s.rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve root dir: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve work_dir: %w", err)
	}
	if rel, err := filepath.Rel(root, resolved); err != nil || !filepath.IsLocal(rel) && rel != "." {
		return "", agentErrors.Newf(agentErrors.CodeToolValidation, "work_dir must stay inside the root dir")
	}
	return resolved, nil
}

// GetAllowedCommands 获取允许的命令列表
func (s *ShellTool) GetAllowedCommands() []string {
	commands := make([]string, 0, len(s.allowedCommands))
	for cmd := range s.allowedCommands {
		commands = append(commands, cmd)
	}
	return commands
}

// IsCommandAllowed 检查命令是否在白名单中
func (s *ShellTool) IsCommandAllowed(command string) bool {
	return s.allowedCommands[command]
}

// ExecuteScript 执行脚本的便捷方法
// 注意：此方法要求 "bash" 命令必须在白名单中
func (s *ShellTool) ExecuteScript(ctx context.Context, scriptPath string, args []string) (*interfaces.ToolOutput, error) {
	resp := tools.NewToolErrorResponse(s.Name()).WithOperation("ExecuteScript")

	// 安全检查：验证 bash 命令是否在白名单中
	if !s.allowedCommands["bash"] && !s.allowedCommands["sh"] {
		output, err := resp.ValidationError("bash or sh command must be in whitelist to execute scripts",
			"allowed_commands", s.GetAllowedCommands())
		// 添加 Metadata 以保持向后兼容性
		output.Metadata = map[string]interface{}{
			"allowed_commands": s.GetAllowedCommands(),
		}
		return output, err
	}

	return s.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "bash",
			"args":    append([]string{scriptPath}, args...),
		},
		Context: ctx,
	})
}

// ExecutePipeline 执行管道命令的便捷方法
// 注意：此方法会验证管道中的每个命令是否在白名单中
func (s *ShellTool) ExecutePipeline(ctx context.Context, commands []string) (*interfaces.ToolOutput, error) {
	resp := tools.NewToolErrorResponse(s.Name()).WithOperation("ExecutePipeline")

	// 安全检查：验证每个命令都在白名单中
	for _, cmd := range commands {
		// 提取命令的第一个单词（实际的命令名）
		fields := strings.Fields(strings.TrimSpace(cmd))
		if len(fields) == 0 {
			return resp.ValidationError("empty command in pipeline")
		}

		// 管道经 bash -c 执行，参数中的元字符会绕过白名单
		if strings.ContainsAny(cmd, shellMetachars) {
			return resp.ValidationError("command in pipeline contains potentially dangerous characters",
				"command", cmd,
				"dangerous_chars", strings.Split(shellMetachars, ""))
		}

		cmdName := fields[0]
		if !s.allowedCommands[cmdName] {
			output, err := resp.ValidationError("command not allowed in pipeline: "+cmdName,
				"disallowed_command", cmdName,
				"allowed_commands", s.GetAllowedCommands())
			// 添加 Metadata 以保持向后兼容性
			output.Metadata = map[string]interface{}{
				"disallowed_command": cmdName,
				"allowed_commands":   s.GetAllowedCommands(),
			}
			return output, err
		}
	}

	// 所有命令都通过验证，构建管道
	pipeline := strings.Join(commands, " | ")
	return s.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "bash",
			"args":    []string{"-c", pipeline},
		},
		Context: ctx,
	})
}

// ShellToolBuilder Shell 工具构建器
type ShellToolBuilder struct {
	allowedCommands []string
	timeout         time.Duration
	rootDir         string
}

// NewShellToolBuilder 创建 Shell 工具构建器
func NewShellToolBuilder() *ShellToolBuilder {
	return &ShellToolBuilder{
		allowedCommands: []string{},
		timeout:         30 * time.Second,
	}
}

// WithAllowedCommands 设置允许的命令
func (b *ShellToolBuilder) WithAllowedCommands(commands ...string) *ShellToolBuilder {
	b.allowedCommands = append(b.allowedCommands, commands...)
	return b
}

// WithTimeout 设置超时
func (b *ShellToolBuilder) WithTimeout(timeout time.Duration) *ShellToolBuilder {
	b.timeout = timeout
	return b
}

// WithRootDir 把工作目录限制在 dir 内：未指定 work_dir 时在 dir 中执行，相对路径基于 dir 解析，
// 经 .. 或符号链接离开 dir 的 work_dir 被拒绝。它只约束工作目录，命令参数仍可引用任意绝对路径。
func (b *ShellToolBuilder) WithRootDir(dir string) *ShellToolBuilder {
	b.rootDir = dir
	return b
}

// Build 构建工具
func (b *ShellToolBuilder) Build() *ShellTool {
	tool := NewShellTool(b.allowedCommands, b.timeout)
	tool.rootDir = b.rootDir
	return tool
}

// CommonShellTools 创建常用的 Shell 工具集合
func CommonShellTools() []*ShellTool {
	return []*ShellTool{
		// 基础命令工具；find、git、curl、wget 可经参数执行任意程序或写任意文件，不作预设
		NewShellTool([]string{"ls", "pwd", "echo", "cat", "grep"}, 30*time.Second),
		// 系统信息工具
		NewShellTool([]string{"uname", "hostname", "whoami", "date"}, 10*time.Second),
	}
}
