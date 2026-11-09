// Package skill adapts a read-only skills catalog to the load_skill tool.
package skill

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"unicode/utf8"

	"github.com/kart-io/goagent/constants"
	agenterrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/skills"
	"github.com/kart-io/goagent/tools"
)

// New returns a tool for loading skill instructions and reading reference files.
// The caller owns catalog and must keep it open while the tool is in use.
// Metadata does not trigger filtering, authorization, prompting, or execution.
func New(catalog *skills.Catalog) interfaces.Tool {
	return tools.NewBaseTool(constants.ToolLoadSkill,
		"加载技能的完整说明。name 为技能名；说明中提到技能目录里的其他文件时，用 file 读取（相对技能目录的路径）。",
		`{
			"type": "object",
			"properties": {
				"name": {"type": "string", "description": "技能名"},
				"file": {"type": "string", "description": "技能目录中的文件，省略时返回 SKILL.md 的说明"}
			},
			"required": ["name"],
			"additionalProperties": false
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(constants.ToolLoadSkill).WithOperation("invoke")
			if err := ctx.Err(); err != nil {
				return resp.ExecutionError("skill loading canceled", nil, err)
			}
			if input == nil {
				return resp.ValidationError("input is required")
			}
			for field := range input.Args {
				if field != "name" && field != "file" {
					return resp.ValidationError("unknown field "+field, "field", field)
				}
			}
			name, ok := input.Args["name"].(string)
			if !ok {
				return resp.ValidationError("name is required and must be a string", "field", "name")
			}
			var file string
			if value, exists := input.Args["file"]; exists {
				file, ok = value.(string)
				if !ok {
					return resp.ValidationError("file must be a string", "field", "file")
				}
			}
			if catalog == nil {
				return resp.ExecutionError("skill catalog is nil", nil, nil)
			}

			if file == "" {
				loaded, err := catalog.Load(ctx, name)
				if err != nil {
					return loadError(ctx, catalog, resp, name, file, err)
				}
				return resp.Success(map[string]any{
					"name": loaded.Metadata.Name, "instructions": loaded.Body, "files": loaded.Files,
				}, nil)
			}
			data, err := catalog.ReadReference(ctx, name, file)
			if err != nil {
				return loadError(ctx, catalog, resp, name, file, err)
			}
			if !utf8.Valid(data) {
				return tools.Rejected(fmt.Sprintf("skill %s file %s is not UTF-8 text", name, file), nil), nil
			}
			return resp.Success(map[string]any{"name": name, "file": file, "content": string(data)}, nil)
		})
}

func loadError(ctx context.Context, catalog *skills.Catalog, resp *tools.ToolErrorResponse, name, file string, err error) (*interfaces.ToolOutput, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return resp.ExecutionError("skill loading canceled", nil, ctxErr)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, fs.ErrClosed) {
		return resp.ExecutionError("skill loading failed", nil, err)
	}
	if !expectedReferenceError(err) {
		return resp.ExecutionError("skill loading failed", nil, err)
	}
	// Missing skills and missing files both wrap fs.ErrNotExist. The catalog's
	// immutable metadata snapshot distinguishes them without another file read.
	if errors.Is(err, fs.ErrNotExist) && !slices.ContainsFunc(catalog.List(), func(m skills.Metadata) bool { return m.Name == name }) {
		return tools.Rejected("unknown skill "+name, nil), nil
	}
	if file != "" && errors.Is(err, fs.ErrNotExist) {
		return tools.Rejected(fmt.Sprintf("skill %s has no file %s", name, file), nil), nil
	}
	// 正文加载也会枚举资源；已知目录限制应反馈给模型，而不是消耗持久重试。
	// 上面的完整错误树检查确保限制错误不会掩盖同时发生的 I/O 故障。
	if file == "" && agenterrors.IsCode(err, agenterrors.CodeInvalidInput) {
		return tools.Rejected(fmt.Sprintf("无法加载技能 %s：%v", name, err), nil), nil
	}
	if file != "" && (errors.Is(err, fs.ErrInvalid) || errors.Is(err, fs.ErrPermission) ||
		agenterrors.IsCode(err, agenterrors.CodeInvalidInput) || agenterrors.IsCode(err, agenterrors.CodeAccessDenied)) {
		return tools.Rejected(fmt.Sprintf("cannot read skill %s file %s: %v", name, file, err), nil), nil
	}
	// Unknown I/O failures are not completed business rejections. Preserve their
	// identity for the host, including opaque os.Root boundary errors: the stdlib
	// does not export a sentinel for those, so do not classify by error text.
	return resp.ExecutionError("skill loading failed", nil, err)
}

// A read/check error can be joined with a close failure. A known request error
// in one branch must not hide unknown I/O in another, or an incomplete read
// would be persisted as a completed rejection. Inspect every cause first.
func expectedReferenceError(err error) bool {
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		found := false
		for _, cause := range wrapped.Unwrap() {
			if cause == nil {
				continue
			}
			found = true
			if !expectedReferenceError(cause) {
				return false
			}
		}
		return found
	case interface{ Unwrap() error }:
		if cause := wrapped.Unwrap(); cause != nil {
			return expectedReferenceError(cause)
		}
	}
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) || errors.Is(err, fs.ErrPermission) ||
		agenterrors.IsCode(err, agenterrors.CodeInvalidInput) || agenterrors.IsCode(err, agenterrors.CodeAccessDenied)
}
