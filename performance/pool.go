package performance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kart-io/goagent/core"
)

var (
	// ErrPoolClosed 池已关闭错误
	ErrPoolClosed = errors.New("agent pool is closed")
	// ErrPoolTimeout 获取 Agent 超时错误
	ErrPoolTimeout = errors.New("timeout acquiring agent from pool")
)

// AgentFactory Agent 工厂函数
type AgentFactory func() (core.Agent, error)

// PoolConfig 池配置
type PoolConfig struct {
	// InitialSize 初始池大小
	InitialSize int
	// MaxSize 最大池大小
	MaxSize int
	// IdleTimeout 空闲超时时间（超过此时间的空闲 Agent 将被回收）
	IdleTimeout time.Duration
	// MaxLifetime Agent 最大生命周期（超过此时间的 Agent 将被销毁）
	MaxLifetime time.Duration
	// AcquireTimeout 获取 Agent 超时时间
	AcquireTimeout time.Duration
	// CleanupInterval 清理间隔
	CleanupInterval time.Duration
}

// DefaultPoolConfig 返回默认池配置
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		InitialSize:     5,
		MaxSize:         50,
		IdleTimeout:     5 * time.Minute,
		MaxLifetime:     30 * time.Minute,
		AcquireTimeout:  10 * time.Second,
		CleanupInterval: 1 * time.Minute,
	}
}

// pooledAgent 池化的 Agent 包装器
type pooledAgent struct {
	agent      core.Agent
	createdAt  time.Time
	lastUsedAt time.Time
	inUse      bool
}

// AgentPool Agent 池
type AgentPool struct {
	factory AgentFactory
	config  PoolConfig

	agents    []*pooledAgent
	mu        sync.RWMutex
	cond      *sync.Cond
	closed    bool
	closeOnce sync.Once

	// 统计信息
	stats poolStats

	// 清理协程控制
	stopCleanup chan struct{}
	wg          sync.WaitGroup
}

// poolStats 池统计信息
type poolStats struct {
	created    atomic.Int64 // 创建的 Agent 总数
	acquired   atomic.Int64 // 获取的 Agent 总数
	released   atomic.Int64 // 释放的 Agent 总数
	recycled   atomic.Int64 // 回收的 Agent 总数
	waitCount  atomic.Int64 // 等待次数
	waitTimeNs atomic.Int64 // 总等待时间（纳秒）
}

// NewAgentPool 创建新的 Agent 池
func NewAgentPool(factory AgentFactory, config PoolConfig) (*AgentPool, error) {
	if factory == nil {
		return nil, errors.New("factory cannot be nil")
	}

	if config.InitialSize < 0 {
		config.InitialSize = 0
	}
	if config.MaxSize <= 0 {
		config.MaxSize = 50
	}
	if config.InitialSize > config.MaxSize {
		config.InitialSize = config.MaxSize
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 5 * time.Minute
	}
	if config.MaxLifetime <= 0 {
		config.MaxLifetime = 30 * time.Minute
	}
	if config.AcquireTimeout <= 0 {
		config.AcquireTimeout = 10 * time.Second
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}

	pool := &AgentPool{
		factory:     factory,
		config:      config,
		agents:      make([]*pooledAgent, 0, config.MaxSize),
		stopCleanup: make(chan struct{}),
	}
	pool.cond = sync.NewCond(&pool.mu)

	// 预创建初始 Agent
	for i := 0; i < config.InitialSize; i++ {
		agent, err := pool.createAgent()
		if err != nil {
			// 清理已创建的 Agent
			pool.Close()
			return nil, err
		}
		pool.agents = append(pool.agents, agent)
	}

	// 启动清理协程
	pool.wg.Add(1)
	go pool.cleanupLoop()

	return pool, nil
}

// Acquire 从池中获取一个 Agent
func (p *AgentPool) Acquire(ctx context.Context) (core.Agent, error) {
	startTime := time.Now()
	defer func() {
		waitTime := time.Since(startTime)
		p.stats.waitTimeNs.Add(int64(waitTime))
	}()

	// 创建带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, p.config.AcquireTimeout)
	defer cancel()

	// 等待可用 Agent
	doneCh := make(chan *pooledAgent, 1)
	errCh := make(chan error, 1)

	go func() {
		p.mu.Lock()
		defer p.mu.Unlock()

		for {
			// 检查是否已关闭
			if p.closed {
				errCh <- ErrPoolClosed
				return
			}

			// 查找空闲 Agent
			for _, agent := range p.agents {
				if !agent.inUse {
					agent.inUse = true
					agent.lastUsedAt = time.Now()
					p.stats.acquired.Add(1)
					doneCh <- agent
					return
				}
			}

			// 如果池未满，创建新 Agent
			if len(p.agents) < p.config.MaxSize {
				newAgent, err := p.createAgent()
				if err != nil {
					errCh <- err
					return
				}
				newAgent.inUse = true
				newAgent.lastUsedAt = time.Now()
				p.agents = append(p.agents, newAgent)
				p.stats.acquired.Add(1)
				doneCh <- newAgent
				return
			}

			// 等待有 Agent 被释放
			p.stats.waitCount.Add(1)
			p.cond.Wait()
		}
	}()

	// 等待结果或超时
	select {
	case agent := <-doneCh:
		return agent.agent, nil
	case err := <-errCh:
		return nil, err
	case <-timeoutCtx.Done():
		p.cond.Broadcast() // 唤醒等待的协程
		return nil, ErrPoolTimeout
	}
}

// Release 将 Agent 归还到池中
func (p *AgentPool) Release(agent core.Agent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrPoolClosed
	}

	// 查找 Agent
	for _, pa := range p.agents {
		if pa.agent == agent {
			if !pa.inUse {
				return errors.New("agent is not in use")
			}
			pa.inUse = false
			pa.lastUsedAt = time.Now()
			p.stats.released.Add(1)

			// 唤醒等待的协程
			p.cond.Signal()
			return nil
		}
	}

	return errors.New("agent not found in pool")
}

// Execute 执行 Agent（自动借用和归还）
func (p *AgentPool) Execute(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	agent, err := p.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Release(agent)

	return agent.Invoke(ctx, input)
}

// Close 关闭池
func (p *AgentPool) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.cond.Broadcast() // 唤醒所有等待的协程
		p.mu.Unlock()

		// 停止清理协程
		close(p.stopCleanup)
		p.wg.Wait()
	})
	return nil
}

// Stats 返回池的统计信息
func (p *AgentPool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	activeCount := 0
	idleCount := 0
	for _, agent := range p.agents {
		if agent.inUse {
			activeCount++
		} else {
			idleCount++
		}
	}

	var avgWaitTime time.Duration
	waitCount := p.stats.waitCount.Load()
	if waitCount > 0 {
		avgWaitTime = time.Duration(p.stats.waitTimeNs.Load() / waitCount)
	}

	return PoolStats{
		TotalCount:     len(p.agents),
		ActiveCount:    activeCount,
		IdleCount:      idleCount,
		MaxSize:        p.config.MaxSize,
		CreatedTotal:   p.stats.created.Load(),
		AcquiredTotal:  p.stats.acquired.Load(),
		ReleasedTotal:  p.stats.released.Load(),
		RecycledTotal:  p.stats.recycled.Load(),
		WaitCount:      waitCount,
		AvgWaitTime:    avgWaitTime,
		UtilizationPct: float64(activeCount) / float64(p.config.MaxSize) * 100,
	}
}

// PoolStats 池统计信息
type PoolStats struct {
	TotalCount     int           // 当前池中 Agent 总数
	ActiveCount    int           // 正在使用的 Agent 数
	IdleCount      int           // 空闲 Agent 数
	MaxSize        int           // 最大池大小
	CreatedTotal   int64         // 创建的 Agent 总数
	AcquiredTotal  int64         // 获取的 Agent 总数
	ReleasedTotal  int64         // 释放的 Agent 总数
	RecycledTotal  int64         // 回收的 Agent 总数
	WaitCount      int64         // 等待次数
	AvgWaitTime    time.Duration // 平均等待时间
	UtilizationPct float64       // 利用率百分比
}

// createAgent 创建新的 Agent
func (p *AgentPool) createAgent() (*pooledAgent, error) {
	agent, err := p.factory()
	if err != nil {
		return nil, err
	}

	p.stats.created.Add(1)
	return &pooledAgent{
		agent:      agent,
		createdAt:  time.Now(),
		lastUsedAt: time.Now(),
		inUse:      false,
	}, nil
}

// cleanupLoop 清理循环
func (p *AgentPool) cleanupLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.cleanup()
		case <-p.stopCleanup:
			return
		}
	}
}

// cleanup 清理过期的 Agent
func (p *AgentPool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	now := time.Now()
	newAgents := make([]*pooledAgent, 0, len(p.agents))

	for _, agent := range p.agents {
		// 跳过正在使用的 Agent
		if agent.inUse {
			newAgents = append(newAgents, agent)
			continue
		}

		// 检查是否超过最大生命周期
		if now.Sub(agent.createdAt) > p.config.MaxLifetime {
			p.stats.recycled.Add(1)
			continue
		}

		// 检查是否超过空闲超时
		if now.Sub(agent.lastUsedAt) > p.config.IdleTimeout {
			// 如果池中 Agent 数量超过初始大小，回收空闲 Agent
			if len(newAgents) >= p.config.InitialSize {
				p.stats.recycled.Add(1)
				continue
			}
		}

		newAgents = append(newAgents, agent)
	}

	p.agents = newAgents
}
