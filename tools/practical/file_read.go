package practical

import (
	"context"
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils"
	"github.com/kart-io/goagent/utils/json"
)

// FileReadTool 处理文件读取相关操作
type FileReadTool struct {
	config *FileToolConfig
}

// NewFileReadTool 创建文件读取工具
func NewFileReadTool(config *FileToolConfig) *FileReadTool {
	if config == nil {
		config = DefaultFileToolConfig()
	}
	return &FileReadTool{
		config: config,
	}
}

// Name 返回工具名称
func (t *FileReadTool) Name() string {
	return "file_read"
}

// Description 返回工具描述
func (t *FileReadTool) Description() string {
	return "Read files, parse structured formats (JSON/YAML/CSV), get file info, and analyze file content"
}

// ArgsSchema 返回参数 JSON Schema
func (t *FileReadTool) ArgsSchema() string {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"read", "parse", "info", "analyze"},
				"description": "Read operation to perform: read (file content), parse (structured files), info (file metadata), analyze (content analysis)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File path to read",
			},
			"options": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"encoding": map[string]interface{}{
						"type":        "string",
						"default":     "utf-8",
						"description": "File encoding",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"json", "yaml", "csv", "xml", "text"},
						"description": "File format for parsing",
					},
					"lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to read (for large files)",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Byte offset to start reading from",
					},
				},
			},
		},
		"required": []string{"operation", "path"},
	}

	schemaJSON, _ := json.Marshal(schema)
	return string(schemaJSON)
}

// Execute 执行文件读取操作
func (t *FileReadTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	params, err := t.parseInput(input.Args)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid input").
			WithComponent("file_read_tool").
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
	case "read":
		result, err = t.readFile(ctx, params)
	case "parse":
		result, err = t.parseFile(ctx, params)
	case "info":
		result, err = t.getFileInfo(ctx, params)
	case "analyze":
		result, err = t.analyzeFile(ctx, params)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported operation").
			WithComponent("file_read_tool").
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
func (t *FileReadTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return t.Execute(ctx, input)
}

// Stream 实现 Runnable 接口
func (t *FileReadTool) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan agentcore.StreamChunk[*interfaces.ToolOutput], error) {
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
func (t *FileReadTool) Batch(ctx context.Context, inputs []*interfaces.ToolInput) ([]*interfaces.ToolOutput, error) {
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
func (t *FileReadTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	return nil
}

// WithCallbacks 实现 Runnable 接口
func (t *FileReadTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// WithConfig 实现 Runnable 接口
func (t *FileReadTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// readFile 读取文件内容
func (t *FileReadTool) readFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// 检查文件大小
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	if info.Size() > t.config.MaxFileSize {
		return nil, agentErrors.New(agentErrors.CodeToolExecution, "file too large").
			WithComponent("file_read_tool").
			WithOperation("readFile").
			WithContext("file_path", params.Path).
			WithContext("size", info.Size()).
			WithContext("max_size", t.config.MaxFileSize)
	}

	// 打开文件
	file, err := os.Open(params.Path)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(file)

	// 根据选项读取
	if params.Options.Lines > 0 {
		// 读取指定行数
		content, err := readLines(file, params.Options.Lines)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"success": true,
			"result":  content,
			"info": map[string]interface{}{
				"size":     info.Size(),
				"modified": info.ModTime().Format(time.RFC3339),
			},
		}, nil
	}

	if params.Options.Offset > 0 {
		// 从偏移位置开始读取
		_, err = file.Seek(params.Options.Offset, 0)
		if err != nil {
			return nil, err
		}
	}

	// 读取整个文件
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"result":  string(content),
		"info": map[string]interface{}{
			"size":     info.Size(),
			"modified": info.ModTime().Format(time.RFC3339),
			"checksum": calculateChecksum(content),
		},
	}, nil
}

// parseFile 解析结构化文件
func (t *FileReadTool) parseFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// 读取文件
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return nil, err
	}

	format := params.Options.Format
	if format == "" {
		// 从扩展名检测格式
		ext := strings.ToLower(filepath.Ext(params.Path))
		switch ext {
		case ".json":
			format = "json"
		case ".yaml", ".yml":
			format = "yaml"
		case ".csv":
			format = "csv"
		default:
			format = "text"
		}
	}

	var result interface{}
	switch format {
	case "json":
		err = json.Unmarshal(content, &result)
	case "yaml":
		err = yaml.Unmarshal(content, &result)
	case "csv":
		result, err = t.parseCSV(content)
	default:
		result = string(content)
	}

	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeToolExecution, "failed to parse file").
			WithComponent("file_read_tool").
			WithOperation("parseFile").
			WithContext("file_path", params.Path).
			WithContext("format", format)
	}

	return map[string]interface{}{
		"success": true,
		"result":  result,
		"info": map[string]interface{}{
			"format": format,
			"size":   len(content),
		},
	}, nil
}

// parseCSV 解析 CSV 内容
func (t *FileReadTool) parseCSV(content []byte) ([][]string, error) {
	reader := csv.NewReader(strings.NewReader(string(content)))
	return reader.ReadAll()
}

// getFileInfo 获取文件详细信息
func (t *FileReadTool) getFileInfo(ctx context.Context, params *fileParams) (interface{}, error) {
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"name":        info.Name(),
		"size":        info.Size(),
		"is_dir":      info.IsDir(),
		"modified":    info.ModTime().Format(time.RFC3339),
		"mode":        info.Mode().String(),
		"permissions": info.Mode().Perm().String(),
	}

	// 为文件添加校验和
	if !info.IsDir() && info.Size() < t.config.MaxFileSize {
		content, err := os.ReadFile(params.Path)
		if err == nil {
			result["md5"] = calculateMD5(content)
			result["sha256"] = calculateSHA256(content)
		}
	}

	// 为目录添加文件计数
	if info.IsDir() {
		entries, _ := os.ReadDir(params.Path)
		result["file_count"] = len(entries)
	}

	return map[string]interface{}{
		"success": true,
		"result":  result,
	}, nil
}

// analyzeFile 分析文件内容
func (t *FileReadTool) analyzeFile(ctx context.Context, params *fileParams) (interface{}, error) {
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	analysis := map[string]interface{}{
		"name":     info.Name(),
		"size":     info.Size(),
		"modified": info.ModTime().Format(time.RFC3339),
		"is_dir":   info.IsDir(),
	}

	if !info.IsDir() && info.Size() < t.config.MaxFileSize {
		content, err := os.ReadFile(params.Path)
		if err == nil {
			// 文件类型检测
			analysis["mime_type"] = detectMimeType(content)

			// 行数统计
			analysis["line_count"] = strings.Count(string(content), "\n") + 1

			// 单词数统计
			analysis["word_count"] = len(strings.Fields(string(content)))

			// 字符数统计
			analysis["char_count"] = len(content)

			// 编码检测
			analysis["appears_binary"] = isBinary(content)

			// 校验和
			analysis["md5"] = calculateMD5(content)
			analysis["sha256"] = calculateSHA256(content)
		}
	}

	return map[string]interface{}{
		"success": true,
		"result":  analysis,
	}, nil
}

// parseInput 解析输入参数
func (t *FileReadTool) parseInput(input interface{}) (*fileParams, error) {
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
