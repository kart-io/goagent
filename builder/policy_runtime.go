package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
)

// WithContextPolicies installs ordered, per-execution context transforms.
func (b *AgentBuilder[C, S]) WithContextPolicies(factories ...contextpolicy.Factory) *AgentBuilder[C, S] {
	b.config.ContextPolicies = slices.Clone(factories)
	return b
}

// WithPolicyToolAdapter lets the host journal policy tools through its Dispatcher.
func (b *AgentBuilder[C, S]) WithPolicyToolAdapter(adapter func([]interfaces.Tool) ([]interfaces.Tool, error)) *AgentBuilder[C, S] {
	b.config.PolicyToolAdapter = adapter
	return b
}

type policyRuntime struct {
	policies    []contextpolicy.Policy
	descriptors []contextpolicy.Descriptor
	tools       []interfaces.Tool
	byName      map[string]interfaces.Tool
}
type policyDescriptorsKey struct{}

func (a *ConfigurableAgent[C, S]) newPolicyRuntime(ctx context.Context) (*policyRuntime, error) {
	p := &policyRuntime{byName: map[string]interfaces.Tool{}}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, t := range a.tools {
		names[t.Name()] = true
	}
	for _, factory := range a.config.ContextPolicies {
		if factory == nil {
			return nil, fmt.Errorf("nil context policy factory")
		}
		policy := factory()
		if policy == nil || (reflect.ValueOf(policy).Kind() == reflect.Pointer && reflect.ValueOf(policy).IsNil()) {
			return nil, fmt.Errorf("nil context policy")
		}
		d := policy.Descriptor()
		if d.ID == "" || d.Version == "" || d.ConfigHash == "" || ids[d.ID] {
			return nil, fmt.Errorf("invalid or duplicate policy descriptor")
		}
		ids[d.ID] = true
		if durable.HasJournal(ctx) && !d.Durable {
			return nil, fmt.Errorf("policy %q does not support durable execution", d.ID)
		}
		p.policies = append(p.policies, policy)
		p.descriptors = append(p.descriptors, d)
		if provider, ok := policy.(contextpolicy.ToolProvider); ok {
			for _, tool := range provider.Tools() {
				if tool == nil || tool.Name() == "" || names[tool.Name()] {
					return nil, fmt.Errorf("invalid or conflicting policy tool")
				}
				names[tool.Name()] = true
				var schema jsonschema.Schema
				if err := json.Unmarshal([]byte(tool.ArgsSchema()), &schema); err != nil {
					return nil, err
				}
				if _, err := schema.Resolve(nil); err != nil {
					return nil, err
				}
				p.tools = append(p.tools, tool)
			}
		}
	}
	if len(p.tools) > 0 {
		if durable.HasJournal(ctx) && a.config.PolicyToolAdapter == nil {
			return nil, fmt.Errorf("durable policy tools require a host adapter")
		}
		if a.config.PolicyToolAdapter != nil {
			adapted, err := a.config.PolicyToolAdapter(slices.Clone(p.tools))
			if err != nil {
				return nil, err
			}
			if len(adapted) != len(p.tools) {
				return nil, fmt.Errorf("policy adapter changed tool count")
			}
			for i, t := range adapted {
				original := p.tools[i]
				if t == nil || t.Name() != original.Name() || t.ArgsSchema() != original.ArgsSchema() || t.Description() != original.Description() {
					return nil, fmt.Errorf("policy adapter changed tool identity")
				}
			}
			p.tools = adapted
		}
		for _, t := range p.tools {
			if durable.HasJournal(ctx) {
				d, ok := t.(interfaces.DurableCallTool)
				if !ok || !d.SupportsDurableCalls() {
					return nil, fmt.Errorf("policy adapter returned an unjournaled tool")
				}
			}
			p.byName[t.Name()] = t
		}
	}
	return p, nil
}
func policyContext(ctx context.Context, p *policyRuntime) context.Context {
	if len(p.policies) == 0 {
		return ctx
	}
	if contextpolicy.ResultScope(ctx) == "" {
		scope := durable.ExecutionKey(ctx)
		if scope == "" {
			scope = uuid.NewString()
		}
		ctx = contextpolicy.WithResultScope(ctx, scope)
	}
	return context.WithValue(ctx, policyDescriptorsKey{}, p.descriptors)
}
func (p *policyRuntime) apply(ctx context.Context, turn contextpolicy.Turn, final bool) (contextpolicy.Turn, error) {
	for i, policy := range p.policies {
		data, err := json.Marshal(turn)
		if err != nil {
			return turn, err
		}
		var copy contextpolicy.Turn
		if err = json.Unmarshal(data, &copy); err != nil {
			return turn, err
		}
		var next contextpolicy.Turn
		if final {
			if restore, ok := policy.(contextpolicy.FinalTurnRestorer); ok {
				next, err = restore.BeforeFinalTurn(ctx, copy)
			} else {
				next = copy
			}
		} else {
			next, err = policy.BeforeTurn(ctx, copy)
		}
		if err != nil {
			return turn, err
		}
		if policy.Descriptor() != p.descriptors[i] {
			return turn, fmt.Errorf("policy descriptor mutated")
		}
		if next.Round != turn.Round || next.RemainingCalls != turn.RemainingCalls || len(next.Messages) != len(turn.Messages) {
			return turn, fmt.Errorf("policy changed turn protocol")
		}
		for j, m := range turn.Messages {
			n := next.Messages[j]
			if m.Role != "system" {
				n.Content = m.Content
			}
			if !reflect.DeepEqual(m, n) {
				return turn, fmt.Errorf("policy changed protected message protocol")
			}
		}
		turn = next
	}
	return turn, nil
}
func (p *policyRuntime) stats(metadata map[string]interface{}) {
	for _, policy := range p.policies {
		if stats, ok := policy.(contextpolicy.StatsProvider); ok {
			for k, v := range stats.Stats() {
				metadata["policy."+policy.Descriptor().ID+"."+k] = v
			}
		}
	}
}
