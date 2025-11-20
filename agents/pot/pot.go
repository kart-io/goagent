package pot

import (
	"bytes"
	"context"
	"fmt"
	"github.com/kart-io/goagent/utils/json"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// Pre-compiled regular expressions for code extraction
var (
	// genericCodeBlockRegex matches code blocks without language specifier: ```\ncode```
	genericCodeBlockRegex = regexp.MustCompile("```\\s*\\n([\\s\\S]*?)```")
)

// PoTAgent implements Program-of-Thought reasoning pattern.
//
// Program-of-Thought (PoT) generates executable code to solve problems,
// especially useful for mathematical and algorithmic tasks. This agent:
// - Generates code in multiple languages (Python, JavaScript, Go)
// - Executes generated code safely
// - Interprets execution results
// - Handles errors and debugging
// - Supports iterative refinement
type PoTAgent struct {
	*agentcore.BaseAgent
	llm         llm.Client
	tools       []interfaces.Tool
	toolsByName map[string]interfaces.Tool
	config      PoTConfig

	// codeExtractors caches compiled regexes per language for performance
	codeExtractors sync.Map
}

// PoTConfig configuration for Program-of-Thought agent
type PoTConfig struct {
	Name        string            // Agent name
	Description string            // Agent description
	LLM         llm.Client        // LLM client
	Tools       []interfaces.Tool // Available tools (optional)

	// Code generation settings
	Language         string        // Primary language ("python", "javascript", "go")
	AllowedLanguages []string      // Languages allowed for code generation
	MaxCodeLength    int           // Maximum length of generated code
	ExecutionTimeout time.Duration // Timeout for code execution
	SafeMode         bool          // Enable safe execution mode
	AllowImports     []string      // Allowed imports/libraries

	// Execution settings
	PythonPath    string // Path to Python interpreter
	NodePath      string // Path to Node.js
	DockerImage   string // Docker image for sandboxed execution
	MaxIterations int    // Max refinement iterations
}

// CodeResult represents the result of code execution
type CodeResult struct {
	Output   string                 // Standard output
	Error    string                 // Error output
	ExitCode int                    // Exit code
	Data     map[string]interface{} // Parsed data (if JSON output)
	Duration time.Duration          // Execution time
}

// NewPoTAgent creates a new Program-of-Thought agent
func NewPoTAgent(config PoTConfig) *PoTAgent {
	if config.Language == "" {
		config.Language = "python"
	}
	if len(config.AllowedLanguages) == 0 {
		config.AllowedLanguages = []string{"python", "javascript", "go"}
	}
	if config.MaxCodeLength <= 0 {
		config.MaxCodeLength = 2000
	}
	if config.ExecutionTimeout <= 0 {
		config.ExecutionTimeout = 10 * time.Second
	}
	if config.MaxIterations <= 0 {
		config.MaxIterations = 3
	}
	if config.PythonPath == "" {
		config.PythonPath = "python3"
	}
	if config.NodePath == "" {
		config.NodePath = "node"
	}

	// Build tools map
	toolsByName := make(map[string]interfaces.Tool)
	for _, tool := range config.Tools {
		toolsByName[tool.Name()] = tool
	}

	capabilities := []string{"program_of_thought", "code_generation", "code_execution"}
	if len(config.Tools) > 0 {
		capabilities = append(capabilities, "tool_calling")
	}

	return &PoTAgent{
		BaseAgent:   agentcore.NewBaseAgent(config.Name, config.Description, capabilities),
		llm:         config.LLM,
		tools:       config.Tools,
		toolsByName: toolsByName,
		config:      config,
	}
}

// Invoke executes the Program-of-Thought reasoning
func (p *PoTAgent) Invoke(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	startTime := time.Now()

	// Trigger start callback
	if err := p.triggerOnStart(ctx, input); err != nil {
		return nil, err
	}

	// Initialize output
	output := &agentcore.AgentOutput{
		ReasoningSteps: make([]agentcore.ReasoningStep, 0),
		ToolCalls:      make([]agentcore.ToolCall, 0),
		Metadata:       make(map[string]interface{}),
	}

	// Generate and execute code iteratively
	var finalResult interface{}
	var finalCode string
	success := false

	for iteration := 0; iteration < p.config.MaxIterations && !success; iteration++ {
		// Generate code
		code, language, err := p.generateCode(ctx, input, finalResult)
		if err != nil {
			return p.handleError(ctx, output, "Code generation failed", err, startTime)
		}

		// Record code generation step
		output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
			Step:        iteration*2 + 1,
			Action:      fmt.Sprintf("Generate %s Code", language),
			Description: fmt.Sprintf("Iteration %d", iteration+1),
			Result:      p.formatCodeForDisplay(code, language),
			Duration:    time.Since(startTime) / time.Duration(iteration+1),
			Success:     true,
		})

		// Validate code
		if err := p.validateCode(code, language); err != nil {
			finalResult = fmt.Sprintf("Code validation failed: %v", err)
			continue
		}

		// Execute code
		execStart := time.Now()
		result, err := p.executeCode(ctx, code, language)

		// Record execution step
		output.ReasoningSteps = append(output.ReasoningSteps, agentcore.ReasoningStep{
			Step:        iteration*2 + 2,
			Action:      "Execute Code",
			Description: fmt.Sprintf("%s execution", language),
			Result:      p.formatExecutionResult(result),
			Duration:    time.Since(execStart),
			Success:     err == nil,
			Error:       p.errorString(err),
		})

		if err != nil {
			// Try to debug and fix
			if iteration < p.config.MaxIterations-1 {
				finalResult = p.debugError(ctx, code, err, result)
				continue
			}
			return p.handleError(ctx, output, "Code execution failed", err, startTime)
		}

		// Parse and validate result
		parsedResult, err := p.parseResult(result)
		if err == nil {
			finalResult = parsedResult
			finalCode = code
			success = true
		} else {
			finalResult = result.Output
			if iteration == p.config.MaxIterations-1 {
				success = true // Accept raw output on last iteration
			}
		}
	}

	// Build final answer
	finalAnswer := p.buildFinalAnswer(finalResult, finalCode)

	output.Status = "success"
	output.Result = finalAnswer
	output.Message = "Program-of-Thought reasoning completed"
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	// Add metadata
	output.Metadata["language"] = p.config.Language
	output.Metadata["iterations"] = p.config.MaxIterations
	output.Metadata["final_code"] = finalCode

	// Trigger finish callback
	if err := p.triggerOnFinish(ctx, output); err != nil {
		return nil, err
	}

	return output, nil
}

// generateCode generates code to solve the problem
func (p *PoTAgent) generateCode(ctx context.Context, input *agentcore.AgentInput, previousResult interface{}) (string, string, error) {
	language := p.selectLanguage(input.Task)

	var prompt string
	if previousResult == nil {
		prompt = p.buildInitialCodePrompt(input.Task, language)
	} else {
		prompt = p.buildRefinementPrompt(input.Task, previousResult, language)
	}

	messages := []llm.Message{
		llm.SystemMessage(p.getSystemPrompt(language)),
		llm.UserMessage(prompt),
	}

	llmResp, err := p.llm.Chat(ctx, messages)
	if err != nil {
		return "", language, err
	}

	// Extract code from response
	code := p.extractCode(llmResp.Content, language)

	// Ensure code doesn't exceed max length
	if len(code) > p.config.MaxCodeLength {
		code = code[:p.config.MaxCodeLength]
	}

	return code, language, nil
}

// buildInitialCodePrompt builds the prompt for initial code generation
func (p *PoTAgent) buildInitialCodePrompt(task string, language string) string {
	return fmt.Sprintf(`Write %s code to solve this problem:

%s

Requirements:
1. The code should be complete and executable
2. Print the final answer in a clear format
3. Include comments explaining the logic
4. Handle edge cases appropriately
5. The output should be parseable (preferably JSON for complex results)

Generate only the code, enclosed in triple backticks.`, language, task)
}

// buildRefinementPrompt builds the prompt for code refinement
func (p *PoTAgent) buildRefinementPrompt(task string, previousResult interface{}, language string) string {
	return fmt.Sprintf(`The previous code attempt had this result:
%v

Please refine the %s code to correctly solve:
%s

Fix any errors and ensure the code produces the correct output.
Generate only the improved code, enclosed in triple backticks.`, previousResult, language, task)
}

// getSystemPrompt returns language-specific system prompt
func (p *PoTAgent) getSystemPrompt(language string) string {
	base := "You are an expert programmer who solves problems by writing clean, efficient code."

	switch language {
	case "python":
		return base + " You write Python code following PEP 8 conventions."
	case "javascript":
		return base + " You write modern JavaScript (ES6+) code."
	case "go":
		return base + " You write idiomatic Go code following Go conventions."
	default:
		return base
	}
}

// selectLanguage selects the best language for the task
func (p *PoTAgent) selectLanguage(task string) string {
	taskLower := strings.ToLower(task)

	// Simple heuristics for language selection
	if strings.Contains(taskLower, "math") || strings.Contains(taskLower, "calculate") ||
		strings.Contains(taskLower, "statistic") || strings.Contains(taskLower, "numpy") {
		return "python"
	}

	if strings.Contains(taskLower, "web") || strings.Contains(taskLower, "json") ||
		strings.Contains(taskLower, "api") {
		return "javascript"
	}

	if strings.Contains(taskLower, "concurrent") || strings.Contains(taskLower, "parallel") ||
		strings.Contains(taskLower, "goroutine") {
		return "go"
	}

	// Default to configured language
	return p.config.Language
}

// extractCode extracts code from LLM response
// Uses cached compiled regexes for performance
func (p *PoTAgent) extractCode(response string, language string) string {
	// Try to extract code with language-specific pattern (cached)
	key := fmt.Sprintf("lang_%s", language)
	var langPattern *regexp.Regexp

	if cached, ok := p.codeExtractors.Load(key); ok {
		langPattern = cached.(*regexp.Regexp)
	} else {
		// Compile and cache the language-specific pattern
		pattern := fmt.Sprintf("```(?:%s)?\\s*\\n([\\s\\S]*?)```", regexp.QuoteMeta(language))
		langPattern = regexp.MustCompile(pattern)
		p.codeExtractors.Store(key, langPattern)
	}

	if matches := langPattern.FindStringSubmatch(response); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Fallback: try generic code blocks (pre-compiled at package level)
	if matches := genericCodeBlockRegex.FindStringSubmatch(response); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Last resort: return the entire response
	return strings.TrimSpace(response)
}

// validateCode performs basic validation on generated code
func (p *PoTAgent) validateCode(code string, language string) error {
	// Check for empty code
	if strings.TrimSpace(code) == "" {
		return agentErrors.New(agentErrors.CodeParserFailed, "generated code is empty").
			WithComponent("pot_agent").
			WithOperation("validateCode")
	}

	// Language-specific validation
	switch language {
	case "python":
		return p.validatePythonCode(code)
	case "javascript":
		return p.validateJavaScriptCode(code)
	case "go":
		return p.validateGoCode(code)
	default:
		return nil
	}
}

// validatePythonCode validates Python code
func (p *PoTAgent) validatePythonCode(code string) error {
	if p.config.SafeMode {
		// Check for dangerous imports
		dangerousImports := []string{"os", "subprocess", "eval", "exec", "__import__"}
		for _, imp := range dangerousImports {
			if strings.Contains(code, imp) && !p.isAllowedImport(imp) {
				return agentErrors.New(agentErrors.CodeInvalidInput, "unsafe import detected").
					WithComponent("pot_agent").
					WithOperation("validateCode").
					WithContext("import", imp)
			}
		}
	}

	// Basic syntax check (simplified)
	if strings.Count(code, "(") != strings.Count(code, ")") {
		return agentErrors.New(agentErrors.CodeInvalidInput, "unbalanced parentheses").
			WithComponent("pot_agent").
			WithOperation("validateCode")
	}

	return nil
}

// validateJavaScriptCode validates JavaScript code
func (p *PoTAgent) validateJavaScriptCode(code string) error {
	if p.config.SafeMode {
		// Check for dangerous functions
		dangerousFuncs := []string{"eval", "Function", "require('child_process')"}
		for _, fn := range dangerousFuncs {
			if strings.Contains(code, fn) {
				return agentErrors.New(agentErrors.CodeInvalidInput, "unsafe function detected").
					WithComponent("pot_agent").
					WithOperation("validateJavaScriptCode").
					WithContext("function", fn)
			}
		}
	}

	return nil
}

// validateGoCode validates Go code
func (p *PoTAgent) validateGoCode(code string) error {
	// For Go, we need a main function
	if !strings.Contains(code, "func main()") && !strings.Contains(code, "package main") {
		// Wrap in main function
		return agentErrors.New(agentErrors.CodeInvalidInput, "go code must have main function").
			WithComponent("pot_agent").
			WithOperation("validateGoCode")
	}

	return nil
}

// executeCode executes the generated code
func (p *PoTAgent) executeCode(ctx context.Context, code string, language string) (*CodeResult, error) {
	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, p.config.ExecutionTimeout)
	defer cancel()

	switch language {
	case "python":
		return p.executePython(execCtx, code)
	case "javascript":
		return p.executeJavaScript(execCtx, code)
	case "go":
		return p.executeGo(execCtx, code)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported language").
			WithComponent("pot_agent").
			WithOperation("executeCode").
			WithContext("language", language)
	}
}

// executePython executes Python code
func (p *PoTAgent) executePython(ctx context.Context, code string) (*CodeResult, error) {
	cmd := exec.CommandContext(ctx, p.config.PythonPath, "-c", code)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	result := &CodeResult{
		Output:   stdout.String(),
		Error:    stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		}
		return result, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "python execution failed").
			WithComponent("pot_agent").
			WithOperation("executePython").
			WithContext("stderr", stderr.String())
	}

	return result, nil
}

// executeJavaScript executes JavaScript code
func (p *PoTAgent) executeJavaScript(ctx context.Context, code string) (*CodeResult, error) {
	cmd := exec.CommandContext(ctx, p.config.NodePath, "-e", code)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	result := &CodeResult{
		Output:   stdout.String(),
		Error:    stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		}
		return result, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "javascript execution failed").
			WithComponent("pot_agent").
			WithOperation("executeJavaScript").
			WithContext("stderr", stderr.String())
	}

	return result, nil
}

// executeGo executes Go code
func (p *PoTAgent) executeGo(ctx context.Context, code string) (*CodeResult, error) {
	// Go requires compilation, so we use go run with a temp file
	// This is simplified - in production, use proper temp file handling
	cmd := exec.CommandContext(ctx, "go", "run", "-", code)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(code)

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	result := &CodeResult{
		Output:   stdout.String(),
		Error:    stderr.String(),
		Duration: duration,
	}

	if err != nil {
		return result, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "go execution failed").
			WithComponent("pot_agent").
			WithOperation("executeGo").
			WithContext("stderr", result.Error)
	}

	return result, nil
}

// parseResult parses execution result
func (p *PoTAgent) parseResult(result *CodeResult) (interface{}, error) {
	output := strings.TrimSpace(result.Output)

	// Try to parse as JSON
	var jsonData interface{}
	if err := json.Unmarshal([]byte(output), &jsonData); err == nil {
		result.Data = map[string]interface{}{"result": jsonData}
		return jsonData, nil
	}

	// Try to parse as number
	if strings.Count(output, "\n") == 0 {
		// Single line output - might be a simple answer
		return output, nil
	}

	// Return raw output
	return output, nil
}

// debugError attempts to understand and fix code errors
func (p *PoTAgent) debugError(ctx context.Context, code string, err error, result *CodeResult) string {
	errorInfo := fmt.Sprintf("Error: %v\nStderr: %s\nStdout: %s",
		err, result.Error, result.Output)

	return fmt.Sprintf("Code execution failed with:\n%s\n\nPlease fix the code.", errorInfo)
}

// buildFinalAnswer constructs the final answer
func (p *PoTAgent) buildFinalAnswer(result interface{}, code string) string {
	return fmt.Sprintf("Solution found through program execution:\n\nResult: %v\n\nGenerated Code:\n```\n%s\n```",
		result, code)
}

// Helper methods

func (p *PoTAgent) isAllowedImport(imp string) bool {
	for _, allowed := range p.config.AllowImports {
		if allowed == imp {
			return true
		}
	}
	return false
}

func (p *PoTAgent) formatCodeForDisplay(code string, language string) string {
	lines := strings.Split(code, "\n")
	if len(lines) > 10 {
		return fmt.Sprintf("```%s\n%s\n... (%d more lines)\n```",
			language, strings.Join(lines[:10], "\n"), len(lines)-10)
	}
	return fmt.Sprintf("```%s\n%s\n```", language, code)
}

func (p *PoTAgent) formatExecutionResult(result *CodeResult) string {
	if result.Output != "" {
		return fmt.Sprintf("Output: %s (Duration: %v)", result.Output, result.Duration)
	}
	if result.Error != "" {
		return fmt.Sprintf("Error: %s (Exit code: %d)", result.Error, result.ExitCode)
	}
	return fmt.Sprintf("Completed in %v", result.Duration)
}

func (p *PoTAgent) errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Stream executes Program-of-Thought with streaming
func (p *PoTAgent) Stream(ctx context.Context, input *agentcore.AgentInput) (<-chan agentcore.StreamChunk[*agentcore.AgentOutput], error) {
	outChan := make(chan agentcore.StreamChunk[*agentcore.AgentOutput])

	go func() {
		defer close(outChan)

		output, err := p.Invoke(ctx, input)
		outChan <- agentcore.StreamChunk[*agentcore.AgentOutput]{
			Data:  output,
			Error: err,
			Done:  true,
		}
	}()

	return outChan, nil
}

// Error handling
func (p *PoTAgent) handleError(ctx context.Context, output *agentcore.AgentOutput, message string, err error, startTime time.Time) (*agentcore.AgentOutput, error) {
	output.Status = "failed"
	output.Message = message
	output.Timestamp = time.Now()
	output.Latency = time.Since(startTime)

	_ = p.triggerOnError(ctx, err)
	return output, err
}

// Callback triggers
func (p *PoTAgent) triggerOnStart(ctx context.Context, input *agentcore.AgentInput) error {
	config := p.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnStart(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (p *PoTAgent) triggerOnFinish(ctx context.Context, output *agentcore.AgentOutput) error {
	config := p.GetConfig()
	for _, cb := range config.Callbacks {
		if err := cb.OnAgentFinish(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (p *PoTAgent) triggerOnError(ctx context.Context, err error) error {
	config := p.GetConfig()
	for _, cb := range config.Callbacks {
		if cbErr := cb.OnError(ctx, err); cbErr != nil {
			return cbErr
		}
	}
	return nil
}

// WithCallbacks adds callback handlers
func (p *PoTAgent) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := &PoTAgent{
		BaseAgent:      p.BaseAgent.WithCallbacks(callbacks...).(*agentcore.BaseAgent),
		llm:            p.llm,
		tools:          p.tools,
		toolsByName:    p.toolsByName,
		config:         p.config,
		codeExtractors: sync.Map{}, // Explicit initialization - each instance gets fresh cache
	}
	return newAgent
}

// WithConfig configures the agent
func (p *PoTAgent) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*agentcore.AgentInput, *agentcore.AgentOutput] {
	newAgent := &PoTAgent{
		BaseAgent:      p.BaseAgent.WithConfig(config).(*agentcore.BaseAgent),
		llm:            p.llm,
		tools:          p.tools,
		toolsByName:    p.toolsByName,
		config:         p.config,
		codeExtractors: sync.Map{}, // Explicit initialization - each instance gets fresh cache
	}
	return newAgent
}
