package coding

import (
	"context"
	"fmt"
	"strings"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// Todo 状态。
const (
	TodoPending    = "pending"
	TodoInProgress = "in_progress"
	TodoCompleted  = "completed"
)

// Todo 是模型用 todo_write 维护的一项任务。
type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// Todos 返回模型当前的任务清单副本，宿主可据此展示进度。
func (w *Workspace) Todos() []Todo {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Todo(nil), w.todos...)
}

func newTodoWriteTool(w *Workspace) interfaces.Tool {
	return tools.NewBaseTool(ToolTodoWrite,
		"维护本次任务的待办清单，每次传入完整清单替换旧清单。多步骤任务开始前先列出步骤，开始某步时标记为 in_progress（同一时间至多一项），完成后立即标记 completed。简单的单步任务不需要使用。",
		`{
			"type": "object",
			"properties": {
				"todos": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"content": {"type": "string", "description": "任务内容"},
							"status": {"type": "string", "enum": ["pending", "in_progress", "completed"]}
						},
						"required": ["content", "status"]
					}
				}
			},
			"required": ["todos"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolTodoWrite).WithOperation("write")
			raw, ok := input.Args["todos"].([]any)
			if !ok {
				return resp.ValidationError("todos must be an array", "field", "todos")
			}
			todos := make([]Todo, len(raw))
			inProgress, done := 0, 0
			for i, item := range raw {
				m, _ := item.(map[string]any)
				content := strings.TrimSpace(stringArg(m, "content"))
				status := stringArg(m, "status")
				if content == "" {
					return resp.ValidationError(fmt.Sprintf("todos[%d].content must be a non-empty string", i), "field", "todos")
				}
				switch status {
				case TodoInProgress:
					inProgress++
				case TodoCompleted:
					done++
				case TodoPending:
				default:
					return resp.ValidationError(fmt.Sprintf("todos[%d].status must be pending, in_progress or completed", i), "field", "todos")
				}
				todos[i] = Todo{Content: content, Status: status}
			}
			if inProgress > 1 {
				return resp.ValidationError("at most one todo can be in_progress", "field", "todos")
			}
			w.mu.Lock()
			w.todos = todos
			w.mu.Unlock()
			return resp.Success(map[string]any{"total": len(todos), "completed": done, "in_progress": inProgress}, nil)
		})
}
