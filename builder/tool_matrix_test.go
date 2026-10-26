package builder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

type matrixTool struct {
	*MockTool
	output *interfaces.ToolOutput
	err    error
	calls  int
}

func (t *matrixTool) Invoke(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	t.calls++
	return t.output, t.err
}

// 20 类执行路径各运行 80 组不同内容/分片/ID，覆盖边界交叉而非重复同一条样例。
func TestNativeToolLargeMatrix(t *testing.T) {
	scenarios := []string{"直接文本", "直接JSON", "默认循环", "多工具", "名单外", "无用量", "无工具答案", "坏参数", "重复ID", "空ID", "未知工具", "失败状态", "空输出", "空文本", "截断", "上游错误", "取消", "长度截断", "预算不足", "空工具"}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			for sample := 0; sample < 80; sample++ {
				text := fmt.Sprintf("记录%d：✅\n路径\\data；\"字段\"；%s", sample, strings.Repeat("跨语言café", sample%20))
				id := fmt.Sprintf("call-%d", sample)
				call := nativeCall(id, "status", fmt.Sprintf(`{"record":%d}`, sample))
				first := &llm.StreamChunk{Done: true, FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{call}, Usage: &llm.Usage{TotalTokens: 10 + sample}}
				turns := [][]*llm.StreamChunk{{{Delta: "查询中"}, first}}
				output := &interfaces.ToolOutput{Success: true, Result: text}
				tool := &matrixTool{MockTool: NewMockTool("status", ""), output: output}
				ctx, cancel := context.WithCancel(context.Background())
				budget, wantCalls, wantTools := 1, 1, 1
				direct, wantErr := true, false
				want := text
				switch scenario {
				case "直接JSON":
					output.Result = map[string]any{"record": sample, "ok": true}
					want = fmt.Sprintf(`{"ok":true,"record":%d}`, sample)
				case "默认循环", "多工具", "名单外":
					budget, wantCalls = 2, 2
					if scenario == "默认循环" {
						direct = false
					}
					if scenario == "多工具" {
						first.ToolCalls = append(first.ToolCalls, nativeCall(id+"-2", "status", "{}"))
						wantTools = 2
					}
					var final []*llm.StreamChunk
					runes := []rune(text)
					step := sample%7 + 1
					for start := 0; start < len(runes); start += step {
						end := min(start+step, len(runes))
						final = append(final, &llm.StreamChunk{Delta: string(runes[start:end])})
					}
					final = append(final, &llm.StreamChunk{Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 20}})
					turns = append(turns, final)
				case "无用量":
					first.Usage = nil
				case "无工具答案":
					first.ToolCalls = nil
					first.FinishReason = "stop"
					turns[0][0].Delta = text
					wantTools = 0
				case "坏参数":
					first.ToolCalls[0].Function.Arguments = []string{"null", "[]", `{"record":`, "false"}[sample%4]
					wantErr = true
					wantTools = 0
				case "重复ID":
					first.ToolCalls = append(first.ToolCalls, call)
					budget = 2
					wantErr = true
					wantTools = 0
				case "空ID":
					first.ToolCalls[0].ID = ""
					wantErr = true
					wantTools = 0
				case "未知工具":
					first.ToolCalls[0].Function.Name = "missing"
					budget = 2
					wantErr = true
					wantTools = 0
				case "失败状态":
					output.Success = false
					wantErr = true
				case "空输出":
					tool.output = nil
					wantErr = true
				case "空文本":
					output.Result = " \n\t"
					wantErr = true
				case "截断":
					turns[0] = turns[0][:1]
					wantErr = true
					wantTools = 0
				case "上游错误":
					first.Error = errors.New("模型服务失败")
					wantErr = true
					wantTools = 0
				case "取消":
					cancel()
					wantErr = true
					wantTools = 0
					wantCalls = 0
				case "长度截断":
					first.FinishReason = "length"
					wantErr = true
					wantTools = 0
				case "预算不足":
					direct = false
					wantErr = true
					wantTools = 0
				case "空工具":
					first.ToolCalls = nil
					wantErr = true
					wantTools = 0
				}
				model := nativeModel(turns...)
				b := NewSimpleBuilder(model).WithTools(tool).WithMaxIterations(budget).WithAutoSaveEnabled(false)
				if direct {
					name := "status"
					if scenario == "名单外" {
						name = "final"
						b.WithTools(NewMockTool("final", "结果"))
					}
					b.WithReturnDirectTools(name)
				}
				agent, err := b.Build()
				require.NoError(t, err, "%s/%d", scenario, sample)
				result, err := agent.AsRunnable().Invoke(ctx, fmt.Sprintf("问题%d", sample))
				cancel()
				if wantErr {
					require.Error(t, err, "%s/%d", scenario, sample)
					require.Nil(t, result)
				} else {
					require.NoError(t, err, "%s/%d", scenario, sample)
					require.Equal(t, want, result.Result, "%s/%d", scenario, sample)
					require.Equal(t, scenario != "无用量", result.Metadata["usage_complete"])
				}
				require.Len(t, model.requests, wantCalls, "%s/%d", scenario, sample)
				if wantCalls == 2 {
					messages := model.requests[1].Messages
					require.GreaterOrEqual(t, len(messages), wantTools)
					firstTool := messages[len(messages)-wantTools]
					require.Equal(t, "tool", firstTool.Role)
					require.Equal(t, text, firstTool.Content)
					require.Equal(t, id, firstTool.ToolCallID)
				}
				require.Equal(t, wantTools, tool.calls, "%s/%d", scenario, sample)
			}
		})
	}
}
