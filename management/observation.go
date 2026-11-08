package management

import (
	"context"
	agentErrors "github.com/kart-io/goagent/errors"
	"math"
	"sort"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

func millisBetween(from, to time.Time) int64 {
	if from.IsZero() || to.Before(from) {
		return 0
	}
	return to.Sub(from).Milliseconds()
}
func projectDurations(run *Run, now time.Time) {
	run.DurationComplete = true
	end := now
	if run.Task.FinishedAt != nil {
		end = *run.Task.FinishedAt
	}
	run.ElapsedMillis = millisBetween(run.Task.CreatedAt, end)
	attempts := append([]Attempt(nil), run.Attempts...)
	sort.Slice(attempts, func(i, j int) bool { return attempts[i].Number < attempts[j].Number })
	if len(attempts) == 0 {
		run.QueuedMillis = run.ElapsedMillis
		return
	}
	run.QueuedMillis = millisBetween(run.Task.CreatedAt, attempts[0].CreatedAt)
	for i, attempt := range attempts {
		finish := end
		if attempt.FinishedAt != nil {
			finish = *attempt.FinishedAt
		} else {
			run.DurationComplete = false
			run.UnfinishedAttempts++
			// 崩溃旧尝试没有可靠退出时间，不把停机或后续尝试区间算成已知执行。
			if i+1 < len(attempts) || run.Task.State.Terminal() || (run.Task.State != Running && run.Task.State != Canceling) {
				continue
			}
		}
		run.ExecutionMillis += millisBetween(attempt.CreatedAt, finish)
		if attempt.TraceID != "" {
			run.TraceID = attempt.TraceID
		}
		if attempt.FinishedAt == nil {
			continue
		}
		next := end
		if i+1 < len(attempts) {
			next = attempts[i+1].CreatedAt
		}
		switch TaskState(attempt.Outcome) {
		case WaitingInput, WaitingApproval, Attention:
			run.WaitingMillis += millisBetween(*attempt.FinishedAt, next)
		case Queued:
			run.QueuedMillis += millisBetween(*attempt.FinishedAt, next)
		}
	}
}

type RunTotals struct {
	DurationComplete       bool                   `json:"duration_complete"`
	DurationIncompleteRuns int                    `json:"duration_incomplete_runs"`
	Tasks                  int                    `json:"tasks"`
	Attempts               int                    `json:"attempts"`
	States                 map[TaskState]int      `json:"states"`
	KnownRuns              int                    `json:"known_runs"`
	UnknownRuns            int                    `json:"unknown_runs"`
	PartialRuns            int                    `json:"partial_runs"`
	KnownTokens            *interfaces.TokenUsage `json:"known_tokens"`
	QueuedMillis           int64                  `json:"queued_millis"`
	ExecutionMillis        int64                  `json:"execution_millis"`
	WaitingMillis          int64                  `json:"waiting_millis"`
	Complete               bool                   `json:"complete"`
}
type RunGroup struct {
	ID string `json:"id"`
	RunTotals
}
type AgentUsageTotal struct {
	AgentID        string                 `json:"agent_id"`
	KnownTokens    *interfaces.TokenUsage `json:"known_tokens"`
	KnownEntries   int                    `json:"known_entries"`
	UnknownEntries int                    `json:"unknown_entries"`
	PartialEntries int                    `json:"partial_entries"`
	Complete       bool                   `json:"complete"`
}

type RunSummary struct {
	UnattributedKnownEntries   int               `json:"unattributed_known_entries"`
	UnattributedUnknownEntries int               `json:"unattributed_unknown_entries"`
	UnattributedPartialEntries int               `json:"unattributed_partial_entries"`
	UnattributedComplete       bool              `json:"unattributed_complete"`
	AgentUsage                 []AgentUsageTotal `json:"agent_usage"`
	// CreatedTo 仅固定创建时间范围；各运行证据在扫描时读取，不承诺跨页事务快照。
	CreatedTo    time.Time              `json:"created_to"`
	Totals       RunTotals              `json:"totals"`
	Agents       []RunGroup             `json:"agents"`
	Sessions     []RunGroup             `json:"sessions"`
	Unattributed *interfaces.TokenUsage `json:"unattributed"`
}

func addTokens(target **interfaces.TokenUsage, value interfaces.TokenUsage) error {
	if *target == nil {
		*target = &interfaces.TokenUsage{}
	}
	dst := []*int{&(*target).PromptTokens, &(*target).CompletionTokens, &(*target).TotalTokens, &(*target).CachedTokens, &(*target).ReasoningTokens, &(*target).ToolUsePromptTokens}
	src := []int{value.PromptTokens, value.CompletionTokens, value.TotalTokens, value.CachedTokens, value.ReasoningTokens, value.ToolUsePromptTokens}
	for i, v := range src {
		if v < 0 || *dst[i] > math.MaxInt-v {
			return agentErrors.ErrManagementInvalid
		}
	}
	for i, v := range src {
		*dst[i] += v
	}
	return nil
}
func addRunTotals(dst *RunTotals, run Run, known *interfaces.TokenUsage) error {
	if dst.States == nil {
		dst.States = map[TaskState]int{}
		dst.Complete = true
		dst.DurationComplete = true
	}
	if !run.DurationComplete {
		dst.DurationComplete = false
		dst.DurationIncompleteRuns++
	}
	dst.Tasks++
	dst.Attempts += len(run.Attempts)
	dst.States[run.Task.State]++
	dst.QueuedMillis += run.QueuedMillis
	dst.ExecutionMillis += run.ExecutionMillis
	dst.WaitingMillis += run.WaitingMillis
	if known == nil {
		dst.UnknownRuns++
		dst.Complete = false
		return nil
	}
	dst.KnownRuns++
	if !run.Complete {
		dst.PartialRuns++
		dst.Complete = false
	}
	return addTokens(&dst.KnownTokens, *known)
}

// SummarizeRuns 扫描全部授权结果，limit 仅控制每页大小，不截断统计。
// Session 与总量每个根运行只加一次；Agent 用量只取该 Agent 的 self，
// 子 Agent 与未归属消耗不冒充顶层 Agent 消耗。没有证据时保留 null 与未知计数。
func (s *Service) SummarizeRuns(ctx context.Context, a Actor, q Query) (RunSummary, error) {
	out := RunSummary{Agents: []RunGroup{}, Sessions: []RunGroup{}, UnattributedComplete: true, Totals: RunTotals{States: map[TaskState]int{}, Complete: true, DurationComplete: true}}
	if q.Cursor != "" {
		return out, agentErrors.ErrManagementInvalid
	}
	if q.CreatedTo.IsZero() {
		q.CreatedTo = s.cfg.Now().UTC()
	}
	out.CreatedTo = q.CreatedTo
	agentUsage := map[string]*AgentUsageTotal{}
	agents := map[string]*RunGroup{}
	sessions := map[string]*RunGroup{}
	for {
		page, err := s.ListRuns(ctx, a, q)
		if err != nil {
			return out, err
		}
		for _, run := range page.Items {
			var root, own *interfaces.TokenUsage
			if run.Usage != nil {
				if run.Usage.Root.KnownUsageEntries > 0 || run.Usage.Root.UnknownUsageEntries == 0 {
					root = &run.Usage.Root.KnownTokens
				}
				for _, agent := range run.Usage.Agents {
					if agentUsage[agent.AgentID] == nil {
						agentUsage[agent.AgentID] = &AgentUsageTotal{AgentID: agent.AgentID, Complete: true}
					}
					total := agentUsage[agent.AgentID]
					total.KnownEntries += agent.Self.KnownUsageEntries
					total.UnknownEntries += agent.Self.UnknownUsageEntries
					total.PartialEntries += agent.Self.PartialUsageEntries
					total.Complete = total.UnknownEntries == 0 && total.PartialEntries == 0
					if agent.Self.KnownUsageEntries > 0 || agent.Self.UnknownUsageEntries == 0 {
						if err = addTokens(&total.KnownTokens, agent.Self.KnownTokens); err != nil {
							return out, err
						}
						if agent.AgentID == run.Task.AgentID {
							copy := agent.Self.KnownTokens
							own = &copy
						}
					}
				}
				unknown := run.Usage.Unattributed
				out.UnattributedKnownEntries += unknown.KnownUsageEntries
				out.UnattributedUnknownEntries += unknown.UnknownUsageEntries
				out.UnattributedPartialEntries += unknown.PartialUsageEntries
				if unknown.UnknownUsageEntries > 0 || unknown.PartialUsageEntries > 0 {
					out.UnattributedComplete = false
				}
				if unknown.KnownUsageEntries > 0 || unknown.UnknownUsageEntries == 0 {
					if err = addTokens(&out.Unattributed, unknown.KnownTokens); err != nil {
						return out, err
					}
				}
			} else {
				out.UnattributedComplete = false
			}
			if err = addRunTotals(&out.Totals, run, root); err != nil {
				return out, err
			}
			if agents[run.Task.AgentID] == nil {
				agents[run.Task.AgentID] = &RunGroup{ID: run.Task.AgentID}
			}
			if sessions[run.Task.SessionID] == nil {
				sessions[run.Task.SessionID] = &RunGroup{ID: run.Task.SessionID}
			}
			if err = addRunTotals(&agents[run.Task.AgentID].RunTotals, run, own); err != nil {
				return out, err
			}
			if err = addRunTotals(&sessions[run.Task.SessionID].RunTotals, run, root); err != nil {
				return out, err
			}
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	for _, total := range agentUsage {
		out.AgentUsage = append(out.AgentUsage, *total)
	}
	sort.Slice(out.AgentUsage, func(i, j int) bool { return out.AgentUsage[i].AgentID < out.AgentUsage[j].AgentID })
	for _, group := range agents {
		out.Agents = append(out.Agents, *group)
	}
	for _, group := range sessions {
		out.Sessions = append(out.Sessions, *group)
	}
	sort.Slice(out.Agents, func(i, j int) bool { return out.Agents[i].ID < out.Agents[j].ID })
	sort.Slice(out.Sessions, func(i, j int) bool { return out.Sessions[i].ID < out.Sessions[j].ID })
	return out, nil
}
