package shell

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// ShellTool Shell 命令执行工具
//
// 提供安全的 shell 命令执行能力，支持命令白名单
type ShellTool struct {
	*tools.BaseTool
	allowedCommands map[string]bool // 命令白名单
	timeout         time.Duration   // 默认超时时间
}

// NewShellTool 创建 Shell 工具
//
// Parameters:
//   - allowedCommands: 允许执行的命令列表（白名单）
//   - timeout: 默认超时时间
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
		tools.ToolShell,
		tools.DescShell,
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

// run 执行 shell 命令
func (s *ShellTool) run(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	// 解析参数
	command, ok := input.Args["command"].(string)
	if !ok || command == "" {
		return &interfaces.ToolOutput{
				Success: false,
				Error:   "command is required and must be a non-empty string",
			}, tools.NewToolError(s.Name(), "invalid input", agentErrors.New(agentErrors.CodeInvalidInput, "command is required").
				WithComponent("shell_tool").
				WithOperation("run"))
	}

	// 安全检查：命令白名单
	if !s.allowedCommands[command] {
		return &interfaces.ToolOutput{
				Success: false,
				Error:   "command not allowed: " + command,
				Metadata: map[string]interface{}{
					"allowed_commands": s.GetAllowedCommands(),
				},
			}, tools.NewToolError(s.Name(), "command not allowed", agentErrors.New(agentErrors.CodeToolValidation, "command not in whitelist").
				WithComponent("shell_tool").
				WithOperation("run").
				WithContext("command", command).
				WithContext("allowed_commands", s.GetAllowedCommands()))
	}

	// 解析参数
	var args []string
	if argsInterface, ok := input.Args["args"].([]interface{}); ok {
		args = make([]string, len(argsInterface))
		for i, arg := range argsInterface {
			args[i] = fmt.Sprint(arg)
		}
	}

	workDir, _ := input.Args["work_dir"].(string)

	// 解析超时
	timeout := s.timeout
	if timeoutSec, ok := input.Args["timeout"].(float64); ok {
		timeout = time.Duration(timeoutSec) * time.Second
	}

	// 应用超时
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Validate command to prevent shell injection
	if strings.Contains(command, ";") || strings.Contains(command, "|") ||
		strings.Contains(command, "&") || strings.Contains(command, "`") ||
		strings.Contains(command, "$") || strings.Contains(command, ">") ||
		strings.Contains(command, "<") {
		return &interfaces.ToolOutput{
				Result:  nil,
				Success: false,
				Error:   "command contains potentially dangerous characters",
				Metadata: map[string]interface{}{
					"tool_name": s.Name(),
					"exit_code": -1,
				},
			}, tools.NewToolError(s.Name(), "invalid command", agentErrors.New(agentErrors.CodeToolValidation, "command contains dangerous characters").
				WithComponent("shell_tool").
				WithOperation("run").
				WithContext("command", command))
	}

	// 构建命令
	cmd := exec.CommandContext(cmdCtx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// 执行命令
	startTime := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	outputStr := string(output)
	success := err == nil && exitCode == 0

	result := map[string]interface{}{
		"command":   command,
		"args":      args,
		"output":    outputStr,
		"exit_code": exitCode,
		"duration":  duration.String(),
	}

	if !success {
		errorMsg := fmt.Sprintf("command failed with exit code %d", exitCode)
		if err != nil && exitCode == 0 {
			errorMsg = err.Error()
		}

		return &interfaces.ToolOutput{
			Result:  result,
			Success: false,
			Error:   errorMsg,
			Metadata: map[string]interface{}{
				"work_dir": workDir,
				"timeout":  timeout.String(),
			},
		}, tools.NewToolError(s.Name(), "command execution failed", err)
	}

	return &interfaces.ToolOutput{
		Result:  result,
		Success: true,
		Metadata: map[string]interface{}{
			"work_dir": workDir,
			"timeout":  timeout.String(),
		},
	}, nil
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
func (s *ShellTool) ExecuteScript(ctx context.Context, scriptPath string, args []string) (*interfaces.ToolOutput, error) {
	return s.Invoke(ctx, &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "bash",
			"args":    append([]string{scriptPath}, args...),
		},
		Context: ctx,
	})
}

// ExecutePipeline 执行管道命令的便捷方法
func (s *ShellTool) ExecutePipeline(ctx context.Context, commands []string) (*interfaces.ToolOutput, error) {
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

// Build 构建工具
func (b *ShellToolBuilder) Build() *ShellTool {
	return NewShellTool(b.allowedCommands, b.timeout)
}

// CommonShellTools 创建常用的 Shell 工具集合
func CommonShellTools() []*ShellTool {
	return []*ShellTool{
		// 基础命令工具
		NewShellTool([]string{"ls", "pwd", "echo", "cat", "grep", "find"}, 30*time.Second),
		// Git 工具
		NewShellTool([]string{"git"}, 60*time.Second),
		// 网络工具
		NewShellTool([]string{"curl", "wget", "ping"}, 60*time.Second),
		// 系统信息工具
		NewShellTool([]string{"uname", "hostname", "whoami", "date"}, 10*time.Second),
	}
}
