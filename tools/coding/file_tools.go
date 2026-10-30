package coding

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// 工具名称，供策略规则按名称匹配。
const (
	ToolReadFile   = "read_file"
	ToolWriteFile  = "write_file"
	ToolEditFile   = "edit_file"
	ToolMultiEdit  = "multi_edit"
	ToolTodoWrite  = "todo_write"
	ToolGlob       = "glob"
	ToolGrep       = "grep"
	ToolRunCommand = "run_command"
	ToolGitStatus  = "git_status"
	ToolGitDiff    = "git_diff"
	ToolGitLog     = "git_log"
	ToolGitCommit  = "git_commit"
)

const (
	defaultReadLimit = 2000
	maxLineRunes     = 2000
)

// fileError 把文件错误分类：模型能据此修正的状态交给模型，其他错误中止调用。
func fileError(resp *tools.ToolErrorResponse, rel string, err error) (*interfaces.ToolOutput, error) {
	switch {
	case errors.Is(err, env.ErrOutsideWorkspace):
		return resp.ValidationError(err.Error(), "field", "path")
	case errors.Is(err, fs.ErrNotExist):
		return tools.Rejected(rel+" does not exist", nil), nil
	case errors.Is(err, fs.ErrPermission):
		return tools.Rejected("permission denied: "+rel, nil), nil
	case errors.Is(err, errNotText):
		return tools.Rejected(rel+": "+err.Error(), nil), nil
	case errors.Is(err, env.ErrNotObserved):
		return tools.Rejected(rel+" has not been read yet; read it with read_file before modifying it", nil), nil
	case errors.Is(err, env.ErrStale), errors.Is(err, env.ErrExists):
		return tools.Rejected(rel+" changed since it was last read; read it again before modifying it", nil), nil
	}
	return resp.ExecutionError("failed to access "+rel, nil, err)
}

func newReadFileTool(w *Workspace) interfaces.Tool {
	return tools.NewBaseTool(ToolReadFile,
		"读取工作区内的文本文件，返回带行号的内容（格式为 行号<TAB>内容）。修改已存在的文件前必须先用本工具读取。大文件用 offset 与 limit 分段读取。",
		`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "相对工作区根目录的文件路径"},
				"offset": {"type": "integer", "minimum": 1, "description": "起始行号，从 1 开始，默认 1"},
				"limit": {"type": "integer", "minimum": 1, "description": "最多读取的行数，默认 2000"}
			},
			"required": ["path"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolReadFile).WithOperation("read")
			rel, err := w.fs.Resolve(stringArg(input.Args, "path"))
			if err != nil {
				return resp.ValidationError(err.Error(), "field", "path")
			}
			offset, err := intArg(input.Args, "offset", 1)
			if err != nil || offset < 1 {
				return resp.ValidationError("offset must be an integer >= 1", "field", "offset")
			}
			limit, err := intArg(input.Args, "limit", defaultReadLimit)
			if err != nil || limit < 1 {
				return resp.ValidationError("limit must be an integer >= 1", "field", "limit")
			}
			data, err := w.readText(ctx, rel, true)
			if err != nil {
				return fileError(resp, rel, err)
			}

			lines := splitLines(string(data))
			if len(lines) > 0 && offset > len(lines) {
				return tools.Rejected(fmt.Sprintf("offset %d exceeds total lines %d of %s", offset, len(lines), rel), nil), nil
			}
			end := min(len(lines), offset-1+limit)
			var sb strings.Builder
			for i := offset - 1; i < end; i++ {
				line := lines[i]
				if r := []rune(line); len(r) > maxLineRunes {
					line = string(r[:maxLineRunes]) + "…"
				}
				fmt.Fprintf(&sb, "%6d\t%s\n", i+1, line)
			}
			return resp.Success(map[string]any{
				"path":        rel,
				"content":     sb.String(),
				"start_line":  offset,
				"end_line":    end,
				"total_lines": len(lines),
			}, nil)
		})
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// resolveWritable 解析写入类工具的 path 参数，失败时返回交给模型的原因。
func (w *Workspace) resolveWritable(args map[string]any) (rel, reason string) {
	rel, err := w.fs.Resolve(stringArg(args, "path"))
	if err != nil {
		return "", err.Error()
	}
	if inGitDir(rel) {
		return "", "modifying files inside .git is not allowed; use the git tools"
	}
	return rel, ""
}

// write 写入并按需运行 linter；检查失败时恢复写入前的内容（previous 为 nil 表示原先不存在）并拒绝。
func (w *Workspace) write(ctx context.Context, resp *tools.ToolErrorResponse, rel string, content, previous []byte, result map[string]any) (*interfaces.ToolOutput, error) {
	if _, err := w.fs.WriteFile(ctx, rel, content, env.WriteIntent{}); err != nil {
		return fileError(resp, rel, err)
	}
	argv := []string(nil)
	if w.linter != nil {
		argv = w.linter(rel)
	}
	if len(argv) == 0 {
		return resp.Success(result, nil)
	}
	r, err := w.execute(ctx, argv, ".")
	if err == nil && r.exitCode == 0 && !r.timedOut {
		return resp.Success(result, nil)
	}
	// 撤销绕过先读后改：linter 可能已改过文件，按版本条件写回会失败。
	var undo error
	if previous == nil {
		if undo = w.cp.Remove(ctx, rel); errors.Is(undo, fs.ErrNotExist) {
			undo = nil
		}
		w.fs.Forget(rel)
	} else if _, undo = w.cp.WriteFile(ctx, rel, previous, env.WriteIntent{}); undo == nil {
		// 恢复后的内容就是模型读过的内容，重新记录版本，模型可直接继续编辑。
		_, _, undo = w.fs.ReadFile(ctx, rel)
	}
	if undo != nil {
		return resp.ExecutionError("failed to undo an edit rejected by the linter", nil, undo)
	}
	if err != nil {
		return tools.Rejected(fmt.Sprintf("the edit to %s was undone because the linter failed to run: %v", rel, err), nil), nil
	}
	return tools.Rejected(fmt.Sprintf("the edit to %s was undone because the linter failed (exit code %d):\n%s", rel, r.exitCode, strings.TrimSpace(r.output)), nil), nil
}

func newWriteFileTool(w *Workspace) interfaces.Tool {
	return w.commandTool(ToolWriteFile,
		"在工作区内创建文件或整体覆盖文件，自动创建父目录。覆盖已存在的文件前必须先用 read_file 读取；只改局部时优先用 edit_file。",
		`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "相对工作区根目录的文件路径"},
				"content": {"type": "string", "description": "文件的完整内容"}
			},
			"required": ["path", "content"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolWriteFile).WithOperation("write")
			rel, reason := w.resolveWritable(input.Args)
			if reason != "" {
				return resp.ValidationError(reason, "field", "path")
			}
			content, ok := input.Args["content"].(string)
			if !ok {
				return resp.ValidationError("content is required and must be a string", "field", "content")
			}
			w.editMu.Lock()
			defer w.editMu.Unlock()
			previous, err := w.readText(ctx, rel, false)
			created := errors.Is(err, fs.ErrNotExist)
			if err != nil && !created {
				return fileError(resp, rel, err)
			}
			return w.write(ctx, resp, rel, []byte(content), previous, map[string]any{"path": rel, "bytes": len(content), "created": created})
		})
}

type edit struct {
	oldStr, newStr string
	all            bool
}

// parseEdit 校验一处编辑的参数，失败时返回交给模型的原因。
func parseEdit(m map[string]any) (edit, string) {
	oldStr, ok1 := m["old_string"].(string)
	newStr, ok2 := m["new_string"].(string)
	if !ok1 || !ok2 || oldStr == "" {
		return edit{}, "old_string must be a non-empty string and new_string must be a string"
	}
	if oldStr == newStr {
		return edit{}, "new_string must differ from old_string"
	}
	return edit{oldStr: oldStr, newStr: newStr, all: boolArg(m, "replace_all")}, ""
}

// applyEdit 在 content 上应用一处编辑，返回替换次数；无法应用时返回交给模型的原因。
func applyEdit(content, rel string, e edit) (string, int, string) {
	count := strings.Count(content, e.oldStr)
	switch {
	case count == 0:
		return "", 0, "old_string not found in " + rel
	case count > 1 && !e.all:
		return "", 0, fmt.Sprintf("old_string occurs %d times in %s; add surrounding context to make it unique or set replace_all", count, rel)
	case e.all:
		return strings.ReplaceAll(content, e.oldStr, e.newStr), count, ""
	}
	return strings.Replace(content, e.oldStr, e.newStr, 1), 1, ""
}

// modifyFile 读取已存在文件的当前内容，用 change 计算新内容后写回；写入以读过的版本为条件，
// 未读或读后被外部改动时拒绝。change 返回新内容、结果和拒绝原因；拒绝时文件不变。
func (w *Workspace) modifyFile(ctx context.Context, resp *tools.ToolErrorResponse, rel string, change func(string) (string, map[string]any, string)) (*interfaces.ToolOutput, error) {
	w.editMu.Lock()
	defer w.editMu.Unlock()
	data, err := w.readText(ctx, rel, false)
	if err != nil {
		return fileError(resp, rel, err)
	}
	content, result, reason := change(string(data))
	if reason != "" {
		return tools.Rejected(reason, nil), nil
	}
	return w.write(ctx, resp, rel, []byte(content), data, result)
}

func newEditFileTool(w *Workspace) interfaces.Tool {
	return w.commandTool(ToolEditFile,
		"精确替换文件中的文本片段。old_string 必须与文件内容逐字一致（含缩进），且在文件中唯一；需要替换全部出现时设置 replace_all。编辑前必须先用 read_file 读取文件，old_string 不要包含行号前缀。同一文件的多处修改用 multi_edit。",
		`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "相对工作区根目录的文件路径"},
				"old_string": {"type": "string", "description": "要被替换的原文"},
				"new_string": {"type": "string", "description": "替换后的文本，必须与 old_string 不同"},
				"replace_all": {"type": "boolean", "description": "是否替换全部出现，默认 false"}
			},
			"required": ["path", "old_string", "new_string"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolEditFile).WithOperation("edit")
			rel, reason := w.resolveWritable(input.Args)
			if reason != "" {
				return resp.ValidationError(reason, "field", "path")
			}
			e, reason := parseEdit(input.Args)
			if reason != "" {
				return resp.ValidationError(reason)
			}
			return w.modifyFile(ctx, resp, rel, func(content string) (string, map[string]any, string) {
				content, count, reason := applyEdit(content, rel, e)
				return content, map[string]any{"path": rel, "replacements": count}, reason
			})
		})
}

func newMultiEditTool(w *Workspace) interfaces.Tool {
	return w.commandTool(ToolMultiEdit,
		"对同一文件按顺序应用多处精确替换，全部成功才写回，任一处失败则文件不变。每处编辑的规则与 edit_file 相同，后一处作用于前一处的结果。编辑前必须先用 read_file 读取文件。",
		`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "相对工作区根目录的文件路径"},
				"edits": {
					"type": "array",
					"minItems": 1,
					"items": {
						"type": "object",
						"properties": {
							"old_string": {"type": "string"},
							"new_string": {"type": "string"},
							"replace_all": {"type": "boolean"}
						},
						"required": ["old_string", "new_string"]
					}
				}
			},
			"required": ["path", "edits"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolMultiEdit).WithOperation("multi_edit")
			rel, reason := w.resolveWritable(input.Args)
			if reason != "" {
				return resp.ValidationError(reason, "field", "path")
			}
			raw, _ := input.Args["edits"].([]any)
			if len(raw) == 0 {
				return resp.ValidationError("edits must be a non-empty array", "field", "edits")
			}
			edits := make([]edit, len(raw))
			for i, item := range raw {
				m, _ := item.(map[string]any)
				e, reason := parseEdit(m)
				if reason != "" {
					return resp.ValidationError(fmt.Sprintf("edits[%d]: %s", i, reason), "field", "edits")
				}
				edits[i] = e
			}
			return w.modifyFile(ctx, resp, rel, func(content string) (string, map[string]any, string) {
				total := 0
				for i, e := range edits {
					next, count, reason := applyEdit(content, rel, e)
					if reason != "" {
						return "", nil, fmt.Sprintf("edits[%d]: %s; no edits were applied", i, reason)
					}
					content, total = next, total+count
				}
				return content, map[string]any{"path": rel, "edits": len(edits), "replacements": total}, ""
			})
		})
}
