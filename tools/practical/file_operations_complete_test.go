package practical

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kart-io/goagent/interfaces"
)

// TestFileOperationsTool_Execute_ReadOperation 测试文件读取操作
func TestFileOperationsTool_Execute_ReadOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	// 创建测试文件
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"
	err := ioutil.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("创建测试文件失败：%v", err)
	}

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "read",
			"path":      testFile,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "result" 字段而非 "content"
	content, ok := result["result"].(string)
	if !ok {
		t.Fatal("result 应该存在并且是字符串")
	}

	if content != testContent {
		t.Errorf("期望 '%s'，得到 '%s'", testContent, content)
	}
}

// TestFileOperationsTool_Execute_WriteOperation 测试文件写入操作
func TestFileOperationsTool_Execute_WriteOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "write_test.txt")
	testContent := "New content"

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "write",
			"path":      testFile,
			"content":   testContent,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "success": true 而非 "status": "success"
	if result["success"] != true {
		t.Errorf("期望 success=true，得到 %v", result["success"])
	}

	// 验证文件内容
	content, err := ioutil.ReadFile(testFile)
	if err != nil {
		t.Errorf("读取文件失败：%v", err)
		return
	}

	if string(content) != testContent {
		t.Errorf("文件内容不匹配")
	}
}

// TestFileOperationsTool_Execute_AppendOperation 测试文件追加操作
func TestFileOperationsTool_Execute_AppendOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "append_test.txt")
	initialContent := "Initial content"
	appendContent := "\nAppended content"

	// 创建初始文件
	ioutil.WriteFile(testFile, []byte(initialContent), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "append",
			"path":      testFile,
			"content":   appendContent,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "success": true 而非 "status": "success"
	if result["success"] != true {
		t.Errorf("期望 success=true，得到 %v", result["success"])
	}

	// 验证文件内容
	content, _ := ioutil.ReadFile(testFile)
	if !strings.Contains(string(content), appendContent) {
		t.Error("追加内容不存在")
	}
}

// TestFileOperationsTool_Execute_DeleteOperation 测试文件删除操作
func TestFileOperationsTool_Execute_DeleteOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "delete_test.txt")
	ioutil.WriteFile(testFile, []byte("content to delete"), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "delete",
			"path":      testFile,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "success": true 而非 "status": "success"
	if result["success"] != true {
		t.Errorf("期望 success=true，得到 %v", result["success"])
	}

	// 验证文件已删除
	if _, err := os.Stat(testFile); err == nil {
		t.Error("文件应该已被删除")
	}
}

// TestFileOperationsTool_Execute_CopyOperation 测试文件复制操作
func TestFileOperationsTool_Execute_CopyOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	sourceFile := filepath.Join(tmpDir, "source.txt")
	destFile := filepath.Join(tmpDir, "dest.txt")
	content := "content to copy"

	ioutil.WriteFile(sourceFile, []byte(content), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation":   "copy",
			"path":        sourceFile,
			"destination": destFile,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "success": true 而非 "status": "success"
	if result["success"] != true {
		t.Errorf("期望 success=true，得到 %v", result["success"])
	}

	// 验证目标文件存在且内容相同
	destContent, _ := ioutil.ReadFile(destFile)
	if string(destContent) != content {
		t.Error("复制的文件内容不匹配")
	}
}

// TestFileOperationsTool_Execute_MoveOperation 测试文件移动操作
func TestFileOperationsTool_Execute_MoveOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	sourceFile := filepath.Join(tmpDir, "move_source.txt")
	destFile := filepath.Join(tmpDir, "move_dest.txt")
	content := "content to move"

	ioutil.WriteFile(sourceFile, []byte(content), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation":   "move",
			"path":        sourceFile,
			"destination": destFile,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "success": true 而非 "status": "success"
	if result["success"] != true {
		t.Errorf("期望 success=true，得到 %v", result["success"])
	}

	// 验证原文件已删除，目标文件存在
	if _, err := os.Stat(sourceFile); err == nil {
		t.Error("源文件应该已被删除")
	}

	if _, err := os.Stat(destFile); err != nil {
		t.Error("目标文件应该存在")
	}
}

// TestFileOperationsTool_Execute_ListOperation 测试目录列表操作
func TestFileOperationsTool_Execute_ListOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	// 创建测试文件
	ioutil.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644)
	ioutil.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "list",
			"path":      tmpDir,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 result 数组而非 items
	items, ok := result["result"].([]map[string]interface{})
	if !ok {
		t.Fatal("result 应该存在并且是数组")
	}

	if len(items) != 3 {
		t.Errorf("期望 3 个项目，得到 %d", len(items))
	}
}

// TestFileOperationsTool_Execute_SearchOperation 测试文件搜索操作
func TestFileOperationsTool_Execute_SearchOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	// 创建测试文件
	ioutil.WriteFile(filepath.Join(tmpDir, "test_file.txt"), []byte("content"), 0644)
	ioutil.WriteFile(filepath.Join(tmpDir, "test_data.json"), []byte("{}"), 0644)
	ioutil.WriteFile(filepath.Join(tmpDir, "other.log"), []byte("log"), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "search",
			"path":      tmpDir,
			"pattern":   "test_*",
			"options": map[string]interface{}{
				"recursive": true,
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 result 数组而非 matches
	matches, ok := result["result"].([]string)
	if !ok {
		t.Fatal("result 应该存在并且是字符串数组")
	}

	if len(matches) != 2 {
		t.Errorf("期望找到 2 个匹配，得到 %d", len(matches))
	}
}

// TestFileOperationsTool_Execute_InfoOperation 测试文件信息获取操作
func TestFileOperationsTool_Execute_InfoOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "info_test.txt")
	content := "some content for info"
	ioutil.WriteFile(testFile, []byte(content), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "info",
			"path":      testFile,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// info 操作返回嵌套的 result 结构
	infoResult, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatal("result 应该存在并且是 map")
	}
	if _, ok := infoResult["size"]; !ok {
		t.Error("size 应该存在")
	}
	if _, ok := infoResult["mode"]; !ok {
		t.Error("mode 应该存在")
	}
	if _, ok := infoResult["modified"]; !ok {
		t.Error("modified 应该存在")
	}
}

// TestFileOperationsTool_Execute_CompressGzip 测试 Gzip 压缩操作
func TestFileOperationsTool_Execute_CompressGzip(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	sourceFile := filepath.Join(tmpDir, "compress.txt")
	destFile := filepath.Join(tmpDir, "compress.txt.gz")
	content := "content to compress with gzip"

	ioutil.WriteFile(sourceFile, []byte(content), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "compress",
			"path":      sourceFile,
			"options": map[string]interface{}{
				"compression": "gzip",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "success": true 而非 "status": "success"
	if result["success"] != true {
		t.Errorf("期望 success=true，得到 %v", result["success"])
	}

	// 验证压缩文件存在
	if _, err := os.Stat(destFile); err != nil {
		t.Error("压缩文件应该存在")
	}

	// 验证可以解压
	file, err := os.Open(destFile)
	if err != nil {
		t.Fatalf("打开压缩文件失败：%v", err)
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("创建 gzip reader 失败：%v", err)
	}
	defer reader.Close()

	decompressed, _ := ioutil.ReadAll(reader)
	if string(decompressed) != content {
		t.Error("解压内容不匹配")
	}
}

// TestFileOperationsTool_Execute_CompressZip 测试 Zip 压缩操作
func TestFileOperationsTool_Execute_CompressZip(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	sourceFile := filepath.Join(tmpDir, "compress.txt")
	// 实际实现会生成 sourceFile + ".zip"，即 compress.txt.zip
	destFile := filepath.Join(tmpDir, "compress.txt.zip")
	content := "content to compress with zip"

	ioutil.WriteFile(sourceFile, []byte(content), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "compress",
			"path":      sourceFile,
			"options": map[string]interface{}{
				"compression": "zip",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "success": true 而非 "status": "success"
	if result["success"] != true {
		t.Errorf("期望 success=true，得到 %v", result["success"])
	}

	// 验证压缩文件存在
	if _, err := os.Stat(destFile); err != nil {
		t.Error("压缩文件应该存在")
	}
}

// TestFileOperationsTool_Execute_DecompressGzip 测试 Gzip 解压操作
func TestFileOperationsTool_Execute_DecompressGzip(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	// 创建一个 gzip 文件
	compressedFile := filepath.Join(tmpDir, "compressed.txt.gz")
	originalContent := "original gzip content"

	file, _ := os.Create(compressedFile)
	writer := gzip.NewWriter(file)
	writer.Write([]byte(originalContent))
	writer.Close()
	file.Close()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "decompress",
			"path":      compressedFile,
			"options": map[string]interface{}{
				"compression": "gzip",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "success": true 而非 "status": "success"
	if result["success"] != true {
		t.Errorf("期望 success=true，得到 %v", result["success"])
	}
}

// TestFileOperationsTool_Execute_DecompressZip 测试 Zip 解压操作
func TestFileOperationsTool_Execute_DecompressZip(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	// 创建一个 zip 文件
	zipFile := filepath.Join(tmpDir, "test.zip")
	archive, err := os.Create(zipFile)
	if err != nil {
		t.Fatalf("创建 zip 文件失败：%v", err)
	}

	// 使用 archive/zip 包创建文件
	zipWriter := zip.NewWriter(archive)
	w, _ := zipWriter.Create("test.txt")
	w.Write([]byte("zip content"))
	zipWriter.Close()
	archive.Close()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "decompress",
			"path":      zipFile,
			"options": map[string]interface{}{
				"compression": "zip",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// 实现返回 "success": true 而非 "status": "success"
	if result["success"] != true {
		t.Errorf("期望 success=true，得到 %v", result["success"])
	}
}

// TestFileOperationsTool_Execute_ParseJSON 测试 JSON 解析
func TestFileOperationsTool_Execute_ParseJSON(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	jsonFile := filepath.Join(tmpDir, "test.json")
	jsonContent := `{"name": "John", "age": 30}`
	ioutil.WriteFile(jsonFile, []byte(jsonContent), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "parse",
			"path":      jsonFile,
			"options": map[string]interface{}{
				"format": "json",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// parse 操作返回嵌套的 result 结构
	if _, ok := result["result"]; !ok {
		t.Error("result 应该存在")
	}
}

// TestFileOperationsTool_Execute_AnalyzeFile 测试文件分析
func TestFileOperationsTool_Execute_AnalyzeFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "analyze.txt")
	content := "line1\nline2\nline3\n"
	ioutil.WriteFile(testFile, []byte(content), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "analyze",
			"path":      testFile,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// analyze 操作返回嵌套的 result 结构
	analyzeResult, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatal("result 应该存在并且是 map")
	}
	if _, ok := analyzeResult["line_count"]; !ok {
		t.Error("line_count 应该存在")
	}
	if _, ok := analyzeResult["size"]; !ok {
		t.Error("size 应该存在")
	}
	if _, ok := analyzeResult["mime_type"]; !ok {
		t.Error("mime_type 应该存在")
	}
}

// TestFileOperationsTool_Execute_InvalidOperation 测试无效操作
func TestFileOperationsTool_Execute_InvalidOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "invalid_op",
			"path":      tmpDir,
		},
	}

	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Error("无效操作应该返回错误")
	}
}

// TestFileOperationsTool_ValidatePath_Extended 测试路径验证扩展
func TestFileOperationsTool_ValidatePath_Extended(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)

	tests := []struct {
		name        string
		path        string
		shouldError bool
	}{
		{
			name:        "禁止路径 /etc",
			path:        "/etc/passwd",
			shouldError: true,
		},
		{
			name:        "禁止路径 /sys",
			path:        "/sys/config",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.validatePath(tt.path)
			if tt.shouldError && err == nil {
				t.Error("期望错误，但未发生")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("不期望错误，但发生了：%v", err)
			}
		})
	}
}

// TestFileOperationsTool_CalculateChecksum 测试校验和计算
func TestFileOperationsTool_CalculateChecksum(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "checksum.txt")
	content := "content for checksum"
	ioutil.WriteFile(testFile, []byte(content), 0644)

	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"operation": "info",
			"path":      testFile,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("执行失败：%v", err)
		return
	}

	result := output.Result.(map[string]interface{})
	// info 操作返回嵌套的 result 结构
	infoResult, ok := result["result"].(map[string]interface{})
	if !ok {
		t.Fatal("result 应该存在并且是 map")
	}
	if _, ok := infoResult["sha256"]; !ok {
		t.Error("sha256 应该存在")
	}
	if _, ok := infoResult["md5"]; !ok {
		t.Error("md5 应该存在")
	}
}
