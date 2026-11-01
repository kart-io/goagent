package common

import (
	"fmt"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/llm"
)

// ResolveReasoning returns a value copy. A nonnil empty request explicitly
// clears client defaults; nil preserves them.
func (b *BaseProvider) ResolveReasoning(req *llm.CompletionRequest) *usage.ReasoningConfig {
	c := b.Config.Reasoning
	if req != nil && req.Reasoning != nil {
		c = req.Reasoning
	}
	if c == nil {
		return nil
	}
	return c.Clone()
}

// RejectReasoning is used by providers without an implementation for controls.
func (b *BaseProvider) RejectReasoning(req *llm.CompletionRequest) error {
	if b.ResolveReasoning(req) != nil {
		return fmt.Errorf("provider %s does not support explicit reasoning configuration", b.Config.Provider)
	}
	return nil
}
