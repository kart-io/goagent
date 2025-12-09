package practical

import (
	"context"
	"os"
	"time"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils/json"
)

// FileWatchTool 处理文件监控操作
type FileWatchTool struct {
	config *FileToolConfig
}

// NewFileWatchTool 创建文件监控工具
func NewFileWatchTool(config *FileToolConfig) *FileWatchTool {
	if config == nil {
		config = DefaultFileToolConfig()
	}
	return &FileWatchTool{
		config: config,
	}
}

// Name 返回工具名称
func (t *FileWatchTool) Name() string {
	return "file_watch"
}

// Description 返回工具描述
func (t *FileWatchTool) Description() string {
	return "Watch file changes and monitor file system events"
}

// ArgsSchema 返回参数 JSON Schema
func (t *FileWatchTool) ArgsSchema() string {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"watch"},
				"description": "Watch operation",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File path to watch",
			},
			"options": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"follow": map[string]interface{}{
						"type":        "boolean",
						"default":     false,
						"description": "Follow file changes (tail -f behavior)",
					},
				},
			},
		},
		"required": []string{"operation", "path"},
	}

	schemaJSON, _ := json.Marshal(schema)
	return string(schemaJSON)
}

// Execute 执行文件监控操作
func (t *FileWatchTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	params, err := t.parseInput(input.Args)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid input").
			WithComponent("file_watch_tool").
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

	// 执行监控操作
	var result interface{}
	switch params.Operation {
	case "watch":
		result, err = t.watchFile(ctx, params)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported operation").
			WithComponent("file_watch_tool").
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
func (t *FileWatchTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return t.Execute(ctx, input)
}

// Stream 实现 Runnable 接口
func (t *FileWatchTool) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan agentcore.StreamChunk[*interfaces.ToolOutput], error) {
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
func (t *FileWatchTool) Batch(ctx context.Context, inputs []*interfaces.ToolInput) ([]*interfaces.ToolOutput, error) {
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
func (t *FileWatchTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	return nil
}

// WithCallbacks 实现 Runnable 接口
func (t *FileWatchTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// WithConfig 实现 Runnable 接口
func (t *FileWatchTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// watchFile 监控文件变化
// 注意: 这是一个简单的轮询实现，实际生产环境应使用 fsnotify 等库
func (t *FileWatchTool) watchFile(ctx context.Context, params *fileParams) (interface{}, error) {
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	lastModified := info.ModTime()
	lastSize := info.Size()

	changes := []map[string]interface{}{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second) // 最多监控 30 秒

	for {
		select {
		case <-ctx.Done():
			return map[string]interface{}{
				"success": true,
				"result":  changes,
			}, nil
		case <-timeout:
			return map[string]interface{}{
				"success": true,
				"result":  changes,
				"info": map[string]interface{}{
					"timeout": true,
				},
			}, nil
		case <-ticker.C:
			newInfo, err := os.Stat(params.Path)
			if err != nil {
				if os.IsNotExist(err) {
					changes = append(changes, map[string]interface{}{
						"type":      "deleted",
						"timestamp": time.Now().Format(time.RFC3339),
					})
					return map[string]interface{}{
						"success": true,
						"result":  changes,
					}, nil
				}
				continue
			}

			if newInfo.ModTime() != lastModified || newInfo.Size() != lastSize {
				changes = append(changes, map[string]interface{}{
					"type":      "modified",
					"timestamp": time.Now().Format(time.RFC3339),
					"size":      newInfo.Size(),
					"size_diff": newInfo.Size() - lastSize,
				})
				lastModified = newInfo.ModTime()
				lastSize = newInfo.Size()

				if !params.Options.Follow {
					return map[string]interface{}{
						"success": true,
						"result":  changes,
					}, nil
				}
			}
		}
	}
}

// parseInput 解析输入参数
func (t *FileWatchTool) parseInput(input interface{}) (*fileParams, error) {
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
