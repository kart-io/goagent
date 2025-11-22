package distributed

import (
	"context"
	"strings"
	"sync"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/logger/core"
)

// Coordinator 分布式 Agent 协调器
// 负责跨服务的 Agent 调用和协调
type Coordinator struct {
	registry *Registry
	client   *Client
	logger   core.Logger

	// 负载均衡
	mu              sync.RWMutex
	roundRobinIndex map[string]int

	// 并发控制
	maxConcurrency int
}

const (
	// DefaultMaxConcurrency 默认最大并发数
	DefaultMaxConcurrency = 100
)

// CoordinatorOption Coordinator 配置选项
type CoordinatorOption func(*Coordinator)

// WithMaxConcurrency 设置最大并发数
func WithMaxConcurrency(max int) CoordinatorOption {
	return func(c *Coordinator) {
		if max > 0 {
			c.maxConcurrency = max
		}
	}
}

// NewCoordinator 创建协调器
func NewCoordinator(registry *Registry, client *Client, logger core.Logger, opts ...CoordinatorOption) *Coordinator {
	c := &Coordinator{
		registry:        registry,
		client:          client,
		logger:          logger.With("component", "agent-coordinator"),
		roundRobinIndex: make(map[string]int),
		maxConcurrency:  DefaultMaxConcurrency,
	}

	// 应用配置选项
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// ExecuteAgent 执行远程 Agent
func (c *Coordinator) ExecuteAgent(ctx context.Context, serviceName, agentName string, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	// 获取服务实例
	instance, err := c.selectInstance(serviceName)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeDistributedCoordination, "failed to select instance").
			WithComponent("distributed_coordinator").
			WithOperation("execute_agent").
			WithContext("service_name", serviceName).
			WithContext("agent_name", agentName)
	}

	c.logger.Info("Executing remote agent",
		"service", serviceName,
		"agent", agentName,
		"instance", instance.ID)

	// 调用远程 Agent
	output, err := c.client.ExecuteAgent(ctx, instance.Endpoint, agentName, input)
	if err != nil {
		// 标记实例为不健康
		c.registry.MarkUnhealthy(instance.ID)

		// 尝试故障转移
		if c.shouldRetry(err) {
			c.logger.Warnw("Agent execution failed, trying failover",
				"error", err,
				"instance", instance.ID)

			return c.executeWithFailover(ctx, serviceName, agentName, input, instance.ID)
		}

		return nil, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "agent execution failed").
			WithComponent("distributed_coordinator").
			WithOperation("execute_agent").
			WithContext("service_name", serviceName).
			WithContext("agent_name", agentName).
			WithContext("instance_id", instance.ID)
	}

	// 标记实例为健康
	c.registry.MarkHealthy(instance.ID)

	return output, nil
}

// ExecuteAgentWithRetry 执行 Agent 并支持重试
func (c *Coordinator) ExecuteAgentWithRetry(ctx context.Context, serviceName, agentName string, input *agentcore.AgentInput, maxRetries int) (*agentcore.AgentOutput, error) {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			c.logger.Infow("Retrying agent execution",
				"attempt", i+1,
				"max_retries", maxRetries,
				"service", serviceName,
				"agent", agentName)

			// 退避等待
			backoff := time.Duration(i) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		output, err := c.ExecuteAgent(ctx, serviceName, agentName, input)
		if err == nil {
			return output, nil
		}

		lastErr = err

		// 如果是上下文取消，立即返回
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	return nil, agentErrors.Wrap(lastErr, agentErrors.CodeAgentExecution, "agent execution failed after retries").
		WithComponent("distributed_coordinator").
		WithOperation("execute_agent_with_retry").
		WithContext("service_name", serviceName).
		WithContext("agent_name", agentName).
		WithContext("max_retries", maxRetries)
}

// ExecuteParallel 并行执行多个 Agent
// 使用信号量模式限制并发 goroutine 数量，防止资源耗尽
func (c *Coordinator) ExecuteParallel(ctx context.Context, tasks []AgentTask) ([]AgentTaskResult, error) {
	results := make([]AgentTaskResult, len(tasks))
	var wg sync.WaitGroup
	errCh := make(chan error, len(tasks))

	// 使用 buffered channel 作为信号量来限制并发数
	semaphore := make(chan struct{}, c.maxConcurrency)

	for i, task := range tasks {
		wg.Add(1)

		// 获取信号量（阻塞直到有可用槽位）
		select {
		case <-ctx.Done():
			// 上下文取消，停止启动新任务
			wg.Done()
			break
		case semaphore <- struct{}{}:
			// 成功获取信号量，启动 goroutine
			go func(index int, t AgentTask) {
				defer wg.Done()
				defer func() { <-semaphore }() // 释放信号量

				output, err := c.ExecuteAgent(ctx, t.ServiceName, t.AgentName, t.Input)
				results[index] = AgentTaskResult{
					Task:   t,
					Output: output,
					Error:  err,
				}

				if err != nil {
					select {
					case errCh <- err:
					default:
						// errCh 已满，丢弃错误（已经记录在 results 中）
					}
				}
			}(i, task)
		}
	}

	wg.Wait()
	close(errCh)

	// 检查是否有错误
	errs := make([]error, 0, len(tasks))
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return results, agentErrors.New(agentErrors.CodeAgentExecution, "some tasks failed").
			WithComponent("distributed_coordinator").
			WithOperation("execute_parallel").
			WithContext("failed_count", len(errs)).
			WithContext("total_tasks", len(tasks))
	}

	return results, nil
}

// ExecuteSequential 顺序执行多个 Agent
func (c *Coordinator) ExecuteSequential(ctx context.Context, tasks []AgentTask) ([]AgentTaskResult, error) {
	results := make([]AgentTaskResult, len(tasks))

	for i, task := range tasks {
		output, err := c.ExecuteAgent(ctx, task.ServiceName, task.AgentName, task.Input)
		results[i] = AgentTaskResult{
			Task:   task,
			Output: output,
			Error:  err,
		}

		if err != nil {
			return results, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "task failed").
				WithComponent("distributed_coordinator").
				WithOperation("execute_sequential").
				WithContext("task_index", i).
				WithContext("service_name", task.ServiceName).
				WithContext("agent_name", task.AgentName)
		}

		// 将前一个任务的输出传递到下一个任务
		if i < len(tasks)-1 && output != nil {
			if tasks[i+1].Input.Context == nil {
				tasks[i+1].Input.Context = make(map[string]interface{})
			}
			tasks[i+1].Input.Context["previous_output"] = output.Result
		}
	}

	return results, nil
}

// selectInstance 选择服务实例（负载均衡）
func (c *Coordinator) selectInstance(serviceName string) (*ServiceInstance, error) {
	instances, err := c.registry.GetHealthyInstances(serviceName)
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		return nil, agentErrors.New(agentErrors.CodeAgentNotFound, "no healthy instances for service").
			WithComponent("distributed_coordinator").
			WithOperation("select_instance").
			WithContext("service_name", serviceName)
	}

	// Round-robin 负载均衡
	c.mu.Lock()
	defer c.mu.Unlock()

	index := c.roundRobinIndex[serviceName]
	instance := instances[index%len(instances)]
	c.roundRobinIndex[serviceName] = (index + 1) % len(instances)

	return instance, nil
}

// executeWithFailover 故障转移
func (c *Coordinator) executeWithFailover(ctx context.Context, serviceName, agentName string, input *agentcore.AgentInput, failedInstanceID string) (*agentcore.AgentOutput, error) {
	instances, err := c.registry.GetHealthyInstances(serviceName)
	if err != nil {
		return nil, err
	}

	// 过滤掉失败的实例
	var availableInstances []*ServiceInstance
	for _, inst := range instances {
		if inst.ID != failedInstanceID {
			availableInstances = append(availableInstances, inst)
		}
	}

	if len(availableInstances) == 0 {
		return nil, agentErrors.New(agentErrors.CodeDistributedCoordination, "no available instances for failover").
			WithComponent("distributed_coordinator").
			WithOperation("execute_with_failover").
			WithContext("service_name", serviceName).
			WithContext("agent_name", agentName).
			WithContext("failed_instance_id", failedInstanceID)
	}

	// 尝试第一个可用实例
	instance := availableInstances[0]
	c.logger.Infow("Attempting failover",
		"service", serviceName,
		"agent", agentName,
		"failover_instance", instance.ID)

	return c.client.ExecuteAgent(ctx, instance.Endpoint, agentName, input)
}

// shouldRetry 判断是否应该重试
func (c *Coordinator) shouldRetry(err error) bool {
	// 可以根据错误类型判断是否重试
	// 例如：网络错误可以重试，业务逻辑错误不重试
	if err == nil {
		return false
	}

	errStr := err.Error()
	// 网络错误 - use standard library strings.Contains for O(n) performance
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection reset")
}

// AgentTask Agent 任务
type AgentTask struct {
	ServiceName string
	AgentName   string
	Input       *agentcore.AgentInput
}

// AgentTaskResult Agent 任务结果
type AgentTaskResult struct {
	Task   AgentTask
	Output *agentcore.AgentOutput
	Error  error
}
