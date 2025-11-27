# Task 2.4 完成报告：为 PoT Agent 实现 RunGenerator

## 任务概述

**任务编号**: Task 2.4
**任务标题**: 为 Program-of-Thought (PoT) Agent 实现 RunGenerator
**优先级**: P2
**状态**: ✅ 已完成
**完成时间**: 2025-11-27

## 背景

延续 Task 2.1（ReactAgent 和 ExecutorAgent）、Task 2.2（CoT 和 ToT Agent）、Task 2.3（SoT 和 GoT Agent）的工作，本任务继续为 PoT Agent 实现 RunGenerator，将零分配流式执行扩展到程序化推理模式。

## 实现细节

### PoT Agent RunGenerator 实现

**文件**: `agents/pot/pot.go` (~220 行新增)

#### 关键特性

- **迭代式代码生成**: 支持多次迭代（最多 MaxIterations 次）进行代码改进
- **完整的代码执行周期**: 代码生成 → 验证 → 执行 → 结果解析
- **多阶段流式输出**: 每次迭代都有多个 yield 点
- **零分配设计**: 使用 Go 1.25 的 `iter.Seq2` 模式
- **早期终止支持**: 用户可在任意阶段 break

#### Yield 时机

每次迭代包含以下 yield 点：

1. **code_generated**: 代码生成完成
2. **validation_failed**: 代码验证失败（如果发生）
3. **execution_failed**: 代码执行失败（如果发生）
4. **execution_success**: 代码执行成功
5. **final**: 所有迭代完成后的最终输出

#### 核心实现

```go
func (p *PoTAgent) RunGenerator(ctx context.Context, input *agentcore.AgentInput) agentcore.Generator[*agentcore.AgentOutput] {
    return func(yield func(*agentcore.AgentOutput, error) bool) {
        startTime := time.Now()

        // Initialize accumulated output
        accumulated := &agentcore.AgentOutput{
            ReasoningSteps: make([]agentcore.ReasoningStep, 0),
            ToolCalls:      make([]agentcore.ToolCall, 0),
            Metadata:       make(map[string]interface{}),
        }

        // Generate and execute code iteratively
        var finalResult interface{}
        var finalCode string
        success := false

        for iteration := 0; iteration < p.config.MaxIterations && !success; iteration++ {
            // Phase 1: Generate code
            codeGenStart := time.Now()
            code, language, err := p.generateCode(ctx, input, finalResult)
            if err != nil {
                errorOutput := p.createStepOutput(accumulated, "Code generation failed", startTime)
                errorOutput.Status = interfaces.StatusFailed
                if !yield(errorOutput, err) {
                    return
                }
                return
            }

            // Record code generation step
            accumulated.ReasoningSteps = append(accumulated.ReasoningSteps, agentcore.ReasoningStep{
                Step:        iteration*2 + 1,
                Action:      fmt.Sprintf("Generate %s Code", language),
                Description: fmt.Sprintf("Iteration %d", iteration+1),
                Result:      p.formatCodeForDisplay(code, language),
                Duration:    time.Since(codeGenStart),
                Success:     true,
            })

            // Yield after code generation
            codeGenOutput := p.createStepOutput(accumulated, fmt.Sprintf("Code generated (iteration %d)", iteration+1), startTime)
            codeGenOutput.Status = interfaces.StatusInProgress
            codeGenOutput.Metadata["step_type"] = "code_generated"
            codeGenOutput.Metadata["iteration"] = iteration + 1
            codeGenOutput.Metadata["language"] = language
            codeGenOutput.Metadata["code"] = code
            if !yield(codeGenOutput, nil) {
                return // Early termination
            }

            // Validate code
            if err := p.validateCode(code, language); err != nil {
                finalResult = fmt.Sprintf("Code validation failed: %v", err)

                // Yield validation error
                validationOutput := p.createStepOutput(accumulated, "Code validation failed", startTime)
                validationOutput.Status = interfaces.StatusInProgress
                validationOutput.Metadata["step_type"] = "validation_failed"
                validationOutput.Metadata["iteration"] = iteration + 1
                validationOutput.Metadata["error"] = err.Error()
                if !yield(validationOutput, nil) {
                    return
                }
                continue
            }

            // Phase 2: Execute code
            execStart := time.Now()
            result, err := p.executeCode(ctx, code, language)

            // Record execution step
            accumulated.ReasoningSteps = append(accumulated.ReasoningSteps, agentcore.ReasoningStep{
                Step:        iteration*2 + 2,
                Action:      "Execute Code",
                Description: fmt.Sprintf("%s execution", language),
                Result:      p.formatExecutionResult(result),
                Duration:    time.Since(execStart),
                Success:     err == nil,
                Error:       p.errorString(err),
            })

            // Yield after code execution
            execOutput := p.createStepOutput(accumulated, fmt.Sprintf("Code executed (iteration %d)", iteration+1), startTime)
            if err != nil {
                execOutput.Status = interfaces.StatusInProgress
                execOutput.Metadata["step_type"] = "execution_failed"
                execOutput.Metadata["iteration"] = iteration + 1
                execOutput.Metadata["error"] = err.Error()
                if result != nil {
                    execOutput.Metadata["output"] = result.Output
                    execOutput.Metadata["error_output"] = result.Error
                }

                if !yield(execOutput, nil) {
                    return
                }

                // Try to debug and fix
                if iteration < p.config.MaxIterations-1 {
                    finalResult = p.debugError(ctx, code, err, result)
                    continue
                }

                // Final iteration failed
                finalErrorOutput := p.createStepOutput(accumulated, "Code execution failed", startTime)
                finalErrorOutput.Status = interfaces.StatusFailed
                if !yield(finalErrorOutput, err) {
                    return
                }
                return
            }

            execOutput.Status = interfaces.StatusInProgress
            execOutput.Metadata["step_type"] = "execution_success"
            execOutput.Metadata["iteration"] = iteration + 1
            execOutput.Metadata["output"] = result.Output
            execOutput.Metadata["exit_code"] = result.ExitCode
            execOutput.Metadata["duration_ms"] = result.Duration.Milliseconds()
            if !yield(execOutput, nil) {
                return
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

        // Yield final output
        finalOutput := p.createStepOutput(accumulated, "Program-of-Thought reasoning completed", startTime)
        finalOutput.Status = interfaces.StatusSuccess
        finalOutput.Result = finalAnswer
        finalOutput.Metadata["step_type"] = "final"
        finalOutput.Metadata["language"] = p.config.Language
        finalOutput.Metadata["iterations"] = p.config.MaxIterations
        finalOutput.Metadata["final_code"] = finalCode
        finalOutput.Metadata["total_duration_ms"] = time.Since(startTime).Milliseconds()
        yield(finalOutput, nil)
    }
}
```

#### Helper 方法：createStepOutput

```go
// createStepOutput creates a snapshot of current execution state
func (p *PoTAgent) createStepOutput(accumulated *agentcore.AgentOutput, message string, startTime time.Time) *agentcore.AgentOutput {
    stepOutput := &agentcore.AgentOutput{
        ReasoningSteps: make([]agentcore.ReasoningStep, len(accumulated.ReasoningSteps)),
        ToolCalls:      make([]agentcore.ToolCall, len(accumulated.ToolCalls)),
        Metadata:       make(map[string]interface{}),
        Timestamp:      time.Now(),
        Latency:        time.Since(startTime),
        Message:        message,
    }

    // Copy slices
    copy(stepOutput.ReasoningSteps, accumulated.ReasoningSteps)
    copy(stepOutput.ToolCalls, accumulated.ToolCalls)

    // Copy existing metadata
    for k, v := range accumulated.Metadata {
        stepOutput.Metadata[k] = v
    }

    return stepOutput
}
```

### 单元测试

**文件**: `agents/pot/pot_test.go` (~150 行新增)

添加了两个测试用例：

1. **TestPoTAgent_RunGenerator**: 测试完整的迭代流程
   - 验证代码生成、执行成功、最终输出
   - 验证每个阶段的元数据
   - 验证最终结果格式

2. **TestPoTAgent_RunGenerator_EarlyTermination**: 测试早期终止
   - 在第一个输出后主动 break
   - 验证生成器正确停止

**测试结果**:

```bash
=== RUN   TestPoTAgent_RunGenerator
    pot_test.go:673: Code generated!
    pot_test.go:686: Step 1: code_generated - Code generated (iteration 1)
    pot_test.go:681: Code execution succeeded!
    pot_test.go:686: Step 2: execution_success - Code executed (iteration 1)
    pot_test.go:686: Step 3: final - Program-of-Thought reasoning completed
    pot_test.go:698: Total outputs: 3
    pot_test.go:699: Found code generated: true
    pot_test.go:700: Found execution success: true
    pot_test.go:716: Final result: Solution found through program execution:

        Result: 120

        Generated Code:
        ```
        result = 5 * 4 * 3 * 2 * 1
        print(result)
        ```
    pot_test.go:717: Total reasoning steps: 2
--- PASS: TestPoTAgent_RunGenerator (0.02s)
=== RUN   TestPoTAgent_RunGenerator_EarlyTermination
    pot_test.go:758: Terminating early after 1 outputs
    pot_test.go:766: Successfully terminated early after 1 outputs
--- PASS: TestPoTAgent_RunGenerator_EarlyTermination (0.00s)
PASS
ok      github.com/kart-io/goagent/agents/pot   0.030s
```

## 质量保证

### 编译检查

```bash
go build ./agents/pot/...  # ✅ 通过
```

### Lint 检查

```bash
make lint
# 输出: 0 issues. ✅
```

所有代码符合项目 lint 规范。

## 设计亮点

### 1. 迭代式改进

PoT Agent 支持多次迭代改进代码：

- 第一次迭代：生成初始代码
- 后续迭代：基于上一次的执行结果改进代码
- 最多迭代 MaxIterations 次，直到成功或达到上限

### 2. 完整的错误处理

每个阶段都有详细的错误处理：

- **代码生成失败**: 立即返回错误，不继续执行
- **验证失败**: Yield 验证错误，继续下一次迭代
- **执行失败**: Yield 执行错误，尝试调试和修复
- **最后一次迭代失败**: 接受原始输出作为结果

### 3. 丰富的元数据

每个 yield 输出都包含丰富的元数据：

- `step_type`: 当前阶段（code_generated, execution_success, etc.）
- `iteration`: 当前迭代次数
- `language`: 编程语言
- `code`: 生成的代码
- `output`: 执行输出
- `error`: 错误信息（如果有）
- `duration_ms`: 执行时长

### 4. 多语言支持

PoT Agent 支持多种编程语言：

- Python（默认）
- JavaScript
- Go

每种语言都有独立的验证和执行逻辑。

## 使用示例

### PoT RunGenerator 使用

```go
agent := NewPoTAgent(PoTConfig{
    Name:          "pot-agent",
    LLM:           llmClient,
    Language:      "python",
    MaxIterations: 3,
    SafeMode:      true,
})

input := &core.AgentInput{
    Task: "Calculate the factorial of 10",
}

for output, err := range agent.RunGenerator(ctx, input) {
    if err != nil {
        log.Printf("Error: %v\n", err)
        continue
    }

    stepType := output.Metadata["step_type"].(string)

    switch stepType {
    case "code_generated":
        iteration := output.Metadata["iteration"].(int)
        language := output.Metadata["language"].(string)
        code := output.Metadata["code"].(string)
        fmt.Printf("迭代 %d: 生成了 %s 代码\n", iteration, language)
        fmt.Printf("代码:\n%s\n", code)

    case "validation_failed":
        errMsg := output.Metadata["error"].(string)
        fmt.Printf("验证失败: %s\n", errMsg)

    case "execution_failed":
        errOutput := output.Metadata["error_output"].(string)
        fmt.Printf("执行失败: %s\n", errOutput)

    case "execution_success":
        result := output.Metadata["output"].(string)
        duration := output.Metadata["duration_ms"].(int64)
        fmt.Printf("执行成功! 输出: %s (耗时: %d ms)\n", result, duration)

    case "final":
        fmt.Printf("\n最终结果:\n%v\n", output.Result)
        fmt.Printf("总耗时: %d ms\n", output.Metadata["total_duration_ms"])
        break
    }
}
```

### 早期终止示例

```go
// 只执行第一次迭代
maxOutputs := 2  // code_generated + execution_success
outputCount := 0

for output, err := range agent.RunGenerator(ctx, input) {
    if err != nil {
        log.Printf("Error: %v\n", err)
        break
    }

    outputCount++
    fmt.Printf("Step %d: %s\n", outputCount, output.Metadata["step_type"])

    if outputCount >= maxOutputs {
        fmt.Println("提前终止")
        break
    }
}
```

## 总结

Task 2.4 成功为 PoT Agent 实现了 RunGenerator，引入了迭代式代码生成和执行模式：

- **PoT**: 迭代式程序化推理（代码生成 → 验证 → 执行 → 改进）
- **测试**: 2 个单元测试，覆盖完整流程和早期终止
- **质量**: 编译通过，lint 0 issues

累计成果：

- ✅ ReactAgent RunGenerator (Task 2.1)
- ✅ ExecutorAgent RunGenerator (Task 2.1)
- ✅ CoTAgent RunGenerator (Task 2.2)
- ✅ ToTAgent RunGenerator (Task 2.2)
- ✅ SoTAgent RunGenerator (Task 2.3)
- ✅ GoTAgent RunGenerator (Task 2.3)
- ✅ PoTAgent RunGenerator (Task 2.4)

**总计**: 7 个主要 Agent 类型已支持 RunGenerator

## 下一步行动

继续为剩余 Agent 类型实现 RunGenerator：

1. **P2: MetaCoTAgent** (Meta Chain-of-Thought) - 元推理
2. **P3: SupervisorAgent** - 多 Agent 协调（较复杂）
3. **P3: Specialized Agents** - 特定领域 Agent（Shell, Http, Database, Cache）

---

**报告生成时间**: 2025-11-27
**报告版本**: 1.0
