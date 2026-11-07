package llm

import (
	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
)

// ReasoningRequestValidator opts a client into explicit reasoning controls.
// A client must validate the exact settings it will serialize.
type ReasoningRequestValidator interface {
	ValidateReasoning(*CompletionRequest) error
}

// ValidateReasoningRequest prevents unsupported clients from silently ignoring
// settings. Client defaults are resolved before the capability check.
func ValidateReasoningRequest(client Client, req *CompletionRequest) error {
	var config *usage.ReasoningConfig
	if req != nil {
		config = req.Reasoning
	}
	if resolver, ok := client.(interface {
		ResolveReasoning(*CompletionRequest) *usage.ReasoningConfig
	}); ok {
		config = resolver.ResolveReasoning(req)
	}
	if config == nil {
		return nil
	}
	if err := config.Validate(); err != nil {
		return err
	}
	if validator, ok := client.(ReasoningRequestValidator); ok {
		return validator.ValidateReasoning(req)
	}
	return agentErrors.Newf(agentErrors.CodeInvalidConfig, "client %T does not support explicit reasoning configuration", client).
		WithComponent("llm").
		WithOperation("validate_reasoning")
}
