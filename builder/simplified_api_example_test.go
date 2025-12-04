package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 测试简化 API 的基本功能
func TestSimplifiedAPI(t *testing.T) {
	t.Run("NewSimpleBuilder 创建构建器", func(t *testing.T) {
		client := NewMockLLMClient("test response")

		// 使用简化 API，无需泛型参数
		simpleBuilder := NewSimpleBuilder(client)

		require.NotNil(t, simpleBuilder)

		// 验证可以链式调用
		agent, err := simpleBuilder.
			WithSystemPrompt("你是一个助手").
			Build()

		require.NoError(t, err)
		require.NotNil(t, agent)
	})

	t.Run("类型别名正常工作", func(t *testing.T) {
		client := NewMockLLMClient("test response")

		// SimpleAgentBuilder 应该等价于 AgentBuilder[any, *core.AgentState]
		var simpleBuilder *SimpleAgentBuilder
		simpleBuilder = NewSimpleBuilder(client)

		require.NotNil(t, simpleBuilder)

		// 返回的 agent 应该是 SimpleAgent 类型
		agent, err := simpleBuilder.
			WithSystemPrompt("测试").
			Build()

		require.NoError(t, err)

		// 类型断言应该成功
		var _ *SimpleAgent = agent
	})

	t.Run("Quick builders 使用简化类型", func(t *testing.T) {
		client := NewMockLLMClient("test response")

		// 所有 quick builders 现在应该返回 SimpleAgent
		agents := make([]*SimpleAgent, 0)

		agent1, err := QuickAgent(client, "quick prompt")
		require.NoError(t, err)
		agents = append(agents, agent1)

		agent2, err := RAGAgent(client, nil)
		require.NoError(t, err)
		agents = append(agents, agent2)

		agent3, err := ChatAgent(client, "Alice")
		require.NoError(t, err)
		agents = append(agents, agent3)

		agent4, err := AnalysisAgent(client, nil)
		require.NoError(t, err)
		agents = append(agents, agent4)

		// 验证所有 agents 都创建成功
		assert.Equal(t, 4, len(agents))
		for i, agent := range agents {
			assert.NotNil(t, agent, "Agent %d should not be nil", i)
		}
	})
}
