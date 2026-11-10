package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetiredCompatibilityAPIsCannotReturn(t *testing.T) {
	for _, tc := range []struct {
		name, file, source string
		bad                bool
	}{
		{"工具旧名别名", "tools/executor_tool.go", `package tools; type ToolCall = ToolCallRequest`, true},
		{"工具旧名结构", "tools/executor_tool.go", `package tools; type ToolCall struct{}`, true},
		{"其他工具类型名", "tools/executor_tool.go", `package tools; type ToolCallRequest struct{}`, false},
		{"旧流别名", "core/streaming_alias.go", `package core; import e "github.com/kart-io/goagent/core/execution";type LegacyStreamChunk=e.LegacyStreamChunk`, true},
		{"旧流工厂变量", "core/streaming_alias.go", `package core;var NewTextChunk=makeChunk`, true},
		{"旧流协议", "core/execution/streaming.go", `package execution;type StreamOutput interface{Next()}`, true},
		{"旧流元数据枚举", "core/execution/streaming.go", `package execution;const ChunkTypeMetadata="metadata"`, true},
		{"旧流空闲状态", "core/execution/streaming.go", `package execution;const StreamStateIdle="idle"`, true},
		{"现行泛型流", "core/stream.go", `package core;type StreamChunk[T any] struct{Data T}`, false},
		{"旧缓存构造", "cache/base.go", `package cache;func NewInMemoryCache(){}`, true},
		{"旧多级缓存", "cache/base.go", `package cache;type MultiTierCache struct{}`, true},
		{"现行缓存", "cache/simple_cache.go", `package cache;type SimpleCache struct{}`, false},
		{"无效缓存配置", "cache/base.go", `package cache;type CacheConfig struct{MaxSize int}`, true},
		{"缓存配置别名", "cache/base.go", `package cache;type historicalConfig struct{MaxSize int};type CacheConfig=historicalConfig`, true},
		{"缓存配置底层结构", "cache/base.go", `package cache;type historicalConfig struct{CleanupInterval int};type CacheConfig historicalConfig`, true},
		{"泛型配置别名", "cache/base.go", `package cache;type historicalConfig[T any] struct{Type T};type CacheConfig=historicalConfig[string]`, true},
		{"有效缓存配置", "cache/base.go", `package cache;type CacheConfig struct{Enabled bool}`, false},
		{"旧钩子接口", "tools/middleware/middleware.go", `package middleware;type ToolMiddleware interface{OnError()}`, true},
		{"旧钩子转换", "tools/middleware/middleware.go", `package middleware;func Chain(){}`, true},
		{"工具包重建中间件装配", "tools/tool_wrapper.go", `package tools;import "github.com/kart-io/goagent/interfaces";import mw "github.com/kart-io/goagent/tools/middleware";func WithMiddleware(t interfaces.Tool,m ...mw.ToolMiddlewareFunc)interfaces.Tool{return t}`, true},
		{"工具包重建包装类型", "tools/tool_wrapper.go", `package tools;type MiddlewareTool struct{}`, true},
		{"中间件包装配入口", "tools/middleware/wrap.go", `package middleware;func Wrap(){}`, false},
		{"无效运行配置", "core/execution/runtime.go", `package execution;type RuntimeConfig struct{EnableAutoSave bool}`, true},
		{"空指标", "core/execution/runtime.go", `package execution;type RuntimeMetrics struct{}`, true},
		{"旧指标方法", "core/execution/runtime.go", `package execution;func(m *RuntimeManager[C,S])Metrics(){}`, true},
		{"旧运行构造参数", "core/execution/runtime.go", `package execution;func NewRuntimeManager[C,S any](c any){}`, true},
		{"函数变量重建运行构造", "core/execution/runtime.go", `package execution;var NewRuntimeManager=func(c any){}`, true},
		{"无配置运行构造", "core/execution/runtime.go", `package execution;func NewRuntimeManager[C,S any](){}`, false},
		{"工具独立有效配置", "tools/tool_runtime.go", `package tools;type RuntimeConfig struct{MaxConcurrency int}`, false},
		{"无效重试字段", "core/runnable.go", `package core;type RunnableConfig struct{RetryPolicy any}`, true},
		{"匿名嵌入旧配置", "core/runnable.go", `package core;type oldOptions struct{RetryPolicy int};type RunnableConfig struct{oldOptions}`, true},
		{"指针嵌入旧配置", "core/runnable.go", `package core;type oldOptions struct{RetryPolicy int};type RunnableConfig struct{*oldOptions}`, true},
		{"循环嵌入终止", "core/runnable.go", `package core;type options struct{*RunnableConfig;RetryPolicy int};type RunnableConfig struct{*options}`, true},
		{"普通嵌套字段", "core/runnable.go", `package core;type options struct{RetryPolicy int};type RunnableConfig struct{Options options}`, false},
		{"无效重试类型", "core/runnable.go", `package core;type RetryPolicy struct{}`, true},
		{"工具独立重试", "tools/executor_tool.go", `package tools;type RetryPolicy struct{MaxRetries int}`, false},
		{"停用切回字段", "core/checkpoint/distributed.go", `package checkpoint;type DistributedCheckpointerConfig struct{FailbackDelay int}`, true},
		{"切回死分支", "core/checkpoint/distributed.go", `package checkpoint;func(c *DistributedCheckpointer)tryFailback(){}`, true},
		{"别名接回切回方法", "core/checkpoint/distributed.go", `package checkpoint;type DistributedCheckpointer struct{};type Alias=DistributedCheckpointer;func(c *Alias)tryFailback(){}`, true},
		{"链式别名接回切回方法", "core/checkpoint/distributed.go", `package checkpoint;type DistributedCheckpointer struct{};type A=DistributedCheckpointer;type B=A;func(c *B)tryFailback(){}`, true},
		{"独立定义类型的方法", "core/checkpoint/distributed.go", `package checkpoint;type DistributedCheckpointer struct{};type Other DistributedCheckpointer;func(c *Other)tryFailback(){}`, false},
		{"在线旧版本入口", "core/durable/operation.go", `package durable;const operationVersionV1="goagent-operation-v1"`, true},
		{"离线迁移版本", "core/durable/migrate/operation.go", `package migrate;const operationVersionV1="goagent-operation-v1"`, false},
		{"现行协议别名", "codec/json/json.go", `package json;import j "encoding/json";type RawMessage=j.RawMessage`, false},
		{"正常领域方法", "core/other.go", `package core;func(c *Other)Metrics(){}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tc.file, tc.source, 0)
			require.NoError(t, err)
			var messages []string
			for _, finding := range inspect(fset, tc.file, file) {
				messages = append(messages, finding.message)
			}
			require.Equal(t, tc.bad, len(messages) > 0, messages)
		})
	}
}

func TestRetiredConfigBindingsAcrossFiles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  int
	}{
		{"跨文件结构别名", map[string]string{"cache/a.go": `package cache;type CacheConfig=fields`, "cache/z.go": `package cache;type fields struct{MaxSize int}`}, 1},
		{"跨文件匿名嵌入", map[string]string{"core/a.go": `package core;type RunnableConfig struct{*fields}`, "core/z.go": `package core;type fields struct{RetryPolicy int}`}, 1},
		{"跨文件接收者别名", map[string]string{"core/checkpoint/a.go": `package checkpoint;type DistributedCheckpointer struct{};func(c *Alias)tryFailback(){}`, "core/checkpoint/z.go": `package checkpoint;type Alias=DistributedCheckpointer`}, 1},
		{"其它目录不污染", map[string]string{"cache/a.go": `package cache;type CacheConfig=fields;type fields struct{Enabled bool}`, "store/z.go": `package cache;type fields struct{MaxSize int}`}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, source := range tc.files {
				file := filepath.Join(root, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(file), 0755))
				require.NoError(t, os.WriteFile(file, []byte(source), 0644))
			}
			count, err := scan(root)
			require.NoError(t, err)
			require.Equal(t, tc.want, count)
		})
	}
}
