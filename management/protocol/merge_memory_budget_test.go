package protocol_test

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/adapters"
	"github.com/kart-io/goagent/management/httpapi"
	"github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/kart-io/goagent/memory"
	"github.com/kart-io/goagent/testing/mocks"
	"github.com/kart-io/goagent/tools"
)

// 不用测试运行函数代替模型准入：HTTP 的宿主额度必须贯穿 Worker、
// NativeFactory、重开的 /4 记忆召回及第二次真实模型请求。
func TestRemoteBudgetWithReopenedMemoryV4(t *testing.T) {
	for _, tc := range []struct {
		name       string
		requests   int
		wantState  mg.TaskState
		wantKind   string
		wantModels int
		secondTool bool
	}{
		{"召回不额外消耗模型额度且两次请求成功", 2, mg.Succeeded, "", 2, false},
		{"客户端不能放开宿主的一次模型额度", 1, mg.Failed, "budget_exhausted", 1, false},
		{"客户端不能放开宿主的一次工具额度", 3, mg.Failed, "budget_exhausted", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			const remembered = "coffee preference: Colombian beans"
			memoryDir := t.TempDir()
			openMemory := func() *memory.Memory {
				store, err := durable.NewFileStore(memoryDir)
				require.NoError(t, err)
				mem, err := memory.New(memory.Config{Durable: store})
				require.NoError(t, err)
				return mem
			}
			namespace := memory.WithNamespace(ctx, "tenant-a", "tester")
			_, err := openMemory().Remember(namespace, memory.Record{Content: remembered, Importance: .9})
			require.NoError(t, err)
			requireMergeMemoryV4(t, memoryDir)
			mem := openMemory()

			rules := []mocks.ScriptedRule{mocks.ToolCallReply("查询工具", mocks.ToolCall("lookup-1", "lookup", `{}`))}
			if tc.secondTool {
				rules = append(rules, mocks.ToolCallReply("超额查询工具", mocks.ToolCall("lookup-2", "lookup", `{}`)))
			}
			rules = append(rules, mocks.TextReply("最终答案", "已完成查询"))
			model := mocks.NewScriptedClient(rules...)
			var effects atomic.Int64
			registry := tools.NewRegistry()
			require.NoError(t, registry.Register(tools.NewFunctionTool("lookup", "查询咖啡库存", `{"type":"object"}`, func(context.Context, map[string]any) (any, error) {
				effects.Add(1)
				return "有库存", nil
			})))
			dispatcher, err := tools.NewDispatcher(registry, tools.DispatchConfig{})
			require.NoError(t, err)
			factory := mergeMemoryFactory{NativeFactory: adapters.NativeFactory{
				Check: func(context.Context, mg.Version) error { return nil },
				BuildAgent: func(context.Context, mg.Version, mg.Resources, tools.Approver) (*builder.SimpleAgentBuilder, func(context.Context) error, error) {
					return builder.NewSimpleBuilder(model).WithTools(dispatcher.Tools()...).WithLongTermMemory(mem), nil, nil
				},
			}}
			f := managementtest.NewFixture(t)
			cfg := f.Config()
			cfg.Factories["offline"] = factory
			remoteConfig := httpapi.Config{ActorFromRequest: func(*http.Request) (mg.Actor, error) { return f.Actor, nil }}
			cfg.DefaultTaskBudget = mg.TaskBudget{MaxModelRequests: tc.requests, MaxToolCalls: 1, Timeout: time.Minute}
			f.Service, err = mg.New(cfg)
			require.NoError(t, err)
			se := f.ReadySession(t)
			handler, err := httpapi.New(f.Service, remoteConfig)
			require.NoError(t, err)
			server := httptest.NewServer(handler)
			defer server.Close()
			body, err := json.Marshal(map[string]any{
				"session_id": se.ID,
				"input":      "coffee recommendation",
				"budget":     map[string]int{"max_model_requests": 1000, "max_tool_calls": 1000},
			})
			require.NoError(t, err)
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/tasks", strings.NewReader(string(body)))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "memory-budget")
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusAccepted, response.StatusCode)
			var submitted mg.Task
			require.NoError(t, json.Decode(response.Body, &submitted))
			require.Equal(t, tc.requests, submitted.Budget.MaxModelRequests)
			require.Equal(t, 1, submitted.Budget.MaxToolCalls)
			f.StartWorker(t)
			var completed mg.Task
			require.Eventually(t, func() bool {
				completed, err = f.Service.GetTask(ctx, f.Actor, submitted.ID)
				return err == nil && completed.State.Terminal()
			}, 5*time.Second, 5*time.Millisecond)
			require.Equal(t, tc.wantState, completed.State, completed.ErrorKind)
			require.Equal(t, tc.wantKind, completed.ErrorKind)
			if completed.State == mg.Succeeded {
				require.JSONEq(t, `"已完成查询"`, string(completed.Result))
			}
			require.EqualValues(t, 1, effects.Load(), "额度不足时已完成的工具也不得重跑")
			requests := model.Requests()
			require.Len(t, requests, tc.wantModels, "超额模型请求不得抵达客户端")
			for _, actual := range requests {
				messages, err := json.Marshal(actual.Messages)
				require.NoError(t, err)
				require.Contains(t, string(messages), remembered, "重开的 /4 存储必须仍能装配召回")
			}
			events, err := f.Sessions.Read(ctx, se.LogID, 0)
			require.NoError(t, err)
			for _, event := range events {
				require.NotContains(t, string(event.Data), remembered, "管理会话及持久日志仅保存召回引用")
			}
		})
	}
}

// 测试宿主只绑定受信身份命名空间，保留 Worker 提供的原预算和 journal。
type mergeMemoryFactory struct{ adapters.NativeFactory }

func (f mergeMemoryFactory) Build(ctx context.Context, v mg.Version, resources mg.Resources) (mg.Binding, error) {
	binding, err := f.NativeFactory.Build(ctx, v, resources)
	if err == nil {
		binding.Runtime = mergeMemoryRuntime{binding.Runtime}
	}
	return binding, err
}

type mergeMemoryRuntime struct{ mg.Runtime }

func (r mergeMemoryRuntime) Run(ctx context.Context, spec mg.RunSpec) (mg.RunResult, error) {
	return r.Runtime.Run(memory.WithNamespace(ctx, "tenant-a", "tester"), spec)
}

func (r mergeMemoryRuntime) Inspect(ctx context.Context, spec mg.RunSpec) (mg.RunEvidence, error) {
	return r.Runtime.Inspect(memory.WithNamespace(ctx, "tenant-a", "tester"), spec)
}

func requireMergeMemoryV4(t *testing.T, dir string) {
	t.Helper()
	found := false
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var header struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			return err
		}
		found = found || header.Version == "memory-head/4"
		return nil
	}))
	require.True(t, found, "必须实际落盘 memory-head/4，避免只测内存缓存")
}
