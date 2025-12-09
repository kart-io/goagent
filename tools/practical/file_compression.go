package practical

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/utils"
	"github.com/kart-io/goagent/utils/json"
)

// FileCompressionTool 处理文件压缩和解压操作
type FileCompressionTool struct {
	config *FileToolConfig
}

// NewFileCompressionTool 创建文件压缩工具
func NewFileCompressionTool(config *FileToolConfig) *FileCompressionTool {
	if config == nil {
		config = DefaultFileToolConfig()
	}
	return &FileCompressionTool{
		config: config,
	}
}

// Name 返回工具名称
func (t *FileCompressionTool) Name() string {
	return "file_compression"
}

// Description 返回工具描述
func (t *FileCompressionTool) Description() string {
	return "Compress and decompress files using gzip or zip formats"
}

// ArgsSchema 返回参数 JSON Schema
func (t *FileCompressionTool) ArgsSchema() string {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"compress", "decompress"},
				"description": "Compression operation: compress or decompress",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File path to compress/decompress",
			},
			"destination": map[string]interface{}{
				"type":        "string",
				"description": "Destination path (optional, auto-generated if not provided)",
			},
			"options": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"compression": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"gzip", "zip"},
						"default":     "gzip",
						"description": "Compression format",
					},
				},
			},
		},
		"required": []string{"operation", "path"},
	}

	schemaJSON, _ := json.Marshal(schema)
	return string(schemaJSON)
}

// Execute 执行文件压缩操作
func (t *FileCompressionTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	params, err := t.parseInput(input.Args)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid input").
			WithComponent("file_compression_tool").
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
	case "compress":
		result, err = t.compressFile(ctx, params)
	case "decompress":
		result, err = t.decompressFile(ctx, params)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported operation").
			WithComponent("file_compression_tool").
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
func (t *FileCompressionTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return t.Execute(ctx, input)
}

// Stream 实现 Runnable 接口
func (t *FileCompressionTool) Stream(ctx context.Context, input *interfaces.ToolInput) (<-chan agentcore.StreamChunk[*interfaces.ToolOutput], error) {
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
func (t *FileCompressionTool) Batch(ctx context.Context, inputs []*interfaces.ToolInput) ([]*interfaces.ToolOutput, error) {
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
func (t *FileCompressionTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	return nil
}

// WithCallbacks 实现 Runnable 接口
func (t *FileCompressionTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// WithConfig 实现 Runnable 接口
func (t *FileCompressionTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// compressFile 压缩文件
func (t *FileCompressionTool) compressFile(ctx context.Context, params *fileParams) (interface{}, error) {
	compression := params.Options.Compression
	if compression == "" {
		compression = "gzip"
	}

	outputPath := params.Destination
	if outputPath == "" {
		switch compression {
		case "gzip":
			outputPath = params.Path + ".gz"
		case "zip":
			outputPath = params.Path + ".zip"
		}
	}

	switch compression {
	case "gzip":
		return t.compressGzip(params.Path, outputPath)
	case "zip":
		return t.compressZip(params.Path, outputPath)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported compression format").
			WithComponent("file_compression_tool").
			WithOperation("compressFile").
			WithContext("compression", compression)
	}
}

// compressGzip 使用 gzip 压缩文件
func (t *FileCompressionTool) compressGzip(src, dst string) (interface{}, error) {
	// 打开源文件
	srcFile, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(srcFile)

	// 创建目标文件
	dstFile, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(dstFile)

	// 创建 gzip 写入器
	gz := gzip.NewWriter(dstFile)
	defer utils.CloseQuietly(gz)

	// 复制内容
	written, err := io.Copy(gz, srcFile)
	if err != nil {
		return nil, err
	}

	srcInfo, _ := os.Stat(src)
	dstInfo, _ := os.Stat(dst)

	return map[string]interface{}{
		"success": true,
		"result":  "File compressed successfully",
		"info": map[string]interface{}{
			"source":            src,
			"destination":       dst,
			"original_size":     srcInfo.Size(),
			"compressed_size":   dstInfo.Size(),
			"compression_ratio": float64(dstInfo.Size()) / float64(srcInfo.Size()),
			"bytes_written":     written,
		},
	}, nil
}

// compressZip 使用 zip 压缩文件
func (t *FileCompressionTool) compressZip(src, dst string) (interface{}, error) {
	// 创建目标文件
	dstFile, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(dstFile)

	// 创建 zip 写入器
	zipWriter := zip.NewWriter(dstFile)
	defer utils.CloseQuietly(zipWriter)

	// 添加文件到 zip
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		// 压缩目录
		if err := filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			return t.addFileToZip(zipWriter, path, info, src)
		}); err != nil {
			return nil, err
		}
	} else {
		// 压缩单个文件
		writer, err := zipWriter.Create(filepath.Base(src))
		if err != nil {
			return nil, err
		}

		file, err := os.Open(src)
		if err != nil {
			return nil, err
		}
		defer utils.CloseQuietly(file)

		_, err = io.Copy(writer, file)
		if err != nil {
			return nil, err
		}
	}

	dstInfo, _ := os.Stat(dst)

	return map[string]interface{}{
		"success": true,
		"result":  "File compressed successfully",
		"info": map[string]interface{}{
			"source":          src,
			"destination":     dst,
			"compressed_size": dstInfo.Size(),
		},
	}, nil
}

// addFileToZip 将单个文件添加到 zip 归档中
func (t *FileCompressionTool) addFileToZip(zipWriter *zip.Writer, path string, info os.FileInfo, baseDir string) error {
	// 创建头部
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}

	header.Name, _ = filepath.Rel(baseDir, path)
	if info.IsDir() {
		header.Name += "/"
	}

	// 创建写入器
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	// 复制文件内容
	if !info.IsDir() {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer utils.CloseQuietly(file)
		if _, err = io.Copy(writer, file); err != nil {
			return err
		}
	}

	return nil
}

// decompressFile 解压文件
func (t *FileCompressionTool) decompressFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// 检测压缩格式
	var compression string
	if params.Options.Compression != "" {
		compression = params.Options.Compression
	} else if strings.HasSuffix(params.Path, ".gz") {
		compression = "gzip"
	} else if strings.HasSuffix(params.Path, ".zip") {
		compression = "zip"
	} else {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "cannot detect compression format").
			WithComponent("file_compression_tool").
			WithOperation("decompressFile").
			WithContext("file_path", params.Path)
	}

	outputPath := params.Destination
	if outputPath == "" {
		outputPath = strings.TrimSuffix(params.Path, filepath.Ext(params.Path))
	}

	switch compression {
	case "gzip":
		return t.decompressGzip(params.Path, outputPath)
	case "zip":
		return t.decompressZip(params.Path, outputPath)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported compression format").
			WithComponent("file_compression_tool").
			WithOperation("decompressFile").
			WithContext("compression", compression)
	}
}

// decompressGzip 解压 gzip 文件
func (t *FileCompressionTool) decompressGzip(src, dst string) (interface{}, error) {
	// 打开源文件
	srcFile, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(srcFile)

	// 创建 gzip 读取器
	gz, err := gzip.NewReader(srcFile)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(gz)

	// 创建目标文件
	dstFile, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(dstFile)

	// 复制内容
	written, err := io.Copy(dstFile, gz)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"result":  "File decompressed successfully",
		"info": map[string]interface{}{
			"source":        src,
			"destination":   dst,
			"bytes_written": written,
		},
	}, nil
}

// decompressZip 解压 zip 文件
func (t *FileCompressionTool) decompressZip(src, dst string) (interface{}, error) {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return nil, err
	}
	defer utils.CloseQuietly(reader)

	// 创建目标目录
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, err
	}

	filesExtracted := 0
	for _, file := range reader.File {
		if err := t.extractZipFile(file, dst); err != nil {
			return nil, err
		}
		if !file.FileInfo().IsDir() {
			filesExtracted++
		}
	}

	return map[string]interface{}{
		"success": true,
		"result":  "Archive extracted successfully",
		"info": map[string]interface{}{
			"source":          src,
			"destination":     dst,
			"files_extracted": filesExtracted,
		},
	}, nil
}

// extractZipFile 解压单个 zip 文件条目
func (t *FileCompressionTool) extractZipFile(file *zip.File, destDir string) error {
	path := filepath.Join(destDir, file.Name)

	if file.FileInfo().IsDir() {
		return os.MkdirAll(path, file.Mode())
	}

	// 打开 zip 中的文件
	fileReader, err := file.Open()
	if err != nil {
		return err
	}
	defer utils.CloseQuietly(fileReader)

	// 创建目标文件
	targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
	if err != nil {
		return err
	}
	defer utils.CloseQuietly(targetFile)

	// 复制内容
	_, err = io.Copy(targetFile, fileReader)
	return err
}

// parseInput 解析输入参数
func (t *FileCompressionTool) parseInput(input interface{}) (*fileParams, error) {
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
