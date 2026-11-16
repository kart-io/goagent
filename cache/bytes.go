package cache

import (
	"context"
	"time"
)

// Bytes 是只收发字节的缓存接口。外部缓存（如 Redis）无法保存任意 Go 值，收窄后误用在编译期暴露。
type Bytes interface {
	// GetBytes 未命中时返回 errors.ErrCacheMiss。
	GetBytes(ctx context.Context, key string) ([]byte, error)
	SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

var _ Bytes = (*SimpleCache)(nil)
