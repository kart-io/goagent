package tools_test

import (
	"testing"

	"github.com/kart-io/goagent/toolkits"
	"github.com/kart-io/goagent/tools/compute"
	"github.com/kart-io/goagent/tools/search"
)

// TestToolkitBasic 测试基础工具集
func TestToolkitBasic(t *testing.T) {
	tool1 := compute.NewCalculatorTool()
	tool2 := search.NewSearchTool(search.NewMockSearchEngine())

	toolkit := toolkits.NewBaseToolkit(tool1, tool2)

	tools := toolkit.GetTools()
	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	names := toolkit.GetToolNames()
	if len(names) != 2 {
		t.Errorf("Expected 2 tool names, got %d", len(names))
	}

	tool, err := toolkit.GetToolByName("calculator")
	if err != nil {
		t.Fatalf("GetToolByName failed: %v", err)
	}

	if tool.Name() != "calculator" {
		t.Errorf("Expected tool name 'calculator', got '%s'", tool.Name())
	}

	_, err = toolkit.GetToolByName("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent tool")
	}
}

// TestToolkitBuilder 测试工具集构建器
func TestToolkitBuilder(t *testing.T) {
	toolkit := toolkits.NewToolkitBuilder().
		WithCalculator().
		WithSearch(nil).
		Build()

	tools := toolkit.GetTools()
	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	names := toolkit.GetToolNames()
	expectedNames := map[string]bool{
		"calculator": true,
		"search":     true,
	}

	for _, name := range names {
		if !expectedNames[name] {
			t.Errorf("Unexpected tool name: %s", name)
		}
	}
}

// TestToolRegistry 测试工具注册表
func TestToolRegistry(t *testing.T) {
	registry := toolkits.NewToolRegistry()

	tool := compute.NewCalculatorTool()
	err := registry.Register(tool)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	retrieved, err := registry.Get("calculator")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Name() != "calculator" {
		t.Errorf("Expected tool name 'calculator', got '%s'", retrieved.Name())
	}

	// Test duplicate registration
	err = registry.Register(tool)
	if err == nil {
		t.Error("Expected error for duplicate registration")
	}

	// Test unregister
	err = registry.Unregister("calculator")
	if err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	_, err = registry.Get("calculator")
	if err == nil {
		t.Error("Expected error after unregistration")
	}
}
