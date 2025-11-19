package specialized

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/logger/core"
)

// ShellAgent Shell 命令执行 Agent
// 提供安全的 shell 命令执行能力
type ShellAgent struct {
	*agentcore.BaseAgent
	allowedCommands map[string]bool // 命令白名单
	logger          core.Logger
}

// NewShellAgent 创建 Shell Agent
func NewShellAgent(allowedCommands []string, logger core.Logger) *ShellAgent {
	whitelist := make(map[string]bool)
	for _, cmd := range allowedCommands {
		whitelist[cmd] = true
	}

	return &ShellAgent{
		BaseAgent: agentcore.NewBaseAgent(
			"shell-agent",
			"Executes whitelisted shell commands with timeout and security controls",
			[]string{
				"command_execution",
				"output_capture",
				"error_handling",
				"timeout_control",
			},
		),
		allowedCommands: whitelist,
		logger:          logger.With("agent", "shell"),
	}
}

// Execute 执行 shell 命令
func (a *ShellAgent) Execute(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	start := time.Now()

	// 解析参数
	command, ok := input.Context["command"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "command is required").
			WithComponent("shell_agent").
			WithOperation("Execute")
	}

	args, _ := input.Context["args"].([]string)
	workDir, _ := input.Context["work_dir"].(string)

	// 安全检查：命令白名单
	if !a.allowedCommands[command] {
		return &agentcore.AgentOutput{
				Status:    interfaces.StatusFailed,
				Message:   "Command not allowed",
				Latency:   time.Since(start),
				Timestamp: start,
			}, agentErrors.New(agentErrors.CodeInvalidInput, "command not allowed").
				WithComponent("shell_agent").
				WithOperation("Execute").
				WithContext("command", command)
	}

	// 应用超时
	timeout := input.Options.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	a.logger.Info("Executing shell command",
		"command", command,
		"args", args,
		"work_dir", workDir,
		"timeout", timeout)

	// Validate command to prevent shell injection
	if strings.Contains(command, ";") || strings.Contains(command, "|") ||
		strings.Contains(command, "&") || strings.Contains(command, "`") ||
		strings.Contains(command, "$") || strings.Contains(command, ">") ||
		strings.Contains(command, "<") {
		return &agentcore.AgentOutput{
			Status:  interfaces.StatusFailed,
			Message: "command contains potentially dangerous characters",
			Result: map[string]interface{}{
				"agent_name": a.Name(),
				"exit_code":  -1,
			},
			Latency:   time.Since(start),
			Timestamp: start,
		}, nil
	}

	// 构建命令
	cmd := exec.CommandContext(cmdCtx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// 执行命令
	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	outputStr := string(output)
	success := err == nil && exitCode == 0

	// 构建输出
	result := &agentcore.AgentOutput{
		Status: interfaces.StatusSuccess,
		Result: map[string]interface{}{
			"command":   command,
			"args":      args,
			"output":    outputStr,
			"exit_code": exitCode,
		},
		ToolCalls: []agentcore.ToolCall{
			{
				ToolName: "shell",
				Input: map[string]interface{}{
					"command": command,
					"args":    args,
				},
				Output: map[string]interface{}{
					"output":    outputStr,
					"exit_code": exitCode,
				},
				Duration: time.Since(start),
				Success:  success,
			},
		},
		Latency:   time.Since(start),
		Timestamp: start,
	}

	if !success {
		result.Status = interfaces.StatusFailed
		result.Message = fmt.Sprintf("Command failed with exit code %d", exitCode)
		if err != nil {
			result.ToolCalls[0].Error = err.Error()
		}
	} else {
		result.Message = "Command executed successfully"
	}

	a.logger.Info("Shell command completed",
		"command", command,
		"exit_code", exitCode,
		"duration", result.Latency)

	return result, nil
}

// ExecuteScript 执行脚本
func (a *ShellAgent) ExecuteScript(ctx context.Context, scriptPath string, args []string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"command": "bash",
			"args":    append([]string{scriptPath}, args...),
		},
	})
}

// ExecutePipeline 执行管道��令
func (a *ShellAgent) ExecutePipeline(ctx context.Context, commands []string) (*agentcore.AgentOutput, error) {
	pipeline := strings.Join(commands, " | ")
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"command": "bash",
			"args":    []string{"-c", pipeline},
		},
	})
}

// GetAllowedCommands 获取允许的命令列表
func (a *ShellAgent) GetAllowedCommands() []string {
	commands := make([]string, 0, len(a.allowedCommands))
	for cmd := range a.allowedCommands {
		commands = append(commands, cmd)
	}
	return commands
}

// IsCommandAllowed 检查命令是否在白名单中
func (a *ShellAgent) IsCommandAllowed(command string) bool {
	return a.allowedCommands[command]
}
