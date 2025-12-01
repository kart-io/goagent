// Package core provides plugin bridge utilities for dynamic component loading.
//
// This file addresses the fundamental tension between:
//   - Compile-time type safety (Go generics)
//   - Runtime flexibility (plugin systems, dynamic registration)
//
// The solution introduces a layered architecture:
//  1. DynamicRunnable - Runtime-safe interface using any types
//  2. TypedAdapter - Converts between generic and dynamic versions
//  3. PluginRegistry - Manages dynamically loaded components
package core

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/utils/json"
)

// =============================================================================
// Layer 1: Dynamic Interfaces (Plugin-Compatible)
// =============================================================================

// DynamicRunnable 是插件兼容的动态 Runnable 接口
//
// 设计理念:
//   - 在插件边界使用 any 类型，保证运行时兼容性
//   - 通过 InputSchema/OutputSchema 提供运行时类型信息
//   - 支持 JSON 序列化的输入输出，便于跨进程通信
//
// 使用场景:
//   - Go 原生插件 (.so)
//   - 动态注册的第三方组件
//   - RPC/HTTP 调用的远程组件
type DynamicRunnable interface {
	// Invoke 执行动态输入，返回动态输出
	InvokeDynamic(ctx context.Context, input any) (any, error)

	// Stream 流式执行
	StreamDynamic(ctx context.Context, input any) (<-chan DynamicStreamChunk, error)

	// Batch 批量执行
	BatchDynamic(ctx context.Context, inputs []any) ([]any, error)

	// TypeInfo 返回类型信息，用于运行时验证
	TypeInfo() TypeInfo
}

// DynamicStreamChunk 动态流式输出块
type DynamicStreamChunk struct {
	Data  any   `json:"data,omitempty"`
	Error error `json:"-"`
	Done  bool  `json:"done"`
}

// TypeInfo 提供运行时类型信息
//
// 用于:
//   - 插件注册时的类型验证
//   - 运行时类型检查和转换
//   - 文档生成和 API 描述
type TypeInfo struct {
	// InputType 输入类型的反射信息
	InputType reflect.Type `json:"-"`

	// OutputType 输出类型的反射信息
	OutputType reflect.Type `json:"-"`

	// InputSchema JSON Schema 描述（可选，用于验证）
	InputSchema string `json:"input_schema,omitempty"`

	// OutputSchema JSON Schema 描述（可选，用于验证）
	OutputSchema string `json:"output_schema,omitempty"`

	// Name 组件名称
	Name string `json:"name"`

	// Description 组件描述
	Description string `json:"description,omitempty"`
}

// =============================================================================
// Layer 2: Type-Safe Adapters
// =============================================================================

// TypedToDynamicAdapter 将泛型 Runnable 转换为 DynamicRunnable
//
// 这是桥接编译时类型安全和运行时灵活性的关键组件
type TypedToDynamicAdapter[I, O any] struct {
	typed    Runnable[I, O]
	typeInfo TypeInfo
}

// NewTypedToDynamicAdapter 创建类型安全到动态的适配器
func NewTypedToDynamicAdapter[I, O any](typed Runnable[I, O], name string) *TypedToDynamicAdapter[I, O] {
	var inputZero I
	var outputZero O

	return &TypedToDynamicAdapter[I, O]{
		typed: typed,
		typeInfo: TypeInfo{
			InputType:  reflect.TypeOf(inputZero),
			OutputType: reflect.TypeOf(outputZero),
			Name:       name,
		},
	}
}

// InvokeDynamic 实现 DynamicRunnable 接口
func (a *TypedToDynamicAdapter[I, O]) InvokeDynamic(ctx context.Context, input any) (any, error) {
	// 类型转换
	typedInput, err := convertToType[I](input)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "input type conversion failed").
			WithComponent("typed_to_dynamic_adapter").
			WithOperation("invoke_dynamic").
			WithContext("expected_type", reflect.TypeOf((*I)(nil)).Elem().String())
	}

	// 调用类型安全版本（带 panic 保护 - 防止第三方插件绕过隔离）
	output, err := safeInvoke(a.typed.Invoke, ctx, typedInput)
	if err != nil {
		return nil, err
	}

	return output, nil
}

// StreamDynamic 实现流式执行
func (a *TypedToDynamicAdapter[I, O]) StreamDynamic(ctx context.Context, input any) (<-chan DynamicStreamChunk, error) {
	// 类型转换
	typedInput, err := convertToType[I](input)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "input type conversion failed").
			WithComponent("typed_to_dynamic_adapter").
			WithOperation("stream_dynamic")
	}

	// 调用类型安全版本（带 panic 保护 - 使用可配置的 PanicHandler）
	var typedStream <-chan StreamChunk[O]
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = panicToError(ctx, "typed_to_dynamic_adapter", "stream", r)
			}
		}()
		typedStream, err = a.typed.Stream(ctx, typedInput)
	}()

	if err != nil {
		return nil, err
	}

	// 转换流
	dynamicStream := make(chan DynamicStreamChunk)
	go func() {
		defer close(dynamicStream)
		for chunk := range typedStream {
			dynamicStream <- DynamicStreamChunk{
				Data:  chunk.Data,
				Error: chunk.Error,
				Done:  chunk.Done,
			}
		}
	}()

	return dynamicStream, nil
}

// BatchDynamic 实现批量执行
func (a *TypedToDynamicAdapter[I, O]) BatchDynamic(ctx context.Context, inputs []any) ([]any, error) {
	// 转换输入
	typedInputs := make([]I, len(inputs))
	for i, input := range inputs {
		typed, err := convertToType[I](input)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "input type conversion failed").
				WithComponent("typed_to_dynamic_adapter").
				WithOperation("batch_dynamic").
				WithContext("index", i)
		}
		typedInputs[i] = typed
	}

	// 调用类型安全版本（带 panic 保护 - 使用可配置的 PanicHandler）
	var outputs []O
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = panicToError(ctx, "typed_to_dynamic_adapter", "batch", r)
			}
		}()
		outputs, err = a.typed.Batch(ctx, typedInputs)
	}()

	if err != nil {
		return nil, err
	}

	// 转换输出
	dynamicOutputs := make([]any, len(outputs))
	for i, output := range outputs {
		dynamicOutputs[i] = output
	}

	return dynamicOutputs, nil
}

// TypeInfo 返回类型信息
func (a *TypedToDynamicAdapter[I, O]) TypeInfo() TypeInfo {
	return a.typeInfo
}

// DynamicToTypedAdapter 将 DynamicRunnable 转换为泛型 Runnable
//
// 用于在类型安全的代码中使用动态加载的组件
type DynamicToTypedAdapter[I, O any] struct {
	dynamic   DynamicRunnable
	validator func(any) error // 可选的运行时验证器
}

// NewDynamicToTypedAdapter 创建动态到类型安全的适配器
func NewDynamicToTypedAdapter[I, O any](dynamic DynamicRunnable) *DynamicToTypedAdapter[I, O] {
	return &DynamicToTypedAdapter[I, O]{
		dynamic: dynamic,
	}
}

// WithValidator 添加运行时验证器
func (a *DynamicToTypedAdapter[I, O]) WithValidator(validator func(any) error) *DynamicToTypedAdapter[I, O] {
	a.validator = validator
	return a
}

// Invoke 实现 Runnable[I, O] 接口
func (a *DynamicToTypedAdapter[I, O]) Invoke(ctx context.Context, input I) (O, error) {
	var zero O

	// 可选的运行时验证
	if a.validator != nil {
		if err := a.validator(input); err != nil {
			return zero, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "input validation failed").
				WithComponent("dynamic_to_typed_adapter").
				WithOperation("invoke")
		}
	}

	// 调用动态版本
	output, err := a.dynamic.InvokeDynamic(ctx, input)
	if err != nil {
		return zero, err
	}

	// 类型转换
	typedOutput, err := convertToType[O](output)
	if err != nil {
		return zero, agentErrors.Wrap(err, agentErrors.CodeInvalidOutput, "output type conversion failed").
			WithComponent("dynamic_to_typed_adapter").
			WithOperation("invoke")
	}

	return typedOutput, nil
}

// Stream 实现流式执行
func (a *DynamicToTypedAdapter[I, O]) Stream(ctx context.Context, input I) (<-chan StreamChunk[O], error) {
	// 调用动态版本
	dynamicStream, err := a.dynamic.StreamDynamic(ctx, input)
	if err != nil {
		return nil, err
	}

	// 转换流
	typedStream := make(chan StreamChunk[O])
	go func() {
		defer close(typedStream)
		for chunk := range dynamicStream {
			var typedData O
			if chunk.Data != nil {
				var convErr error
				typedData, convErr = convertToType[O](chunk.Data)
				if convErr != nil {
					typedStream <- StreamChunk[O]{
						Error: agentErrors.Wrap(convErr, agentErrors.CodeInvalidOutput, "stream chunk type conversion failed"),
						Done:  true,
					}
					return
				}
			}
			typedStream <- StreamChunk[O]{
				Data:  typedData,
				Error: chunk.Error,
				Done:  chunk.Done,
			}
		}
	}()

	return typedStream, nil
}

// Batch 实现批量执行
func (a *DynamicToTypedAdapter[I, O]) Batch(ctx context.Context, inputs []I) ([]O, error) {
	// 转换输入
	dynamicInputs := make([]any, len(inputs))
	for i, input := range inputs {
		dynamicInputs[i] = input
	}

	// 调用动态版本
	outputs, err := a.dynamic.BatchDynamic(ctx, dynamicInputs)
	if err != nil {
		return nil, err
	}

	// 转换输出
	typedOutputs := make([]O, len(outputs))
	for i, output := range outputs {
		typed, err := convertToType[O](output)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidOutput, "output type conversion failed").
				WithComponent("dynamic_to_typed_adapter").
				WithOperation("batch").
				WithContext("index", i)
		}
		typedOutputs[i] = typed
	}

	return typedOutputs, nil
}

// Pipe 连接到另一个 Runnable
func (a *DynamicToTypedAdapter[I, O]) Pipe(next Runnable[O, any]) Runnable[I, any] {
	return NewRunnablePipe[I, O, any](a, next)
}

// WithCallbacks 添加回调
func (a *DynamicToTypedAdapter[I, O]) WithCallbacks(callbacks ...Callback) Runnable[I, O] {
	// 简化实现：返回自身（回调需要更复杂的包装）
	return a
}

// WithConfig 配置 Runnable
func (a *DynamicToTypedAdapter[I, O]) WithConfig(config RunnableConfig) Runnable[I, O] {
	return a
}

// =============================================================================
// Layer 3: Plugin Registry
// =============================================================================

// PluginRegistry 插件注册中心
//
// 管理动态加载的组件，提供:
//   - 组件注册和发现
//   - 类型信息查询
//   - 版本管理
type PluginRegistry struct {
	mu       sync.RWMutex
	plugins  map[string]DynamicRunnable
	metadata map[string]*PluginMetadata
}

// PluginMetadata 插件元数据
type PluginMetadata struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	TypeInfo    TypeInfo `json:"type_info"`
}

// NewPluginRegistry 创建插件注册中心
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins:  make(map[string]DynamicRunnable),
		metadata: make(map[string]*PluginMetadata),
	}
}

// Register 注册插件
func (r *PluginRegistry) Register(name string, plugin DynamicRunnable, metadata *PluginMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[name]; exists {
		return agentErrors.New(agentErrors.CodeAlreadyExists, "plugin already registered").
			WithComponent("plugin_registry").
			WithOperation("register").
			WithContext("name", name)
	}

	r.plugins[name] = plugin
	if metadata != nil {
		metadata.Name = name
		metadata.TypeInfo = plugin.TypeInfo()
		r.metadata[name] = metadata
	} else {
		r.metadata[name] = &PluginMetadata{
			Name:     name,
			TypeInfo: plugin.TypeInfo(),
		}
	}

	return nil
}

// RegisterTyped 注册类型安全的组件（自动适配为 DynamicRunnable）
func RegisterTyped[I, O any](r *PluginRegistry, name string, runnable Runnable[I, O], metadata *PluginMetadata) error {
	adapter := NewTypedToDynamicAdapter(runnable, name)
	return r.Register(name, adapter, metadata)
}

// Get 获取插件
func (r *PluginRegistry) Get(name string) (DynamicRunnable, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, exists := r.plugins[name]
	if !exists {
		return nil, agentErrors.New(agentErrors.CodeNotFound, "plugin not found").
			WithComponent("plugin_registry").
			WithOperation("get").
			WithContext("name", name)
	}

	return plugin, nil
}

// GetTyped 获取类型安全的插件包装
func GetTyped[I, O any](r *PluginRegistry, name string) (Runnable[I, O], error) {
	dynamic, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	// 类型检查（可选但推荐）
	typeInfo := dynamic.TypeInfo()
	var inputZero I
	var outputZero O
	expectedInputType := reflect.TypeOf(inputZero)
	expectedOutputType := reflect.TypeOf(outputZero)

	if typeInfo.InputType != nil && expectedInputType != nil {
		if typeInfo.InputType != expectedInputType {
			return nil, agentErrors.New(agentErrors.CodeTypeMismatch, "input type mismatch").
				WithComponent("plugin_registry").
				WithOperation("get_typed").
				WithContext("expected", expectedInputType.String()).
				WithContext("actual", typeInfo.InputType.String())
		}
	}

	if typeInfo.OutputType != nil && expectedOutputType != nil {
		if typeInfo.OutputType != expectedOutputType {
			return nil, agentErrors.New(agentErrors.CodeTypeMismatch, "output type mismatch").
				WithComponent("plugin_registry").
				WithOperation("get_typed").
				WithContext("expected", expectedOutputType.String()).
				WithContext("actual", typeInfo.OutputType.String())
		}
	}

	return NewDynamicToTypedAdapter[I, O](dynamic), nil
}

// Unregister 注销插件
func (r *PluginRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[name]; !exists {
		return agentErrors.New(agentErrors.CodeNotFound, "plugin not found").
			WithComponent("plugin_registry").
			WithOperation("unregister").
			WithContext("name", name)
	}

	delete(r.plugins, name)
	delete(r.metadata, name)
	return nil
}

// List 列出所有插件
func (r *PluginRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}

// GetMetadata 获取插件元数据
func (r *PluginRegistry) GetMetadata(name string) (*PluginMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	meta, exists := r.metadata[name]
	if !exists {
		return nil, agentErrors.New(agentErrors.CodeNotFound, "plugin metadata not found").
			WithComponent("plugin_registry").
			WithOperation("get_metadata").
			WithContext("name", name)
	}

	return meta, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// convertToType 智能类型转换
//
// 支持以下转换策略:
//  1. 直接类型断言
//  2. JSON 序列化/反序列化
//  3. reflect 基础类型转换
func convertToType[T any](value any) (T, error) {
	var zero T

	if value == nil {
		return zero, nil
	}

	// 策略 1: 直接类型断言
	if typed, ok := value.(T); ok {
		return typed, nil
	}

	// 策略 2: 指针解引用
	if reflect.TypeOf(value).Kind() == reflect.Ptr {
		elem := reflect.ValueOf(value).Elem()
		if typed, ok := elem.Interface().(T); ok {
			return typed, nil
		}
	}

	// 策略 3: JSON 序列化/反序列化（最通用但最慢）
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return zero, fmt.Errorf("failed to marshal value: %w", err)
	}

	var result T
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return zero, fmt.Errorf("failed to unmarshal to target type: %w", err)
	}

	return result, nil
}

// =============================================================================
// Global Registry
// =============================================================================

var (
	globalRegistry     *PluginRegistry
	globalRegistryOnce sync.Once
)

// GlobalPluginRegistry 获取全局插件注册中心
func GlobalPluginRegistry() *PluginRegistry {
	globalRegistryOnce.Do(func() {
		globalRegistry = NewPluginRegistry()
	})
	return globalRegistry
}
