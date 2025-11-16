package toolbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/mcp/core"
)

// MockTool 模拟工具
type MockTool struct {
	*core.BaseTool
	executeFunc  func(context.Context, map[string]interface{}) (*core.ToolResult, error)
	validateFunc func(map[string]interface{}) error
}

func NewMockTool(name, category string) *MockTool {
	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"input": {
				Type:        "string",
				Description: "输入参数",
			},
		},
		Required: []string{"input"},
	}

	return &MockTool{
		BaseTool: core.NewBaseTool(name, "模拟工具", category, schema),
	}
}

func (m *MockTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, input)
	}

	return &core.ToolResult{
		Success:   true,
		Data:      map[string]interface{}{"output": "success"},
		Timestamp: time.Now(),
	}, nil
}

func (m *MockTool) Validate(input map[string]interface{}) error {
	if m.validateFunc != nil {
		return m.validateFunc(input)
	}
	return nil
}

// TestStandardToolBox_Register 测试工具注册
func TestStandardToolBox_Register(t *testing.T) {
	tb := NewStandardToolBox()

	tool := NewMockTool("test_tool", "test")
	err := tb.Register(tool)

	require.NoError(t, err)
	assert.Equal(t, 1, tb.Statistics().TotalTools)

	// 测试重复注册
	err = tb.Register(tool)
	assert.Error(t, err)
}

// TestStandardToolBox_Unregister 测试工具注销
func TestStandardToolBox_Unregister(t *testing.T) {
	tb := NewStandardToolBox()

	tool := NewMockTool("test_tool", "test")
	tb.Register(tool)

	err := tb.Unregister("test_tool")
	require.NoError(t, err)
	assert.Equal(t, 0, tb.Statistics().TotalTools)

	// 测试注销不存在的工具
	err = tb.Unregister("nonexistent")
	assert.Error(t, err)
}

// TestStandardToolBox_Get 测试获取工具
func TestStandardToolBox_Get(t *testing.T) {
	tb := NewStandardToolBox()

	tool := NewMockTool("test_tool", "test")
	tb.Register(tool)

	// 获取存在的工具
	retrieved, err := tb.Get("test_tool")
	require.NoError(t, err)
	assert.Equal(t, "test_tool", retrieved.Name())

	// 获取不存在的工具
	_, err = tb.Get("nonexistent")
	assert.Error(t, err)
}

// TestStandardToolBox_List 测试列出工具
func TestStandardToolBox_List(t *testing.T) {
	tb := NewStandardToolBox()

	tb.Register(NewMockTool("tool1", "cat1"))
	tb.Register(NewMockTool("tool2", "cat1"))
	tb.Register(NewMockTool("tool3", "cat2"))

	tools := tb.List()
	assert.Len(t, tools, 3)

	// 按分类列出
	cat1Tools := tb.ListByCategory("cat1")
	assert.Len(t, cat1Tools, 2)
}

// TestStandardToolBox_Execute 测试工具执行
func TestStandardToolBox_Execute(t *testing.T) {
	tb := NewStandardToolBox()

	tool := NewMockTool("test_tool", "test")
	tool.executeFunc = func(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
		return &core.ToolResult{
			Success: true,
			Data: map[string]interface{}{
				"result": "executed",
			},
			Timestamp: time.Now(),
		}, nil
	}
	tb.Register(tool)

	call := &core.ToolCall{
		ID:       "call-1",
		ToolName: "test_tool",
		Input: map[string]interface{}{
			"input": "test",
		},
		Timestamp: time.Now(),
	}

	result, err := tb.Execute(context.Background(), call)
	require.NoError(t, err)
	assert.True(t, result.Result.Success)
	assert.NotEmpty(t, result.Call.ID)
}

// TestStandardToolBox_ExecuteBatch 测试批量执行
func TestStandardToolBox_ExecuteBatch(t *testing.T) {
	tb := NewStandardToolBox()

	tool := NewMockTool("test_tool", "test")
	tb.Register(tool)

	calls := []*core.ToolCall{
		{
			ID:        "call-1",
			ToolName:  "test_tool",
			Input:     map[string]interface{}{"input": "test1"},
			Timestamp: time.Now(),
		},
		{
			ID:        "call-2",
			ToolName:  "test_tool",
			Input:     map[string]interface{}{"input": "test2"},
			Timestamp: time.Now(),
		},
	}

	results, err := tb.ExecuteBatch(context.Background(), calls)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// TestStandardToolBox_Search 测试搜索工具
func TestStandardToolBox_Search(t *testing.T) {
	tb := NewStandardToolBox()

	tb.Register(NewMockTool("read_file", "filesystem"))
	tb.Register(NewMockTool("write_file", "filesystem"))
	tb.Register(NewMockTool("http_request", "network"))

	// 搜索 "file"
	results := tb.Search("file")
	assert.Len(t, results, 2)

	// 搜索 "http"
	results = tb.Search("http")
	assert.Len(t, results, 1)
}

// TestStandardToolBox_Statistics 测试统计信息
func TestStandardToolBox_Statistics(t *testing.T) {
	tb := NewStandardToolBox()

	tool := NewMockTool("test_tool", "test")
	tb.Register(tool)

	// 执行几次
	for i := 0; i < 5; i++ {
		call := &core.ToolCall{
			ToolName:  "test_tool",
			Input:     map[string]interface{}{"input": "test"},
			Timestamp: time.Now(),
		}
		tb.Execute(context.Background(), call)
	}

	stats := tb.Statistics()
	assert.Equal(t, 1, stats.TotalTools)
	assert.Equal(t, int64(5), stats.TotalCalls)
	assert.Equal(t, int64(5), stats.SuccessfulCalls)
	assert.Equal(t, int64(5), stats.ToolUsage["test_tool"])
}

// TestStandardToolBox_CallHistory 测试调用历史
func TestStandardToolBox_CallHistory(t *testing.T) {
	tb := NewStandardToolBox()

	tool := NewMockTool("test_tool", "test")
	tb.Register(tool)

	// 执行一些调用
	for i := 0; i < 3; i++ {
		call := &core.ToolCall{
			ToolName:  "test_tool",
			Input:     map[string]interface{}{"input": "test"},
			Timestamp: time.Now(),
		}
		tb.Execute(context.Background(), call)
	}

	history := tb.GetCallHistory()
	assert.Len(t, history, 3)

	// 清空历史
	tb.ClearHistory()
	history = tb.GetCallHistory()
	assert.Len(t, history, 0)
}

// TestPermissionManager 测试权限管理
func TestPermissionManager(t *testing.T) {
	pm := NewPermissionManager()

	// 设置权限
	pm.SetPermission(&core.ToolPermission{
		UserID:   "user1",
		ToolName: "tool1",
		Allowed:  true,
	})

	// 检查权限
	allowed, err := pm.HasPermission("user1", "tool1")
	require.NoError(t, err)
	assert.True(t, allowed)

	// 拒绝权限
	pm.SetPermission(&core.ToolPermission{
		UserID:   "user1",
		ToolName: "tool2",
		Allowed:  false,
		Reason:   "unauthorized",
	})

	allowed, err = pm.HasPermission("user1", "tool2")
	assert.Error(t, err)
	assert.False(t, allowed)
}

// TestPermissionManager_RateLimit 测试速率限制
func TestPermissionManager_RateLimit(t *testing.T) {
	pm := NewPermissionManager()

	// 设置速率限制：每分钟 3 次
	pm.SetPermission(&core.ToolPermission{
		UserID:            "user1",
		ToolName:          "tool1",
		Allowed:           true,
		MaxCallsPerMinute: 3,
	})

	// 前 3 次应该成功
	for i := 0; i < 3; i++ {
		allowed, err := pm.HasPermission("user1", "tool1")
		require.NoError(t, err)
		assert.True(t, allowed)
	}

	// 第 4 次应该失败（超过速率限制）
	allowed, err := pm.HasPermission("user1", "tool1")
	assert.Error(t, err)
	assert.False(t, allowed)
}

// TestJSONSchemaValidator 测试 JSON Schema 验证
func TestJSONSchemaValidator(t *testing.T) {
	validator := NewJSONSchemaValidator()

	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"name": {
				Type:        "string",
				Description: "名称",
			},
			"age": {
				Type:        "integer",
				Description: "年龄",
			},
		},
		Required: []string{"name"},
	}

	// 有效输入
	validInput := map[string]interface{}{
		"name": "Alice",
		"age":  30,
	}
	err := validator.ValidateInput(schema, validInput)
	assert.NoError(t, err)

	// 缺少必需字段
	invalidInput := map[string]interface{}{
		"age": 30,
	}
	err = validator.ValidateInput(schema, invalidInput)
	assert.Error(t, err)

	// 类型错误
	wrongTypeInput := map[string]interface{}{
		"name": "Alice",
		"age":  "thirty", // 应该是数字
	}
	err = validator.ValidateInput(schema, wrongTypeInput)
	assert.Error(t, err)
}

// BenchmarkToolExecution 基准测试工具执行
func BenchmarkToolExecution(b *testing.B) {
	tb := NewStandardToolBox()
	tool := NewMockTool("test_tool", "test")
	tb.Register(tool)

	call := &core.ToolCall{
		ToolName:  "test_tool",
		Input:     map[string]interface{}{"input": "test"},
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Execute(context.Background(), call)
	}
}
