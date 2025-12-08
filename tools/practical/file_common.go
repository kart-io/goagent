package practical

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
)

// FileToolConfig 文件工具通用配置
type FileToolConfig struct {
	BasePath       string
	MaxFileSize    int64
	AllowedPaths   []string
	ForbiddenPaths []string
}

// DefaultFileToolConfig 返回默认配置
func DefaultFileToolConfig() *FileToolConfig {
	return &FileToolConfig{
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
}

// fileParams 定义文件操作参数
type fileParams struct {
	Operation   string      `json:"operation"`
	Path        string      `json:"path"`
	Content     string      `json:"content"`
	Destination string      `json:"destination"`
	Pattern     string      `json:"pattern"`
	Options     fileOptions `json:"options"`
}

// fileOptions 定义文件操作选项
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

// validatePath 验证路径安全性
func validatePath(config *FileToolConfig, path string) error {
	// 转换为绝对路径
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	// 检查禁止路径
	for _, forbidden := range config.ForbiddenPaths {
		if strings.HasPrefix(absPath, forbidden) {
			return agentErrors.New(agentErrors.CodeToolExecution, "access to path is forbidden").
				WithComponent("file_tool").
				WithOperation("validatePath").
				WithContext("path", path).
				WithContext("forbidden_path", forbidden)
		}
	}

	// 如果设置了 basePath，确保路径在其范围内
	if config.BasePath != "" {
		if !strings.HasPrefix(absPath, config.BasePath) {
			return agentErrors.New(agentErrors.CodeToolExecution, "path must be within base path").
				WithComponent("file_tool").
				WithOperation("validatePath").
				WithContext("path", path).
				WithContext("base_path", config.BasePath)
		}
	}

	return nil
}

// readLines 从文件中读取指定行数
func readLines(file *os.File, lines int) (string, error) {
	scanner := bufio.NewScanner(file)
	var result []string
	count := 0

	for scanner.Scan() && count < lines {
		result = append(result, scanner.Text())
		count++
	}

	return strings.Join(result, "\n"), scanner.Err()
}

// calculateMD5 计算 MD5 校验和
func calculateMD5(data []byte) string {
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

// calculateSHA256 计算 SHA256 校验和
func calculateSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// calculateChecksum 计算校验和（默认使用 MD5）
func calculateChecksum(data []byte) string {
	return calculateMD5(data)
}

// detectMimeType 检测文件的 MIME 类型
func detectMimeType(data []byte) string {
	if len(data) == 0 {
		return interfaces.ContentTypeOctetStream
	}

	// 使用标准库检测 MIME 类型（检测前 512 字节）
	mimeType := http.DetectContentType(data)

	// 标准库返回格式如 "text/plain; charset=utf-8"，需要取主类型
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	return mimeType
}

// isBinary 判断文件是否为二进制文件
func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// 使用 MIME 类型判断
	mimeType := detectMimeType(data)

	// 文本类型
	if strings.HasPrefix(mimeType, "text/") {
		return false
	}

	// JSON/XML 等结构化文本
	textTypes := []string{
		"application/json",
		"application/xml",
		"application/javascript",
		"application/x-sh",
	}
	for _, textType := range textTypes {
		if mimeType == textType {
			return false
		}
	}

	// 默认视为二进制
	return true
}
