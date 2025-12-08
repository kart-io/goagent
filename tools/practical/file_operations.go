package practical

import (
	"context"
	"fmt"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/utils/json"
)

// FileOperationsTool 保留用于向后兼容，内部委托给专门的工具
// 已废弃: 请直接使用 FileReadTool, FileWriteTool, FileManagementTool, FileCompressionTool, FileWatchTool
// Deprecated: Use specific tools (FileReadTool, FileWriteTool, FileManagementTool, FileCompressionTool, FileWatchTool) instead
type FileOperationsTool struct {
	basePath       string
	maxFileSize    int64
	allowedPaths   []string
	forbiddenPaths []string

	// 内部委托的专门工具
	readTool        *FileReadTool
	writeTool       *FileWriteTool
	managementTool  *FileManagementTool
	compressionTool *FileCompressionTool
	watchTool       *FileWatchTool
}

// NewFileOperationsTool 创建文件操作工具（向后兼容）
// Deprecated: Use NewFileReadTool, NewFileWriteTool, etc. instead
func NewFileOperationsTool(basePath string) *FileOperationsTool {
	config := &FileToolConfig{
		BasePath:    basePath,
		MaxFileSize: 100 * 1024 * 1024, // 100MB
		AllowedPaths: []string{
			"/tmp",
			"/var/tmp",
		},
		ForbiddenPaths: []string{
			"/etc",
			"/sys",
			"/proc",
		},
	}

	return &FileOperationsTool{
		basePath:        basePath,
		maxFileSize:     config.MaxFileSize,
		allowedPaths:    config.AllowedPaths,
		forbiddenPaths:  config.ForbiddenPaths,
		readTool:        NewFileReadTool(config),
		writeTool:       NewFileWriteTool(config),
		managementTool:  NewFileManagementTool(config),
		compressionTool: NewFileCompressionTool(config),
		watchTool:       NewFileWatchTool(config),
	}
}

// Name 返回工具名称
func (t *FileOperationsTool) Name() string {
	return "file_operations"
}

// Description 返回工具描述
func (t *FileOperationsTool) Description() string {
	return "Performs file system operations including read, write, search, compress, and analyze files (DEPRECATED: use specific file tools instead)"
}

// ArgsSchema 返回参数 JSON Schema
func (t *FileOperationsTool) ArgsSchema() string {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type": "string",
				"enum": []string{
					"read", "write", "append", "delete", "copy", "move",
					"list", "search", "info", "compress", "decompress",
					"parse", "analyze", "watch",
				},
				"description": "File operation to perform",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File or directory path",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content for write/append operations",
			},
			"destination": map[string]interface{}{
				"type":        "string",
				"description": "Destination path for copy/move operations",
			},
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Search pattern (glob or regex)",
			},
			"options": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"encoding": map[string]interface{}{
						"type":        "string",
						"default":     "utf-8",
						"description": "File encoding",
					},
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"default":     false,
						"description": "Recursive operation for directories",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"json", "yaml", "csv", "xml", "text"},
						"description": "File format for parsing",
					},
					"compression": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"gzip", "zip"},
						"description": "Compression format",
					},
					"lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to read (for large files)",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Byte offset to start reading from",
					},
					"follow": map[string]interface{}{
						"type":        "boolean",
						"default":     false,
						"description": "Follow file changes (tail -f behavior)",
					},
					"permissions": map[string]interface{}{
						"type":        "string",
						"description": "File permissions (e.g., '0644')",
					},
				},
			},
		},
		"required": []string{"operation", "path"},
	}

	schemaJSON, _ := json.Marshal(schema)
	return string(schemaJSON)
}

// Execute 执行文件操作（委托给专门工具）
func (t *FileOperationsTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	// 解析操作参数
	params, err := t.parseFileInput(input.Args)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid input").
			WithComponent("file_operations_tool").
			WithOperation("execute")
	}

	// 根据操作委托给相应的专门工具
	switch params.Operation {
	case "read", "parse", "info", "analyze":
		return t.readTool.Execute(ctx, input)
	case "write", "append":
		return t.writeTool.Execute(ctx, input)
	case "delete", "copy", "move", "list", "search":
		return t.managementTool.Execute(ctx, input)
	case "compress", "decompress":
		return t.compressionTool.Execute(ctx, input)
	case "watch":
		return t.watchTool.Execute(ctx, input)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported operation").
			WithComponent("file_operations_tool").
			WithOperation("execute").
			WithContext("operation", params.Operation)
	}
}

// Invoke 实现 Runnable 接口
func (t *FileOperationsTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return t.Execute(ctx, input)
}

// Stream 实现 Runnable 接口
func (t *FileOperationsTool) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan agentcore.StreamChunk[*interfaces.ToolOutput], error) {
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
func (t *FileOperationsTool) Batch(ctx context.Context, inputs []*interfaces.ToolInput) ([]*interfaces.ToolOutput, error) {
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
func (t *FileOperationsTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	return nil
}

// WithCallbacks 实现 Runnable 接口
func (t *FileOperationsTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// WithConfig 实现 Runnable 接口
func (t *FileOperationsTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// parseFileInput 解析输入参数
func (t *FileOperationsTool) parseFileInput(input interface{}) (*fileParams, error) {
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

// validatePath 验证路径（保持向后兼容）
func (t *FileOperationsTool) validatePath(path string) error {
	config := &FileToolConfig{
		BasePath:       t.basePath,
		MaxFileSize:    t.maxFileSize,
		AllowedPaths:   t.allowedPaths,
		ForbiddenPaths: t.forbiddenPaths,
	}
	return validatePath(config, path)
}

// FileOperationsRuntimeTool 扩展 FileOperationsTool 以支持运行时
type FileOperationsRuntimeTool struct {
	*FileOperationsTool
}

// NewFileOperationsRuntimeTool 创建支持运行时的文件操作工具
func NewFileOperationsRuntimeTool(basePath string) *FileOperationsRuntimeTool {
	return &FileOperationsRuntimeTool{
		FileOperationsTool: NewFileOperationsTool(basePath),
	}
}

// ExecuteWithRuntime 使用运行时支持执行
func (t *FileOperationsRuntimeTool) ExecuteWithRuntime(ctx context.Context, input *interfaces.ToolInput, runtime *tools.ToolRuntime) (*interfaces.ToolOutput, error) {
	// 流式输出状态
	if runtime != nil && runtime.StreamWriter != nil {
		if err := runtime.StreamWriter(map[string]interface{}{
			"status": "performing_file_operation",
			"tool":   t.Name(),
		}); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to stream status").
				WithComponent("file_operations_tool").
				WithOperation("executeWithRuntime")
		}
	}

	// 执行操作
	result, err := t.Execute(ctx, input)

	// 将文件操作存储到运行时进行审计
	if runtime != nil {
		params, _ := t.parseFileInput(input.Args)
		if params != nil {
			// 存储操作日志
			if err := runtime.PutToStore([]string{"file_operations"}, fmt.Sprintf("%d", ctx.Value("timestamp")), map[string]interface{}{
				"operation": params.Operation,
				"path":      params.Path,
				"success":   err == nil,
				"error":     err,
			}); err != nil {
				return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to store operation log").
					WithComponent("file_operations_tool").
					WithOperation("executeWithRuntime")
			}
		}
	}

	// 流式输出完成状态
	if runtime != nil && runtime.StreamWriter != nil {
		if err := runtime.StreamWriter(map[string]interface{}{
			"status": "completed",
			"tool":   t.Name(),
			"error":  err,
		}); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to stream completion").
				WithComponent("file_operations_tool").
				WithOperation("executeWithRuntime")
		}
	}

	return result, err
}
