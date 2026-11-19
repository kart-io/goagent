package contextpolicy

import (
	"context"
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
	store session.Store
}

// NewRecallTool returns a recall tool that reads session logs from store; it
// must use the same store as WithSessionLog.
func NewRecallTool(store session.Store) (*RecallTool, error) {
	if store == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "recall_history 需要会话日志存储").WithComponent("builder/contextpolicy").WithOperation("new_recall_tool")
	}
	return &RecallTool{store: store}, nil
}

func (r *RecallTool) Name() string { return constants.ToolRecallHistory }
func (r *RecallTool) Description() string {
	return "取回本会话中已被压缩为摘要的原始消息。query 按子串过滤；before 只返回序号小于它的消息，用上次结果的 next_before 向前翻页；limit 默认 20。"
}

func (r *RecallTool) ArgsSchema() string {
	return `{"type":"object","properties":{"query":{"type":"string"},"before":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`
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
	limit = min(limit, constants.MaxRecallHistoryLimit)
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
		item := recalled{Seq: e.Seq, Role: e.Message.Role, Content: truncateRunes(e.Message.Content, constants.MaxRecallHistoryRunes)}
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
