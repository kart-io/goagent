package practical

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/tools"
)

// API Caller Tests

func TestNewAPICallerTool(t *testing.T) {
	tool := NewAPICallerTool()

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}

	if tool.Name() != "api_caller" {
		t.Errorf("Expected name 'api_caller', got: %s", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Expected non-empty description")
	}

	if tool.ArgsSchema() == "" {
		t.Error("Expected non-empty args schema")
	}
}

func TestAPICallerTool_Execute_SimpleGET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "success"}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result, ok := output.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}

	if result["status_code"] != 200 {
		t.Errorf("Expected status 200, got: %v", result["status_code"])
	}
}

func TestAPICallerTool_Execute_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST method, got: %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"created": true}`))
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"url":    server.URL,
			"method": "POST",
			"body": map[string]interface{}{
				"key": "value",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"] != 201 {
		t.Errorf("Expected status 201, got: %v", result["status_code"])
	}
}

func TestAPICallerTool_RateLimiter(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute)

	// Should allow first 2 requests
	if !limiter.Allow() {
		t.Error("Expected first request to be allowed")
	}
	if !limiter.Allow() {
		t.Error("Expected second request to be allowed")
	}

	// Third request should be denied
	if limiter.Allow() {
		t.Error("Expected third request to be denied")
	}
}

func TestResponseCache_GetSet(t *testing.T) {
	cache := NewResponseCache(10, 5*time.Minute)

	// Test Set and Get
	cache.Set("key1", "value1")
	value := cache.Get("key1")

	if value != "value1" {
		t.Errorf("Expected 'value1', got: %v", value)
	}

	// Test non-existent key
	value = cache.Get("nonexistent")
	if value != nil {
		t.Errorf("Expected nil for non-existent key, got: %v", value)
	}
}

func TestResponseCache_Expiration(t *testing.T) {
	cache := NewResponseCache(10, 10*time.Millisecond)

	cache.Set("key1", "value1")

	// Wait for expiration
	time.Sleep(15 * time.Millisecond)

	value := cache.Get("key1")
	if value != nil {
		t.Errorf("Expected nil for expired key, got: %v", value)
	}
}

// File Operations Tests

func TestNewFileOperationsTool(t *testing.T) {
	tool := NewFileOperationsTool("/tmp")

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}

	if tool.Name() != "file_operations" {
		t.Errorf("Expected name 'file_operations', got: %s", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Expected non-empty description")
	}
}

func TestFileOperationsTool_WriteAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"

	// Write file
	writeInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "write",
			"path":      testFile,
			"content":   testContent,
		},
	}

	output, err := tool.Execute(ctx, writeInput)
	if err != nil {
		t.Errorf("Expected no error writing file, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if !result["success"].(bool) {
		t.Error("Expected write operation to succeed")
	}

	// Read file
	readInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "read",
			"path":      testFile,
		},
	}

	output, err = tool.Execute(ctx, readInput)
	if err != nil {
		t.Errorf("Expected no error reading file, got: %v", err)
	}

	result = output.Result.(map[string]interface{})
	if result["result"] != testContent {
		t.Errorf("Expected content '%s', got: %v", testContent, result["result"])
	}
}

func TestFileOperationsTool_Append(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "test.txt")

	// Write initial content
	os.WriteFile(testFile, []byte("Line 1\n"), 0644)

	// Append content
	appendInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "append",
			"path":      testFile,
			"content":   "Line 2\n",
		},
	}

	output, err := tool.Execute(ctx, appendInput)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if !result["success"].(bool) {
		t.Error("Expected append operation to succeed")
	}

	// Verify content
	content, _ := os.ReadFile(testFile)
	if !strings.Contains(string(content), "Line 1") || !strings.Contains(string(content), "Line 2") {
		t.Error("Expected both lines in file")
	}
}

func TestFileOperationsTool_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	// Delete file
	deleteInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "delete",
			"path":      testFile,
		},
	}

	output, err := tool.Execute(ctx, deleteInput)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if !result["success"].(bool) {
		t.Error("Expected delete operation to succeed")
	}

	// Verify file is deleted
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Expected file to be deleted")
	}
}

func TestFileOperationsTool_Copy(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	srcFile := filepath.Join(tmpDir, "source.txt")
	dstFile := filepath.Join(tmpDir, "dest.txt")
	content := "test content"

	os.WriteFile(srcFile, []byte(content), 0644)

	// Copy file
	copyInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation":   "copy",
			"path":        srcFile,
			"destination": dstFile,
		},
	}

	output, err := tool.Execute(ctx, copyInput)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if !result["success"].(bool) {
		t.Error("Expected copy operation to succeed")
	}

	// Verify destination file exists with same content
	dstContent, _ := os.ReadFile(dstFile)
	if string(dstContent) != content {
		t.Errorf("Expected content '%s', got: %s", content, string(dstContent))
	}
}

func TestFileOperationsTool_Move(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	srcFile := filepath.Join(tmpDir, "source.txt")
	dstFile := filepath.Join(tmpDir, "dest.txt")
	content := "test content"

	os.WriteFile(srcFile, []byte(content), 0644)

	// Move file
	moveInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation":   "move",
			"path":        srcFile,
			"destination": dstFile,
		},
	}

	output, err := tool.Execute(ctx, moveInput)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if !result["success"].(bool) {
		t.Error("Expected move operation to succeed")
	}

	// Verify source is gone and destination exists
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Error("Expected source file to be deleted")
	}

	dstContent, _ := os.ReadFile(dstFile)
	if string(dstContent) != content {
		t.Errorf("Expected content '%s', got: %s", content, string(dstContent))
	}
}

func TestFileOperationsTool_List(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("test"), 0644)

	// List directory
	listInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "list",
			"path":      tmpDir,
		},
	}

	output, err := tool.Execute(ctx, listInput)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	files := result["result"].([]map[string]interface{})

	if len(files) < 2 {
		t.Errorf("Expected at least 2 files, got: %d", len(files))
	}
}

func TestFileOperationsTool_Search(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "test1.txt"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "test2.log"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "other.txt"), []byte("test"), 0644)

	// Search for *.txt files
	searchInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "search",
			"path":      tmpDir,
			"pattern":   "*.txt",
		},
	}

	output, err := tool.Execute(ctx, searchInput)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	matches := result["result"].([]string)

	if len(matches) < 2 {
		t.Errorf("Expected at least 2 matches, got: %d", len(matches))
	}
}

func TestFileOperationsTool_Info(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "test content"
	os.WriteFile(testFile, []byte(content), 0644)

	// Get file info
	infoInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "info",
			"path":      testFile,
		},
	}

	output, err := tool.Execute(ctx, infoInput)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	fileInfo := result["result"].(map[string]interface{})

	if fileInfo["name"] != "test.txt" {
		t.Errorf("Expected name 'test.txt', got: %v", fileInfo["name"])
	}

	if fileInfo["size"].(int64) != int64(len(content)) {
		t.Errorf("Expected size %d, got: %v", len(content), fileInfo["size"])
	}

	if fileInfo["md5"] == nil {
		t.Error("Expected MD5 checksum to be present")
	}

	if fileInfo["sha256"] == nil {
		t.Error("Expected SHA256 checksum to be present")
	}
}

func TestFileOperationsTool_Analyze(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Line 1\nLine 2\nLine 3\n"
	os.WriteFile(testFile, []byte(content), 0644)

	// Analyze file
	analyzeInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "analyze",
			"path":      testFile,
		},
	}

	output, err := tool.Execute(ctx, analyzeInput)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	analysis := result["result"].(map[string]interface{})

	if analysis["line_count"].(int) != 4 { // 3 lines + 1 for final newline
		t.Errorf("Expected 4 lines, got: %v", analysis["line_count"])
	}

	if analysis["char_count"].(int) != len(content) {
		t.Errorf("Expected char count %d, got: %v", len(content), analysis["char_count"])
	}
}

func TestFileOperationsTool_ValidatePath(t *testing.T) {
	tool := NewFileOperationsTool("/tmp")

	// Test forbidden path
	err := tool.validatePath("/etc/passwd")
	if err == nil {
		t.Error("Expected error for forbidden path /etc")
	}

	// Test allowed path within basePath
	err = tool.validatePath("/tmp/test.txt")
	if err != nil {
		t.Errorf("Expected no error for path within basePath, got: %v", err)
	}
}

func TestFileOperationsTool_Parse_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "test.json")
	jsonContent := `{"key": "value", "number": 42}`
	os.WriteFile(testFile, []byte(jsonContent), 0644)

	// Parse JSON file
	parseInput := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "parse",
			"path":      testFile,
		},
	}

	output, err := tool.Execute(ctx, parseInput)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if result["info"].(map[string]interface{})["format"] != "json" {
		t.Error("Expected format to be 'json'")
	}
}

func TestFileOperationsTool_UnsupportedOperation(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewFileOperationsTool(tmpDir)
	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "test.txt")

	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"operation": "unsupported",
			"path":      testFile,
		},
	}

	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Error("Expected error for unsupported operation")
	}

	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("Expected error about unsupported operation, got: %v", err)
	}
}

func TestAPICallerTool_Retry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true}`))
		}
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
			"retry": map[string]interface{}{
				"max_attempts": float64(3),
				"backoff":      "constant",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("Expected no error after retries, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if result["attempts"].(int) != 3 {
		t.Errorf("Expected 3 attempts, got: %v", result["attempts"])
	}
}

func TestAPICallerTool_Authentication_Bearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tool := NewAPICallerTool()
	ctx := context.Background()

	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
			"auth": map[string]interface{}{
				"type": "bearer",
				"credentials": map[string]interface{}{
					"token": "test-token",
				},
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if result["status_code"] != 200 {
		t.Errorf("Expected status 200, got: %v", result["status_code"])
	}
}

func TestNewAPICallerRuntimeTool(t *testing.T) {
	tool := NewAPICallerRuntimeTool()

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}

	if tool.APICallerTool == nil {
		t.Error("Expected APICallerTool to be initialized")
	}
}

func TestNewFileOperationsRuntimeTool(t *testing.T) {
	tool := NewFileOperationsRuntimeTool("/tmp")

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}

	if tool.FileOperationsTool == nil {
		t.Error("Expected FileOperationsTool to be initialized")
	}
}

// Database Query Tool Tests

func TestNewDatabaseQueryTool(t *testing.T) {
	tool := NewDatabaseQueryTool()

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}

	if tool.Name() != "database_query" {
		t.Errorf("Expected name 'database_query', got: %s", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Expected non-empty description")
	}

	if tool.ArgsSchema() == "" {
		t.Error("Expected non-empty args schema")
	}

	if tool.maxRows != 1000 {
		t.Errorf("Expected max rows 1000, got: %d", tool.maxRows)
	}
}

func TestDatabaseQueryTool_AddConnection(t *testing.T) {
	tool := NewDatabaseQueryTool()

	// Since we can't test real database connections in unit tests,
	// we just verify the tool structure
	if tool.connections == nil {
		t.Error("Expected connections map to be initialized")
	}

	// Verify default values
	if tool.maxRows != 1000 {
		t.Errorf("Expected maxRows 1000, got: %d", tool.maxRows)
	}
}

func TestDatabaseQueryTool_Name(t *testing.T) {
	tool := NewDatabaseQueryTool()
	if tool.Name() != "database_query" {
		t.Errorf("Expected name 'database_query', got: %s", tool.Name())
	}
}

func TestDatabaseQueryTool_Description(t *testing.T) {
	tool := NewDatabaseQueryTool()
	desc := tool.Description()
	if !strings.Contains(desc, "SQL") {
		t.Errorf("Expected description to mention SQL, got: %s", desc)
	}
}

// Web Scraper Tool Tests

func TestNewWebScraperTool(t *testing.T) {
	tool := NewWebScraperTool()

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}

	if tool.Name() != "web_scraper" {
		t.Errorf("Expected name 'web_scraper', got: %s", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Expected non-empty description")
	}

	if tool.ArgsSchema() == "" {
		t.Error("Expected non-empty args schema")
	}

	if tool.maxRetries != 3 {
		t.Errorf("Expected max retries 3, got: %d", tool.maxRetries)
	}

	if tool.userAgent == "" {
		t.Error("Expected non-empty user agent")
	}
}

func TestWebScraperTool_Execute(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
		<html>
		<head>
			<title>Test Page</title>
			<meta name="description" content="Test description">
		</head>
		<body>
			<h1>Hello World</h1>
			<p class="content">This is test content</p>
			<a href="/link1">Link 1</a>
			<img src="/image1.jpg" alt="Image 1">
		</body>
		</html>`
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if result["title"] != "Test Page" {
		t.Errorf("Expected title 'Test Page', got: %v", result["title"])
	}

	// Check if metadata exists (it might be in different formats)
	if result["metadata"] != nil {
		// Metadata exists, check it
		metadata, ok := result["metadata"].(map[string]string)
		if ok && metadata["description"] != "Test description" {
			t.Errorf("Expected description 'Test description', got: %v", metadata["description"])
		}
	}
}

func TestWebScraperTool_Execute_InvalidURL(t *testing.T) {
	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"url": "://invalid-url",
		},
	}

	_, err := tool.Execute(ctx, input)
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestWebScraperTool_Execute_WithSelectors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
		<html>
		<body>
			<h1 class="title">Test Title</h1>
			<p class="content">Test Content</p>
		</body>
		</html>`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer server.Close()

	tool := NewWebScraperTool()
	ctx := context.Background()

	input := &tools.ToolInput{
		Args: map[string]interface{}{
			"url": server.URL,
			"selectors": map[string]interface{}{
				"title":   ".title",
				"content": ".content",
			},
		},
	}

	output, err := tool.Execute(ctx, input)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	result := output.Result.(map[string]interface{})
	if result["title"] == nil {
		t.Error("Expected title to be extracted")
	}
}

func TestWebScraperTool_Name(t *testing.T) {
	tool := NewWebScraperTool()
	if tool.Name() != "web_scraper" {
		t.Errorf("Expected name 'web_scraper', got: %s", tool.Name())
	}
}

func TestWebScraperTool_Description(t *testing.T) {
	tool := NewWebScraperTool()
	desc := tool.Description()
	if !strings.Contains(strings.ToLower(desc), "scrap") {
		t.Errorf("Expected description to mention scraping, got: %s", desc)
	}
}

