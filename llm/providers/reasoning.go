package providers

import (
	"strings"

	"google.golang.org/genai"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

func (p *OpenAIProvider) reasoningEffort(req *llm.CompletionRequest) (string, error) {
	if !p.reasoning {
		return "", p.RejectReasoning(req)
	}
	c := p.ResolveReasoning(req)
	if c == nil {
		return "", nil
	}
	if err := c.Validate(); err != nil {
		return "", err
	}
	if c.BudgetSet {
		return "", agentErrors.Newf(agentErrors.CodeInvalidInput, "OpenAI-compatible requests do not support a thinking budget; use effort").
			WithComponent("openai").
			WithOperation("validate_reasoning")
	}
	return c.Effort, nil
}
func (p *GeminiProvider) thinkingConfig(req *llm.CompletionRequest) (*genai.ThinkingConfig, error) {
	c := p.ResolveReasoning(req)
	if c == nil {
		return nil, nil
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	cfg := &genai.ThinkingConfig{}
	if c.BudgetSet {
		n := int32(c.BudgetTokens)
		cfg.ThinkingBudget = &n
	}
	if c.Effort != "" {
		switch c.Effort {
		case "minimal", "low", "medium", "high":
			cfg.ThinkingLevel = genai.ThinkingLevel(strings.ToUpper(c.Effort))
		default:
			return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "gemini does not support reasoning effort %q; use a supported level or thinking budget", c.Effort).
				WithComponent("gemini").
				WithOperation("validate_reasoning").
				WithContext("effort", c.Effort)
		}
	}
	return cfg, nil
}

func (p *OpenAIProvider) ValidateReasoning(req *llm.CompletionRequest) error {
	_, err := p.reasoningEffort(req)
	return err
}
func (p *DeepSeekProvider) ValidateReasoning(req *llm.CompletionRequest) error {
	return p.nativeStreamProvider().ValidateReasoning(req)
}
func (p *GeminiProvider) ValidateReasoning(req *llm.CompletionRequest) error {
	_, err := p.thinkingConfig(req)
	return err
}
