package httpclient_test

import (
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/utils/httpclient"
)

// TestClientCaching 测试客户端缓存功能
func TestClientCaching(t *testing.T) {
	// 清空缓存
	httpclient.ClearCache()

	config1 := &httpclient.Config{
		Timeout: 30 * time.Second,
		Headers: map[string]string{
			"Content-Type": "application/json",
			"X-API-Key":    "test-key-1",
		},
		BaseURL: "https://api.example.com",
	}

	config2 := &httpclient.Config{
		Timeout: 30 * time.Second,
		Headers: map[string]string{
			"Content-Type": "application/json",
			"X-API-Key":    "test-key-1",
		},
		BaseURL: "https://api.example.com",
	}

	config3 := &httpclient.Config{
		Timeout: 30 * time.Second,
		Headers: map[string]string{
			"Content-Type": "application/json",
			"X-API-Key":    "test-key-2", // 不同的 API Key
		},
		BaseURL: "https://api.example.com",
	}

	// 测试相同配置返回相同实例
	client1 := httpclient.GetOrCreateClient(config1)
	client2 := httpclient.GetOrCreateClient(config2)

	if client1 != client2 {
		t.Error("相同配置应该返回相同的客户端实例")
	}

	// 测试不同配置返回不同实例
	client3 := httpclient.GetOrCreateClient(config3)

	if client1 == client3 {
		t.Error("不同配置应该返回不同的客户端实例")
	}

	// 验证缓存统计
	stats := httpclient.GetCacheStats()
	if stats["cached_clients"] != 2 {
		t.Errorf("期望缓存 2 个客户端，实际缓存 %d 个", stats["cached_clients"])
	}
}

// TestClientCachingConcurrency 测试并发场景下的客户端缓存
func TestClientCachingConcurrency(t *testing.T) {
	httpclient.ClearCache()

	config := &httpclient.Config{
		Timeout: 30 * time.Second,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		BaseURL: "https://api.test.com",
	}

	const goroutines = 100
	var wg sync.WaitGroup
	clients := make([]*httpclient.Client, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(index int) {
			defer wg.Done()
			clients[index] = httpclient.GetOrCreateClient(config)
		}(i)
	}
	wg.Wait()

	// 验证所有 goroutine 获取的是同一个客户端实例
	firstClient := clients[0]
	for i := 1; i < goroutines; i++ {
		if clients[i] != firstClient {
			t.Errorf("并发场景下，第 %d 个客户端实例与第一个不同", i)
		}
	}

	// 验证只创建了一个客户端
	stats := httpclient.GetCacheStats()
	if stats["cached_clients"] != 1 {
		t.Errorf("并发场景下应该只缓存 1 个客户端，实际缓存 %d 个", stats["cached_clients"])
	}
}

// TestClearCache 测试清空缓存功能
func TestClearCache(t *testing.T) {
	httpclient.ClearCache()

	config := &httpclient.Config{
		BaseURL: "https://api.test.com",
	}

	// 创建客户端
	_ = httpclient.GetOrCreateClient(config)

	// 验证缓存中有客户端
	stats := httpclient.GetCacheStats()
	if stats["cached_clients"] != 1 {
		t.Error("清空前应该有 1 个缓存的客户端")
	}

	// 清空缓存
	httpclient.ClearCache()

	// 验证缓存已清空
	stats = httpclient.GetCacheStats()
	if stats["cached_clients"] != 0 {
		t.Error("清空后应该没有缓存的客户端")
	}
}

// TestCachingEnabled 测试启用/禁用缓存
func TestCachingEnabled(t *testing.T) {
	httpclient.ClearCache()

	config := &httpclient.Config{
		BaseURL: "https://api.test.com",
	}

	// 启用缓存
	httpclient.SetCachingEnabled(true)
	client1 := httpclient.GetOrCreateClient(config)
	client2 := httpclient.GetOrCreateClient(config)

	if client1 != client2 {
		t.Error("启用缓存时，相同配置应该返回相同实例")
	}

	// 禁用缓存
	httpclient.SetCachingEnabled(false)
	client3 := httpclient.GetOrCreateClient(config)
	client4 := httpclient.GetOrCreateClient(config)

	if client3 == client4 {
		t.Error("禁用缓存时，每次应该返回新实例")
	}

	// 重新启用缓存（为后续测试）
	httpclient.SetCachingEnabled(true)
}

// BenchmarkClientCreation 基准测试：直接创建客户端
func BenchmarkClientCreation(b *testing.B) {
	config := &httpclient.Config{
		Timeout: 30 * time.Second,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		BaseURL: "https://api.test.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = httpclient.NewClient(config)
	}
}

// BenchmarkClientCaching 基准测试：使用缓存获取客户端
func BenchmarkClientCaching(b *testing.B) {
	httpclient.ClearCache()

	config := &httpclient.Config{
		Timeout: 30 * time.Second,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		BaseURL: "https://api.test.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = httpclient.GetOrCreateClient(config)
	}
}

// BenchmarkClientCachingParallel 基准测试：并发使用缓存
func BenchmarkClientCachingParallel(b *testing.B) {
	httpclient.ClearCache()

	config := &httpclient.Config{
		Timeout: 30 * time.Second,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		BaseURL: "https://api.test.com",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = httpclient.GetOrCreateClient(config)
		}
	})
}
