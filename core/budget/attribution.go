package budget

import (
	"context"
	"fmt"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"math"
	"sort"
)

type UsageSummary struct {
	UnknownPromptEntries, UnknownCompletionEntries, UnknownTotalEntries, UnknownCachedEntries int
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

func (c *Controller) AttributionEnabled() bool { return c != nil && c.config.AttributionVersion != "" }
func (c *Controller) ledgerVersion() string {
	if c.AttributionEnabled() {
		return "goagent-model-budget-v2"
	}
	return "goagent-model-budget-v1"
}
func validOwner(o usage.Owner) bool {
	return o.RunID != "" && o.AgentID != "" && o.AgentCallKey != "" && len(o.RunID) <= 1024 && len(o.AgentID) <= 256 && len(o.AgentCallKey) <= 1024 && len(o.ParentAgentCallKey) <= 1024
}
func (c *Controller) register(l *ledger, chain []usage.Owner) error {
	if !c.AttributionEnabled() {
		return fmt.Errorf("attribution disabled")
	}
	if len(chain) == 0 || len(chain) > c.config.MaxAgentCalls {
		return fmt.Errorf("invalid attribution lineage")
	}
	seen := map[string]bool{}
	added := 0
	for i, o := range chain {
		if !validOwner(o) || o.RunID != c.config.RootKey || seen[o.AgentCallKey] || (i == 0 && o.ParentAgentCallKey != "") || (i > 0 && o.ParentAgentCallKey != chain[i-1].AgentCallKey) {
			return fmt.Errorf("invalid attribution owner or lineage")
		}
		seen[o.AgentCallKey] = true
		if old, ok := l.AgentCalls[o.AgentCallKey]; ok {
			if old != o {
				return fmt.Errorf("agent owner changed")
			}
		} else {
			added++
		}
	}
	if len(l.AgentCalls)+added > c.config.MaxAgentCalls {
		return fmt.Errorf("agent call limit exceeded")
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
		return fmt.Errorf("invalid reservation")
	}
	if c.config.MaxReservedTokens > 0 && tokens == 0 {
		return ErrTokenEstimateRequired
	}
	return c.transaction(ctx, true, func(l *ledger) error {
		if err := c.checkDeadline(ctx); err != nil {
			return err
		}
		if err := c.register(l, lineage); err != nil {
			return err
		}
		if _, ok := l.Entries[key]; ok {
			return ErrReservationExists
		}
		if len(l.Entries) >= c.config.MaxModelRequests {
			return ErrBudgetExceeded
		}
		n, e := reservedTokens(l)
		if e != nil {
			return e
		}
		if tokens > math.MaxInt-n || (c.config.MaxReservedTokens > 0 && tokens > c.config.MaxReservedTokens-n) {
			return ErrBudgetExceeded
		}
		l.Entries[key] = nil
		if tokens > 0 {
			if l.Estimates == nil {
				l.Estimates = map[string]int{}
			}
			l.Estimates[key] = tokens
		}
		if l.AttemptOwners == nil {
			l.AttemptOwners = map[string]string{}
		}
		l.AttemptOwners[key] = lineage[len(lineage)-1].AgentCallKey
		return nil
	})
}
func (c *Controller) RecordEvidence(ctx context.Context, key string, lineage []usage.Owner, e usage.Evidence) (out usage.Evidence, err error) {
	if err = e.Validate(); err != nil {
		return
	}
	err = c.transaction(ctx, true, func(l *ledger) error {
		if err := c.register(l, lineage); err != nil {
			return err
		}
		if _, ok := l.Entries[key]; !ok {
			return fmt.Errorf("usage has no reservation")
		}
		if l.AttemptOwners[key] != lineage[len(lineage)-1].AgentCallKey {
			return fmt.Errorf("attempt owner changed")
		}
		old := l.Evidence[key]
		var err error
		out, err = usage.Merge(old, e)
		if err != nil {
			return err
		}
		if l.Evidence == nil {
			l.Evidence = map[string]usage.Evidence{}
		}
		l.Evidence[key] = out
		if out.KnownFields != 0 {
			u := out.Tokens
			l.Entries[key] = &u
		}
		return nil
	})
	return
}
func (c *Controller) validateAttribution(l *ledger) error {
	if !c.AttributionEnabled() {
		if len(l.AgentCalls)+len(l.AttemptOwners)+len(l.Evidence) > 0 {
			return fmt.Errorf("v1 ledger contains attribution")
		}
		return nil
	}
	if len(l.AgentCalls) > c.config.MaxAgentCalls {
		return fmt.Errorf("agent call limit exceeded")
	}
	for key, o := range l.AgentCalls {
		if key != o.AgentCallKey || !validOwner(o) || o.RunID != c.config.RootKey {
			return fmt.Errorf("invalid saved owner")
		}
		seen := map[string]bool{}
		for o.ParentAgentCallKey != "" {
			if seen[o.AgentCallKey] {
				return fmt.Errorf("attribution cycle")
			}
			seen[o.AgentCallKey] = true
			var ok bool
			o, ok = l.AgentCalls[o.ParentAgentCallKey]
			if !ok {
				return fmt.Errorf("missing ancestor")
			}
		}
	}
	for key, u := range l.Entries {
		if l.AttemptOwners[key] == "" {
			return fmt.Errorf("missing attempt owner")
		}
		if u != nil {
			if _, ok := l.Evidence[key]; !ok {
				return fmt.Errorf("missing usage evidence")
			}
		}
	}
	for key, owner := range l.AttemptOwners {
		if _, ok := l.Entries[key]; !ok {
			return fmt.Errorf("orphan attempt owner")
		}
		if _, ok := l.AgentCalls[owner]; !ok {
			return fmt.Errorf("unknown attempt owner")
		}
	}
	for key, e := range l.Evidence {
		u, ok := l.Entries[key]
		if !ok {
			return fmt.Errorf("orphan evidence")
		}
		if err := e.Validate(); err != nil {
			return err
		}
		if (e.KnownFields == 0 && u != nil) || (e.KnownFields != 0 && (u == nil || *u != e.Tokens)) {
			return fmt.Errorf("inconsistent evidence")
		}
	}
	return nil
}
func addTokens(dst *interfaces.TokenUsage, src interfaces.TokenUsage) error {
	d := []*int{&dst.PromptTokens, &dst.CompletionTokens, &dst.TotalTokens, &dst.CachedTokens}
	s := []int{src.PromptTokens, src.CompletionTokens, src.TotalTokens, src.CachedTokens}
	for i, p := range d {
		if s[i] < 0 || *p > math.MaxInt-s[i] {
			return fmt.Errorf("token sum overflow")
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
	return nil
}
func (c *Controller) Attribution(ctx context.Context) (out AttributionReport, err error) {
	err = c.transaction(ctx, false, func(l *ledger) error {
		calls := map[string]*CallAttribution{}
		roles := map[string]*AgentAttribution{}
		out.Complete = c.AttributionEnabled()
		for k, o := range l.AgentCalls {
			calls[k] = &CallAttribution{Owner: o}
			if roles[o.AgentID] == nil {
				roles[o.AgentID] = &AgentAttribution{AgentID: o.AgentID}
			}
		}
		out.Root.ReservedRequests = len(l.Entries)
		out.Root.ReservedToolCalls = len(l.Tools)
		var e error
		out.Root.ReservedTokens, e = reservedTokens(l)
		if e != nil {
			return e
		}
		for key, u := range l.Entries {
			s := UsageSummary{ReservedRequests: 1}
			fields := usage.Fields(0)
			if u == nil {
				s.UnknownUsageEntries = 1
				out.Root.UnknownUsageEntries++
			} else {
				s.KnownUsageEntries = 1
				s.KnownTokens = *u
				out.Root.KnownUsageEntries++
				if err := addTokens(&out.Root.KnownTokens, *u); err != nil {
					return err
				}
				if c.knownTotal(l, key) {
					delta := u.TotalTokens - l.Estimates[key]
					if (delta > 0 && out.Root.KnownTokenDelta > math.MaxInt-delta) || (delta < 0 && out.Root.KnownTokenDelta < math.MinInt-delta) {
						return fmt.Errorf("token delta overflow")
					}
					out.Root.KnownTokenDelta += delta
				}
				fields = usage.BasicFields
				if evidence, ok := l.Evidence[key]; ok {
					fields = evidence.KnownFields
				}
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
			out.UnknownUsageEntries += s.UnknownUsageEntries
			out.PartialUsageEntries += s.PartialUsageEntries
			if s.UnknownUsageEntries+s.PartialUsageEntries > 0 {
				out.Complete = false
			}
			call := calls[l.AttemptOwners[key]]
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

// knownTotal retains legacy total semantics and gates v2 deltas on reported evidence.
func (c *Controller) knownTotal(l *ledger, key string) bool {
	return !c.AttributionEnabled() || l.Evidence[key].KnownFields&usage.Total != 0
}
