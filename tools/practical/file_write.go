package practical

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils"
	"github.com/kart-io/goagent/utils/json"
)

// FileWriteTool 处理文件写入相关操作
type FileWriteTool struct {
	config *FileToolConfig
}

// NewFileWriteTool 创建文件写入工具
func NewFileWriteTool(config *FileToolConfig) *FileWriteTool {
	if config == nil {
		config = DefaultFileToolConfig()
	}
	return &FileWriteTool{
		config: config,
	}
}

// Name 返回工具名称
func (t *FileWriteTool) Name() string {
	return "file_write"
}

// Description 返回工具描述
func (t *FileWriteTool) Description() string {
	return "Write content to files or append to existing files"
}

// ArgsSchema 返回参数 JSON Schema
func (t *FileWriteTool) ArgsSchema() string {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"write", "append"},
				"description": "Write operation: write (overwrite/create), append (add to end)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File path to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write/append",
			},
			"options": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"encoding": map[string]interface{}{
						"type":        "string",
						"default":     "utf-8",
						"description": "File encoding",
					},
					"permissions": map[string]interface{}{
						"type":        "string",
						"description": "File permissions (e.g., '0644')",
					},
				},
			},
		},
		"required": []string{"operation", "path", "content"},
	}

	schemaJSON, _ := json.Marshal(schema)
	return string(schemaJSON)
}

// Execute 执行文件写入操作
func (t *FileWriteTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	params, err := t.parseInput(input.Args)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid input").
			WithComponent("file_write_tool").
			WithOperation("execute")
	}

	// 验证路径安全性
	if err := validatePath(t.config, params.Path); err != nil {
		return &interfaces.ToolOutput{
			Result: map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			},
			Error: err.Error(),
		}, err
	}

	// 根据操作类型执行
	var result interface{}
	switch params.Operation {
	case "write":
		result, err = t.writeFile(ctx, params)
	case "append":
		result, err = t.appendFile(ctx, params)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported operation").
			WithComponent("file_write_tool").
			WithOperation("execute").
			WithContext("operation", params.Operation)
	}

	if err != nil {
		return nil, err
	}

	return &interfaces.ToolOutput{
		Result: result,
	}, nil
}

// Invoke 实现 Runnable 接口
func (t *FileWriteTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return t.Execute(ctx, input)
}

// Stream 实现 Runnable 接口
func (t *FileWriteTool) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan agentcore.StreamChunk[*interfaces.ToolOutput], error) {
	ch := make(chan agentcore.StreamChunk[*interfaces.ToolOutput])
	go func() {
		defer close(ch)
		output, err := t.Execute(ctx, input)
		if err != nil {
			ch <- agentcore.StreamChunk[*interfaces.ToolOutput]{Error: err}
		} else {
			ch <- agentcore.StreamChunk[*interfaces.ToolOutput]{Data: output}
		}
	}()
	return ch, nil
}

// Batch 实现 Runnable 接口
func (t *FileWriteTool) Batch(ctx context.Context, inputs []*interfaces.ToolInput) ([]*interfaces.ToolOutput, error) {
	outputs := make([]*interfaces.ToolOutput, len(inputs))
	for i, input := range inputs {
		output, err := t.Execute(ctx, input)
		if err != nil {
			return nil, err
		}
		outputs[i] = output
	}
	return outputs, nil
}

// Pipe 实现 Runnable 接口
func (t *FileWriteTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	return nil
}

// WithCallbacks 实现 Runnable 接口
func (t *FileWriteTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// WithConfig 实现 Runnable 接口
func (t *FileWriteTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// writeFile 写入文件内容
func (t *FileWriteTool) writeFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// 确保目录存在
	dir := filepath.Dir(params.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	// 设置权限
	perm := os.FileMode(0o644)
	if params.Options.Permissions != "" {
		// 解析权限
		var p uint64
		if _, err := fmt.Sscanf(params.Options.Permissions, "%o", &p); err != nil {
			return nil, err
		}
		// 验证权限防止溢出
		if p > 0xFFFFFFFF { // Max uint32 for FileMode
			p = 0xFFFFFFFF
		}
		perm = os.FileMode(p)
	}

	// 写入文件
	err := os.WriteFile(params.Path, []byte(params.Content), perm)
	if err != nil {
		return nil, err
	}

	// 获取文件信息
	info, _ := os.Stat(params.Path)

	return map[string]interface{}{
		"success": true,
		"result":  "File written successfully",
		"info": map[string]interface{}{
			"size":     len(params.Content),
			"path":     params.Path,
			"checksum": calculateChecksum([]byte(params.Content)),
			"modified": info.ModTime().Format(time.RFC3339),
		},
	}, nil
}

// appendFile 追加内容到文件
func (t *FileWriteTool) appendFile(ctx context.Context, params *fileParams) (interface{}, error) {
	file, err := os.OpenFile(params.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(file)

	_, err = file.WriteString(params.Content)
	if err != nil {
		return nil, err
	}

	info, _ := os.Stat(params.Path)

	return map[string]interface{}{
		"success": true,
		"result":  "Content appended successfully",
		"info": map[string]interface{}{
			"size":     info.Size(),
			"modified": info.ModTime().Format(time.RFC3339),
		},
	}, nil
}

// parseInput 解析输入参数
func (t *FileWriteTool) parseInput(input interface{}) (*fileParams, error) {
	var params fileParams

	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &params); err != nil {
		return nil, err
	}

	// 设置默认值
	if params.Options.Encoding == "" {
		params.Options.Encoding = "utf-8"
	}

	return &params, nil
}
