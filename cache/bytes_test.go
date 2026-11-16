package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

func TestSimpleCacheBytes(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		setup   func(c *SimpleCache)
		want    string
		wantErr func(error) bool
	}{
		{
			name:  "写入后读回",
			setup: func(c *SimpleCache) { _ = c.SetBytes(ctx, "k", []byte("v"), time.Minute) },
			want:  "v",
		},
		{
			name:    "不存在的键未命中",
			setup:   func(c *SimpleCache) {},
			wantErr: func(err error) bool { return errors.Is(err, agentErrors.ErrCacheMiss) },
		},
		{
			name: "过期后未命中",
			setup: func(c *SimpleCache) {
				_ = c.SetBytes(ctx, "k", []byte("v"), 10*time.Millisecond)
				time.Sleep(30 * time.Millisecond)
			},
			wantErr: func(err error) bool { return errors.Is(err, agentErrors.ErrCacheMiss) },
		},
		{
			name:    "非字节值返回 CodeInvalidInput",
			setup:   func(c *SimpleCache) { _ = c.Set(ctx, "k", "str", time.Minute) },
			wantErr: func(err error) bool { return agentErrors.IsCode(err, agentErrors.CodeInvalidInput) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewSimpleCache(time.Hour, WithoutBackgroundCleanup())
			tt.setup(c)
			got, err := c.GetBytes(ctx, "k")
			if tt.wantErr != nil {
				if err == nil || !tt.wantErr(err) {
					t.Fatalf("错误不符合预期: %v", err)
				}
				return
			}
			if err != nil || string(got) != tt.want {
				t.Fatalf("got %q, err %v, want %q", got, err, tt.want)
			}
		})
	}
}
