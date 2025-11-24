package core

import (
	"context"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPluginBridge_TypedToDynamic tests the type-safe to dynamic adapter
func TestPluginBridge_TypedToDynamic(t *testing.T) {
	// Create a type-safe runnable
	typedRunnable := NewRunnableFunc(func(ctx context.Context, input string) (int, error) {
		return len(input), nil
	})

	// Wrap it as dynamic
	adapter := NewTypedToDynamicAdapter(typedRunnable, "string-length")

	t.Run("InvokeDynamic with correct type", func(t *testing.T) {
		result, err := adapter.InvokeDynamic(context.Background(), "hello")
		require.NoError(t, err)
		assert.Equal(t, 5, result)
	})

	t.Run("InvokeDynamic with JSON-compatible type", func(t *testing.T) {
		// String should work through JSON conversion
		result, err := adapter.InvokeDynamic(context.Background(), "world")
		require.NoError(t, err)
		assert.Equal(t, 5, result)
	})

	t.Run("TypeInfo returns correct information", func(t *testing.T) {
		info := adapter.TypeInfo()
		assert.Equal(t, "string-length", info.Name)
		assert.NotNil(t, info.InputType)
		assert.NotNil(t, info.OutputType)
	})

	t.Run("BatchDynamic works correctly", func(t *testing.T) {
		inputs := []any{"a", "bb", "ccc"}
		results, err := adapter.BatchDynamic(context.Background(), inputs)
		require.NoError(t, err)
		assert.Len(t, results, 3)
		assert.Equal(t, 1, results[0])
		assert.Equal(t, 2, results[1])
		assert.Equal(t, 3, results[2])
	})
}

// TestPluginBridge_DynamicToTyped tests the dynamic to type-safe adapter
func TestPluginBridge_DynamicToTyped(t *testing.T) {
	// Create a dynamic runnable (simulating a plugin)
	dynamicRunnable := &mockDynamicRunnable{
		invokeFunc: func(ctx context.Context, input any) (any, error) {
			str := input.(string)
			return len(str), nil
		},
	}

	// Wrap it as type-safe
	adapter := NewDynamicToTypedAdapter[string, int](dynamicRunnable)

	t.Run("Invoke with type safety", func(t *testing.T) {
		result, err := adapter.Invoke(context.Background(), "hello")
		require.NoError(t, err)
		assert.Equal(t, 5, result)
	})

	t.Run("Batch with type safety", func(t *testing.T) {
		inputs := []string{"a", "bb", "ccc"}
		results, err := adapter.Batch(context.Background(), inputs)
		require.NoError(t, err)
		assert.Len(t, results, 3)
		assert.Equal(t, 1, results[0])
		assert.Equal(t, 2, results[1])
		assert.Equal(t, 3, results[2])
	})
}

// TestPluginRegistry tests the plugin registry functionality
func TestPluginRegistry(t *testing.T) {
	registry := NewPluginRegistry()

	// Create a typed runnable
	stringProcessor := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
		return "processed: " + input, nil
	})

	t.Run("RegisterTyped and GetTyped", func(t *testing.T) {
		err := RegisterTyped(registry, "string-processor", stringProcessor, &PluginMetadata{
			Version:     "1.0.0",
			Description: "Processes strings",
		})
		require.NoError(t, err)

		// Get back as typed
		typed, err := GetTyped[string, string](registry, "string-processor")
		require.NoError(t, err)

		result, err := typed.Invoke(context.Background(), "test")
		require.NoError(t, err)
		assert.Equal(t, "processed: test", result)
	})

	t.Run("Get as dynamic", func(t *testing.T) {
		dynamic, err := registry.Get("string-processor")
		require.NoError(t, err)

		result, err := dynamic.InvokeDynamic(context.Background(), "dynamic-test")
		require.NoError(t, err)
		assert.Equal(t, "processed: dynamic-test", result)
	})

	t.Run("Duplicate registration fails", func(t *testing.T) {
		err := RegisterTyped(registry, "string-processor", stringProcessor, nil)
		assert.Error(t, err)
	})

	t.Run("List returns all plugins", func(t *testing.T) {
		list := registry.List()
		assert.Contains(t, list, "string-processor")
	})

	t.Run("GetMetadata returns plugin info", func(t *testing.T) {
		meta, err := registry.GetMetadata("string-processor")
		require.NoError(t, err)
		assert.Equal(t, "string-processor", meta.Name)
		assert.Equal(t, "1.0.0", meta.Version)
	})

	t.Run("Unregister removes plugin", func(t *testing.T) {
		err := registry.Unregister("string-processor")
		require.NoError(t, err)

		_, err = registry.Get("string-processor")
		assert.Error(t, err)
	})

	t.Run("Get non-existent plugin fails", func(t *testing.T) {
		_, err := registry.Get("non-existent")
		assert.Error(t, err)
	})
}

// TestPluginRegistry_TypeMismatch tests type checking at runtime
func TestPluginRegistry_TypeMismatch(t *testing.T) {
	registry := NewPluginRegistry()

	// Register a string->int runnable
	intProcessor := NewRunnableFunc(func(ctx context.Context, input string) (int, error) {
		return len(input), nil
	})

	err := RegisterTyped(registry, "int-processor", intProcessor, nil)
	require.NoError(t, err)

	t.Run("Type mismatch detected at GetTyped", func(t *testing.T) {
		// Try to get as string->string (wrong output type)
		_, err := GetTyped[string, string](registry, "int-processor")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type mismatch")
	})
}

// TestGlobalPluginRegistry tests the global registry singleton
func TestGlobalPluginRegistry(t *testing.T) {
	registry1 := GlobalPluginRegistry()
	registry2 := GlobalPluginRegistry()
	assert.Same(t, registry1, registry2)
}

// TestConvertToType tests the type conversion helper
func TestConvertToType(t *testing.T) {
	t.Run("Direct type assertion", func(t *testing.T) {
		result, err := convertToType[string]("hello")
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("Integer conversion", func(t *testing.T) {
		result, err := convertToType[int](42)
		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("Nil value", func(t *testing.T) {
		result, err := convertToType[string](nil)
		require.NoError(t, err)
		assert.Equal(t, "", result)
	})

	t.Run("JSON conversion for struct", func(t *testing.T) {
		input := map[string]interface{}{
			"name": "test",
			"age":  30,
		}

		type Person struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}

		result, err := convertToType[Person](input)
		require.NoError(t, err)
		assert.Equal(t, "test", result.Name)
		assert.Equal(t, 30, result.Age)
	})
}

// mockDynamicRunnable is a mock implementation of DynamicRunnable for testing
type mockDynamicRunnable struct {
	invokeFunc func(context.Context, any) (any, error)
}

func (m *mockDynamicRunnable) InvokeDynamic(ctx context.Context, input any) (any, error) {
	return m.invokeFunc(ctx, input)
}

func (m *mockDynamicRunnable) StreamDynamic(ctx context.Context, input any) (<-chan DynamicStreamChunk, error) {
	ch := make(chan DynamicStreamChunk, 1)
	go func() {
		defer close(ch)
		result, err := m.invokeFunc(ctx, input)
		ch <- DynamicStreamChunk{Data: result, Error: err, Done: true}
	}()
	return ch, nil
}

func (m *mockDynamicRunnable) BatchDynamic(ctx context.Context, inputs []any) ([]any, error) {
	results := make([]any, len(inputs))
	for i, input := range inputs {
		result, err := m.invokeFunc(ctx, input)
		if err != nil {
			return nil, err
		}
		results[i] = result
	}
	return results, nil
}

func (m *mockDynamicRunnable) TypeInfo() TypeInfo {
	return TypeInfo{Name: "mock"}
}

// =============================================================================
// Panic Recovery Tests for Plugin Bridge
// =============================================================================

// TestTypedToDynamicAdapter_InvokeDynamic_PanicRecovery 测试 InvokeDynamic 的 panic 保护
func TestTypedToDynamicAdapter_InvokeDynamic_PanicRecovery(t *testing.T) {
	t.Run("Panic in typed Invoke", func(t *testing.T) {
		// 创建会 panic 的 Runnable
		panicRunnable := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
			panic("plugin crashed in Invoke")
		})

		// 创建 adapter
		adapter := NewTypedToDynamicAdapter(panicRunnable, "panic-test")

		// 调用应该捕获 panic 并返回错误
		result, err := adapter.InvokeDynamic(context.Background(), "test")

		// 验证
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "plugin crashed in Invoke")

		// 验证是 AgentError
		agentErr, ok := err.(*agentErrors.AgentError)
		require.True(t, ok)
		assert.Equal(t, agentErrors.CodeInternal, agentErr.Code)
	})

	t.Run("Nil pointer in plugin", func(t *testing.T) {
		panicRunnable := NewRunnableFunc(func(ctx context.Context, input int) (int, error) {
			var ptr *int
			return *ptr, nil // nil pointer dereference
		})

		adapter := NewTypedToDynamicAdapter(panicRunnable, "nil-ptr-test")

		result, err := adapter.InvokeDynamic(context.Background(), 42)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "panic recovered")
	})
}

// TestTypedToDynamicAdapter_StreamDynamic_PanicRecovery 测试 StreamDynamic 的 panic 保护
func TestTypedToDynamicAdapter_StreamDynamic_PanicRecovery(t *testing.T) {
	t.Run("Panic when creating stream", func(t *testing.T) {
		// 创建在 Stream 调用时会 panic 的 Runnable
		panicRunnable := &mockPanicInStreamRunnable{}

		adapter := NewTypedToDynamicAdapter[string, string](panicRunnable, "stream-panic-test")

		// 调用 StreamDynamic 应该捕获 panic
		stream, err := adapter.StreamDynamic(context.Background(), "test")

		// 验证 panic 被捕获
		require.Error(t, err)
		assert.Nil(t, stream)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "panic in Stream method")
	})
}

// TestTypedToDynamicAdapter_BatchDynamic_PanicRecovery 测试 BatchDynamic 的 panic 保护
func TestTypedToDynamicAdapter_BatchDynamic_PanicRecovery(t *testing.T) {
	t.Run("Panic in Batch", func(t *testing.T) {
		// 创建在 Batch 时会 panic 的 Runnable
		panicRunnable := &mockPanicInBatchRunnable{}

		adapter := NewTypedToDynamicAdapter[string, string](panicRunnable, "batch-panic-test")

		// 调用 BatchDynamic 应该捕获 panic
		results, err := adapter.BatchDynamic(context.Background(), []any{"input1", "input2"})

		// 验证 panic 被捕获
		require.Error(t, err)
		assert.Nil(t, results)
		assert.Contains(t, err.Error(), "panic recovered")
		assert.Contains(t, err.Error(), "panic in Batch method")
	})
}

// TestPluginRegistry_WithPanicPlugin 测试通过 Registry 使用会 panic 的插件
func TestPluginRegistry_WithPanicPlugin(t *testing.T) {
	registry := NewPluginRegistry()

	// 注册会 panic 的插件
	panicRunnable := NewRunnableFunc(func(ctx context.Context, input string) (string, error) {
		panic("registered plugin panicked")
	})
	adapter := NewTypedToDynamicAdapter(panicRunnable, "panic-plugin")

	err := registry.Register("panic-plugin", adapter, &PluginMetadata{
		Name:        "panic-plugin",
		Version:     "1.0.0",
		Description: "A plugin that panics",
	})
	require.NoError(t, err)

	// 通过 registry 获取并调用
	plugin, err := registry.Get("panic-plugin")
	require.NoError(t, err)

	result, err := plugin.InvokeDynamic(context.Background(), "test")

	// 验证 panic 被捕获，系统继续运行
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "panic recovered")
	assert.Contains(t, err.Error(), "registered plugin panicked")

	// 验证 registry 仍然可用
	plugins := registry.List()
	assert.Contains(t, plugins, "panic-plugin")
}

// =============================================================================
// Mock Runnables for Panic Tests
// =============================================================================

// mockPanicInStreamRunnable 在 Stream 方法中 panic
type mockPanicInStreamRunnable struct{}

func (m *mockPanicInStreamRunnable) Invoke(ctx context.Context, input string) (string, error) {
	return "ok", nil
}

func (m *mockPanicInStreamRunnable) Stream(ctx context.Context, input string) (<-chan StreamChunk[string], error) {
	panic("panic in Stream method")
}

func (m *mockPanicInStreamRunnable) Batch(ctx context.Context, inputs []string) ([]string, error) {
	return nil, nil
}

func (m *mockPanicInStreamRunnable) Pipe(next Runnable[string, any]) Runnable[string, any] {
	return nil
}

func (m *mockPanicInStreamRunnable) WithCallbacks(callbacks ...Callback) Runnable[string, string] {
	return m
}

func (m *mockPanicInStreamRunnable) WithConfig(config RunnableConfig) Runnable[string, string] {
	return m
}

// mockPanicInBatchRunnable 在 Batch 方法中 panic
type mockPanicInBatchRunnable struct{}

func (m *mockPanicInBatchRunnable) Invoke(ctx context.Context, input string) (string, error) {
	return "ok", nil
}

func (m *mockPanicInBatchRunnable) Stream(ctx context.Context, input string) (<-chan StreamChunk[string], error) {
	return nil, nil
}

func (m *mockPanicInBatchRunnable) Batch(ctx context.Context, inputs []string) ([]string, error) {
	panic("panic in Batch method")
}

func (m *mockPanicInBatchRunnable) Pipe(next Runnable[string, any]) Runnable[string, any] {
	return nil
}

func (m *mockPanicInBatchRunnable) WithCallbacks(callbacks ...Callback) Runnable[string, string] {
	return m
}

func (m *mockPanicInBatchRunnable) WithConfig(config RunnableConfig) Runnable[string, string] {
	return m
}
