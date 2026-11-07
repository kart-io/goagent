package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/store"
	"github.com/stretchr/testify/require"
)

type reviewStore struct {
	store.Store
	get func(context.Context, []string, string) (*store.Value, error)
	put func(context.Context, []string, string, interface{}) error
}

func (s *reviewStore) Get(ctx context.Context, ns []string, key string) (*store.Value, error) {
	return s.get(ctx, ns, key)
}

func (s *reviewStore) Put(ctx context.Context, ns []string, key string, value interface{}) error {
	return s.put(ctx, ns, key, value)
}

type reviewRuntimeTool struct {
	*BaseTool
	run func(context.Context, *interfaces.ToolInput, *ToolRuntime) (*interfaces.ToolOutput, error)
}

func (t *reviewRuntimeTool) ExecuteWithRuntime(ctx context.Context, in *interfaces.ToolInput, rt *ToolRuntime) (*interfaces.ToolOutput, error) {
	return t.run(ctx, in, rt)
}

type reviewContextKey struct{}

// 旧适配器传入宿主 Context 时，存储会在本次调用取消后继续写入。
func TestReviewRuntimeAdapterStoreUsesInvocationContext(t *testing.T) {
	for _, mode := range []string{"截止时间", "主动取消"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), reviewContextKey{}, "本次请求")
			var cancel context.CancelFunc
			if mode == "截止时间" {
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()
			wrote := false
			var storeCtx context.Context
			storage := &reviewStore{put: func(got context.Context, _ []string, _ string, _ interface{}) error {
				storeCtx = got
				if mode == "主动取消" {
					cancel()
				}
				select {
				case <-got.Done():
					return got.Err()
				case <-time.After(100 * time.Millisecond):
					wrote = true
					return nil
				}
			}}
			host := NewToolRuntime(context.Background(), NewMockState(), storage)
			tool := &reviewRuntimeTool{BaseTool: NewBaseTool("write", "", `{"type":"object"}`, nil), run: func(_ context.Context, _ *interfaces.ToolInput, rt *ToolRuntime) (*interfaces.ToolOutput, error) {
				return &interfaces.ToolOutput{Success: true}, rt.PutToStore([]string{"n"}, "k", "v")
			}}
			_, err := NewRuntimeToolAdapter(tool, host).Invoke(ctx, &interfaces.ToolInput{})
			require.ErrorIs(t, err, ctx.Err(), "存储必须响应本次请求的取消")
			require.False(t, wrote, "取消后不能产生写入")
			require.Equal(t, "本次请求", storeCtx.Value(reviewContextKey{}))
			_, hasDeadline := storeCtx.Deadline()
			require.Equal(t, mode == "截止时间", hasDeadline)
			require.Equal(t, context.Background(), host.Context, "宿主 Context 不能被覆盖")
		})
	}
}

// 两次调用重叠后再访问 Runtime，能捕获共享 Context 被覆盖和 metadata 锁分裂。
func TestReviewRuntimeAdapterConcurrentViewsPreserveHostResources(t *testing.T) {
	entered := make(chan *ToolRuntime, 2)
	release := make(chan struct{})
	storage := &reviewStore{put: func(ctx context.Context, _ []string, key string, _ interface{}) error {
		if ctx.Value(reviewContextKey{}) != key {
			return fmt.Errorf("请求上下文串扰：%s", key)
		}
		return nil
	}}
	host := NewToolRuntime(context.Background(), NewMockState(), storage).WithMetadata("宿主", "保留")
	host.AgentID, host.SessionID, host.ToolCallID = "agent", "session", "host-call"
	host.StreamWriter = func(interface{}) error { return nil }
	tool := &reviewRuntimeTool{BaseTool: NewBaseTool("views", "", `{"type":"object"}`, nil), run: func(ctx context.Context, in *interfaces.ToolInput, rt *ToolRuntime) (*interfaces.ToolOutput, error) {
		entered <- rt
		<-release
		key := in.Args["key"].(string)
		for range 20 {
			rt.WithMetadata(key, key)
			_, _ = host.GetMetadata("宿主")
		}
		return &interfaces.ToolOutput{Success: true}, rt.PutToStore([]string{"n"}, key, key)
	}}
	adapter := NewRuntimeToolAdapter(tool, host)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"one", "two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := adapter.Invoke(context.WithValue(context.Background(), reviewContextKey{}, key), &interfaces.ToolInput{Args: map[string]interface{}{"key": key}})
			errs <- err
		}()
	}
	first, second := <-entered, <-entered
	close(release)
	wg.Wait()
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	require.NotSame(t, host, first)
	require.NotSame(t, first, second)
	for _, rt := range []*ToolRuntime{first, second} {
		require.Same(t, host.Config, rt.Config)
		require.Same(t, host.Store, rt.Store)
		require.Same(t, host.State, rt.State)
		require.Equal(t, "agent", rt.AgentID)
		require.Equal(t, "session", rt.SessionID)
		require.Equal(t, "host-call", rt.ToolCallID)
		require.NoError(t, rt.Stream("进度"))
	}
	for _, key := range []string{"one", "two"} {
		value, ok := host.GetMetadata(key)
		require.True(t, ok, "工具元数据应继续对宿主可见")
		require.Equal(t, key, value)
	}
}

// 读失败及不匹配记录不能被当成缺失；明确缺失仍允许创建。
func TestReviewSavePreferenceReadSafety(t *testing.T) {
	readErr := errors.New("临时读取失败")
	for _, tc := range []struct {
		name      string
		value     *store.Value
		err       error
		wantWrite bool
		want      map[string]interface{}
	}{
		{"读取故障", nil, readErr, false, nil},
		{"记录类型不匹配", &store.Value{Value: "原始文档"}, nil, false, nil},
		{"空值记录", &store.Value{Value: nil}, nil, false, nil},
		{"空指针map记录", &store.Value{Value: map[string]interface{}(nil)}, nil, false, nil},
		{"明确缺失", nil, nil, true, map[string]interface{}{"theme": "dark"}},
		{"包装的缺失错误", nil, fmt.Errorf("读取：%w", ErrValueNotFound), true, map[string]interface{}{"theme": "dark"}},
		{"合并已有偏好", &store.Value{Value: map[string]interface{}{"language": "zh", "notifications": true}}, nil, true, map[string]interface{}{"language": "zh", "notifications": true, "theme": "dark"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			var saved interface{}
			storage := &reviewStore{get: func(context.Context, []string, string) (*store.Value, error) { return tc.value, tc.err }, put: func(_ context.Context, ns []string, key string, value interface{}) error {
				writes++
				require.Equal(t, []string{"preferences"}, ns)
				require.Equal(t, "u1", key)
				saved = value
				return nil
			}}
			state := NewMockState()
			state.Set("user_id", "u1")
			rt := NewToolRuntime(context.Background(), state, storage)
			out, err := NewSavePreferenceTool().ExecuteWithRuntime(context.Background(), &interfaces.ToolInput{Args: map[string]interface{}{"key": "theme", "value": "dark"}}, rt)
			if tc.wantWrite {
				require.NoError(t, err)
				require.True(t, out.Success)
				require.Equal(t, 1, writes)
				require.Equal(t, tc.want, saved)
			} else {
				require.Error(t, err)
				if tc.err != nil {
					require.ErrorIs(t, err, tc.err)
				}
				require.Zero(t, writes, "读取未确认时不能覆盖记录")
				_, updated := state.Get("pref_theme")
				require.False(t, updated)
			}
		})
	}
}
