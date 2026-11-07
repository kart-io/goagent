package agentbench

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// ToolContract 是精确工具参数与最终回执的宿主验收规则，不改变 Provider 的输出原文。
// StringNumbers 显式选择字符串传输；上游已经舍入的 JSON 数字不能恢复原值。
type ToolContract struct {
	ID, Rate, Label, Receipt string
	StringNumbers            bool
}

// Schema 返回本组验收使用的参数协议，数值模式只用于仍能保留数字原文的端点。
func (c ToolContract) Schema() string {
	if c.StringNumbers {
		return `{"type":"object","properties":{"id":{"type":"string","pattern":"^[0-9]+$"},"rate":{"type":"string","pattern":"^-?(0|[1-9][0-9]*)([.][0-9]+)?([eE][+-]?[0-9]+)?$"},"label":{"type":"string"}},"required":["id","rate","label"],"additionalProperties":false}`
	}
	return `{"type":"object","properties":{"id":{"type":"integer"},"rate":{"type":"number"},"label":{"type":"string"}},"required":["id","rate","label"],"additionalProperties":false}`
}

// CheckArguments 在授权和执行两处检查同一个原始值，任何不一致都应在效果发生前失败。
func (c ToolContract) CheckArguments(input *interfaces.ToolInput) error {
	if input == nil || c.ID == "" || c.Rate == "" {
		return agentErrors.Newf(agentErrors.CodeInvalidOutput, "缺少精确参数或宿主参考值").WithComponent("evaluation/agentbench").WithOperation("check_arguments")
	}
	for _, field := range []struct{ name, want string }{{"id", c.ID}, {"rate", c.Rate}} {
		value := input.Args[field.name]
		if c.StringNumbers {
			if value != field.want {
				return agentErrors.Newf(agentErrors.CodeInvalidOutput, "字段 %s 必须按字符串保持原始精度", field.name).WithComponent("evaluation/agentbench").WithOperation("check_arguments")
			}
		} else {
			b, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("字段 %s 不是有效数字: %w", field.name, err)
			}
			got, ok := new(big.Rat).SetString(string(b))
			want, valid := new(big.Rat).SetString(field.want)
			if !ok || !valid || got.Cmp(want) != 0 {
				return agentErrors.Newf(agentErrors.CodeInvalidOutput, "字段 %s 与宿主精确值不一致", field.name).WithComponent("evaluation/agentbench").WithOperation("check_arguments")
			}
		}
	}
	if input.Args["label"] != c.Label {
		return agentErrors.Newf(agentErrors.CodeInvalidOutput, "字段 label 与宿主参考值不一致").WithComponent("evaluation/agentbench").WithOperation("check_arguments")
	}
	return nil
}

// CheckAnswer 只接受原回执文本。调用方须在返回权威终态前再执行它，不能把 JSON 提取成成功答案。
func (c ToolContract) CheckAnswer(answer any) error {
	text, ok := answer.(string)
	if c.Receipt == "" || !ok || strings.TrimSpace(text) != c.Receipt {
		return agentErrors.Newf(agentErrors.CodeInvalidOutput, "最终答案必须只包含实际工具回执").WithComponent("evaluation/agentbench").WithOperation("check_answer")
	}
	return nil
}

// Stop 由 Builder 的 TurnStopping 调用，宿主另以根预算约束真实请求数。
func (c ToolContract) Stop(ctx context.Context, stopping *builder.Stopping) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if stopping == nil || c.Receipt == "" {
		return "", agentErrors.Newf(agentErrors.CodeInvalidOutput, "缺少最终答复或宿主回执").WithComponent("evaluation/agentbench").WithOperation("stop")
	}
	if c.CheckAnswer(stopping.Answer) == nil {
		return "", nil
	}
	if stopping.Blocked >= 1 {
		return "", agentErrors.Newf(agentErrors.CodeInvalidOutput, "已纠正一次，最终答案仍不符合回执格式").WithComponent("evaluation/agentbench").WithOperation("stop")
	}
	return "上一轮回答格式错误。最终只输出已完成工具结果中 receipt 字段的原始字符串值；不要输出 JSON、引号、代码块或解释。不得再次调用工具。", nil
}
