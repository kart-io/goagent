package common_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kart-io/goagent/constants"
	agentllm "github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
)

func TestConvertMessages(t *testing.T) {
	messages := []agentllm.Message{
		{Role: constants.RoleUser, Content: "Hello", Name: "User1"},
		{Role: constants.RoleAssistant, Content: "Hi there", Name: ""},
		{Role: constants.RoleSystem, Content: "You are helpful", Name: ""},
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
		Role:    constants.RoleUser,
		Content: "Hello",
		Name:    "TestUser",
	}

	result := common.ToStandardMessage(msg)

	assert.Equal(t, constants.RoleUser, result.Role)
	assert.Equal(t, "Hello", result.Content)
	assert.Equal(t, "TestUser", result.Name)
}

func TestConvertToStandardMessages(t *testing.T) {
	messages := []agentllm.Message{
		{Role: constants.RoleUser, Content: "Hello"},
		{Role: constants.RoleAssistant, Content: "Hi"},
	}

	result := common.ConvertToStandardMessages(messages)

	assert.Len(t, result, 2)
	assert.Equal(t, constants.RoleUser, result[0].Role)
	assert.Equal(t, "Hello", result[0].Content)
	assert.Equal(t, constants.RoleAssistant, result[1].Role)
	assert.Equal(t, "Hi", result[1].Content)
}

func TestDefaultRoleMapper(t *testing.T) {
	testCases := []string{constants.RoleUser, constants.RoleAssistant, constants.RoleSystem, constants.RoleTool}

	for _, role := range testCases {
		result := common.DefaultRoleMapper(role)
		assert.Equal(t, role, result, "common.DefaultRoleMapper should return role unchanged")
	}
}

func TestConvertMessagesWithRoleMapping(t *testing.T) {
	messages := []agentllm.Message{
		{Role: constants.RoleUser, Content: "Hello"},
		{Role: constants.RoleAssistant, Content: "Hi"},
		{Role: constants.RoleSystem, Content: "Be helpful"},
	}

	// Custom role mapper (like Cohere)
	roleMapper := func(role string) string {
		switch role {
		case constants.RoleUser:
			return "USER"
		case constants.RoleAssistant:
			return "CHATBOT"
		case constants.RoleSystem:
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
