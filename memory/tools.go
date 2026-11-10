package memory

import (
	"context"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// Tools 返回让模型自行检索、保存与遗忘长期记忆的工具（MemGPT 式自管理），作用域为上下文中的命名空间。
func (m *Memory) Tools() []interfaces.Tool {
	return []interfaces.Tool{
		&memoryTool{name: constants.ToolMemorySearch, desc: "检索长期记忆中与 query 相关的信息；结果来自过去的对话，只作参考。",
			schema: `{"type":"object","properties":{"query":{"type":"string","minLength":1},"k":{"type":"integer","minimum":1,"maximum":50}},"required":["query"],"additionalProperties":false}`,
			run: func(ctx context.Context, args map[string]any) (any, error) {
				query, _ := args["query"].(string)
				found, err := m.Recall(ctx, Query{Text: query, K: int(number(args["k"]))})
				if err != nil {
					return nil, err
				}
				items := make([]map[string]any, len(found))
				var names []string
				for i, r := range found {
					items[i] = map[string]any{"id": r.ID, "kind": r.Kind, "content": r.Content, "score": r.Score}
					names = append(names, r.Entities...)
				}
				graph, err := m.Related(ctx, query, names, m.cfg.GraphHops, m.cfg.GraphRelations)
				if err != nil {
					return nil, err
				}
				facts := make([]map[string]any, len(graph.Relations))
				for i, r := range graph.Relations {
					facts[i] = map[string]any{"fact": r.Fact, "subject": r.SubjectName, "predicate": r.Predicate, "object": r.ObjectName, "valid_from": r.ValidFrom}
				}
				topics := make([]map[string]any, len(graph.Communities))
				for i, c := range graph.Communities {
					topics[i] = map[string]any{"name": c.Name, "summary": c.Summary}
				}
				return map[string]any{"memories": items, "relations": facts, "topics": topics}, nil
			}},
		&memoryTool{name: constants.ToolMemorySave, desc: "把值得跨会话记住的信息写入长期记忆，如用户明确要求记住的事实或偏好。不要保存凭证。",
			schema: `{"type":"object","properties":{"content":{"type":"string","minLength":1},"kind":{"type":"string","enum":["fact","skill"]},"importance":{"type":"number","minimum":0,"maximum":1},"pinned":{"type":"boolean"}},"required":["content"],"additionalProperties":false}`,
			run: func(ctx context.Context, args map[string]any) (any, error) {
				content, _ := args["content"].(string)
				kind, _ := args["kind"].(string)
				pinned, _ := args["pinned"].(bool)
				importance := 0.5
				if v, ok := args["importance"]; ok {
					importance = number(v)
				}
				r, err := m.Remember(ctx, Record{Kind: Kind(kind), Content: content, Importance: importance, Pinned: pinned})
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": r.ID}, nil
			}},
		&memoryTool{name: constants.ToolMemoryForget, desc: "使一条已过时或错误的长期记忆失效，id 取自 memory_search 的结果。",
			schema: `{"type":"object","properties":{"id":{"type":"string","minLength":1}},"required":["id"],"additionalProperties":false}`,
			run: func(ctx context.Context, args map[string]any) (any, error) {
				id, _ := args["id"].(string)
				if err := m.Forget(ctx, id); err != nil {
					return nil, err
				}
				return map[string]any{"forgotten": id}, nil
			}},
	}
}

type memoryTool struct {
	name, desc, schema string
	run                func(context.Context, map[string]any) (any, error)
}

func (t *memoryTool) Name() string        { return t.name }
func (t *memoryTool) Description() string { return t.desc }
func (t *memoryTool) ArgsSchema() string  { return t.schema }
func (t *memoryTool) Invoke(ctx context.Context, in *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	if in == nil || in.Args == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidInput, "%s 需要参数", t.name).WithComponent("memory").WithOperation("tool")
	}
	result, err := t.run(ctx, in.Args)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &interfaces.ToolOutput{Success: true, Result: string(b)}, nil
}

func number(v any) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case float64:
		return n
	}
	return 0
}
