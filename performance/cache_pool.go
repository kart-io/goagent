package performance

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/cache"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/utils/json"
)

// CacheEntry 缓存条目
type CacheEntry struct {
	Output    *core.AgentOutput
	CreatedAt time.Time
	ExpiresAt time.Time
	HitCount  atomic.Int64
}

// CacheConfig 缓存配置
type CacheConfig struct {
	// MaxSize 最大缓存条目数
	MaxSize int
	// TTL 缓存过期时间
	TTL time.Duration
	// CleanupInterval 清理间隔
	CleanupInterval time.Duration
	// EnableStats 是否启用统计
	EnableStats bool
	// KeyGenerator 自定义缓存键生成器
	KeyGenerator func(*core.AgentInput) string
}

// DefaultCacheConfig 返回默认缓存配置
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxSize:         1000,
		TTL:             10 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		EnableStats:     true,
		KeyGenerator:    nil, // 使用默认生成器
	}
}

// CachedAgent 缓存包装器 (简化版本)
//
// 使用 cache.SimpleCache 替代自定义map实现
type CachedAgent struct {
	agent  core.Agent
	config CacheConfig

	cache    *cache.SimpleCache
	closed   bool
	closeOne sync.Once

	// 统计信息
	stats cacheStats
}

// cacheStats 缓存统计信息
type cacheStats struct {
	hits            atomic.Int64 // 缓存命中次数
	misses          atomic.Int64 // 缓存未命中次数
	evictions       atomic.Int64 // 缓存驱逐次数
	expirations     atomic.Int64 // 缓存过期次数
	totalHitTimeNs  atomic.Int64 // 总命中响应时间（纳秒）
	totalMissTimeNs atomic.Int64 // 总未命中响应时间（纳秒）
}

// NewCachedAgent 创建新的缓存 Agent
func NewCachedAgent(agent core.Agent, config CacheConfig) *CachedAgent {
	if config.MaxSize <= 0 {
		config.MaxSize = 1000
	}
	if config.TTL <= 0 {
		config.TTL = 10 * time.Minute
	}
	if config.KeyGenerator == nil {
		config.KeyGenerator = defaultKeyGenerator
	}

	ca := &CachedAgent{
		agent:  agent,
		config: config,
		cache:  cache.NewSimpleCache(config.TTL),
	}

	return ca
}

// Invoke 执行 Agent（带缓存）
func (c *CachedAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	startTime := time.Now()

	// 生成缓存键
	cacheKey := c.config.KeyGenerator(input)

	// 尝试从缓存获取
	if output, found := c.getFromCache(cacheKey); found {
		if c.config.EnableStats {
			hitTime := time.Since(startTime)
			c.stats.totalHitTimeNs.Add(int64(hitTime))
		}
		return output, nil
	}

	// 缓存未命中，执行 Agent
	output, err := c.agent.Invoke(ctx, input)
	if err != nil {
		return nil, err
	}

	// 保存到缓存
	c.putToCache(cacheKey, output)

	if c.config.EnableStats {
		missTime := time.Since(startTime)
		c.stats.totalMissTimeNs.Add(int64(missTime))
	}

	return output, nil
}

// Name 返回 Agent 名称
func (c *CachedAgent) Name() string {
	return c.agent.Name()
}

// Description 返回 Agent 描述
func (c *CachedAgent) Description() string {
	return c.agent.Description()
}

// Capabilities 返回 Agent 能力列表
func (c *CachedAgent) Capabilities() []string {
	return c.agent.Capabilities()
}

// getFromCache 从缓存获取
func (c *CachedAgent) getFromCache(key string) (*core.AgentOutput, bool) {
	val, err := c.cache.Get(context.Background(), key)
	if err != nil {
		c.stats.misses.Add(1)
		return nil, false
	}

	entry := val.(*CacheEntry)
	// 检查是否过期 (SimpleCache已处理)
	c.stats.hits.Add(1)
	entry.HitCount.Add(1)

	// 返回输出的副本
	return copyOutput(entry.Output), true
}

// putToCache 保存到缓存
func (c *CachedAgent) putToCache(key string, output *core.AgentOutput) {
	if c.closed {
		return
	}

	// SimpleCache会自动处理容量和TTL
	entry := &CacheEntry{
		Output:    copyOutput(output),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(c.config.TTL),
	}
	c.cache.Set(context.Background(), key, entry, c.config.TTL)
}

// Invalidate 失效指定缓存键
func (c *CachedAgent) Invalidate(input *core.AgentInput) {
	key := c.config.KeyGenerator(input)
	c.cache.Delete(context.Background(), key)
}

// InvalidateAll 清空所有缓存
func (c *CachedAgent) InvalidateAll() {
	c.cache.Clear(context.Background())
}

// Stats 返回缓存统计信息
func (c *CachedAgent) Stats() CacheStats {
	hits := c.stats.hits.Load()
	misses := c.stats.misses.Load()
	total := hits + misses

	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}

	var avgHitTime, avgMissTime time.Duration
	if hits > 0 {
		avgHitTime = time.Duration(c.stats.totalHitTimeNs.Load() / hits)
	}
	if misses > 0 {
		avgMissTime = time.Duration(c.stats.totalMissTimeNs.Load() / misses)
	}

	cacheStats := c.cache.GetStats()

	return CacheStats{
		Size:        int(cacheStats.Size),
		MaxSize:     c.config.MaxSize,
		Hits:        hits,
		Misses:      misses,
		HitRate:     hitRate,
		Evictions:   c.stats.evictions.Load(),
		Expirations: c.stats.expirations.Load(),
		AvgHitTime:  avgHitTime,
		AvgMissTime: avgMissTime,
	}
}

// CacheStats 缓存统计信息
type CacheStats struct {
	Size        int           // 当前缓存大小
	MaxSize     int           // 最大缓存大小
	Hits        int64         // 缓存命中次数
	Misses      int64         // 缓存未命中次数
	HitRate     float64       // 命中率百分比
	Evictions   int64         // 驱逐次数
	Expirations int64         // 过期次数
	AvgHitTime  time.Duration // 平均命中响应时间
	AvgMissTime time.Duration // 平均未命中响应时间
}

// Close 关闭缓存
func (c *CachedAgent) Close() error {
	c.closeOne.Do(func() {
		c.closed = true
		c.cache.Close()
	})
	return nil
}

// defaultKeyGenerator 默认缓存键生成器
func defaultKeyGenerator(input *core.AgentInput) string {
	// 使用 Task + Instruction + Context 生成缓存键
	data := struct {
		Task        string
		Instruction string
		Context     map[string]interface{}
	}{
		Task:        input.Task,
		Instruction: input.Instruction,
		Context:     input.Context,
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		// 降级到简单哈希
		return fmt.Sprintf("%s:%s", input.Task, input.Instruction)
	}

	hash := sha256.Sum256(jsonBytes)
	return fmt.Sprintf("%x", hash)
}

// copyOutput 创建输出的副本
func copyOutput(output *core.AgentOutput) *core.AgentOutput {
	// 创建浅拷贝
	copied := &core.AgentOutput{
		Result:    output.Result,
		Status:    output.Status,
		Message:   output.Message,
		Steps:     make([]core.AgentStep, len(output.Steps)),
		ToolCalls: make([]core.AgentToolCall, len(output.ToolCalls)),
		Latency:   output.Latency,
		Timestamp: output.Timestamp,
		Metadata:  make(map[string]interface{}),
	}

	// 拷贝 Steps
	copy(copied.Steps, output.Steps)

	// 拷贝 ToolCalls
	copy(copied.ToolCalls, output.ToolCalls)

	// 拷贝 Metadata
	for k, v := range output.Metadata {
		copied.Metadata[k] = v
	}

	return copied
}

// Stream 流式执行 Agent（委托给内部 agent）
func (c *CachedAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	return c.agent.Stream(ctx, input)
}

// Batch 批量执行 Agent（委托给内部 agent）
func (c *CachedAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	return c.agent.Batch(ctx, inputs)
}

// Pipe 连接到另一个 Runnable（委托给内部 agent）
func (c *CachedAgent) Pipe(next core.Runnable[*core.AgentOutput, any]) core.Runnable[*core.AgentInput, any] {
	return c.agent.Pipe(next)
}

// WithCallbacks 添加回调处理器（委托给内部 agent）
func (c *CachedAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return c.agent.WithCallbacks(callbacks...)
}

// WithConfig 配置 Agent（委托给内部 agent）
func (c *CachedAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	return c.agent.WithConfig(config)
}
