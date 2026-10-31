package builder

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/checkpoint"
)

type buildContextCheckpoint struct {
	checkpoint.Checkpointer
	exists func(context.Context) (bool, error)
	load   func(context.Context) (core.State, error)
}

func (c *buildContextCheckpoint) Exists(ctx context.Context, _ string) (bool, error) {
	return c.exists(ctx)
}
func (c *buildContextCheckpoint) Load(ctx context.Context, _ string) (core.State, error) {
	return c.load(ctx)
}

type buildContextCallback struct {
	core.BaseCallback
	start func(context.Context) error
}

func (c buildContextCallback) OnStart(ctx context.Context, _ interface{}) error { return c.start(ctx) }

func TestBuildContextRejectsInvalidContextBeforeInitialization(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want error
	}{
		{"nil", nil, nil}, {"canceled", canceled, context.Canceled}, {"expired", expired, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cp := &buildContextCheckpoint{exists: func(context.Context) (bool, error) {
				t.Error("无效上下文不应开始检查点读取")
				return false, nil
			}}
			agent, err := NewSimpleBuilder(NewMockLLMClient()).WithCheckpointer(cp).BuildContext(tc.ctx)
			if agent != nil || err == nil {
				t.Fatalf("应拒绝上下文，agent=%v err=%v", agent, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("取消原因丢失：%v", err)
			}
			if tc.ctx == nil && !strings.Contains(err.Error(), "context") {
				t.Fatalf("应明确说明 context 为空：%v", err)
			}
		})
	}
}

func TestBuildContextCheckpointCancellationStopsInitialization(t *testing.T) {
	for _, stage := range []string{"exists", "load"} {
		for _, cooperative := range []bool{true, false} {
			name := stage + "/ignores_context"
			if cooperative {
				name = stage + "/cooperative"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				entered := make(chan struct{})
				release := make(chan struct{}, 1)
				defer close(release)
				wait := func(ctx context.Context) error {
					close(entered)
					if cooperative {
						<-ctx.Done()
						return ctx.Err()
					}
					<-release
					return nil
				}
				cp := &buildContextCheckpoint{
					exists: func(ctx context.Context) (bool, error) {
						if stage == "exists" {
							return true, wait(ctx)
						}
						return true, nil
					},
					load: func(ctx context.Context) (core.State, error) {
						if stage != "load" {
							t.Error("Exists 取消后不应 Load")
							return core.NewAgentState(), nil
						}
						return core.NewAgentState(), wait(ctx)
					},
				}
				cb := buildContextCallback{start: func(context.Context) error { t.Error("检查点取消后不应 OnStart"); return nil }}
				done := make(chan error, 1)
				go func() {
					agent, err := NewSimpleBuilder(NewMockLLMClient()).WithCheckpointer(cp).WithCallbacks(&cb).BuildContext(ctx)
					if agent != nil {
						done <- errors.New("取消后返回了 Agent")
						return
					}
					done <- err
				}()
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("检查点未开始")
				}
				cancel()
				release <- struct{}{}
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("取消原因丢失：%v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("构建未响应检查点取消")
				}
			})
		}
	}
}

func TestBuildContextPropagatesContextAndStopsAfterCallbackCancellation(t *testing.T) {
	type key struct{}
	for _, cancelDuringStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "cancel"}[cancelDuringStart], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "宿主值"))
			defer cancel()
			calls := 0
			first := buildContextCallback{start: func(got context.Context) error {
				calls++
				if got != ctx || got.Value(key{}) != "宿主值" {
					t.Error("OnStart 未收到原始宿主 context")
				}
				if cancelDuringStart {
					cancel()
				}
				return nil
			}}
			second := buildContextCallback{start: func(context.Context) error { calls++; return nil }}
			agent, err := NewSimpleBuilder(NewMockLLMClient()).WithCallbacks(&first, &second).BuildContext(ctx)
			if cancelDuringStart {
				if agent != nil || !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatalf("取消后应停止后续回调：agent=%v err=%v calls=%d", agent, err, calls)
				}
			} else if agent == nil || err != nil || calls != 2 {
				t.Fatalf("正常构建失败：agent=%v err=%v calls=%d", agent, err, calls)
			}
		})
	}
}

func TestBuildContextPreservesInitializationFailures(t *testing.T) {
	sentinel := errors.New("宿主初始化失败")
	for _, stage := range []string{"exists", "load", "typed_nil", "callback"} {
		t.Run(stage, func(t *testing.T) {
			cp := &buildContextCheckpoint{
				exists: func(context.Context) (bool, error) {
					if stage == "exists" {
						return false, sentinel
					}
					return true, nil
				},
				load: func(context.Context) (core.State, error) {
					if stage == "load" {
						return nil, sentinel
					}
					if stage == "typed_nil" {
						var state *core.AgentState
						return state, nil
					}
					return core.NewAgentState(), nil
				},
			}
			cb := buildContextCallback{start: func(context.Context) error {
				if stage == "callback" {
					return sentinel
				}
				t.Error("检查点失败后不应回调")
				return nil
			}}
			agent, err := NewSimpleBuilder(NewMockLLMClient()).WithCheckpointer(cp).WithCallbacks(&cb).BuildContext(context.Background())
			if agent != nil || err == nil {
				t.Fatalf("初始化失败返回了 Agent：%v %v", agent, err)
			}
			if stage != "typed_nil" && !errors.Is(err, sentinel) {
				t.Fatalf("宿主错误链丢失：%v", err)
			}
		})
	}
}

func TestBuildKeepsBackgroundInitialization(t *testing.T) {
	cb := buildContextCallback{start: func(ctx context.Context) error {
		if ctx != context.Background() {
			t.Error("Build 应使用 Background")
		}
		return nil
	}}
	agent, err := NewSimpleBuilder(NewMockLLMClient()).WithCallbacks(&cb).Build()
	if agent == nil || err != nil {
		t.Fatalf("旧 Build 不兼容：%v %v", agent, err)
	}
}
