package tools

import (
	"context"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/mcp/core"
)

// ReadFileTool 读取文件工具
type ReadFileTool struct {
	*core.BaseTool
}

// NewReadFileTool 创建读取文件工具
func NewReadFileTool() *ReadFileTool {
	schema := &core.ToolSchema{
		Type: "object",
		Properties: map[string]core.PropertySchema{
			"path": {
				Type:        "string",
				Description: "文件路径",
			},
			"encoding": {
				Type:        "string",
				Description: "文件编码（utf-8/ascii）",
				Default:     "utf-8",
				Enum:        []interface{}{"utf-8", "ascii"},
			},
		},
		Required: []string{"path"},
	}

	tool := &ReadFileTool{
		BaseTool: core.NewBaseTool(
			"read_file",
			"读取文件内容",
			"filesystem",
			schema,
		),
	}

	tool.SetRequiresAuth(false)
	tool.SetIsDangerous(false)

	return tool
}

// Execute 执行工具
func (t *ReadFileTool) Execute(ctx context.Context, input map[string]interface{}) (*core.ToolResult, error) {
	startTime := time.Now()

	path, ok := input["path"].(string)
	if !ok {
		return &core.ToolResult{
			Success:   false,
			Error:     "path must be a string",
			ErrorCode: "INVALID_INPUT",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, agentErrors.New(agentErrors.CodeInvalidInput, "invalid path type").
			WithComponent("read_file_tool").
			WithOperation("execute").
			WithContext("field", "path")
	}

	encoding, err := readEncoding(input)
	if err != nil {
		return &core.ToolResult{
			Success:   false,
			Error:     err.Error(),
			ErrorCode: "INVALID_INPUT",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}

	// 读取文件
	content, err := os.ReadFile(path)
	if err != nil {
		return &core.ToolResult{
			Success:   false,
			Error:     fmt.Sprintf("failed to read file: %v", err),
			ErrorCode: "FILE_READ_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}

	if !validEncoding(content, encoding) {
		err := fmt.Errorf("file is not valid %s: %s", encoding, path)
		return &core.ToolResult{
			Success:   false,
			Error:     err.Error(),
			ErrorCode: "ENCODING_ERROR",
			Duration:  time.Since(startTime),
			Timestamp: time.Now(),
		}, err
	}

	// 获取文件信息
	fileInfo, _ := os.Stat(path)

	result := &core.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"content": string(content),
			"size":    len(content),
			"path":    path,
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
func (t *ReadFileTool) Validate(input map[string]interface{}) error {
	path, ok := input["path"].(string)
	if !ok {
		return &core.ErrInvalidInput{Field: "path", Message: "must be a string"}
	}

	if path == "" {
		return &core.ErrInvalidInput{Field: "path", Message: "cannot be empty"}
	}

	_, err := readEncoding(input)
	return err
}

// readEncoding 解析 encoding 参数，缺省为 utf-8。
func readEncoding(input map[string]interface{}) (string, error) {
	value, exists := input["encoding"]
	if !exists || value == nil || value == "" {
		return "utf-8", nil
	}
	if encoding, _ := value.(string); encoding == "utf-8" || encoding == "ascii" {
		return encoding, nil
	}
	return "", &core.ErrInvalidInput{Field: "encoding", Message: "must be utf-8 or ascii"}
}

// validEncoding 校验内容。非法字节不能原样返回，转成字符串后会在 JSON 序列化时被静默替换。
func validEncoding(content []byte, encoding string) bool {
	if encoding == "ascii" {
		for _, b := range content {
			if b >= utf8.RuneSelf {
				return false
			}
		}
		return true
	}
	return utf8.Valid(content)
}
