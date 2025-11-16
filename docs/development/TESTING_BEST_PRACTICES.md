# Testing Best Practices for GoAgent

This document outlines testing best practices, patterns, and guidelines for the GoAgent project.

## Table of Contents

1. [Testing Philosophy](#testing-philosophy)
2. [Test Organization](#test-organization)
3. [Testing Patterns](#testing-patterns)
4. [Mock Usage](#mock-usage)
5. [Test Coverage Guidelines](#test-coverage-guidelines)
6. [Performance Testing](#performance-testing)
7. [Integration Testing](#integration-testing)
8. [CI/CD Integration](#cicd-integration)

## Testing Philosophy

### Core Principles

1. **Test Behavior, Not Implementation**: Focus on what the code does, not how it does it
2. **Isolation**: Each test should be independent and not rely on other tests
3. **Clarity**: Test names should clearly describe what is being tested
4. **Fast Feedback**: Unit tests should run quickly (< 100ms per test)
5. **Deterministic**: Tests should always produce the same result

### Test Pyramid

```
         /\
        /  \    E2E Tests (5%)
       /    \   - Full system tests
      /------\
     /        \ Integration Tests (20%)
    /          \- Component interaction
   /------------\
  /              \ Unit Tests (75%)
 /________________\- Individual functions/methods
```

## Test Organization

### Directory Structure

```
GoAgent/
├── module/
│   ├── file.go              # Implementation
│   ├── file_test.go         # Unit tests (same package)
│   └── testdata/            # Test fixtures
├── testing/
│   ├── mocks/               # Shared mock implementations
│   ├── testutil/            # Test helpers and utilities
│   └── fixtures/            # Shared test data
└── integration/
    └── module_test.go       # Integration tests
```

### Naming Conventions

```go
// Test function naming
func TestComponentName_MethodName_Scenario(t *testing.T) {}

// Examples:
func TestAgentState_Set_ConcurrentWrites(t *testing.T) {}
func TestToolExecutor_ExecuteParallel_WithTimeout(t *testing.T) {}
func TestMiddleware_Chain_ErrorPropagation(t *testing.T) {}
```

### Test File Organization

```go
package mypackage_test

import (
    "testing"
    // Standard library imports

    // Third-party test libraries
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    // Project imports
    "github.com/kart-io/goagent/mypackage"
    "github.com/kart-io/goagent/testing/mocks"
    "github.com/kart-io/goagent/testing/testutil"
)

// Test helpers at the top
func setupTest(t *testing.T) *TestContext {
    // Setup code
}

// Grouped tests by component
func TestComponent_Method(t *testing.T) {
    // Table-driven tests for multiple scenarios
    tests := []struct {
        name     string
        input    interface{}
        expected interface{}
        wantErr  bool
    }{
        // Test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

## Testing Patterns

### Table-Driven Tests

```go
func TestCalculate(t *testing.T) {
    tests := []struct {
        name     string
        a, b     int
        expected int
        wantErr  bool
    }{
        {"positive numbers", 2, 3, 5, false},
        {"negative numbers", -2, -3, -5, false},
        {"zero", 0, 0, 0, false},
        {"overflow", math.MaxInt, 1, 0, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Calculate(tt.a, tt.b)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### Test Fixtures

```go
func TestWithFixture(t *testing.T) {
    // Load test data
    data, err := os.ReadFile("testdata/input.json")
    require.NoError(t, err)

    // Parse and use
    var config Config
    err = json.Unmarshal(data, &config)
    require.NoError(t, err)

    // Test with fixture data
    result := ProcessConfig(config)
    assert.NotNil(t, result)
}
```

### Test Context Pattern

```go
func TestWithContext(t *testing.T) {
    tc := testutil.NewTestContext(t)
    defer tc.Cleanup()

    // Use test context components
    tc.State.Set("key", "value")
    tc.MockLLM.SetResponse("test response")

    // Run test
    agent := NewAgent(tc.MockLLM, tc.State)
    result, err := agent.Execute(tc.Ctx, "input")
    require.NoError(t, err)
    assert.Equal(t, "expected", result)
}
```

### Subtests for Organization

```go
func TestAgent(t *testing.T) {
    t.Run("Initialization", func(t *testing.T) {
        t.Run("WithDefaultConfig", func(t *testing.T) {
            // Test default initialization
        })

        t.Run("WithCustomConfig", func(t *testing.T) {
            // Test custom initialization
        })
    })

    t.Run("Execution", func(t *testing.T) {
        t.Run("Success", func(t *testing.T) {
            // Test successful execution
        })

        t.Run("Error", func(t *testing.T) {
            // Test error handling
        })
    })
}
```

## Mock Usage

### Creating Mocks

```go
// Mock implementation
type MockLLMClient struct {
    mu        sync.Mutex
    responses []string
    calls     []string
}

func (m *MockLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.calls = append(m.calls, prompt)
    if len(m.responses) > 0 {
        resp := m.responses[0]
        m.responses = m.responses[1:]
        return resp, nil
    }
    return "", errors.New("no response configured")
}

// Usage in tests
func TestWithMock(t *testing.T) {
    mock := &MockLLMClient{
        responses: []string{"response1", "response2"},
    }

    agent := NewAgent(mock)
    result, err := agent.Process(context.Background(), "input")
    require.NoError(t, err)
    assert.Equal(t, "response1", result)

    // Verify interactions
    assert.Equal(t, 1, len(mock.calls))
    assert.Contains(t, mock.calls[0], "input")
}
```

### Mock Builders

```go
func TestWithMockBuilder(t *testing.T) {
    mock := mocks.NewMockTool("calculator").
        WithSchema(`{"type": "object"}`).
        WithResponse("42").
        Build()

    result, err := mock.Invoke(context.Background(), input)
    require.NoError(t, err)
    assert.Equal(t, "42", result.Result)
}
```

## Test Coverage Guidelines

### Coverage Targets

- **Overall Project**: ≥ 75%
- **Core Packages**: ≥ 80%
- **Critical Paths**: ≥ 90%
- **Utilities**: ≥ 70%
- **Examples**: ≥ 50%

### Coverage Commands

```bash
# Run tests with coverage
go test -v -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Check specific package coverage
go test -cover ./core/...

# Coverage with race detection
go test -race -cover ./...
```

### What to Test

**Must Test:**

- Public APIs
- Error conditions
- Edge cases
- Concurrent operations
- State mutations
- Resource cleanup

**Consider Testing:**

- Complex internal logic
- Performance-critical paths
- Configuration parsing
- Retry/timeout logic

**Skip Testing:**

- Simple getters/setters
- Obvious delegation
- Generated code
- Third-party library calls

## Performance Testing

### Benchmarks

```go
func BenchmarkToolExecution(b *testing.B) {
    tool := NewCalculatorTool()
    input := &ToolInput{
        Args: map[string]interface{}{
            "expression": "2 + 2",
        },
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = tool.Invoke(context.Background(), input)
    }
}

// Run benchmarks
// go test -bench=. -benchmem ./...
```

### Load Testing

```go
func TestConcurrentLoad(t *testing.T) {
    agent := NewAgent()
    ctx := context.Background()

    // Concurrent requests
    var wg sync.WaitGroup
    errors := make(chan error, 100)

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            _, err := agent.Execute(ctx, fmt.Sprintf("request-%d", id))
            if err != nil {
                errors <- err
            }
        }(i)
    }

    wg.Wait()
    close(errors)

    // Check for errors
    var errorCount int
    for err := range errors {
        t.Logf("Error: %v", err)
        errorCount++
    }

    assert.Less(t, errorCount, 5, "Too many errors under load")
}
```

## Integration Testing

### Database Integration

```go
//go:build integration
// +build integration

func TestDatabaseIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // Setup test database
    db := setupTestDB(t)
    defer cleanupTestDB(db)

    store := NewSQLStore(db)

    // Test operations
    err := store.Put(ctx, []string{"test"}, "key", "value")
    require.NoError(t, err)

    item, err := store.Get(ctx, []string{"test"}, "key")
    require.NoError(t, err)
    assert.Equal(t, "value", item.Value)
}
```

### External Service Integration

```go
func TestLLMIntegration(t *testing.T) {
    if os.Getenv("OPENAI_API_KEY") == "" {
        t.Skip("OpenAI API key not set")
    }

    client := llm.NewOpenAIClient(os.Getenv("OPENAI_API_KEY"))

    resp, err := client.Complete(context.Background(), &llm.Request{
        Prompt: "Hello, world!",
    })

    require.NoError(t, err)
    assert.NotEmpty(t, resp.Content)
}
```

## CI/CD Integration

### GitHub Actions Workflow

```yaml
name: Tests

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: "1.21"

      - name: Install dependencies
        run: go mod download

      - name: Run tests
        run: go test -v -race -coverprofile=coverage.out ./...

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./coverage.out

      - name: Run integration tests
        env:
          INTEGRATION: true
        run: go test -v -tags=integration ./...
```

### Makefile Targets

```makefile
.PHONY: test test-unit test-integration test-coverage test-race

test: test-unit

test-unit:
	go test -v ./...

test-integration:
	go test -v -tags=integration ./...

test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

test-race:
	go test -v -race ./...

test-all: test-unit test-race test-integration
```

## Testing Checklist

### Before Committing

- [ ] All tests pass locally
- [ ] Test coverage meets requirements
- [ ] No race conditions detected
- [ ] Tests are deterministic
- [ ] Mock data is realistic
- [ ] Error cases are covered
- [ ] Documentation updated

### Code Review Checklist

- [ ] Tests follow naming conventions
- [ ] Table-driven tests used where appropriate
- [ ] Proper test isolation
- [ ] Adequate assertions
- [ ] Clear failure messages
- [ ] No test interdependencies
- [ ] Performance benchmarks for critical paths

## Common Pitfalls to Avoid

1. **Global State**: Avoid modifying global variables in tests
2. **Time Dependencies**: Use time injection or mock clocks
3. **File System**: Use temp directories or in-memory filesystems
4. **Network Calls**: Mock external services
5. **Random Data**: Use seeded random for reproducibility
6. **Resource Leaks**: Always cleanup resources
7. **Assertion Fatigue**: Too many assertions make tests brittle
8. **Test Data Reuse**: Can create hidden dependencies

## Testing Tools and Libraries

### Essential Libraries

- **testify**: Assertions and mocks
- **gomock**: Code generation for mocks
- **ginkgo/gomega**: BDD-style testing
- **httptest**: HTTP server mocking
- **sqlmock**: SQL database mocking

### Useful Commands

```bash
# Run specific test
go test -v -run TestAgentState_Set

# Run tests matching pattern
go test -v -run ".*Concurrent.*"

# Test with timeout
go test -timeout 30s ./...

# Generate test coverage badge
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## Summary

Good testing is essential for maintaining code quality and preventing regressions. Follow these best practices:

1. Write tests first (TDD) when possible
2. Keep tests simple and focused
3. Use mocks judiciously
4. Maintain high coverage on critical paths
5. Run tests frequently during development
6. Automate testing in CI/CD pipelines
7. Review and refactor tests regularly

Remember: Tests are code too - they need to be maintained, documented, and refactored just like production code.
