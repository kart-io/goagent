package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/errors"
)

// 示例 1: 创建和使用不同类型的错误
func example1CreateErrors() {
	fmt.Println("=== Example 1: Creating Different Types of Errors ===\n")

	// LLM 请求错误
	llmErr := errors.NewLLMRequestError("openai", "gpt-4", fmt.Errorf("API connection failed"))
	fmt.Printf("LLM Error: %v\n", llmErr)
	fmt.Printf("Error Code: %s\n", llmErr.Code)
	fmt.Printf("Component: %s\n\n", llmErr.Component)

	// 工具执行错误
	toolErr := errors.NewToolExecutionError("calculator", "compute", fmt.Errorf("division by zero"))
	fmt.Printf("Tool Error: %v\n", toolErr)
	fmt.Printf("Context: %+v\n\n", toolErr.Context)

	// 文档未找到错误
	docErr := errors.NewDocumentNotFoundError("doc-123")
	fmt.Printf("Document Error: %v\n", docErr)
	fmt.Printf("Error Code: %s\n\n", docErr.Code)

	// 计划执行错误
	planErr := errors.NewPlanExecutionError("plan-456", "step-3", fmt.Errorf("prerequisite failed"))
	fmt.Printf("Planning Error: %v\n", planErr)
	fmt.Printf("Context: %+v\n\n", planErr.Context)
}

// 示例 2: 链式添加上下文
func example2ChainedContext() {
	fmt.Println("=== Example 2: Chained Context ===\n")

	err := errors.New(errors.CodeLLMRateLimit, "rate limit exceeded").
		WithComponent("llm_client").
		WithOperation("request").
		WithContext("provider", "openai").
		WithContext("model", "gpt-4").
		WithContext("requests_per_minute", 60).
		WithContext("current_count", 75)

	fmt.Printf("Error: %v\n", err)
	fmt.Printf("Full Context: %+v\n\n", err.Context)
}

// 示例 3: 错误包装
func example3ErrorWrapping() {
	fmt.Println("=== Example 3: Error Wrapping ===\n")

	// 模拟底层错误
	originalErr := fmt.Errorf("network timeout")

	// 包装第一层
	wrappedErr1 := errors.Wrap(originalErr, errors.CodeLLMTimeout, "LLM request timed out").
		WithContext("timeout_seconds", 30)

	// 包装第二层
	wrappedErr2 := errors.Wrap(wrappedErr1, errors.CodeAgentExecution, "agent execution failed").
		WithContext("agent_name", "research-agent")

	// 打印错误链
	fmt.Println("Error Chain:")
	chain := errors.ErrorChain(wrappedErr2)
	for i, e := range chain {
		fmt.Printf("  [%d] %v\n", i, e)
	}

	// 获取根因
	root := errors.RootCause(wrappedErr2)
	fmt.Printf("\nRoot Cause: %v\n\n", root)
}

// 示例 4: 错误检查和分支处理
func example4ErrorChecking() {
	fmt.Println("=== Example 4: Error Checking ===\n")

	// 模拟不同类型的错误
	testErrors := []error{
		errors.NewDocumentNotFoundError("doc-1"),
		errors.NewLLMRateLimitError("openai", "gpt-4", 60),
		errors.NewToolTimeoutError("web_scraper", 30),
		errors.New(errors.CodeInvalidInput, "invalid parameter"),
	}

	for i, err := range testErrors {
		fmt.Printf("Error %d: %v\n", i+1, err)

		// 使用错误代码检查
		if errors.IsCode(err, errors.CodeDocumentNotFound) {
			fmt.Println("  → Handling: Return 404 Not Found")
		} else if errors.IsCode(err, errors.CodeLLMRateLimit) {
			ctx := errors.GetContext(err)
			retryAfter := ctx["retry_after_seconds"]
			fmt.Printf("  → Handling: Wait %v seconds before retry\n", retryAfter)
		} else if errors.IsCode(err, errors.CodeToolTimeout) {
			fmt.Println("  → Handling: Retry with increased timeout")
		} else {
			fmt.Println("  → Handling: Return 400 Bad Request")
		}
		fmt.Println()
	}
}

// 示例 5: 实际场景 - 重试逻辑
func example5RetryLogic() {
	fmt.Println("=== Example 5: Retry Logic ===\n")

	// 模拟可能失败的操作
	attemptCounter := 0
	failingOperation := func() error {
		attemptCounter++
		if attemptCounter < 3 {
			return errors.NewLLMRateLimitError("openai", "gpt-4", 1)
		}
		return nil // 第三次成功
	}

	// 带重试的执行
	const maxAttempts = 5
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("Attempt %d/%d...\n", attempt, maxAttempts)

		err := failingOperation()
		if err == nil {
			fmt.Println("✓ Operation succeeded!\n")
			return
		}

		lastErr = err

		// 检查是否需要重试
		if errors.IsCode(err, errors.CodeLLMRateLimit) {
			ctx := errors.GetContext(err)
			retryAfter := ctx["retry_after_seconds"].(int)
			fmt.Printf("  Rate limited, waiting %d second(s)...\n", retryAfter)
			time.Sleep(time.Duration(retryAfter) * time.Second)
			continue
		}

		// 其他错误直接返回
		fmt.Printf("  Non-retriable error: %v\n\n", err)
		return
	}

	// 重试耗尽
	finalErr := errors.NewToolRetryExhaustedError("llm_client", maxAttempts, lastErr)
	fmt.Printf("✗ %v\n\n", finalErr)
}

// 示例 6: 实际场景 - 降级处理
type Store interface {
	Get(ctx context.Context, key string) (string, error)
}

type mockStore struct {
	name       string
	shouldFail bool
}

func (s *mockStore) Get(ctx context.Context, key string) (string, error) {
	if s.shouldFail {
		return "", errors.NewStoreConnectionError(s.name, "localhost:6379", fmt.Errorf("connection refused"))
	}
	return fmt.Sprintf("data from %s", s.name), nil
}

func example6Fallback() {
	fmt.Println("=== Example 6: Fallback/Degradation ===\n")

	primaryStore := &mockStore{name: "primary", shouldFail: true}
	backupStore := &mockStore{name: "backup", shouldFail: false}

	ctx := context.Background()
	key := "user:123"

	// 尝试主存储
	fmt.Println("Trying primary store...")
	data, err := primaryStore.Get(ctx, key)
	if err != nil {
		fmt.Printf("  Primary store failed: %v\n", err)

		// 检查是否是连接错误，尝试降级到备份
		if errors.IsCode(err, errors.CodeStoreConnection) {
			fmt.Println("  → Falling back to backup store...")

			data, backupErr := backupStore.Get(ctx, key)
			if backupErr == nil {
				fmt.Printf("  ✓ Success from backup: %s\n\n", data)
				return
			}
			fmt.Printf("  ✗ Backup also failed: %v\n\n", backupErr)
			return
		}

		fmt.Println("  ✗ Non-retriable error\n")
		return
	}

	fmt.Printf("✓ Success from primary: %s\n\n", data)
}

// 示例 7: 错误转换 (内部错误 → HTTP 状态码)
func example7ErrorConversion() {
	fmt.Println("=== Example 7: Error Conversion (to HTTP Status) ===\n")

	toHTTPStatus := func(err error) (int, string) {
		code := errors.GetCode(err)

		switch code {
		case errors.CodeDocumentNotFound, errors.CodeAgentNotFound, errors.CodeToolNotFound:
			return 404, "Not Found"
		case errors.CodeInvalidInput, errors.CodeInvalidConfig:
			return 400, "Bad Request"
		case errors.CodeLLMRateLimit:
			return 429, "Too Many Requests"
		case errors.CodeLLMTimeout, errors.CodeToolTimeout:
			return 504, "Gateway Timeout"
		case errors.CodeContextCanceled:
			return 499, "Client Closed Request"
		default:
			return 500, "Internal Server Error"
		}
	}

	// 测试不同错误的转换
	testErrors := []error{
		errors.NewDocumentNotFoundError("doc-1"),
		errors.NewInvalidInputError("api", "query", "query is required"),
		errors.NewLLMRateLimitError("openai", "gpt-4", 60),
		errors.NewToolTimeoutError("search", 30),
		errors.New(errors.CodeInternal, "unexpected error"),
	}

	for _, err := range testErrors {
		status, message := toHTTPStatus(err)
		fmt.Printf("Error: %v\n", err)
		fmt.Printf("  → HTTP %d: %s\n\n", status, message)
	}
}

// 示例 8: 提取错误信息用于日志
func example8Logging() {
	fmt.Println("=== Example 8: Logging with Structured Errors ===\n")

	err := errors.NewLLMRequestError("openai", "gpt-4", fmt.Errorf("connection refused")).
		WithOperation("generate_response").
		WithContext("prompt_length", 1500).
		WithContext("temperature", 0.7).
		WithContext("max_tokens", 500)

	// 提取所有字段用于结构化日志
	fmt.Println("Structured Log Entry:")
	fmt.Printf("  level: error\n")
	fmt.Printf("  error_code: %s\n", errors.GetCode(err))
	fmt.Printf("  component: %s\n", errors.GetComponent(err))
	fmt.Printf("  operation: %s\n", errors.GetOperation(err))
	fmt.Printf("  message: %s\n", err.Error())
	fmt.Printf("  context: %+v\n", errors.GetContext(err))

	// 获取堆栈跟踪 (err 已经是 *errors.AgentError 类型)
	fmt.Printf("  stack_trace:\n")
	stackFrames := err.Stack
	maxFrames := 3
	if len(stackFrames) < maxFrames {
		maxFrames = len(stackFrames)
	}
	for i := 0; i < maxFrames; i++ {
		frame := stackFrames[i]
		fmt.Printf("    [%d] %s:%d - %s\n", i, frame.File, frame.Line, frame.Function)
	}
	fmt.Println()
}

// 示例 9: 批处理错误聚合
func example9BatchErrors() {
	fmt.Println("=== Example 9: Batch Processing with Error Aggregation ===\n")

	type Item struct {
		ID   string
		Data string
	}

	items := []Item{
		{ID: "item-1", Data: "valid"},
		{ID: "item-2", Data: ""}, // 将失败
		{ID: "item-3", Data: "valid"},
		{ID: "item-4", Data: "invalid"}, // 将失败
		{ID: "item-5", Data: "valid"},
	}

	processItem := func(item Item) error {
		if item.Data == "" {
			return errors.NewInvalidInputError("processor", "data", "data cannot be empty").
				WithContext("item_id", item.ID)
		}
		if item.Data == "invalid" {
			return errors.New(errors.CodeInvalidInput, "data validation failed").
				WithContext("item_id", item.ID)
		}
		return nil
	}

	// 处理批次
	var errs []error
	successCount := 0

	for i, item := range items {
		fmt.Printf("Processing item %d: %s\n", i+1, item.ID)

		err := processItem(item)
		if err != nil {
			fmt.Printf("  ✗ Error: %v\n", err)
			errs = append(errs, err)
			continue
		}

		fmt.Printf("  ✓ Success\n")
		successCount++
	}

	fmt.Printf("\nBatch Summary:\n")
	fmt.Printf("  Total: %d\n", len(items))
	fmt.Printf("  Success: %d\n", successCount)
	fmt.Printf("  Failed: %d\n\n", len(errs))

	if len(errs) > 0 {
		batchErr := errors.New(errors.CodeInternal, "batch processing partially failed").
			WithContext("total_items", len(items)).
			WithContext("successful_items", successCount).
			WithContext("failed_items", len(errs))

		fmt.Printf("Aggregated Error: %v\n", batchErr)
		fmt.Printf("Context: %+v\n\n", batchErr.Context)
	}
}

// 示例 10: 错误链分析
func example10ErrorChainAnalysis() {
	fmt.Println("=== Example 10: Error Chain Analysis ===\n")

	// 构建多层错误链
	err1 := fmt.Errorf("database connection timeout")
	err2 := errors.Wrap(err1, errors.CodeStoreConnection, "failed to connect to PostgreSQL").
		WithContext("host", "db.example.com").
		WithContext("port", 5432)
	err3 := errors.Wrap(err2, errors.CodeStateLoad, "failed to load session state").
		WithContext("session_id", "sess-123")
	err4 := errors.Wrap(err3, errors.CodeAgentExecution, "agent execution failed").
		WithContext("agent_name", "chat-agent")

	fmt.Println("Complete Error Chain:")
	chain := errors.ErrorChain(err4)
	for i, e := range chain {
		fmt.Printf("\n[Level %d]\n", i)
		fmt.Printf("  Error: %v\n", e)

		if agentErr, ok := e.(*errors.AgentError); ok {
			fmt.Printf("  Code: %s\n", agentErr.Code)
			fmt.Printf("  Component: %s\n", agentErr.Component)
			fmt.Printf("  Operation: %s\n", agentErr.Operation)
			fmt.Printf("  Context: %+v\n", agentErr.Context)
		}
	}

	fmt.Printf("\nRoot Cause: %v\n\n", errors.RootCause(err4))
}

func main() {
	log.SetFlags(0) // 简化日志输出

	example1CreateErrors()
	example2ChainedContext()
	example3ErrorWrapping()
	example4ErrorChecking()
	example5RetryLogic()
	example6Fallback()
	example7ErrorConversion()
	example8Logging()
	example9BatchErrors()
	example10ErrorChainAnalysis()

	fmt.Println("=== All Examples Completed ===")
}
