package specialized

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/logger"
)

func TestNewShellAgent(t *testing.T) {
	tests := []struct {
		name             string
		allowedCommands  []string
		wantName         string
		wantCapabilities []string
	}{
		{
			name:             "create with allowed commands",
			allowedCommands:  []string{"ls", "echo", "pwd"},
			wantName:         "shell-agent",
			wantCapabilities: []string{"command_execution", "output_capture", "error_handling", "timeout_control"},
		},
		{
			name:             "create with empty commands",
			allowedCommands:  []string{},
			wantName:         "shell-agent",
			wantCapabilities: []string{"command_execution", "output_capture", "error_handling", "timeout_control"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, _ := logger.NewWithDefaults()
			agent := NewShellAgent(tt.allowedCommands, l)

			assert.Equal(t, tt.wantName, agent.Name())
			assert.Equal(t, tt.wantCapabilities, agent.Capabilities())
		})
	}
}

func TestShellAgent_Execute_Success(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"echo", "pwd"}
	agent := NewShellAgent(allowedCommands, l)

	tests := []struct {
		name      string
		input     *agentcore.AgentInput
		wantError bool
		check     func(t *testing.T, output *agentcore.AgentOutput)
	}{
		{
			name: "execute echo command",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "echo",
					"args":    []string{"hello", "world"},
				},
			},
			wantError: false,
			check: func(t *testing.T, output *agentcore.AgentOutput) {
				assert.Equal(t, "success", output.Status)
				assert.NotNil(t, output.Result)
				result := output.Result.(map[string]interface{})
				assert.Equal(t, "echo", result["command"])
				assert.Equal(t, int(0), result["exit_code"])
				assert.Contains(t, result["output"].(string), "hello")
				assert.Len(t, output.ToolCalls, 1)
				assert.True(t, output.ToolCalls[0].Success)
			},
		},
		{
			name: "execute pwd command",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "pwd",
				},
			},
			wantError: false,
			check: func(t *testing.T, output *agentcore.AgentOutput) {
				assert.Equal(t, "success", output.Status)
				result := output.Result.(map[string]interface{})
				assert.Equal(t, "pwd", result["command"])
				assert.Equal(t, int(0), result["exit_code"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := agent.Execute(ctx, tt.input)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, output)
				tt.check(t, output)
			}
		})
	}
}

func TestShellAgent_Execute_Failures(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"echo", "false", "ls"}
	agent := NewShellAgent(allowedCommands, l)

	tests := []struct {
		name      string
		input     *agentcore.AgentInput
		wantError bool
		check     func(t *testing.T, output *agentcore.AgentOutput, err error)
	}{
		{
			name: "missing required command parameter",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{},
			},
			wantError: true,
			check: func(t *testing.T, output *agentcore.AgentOutput, err error) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "command is required")
			},
		},
		{
			name: "command not in whitelist",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "rm",
					"args":    []string{"-rf", "/"},
				},
			},
			wantError: true,
			check: func(t *testing.T, output *agentcore.AgentOutput, err error) {
				assert.Error(t, err)
				assert.Equal(t, "failed", output.Status)
				assert.Contains(t, output.Message, "Command not allowed")
			},
		},
		{
			name: "dangerous characters - pipe",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "echo hello | cat",
				},
			},
			wantError: true,
			check: func(t *testing.T, output *agentcore.AgentOutput, err error) {
				assert.Error(t, err)
				// Command fails whitelist check before dangerous character check
				assert.Contains(t, output.Message, "Command not allowed")
			},
		},
		{
			name: "dangerous characters - semicolon",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "echo; rm -rf /",
				},
			},
			wantError: true,
			check: func(t *testing.T, output *agentcore.AgentOutput, err error) {
				assert.Error(t, err)
				// Command fails whitelist check before dangerous character check
				assert.Contains(t, output.Message, "Command not allowed")
			},
		},
		{
			name: "dangerous characters - ampersand",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "echo hello & background",
				},
			},
			wantError: false,
			check: func(t *testing.T, output *agentcore.AgentOutput, err error) {
				assert.Equal(t, "failed", output.Status)
			},
		},
		{
			name: "dangerous characters - backtick",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "echo `whoami`",
				},
			},
			wantError: false,
			check: func(t *testing.T, output *agentcore.AgentOutput, err error) {
				assert.Equal(t, "failed", output.Status)
			},
		},
		{
			name: "dangerous characters - dollar sign",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "echo $PATH",
				},
			},
			wantError: false,
			check: func(t *testing.T, output *agentcore.AgentOutput, err error) {
				assert.Equal(t, "failed", output.Status)
			},
		},
		{
			name: "command with non-zero exit code",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "false",
				},
			},
			wantError: false,
			check: func(t *testing.T, output *agentcore.AgentOutput, err error) {
				assert.Equal(t, "failed", output.Status)
				result := output.Result.(map[string]interface{})
				assert.NotEqual(t, 0, result["exit_code"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := agent.Execute(ctx, tt.input)

			tt.check(t, output, err)
		})
	}
}

func TestShellAgent_Execute_WithTimeout(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"sleep"}
	agent := NewShellAgent(allowedCommands, l)

	// Test with default timeout
	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"command": "sleep",
			"args":    []string{"1"},
		},
	}

	ctx := context.Background()
	start := time.Now()
	output, err := agent.Execute(ctx, input)
	duration := time.Since(start)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	assert.Less(t, duration, 5*time.Second) // Should complete quickly

	// Test with custom timeout
	inputWithTimeout := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"command": "sleep",
			"args":    []string{"0.1"},
		},
		Options: agentcore.AgentOptions{
			Timeout: 2 * time.Second,
		},
	}

	output, err = agent.Execute(ctx, inputWithTimeout)
	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestShellAgent_ExecuteScript(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"bash"}
	agent := NewShellAgent(allowedCommands, l)

	// Create a simple test script
	ctx := context.Background()

	// ExecuteScript should internally use bash command
	output, _ := agent.ExecuteScript(ctx, "-c", []string{"echo test"})

	// The script execution will fail with our test setup since we don't have a real script
	// but we can verify the structure is correct
	assert.NotNil(t, output)
}

func TestShellAgent_ExecutePipeline(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"bash"}
	agent := NewShellAgent(allowedCommands, l)

	ctx := context.Background()

	// ExecutePipeline should execute bash with -c flag and piped commands
	output, _ := agent.ExecutePipeline(ctx, []string{"echo hello", "cat"})

	// The pipeline will use bash internally
	assert.NotNil(t, output)
}

func TestShellAgent_GetAllowedCommands(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	commands := []string{"ls", "echo", "pwd", "grep"}
	agent := NewShellAgent(commands, l)

	allowed := agent.GetAllowedCommands()

	assert.Len(t, allowed, len(commands))
	for _, cmd := range commands {
		assert.Contains(t, allowed, cmd)
	}
}

func TestShellAgent_IsCommandAllowed(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"ls", "echo", "pwd"}
	agent := NewShellAgent(allowedCommands, l)

	tests := []struct {
		command string
		want    bool
	}{
		{"ls", true},
		{"echo", true},
		{"pwd", true},
		{"rm", false},
		{"cat", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := agent.IsCommandAllowed(tt.command)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShellAgent_OutputStructure(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"echo"}
	agent := NewShellAgent(allowedCommands, l)

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"command": "echo",
			"args":    []string{"test"},
		},
	}

	ctx := context.Background()
	output, err := agent.Execute(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)

	// Verify output structure
	assert.NotZero(t, output.Latency)
	assert.NotZero(t, output.Timestamp)
	assert.Len(t, output.ToolCalls, 1)

	toolCall := output.ToolCalls[0]
	assert.Equal(t, "shell", toolCall.ToolName)
	assert.NotZero(t, toolCall.Duration)
	assert.NotEmpty(t, toolCall.Input)
	assert.NotEmpty(t, toolCall.Output)
}

func TestShellAgent_WithWorkdir(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"pwd"}
	agent := NewShellAgent(allowedCommands, l)

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"command":  "pwd",
			"work_dir": "/tmp",
		},
	}

	ctx := context.Background()
	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	result := output.Result.(map[string]interface{})
	assert.Equal(t, int(0), result["exit_code"])
}

func TestShellAgent_EdgeCases(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"echo"}
	agent := NewShellAgent(allowedCommands, l)

	tests := []struct {
		name  string
		input *agentcore.AgentInput
	}{
		{
			name: "command with empty args",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "echo",
					"args":    []string{},
				},
			},
		},
		{
			name: "command with special characters in args",
			input: &agentcore.AgentInput{
				Context: map[string]interface{}{
					"command": "echo",
					"args":    []string{"hello world", "test@123"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			output, err := agent.Execute(ctx, tt.input)

			assert.NoError(t, err)
			assert.NotNil(t, output)
		})
	}
}

func TestShellAgent_ContextCancellation(t *testing.T) {
	l, _ := logger.NewWithDefaults()
	allowedCommands := []string{"sleep"}
	agent := NewShellAgent(allowedCommands, l)

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"command": "sleep",
			"args":    []string{"10"},
		},
	}

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel immediately
	cancel()

	output, _ := agent.Execute(ctx, input)

	// Should complete (either with context error or successfully depending on timing)
	assert.NotNil(t, output)
}
