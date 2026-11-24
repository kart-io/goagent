package contextpolicy

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// RecallTool lets the model retrieve original messages in this session that
// were replaced by compaction or offloading (messages rewritten by
// history/rewrite in the log and not yet undone). The session ID comes from the
// request context (tools.WithSessionID; the Builder fills in the WithSessionID
// value by default), so only this session can be read.
type RecallTool struct {
	store  session.Store
	config RecallConfig
}

// RecallConfig limits what one recall_history call returns. Limits are caller
// policy: 0 means unlimited, negative values are rejected.
type RecallConfig struct {
	// MaxLimit caps the number of messages the model may ask for in one call;
	// a call without limit still returns constants.DefaultRecallHistoryLimit.
	MaxLimit int
	// MaxRunes truncates each returned message, marking the cut.
	MaxRunes int
}

// NewRecallTool returns a recall tool that reads session logs from store; it
// must use the same store as WithSessionLog. It keeps the fixed caps it always
// had (constants.MaxRecallHistoryLimit messages, constants.MaxRecallHistoryRunes
// per message); use NewRecallToolConfig to choose them.
func NewRecallTool(store session.Store) (*RecallTool, error) {
	return NewRecallToolConfig(store, RecallConfig{MaxLimit: constants.MaxRecallHistoryLimit, MaxRunes: constants.MaxRecallHistoryRunes})
}

// NewRecallToolConfig is NewRecallTool with caller-owned limits.
func NewRecallToolConfig(store session.Store, cfg RecallConfig) (*RecallTool, error) {
	if store == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "recall_history 需要会话日志存储").WithComponent("builder/contextpolicy").WithOperation("new_recall_tool")
	}
	if cfg.MaxLimit < 0 || cfg.MaxRunes < 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "recall_history 的上限不能为负").WithComponent("builder/contextpolicy").WithOperation("new_recall_tool")
	}
	return &RecallTool{store: store, config: cfg}, nil
}

// Name returns constants.ToolRecallHistory.
func (r *RecallTool) Name() string { return constants.ToolRecallHistory }

// SupportsConcurrentCalls implements interfaces.ConcurrentCallTool: a recall
// only reads the session log.
func (r *RecallTool) SupportsConcurrentCalls() bool { return true }

// Description returns the model-facing description of recall_history.
func (r *RecallTool) Description() string {
	return "取回本会话中已被压缩为摘要的原始消息。query 按子串过滤；before 只返回序号小于它的消息，用上次结果的 next_before 向前翻页；limit 默认 20。"
}

// ArgsSchema returns the JSON schema for query, before and limit; limit is
// capped at MaxLimit when set.
func (r *RecallTool) ArgsSchema() string {
	limit := `{"type":"integer","minimum":1}`
	if r.config.MaxLimit > 0 {
		limit = fmt.Sprintf(`{"type":"integer","minimum":1,"maximum":%d}`, r.config.MaxLimit)
	}
	return `{"type":"object","properties":{"query":{"type":"string"},"before":{"type":"integer","minimum":1},"limit":` + limit + `},"additionalProperties":false}`
}

type recalled struct {
	Seq     int64  `json:"seq"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Tool    string `json:"tool,omitempty"`
}

// Invoke fetches matching messages from the most recent backward, returning
// results in ascending sequence order; next_before of 0 means there are no
// earlier matches.
func (r *RecallTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	id := tools.SessionIDFrom(ctx)
	if id == "" {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "recall_history 需要会话 ID").WithComponent("builder/contextpolicy").WithOperation("recall")
	}
	var args map[string]any
	if input != nil {
		args = input.Args
	}
	query, _ := args["query"].(string)
	before, limit := intArg(args["before"]), intArg(args["limit"])
	if limit <= 0 {
		limit = constants.DefaultRecallHistoryLimit
	}
	if r.config.MaxLimit > 0 {
		limit = min(limit, int64(r.config.MaxLimit))
	}
	events, err := r.store.Read(ctx, id, 0)
	if err != nil {
		return nil, err
	}
	archived, err := session.Archived(events)
	if err != nil {
		return nil, err
	}
	var out []recalled
	next := int64(0)
	for i := len(archived) - 1; i >= 0; i-- {
		e := archived[i]
		if (before > 0 && e.Seq >= before) || (query != "" && !strings.Contains(e.Message.Content, query)) {
			continue
		}
		if int64(len(out)) == limit {
			next = out[len(out)-1].Seq
			break
		}
		item := recalled{Seq: e.Seq, Role: e.Message.Role, Content: e.Message.Content}
		if r.config.MaxRunes > 0 {
			item.Content = truncateRunes(item.Content, r.config.MaxRunes)
		}
		if e.Tool != nil {
			item.Tool = e.Tool.Name
		}
		out = append(out, item)
	}
	slices.Reverse(out)
	b, err := json.Marshal(map[string]any{"messages": out, "next_before": next})
	if err != nil {
		return nil, err
	}
	return &interfaces.ToolOutput{Success: true, Result: string(b)}, nil
}

func intArg(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
