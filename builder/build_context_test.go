package builder

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
)

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
			cb := buildContextCallback{start: func(context.Context) error {
				t.Error("无效上下文不应开始初始化")
				return nil
			}}
			agent, err := NewSimpleBuilder(NewMockLLMClient()).WithCallbacks(&cb).BuildContext(tc.ctx)
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
	cb := buildContextCallback{start: func(context.Context) error { return sentinel }}
	agent, err := NewSimpleBuilder(NewMockLLMClient()).WithCallbacks(&cb).BuildContext(context.Background())
	if agent != nil || err == nil {
		t.Fatalf("初始化失败返回了 Agent：%v %v", agent, err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("宿主错误链丢失：%v", err)
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
