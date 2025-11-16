package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kart-io/goagent/mcp/core"
)

// WriteFileTool 写入文件工具
type WriteFileTool struct {
	*core.BaseTool
}

// NewWriteFileTool 创建写入文件工具
func NewWriteFileTool() *WriteFileTool {
	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"path": {
				Type:        "string",
				Description: "文件路径",
			},
			"content": {
				Type:        "string",
				Description: "文件内容",
			},
			"mode": {
				Type:        "string",
				Description: "写入模式（overwrite/append）",
				Default:     "overwrite",
				Enum:        []interface{}{"overwrite", "append"},
			},
			"create_dirs": {
				Type:        "boolean",
				Description: "是否自动创建目录",
				Default:     false,
			},
		},
		Required: []string{"path", "content"},
	}

	tool := &WriteFileTool{
		BaseTool: core.NewBaseTool(
			"write_file",
			"写入文件内容",
			"filesystem",
			schema,
		),
	}

	tool.SetRequiresAuth(true)
	tool.SetIsDangerous(true) // 写文件是危险操作

	return tool
}

// Execute 执行工具
func (t *WriteFileTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
	startTime := time.Now()

	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	mode, _ := input["mode"].(string)
	if mode == "" {
		mode = "overwrite"
	}
	createDirs, _ := input["create_dirs"].(bool)

	// 如果需要创建目录
	if createDirs {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return &core.ToolResult{
				Success:   false,
				Error:     fmt.Sprintf("failed to create directories: %v", err),
				ErrorCode: "DIR_CREATE_ERROR",
				Duration:  time.Since(startTime),
				Timestamp: time.Now(),
			}, err
		}
	}

	// 写入文件
	var err error
	if mode == "append" {
		// 追加模式
		var f *os.File
		f, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return &core.ToolResult{
				Success:   false,
				Error:     fmt.Sprintf("failed to open file: %v", err),
				ErrorCode: "FILE_OPEN_ERROR",
				Duration:  time.Since(startTime),
				Timestamp: time.Now(),
			}, err
		}
		defer func() { _ = f.Close() }()

		_, err = f.WriteString(content)
	} else {
		// 覆盖模式
		err = os.WriteFile(path, []byte(content), 0o644)
	}

	if err != nil {
		return &core.ToolResult{
			Success:   false,
			Error:     fmt.Sprintf("failed to write file: %v", err),
			ErrorCode: "FILE_WRITE_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}

	// 获取文件信息
	fileInfo, _ := os.Stat(path)

	result := &core.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"path":          path,
			"bytes_written": len(content),
			"mode":          mode,
		},
		Metadata: map[string]interface{}{
			"file_size":     fileInfo.Size(),
			"modified_time": fileInfo.ModTime(),
		},
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
	}

	return result, nil
}

// Validate 验证输入
func (t *WriteFileTool) Validate(input map[string]interface{}) error {
	path, ok := input["path"].(string)
	if !ok || path == "" {
		return &core.ErrInvalidInput{Field: "path", Message: "must be a non-empty string"}
	}

	_, ok = input["content"].(string)
	if !ok {
		return &core.ErrInvalidInput{Field: "content", Message: "must be a string"}
	}

	return nil
}
