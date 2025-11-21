# GoAgent 测试最佳实践

## 概述

本文档提供 GoAgent 项目的测试指南和最佳实践，确保代码质量和可靠性。

## 测试覆盖率标准

- **最低要求**：所有包 80% 覆盖率
- **新代码**：PR 合并前必须包含测试
- **关键路径**：目标覆盖率超过 90%

## 运行测试

### 基本命令

```bash
# 运行所有测试
make test
# 或
go test ./...

# 带竞态检测
go test -v -race -timeout 30s ./...

# 仅运行短测试
make test-short

# 运行单个测试
go test -v -run TestSpecificTest ./path/to/package

# 运行集成测试
make test-integration
```

### 覆盖率报告

```bash
# 生成覆盖率报告
make coverage

# 在浏览器中查看
make coverage-view

# 手动生成
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## 测试组织

### 表驱动测试

对于多个测试用例，使用表驱动测试：

```go
func TestCalculate(t *testing.T) {
    tests := []struct {
        name     string
        input    int
        expected int
        wantErr  bool
    }{
        {
            name:     "positive number",
            input:    5,
            expected: 10,
            wantErr:  false,
        },
        {
            name:     "zero",
            input:    0,
            expected: 0,
            wantErr:  false,
        },
        {
            name:     "negative number",
            input:    -1,
            expected: 0,
            wantErr:  true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Calculate(tt.input)

            if tt.wantErr {
                assert.Error(t, err)
                return
            }

            assert.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### 子测试

使用子测试组织相关测试：

```go
func TestAgent(t *testing.T) {
    t.Run("Invoke", func(t *testing.T) {
        t.Run("with valid input", func(t *testing.T) {
            // 测试有效输入
        })

        t.Run("with invalid input", func(t *testing.T) {
            // 测试无效输入
        })
    })

    t.Run("Stream", func(t *testing.T) {
        t.Run("with context cancellation", func(t *testing.T) {
            // 测试上下文取消
        })
    })
}
```

## Mock 和 Stub

### LLM Mock

```go
// MockLLMClient 模拟 LLM 客户端
type MockLLMClient struct {
    responses []*llm.CompletionResponse
    index     int
    err       error
}

func NewMockLLMClient(responses ...*llm.CompletionResponse) *MockLLMClient {
    return &MockLLMClient{
        responses: responses,
    }
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
    if m.err != nil {
        return nil, m.err
    }
    if m.index >= len(m.responses) {
        return &llm.CompletionResponse{Content: "default response"}, nil
    }
    resp := m.responses[m.index]
    m.index++
    return resp, nil
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
    return m.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (m *MockLLMClient) Provider() llm.Provider {
    return llm.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
    return true
}
```

### 工具 Mock

```go
// MockTool 模拟工具
type MockTool struct {
    name        string
    description string
    result      interface{}
    err         error
}

func NewMockTool(name string, result interface{}) *MockTool {
    return &MockTool{
        name:        name,
        description: "Mock tool for testing",
        result:      result,
    }
}

func (m *MockTool) Name() string {
    return m.name
}

func (m *MockTool) Description() string {
    return m.description
}

func (m *MockTool) ArgsSchema() string {
    return `{"type": "object", "properties": {}}`
}

func (m *MockTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
    if m.err != nil {
        return &interfaces.ToolOutput{
            Success: false,
            Error:   m.err.Error(),
        }, nil
    }
    return &interfaces.ToolOutput{
        Result:  m.result,
        Success: true,
    }, nil
}
```

## 测试模式

### 上下文测试

```go
func TestAgentWithContext(t *testing.T) {
    t.Run("context cancellation", func(t *testing.T) {
        agent := createTestAgent()

        ctx, cancel := context.WithCancel(context.Background())
        cancel() // 立即取消

        input := &interfaces.Input{
            Messages: []interfaces.Message{
                {Role: "user", Content: "test"},
            },
        }

        _, err := agent.Invoke(ctx, input)
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "context canceled")
    })

    t.Run("context timeout", func(t *testing.T) {
        agent := createTestAgent()

        ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
        defer cancel()

        time.Sleep(10 * time.Millisecond) // 确保超时

        input := &interfaces.Input{
            Messages: []interfaces.Message{
                {Role: "user", Content: "test"},
            },
        }

        _, err := agent.Invoke(ctx, input)
        assert.Error(t, err)
    })
}
```

### 并发测试

```go
func TestAgentConcurrency(t *testing.T) {
    agent := createTestAgent()

    const numGoroutines = 100
    var wg sync.WaitGroup
    errors := make(chan error, numGoroutines)

    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()

            input := &interfaces.Input{
                Messages: []interfaces.Message{
                    {Role: "user", Content: fmt.Sprintf("test %d", id)},
                },
            }

            _, err := agent.Invoke(context.Background(), input)
            if err != nil {
                errors <- err
            }
        }(i)
    }

    wg.Wait()
    close(errors)

    for err := range errors {
        t.Errorf("并发调用失败: %v", err)
    }
}
```

### 错误测试

```go
func TestAgentErrors(t *testing.T) {
    tests := []struct {
        name        string
        setupMock   func() *MockLLMClient
        input       *interfaces.Input
        expectedErr string
    }{
        {
            name: "LLM error",
            setupMock: func() *MockLLMClient {
                mock := NewMockLLMClient()
                mock.err = fmt.Errorf("API error")
                return mock
            },
            input: &interfaces.Input{
                Messages: []interfaces.Message{
                    {Role: "user", Content: "test"},
                },
            },
            expectedErr: "API error",
        },
        {
            name: "empty input",
            setupMock: func() *MockLLMClient {
                return NewMockLLMClient()
            },
            input:       &interfaces.Input{},
            expectedErr: "empty input",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mock := tt.setupMock()
            agent := builder.NewAgentBuilder(mock).Build()

            _, err := agent.Invoke(context.Background(), tt.input)

            assert.Error(t, err)
            assert.Contains(t, err.Error(), tt.expectedErr)
        })
    }
}
```

### 流式测试

```go
func TestAgentStream(t *testing.T) {
    mockClient := NewMockStreamClient()
    agent := builder.NewAgentBuilder(mockClient).Build()

    input := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "test"},
        },
    }

    stream, err := agent.Stream(context.Background(), input)
    require.NoError(t, err)

    var chunks []*interfaces.StreamChunk
    for chunk := range stream {
        chunks = append(chunks, chunk)
        if chunk.Done {
            break
        }
    }

    assert.NotEmpty(t, chunks)
    assert.True(t, chunks[len(chunks)-1].Done)
}
```

## 集成测试

### 标记集成测试

```go
//go:build integration

package integration_test

import (
    "testing"
    // ...
)

func TestRealLLMIntegration(t *testing.T) {
    if os.Getenv("OPENAI_API_KEY") == "" {
        t.Skip("需要 OPENAI_API_KEY")
    }

    // 集成测试代码
}
```

运行集成测试：

```bash
go test -v -tags=integration ./...
```

### 数据库集成测试

```go
func TestRedisCheckpointer(t *testing.T) {
    if testing.Short() {
        t.Skip("跳过 Redis 集成测试")
    }

    // 确保 Redis 可用
    client := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })
    defer client.Close()

    if err := client.Ping(context.Background()).Err(); err != nil {
        t.Skipf("Redis 不可用: %v", err)
    }

    // 测试代码
}
```

## 断言和辅助函数

### 使用 testify

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestExample(t *testing.T) {
    // assert - 失败后继续执行
    assert.Equal(t, expected, actual)
    assert.NoError(t, err)
    assert.True(t, condition)
    assert.Contains(t, str, substr)

    // require - 失败后立即停止
    require.NoError(t, err)
    require.NotNil(t, obj)
}
```

### 自定义辅助函数

```go
// testutil/helpers.go
package testutil

import (
    "testing"

    "github.com/kart-io/goagent/builder"
    "github.com/kart-io/goagent/interfaces"
    "github.com/kart-io/goagent/llm"
)

// CreateTestAgent 创建测试用 Agent
func CreateTestAgent(t *testing.T, responses ...*llm.CompletionResponse) interfaces.Agent {
    t.Helper()
    mock := NewMockLLMClient(responses...)
    return builder.NewAgentBuilder(mock).Build()
}

// CreateTestInput 创建测试输入
func CreateTestInput(content string) *interfaces.Input {
    return &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: content},
        },
    }
}
```

## 基准测试

```go
func BenchmarkAgentInvoke(b *testing.B) {
    mock := NewMockLLMClient(&llm.CompletionResponse{
        Content: "response",
    })
    agent := builder.NewAgentBuilder(mock).Build()

    input := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "test"},
        },
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := agent.Invoke(context.Background(), input)
        if err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkAgentInvokeParallel(b *testing.B) {
    mock := NewMockLLMClient(&llm.CompletionResponse{
        Content: "response",
    })
    agent := builder.NewAgentBuilder(mock).Build()

    input := &interfaces.Input{
        Messages: []interfaces.Message{
            {Role: "user", Content: "test"},
        },
    }

    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _, err := agent.Invoke(context.Background(), input)
            if err != nil {
                b.Fatal(err)
            }
        }
    })
}
```

运行基准测试：

```bash
go test -bench=. -benchmem ./...
```

## 测试命名约定

- 测试函数：`Test<FunctionName>`
- 子测试：描述性名称，使用空格
- 基准测试：`Benchmark<FunctionName>`
- 示例：`Example<FunctionName>`

```go
func TestAgentInvoke(t *testing.T) {
    t.Run("with valid input", func(t *testing.T) { ... })
    t.Run("with empty messages", func(t *testing.T) { ... })
    t.Run("with context cancellation", func(t *testing.T) { ... })
}

func BenchmarkAgentInvoke(b *testing.B) { ... }

func ExampleAgent_Invoke() {
    // 示例代码
    // Output: expected output
}
```

## 常见陷阱

### 1. 不要使用固定的时间

```go
// 不好
time.Sleep(100 * time.Millisecond)
assert.True(t, isDone)

// 好
select {
case <-done:
    // 成功
case <-time.After(time.Second):
    t.Fatal("超时")
}
```

### 2. 清理测试资源

```go
func TestWithTempFile(t *testing.T) {
    f, err := os.CreateTemp("", "test")
    require.NoError(t, err)
    defer os.Remove(f.Name())
    defer f.Close()

    // 测试代码
}
```

### 3. 避免测试间依赖

```go
// 不好 - 测试间共享状态
var globalCounter int

func TestA(t *testing.T) {
    globalCounter++
}

func TestB(t *testing.T) {
    assert.Equal(t, 1, globalCounter) // 依赖 TestA 先执行
}

// 好 - 独立测试
func TestA(t *testing.T) {
    counter := 0
    counter++
    assert.Equal(t, 1, counter)
}

func TestB(t *testing.T) {
    counter := 0
    counter++
    assert.Equal(t, 1, counter)
}
```

### 4. 使用 t.Helper()

```go
func assertAgentOutput(t *testing.T, output *interfaces.Output, expectedContent string) {
    t.Helper() // 报告调用者的行号
    require.NotNil(t, output)
    require.NotEmpty(t, output.Messages)
    assert.Equal(t, expectedContent, output.Messages[0].Content)
}
```

## CI/CD 集成

### GitHub Actions 配置

```yaml
name: Test

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.25'

      - name: Run tests
        run: make test

      - name: Check coverage
        run: |
          make coverage
          go tool cover -func=coverage.out | grep total | awk '{print $3}'
```

## 相关文档

- [架构概述](../architecture/ARCHITECTURE.md)
- [导入层级说明](../architecture/IMPORT_LAYERING.md)
