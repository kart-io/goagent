package practical

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils"
	"github.com/kart-io/goagent/utils/json"
)

// FileManagementTool 处理文件管理相关操作
type FileManagementTool struct {
	config *FileToolConfig
}

// NewFileManagementTool 创建文件管理工具
func NewFileManagementTool(config *FileToolConfig) *FileManagementTool {
	if config == nil {
		config = DefaultFileToolConfig()
	}
	return &FileManagementTool{
		config: config,
	}
}

// Name 返回工具名称
func (t *FileManagementTool) Name() string {
	return "file_management"
}

// Description 返回工具描述
func (t *FileManagementTool) Description() string {
	return "Manage files and directories: delete, copy, move, list, and search"
}

// ArgsSchema 返回参数 JSON Schema
func (t *FileManagementTool) ArgsSchema() string {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"delete", "copy", "move", "list", "search"},
				"description": "Management operation: delete, copy, move (rename), list (directory), search (find files)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File or directory path",
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
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"default":     false,
						"description": "Recursive operation for directories",
					},
				},
			},
		},
		"required": []string{"operation", "path"},
	}

	schemaJSON, _ := json.Marshal(schema)
	return string(schemaJSON)
}

// Execute 执行文件管理操作
func (t *FileManagementTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	params, err := t.parseInput(input.Args)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid input").
			WithComponent("file_management_tool").
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
	case "delete":
		result, err = t.deleteFile(ctx, params)
	case "copy":
		result, err = t.copyFile(ctx, params)
	case "move":
		result, err = t.moveFile(ctx, params)
	case "list":
		result, err = t.listDirectory(ctx, params)
	case "search":
		result, err = t.searchFiles(ctx, params)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported operation").
			WithComponent("file_management_tool").
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
func (t *FileManagementTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return t.Execute(ctx, input)
}

// Stream 实现 Runnable 接口
func (t *FileManagementTool) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan agentcore.StreamChunk[*interfaces.ToolOutput], error) {
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
func (t *FileManagementTool) Batch(ctx context.Context, inputs []*interfaces.ToolInput) ([]*interfaces.ToolOutput, error) {
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
func (t *FileManagementTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	return nil
}

// WithCallbacks 实现 Runnable 接口
func (t *FileManagementTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// WithConfig 实现 Runnable 接口
func (t *FileManagementTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// deleteFile 删除文件或目录
func (t *FileManagementTool) deleteFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// 检查路径是否存在
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	// 根据类型删除
	if info.IsDir() {
		if params.Options.Recursive {
			err = os.RemoveAll(params.Path)
		} else {
			err = os.Remove(params.Path)
		}
	} else {
		err = os.Remove(params.Path)
	}

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"result":  "File/directory deleted successfully",
		"info": map[string]interface{}{
			"path":    params.Path,
			"was_dir": info.IsDir(),
		},
	}, nil
}

// copyFile 复制文件
func (t *FileManagementTool) copyFile(ctx context.Context, params *fileParams) (interface{}, error) {
	if params.Destination == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "destination is required for copy operation").
			WithComponent("file_management_tool").
			WithOperation("copyFile").
			WithContext("source", params.Path)
	}

	// 验证目标路径
	if err := validatePath(t.config, params.Destination); err != nil {
		return nil, err
	}

	// 检查源
	srcInfo, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	if srcInfo.IsDir() {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "use recursive option to copy directories").
			WithComponent("file_management_tool").
			WithOperation("copyFile").
			WithContext("source", params.Path)
	}

	// 打开源文件
	src, err := os.Open(params.Path)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(src)

	// 创建目标文件
	dst, err := os.Create(params.Destination)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(dst)

	// 复制内容
	bytesCopied, err := io.Copy(dst, src)
	if err != nil {
		return nil, err
	}

	// 复制权限
	if err := dst.Chmod(srcInfo.Mode()); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"result":  "File copied successfully",
		"info": map[string]interface{}{
			"source":       params.Path,
			"destination":  params.Destination,
			"bytes_copied": bytesCopied,
		},
	}, nil
}

// moveFile 移动文件
func (t *FileManagementTool) moveFile(ctx context.Context, params *fileParams) (interface{}, error) {
	if params.Destination == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "destination is required for move operation").
			WithComponent("file_management_tool").
			WithOperation("moveFile").
			WithContext("source", params.Path)
	}

	// 验证目标路径
	if err := validatePath(t.config, params.Destination); err != nil {
		return nil, err
	}

	// 获取源信息
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	// 重命名（移动）
	err = os.Rename(params.Path, params.Destination)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"result":  "File moved successfully",
		"info": map[string]interface{}{
			"source":      params.Path,
			"destination": params.Destination,
			"size":        info.Size(),
		},
	}, nil
}

// listDirectory 列出目录内容
func (t *FileManagementTool) listDirectory(ctx context.Context, params *fileParams) (interface{}, error) {
	var files []map[string]interface{}

	if params.Options.Recursive {
		err := filepath.Walk(params.Path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			files = append(files, map[string]interface{}{
				"path":     path,
				"name":     info.Name(),
				"size":     info.Size(),
				"is_dir":   info.IsDir(),
				"modified": info.ModTime().Format(time.RFC3339),
				"mode":     info.Mode().String(),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(params.Path)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			info, _ := entry.Info()
			files = append(files, map[string]interface{}{
				"path":     filepath.Join(params.Path, entry.Name()),
				"name":     entry.Name(),
				"size":     info.Size(),
				"is_dir":   entry.IsDir(),
				"modified": info.ModTime().Format(time.RFC3339),
				"mode":     info.Mode().String(),
			})
		}
	}

	return map[string]interface{}{
		"success": true,
		"result":  files,
		"info": map[string]interface{}{
			"total_files": len(files),
			"path":        params.Path,
		},
	}, nil
}

// searchFiles 搜索匹配的文件
func (t *FileManagementTool) searchFiles(ctx context.Context, params *fileParams) (interface{}, error) {
	if params.Pattern == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "pattern is required for search operation").
			WithComponent("file_management_tool").
			WithOperation("searchFiles").
			WithContext("path", params.Path)
	}

	var matches []string
	isRegex := strings.Contains(params.Pattern, "[") || strings.Contains(params.Pattern, "*") || strings.Contains(params.Pattern, "?")

	var pattern *regexp.Regexp
	if !isRegex {
		// 作为正则表达式处理
		var err error
		pattern, err = regexp.Compile(params.Pattern)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid regex pattern").
				WithComponent("file_management_tool").
				WithOperation("searchFiles").
				WithContext("pattern", params.Pattern)
		}
	}

	err := filepath.Walk(params.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 检查模式
		matched := false
		if isRegex {
			matched, _ = filepath.Match(params.Pattern, filepath.Base(path))
		} else if pattern != nil {
			matched = pattern.MatchString(path)
		}

		if matched {
			matches = append(matches, path)
		}

		// 如果不是递归模式且在子目录中，跳过
		if !params.Options.Recursive && info.IsDir() && path != params.Path {
			return filepath.SkipDir
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"result":  matches,
		"info": map[string]interface{}{
			"total_matches": len(matches),
			"pattern":       params.Pattern,
			"search_path":   params.Path,
		},
	}, nil
}

// parseInput 解析输入参数
func (t *FileManagementTool) parseInput(input interface{}) (*fileParams, error) {
	var params fileParams

	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &params); err != nil {
		return nil, err
	}

	return &params, nil
}
