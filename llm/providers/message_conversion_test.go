package providers

import (
	"testing"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/stretchr/testify/assert"
)

func TestConvertMessages(t *testing.T) {
	messages := []agentllm.Message{
		{Role: "user", Content: "Hello", Name: "User1"},
		{Role: "assistant", Content: "Hi there", Name: ""},
		{Role: "system", Content: "You are helpful", Name: ""},
	}

	type TestMessage struct {
		Role    string
		Content string
		Name    string
	}

	converter := func(msg agentllm.Message) TestMessage {
		return TestMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		}
	}

	result := common.ConvertMessages(messages, converter)

	assert.Len(t, result, 3)
	assert.Equal(t, "user", result[0].Role)
	assert.Equal(t, "Hello", result[0].Content)
	assert.Equal(t, "User1", result[0].Name)
	assert.Equal(t, "assistant", result[1].Role)
	assert.Equal(t, "Hi there", result[1].Content)
}

func TestConvertMessages_Empty(t *testing.T) {
	messages := []agentllm.Message{}

	converter := func(msg agentllm.Message) common.StandardMessage {
		return common.ToStandardMessage(msg)
	}

	result := common.ConvertMessages(messages, converter)

	assert.Empty(t, result)
}

func TestToStandardMessage(t *testing.T) {
	msg := agentllm.Message{
		Role:    "user",
		Content: "Hello",
		Name:    "TestUser",
	}

	result := common.ToStandardMessage(msg)

	assert.Equal(t, "user", result.Role)
	assert.Equal(t, "Hello", result.Content)
	assert.Equal(t, "TestUser", result.Name)
}

func TestConvertToStandardMessages(t *testing.T) {
	messages := []agentllm.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}

	result := common.ConvertToStandardMessages(messages)

	assert.Len(t, result, 2)
	assert.Equal(t, "user", result[0].Role)
	assert.Equal(t, "Hello", result[0].Content)
	assert.Equal(t, "assistant", result[1].Role)
	assert.Equal(t, "Hi", result[1].Content)
}

func TestDefaultRoleMapper(t *testing.T) {
	testCases := []string{"user", "assistant", "system", "function"}

	for _, role := range testCases {
		result := common.DefaultRoleMapper(role)
		assert.Equal(t, role, result, "common.DefaultRoleMapper should return role unchanged")
	}
}

func TestConvertMessagesWithRoleMapping(t *testing.T) {
	messages := []agentllm.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
		{Role: "system", Content: "Be helpful"},
	}

	// Custom role mapper (like Cohere)
	roleMapper := func(role string) string {
		switch role {
		case "user":
			return "USER"
		case "assistant":
			return "CHATBOT"
		case "system":
			return "SYSTEM"
		default:
			return "USER"
		}
	}

	type CustomMessage struct {
		Role    string
		Message string
	}

	converter := func(msg agentllm.Message, mappedRole string) CustomMessage {
		return CustomMessage{
			Role:    mappedRole,
			Message: msg.Content,
		}
	}

	result := common.ConvertMessagesWithRoleMapping(messages, roleMapper, converter)

	assert.Len(t, result, 3)
	assert.Equal(t, "USER", result[0].Role)
	assert.Equal(t, "Hello", result[0].Message)
	assert.Equal(t, "CHATBOT", result[1].Role)
	assert.Equal(t, "Hi", result[1].Message)
	assert.Equal(t, "SYSTEM", result[2].Role)
	assert.Equal(t, "Be helpful", result[2].Message)
}

func TestMessagesToPrompt(t *testing.T) {
	messages := []agentllm.Message{
		{Role: "user", Content: "What is AI?"},
		{Role: "assistant", Content: "AI is artificial intelligence."},
	}

	formatter := func(msg agentllm.Message) string {
		return msg.Role + ": " + msg.Content + "\n"
	}

	result := common.MessagesToPrompt(messages, formatter)

	expected := "user: What is AI?\nassistant: AI is artificial intelligence.\n"
	assert.Equal(t, expected, result)
}

func TestMessagesToPrompt_Empty(t *testing.T) {
	messages := []agentllm.Message{}

	formatter := func(msg agentllm.Message) string {
		return msg.Role + ": " + msg.Content + "\n"
	}

	result := common.MessagesToPrompt(messages, formatter)

	assert.Equal(t, "", result)
}

func TestDefaultPromptFormatter(t *testing.T) {
	testCases := []struct {
		name     string
		message  agentllm.Message
		expected string
	}{
		{
			name:     "system message",
			message:  agentllm.Message{Role: "system", Content: "Be helpful"},
			expected: "System: Be helpful\n",
		},
		{
			name:     "user message",
			message:  agentllm.Message{Role: "user", Content: "Hello"},
			expected: "User: Hello\n",
		},
		{
			name:     "assistant message",
			message:  agentllm.Message{Role: "assistant", Content: "Hi there"},
			expected: "Assistant: Hi there\n",
		},
		{
			name:     "unknown role",
			message:  agentllm.Message{Role: "custom", Content: "Test"},
			expected: "custom: Test\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := common.DefaultPromptFormatter(tc.message)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestMessagesToPrompt_WithDefaultFormatter(t *testing.T) {
	messages := []agentllm.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello"},
	}

	result := common.MessagesToPrompt(messages, common.DefaultPromptFormatter)

	expected := "System: You are helpful\nUser: Hi\nAssistant: Hello\n"
	assert.Equal(t, expected, result)
}
