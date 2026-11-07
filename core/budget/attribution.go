package budget

import (
	"context"
	"math"
	"sort"

	"github.com/kart-io/goagent/core/usage"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

type UsageSummary struct {
	UnknownPromptEntries, UnknownCompletionEntries, UnknownTotalEntries, UnknownCachedEntries int
	UnknownReasoningEntries, UnknownToolUsePromptEntries                                      int
	KnownTokens                                                                               interfaces.TokenUsage
	ReservedRequests, KnownUsageEntries, UnknownUsageEntries, PartialUsageEntries             int
}
type CallAttribution struct {
	usage.Owner
	Self, Inclusive UsageSummary
}
type AgentAttribution struct {
	AgentID string
	Self    UsageSummary
}
type AttributionReport struct {
	Root                                     Snapshot
	Calls                                    []CallAttribution
	Agents                                   []AgentAttribution
	Unattributed                             UsageSummary
	UnknownUsageEntries, PartialUsageEntries int
	// Complete means attributed evidence has all BasicFields; cache is optional.
	Complete bool
}

func validOwner(o usage.Owner) bool {
	return o.RunID != "" && o.AgentID != "" && o.AgentCallKey != "" && len(o.RunID) <= 1024 && len(o.AgentID) <= 256 && len(o.AgentCallKey) <= 1024 && len(o.ParentAgentCallKey) <= 1024
}
func (c *Controller) register(l *ledger, chain []usage.Owner) error {
	if len(chain) == 0 || len(chain) > c.config.MaxAgentCalls {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "invalid attribution lineage").WithComponent("budget").WithOperation("register")
	}
	seen := map[string]bool{}
	added := 0
	for i, o := range chain {
		if !validOwner(o) || o.RunID != c.config.RootKey || seen[o.AgentCallKey] || (i == 0 && o.ParentAgentCallKey != "") || (i > 0 && o.ParentAgentCallKey != chain[i-1].AgentCallKey) {
			return agentErrors.Newf(agentErrors.CodeInvalidInput, "invalid attribution owner or lineage").WithComponent("budget").WithOperation("register")
		}
		seen[o.AgentCallKey] = true
		if old, ok := l.AgentCalls[o.AgentCallKey]; ok {
			if old != o {
				return agentErrors.Newf(agentErrors.CodeConflict, "agent owner changed").WithComponent("budget").WithOperation("register")
			}
		} else {
			added++
		}
	}
	if len(l.AgentCalls)+added > c.config.MaxAgentCalls {
		return agentErrors.Newf(agentErrors.CodeExecutionLimit, "agent call limit exceeded").WithComponent("budget").WithOperation("register")
	}
	if l.AgentCalls == nil {
		l.AgentCalls = map[string]usage.Owner{}
	}
	for _, o := range chain {
		l.AgentCalls[o.AgentCallKey] = o
	}
	return nil
}
func (c *Controller) RegisterAgent(ctx context.Context, lineage []usage.Owner) error {
	return c.transaction(ctx, true, func(l *ledger) error { return c.register(l, lineage) })
}
func (c *Controller) ReserveAttributed(ctx context.Context, key string, tokens int, lineage []usage.Owner) error {
	if key == "" || tokens < 0 {
		return agentErrors.Newf(agentErrors.CodeInvalidInput, "invalid reservation").WithComponent("budget").WithOperation("reserve_attributed")
	}
	if c.config.MaxReservedTokens > 0 && tokens == 0 {
		return agentErrors.ErrTokenEstimateRequired
	}
	return c.transaction(ctx, true, func(l *ledger) error {
		if err := c.checkDeadline(ctx); err != nil {
			return err
		}
		if len(lineage) > 0 {
			if err := c.register(l, lineage); err != nil {
				return err
			}
		}
		if _, ok := l.Entries[key]; ok {
			return agentErrors.ErrReservationExists
		}
		if len(l.Entries) >= c.config.MaxModelRequests {
			return agentErrors.ErrModelBudgetExceeded
		}
		n, e := reservedTokens(l)
		if e != nil {
			return e
		}
		if tokens > math.MaxInt-n || (c.config.MaxReservedTokens > 0 && tokens > c.config.MaxReservedTokens-n) {
			return agentErrors.ErrModelBudgetExceeded
		}
		item := entry{Estimate: tokens}
		if len(lineage) > 0 {
			item.AgentCallKey = lineage[len(lineage)-1].AgentCallKey
		}
		l.Entries[key] = item
		return nil
	})
}
func (c *Controller) RecordEvidence(ctx context.Context, key string, lineage []usage.Owner, e usage.Evidence) (out usage.Evidence, err error) {
	if err = e.Validate(); err != nil {
		return
	}
	err = c.transaction(ctx, true, func(l *ledger) error {
		if len(lineage) > 0 {
			if err := c.register(l, lineage); err != nil {
				return err
			}
		}
		item, ok := l.Entries[key]
		if !ok {
			return agentErrors.Newf(agentErrors.CodeInvalidInput, "usage has no reservation").WithComponent("budget").WithOperation("record_evidence")
		}
		owner := ""
		if len(lineage) > 0 {
			owner = lineage[len(lineage)-1].AgentCallKey
		}
		if owner != item.AgentCallKey {
			return agentErrors.Newf(agentErrors.CodeConflict, "attempt owner changed").WithComponent("budget").WithOperation("record_evidence")
		}
		var err error
		out, err = usage.Merge(item.Evidence, e)
		if err != nil {
			return err
		}
		item.Evidence = out
		l.Entries[key] = item
		return nil
	})
	return
}
func (c *Controller) validateAttribution(l *ledger) error {
	if len(l.AgentCalls) > c.config.MaxAgentCalls {
		return agentErrors.Newf(agentErrors.CodeStateValidation, "agent call limit exceeded").WithComponent("budget").WithOperation("validate_attribution")
	}
	for key, o := range l.AgentCalls {
		if key != o.AgentCallKey || !validOwner(o) || o.RunID != c.config.RootKey {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "invalid saved owner").WithComponent("budget").WithOperation("validate_attribution")
		}
		seen := map[string]bool{}
		for o.ParentAgentCallKey != "" {
			if seen[o.AgentCallKey] {
				return agentErrors.Newf(agentErrors.CodeStateValidation, "attribution cycle").WithComponent("budget").WithOperation("validate_attribution")
			}
			seen[o.AgentCallKey] = true
			var ok bool
			o, ok = l.AgentCalls[o.ParentAgentCallKey]
			if !ok {
				return agentErrors.Newf(agentErrors.CodeStateValidation, "missing ancestor").WithComponent("budget").WithOperation("validate_attribution")
			}
		}
	}
	for _, item := range l.Entries {
		if item.AgentCallKey != "" {
			if _, ok := l.AgentCalls[item.AgentCallKey]; !ok {
				return agentErrors.Newf(agentErrors.CodeStateValidation, "unknown attempt owner").WithComponent("budget").WithOperation("validate_attribution")
			}
		}
	}
	return nil
}

// rootSnapshot is the sole cumulative root accounting implementation.
func rootSnapshot(l *ledger) (out Snapshot, err error) {
	out.ReservedRequests = len(l.Entries)
	out.ReservedToolCalls = len(l.Tools)
	out.ReservedTokens, err = reservedTokens(l)
	if err != nil {
		return
	}
	for _, item := range l.Entries {
		e := item.Evidence
		if e.KnownFields == 0 {
			out.UnknownUsageEntries++
		} else {
			out.KnownUsageEntries++
			if err = addTokens(&out.KnownTokens, e.Tokens); err != nil {
				return
			}
		}
		if e.KnownFields&usage.Total != 0 {
			delta := e.Tokens.TotalTokens - item.Estimate
			if (delta > 0 && out.KnownTokenDelta > math.MaxInt-delta) || (delta < 0 && out.KnownTokenDelta < math.MinInt-delta) {
				err = agentErrors.Newf(agentErrors.CodeStateValidation, "token delta overflow").WithComponent("budget").WithOperation("root_snapshot")
				return
			}
			out.KnownTokenDelta += delta
		}
	}
	return
}
func addTokens(dst *interfaces.TokenUsage, src interfaces.TokenUsage) error {
	d := []*int{&dst.PromptTokens, &dst.CompletionTokens, &dst.TotalTokens, &dst.CachedTokens, &dst.ReasoningTokens, &dst.ToolUsePromptTokens}
	s := []int{src.PromptTokens, src.CompletionTokens, src.TotalTokens, src.CachedTokens, src.ReasoningTokens, src.ToolUsePromptTokens}
	for i, p := range d {
		if s[i] < 0 || *p > math.MaxInt-s[i] {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "token sum overflow").WithComponent("budget").WithOperation("add_tokens")
		}
	}
	for i, p := range d {
		*p += s[i]
	}
	return nil
}
func addSummary(dst *UsageSummary, src UsageSummary) error {
	if err := addTokens(&dst.KnownTokens, src.KnownTokens); err != nil {
		return err
	}
	dst.ReservedRequests += src.ReservedRequests
	dst.KnownUsageEntries += src.KnownUsageEntries
	dst.UnknownUsageEntries += src.UnknownUsageEntries
	dst.PartialUsageEntries += src.PartialUsageEntries
	dst.UnknownPromptEntries += src.UnknownPromptEntries
	dst.UnknownCompletionEntries += src.UnknownCompletionEntries
	dst.UnknownTotalEntries += src.UnknownTotalEntries
	dst.UnknownCachedEntries += src.UnknownCachedEntries
	dst.UnknownReasoningEntries += src.UnknownReasoningEntries
	dst.UnknownToolUsePromptEntries += src.UnknownToolUsePromptEntries
	return nil
}
func (c *Controller) Attribution(ctx context.Context) (out AttributionReport, err error) {
	err = c.transaction(ctx, false, func(l *ledger) error {
		calls := map[string]*CallAttribution{}
		roles := map[string]*AgentAttribution{}
		out.Complete = true
		for k, o := range l.AgentCalls {
			calls[k] = &CallAttribution{Owner: o}
			if roles[o.AgentID] == nil {
				roles[o.AgentID] = &AgentAttribution{AgentID: o.AgentID}
			}
		}
		var e error
		out.Root, e = rootSnapshot(l)
		if e != nil {
			return e
		}
		for _, item := range l.Entries {
			evidence := item.Evidence
			fields := evidence.KnownFields
			s := UsageSummary{ReservedRequests: 1, KnownTokens: evidence.Tokens}
			if fields == 0 {
				s.UnknownUsageEntries = 1
			} else {
				s.KnownUsageEntries = 1
				if fields&usage.BasicFields != usage.BasicFields {
					s.PartialUsageEntries = 1
				}
			}
			if fields&usage.Prompt == 0 {
				s.UnknownPromptEntries = 1
			}
			if fields&usage.Completion == 0 {
				s.UnknownCompletionEntries = 1
			}
			if fields&usage.Total == 0 {
				s.UnknownTotalEntries = 1
			}
			if fields&usage.Cached == 0 {
				s.UnknownCachedEntries = 1
			}

			if fields&usage.Reasoning == 0 {
				s.UnknownReasoningEntries = 1
			}
			if fields&usage.ToolUsePrompt == 0 {
				s.UnknownToolUsePromptEntries = 1
			}
			out.UnknownUsageEntries += s.UnknownUsageEntries
			out.PartialUsageEntries += s.PartialUsageEntries
			if s.UnknownUsageEntries+s.PartialUsageEntries > 0 {
				out.Complete = false
			}
			call := calls[item.AgentCallKey]
			if call == nil {
				out.Complete = false
				if err := addSummary(&out.Unattributed, s); err != nil {
					return err
				}
				continue
			}
			if err := addSummary(&call.Self, s); err != nil {
				return err
			}
			if err := addSummary(&roles[call.AgentID].Self, s); err != nil {
				return err
			}
			for node := call; node != nil; node = calls[node.ParentAgentCallKey] {
				if err := addSummary(&node.Inclusive, s); err != nil {
					return err
				}
			}
		}
		for _, v := range calls {
			out.Calls = append(out.Calls, *v)
		}
		for _, v := range roles {
			out.Agents = append(out.Agents, *v)
		}
		sort.Slice(out.Calls, func(i, j int) bool { return out.Calls[i].AgentCallKey < out.Calls[j].AgentCallKey })
		sort.Slice(out.Agents, func(i, j int) bool { return out.Agents[i].AgentID < out.Agents[j].AgentID })
		return nil
	})
	return
}
