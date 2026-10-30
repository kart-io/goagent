package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/kart-io/goagent/interfaces"
)

// Rejected 构造业务拒绝结果：目标系统明确给出的结论，如不存在、无权限、余额不足、状态不允许。
// 它作为工具结果交给模型，经 Dispatcher 时日志记为已完成，恢复时只回放、不重试。
// error 只用于调用没有完成（网络中断、服务不可用、超时）。message 原样交给模型，不得包含内部细节；
// data 可为 nil，须能 JSON 编码。Result 编码为 {"error":"rejected","message":...,"data":...}，
// 但只有本函数构造的结果才被识别为业务拒绝，同形状的 map 按普通失败处理。
func Rejected(message string, data any) *interfaces.ToolOutput {
	return &interfaces.ToolOutput{Success: false, Result: rejection{message: message, data: data}}
}

// RejectedResult 判断 out 是否为 Rejected 构造的结果（含经 Dispatcher 日志回放的结果），并返回其中的 message。
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
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(out); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// journaledOutput 是工具结果在操作日志中的形式。业务拒绝标记位于 ToolOutput 字段之外，
// 工具返回的任何数据都写不到这一层，恢复时据此重建 rejection。
type journaledOutput struct {
	*interfaces.ToolOutput
	Rejected bool `json:"goagent_rejected,omitempty"`
}

func encodeJournaledOutput(out *interfaces.ToolOutput) (json.RawMessage, error) {
	_, rejected := RejectedResult(out)
	return json.Marshal(journaledOutput{ToolOutput: out, Rejected: rejected})
}

// decodeJournaledOutput 解码日志中的工具结果，数字为 json.Number。
func decodeJournaledOutput(data json.RawMessage) (*interfaces.ToolOutput, error) {
	var j journaledOutput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&j); err != nil {
		return nil, err
	}
	if !j.Rejected {
		return j.ToolOutput, nil
	}
	if j.ToolOutput == nil || j.Success || j.Error != "" {
		return nil, fmt.Errorf("invalid journaled rejected tool result")
	}
	m, _ := j.Result.(map[string]any)
	message, ok := m["message"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid journaled rejected tool result")
	}
	j.Result = rejection{message: message, data: m["data"]}
	return j.ToolOutput, nil
}

// ReconcilableTool 由做不到幂等、但能按执行键查询效果是否已生效的工具实现。
// 经 Dispatcher 持久执行时，上次执行中断、结果未知的调用在重跑前先核对：found 为真时用 out 补记为已完成，
// 不再执行工具；返回错误时日志不变，本次调用失败。input 与原调用的 ExecutionKey、CallID、Args 相同。
// out 必须成功或为 Rejected 结果。
type ReconcilableTool interface {
	interfaces.Tool
	Reconcile(ctx context.Context, input *interfaces.ToolInput) (out *interfaces.ToolOutput, found bool, err error)
}
