package tools

import (
	"context"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// Rejected builds a business rejection result: an explicit conclusion from the
// target system, such as not found, permission denied, insufficient balance or
// state not allowed. It is passed to the model as the tool result; through the
// Dispatcher it is journaled as completed and is only replayed, never retried,
// on resume. Use an error only when the call did not complete (network
// interruption, service unavailable, timeout). message is passed to the model
// verbatim and must not contain internal details; data may be nil and must be
// JSON-encodable. Result is encoded as
// {"error":"rejected","message":...,"data":...}, but only results built by
// this function are recognized as business rejections; maps of the same shape
// are treated as ordinary failures.
func Rejected(message string, data any) *interfaces.ToolOutput {
	return &interfaces.ToolOutput{Success: false, Result: rejection{message: message, data: data}}
}

// RejectedResult reports whether out was built by Rejected (including results
// replayed from the Dispatcher journal), and returns its message.
func RejectedResult(out *interfaces.ToolOutput) (string, bool) {
	if out == nil || out.Success || out.Error != "" {
		return "", false
	}
	r, ok := out.Result.(rejection)
	return r.message, ok
}

// rejection 不导出，包外无法构造：透传上游 JSON 的工具拿到同形状的数据时不会被误认为业务拒绝。
type rejection struct {
	message string
	data    any
}

func (r rejection) MarshalJSON() ([]byte, error) {
	out := map[string]any{"error": "rejected", "message": r.message}
	if r.data != nil {
		out["data"] = r.data
	}
	return json.Marshal(out, json.NoEscapeHTML)
}

// journaledOutput 是工具结果在操作日志中的形式。业务拒绝标记位于 ToolOutput 字段之外，
// 工具返回的任何数据都写不到这一层，恢复时据此重建 rejection。
type journaledOutput struct {
	*interfaces.ToolOutput
	Rejected bool `json:"goagent_rejected,omitzero"`
}

func encodeJournaledOutput(out *interfaces.ToolOutput) (json.RawMessage, error) {
	_, rejected := RejectedResult(out)
	return json.Marshal(journaledOutput{ToolOutput: out, Rejected: rejected})
}

// decodeJournaledOutput 解码日志中的工具结果，数字为 json.Number。
func decodeJournaledOutput(data json.RawMessage) (*interfaces.ToolOutput, error) {
	var j journaledOutput
	if err := json.Unmarshal(data, &j, json.UseNumber); err != nil {
		return nil, err
	}
	if !j.Rejected {
		return j.ToolOutput, nil
	}
	if j.ToolOutput == nil || j.Success || j.Error != "" {
		return nil, agentErrors.Newf(agentErrors.CodeStateValidation, "invalid journaled rejected tool result").
			WithComponent("dispatcher").
			WithOperation("decode_journaled_output")
	}
	m, _ := j.Result.(map[string]any)
	message, ok := m["message"].(string)
	if !ok {
		return nil, agentErrors.Newf(agentErrors.CodeStateValidation, "invalid journaled rejected tool result").
			WithComponent("dispatcher").
			WithOperation("decode_journaled_output")
	}
	j.Result = rejection{message: message, data: m["data"]}
	return j.ToolOutput, nil
}

// ReconcilableTool is implemented by tools that cannot be idempotent but can
// query by execution key whether an effect has already taken place. During
// durable execution through the Dispatcher, a call whose previous execution
// was interrupted with an unknown result is reconciled before rerunning: if
// found is true, out is recorded as completed and the tool is not executed;
// if it returns an error, the journal is unchanged and this call fails. input
// has the same ExecutionKey, CallID and Args as the original call. out must be
// successful or a Rejected result.
type ReconcilableTool interface {
	interfaces.Tool
	Reconcile(ctx context.Context, input *interfaces.ToolInput) (out *interfaces.ToolOutput, found bool, err error)
}
