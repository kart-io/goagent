package practical

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	agentcore "github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// FileOperationsTool handles various file system operations
type FileOperationsTool struct {
	basePath       string
	maxFileSize    int64
	allowedPaths   []string
	forbiddenPaths []string
}

// NewFileOperationsTool creates a new file operations tool
func NewFileOperationsTool(basePath string) *FileOperationsTool {
	return &FileOperationsTool{
		basePath:    basePath,
		maxFileSize: 100 * 1024 * 1024, // 100MB
		allowedPaths: []string{
			"/tmp",
			"/var/tmp",
		},
		forbiddenPaths: []string{
			"/etc",
			"/sys",
			"/proc",
		},
	}
}

// Name returns the tool name
func (t *FileOperationsTool) Name() string {
	return "file_operations"
}

// Description returns the tool description
func (t *FileOperationsTool) Description() string {
	return "Performs file system operations including read, write, search, compress, and analyze files"
}

// ArgsSchema returns the arguments schema as a JSON string
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

// OutputSchema returns the output schema

// Execute performs the file operation
func (t *FileOperationsTool) Execute(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	params, err := t.parseFileInput(input.Args)
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid input").
			WithComponent("file_operations_tool").
			WithOperation("execute")
	}

	// Validate path security
	if err := t.validatePath(params.Path); err != nil {
		return &interfaces.ToolOutput{
			Result: map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			},
			Error: err.Error(),
		}, err
	}

	// Execute operation
	var result interface{}
	switch params.Operation {
	case "read":
		result, err = t.readFile(ctx, params)
	case "write":
		result, err = t.writeFile(ctx, params)
	case "append":
		result, err = t.appendFile(ctx, params)
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
	case "info":
		result, err = t.getFileInfo(ctx, params)
	case "compress":
		result, err = t.compressFile(ctx, params)
	case "decompress":
		result, err = t.decompressFile(ctx, params)
	case "parse":
		result, err = t.parseFile(ctx, params)
	case "analyze":
		result, err = t.analyzeFile(ctx, params)
	case "watch":
		result, err = t.watchFile(ctx, params)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported operation").
			WithComponent("file_operations_tool").
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

// Implement Runnable interface
func (t *FileOperationsTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return t.Execute(ctx, input)
}

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

func (t *FileOperationsTool) Pipe(next agentcore.Runnable[*interfaces.ToolOutput, any]) agentcore.Runnable[*interfaces.ToolInput, any] {
	return nil
}

func (t *FileOperationsTool) WithCallbacks(callbacks ...agentcore.Callback) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

func (t *FileOperationsTool) WithConfig(config agentcore.RunnableConfig) agentcore.Runnable[*interfaces.ToolInput, *interfaces.ToolOutput] {
	return t
}

// readFile reads file content
func (t *FileOperationsTool) readFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// Check file size
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	if info.Size() > t.maxFileSize {
		return nil, agentErrors.New(agentErrors.CodeToolExecution, "file too large").
			WithComponent("file_operations_tool").
			WithOperation("readFile").
			WithContext("file_path", params.Path).
			WithContext("size", info.Size()).
			WithContext("max_size", t.maxFileSize)
	}

	// Open file
	file, err := os.Open(params.Path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("failed to close file: %v", err)
		}
	}()

	// Read with options
	if params.Options.Lines > 0 {
		// Read specific number of lines
		content, err := t.readLines(file, params.Options.Lines)
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
		// Seek to offset
		_, err = file.Seek(params.Options.Offset, 0)
		if err != nil {
			return nil, err
		}
	}

	// Read entire file
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
			"checksum": t.calculateChecksum(content),
		},
	}, nil
}

// writeFile writes content to file
func (t *FileOperationsTool) writeFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// Ensure directory exists
	dir := filepath.Dir(params.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	// Set permissions
	perm := os.FileMode(0o644)
	if params.Options.Permissions != "" {
		// Parse permissions
		var p uint64
		if _, err := fmt.Sscanf(params.Options.Permissions, "%o", &p); err != nil {
			return nil, err
		}
		// Validate permissions to prevent overflow
		if p > 0xFFFFFFFF { // Max uint32 for FileMode
			p = 0xFFFFFFFF
		}
		perm = os.FileMode(p)
	}

	// Write file
	err := os.WriteFile(params.Path, []byte(params.Content), perm)
	if err != nil {
		return nil, err
	}

	// Get file info
	info, _ := os.Stat(params.Path)

	return map[string]interface{}{
		"success": true,
		"result":  "File written successfully",
		"info": map[string]interface{}{
			"size":     len(params.Content),
			"path":     params.Path,
			"checksum": t.calculateChecksum([]byte(params.Content)),
			"modified": info.ModTime().Format(time.RFC3339),
		},
	}, nil
}

// appendFile appends content to file
func (t *FileOperationsTool) appendFile(ctx context.Context, params *fileParams) (interface{}, error) {
	file, err := os.OpenFile(params.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("failed to close file: %v", err)
		}
	}()

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

// deleteFile deletes a file or directory
func (t *FileOperationsTool) deleteFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// Check if path exists
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	// Delete based on type
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

// copyFile copies a file
func (t *FileOperationsTool) copyFile(ctx context.Context, params *fileParams) (interface{}, error) {
	if params.Destination == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "destination is required for copy operation").
			WithComponent("file_operations_tool").
			WithOperation("copyFile").
			WithContext("source", params.Path)
	}

	// Validate destination path
	if err := t.validatePath(params.Destination); err != nil {
		return nil, err
	}

	// Check source
	srcInfo, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	if srcInfo.IsDir() {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "use recursive option to copy directories").
			WithComponent("file_operations_tool").
			WithOperation("copyFile").
			WithContext("source", params.Path)
	}

	// Open source
	src, err := os.Open(params.Path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := src.Close(); err != nil {
			fmt.Printf("failed to close source file: %v", err)
		}
	}()

	// Create destination
	dst, err := os.Create(params.Destination)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := dst.Close(); err != nil {
			fmt.Printf("failed to close destination file: %v", err)
		}
	}()

	// Copy content
	bytesCopied, err := io.Copy(dst, src)
	if err != nil {
		return nil, err
	}

	// Copy permissions
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

// moveFile moves a file
func (t *FileOperationsTool) moveFile(ctx context.Context, params *fileParams) (interface{}, error) {
	if params.Destination == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "destination is required for move operation").
			WithComponent("file_operations_tool").
			WithOperation("moveFile").
			WithContext("source", params.Path)
	}

	// Validate destination path
	if err := t.validatePath(params.Destination); err != nil {
		return nil, err
	}

	// Get source info
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	// Rename (move)
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

// listDirectory lists directory contents
func (t *FileOperationsTool) listDirectory(ctx context.Context, params *fileParams) (interface{}, error) {
	var files []map[string]interface{}

	if params.Options.Recursive {
		err := filepath.Walk(params.Path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err // Return the error to stop walking or handle it
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

// searchFiles searches for files matching pattern
func (t *FileOperationsTool) searchFiles(ctx context.Context, params *fileParams) (interface{}, error) {
	if params.Pattern == "" {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "pattern is required for search operation").
			WithComponent("file_operations_tool").
			WithOperation("searchFiles").
			WithContext("path", params.Path)
	}

	var matches []string
	isRegex := strings.Contains(params.Pattern, "[") || strings.Contains(params.Pattern, "*") || strings.Contains(params.Pattern, "?")

	var pattern *regexp.Regexp
	if !isRegex {
		// Treat as regex
		var err error
		pattern, err = regexp.Compile(params.Pattern)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidInput, "invalid regex pattern").
				WithComponent("file_operations_tool").
				WithOperation("searchFiles").
				WithContext("pattern", params.Pattern)
		}
	}

	err := filepath.Walk(params.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // Return the error to be handled
		}

		// Check pattern
		matched := false
		if isRegex {
			matched, _ = filepath.Match(params.Pattern, filepath.Base(path))
		} else if pattern != nil {
			matched = pattern.MatchString(path)
		}

		if matched {
			matches = append(matches, path)
		}

		// Stop if not recursive and we're in a subdirectory
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

// getFileInfo gets detailed file information
func (t *FileOperationsTool) getFileInfo(ctx context.Context, params *fileParams) (interface{}, error) {
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
		"permissions": fmt.Sprintf("%04o", info.Mode().Perm()),
	}

	// Add checksum for files
	if !info.IsDir() && info.Size() < t.maxFileSize {
		content, err := os.ReadFile(params.Path)
		if err == nil {
			result["md5"] = t.calculateMD5(content)
			result["sha256"] = t.calculateSHA256(content)
		}
	}

	// Add directory info
	if info.IsDir() {
		entries, _ := os.ReadDir(params.Path)
		result["file_count"] = len(entries)
	}

	return map[string]interface{}{
		"success": true,
		"result":  result,
	}, nil
}

// compressFile compresses a file
func (t *FileOperationsTool) compressFile(ctx context.Context, params *fileParams) (interface{}, error) {
	compression := params.Options.Compression
	if compression == "" {
		compression = "gzip"
	}

	outputPath := params.Path
	if params.Destination != "" {
		outputPath = params.Destination
	} else {
		switch compression {
		case "gzip":
			outputPath += ".gz"
		case "zip":
			outputPath += ".zip"
		}
	}

	switch compression {
	case "gzip":
		return t.compressGzip(params.Path, outputPath)
	case "zip":
		return t.compressZip(params.Path, outputPath)
	default:
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "unsupported compression format").
			WithComponent("file_operations_tool").
			WithOperation("compressFile").
			WithContext("compression", compression)
	}
}

// compressGzip compresses file with gzip
func (t *FileOperationsTool) compressGzip(src, dst string) (interface{}, error) {
	// Open source
	srcFile, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			fmt.Printf("failed to close source file: %v", err)
		}
	}()

	// Create destination
	dstFile, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := dstFile.Close(); err != nil {
			fmt.Printf("failed to close destination file: %v", err)
		}
	}()

	// Create gzip writer
	gz := gzip.NewWriter(dstFile)
	defer func() {
		if err := gz.Close(); err != nil {
			fmt.Printf("failed to close gzip writer: %v", err)
		}
	}()

	// Copy content
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

// compressZip compresses file with zip
func (t *FileOperationsTool) compressZip(src, dst string) (interface{}, error) {
	// Create destination
	dstFile, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := dstFile.Close(); err != nil {
			fmt.Printf("failed to close destination file: %v", err)
		}
	}()

	// Create zip writer
	zipWriter := zip.NewWriter(dstFile)
	defer func() {
		if err := zipWriter.Close(); err != nil {
			fmt.Printf("failed to close zip writer: %v", err)
		}
	}()

	// Add file to zip
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		// Compress directory
		if err := filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			// Create header
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}

			header.Name, _ = filepath.Rel(src, path)
			if info.IsDir() {
				header.Name += "/"
			}

			// Create writer
			writer, err := zipWriter.CreateHeader(header)
			if err != nil {
				return err
			}

			// Copy file content
			if !info.IsDir() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer func() {
					if err := file.Close(); err != nil {
						fmt.Printf("failed to close file: %v", err)
					}
				}()
				if _, err = io.Copy(writer, file); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			return nil, err
		}
	} else {
		// Compress single file
		writer, err := zipWriter.Create(filepath.Base(src))
		if err != nil {
			return nil, err
		}

		file, err := os.Open(src)
		if err != nil {
			return nil, err
		}
		defer func() {
			if err := file.Close(); err != nil {
				fmt.Printf("failed to close file: %v", err)
			}
		}()

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

// decompressFile decompresses a file
func (t *FileOperationsTool) decompressFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// Detect compression format
	var compression string
	if params.Options.Compression != "" {
		compression = params.Options.Compression
	} else if strings.HasSuffix(params.Path, ".gz") {
		compression = "gzip"
	} else if strings.HasSuffix(params.Path, ".zip") {
		compression = "zip"
	} else {
		return nil, agentErrors.New(agentErrors.CodeInvalidInput, "cannot detect compression format").
			WithComponent("file_operations_tool").
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
			WithComponent("file_operations_tool").
			WithOperation("decompressFile").
			WithContext("compression", compression)
	}
}

// decompressGzip decompresses gzip file
func (t *FileOperationsTool) decompressGzip(src, dst string) (interface{}, error) {
	// Open source
	srcFile, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			fmt.Printf("failed to close source file: %v", err)
		}
	}()

	// Create gzip reader
	gz, err := gzip.NewReader(srcFile)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := gz.Close(); err != nil {
			fmt.Printf("failed to close gzip reader: %v", err)
		}
	}()

	// Create destination
	dstFile, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := dstFile.Close(); err != nil {
			fmt.Printf("failed to close destination file: %v", err)
		}
	}()

	// Copy content
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

// decompressZip decompresses zip file
func (t *FileOperationsTool) decompressZip(src, dst string) (interface{}, error) {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			fmt.Printf("failed to close zip reader: %v", err)
		}
	}()

	// Create destination directory
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, err
	}

	filesExtracted := 0
	for _, file := range reader.File {
		path := filepath.Join(dst, file.Name)

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(path, file.Mode()); err != nil {
				return nil, err
			}
			continue
		}

		// Create file
		fileReader, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer func() {
			if err := fileReader.Close(); err != nil {
				fmt.Printf("failed to close file reader: %v", err)
			}
		}()

		// Create destination file
		targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return nil, err
		}
		defer func() {
			if err := targetFile.Close(); err != nil {
				fmt.Printf("failed to close target file: %v", err)
			}
		}()

		// Copy content
		_, err = io.Copy(targetFile, fileReader)
		if err != nil {
			return nil, err
		}

		filesExtracted++
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

// parseFile parses structured file formats
func (t *FileOperationsTool) parseFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// Read file
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return nil, err
	}

	format := params.Options.Format
	if format == "" {
		// Detect format from extension
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
			WithComponent("file_operations_tool").
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

// parseCSV parses CSV content
func (t *FileOperationsTool) parseCSV(content []byte) ([][]string, error) {
	reader := csv.NewReader(strings.NewReader(string(content)))
	return reader.ReadAll()
}

// analyzeFile analyzes file content
func (t *FileOperationsTool) analyzeFile(ctx context.Context, params *fileParams) (interface{}, error) {
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

	if !info.IsDir() && info.Size() < t.maxFileSize {
		content, err := os.ReadFile(params.Path)
		if err == nil {
			// File type detection
			analysis["mime_type"] = t.detectMimeType(content)

			// Line count
			analysis["line_count"] = strings.Count(string(content), "\n") + 1

			// Word count
			analysis["word_count"] = len(strings.Fields(string(content)))

			// Character count
			analysis["char_count"] = len(content)

			// Encoding detection
			analysis["appears_binary"] = t.isBinary(content)

			// Checksums
			analysis["md5"] = t.calculateMD5(content)
			analysis["sha256"] = t.calculateSHA256(content)
		}
	}

	return map[string]interface{}{
		"success": true,
		"result":  analysis,
	}, nil
}

// watchFile watches file for changes
func (t *FileOperationsTool) watchFile(ctx context.Context, params *fileParams) (interface{}, error) {
	// This would typically use fsnotify or similar
	// For demonstration, we'll do a simple polling approach
	info, err := os.Stat(params.Path)
	if err != nil {
		return nil, err
	}

	lastModified := info.ModTime()
	lastSize := info.Size()

	changes := []map[string]interface{}{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second) // Watch for max 30 seconds

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

// Helper functions

func (t *FileOperationsTool) validatePath(path string) error {
	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	// Check forbidden paths
	for _, forbidden := range t.forbiddenPaths {
		if strings.HasPrefix(absPath, forbidden) {
			return agentErrors.New(agentErrors.CodeToolExecution, "access to path is forbidden").
				WithComponent("file_operations_tool").
				WithOperation("validatePath").
				WithContext("path", path).
				WithContext("forbidden_path", forbidden)
		}
	}

	// If basePath is set, ensure path is within it
	if t.basePath != "" {
		if !strings.HasPrefix(absPath, t.basePath) {
			return agentErrors.New(agentErrors.CodeToolExecution, "path must be within base path").
				WithComponent("file_operations_tool").
				WithOperation("validatePath").
				WithContext("path", path).
				WithContext("base_path", t.basePath)
		}
	}

	return nil
}

func (t *FileOperationsTool) readLines(file *os.File, lines int) (string, error) {
	scanner := bufio.NewScanner(file)
	var result []string
	count := 0

	for scanner.Scan() && count < lines {
		result = append(result, scanner.Text())
		count++
	}

	return strings.Join(result, "\n"), scanner.Err()
}

func (t *FileOperationsTool) calculateChecksum(data []byte) string {
	return t.calculateMD5(data)
}

func (t *FileOperationsTool) calculateMD5(data []byte) string {
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

func (t *FileOperationsTool) calculateSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (t *FileOperationsTool) detectMimeType(data []byte) string {
	// Simple detection based on content
	if len(data) == 0 {
		return interfaces.ContentTypeOctetStream
	}

	// Check for common text formats
	if json.Valid(data) {
		return interfaces.ContentTypeJSON
	}

	// Check for binary
	if t.isBinary(data) {
		return interfaces.ContentTypeOctetStream
	}

	return interfaces.ContentTypeText
}

func (t *FileOperationsTool) isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func (t *FileOperationsTool) parseFileInput(input interface{}) (*fileParams, error) {
	var params fileParams

	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &params); err != nil {
		return nil, err
	}

	// Set defaults
	if params.Options.Encoding == "" {
		params.Options.Encoding = "utf-8"
	}

	return &params, nil
}

type fileParams struct {
	Operation   string      `json:"operation"`
	Path        string      `json:"path"`
	Content     string      `json:"content"`
	Destination string      `json:"destination"`
	Pattern     string      `json:"pattern"`
	Options     fileOptions `json:"options"`
}

type fileOptions struct {
	Encoding    string `json:"encoding"`
	Recursive   bool   `json:"recursive"`
	Format      string `json:"format"`
	Compression string `json:"compression"`
	Lines       int    `json:"lines"`
	Offset      int64  `json:"offset"`
	Follow      bool   `json:"follow"`
	Permissions string `json:"permissions"`
}

// FileOperationsRuntimeTool extends FileOperationsTool with runtime support
type FileOperationsRuntimeTool struct {
	*FileOperationsTool
}

// NewFileOperationsRuntimeTool creates a runtime-aware file operations tool
func NewFileOperationsRuntimeTool(basePath string) *FileOperationsRuntimeTool {
	return &FileOperationsRuntimeTool{
		FileOperationsTool: NewFileOperationsTool(basePath),
	}
}

// ExecuteWithRuntime executes with runtime support
func (t *FileOperationsRuntimeTool) ExecuteWithRuntime(ctx context.Context, input *interfaces.ToolInput, runtime *tools.ToolRuntime) (*interfaces.ToolOutput, error) {
	// Stream status
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

	// Execute the operation
	result, err := t.Execute(ctx, input)

	// Store file operations in runtime for audit
	if runtime != nil {
		params, _ := t.parseFileInput(input.Args)
		if params != nil {
			// Store operation log
			if err := runtime.PutToStore([]string{"file_operations"}, time.Now().Format(time.RFC3339), map[string]interface{}{
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

	// Stream completion
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
