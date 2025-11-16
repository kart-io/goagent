package tools_test

import (
	"context"
	"testing"
	"time"

	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/tools/compute"
	"github.com/kart-io/goagent/tools/http"
	"github.com/kart-io/goagent/tools/search"
	"github.com/kart-io/goagent/tools/shell"
)

// TestCalculatorTool 测试计算器工具
func TestCalculatorTool(t *testing.T) {
	tool := compute.NewCalculatorTool()
	ctx := context.Background()

	tests := []struct {
		expression string
		expected   float64
	}{
		{"2 + 3", 5.0},
		{"10 - 5", 5.0},
		{"4 * 3", 12.0},
		{"15 / 3", 5.0},
		{"2 + 3 * 4", 14.0},
		{"(2 + 3) * 4", 20.0},
		{"2^3", 8.0},
	}

	for _, tt := range tests {
		t.Run(tt.expression, func(t *testing.T) {
			input := &tools.ToolInput{
				Args: map[string]interface{}{
					"expression": tt.expression,
				},
				Context: ctx,
			}

			output, err := tool.Invoke(ctx, input)
			if err != nil {
				t.Fatalf("Invoke failed: %v", err)
			}

			if !output.Success {
				t.Errorf("Expected success=true, got error: %s", output.Error)
			}

			if result, ok := output.Result.(float64); !ok || result != tt.expected {
				t.Errorf("Expected result %v, got %v", tt.expected, output.Result)
			}
		})
	}
}

// TestSearchTool 测试搜索工具
func TestSearchTool(t *testing.T) {
	engine := search.NewMockSearchEngine()
	tool := search.NewSearchTool(engine)

	ctx := context.Background()
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"query":       "golang",
			"max_results": 2.0,
		},
		Context: ctx,
	}

	output, err := tool.Invoke(ctx, input)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if !output.Success {
		t.Errorf("Expected success=true, got error: %s", output.Error)
	}

	results, ok := output.Result.([]search.SearchResult)
	if !ok {
		t.Fatalf("Expected result to be []SearchResult")
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

// TestShellTool 测试 Shell 工具
func TestShellTool(t *testing.T) {
	tool := shell.NewShellTool([]string{"echo", "pwd"}, 5*time.Second)

	ctx := context.Background()

	t.Run("AllowedCommand", func(t *testing.T) {
		input := &tools.ToolInput{
			Args: map[string]interface{}{
				"command": "echo",
				"args":    []interface{}{"hello", "world"},
			},
			Context: ctx,
		}

		output, err := tool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("Invoke failed: %v", err)
		}

		if !output.Success {
			t.Errorf("Expected success=true, got error: %s", output.Error)
		}

		result, ok := output.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected result to be map[string]interface{}")
		}

		if result["exit_code"] != 0 {
			t.Errorf("Expected exit_code 0, got %v", result["exit_code"])
		}
	})

	t.Run("DisallowedCommand", func(t *testing.T) {
		input := &tools.ToolInput{
			Args: map[string]interface{}{
				"command": "rm",
				"args":    []interface{}{"-rf", "/"},
			},
			Context: ctx,
		}

		output, err := tool.Invoke(ctx, input)
		if err == nil {
			t.Error("Expected error for disallowed command")
		}

		if output.Success {
			t.Error("Expected success=false for disallowed command")
		}
	})
}

// TestAPITool 测试 API 工具
func TestAPITool(t *testing.T) {
	tool := http.NewAPITool("https://jsonplaceholder.typicode.com", 10*time.Second, nil)

	ctx := context.Background()

	t.Run("GET Request", func(t *testing.T) {
		input := &tools.ToolInput{
			Args: map[string]interface{}{
				"method": "GET",
				"url":    "/posts/1",
			},
			Context: ctx,
		}

		output, err := tool.Invoke(ctx, input)
		if err != nil {
			t.Fatalf("Invoke failed: %v", err)
		}

		if !output.Success {
			t.Errorf("Expected success=true, got error: %s", output.Error)
		}

		result, ok := output.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected result to be map[string]interface{}")
		}

		if result["status_code"] != 200 {
			t.Errorf("Expected status_code 200, got %v", result["status_code"])
		}
	})
}

// BenchmarkCalculatorTool 性能测试
func BenchmarkCalculatorTool(b *testing.B) {
	tool := compute.NewCalculatorTool()
	ctx := context.Background()
	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"expression": "2 + 3 * 4",
		},
		Context: ctx,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tool.Invoke(ctx, input)
	}
}
