package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/store"
)

// MockState for testing
type MockState struct {
	data map[string]interface{}
}

func NewMockState() *MockState {
	return &MockState{
		data: make(map[string]interface{}),
	}
}

func (s *MockState) Get(key string) (interface{}, bool) {
	val, ok := s.data[key]
	return val, ok
}

func (s *MockState) Set(key string, value interface{}) {
	s.data[key] = value
}

func (s *MockState) Update(updates map[string]interface{}) {
	for k, v := range updates {
		s.data[k] = v
	}
}

func (s *MockState) Delete(key string) {
	delete(s.data, key)
}

func (s *MockState) Clear() {
	s.data = make(map[string]interface{})
}

func (s *MockState) Keys() []string {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys
}

func (s *MockState) Size() int {
	return len(s.data)
}

func (s *MockState) Snapshot() map[string]interface{} {
	snapshot := make(map[string]interface{})
	for k, v := range s.data {
		snapshot[k] = v
	}
	return snapshot
}

func (s *MockState) Clone() core.State {
	clone := NewMockState()
	for k, v := range s.data {
		clone.data[k] = v
	}
	return clone
}

// MockStore for testing
type MockStore struct {
	mock.Mock
}

func (m *MockStore) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	args := m.Called(ctx, namespace, key, value)
	return args.Error(0)
}

func (m *MockStore) Get(ctx context.Context, namespace []string, key string) (*store.Value, error) {
	args := m.Called(ctx, namespace, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*store.Value), args.Error(1)
}

func (m *MockStore) Delete(ctx context.Context, namespace []string, key string) error {
	args := m.Called(ctx, namespace, key)
	return args.Error(0)
}

func (m *MockStore) Search(ctx context.Context, namespace []string, filter map[string]interface{}) ([]*store.Value, error) {
	args := m.Called(ctx, namespace, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*store.Value), args.Error(1)
}

func (m *MockStore) List(ctx context.Context, namespace []string) ([]string, error) {
	args := m.Called(ctx, namespace)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockStore) Clear(ctx context.Context, namespace []string) error {
	args := m.Called(ctx, namespace)
	return args.Error(0)
}

// TestToolRuntime tests
func TestToolRuntime_Creation(t *testing.T) {
	ctx := context.Background()
	state := NewMockState()
	mockStore := &MockStore{}

	runtime := NewToolRuntime(ctx, state, mockStore)

	assert.NotNil(t, runtime)
	assert.NotNil(t, runtime.State)
	assert.NotNil(t, runtime.Context)
	assert.NotNil(t, runtime.Store)
	assert.NotNil(t, runtime.Config)
	assert.NotNil(t, runtime.Metadata)
}

func TestToolRuntime_WithConfig(t *testing.T) {
	runtime := &ToolRuntime{
		Metadata: make(map[string]interface{}),
	}

	config := &RuntimeConfig{
		EnableStateAccess: false,
		EnableStoreAccess: false,
		EnableStreaming:   false,
		MaxExecutionTime:  30,
	}

	runtime = runtime.WithConfig(config)

	assert.Equal(t, config, runtime.Config)
	assert.False(t, runtime.Config.EnableStateAccess)
	assert.False(t, runtime.Config.EnableStoreAccess)
	assert.False(t, runtime.Config.EnableStreaming)
}

func TestToolRuntime_WithStreamWriter(t *testing.T) {
	runtime := &ToolRuntime{
		Metadata: make(map[string]interface{}),
	}

	called := false
	writer := func(data interface{}) error {
		called = true
		return nil
	}

	runtime = runtime.WithStreamWriter(writer)
	assert.NotNil(t, runtime.StreamWriter)

	// Test that the writer is callable
	runtime.StreamWriter("test")
	assert.True(t, called)
}

func TestToolRuntime_WithMetadata(t *testing.T) {
	runtime := &ToolRuntime{
		Metadata: make(map[string]interface{}),
	}

	runtime = runtime.WithMetadata("key1", "value1")
	runtime = runtime.WithMetadata("key2", 123)

	value, exists := runtime.GetMetadata("key1")
	assert.True(t, exists)
	assert.Equal(t, "value1", value)

	value, exists = runtime.GetMetadata("key2")
	assert.True(t, exists)
	assert.Equal(t, 123, value)

	_, exists = runtime.GetMetadata("nonexistent")
	assert.False(t, exists)
}

func TestToolRuntime_StateAccess(t *testing.T) {
	state := NewMockState()
	state.Set("user_id", "123")
	state.Set("session_id", "abc")

	runtime := &ToolRuntime{
		State:  state,
		Config: DefaultRuntimeConfig(),
	}

	// Test GetState
	value, err := runtime.GetState("user_id")
	assert.NoError(t, err)
	assert.Equal(t, "123", value)

	// Test SetState
	err = runtime.SetState("new_key", "new_value")
	assert.NoError(t, err)
	val, ok := state.Get("new_key")
	assert.True(t, ok)
	assert.Equal(t, "new_value", val)

	// Test with disabled state access
	runtime.Config.EnableStateAccess = false
	_, err = runtime.GetState("user_id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "state access is disabled")

	err = runtime.SetState("another_key", "value")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "state access is disabled")
}

func TestToolRuntime_StoreAccess(t *testing.T) {
	ctx := context.Background()
	mockStore := &MockStore{}
	runtime := &ToolRuntime{
		Context: ctx,
		Store:   mockStore,
		Config:  DefaultRuntimeConfig(),
	}

	// Setup mock expectations
	mockStore.On("Get", ctx, []string{"users"}, "123").Return(&store.Value{Value: "user_data"}, nil)
	mockStore.On("Put", ctx, []string{"users"}, "123", "updated_data").Return(nil)

	// Test GetFromStore
	value, err := runtime.GetFromStore([]string{"users"}, "123")
	assert.NoError(t, err)
	assert.Equal(t, "user_data", value)

	// Test PutToStore
	err = runtime.PutToStore([]string{"users"}, "123", "updated_data")
	assert.NoError(t, err)

	// Test with disabled store access
	runtime.Config.EnableStoreAccess = false
	_, err = runtime.GetFromStore([]string{"users"}, "123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "store access is disabled")

	err = runtime.PutToStore([]string{"users"}, "123", "data")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "store access is disabled")

	mockStore.AssertExpectations(t)
}

func TestToolRuntime_NamespaceRestrictions(t *testing.T) {
	ctx := context.Background()
	mockStore := &MockStore{}
	runtime := &ToolRuntime{
		Context: ctx,
		Store:   mockStore,
		Config: &RuntimeConfig{
			EnableStoreAccess: true,
			AllowedNamespaces: []string{"users", "preferences"},
		},
	}

	// Test allowed namespace
	mockStore.On("Get", ctx, []string{"users"}, "123").Return(&store.Value{Value: "data"}, nil)

	value, err := runtime.GetFromStore([]string{"users"}, "123")
	assert.NoError(t, err)
	assert.Equal(t, "data", value)

	// Test disallowed namespace
	_, err = runtime.GetFromStore([]string{"secrets"}, "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "access to namespace")

	err = runtime.PutToStore([]string{"secrets"}, "key", "value")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "access to namespace")

	mockStore.AssertExpectations(t)
}

func TestToolRuntime_Stream(t *testing.T) {
	runtime := &ToolRuntime{
		Config: DefaultRuntimeConfig(),
	}

	// Test without stream writer
	err := runtime.Stream("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no stream writer configured")

	// Test with stream writer
	var capturedData interface{}
	runtime.StreamWriter = func(data interface{}) error {
		capturedData = data
		return nil
	}

	err = runtime.Stream("test data")
	assert.NoError(t, err)
	assert.Equal(t, "test data", capturedData)

	// Test with disabled streaming
	runtime.Config.EnableStreaming = false
	err = runtime.Stream("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "streaming is disabled")
}

func TestToolRuntime_Clone(t *testing.T) {
	ctx := context.Background()
	state := NewMockState()
	mockStore := &MockStore{}

	original := NewToolRuntime(ctx, state, mockStore)
	original.ToolCallID = "call_123"
	original.AgentID = "agent_456"
	original.SessionID = "session_789"
	original.WithMetadata("key", "value")

	clone := original.Clone()

	assert.Equal(t, original.ToolCallID, clone.ToolCallID)
	assert.Equal(t, original.AgentID, clone.AgentID)
	assert.Equal(t, original.SessionID, clone.SessionID)
	assert.Equal(t, original.Config, clone.Config)

	// Check metadata was cloned
	value, exists := clone.GetMetadata("key")
	assert.True(t, exists)
	assert.Equal(t, "value", value)

	// Modify clone metadata shouldn't affect original
	clone.WithMetadata("new_key", "new_value")
	_, exists = original.GetMetadata("new_key")
	assert.False(t, exists)
}

// TestRuntimeTool implementations
func TestUserInfoTool(t *testing.T) {
	ctx := context.Background()
	state := NewMockState()
	state.Set("user_id", "user_123")

	mockStore := &MockStore{}
	userData := map[string]interface{}{
		"name":  "John Doe",
		"email": "john@example.com",
	}
	mockStore.On("Get", ctx, []string{"users"}, "user_123").Return(&store.Value{Value: userData}, nil)

	runtime := NewToolRuntime(ctx, state, mockStore)

	// Add stream writer to capture stream data
	var streamedData []interface{}
	runtime.StreamWriter = func(data interface{}) error {
		streamedData = append(streamedData, data)
		return nil
	}

	tool := NewUserInfoTool()
	result, err := tool.ExecuteWithRuntime(ctx, nil, runtime)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, userData, result.Result)
	assert.True(t, result.Success)
	assert.Len(t, streamedData, 2) // Should have streamed 2 status updates
	mockStore.AssertExpectations(t)
}

func TestSavePreferenceTool(t *testing.T) {
	ctx := context.Background()
	state := NewMockState()
	state.Set("user_id", "user_123")

	mockStore := &MockStore{}

	// Setup expectations
	existingPrefs := map[string]interface{}{
		"theme": "light",
	}
	mockStore.On("Get", ctx, []string{"preferences"}, "user_123").Return(&store.Value{Value: existingPrefs}, nil)

	updatedPrefs := map[string]interface{}{
		"theme":    "light",
		"language": "en",
	}
	mockStore.On("Put", ctx, []string{"preferences"}, "user_123", updatedPrefs).Return(nil)

	runtime := NewToolRuntime(ctx, state, mockStore)

	tool := NewSavePreferenceTool()
	input := &ToolInput{
		Args: map[string]interface{}{
			"key":   "language",
			"value": "en",
		},
	}

	result, err := tool.ExecuteWithRuntime(ctx, input, runtime)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	resultData := result.Result.(map[string]interface{})
	assert.Equal(t, "saved", resultData["status"])
	assert.Equal(t, "language", resultData["key"])
	assert.Equal(t, "en", resultData["value"])

	// Check that state was updated
	val, ok := state.Get("pref_language")
	assert.True(t, ok)
	assert.Equal(t, "en", val)

	mockStore.AssertExpectations(t)
}

func TestUpdateStateTool(t *testing.T) {
	ctx := context.Background()
	state := NewMockState()
	runtime := NewToolRuntime(ctx, state, nil)

	tool := NewUpdateStateTool()
	input := &ToolInput{
		Args: map[string]interface{}{
			"key1": "value1",
			"key2": 123,
			"key3": true,
		},
	}

	var streamedData interface{}
	runtime.StreamWriter = func(data interface{}) error {
		streamedData = data
		return nil
	}

	result, err := tool.ExecuteWithRuntime(ctx, input, runtime)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	resultData := result.Result.(map[string]interface{})
	assert.Equal(t, "success", resultData["status"])
	assert.Equal(t, 3, resultData["updated"])

	// Check that state was updated
	val1, ok1 := state.Get("key1")
	assert.True(t, ok1)
	assert.Equal(t, "value1", val1)

	val2, ok2 := state.Get("key2")
	assert.True(t, ok2)
	assert.Equal(t, 123, val2)

	val3, ok3 := state.Get("key3")
	assert.True(t, ok3)
	assert.Equal(t, true, val3)

	// Check stream data
	assert.NotNil(t, streamedData)
}

// TestRuntimeToolAdapter
func TestRuntimeToolAdapter(t *testing.T) {
	ctx := context.Background()
	state := NewMockState()
	runtime := NewToolRuntime(ctx, state, nil)

	tool := NewUserInfoTool()
	adapter := NewRuntimeToolAdapter(tool, runtime)

	assert.Equal(t, tool.Name(), adapter.Name())
	assert.Equal(t, tool.Description(), adapter.Description())
	assert.Equal(t, tool.ArgsSchema(), adapter.ArgsSchema())
}

// TestToolRuntimeManager
func TestToolRuntimeManager(t *testing.T) {
	manager := NewToolRuntimeManager()
	state := NewMockState()
	mockStore := &MockStore{}

	// Create runtime
	runtime1 := manager.CreateRuntime("call_1", state, mockStore)
	assert.NotNil(t, runtime1)
	assert.Equal(t, "call_1", runtime1.ToolCallID)

	// Get runtime
	retrieved, exists := manager.GetRuntime("call_1")
	assert.True(t, exists)
	assert.Equal(t, runtime1, retrieved)

	// Get non-existent runtime
	_, exists = manager.GetRuntime("call_999")
	assert.False(t, exists)

	// Create another runtime
	runtime2 := manager.CreateRuntime("call_2", state, mockStore)
	assert.NotNil(t, runtime2)
	assert.Equal(t, "call_2", runtime2.ToolCallID)

	// Remove runtime
	manager.RemoveRuntime("call_1")
	_, exists = manager.GetRuntime("call_1")
	assert.False(t, exists)

	// call_2 should still exist
	_, exists = manager.GetRuntime("call_2")
	assert.True(t, exists)
}

// Benchmark tests
func BenchmarkToolRuntime_GetState(b *testing.B) {
	state := NewMockState()
	for i := 0; i < 100; i++ {
		state.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}

	runtime := &ToolRuntime{
		State:  state,
		Config: DefaultRuntimeConfig(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runtime.GetState(fmt.Sprintf("key_%d", i%100))
	}
}

func BenchmarkToolRuntime_Clone(b *testing.B) {
	ctx := context.Background()
	state := NewMockState()
	mockStore := &MockStore{}

	runtime := NewToolRuntime(ctx, state, mockStore)
	for i := 0; i < 10; i++ {
		runtime.WithMetadata(fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = runtime.Clone()
	}
}

func BenchmarkToolRuntimeManager_Operations(b *testing.B) {
	manager := NewToolRuntimeManager()
	state := NewMockState()
	mockStore := &MockStore{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		callID := fmt.Sprintf("call_%d", i)
		manager.CreateRuntime(callID, state, mockStore)
		retrieved, _ := manager.GetRuntime(callID)
		_ = retrieved
		manager.RemoveRuntime(callID)
	}
}
