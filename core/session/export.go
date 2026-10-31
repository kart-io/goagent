package session

import (
	"encoding/json"
	"errors"
	"io"
	"sort"

	"github.com/kart-io/goagent/llm"
)

// Trajectory 是从会话日志导出的一条训练轨迹：模型最终看到的对话（压缩与退回之后）、最近的 system 提示词与工具定义，
// 以及工具调用统计。
type Trajectory struct {
	System   string
	Tools    []ToolDefinition
	Messages []llm.Message
	Turns    int
	Stats    []ToolStat
}

// ToolStat 是一个工具的调用统计。Failures 是结果不成功（拒绝、参数错误、执行失败）的次数。
type ToolStat struct {
	Name     string `json:"name"`
	Calls    int    `json:"calls"`
	Failures int    `json:"failures"`
}

// BuildTrajectory 从事件构造轨迹。
func BuildTrajectory(events []Event) (*Trajectory, error) {
	entries, err := Derive(events)
	if err != nil {
		return nil, err
	}
	t := &Trajectory{}
	for _, ev := range events {
		switch ev.Type {
		case TypeSystemMessage:
			var d MessageData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				return nil, err
			}
			t.System = d.Message.Content
		case TypeToolDefinitions:
			var d ToolsData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				return nil, err
			}
			t.Tools = d.Tools
		case TypeTurnEnd:
			var d TurnEnd
			if json.Unmarshal(ev.Data, &d) == nil && !d.Aborted {
				t.Turns++
			}
		}
	}
	stats := map[string]*ToolStat{}
	for _, e := range entries {
		t.Messages = append(t.Messages, e.Message)
		if e.Tool == nil {
			continue
		}
		st, ok := stats[e.Tool.Name]
		if !ok {
			st = &ToolStat{Name: e.Tool.Name}
			stats[e.Tool.Name] = st
		}
		st.Calls++
		if !e.Tool.Success {
			st.Failures++
		}
	}
	for _, st := range stats {
		t.Stats = append(t.Stats, *st)
	}
	sort.Slice(t.Stats, func(i, j int) bool { return t.Stats[i].Name < t.Stats[j].Name })
	return t, nil
}

type openAIMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function ToolDefinition `json:"function"`
}

// WriteOpenAI 以 OpenAI 微调格式写出一行 JSON：{"messages": [...], "tools": [...]}。
func (t *Trajectory) WriteOpenAI(w io.Writer) error {
	var line struct {
		Messages []openAIMessage `json:"messages"`
		Tools    []openAITool    `json:"tools,omitempty"`
	}
	if t.System != "" {
		line.Messages = append(line.Messages, openAIMessage{Role: "system", Content: t.System})
	}
	for _, m := range t.Messages {
		line.Messages = append(line.Messages, openAIMessage{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID})
	}
	for _, d := range t.Tools {
		line.Tools = append(line.Tools, openAITool{Type: "function", Function: d})
	}
	return writeLine(w, line)
}

// ShareGPT 中的一条对话。
type shareGPTTurn struct {
	From  string `json:"from"`
	Value string `json:"value"`
}

// 用户侧的角色；ShareGPT 要求用户侧与模型侧交替出现。
func userSide(from string) bool { return from == "human" || from == "observation" }

// WriteShareGPT 以 ShareGPT 格式（LLaMA-Factory 的 sharegpt 数据格式）写出一行 JSON：
// conversations 中 from 为 human、gpt、function_call、observation，tools 为工具定义的 JSON 字符串。
// 同一回合的多个工具调用或结果合并为 JSON 数组。为满足交替要求，连续的用户侧消息合并为一条：
// 工具结果之后紧接的用户消息（运行中追加的输入）附在 observation 之后。
func (t *Trajectory) WriteShareGPT(w io.Writer) error {
	var turns []shareGPTTurn
	add := func(from, value string) {
		if n := len(turns); n > 0 {
			last := &turns[n-1]
			if last.From == from && from != "function_call" && from != "observation" || userSide(last.From) && userSide(from) && last.From != from {
				last.Value += "\n\n" + value
				return
			}
		}
		turns = append(turns, shareGPTTurn{From: from, Value: value})
	}
	var pending []json.RawMessage
	flush := func() {
		if len(pending) == 0 {
			return
		}
		add("observation", jsonValue(pending))
		pending = nil
	}
	for _, m := range t.Messages {
		if m.Role != "tool" {
			flush()
		}
		switch m.Role {
		case "user":
			add("human", m.Content)
		case "assistant":
			if len(m.ToolCalls) == 0 {
				add("gpt", m.Content)
				continue
			}
			var calls []json.RawMessage
			for _, tc := range m.ToolCalls {
				args := json.RawMessage(tc.Function.Arguments)
				if !json.Valid(args) {
					args, _ = json.Marshal(tc.Function.Arguments)
				}
				c, _ := json.Marshal(struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{tc.Function.Name, args})
				calls = append(calls, c)
			}
			add("function_call", jsonValue(calls))
		case "tool":
			c, _ := json.Marshal(m.Content)
			pending = append(pending, c)
		}
	}
	flush()
	if len(turns) == 0 || turns[0].From != "human" {
		return errors.New("轨迹须以用户消息开始")
	}
	line := struct {
		Conversations []shareGPTTurn `json:"conversations"`
		System        string         `json:"system,omitempty"`
		Tools         string         `json:"tools,omitempty"`
	}{Conversations: turns, System: t.System}
	if len(t.Tools) > 0 {
		data, err := json.Marshal(t.Tools)
		if err != nil {
			return err
		}
		line.Tools = string(data)
	}
	return writeLine(w, line)
}

// jsonValue 把单个元素写为该元素，多个写为数组。
func jsonValue(items []json.RawMessage) string {
	if len(items) == 1 {
		var s string
		if json.Unmarshal(items[0], &s) == nil {
			return s
		}
		return string(items[0])
	}
	data, _ := json.Marshal(items)
	return string(data)
}

func writeLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
