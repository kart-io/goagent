package providers

import (
	"github.com/kart-io/goagent/llm/common"
	"testing"

	agentllm "github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/assert"
)

func TestNewProviderCapabilities(t *testing.T) {
	caps := common.NewProviderCapabilities(
		agentllm.CapabilityChat,
		agentllm.CapabilityStreaming,
		agentllm.CapabilityToolCalling,
	)

	assert.NotNil(t, caps)
	assert.True(t, caps.HasCapability(agentllm.CapabilityChat))
	assert.True(t, caps.HasCapability(agentllm.CapabilityStreaming))
	assert.True(t, caps.HasCapability(agentllm.CapabilityToolCalling))
}

func TestNewProviderCapabilities_Empty(t *testing.T) {
	caps := common.NewProviderCapabilities()

	assert.NotNil(t, caps)
	assert.False(t, caps.HasCapability(agentllm.CapabilityChat))
}

func TestProviderCapabilities_HasCapability(t *testing.T) {
	caps := common.NewProviderCapabilities(agentllm.CapabilityChat)

	assert.True(t, caps.HasCapability(agentllm.CapabilityChat))
	assert.False(t, caps.HasCapability(agentllm.CapabilityEmbedding))
}

func TestProviderCapabilities_HasCapability_NilCaps(t *testing.T) {
	caps := &common.ProviderCapabilities{}

	assert.False(t, caps.HasCapability(agentllm.CapabilityChat))
}

func TestProviderCapabilities_Capabilities(t *testing.T) {
	caps := common.NewProviderCapabilities(
		agentllm.CapabilityChat,
		agentllm.CapabilityStreaming,
	)

	result := caps.Capabilities()

	assert.Len(t, result, 2)
	assert.Contains(t, result, agentllm.CapabilityChat)
	assert.Contains(t, result, agentllm.CapabilityStreaming)
}

func TestProviderCapabilities_Capabilities_Empty(t *testing.T) {
	caps := &common.ProviderCapabilities{}

	result := caps.Capabilities()

	assert.Nil(t, result)
}

func TestProviderCapabilities_AddCapability(t *testing.T) {
	caps := common.NewProviderCapabilities()

	assert.False(t, caps.HasCapability(agentllm.CapabilityChat))

	caps.AddCapability(agentllm.CapabilityChat)

	assert.True(t, caps.HasCapability(agentllm.CapabilityChat))
}

func TestProviderCapabilities_AddCapability_NilCaps(t *testing.T) {
	caps := &common.ProviderCapabilities{}

	caps.AddCapability(agentllm.CapabilityChat)

	assert.True(t, caps.HasCapability(agentllm.CapabilityChat))
}

func TestProviderCapabilities_AddCapability_Duplicate(t *testing.T) {
	caps := common.NewProviderCapabilities(agentllm.CapabilityChat)

	// Adding same capability should be idempotent
	caps.AddCapability(agentllm.CapabilityChat)

	assert.True(t, caps.HasCapability(agentllm.CapabilityChat))
	assert.Len(t, caps.Capabilities(), 1)
}
