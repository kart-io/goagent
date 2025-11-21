package performance

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolManager(t *testing.T) {
	t.Run("Create with default config", func(t *testing.T) {
		manager := NewPoolAgent(DefaultPoolManagerConfig())
		require.NotNil(t, manager)
		defer manager.Close()

		config := manager.GetConfig()
		assert.True(t, config.EnabledPools[PoolTypeByteBuffer])
		assert.True(t, config.EnabledPools[PoolTypeMessage])
	})

	t.Run("Create with custom config", func(t *testing.T) {
		config := &PoolManagerConfig{
			EnabledPools: map[PoolType]bool{
				PoolTypeByteBuffer: true,
				PoolTypeMessage:    false,
			},
			MaxBufferSize: 32 * 1024,
		}

		manager := NewPoolAgent(config)
		require.NotNil(t, manager)
		defer manager.Close()

		assert.True(t, manager.IsPoolEnabled(PoolTypeByteBuffer))
		assert.False(t, manager.IsPoolEnabled(PoolTypeMessage))
	})
}

func TestByteBufferPoolManager(t *testing.T) {
	manager := NewPoolAgent(DefaultPoolManagerConfig())
	defer manager.Close()

	t.Run("Get and Put", func(t *testing.T) {
		buf := manager.GetBuffer()
		require.NotNil(t, buf)
		assert.Equal(t, 0, buf.Len())

		buf.WriteString("test")
		assert.Equal(t, 4, buf.Len())

		manager.PutBuffer(buf)
	})

	t.Run("Large buffer not pooled", func(t *testing.T) {
		buf := manager.GetBuffer()
		// 写入大量数据
		largeData := make([]byte, 128*1024) // 128KB
		buf.Write(largeData)

		stats := manager.GetStats(PoolTypeByteBuffer)
		initialPuts := stats.Puts

		manager.PutBuffer(buf)

		// 大 buffer 不应该被池化
		stats = manager.GetStats(PoolTypeByteBuffer)
		assert.Equal(t, initialPuts, stats.Puts)
	})

	t.Run("Nil buffer", func(t *testing.T) {
		// 不应该 panic
		manager.PutBuffer(nil)
	})
}

func TestMessagePoolManager(t *testing.T) {
	manager := NewPoolAgent(DefaultPoolManagerConfig())
	defer manager.Close()

	t.Run("Get and Put", func(t *testing.T) {
		msg := manager.GetMessage()
		require.NotNil(t, msg)
		assert.Empty(t, msg.Content)

		msg.Role = "user"
		msg.Content = "test message"

		manager.PutMessage(msg)
	})

	t.Run("Message reset after put", func(t *testing.T) {
		msg := manager.GetMessage()
		msg.Role = "assistant"
		msg.Content = "response"
		msg.Name = "agent"

		manager.PutMessage(msg)

		// 再次获取应该是干净的
		msg2 := manager.GetMessage()
		assert.Empty(t, msg2.Role)
		assert.Empty(t, msg2.Content)
		assert.Empty(t, msg2.Name)

		manager.PutMessage(msg2)
	})
}

func TestToolPoolManager(t *testing.T) {
	manager := NewPoolAgent(DefaultPoolManagerConfig())
	defer manager.Close()

	t.Run("ToolInput Get and Put", func(t *testing.T) {
		input := manager.GetToolInput()
		require.NotNil(t, input)
		require.NotNil(t, input.Args)
		assert.Empty(t, input.Args)

		input.Args["key"] = "value"
		manager.PutToolInput(input)
	})

	t.Run("ToolOutput Get and Put", func(t *testing.T) {
		output := manager.GetToolOutput()
		require.NotNil(t, output)
		require.NotNil(t, output.Metadata)

		output.Success = true
		output.Result = "result"
		manager.PutToolOutput(output)
	})
}

func TestAgentPoolManager(t *testing.T) {
	manager := NewPoolAgent(DefaultPoolManagerConfig())
	defer manager.Close()

	t.Run("AgentInput Get and Put", func(t *testing.T) {
		input := manager.GetAgentInput()
		require.NotNil(t, input)
		require.NotNil(t, input.Context)

		input.Task = "test task"
		input.Context["key"] = "value"
		manager.PutAgentInput(input)
	})

	t.Run("AgentOutput Get and Put", func(t *testing.T) {
		output := manager.GetAgentOutput()
		require.NotNil(t, output)

		output.Status = "success"
		output.Result = "result"
		manager.PutAgentOutput(output)
	})
}

func TestPoolManagerConfiguration(t *testing.T) {
	t.Run("Enable and Disable pools", func(t *testing.T) {
		manager := NewPoolAgent(DefaultPoolManagerConfig())
		defer manager.Close()

		// 禁用 ByteBuffer 池
		manager.DisablePool(PoolTypeByteBuffer)
		assert.False(t, manager.IsPoolEnabled(PoolTypeByteBuffer))

		// 启用 ByteBuffer 池
		manager.EnablePool(PoolTypeByteBuffer)
		assert.True(t, manager.IsPoolEnabled(PoolTypeByteBuffer))
	})

	t.Run("Configure", func(t *testing.T) {
		manager := NewPoolAgent(DefaultPoolManagerConfig())
		defer manager.Close()

		newConfig := &PoolManagerConfig{
			EnabledPools: map[PoolType]bool{
				PoolTypeByteBuffer: false,
				PoolTypeMessage:    true,
			},
		}

		err := manager.Configure(newConfig)
		assert.NoError(t, err)

		assert.False(t, manager.IsPoolEnabled(PoolTypeByteBuffer))
		assert.True(t, manager.IsPoolEnabled(PoolTypeMessage))
	})
}

func TestPoolManagerStats(t *testing.T) {
	manager := NewPoolAgent(DefaultPoolManagerConfig())
	defer manager.Close()

	t.Run("Track Gets and Puts", func(t *testing.T) {
		manager.ResetStats()

		// 获取和归还 buffer
		buf := manager.GetBuffer()
		manager.PutBuffer(buf)

		stats := manager.GetStats(PoolTypeByteBuffer)
		assert.Equal(t, int64(1), stats.Gets)
		assert.Equal(t, int64(1), stats.Puts)
	})

	t.Run("Get all stats", func(t *testing.T) {
		manager.ResetStats()

		// 使用不同的池
		buf := manager.GetBuffer()
		msg := manager.GetMessage()
		input := manager.GetAgentInput()

		manager.PutBuffer(buf)
		manager.PutMessage(msg)
		manager.PutAgentInput(input)

		allStats := manager.GetAllStats()
		assert.Equal(t, int64(1), allStats[PoolTypeByteBuffer].Gets)
		assert.Equal(t, int64(1), allStats[PoolTypeMessage].Gets)
		assert.Equal(t, int64(1), allStats[PoolTypeAgentInput].Gets)
	})

	t.Run("Reset stats", func(t *testing.T) {
		// 使用池
		buf := manager.GetBuffer()
		manager.PutBuffer(buf)

		// 重置
		manager.ResetStats()

		stats := manager.GetStats(PoolTypeByteBuffer)
		assert.Equal(t, int64(0), stats.Gets)
		assert.Equal(t, int64(0), stats.Puts)
	})
}

func TestIsolatedPoolManager(t *testing.T) {
	t.Run("Create isolated managers", func(t *testing.T) {
		config1 := &PoolManagerConfig{
			EnabledPools: map[PoolType]bool{
				PoolTypeByteBuffer: true,
			},
		}

		config2 := &PoolManagerConfig{
			EnabledPools: map[PoolType]bool{
				PoolTypeMessage: true,
			},
		}

		manager1 := CreateIsolatedPoolManager(config1)
		manager2 := CreateIsolatedPoolManager(config2)

		defer manager1.Close()
		defer manager2.Close()

		// Manager1 只启用 ByteBuffer
		assert.True(t, manager1.IsPoolEnabled(PoolTypeByteBuffer))
		assert.False(t, manager1.IsPoolEnabled(PoolTypeMessage))

		// Manager2 只启用 Message
		assert.False(t, manager2.IsPoolEnabled(PoolTypeByteBuffer))
		assert.True(t, manager2.IsPoolEnabled(PoolTypeMessage))
	})
}

func TestPoolManagerAgent(t *testing.T) {
	t.Run("Create and execute", func(t *testing.T) {
		agent := NewPoolManagerAgent("test_pool", DefaultPoolManagerConfig())
		require.NotNil(t, agent)

		assert.Equal(t, "test_pool", agent.Name())
		assert.NotEmpty(t, agent.Description())
		assert.NotEmpty(t, agent.Capabilities())

		input := &core.AgentInput{
			Task: "configure_pools",
			Context: map[string]interface{}{
				"scenario": "llm_calls",
			},
		}

		output, err := agent.Execute(context.Background(), input)
		assert.NoError(t, err)
		require.NotNil(t, output)
		assert.Equal(t, "success", output.Status)
	})
}

func TestScenarioBasedStrategy(t *testing.T) {
	config := DefaultPoolManagerConfig()
	strategy := NewScenarioBasedStrategy(config)
	config.UseStrategy = strategy

	manager := NewPoolAgent(config)
	defer manager.Close()

	t.Run("LLM Calls scenario", func(t *testing.T) {
		strategy.SetScenario(ScenarioLLMCalls)

		// LLM 场景应该启用 Message 和 Agent 池
		assert.True(t, strategy.ShouldPool(PoolTypeMessage, 100))
		assert.True(t, strategy.ShouldPool(PoolTypeAgentInput, 100))
	})

	t.Run("JSON Processing scenario", func(t *testing.T) {
		strategy.SetScenario(ScenarioJSONProcess)

		// JSON 场景应该只启用 ByteBuffer 池
		assert.True(t, strategy.ShouldPool(PoolTypeByteBuffer, 100))
		assert.False(t, strategy.ShouldPool(PoolTypeMessage, 100))
	})
}

func TestAdaptivePoolStrategy(t *testing.T) {
	config := DefaultPoolManagerConfig()
	strategy := NewAdaptivePoolStrategy(config)
	config.UseStrategy = strategy

	manager := NewPoolAgent(config)
	defer manager.Close()

	t.Run("Should not pool with low usage", func(t *testing.T) {
		// 低频使用不应该池化
		for i := 0; i < 50; i++ {
			strategy.PreGet(PoolTypeMessage)
		}

		// 使用次数不够，不池化
		shouldPool := strategy.ShouldPool(PoolTypeMessage, 100)
		assert.False(t, shouldPool)
	})

	t.Run("Should pool with high usage", func(t *testing.T) {
		// 高频使用应该池化
		for i := 0; i < 150; i++ {
			strategy.PreGet(PoolTypeByteBuffer)
		}

		// 使用次数足够，池化
		shouldPool := strategy.ShouldPool(PoolTypeByteBuffer, 100)
		assert.True(t, shouldPool)
	})
}

func BenchmarkPoolManager(b *testing.B) {
	manager := NewPoolAgent(DefaultPoolManagerConfig())
	defer manager.Close()

	b.Run("ByteBuffer", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			buf := manager.GetBuffer()
			buf.WriteString("test")
			manager.PutBuffer(buf)
		}
	})

	b.Run("Message", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			msg := manager.GetMessage()
			msg.Content = "test"
			manager.PutMessage(msg)
		}
	})

	b.Run("AgentInput", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			input := manager.GetAgentInput()
			input.Task = "test"
			manager.PutAgentInput(input)
		}
	})
}
