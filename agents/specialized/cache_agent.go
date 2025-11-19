package specialized

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/logger/core"
)

// CacheAgent 缓存操作 Agent
// 提供 Redis 缓存的读写能力
type CacheAgent struct {
	*agentcore.BaseAgent
	client *redis.Client
	logger core.Logger
}

// NewCacheAgent 创建缓存 Agent
func NewCacheAgent(client *redis.Client, logger core.Logger) *CacheAgent {
	return &CacheAgent{
		BaseAgent: agentcore.NewBaseAgent(
			"cache-agent",
			"Manages Redis cache operations with TTL and pattern matching",
			[]string{
				"cache_get",
				"cache_set",
				"cache_delete",
				"cache_exists",
				"cache_expire",
			},
		),
		client: client,
		logger: logger.With("agent", "cache"),
	}
}

// Execute 执行缓存操作
func (a *CacheAgent) Execute(ctx context.Context, input *agentcore.AgentInput) (*agentcore.AgentOutput, error) {
	start := time.Now()

	// 解析参数
	operation, ok := input.Context["operation"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "operation is required").
			WithComponent("cache_agent").
			WithOperation("Execute")
	}

	a.logger.Info("Executing cache operation",
		"operation", operation)

	var result interface{}
	var err error

	// 应用超时
	if input.Options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, input.Options.Timeout)
		defer cancel()
	}

	// 根据操作类型执行
	switch operation {
	case "get":
		result, err = a.executeGet(ctx, input)
	case "set":
		result, err = a.executeSet(ctx, input)
	case "delete":
		result, err = a.executeDelete(ctx, input)
	case "exists":
		result, err = a.executeExists(ctx, input)
	case "expire":
		result, err = a.executeExpire(ctx, input)
	case "keys":
		result, err = a.executeKeys(ctx, input)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unknown operation").
			WithComponent("cache_agent").
			WithOperation("Execute").
			WithContext("operation", operation)
	}

	// 构建输出
	output := &agentcore.AgentOutput{
		Status: interfaces.StatusSuccess,
		Result: result,
		ToolCalls: []agentcore.ToolCall{
			{
				ToolName: "cache",
				Input: map[string]interface{}{
					"operation": operation,
				},
				Output:   result,
				Duration: time.Since(start),
				Success:  err == nil,
			},
		},
		Latency:   time.Since(start),
		Timestamp: start,
	}

	if err != nil {
		output.Status = interfaces.StatusFailed
		output.Message = "Cache operation failed"
		output.ToolCalls[0].Error = err.Error()
		return output, agentErrors.Wrap(err, agentErrors.CodeAgentExecution, "cache operation failed").
			WithComponent("cache_agent").
			WithOperation("Execute").
			WithContext("operation", operation)
	}

	output.Message = "Cache operation completed successfully"

	return output, nil
}

// executeGet 获取缓存值
func (a *CacheAgent) executeGet(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	key, ok := input.Context["key"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "key is required").
			WithComponent("cache_agent").
			WithOperation("executeGet")
	}

	val, err := a.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return map[string]interface{}{
				"found": false,
				"value": nil,
			}, nil
		}
		return nil, err
	}

	return map[string]interface{}{
		"found": true,
		"value": val,
	}, nil
}

// executeSet 设置缓存值
func (a *CacheAgent) executeSet(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	key, ok := input.Context["key"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "key is required").
			WithComponent("cache_agent").
			WithOperation("executeSet")
	}

	value, ok := input.Context["value"]
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "value is required").
			WithComponent("cache_agent").
			WithOperation("executeSet")
	}

	var ttl time.Duration
	if ttlSeconds, ok := input.Context["ttl"].(int); ok && ttlSeconds > 0 {
		ttl = time.Duration(ttlSeconds) * time.Second
	}

	if err := a.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"set": true,
		"key": key,
		"ttl": ttl.Seconds(),
	}, nil
}

// executeDelete 删除缓存
func (a *CacheAgent) executeDelete(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	key, ok := input.Context["key"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "key is required").
			WithComponent("cache_agent").
			WithOperation("executeDelete")
	}

	deleted, err := a.client.Del(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"deleted": deleted > 0,
		"count":   deleted,
	}, nil
}

// executeExists 检查键是否存在
func (a *CacheAgent) executeExists(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	key, ok := input.Context["key"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "key is required").
			WithComponent("cache_agent").
			WithOperation("executeExists")
	}

	exists, err := a.client.Exists(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"exists": exists > 0,
	}, nil
}

// executeExpire 设置过期时间
func (a *CacheAgent) executeExpire(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	key, ok := input.Context["key"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "key is required").
			WithComponent("cache_agent").
			WithOperation("executeExpire")
	}

	ttlSeconds, ok := input.Context["ttl"].(int)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "ttl is required").
			WithComponent("cache_agent").
			WithOperation("executeExpire")
	}

	ttl := time.Duration(ttlSeconds) * time.Second
	set, err := a.client.Expire(ctx, key, ttl).Result()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"set": set,
		"key": key,
		"ttl": ttl.Seconds(),
	}, nil
}

// executeKeys 查询键列表
func (a *CacheAgent) executeKeys(ctx context.Context, input *agentcore.AgentInput) (interface{}, error) {
	pattern, ok := input.Context["pattern"].(string)
	if !ok {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "pattern is required").
			WithComponent("cache_agent").
			WithOperation("executeKeys")
	}

	keys, err := a.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"keys":  keys,
		"count": len(keys),
	}, nil
}

// Get 获取缓存值
func (a *CacheAgent) Get(ctx context.Context, key string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "get",
			"key":       key,
		},
	})
}

// Set 设置缓存值
func (a *CacheAgent) Set(ctx context.Context, key string, value interface{}, ttlSeconds int) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "set",
			"key":       key,
			"value":     value,
			"ttl":       ttlSeconds,
		},
	})
}

// Delete 删除缓存
func (a *CacheAgent) Delete(ctx context.Context, key string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "delete",
			"key":       key,
		},
	})
}

// Exists 检查键是否存在
func (a *CacheAgent) Exists(ctx context.Context, key string) (*agentcore.AgentOutput, error) {
	return a.Execute(ctx, &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "exists",
			"key":       key,
		},
	})
}
