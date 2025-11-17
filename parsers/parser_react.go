package parsers

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
)

// ReActOutput ReAct 解析器的输出结构
type ReActOutput struct {
	FinalAnswer string                 `json:"final_answer,omitempty"`
	Thought     string                 `json:"thought,omitempty"`
	Action      string                 `json:"action,omitempty"`
	ActionInput map[string]interface{} `json:"action_input,omitempty"`
}

// ReActOutputParser 解析 ReAct Agent 的输出
//
// 解析格式:
//
//	Thought: <思考内容>
//	Action: <工具名称>
//	Action Input: <JSON 格式的输入>
//	或
//	Final Answer: <最终答案>
type ReActOutputParser struct {
	*BaseOutputParser[*ReActOutput]
	thoughtPattern     *regexp.Regexp
	actionPattern      *regexp.Regexp
	actionInputPattern *regexp.Regexp
	finalAnswerPattern *regexp.Regexp
}

// NewReActOutputParser 创建 ReAct 输出解析器
func NewReActOutputParser() *ReActOutputParser {
	return &ReActOutputParser{
		BaseOutputParser:   NewBaseOutputParser[*ReActOutput](),
		thoughtPattern:     regexp.MustCompile(`(?i)Thought:\s*(.+?)(?:\n|$)`),
		actionPattern:      regexp.MustCompile(`(?i)Action:\s*(.+?)(?:\n|$)`),
		actionInputPattern: regexp.MustCompile(`(?i)Action Input:\s*(.+?)(?:\n|$)`),
		finalAnswerPattern: regexp.MustCompile(`(?i)Final Answer:\s*(.+?)(?:\n\n|$)`),
	}
}

// Parse 解析 ReAct 输出
func (p *ReActOutputParser) Parse(ctx context.Context, text string) (*ReActOutput, error) {
	result := &ReActOutput{}

	// 清理输出
	text = strings.TrimSpace(text)

	// 检查是否是最终答案
	if matches := p.finalAnswerPattern.FindStringSubmatch(text); len(matches) > 1 {
		result.FinalAnswer = strings.TrimSpace(matches[1])
		return result, nil
	}

	// 解析思考
	if matches := p.thoughtPattern.FindStringSubmatch(text); len(matches) > 1 {
		result.Thought = strings.TrimSpace(matches[1])
	}

	// 解析行动
	if matches := p.actionPattern.FindStringSubmatch(text); len(matches) > 1 {
		result.Action = strings.TrimSpace(matches[1])
	} else {
		return nil, agentErrors.New(agentErrors.CodeParserMissingField, "no action found in output").
			WithComponent("react_parser").
			WithOperation("parse").
			WithContext("text_length", len(text))
	}

	// 解析行动输入
	if matches := p.actionInputPattern.FindStringSubmatch(text); len(matches) > 1 {
		actionInputStr := strings.TrimSpace(matches[1])

		// 尝试解析为 JSON
		var actionInput map[string]interface{}
		if err := json.Unmarshal([]byte(actionInputStr), &actionInput); err == nil {
			result.ActionInput = actionInput
		} else {
			// 如果不是 JSON，作为普通字符串
			result.ActionInput = map[string]interface{}{
				"input": actionInputStr,
			}
		}
	}

	return result, nil
}

// GetFormatInstructions 返回格式说明
func (p *ReActOutputParser) GetFormatInstructions() string {
	return `Use the following format:

Thought: you should always think about what to do
Action: the action to take, should be one of the available tools
Action Input: the input to the action (in JSON format if multiple parameters)
Observation: the result of the action
... (this Thought/Action/Action Input/Observation can repeat N times)
Thought: I now know the final answer
Final Answer: the final answer to the original input question`
}

// ParseWithRetry 带重试的解析
func (p *ReActOutputParser) ParseWithRetry(ctx context.Context, text string, maxRetries int) (*ReActOutput, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		result, err := p.Parse(ctx, text)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return nil, agentErrors.Wrap(lastErr, agentErrors.CodeParserFailed, "failed to parse after retries").
		WithComponent("react_parser").
		WithOperation("parse_with_retry").
		WithContext("max_retries", maxRetries)
}

// Validate 验证解析结果
func (p *ReActOutputParser) Validate(parsed *ReActOutput) error {
	if parsed == nil {
		return agentErrors.New(agentErrors.CodeParserFailed, "parsed output is nil").
			WithComponent("react_parser").
			WithOperation("validate")
	}

	// 检查是否有最终答案或行动
	if parsed.FinalAnswer != "" {
		return nil
	}

	if parsed.Action == "" {
		return agentErrors.New(agentErrors.CodeParserMissingField, "missing action in parsed output").
			WithComponent("react_parser").
			WithOperation("validate").
			WithContext("field", "action")
	}

	if parsed.ActionInput == nil {
		return agentErrors.New(agentErrors.CodeParserMissingField, "missing action_input in parsed output").
			WithComponent("react_parser").
			WithOperation("validate").
			WithContext("field", "action_input")
	}

	return nil
}
