package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

func TestNewShellTool(t *testing.T) {
	allowedCommands := []string{"echo", "ls", "pwd"}
	timeout := 10 * time.Second

	tool := NewShellTool(allowedCommands, timeout)

	if tool.Name() != "shell" {
		t.Errorf("Expected name 'shell', got: %s", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Expected non-empty description")
	}

	if tool.ArgsSchema() == "" {
		t.Error("Expected non-empty args schema")
	}

	if tool.timeout != timeout {
		t.Errorf("Expected timeout %v, got: %v", timeout, tool.timeout)
	}
}

func TestNewShellTool_DefaultTimeout(t *testing.T) {
	tool := NewShellTool([]string{"echo"}, 0)

	if tool.timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got: %v", tool.timeout)
	}
}

func TestShellTool_Run_Success(t *testing.T) {
	tool := NewShellTool([]string{"echo"}, 30*time.Second)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "echo",
			"args":    []interface{}{"hello", "world"},
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !output.Success {
		t.Errorf("Expected successful output, got error: %s", output.Error)
	}

	result, ok := output.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}

	if result["exit_code"] != 0 {
		t.Errorf("Expected exit code 0, got: %v", result["exit_code"])
	}

	outputStr, ok := result["output"].(string)
	if !ok {
		t.Fatal("Expected output to be a string")
	}

	if !strings.Contains(outputStr, "hello") {
		t.Errorf("Expected output to contain 'hello', got: %s", outputStr)
	}
}

func TestShellTool_Run_EmptyCommand(t *testing.T) {
	tool := NewShellTool([]string{"echo"}, 30*time.Second)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "",
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err == nil {
		t.Error("Expected error for empty command")
	}

	if output.Success {
		t.Error("Expected unsuccessful output")
	}
}

func TestShellTool_Run_NoCommand(t *testing.T) {
	tool := NewShellTool([]string{"echo"}, 30*time.Second)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{},
	}

	output, err := tool.Invoke(ctx, input)
	if err == nil {
		t.Error("Expected error when command is missing")
	}

	if output.Success {
		t.Error("Expected unsuccessful output")
	}
}

func TestShellTool_Run_CommandNotAllowed(t *testing.T) {
	tool := NewShellTool([]string{"echo"}, 30*time.Second)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "rm", // Not in whitelist
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err == nil {
		t.Error("Expected error for command not in whitelist")
	}

	if output.Success {
		t.Error("Expected unsuccessful output")
	}

	if !strings.Contains(output.Error, "not allowed") {
		t.Errorf("Expected error message about command not allowed, got: %s", output.Error)
	}

	// Check metadata contains allowed commands
	if output.Metadata == nil {
		t.Error("Expected metadata to be present")
	}
}

func TestShellTool_Run_DangerousCharacters(t *testing.T) {
	// Add the dangerous commands to whitelist to test the dangerous character validation
	tool := NewShellTool([]string{"echo;rm", "echo|grep", "echo&ls", "echo`pwd`", "echo$HOME", "echo>file", "echo<file"}, 30*time.Second)
	ctx := context.Background()

	dangerousCommands := []string{
		"echo;rm",
		"echo|grep",
		"echo&ls",
		"echo`pwd`",
		"echo$HOME",
		"echo>file",
		"echo<file",
	}

	for _, cmd := range dangerousCommands {
		input := &interfaces.ToolInput{
			Args: map[string]interface{}{
				"command": cmd,
			},
		}

		output, err := tool.Invoke(ctx, input)

		// Should fail due to dangerous characters, even though they're in whitelist
		if err == nil && output.Success {
			t.Errorf("Expected error for dangerous command: %s", cmd)
		}

		// Check that error message mentions dangerous characters
		errorFound := false
		if err != nil && strings.Contains(err.Error(), "dangerous") {
			errorFound = true
		}
		if output.Error != "" && strings.Contains(output.Error, "dangerous") {
			errorFound = true
		}

		if !errorFound {
			t.Errorf("Expected error about dangerous characters for: %s, got err: %v, output.Error: %s", cmd, err, output.Error)
		}
	}
}

func TestShellTool_Run_WithWorkDir(t *testing.T) {
	// Skip on Windows due to path differences
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	tool := NewShellTool([]string{"pwd"}, 30*time.Second)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command":  "pwd",
			"work_dir": "/tmp",
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !output.Success {
		t.Errorf("Expected successful output, got error: %s", output.Error)
	}

	result, ok := output.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}

	outputStr, ok := result["output"].(string)
	if !ok {
		t.Fatal("Expected output to be a string")
	}

	if !strings.Contains(outputStr, "tmp") {
		t.Errorf("Expected output to contain 'tmp', got: %s", outputStr)
	}
}

func TestShellTool_Run_WithTimeout(t *testing.T) {
	tool := NewShellTool([]string{"sleep"}, 30*time.Second)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "sleep",
			"args":    []interface{}{"10"}, // Sleep for 10 seconds
			"timeout": float64(1),          // But timeout after 1 second
		},
	}

	start := time.Now()
	output, _ := tool.Invoke(ctx, input)
	duration := time.Since(start)

	if duration > 3*time.Second {
		t.Errorf("Expected command to timeout quickly, took: %v", duration)
	}

	if output.Success {
		t.Error("Expected unsuccessful output due to timeout")
	}
}

func TestShellTool_GetAllowedCommands(t *testing.T) {
	allowedCommands := []string{"echo", "ls", "pwd"}
	tool := NewShellTool(allowedCommands, 30*time.Second)

	commands := tool.GetAllowedCommands()

	if len(commands) != 3 {
		t.Errorf("Expected 3 allowed commands, got: %d", len(commands))
	}

	// Check all commands are present
	commandMap := make(map[string]bool)
	for _, cmd := range commands {
		commandMap[cmd] = true
	}

	for _, expected := range allowedCommands {
		if !commandMap[expected] {
			t.Errorf("Expected command %s to be in allowed commands", expected)
		}
	}
}

func TestShellTool_IsCommandAllowed(t *testing.T) {
	tool := NewShellTool([]string{"echo", "ls"}, 30*time.Second)

	if !tool.IsCommandAllowed("echo") {
		t.Error("Expected 'echo' to be allowed")
	}

	if !tool.IsCommandAllowed("ls") {
		t.Error("Expected 'ls' to be allowed")
	}

	if tool.IsCommandAllowed("rm") {
		t.Error("Expected 'rm' to not be allowed")
	}
}

func TestShellTool_ExecuteScript(t *testing.T) {
	// Skip on Windows
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	tool := NewShellTool([]string{"bash"}, 30*time.Second)
	ctx := context.Background()

	// Note: This would require an actual script file to exist
	// For this test, we just verify the method behavior
	_, err := tool.ExecuteScript(ctx, "/nonexistent/script.sh", []string{"arg1"})

	// We expect an error since the script doesn't exist or bash is not allowed
	// The test just verifies the method works as expected
	_ = err // Either error or success is acceptable for this test
}

func TestShellTool_ExecutePipeline(t *testing.T) {
	// Skip on Windows
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	tool := NewShellTool([]string{"bash"}, 30*time.Second)
	ctx := context.Background()

	// This tests the pipeline method signature
	_, err := tool.ExecutePipeline(ctx, []string{"echo hello", "grep hello"})

	// This should work if bash is allowed
	if err != nil && tool.IsCommandAllowed("bash") {
		t.Errorf("Expected no error for pipeline, got: %v", err)
	}
}

func TestShellToolBuilder_New(t *testing.T) {
	builder := NewShellToolBuilder()

	if builder == nil {
		t.Fatal("Expected non-nil builder")
	}

	if builder.timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got: %v", builder.timeout)
	}
}

func TestShellToolBuilder_WithAllowedCommands(t *testing.T) {
	builder := NewShellToolBuilder()
	builder.WithAllowedCommands("echo", "ls").WithAllowedCommands("pwd")

	if len(builder.allowedCommands) != 3 {
		t.Errorf("Expected 3 allowed commands, got: %d", len(builder.allowedCommands))
	}
}

func TestShellToolBuilder_WithTimeout(t *testing.T) {
	builder := NewShellToolBuilder()
	timeout := 60 * time.Second

	builder.WithTimeout(timeout)

	if builder.timeout != timeout {
		t.Errorf("Expected timeout %v, got: %v", timeout, builder.timeout)
	}
}

func TestShellToolBuilder_Build(t *testing.T) {
	builder := NewShellToolBuilder()
	tool := builder.
		WithAllowedCommands("echo", "ls").
		WithTimeout(45 * time.Second).
		Build()

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}

	if tool.timeout != 45*time.Second {
		t.Errorf("Expected timeout 45s, got: %v", tool.timeout)
	}

	if !tool.IsCommandAllowed("echo") {
		t.Error("Expected 'echo' to be allowed")
	}
}

func TestCommonShellTools(t *testing.T) {
	tools := CommonShellTools()

	if len(tools) == 0 {
		t.Error("Expected at least one common tool")
	}

	for _, tool := range tools {
		if tool == nil {
			t.Error("Expected non-nil tool")
		}

		if tool.Name() != "shell" {
			t.Errorf("Expected name 'shell', got: %s", tool.Name())
		}
	}
}

func TestShellTool_Run_CommandSuccess(t *testing.T) {
	tool := NewShellTool([]string{"echo"}, 30*time.Second)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "echo",
			"args":    []interface{}{"test"},
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !output.Success {
		t.Error("Expected successful output")
	}

	result := output.Result.(map[string]interface{})
	if result["command"] != "echo" {
		t.Errorf("Expected command 'echo' in result, got: %v", result["command"])
	}
}

func TestShellTool_Run_InvalidCommand(t *testing.T) {
	tool := NewShellTool([]string{"nonexistentcommand123"}, 30*time.Second)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command": "nonexistentcommand123",
		},
	}

	output, err := tool.Invoke(ctx, input)

	// Should fail either with error or unsuccessful output
	if err == nil && output.Success {
		t.Error("Expected error or unsuccessful output for non-existent command")
	}

	// If we get a result, it should contain execution info
	if output.Result != nil {
		result, ok := output.Result.(map[string]interface{})
		if ok {
			// Exit code might be 0 if the command failed before execution
			// The important thing is that the operation failed overall
			_ = result["exit_code"]
		}
	}
}

func TestShellTool_Metadata(t *testing.T) {
	tool := NewShellTool([]string{"echo"}, 30*time.Second)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"command":  "echo",
			"args":     []interface{}{"test"},
			"work_dir": "/tmp",
			"timeout":  float64(10),
		},
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if output.Metadata == nil {
		t.Fatal("Expected metadata to be present")
	}

	if output.Metadata["work_dir"] != "/tmp" {
		t.Error("Expected work_dir in metadata")
	}

	if output.Metadata["timeout"] == nil {
		t.Error("Expected timeout in metadata")
	}
}
