package tools

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// Rejected 构造业务拒绝结果：目标系统明确给出的结论，如不存在、无权限、余额不足、状态不允许。
// 它作为工具结果交给模型，经 Dispatcher 时日志记为已完成，恢复时只回放、不重试。
// error 只用于调用没有完成（网络中断、服务不可用、超时）。message 原样交给模型，不得包含内部细节；
// data 可为 nil，须能 JSON 编码。
func Rejected(message string, data any) *interfaces.ToolOutput {
	result := map[string]any{"error": "rejected", "message": message}
	if data != nil {
		result["data"] = data
	}
	return &interfaces.ToolOutput{Success: false, Result: result}
}

// RejectedResult 判断 out 是否为 Rejected 构造的结果，并返回其中的 message。
func RejectedResult(out *interfaces.ToolOutput) (string, bool) {
	if out == nil || out.Success || out.Error != "" {
		return "", false
	}
	m, _ := out.Result.(map[string]any)
	message, ok := m["message"].(string)
	return message, ok && m["error"] == "rejected"
}

// ReconcilableTool 由做不到幂等、但能按执行键查询效果是否已生效的工具实现。
// 经 Dispatcher 持久执行时，上次执行中断、结果未知的调用在重跑前先核对：found 为真时用 out 补记为已完成，
// 不再执行工具；返回错误时日志不变，本次调用失败。input 与原调用的 ExecutionKey、CallID、Args 相同。
// out 必须成功或为 Rejected 结果。
type ReconcilableTool interface {
	interfaces.Tool
	Reconcile(ctx context.Context, input *interfaces.ToolInput) (out *interfaces.ToolOutput, found bool, err error)
}
