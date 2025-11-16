package specialized

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentcore "github.com/kart-io/goagent/core"
	"github.com/kart-io/logger"
)

func setupRedisClient(t *testing.T) *redis.Client {
	// Use miniredis for testing (no external dependencies)
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	t.Cleanup(server.Close)

	client := redis.NewClient(&redis.Options{
		Addr: server.Addr(),
	})

	return client
}

func TestNewCacheAgent(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()

	agent := NewCacheAgent(client, l)

	assert.Equal(t, "cache-agent", agent.Name())
	assert.Contains(t, agent.Capabilities(), "cache_get")
	assert.Contains(t, agent.Capabilities(), "cache_set")
	assert.Contains(t, agent.Capabilities(), "cache_delete")
	assert.Contains(t, agent.Capabilities(), "cache_exists")
	assert.Contains(t, agent.Capabilities(), "cache_expire")
}

func TestCacheAgent_Execute_Get_Success(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	// Set a value first
	err := client.Set(ctx, "testkey", "testvalue", 0).Err()
	require.NoError(t, err)

	// Test get
	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "get",
			"key":       "testkey",
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	assert.Len(t, output.ToolCalls, 1)
	assert.True(t, output.ToolCalls[0].Success)
	assert.Equal(t, "cache", output.ToolCalls[0].ToolName)

	result := output.Result.(map[string]interface{})
	assert.True(t, result["found"].(bool))
	assert.Equal(t, "testvalue", result["value"])
}

func TestCacheAgent_Execute_Get_NotFound(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "get",
			"key":       "nonexistent",
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.False(t, result["found"].(bool))
	assert.Nil(t, result["value"])
}

func TestCacheAgent_Execute_Set_Success(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "set",
			"key":       "mykey",
			"value":     "myvalue",
			"ttl":       60,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.True(t, result["set"].(bool))
	assert.Equal(t, "mykey", result["key"])
	assert.Equal(t, 60.0, result["ttl"])

	// Verify the value was set in Redis
	val, err := client.Get(ctx, "mykey").Result()
	require.NoError(t, err)
	assert.Equal(t, "myvalue", val)
}

func TestCacheAgent_Execute_Set_NoTTL(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "set",
			"key":       "persistent",
			"value":     "data",
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.True(t, result["set"].(bool))
	assert.Equal(t, 0.0, result["ttl"])
}

func TestCacheAgent_Execute_Delete_Success(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	// Set a value first
	err := client.Set(ctx, "todelete", "value", 0).Err()
	require.NoError(t, err)

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "delete",
			"key":       "todelete",
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.True(t, result["deleted"].(bool))
	assert.Equal(t, int64(1), result["count"])

	// Verify key is deleted
	exists, err := client.Exists(ctx, "todelete").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists)
}

func TestCacheAgent_Execute_Delete_NotFound(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "delete",
			"key":       "nonexistent",
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.False(t, result["deleted"].(bool))
	assert.Equal(t, int64(0), result["count"])
}

func TestCacheAgent_Execute_Exists_True(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	// Set a value first
	err := client.Set(ctx, "existing", "value", 0).Err()
	require.NoError(t, err)

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "exists",
			"key":       "existing",
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.True(t, result["exists"].(bool))
}

func TestCacheAgent_Execute_Exists_False(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "exists",
			"key":       "nonexistent",
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.False(t, result["exists"].(bool))
}

func TestCacheAgent_Execute_Expire_Success(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	// Set a value first
	err := client.Set(ctx, "toexpire", "value", 0).Err()
	require.NoError(t, err)

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "expire",
			"key":       "toexpire",
			"ttl":       120,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.True(t, result["set"].(bool))
	assert.Equal(t, "toexpire", result["key"])
	assert.Equal(t, 120.0, result["ttl"])
}

func TestCacheAgent_Execute_Expire_NonExistent(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "expire",
			"key":       "nonexistent",
			"ttl":       120,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	assert.False(t, result["set"].(bool))
}

func TestCacheAgent_Execute_Keys_Pattern(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	// Set multiple values
	err := client.Set(ctx, "user:1", "data1", 0).Err()
	require.NoError(t, err)
	err = client.Set(ctx, "user:2", "data2", 0).Err()
	require.NoError(t, err)
	err = client.Set(ctx, "post:1", "data3", 0).Err()
	require.NoError(t, err)

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "keys",
			"pattern":   "user:*",
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)

	result := output.Result.(map[string]interface{})
	keys := result["keys"].([]string)
	count := result["count"].(int)
	assert.Equal(t, 2, count)
	assert.Len(t, keys, 2)
}

func TestCacheAgent_Execute_InvalidOperation(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "invalid",
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown operation")
}

func TestCacheAgent_Execute_MissingOperation(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "operation is required")
}

func TestCacheAgent_Execute_MissingKey(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	tests := []struct {
		name      string
		operation string
		context   map[string]interface{}
	}{
		{
			name:      "get without key",
			operation: "get",
			context: map[string]interface{}{
				"operation": "get",
			},
		},
		{
			name:      "set without key",
			operation: "set",
			context: map[string]interface{}{
				"operation": "set",
				"value":     "test",
			},
		},
		{
			name:      "delete without key",
			operation: "delete",
			context: map[string]interface{}{
				"operation": "delete",
			},
		},
		{
			name:      "exists without key",
			operation: "exists",
			context: map[string]interface{}{
				"operation": "exists",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &agentcore.AgentInput{
				Context: tt.context,
			}

			_, err := agent.Execute(ctx, input)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), "key is required")
		})
	}
}

func TestCacheAgent_Execute_MissingValue(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "set",
			"key":       "testkey",
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value is required")
}

func TestCacheAgent_Get(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	// Set a value
	err := client.Set(ctx, "testkey", "testvalue", 0).Err()
	require.NoError(t, err)

	output, err := agent.Get(ctx, "testkey")

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	result := output.Result.(map[string]interface{})
	assert.True(t, result["found"].(bool))
	assert.Equal(t, "testvalue", result["value"])
}

func TestCacheAgent_Set(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	output, err := agent.Set(ctx, "newkey", "newvalue", 100)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	result := output.Result.(map[string]interface{})
	assert.True(t, result["set"].(bool))
}

func TestCacheAgent_Delete(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	// Set a value first
	err := client.Set(ctx, "todelete", "value", 0).Err()
	require.NoError(t, err)

	output, err := agent.Delete(ctx, "todelete")

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	result := output.Result.(map[string]interface{})
	assert.True(t, result["deleted"].(bool))
}

func TestCacheAgent_Exists(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	// Set a value
	err := client.Set(ctx, "existing", "value", 0).Err()
	require.NoError(t, err)

	output, err := agent.Exists(ctx, "existing")

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
	result := output.Result.(map[string]interface{})
	assert.True(t, result["exists"].(bool))
}

func TestCacheAgent_Execute_WithTimeout(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "set",
			"key":       "test",
			"value":     "value",
		},
		Options: agentcore.AgentOptions{
			Timeout: 5 * time.Second,
		},
	}

	output, err := agent.Execute(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "success", output.Status)
}

func TestCacheAgent_Execute_ComplexValues(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	tests := []struct {
		name  string
		value interface{}
	}{
		{"string value", "hello"},
		{"integer value", 123},
		{"float value", 45.67},
		{"boolean value", true},
		{"JSON string value", `{"key":"value"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &agentcore.AgentInput{
				Context: map[string]interface{}{
					"operation": "set",
					"key":       "complex",
					"value":     tt.value,
				},
			}

			output, err := agent.Execute(ctx, input)

			assert.NoError(t, err)
			assert.Equal(t, "success", output.Status)
		})
	}
}

func TestCacheAgent_ToolCallStructure(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "set",
			"key":       "test",
			"value":     "value",
		},
	}

	output, err := agent.Execute(ctx, input)

	require.NoError(t, err)
	assert.Len(t, output.ToolCalls, 1)

	toolCall := output.ToolCalls[0]
	assert.Equal(t, "cache", toolCall.ToolName)
	assert.NotZero(t, toolCall.Duration)
	assert.True(t, toolCall.Success)
	assert.NotEmpty(t, toolCall.Input)
	assert.NotEmpty(t, toolCall.Output)
}

func TestCacheAgent_Execute_ExpireMissingTTL(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	// Set a value first
	err := client.Set(ctx, "test", "value", 0).Err()
	require.NoError(t, err)

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "expire",
			"key":       "test",
		},
	}

	_, err2 := agent.Execute(ctx, input)
	err = err2

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ttl is required")
}

func TestCacheAgent_Execute_KeysMissingPattern(t *testing.T) {
	client := setupRedisClient(t)
	l, _ := logger.NewWithDefaults()
	agent := NewCacheAgent(client, l)
	ctx := context.Background()

	input := &agentcore.AgentInput{
		Context: map[string]interface{}{
			"operation": "keys",
		},
	}

	_, err := agent.Execute(ctx, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pattern is required")
}
