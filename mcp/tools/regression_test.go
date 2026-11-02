package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 工具不执行命令时不能向调用方报告成功。
func TestShellExecuteTool_ReportsNotSupported(t *testing.T) {
	res, err := NewShellExecuteTool().Execute(context.Background(), toolInput{"command": "ls"})
	require.Error(t, err)
	assert.False(t, res.Success)
	assert.Equal(t, "NOT_SUPPORTED", res.ErrorCode)
}

func TestWriteFileTool_RejectsUnknownMode(t *testing.T) {
	tool := NewWriteFileTool()
	path := filepath.Join(t.TempDir(), "a.txt")
	writeTestFile(t, path, "原内容")

	input := toolInput{"path": path, "content": "新内容", "mode": "apend"}
	assert.Error(t, tool.Validate(input))

	res, err := tool.Execute(context.Background(), input)
	require.Error(t, err)
	assert.Equal(t, "INVALID_INPUT", res.ErrorCode)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "原内容", string(got), "拼错模式不得覆盖原文件")

	assert.NoError(t, tool.Validate(toolInput{"path": path, "content": "c", "mode": "append"}))
	assert.NoError(t, tool.Validate(toolInput{"path": path, "content": "c", "mode": "overwrite"}))
}

// 起始目录是调用方明确指定的，即使以点开头也不算隐藏项。
func TestListDirectoryTool_RecursiveFromDotRoot(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "x")
	writeTestFile(t, filepath.Join(dir, ".hidden-root", "b.txt"), "x")
	t.Chdir(dir)

	res, err := NewListDirectoryTool().Execute(context.Background(), toolInput{"path": ".", "recursive": true})
	require.NoError(t, err)
	assert.Contains(t, entryNames(t, resultData(t, res)["files"]), "a.txt")

	res, err = NewListDirectoryTool().Execute(context.Background(), toolInput{"path": ".hidden-root", "recursive": true})
	require.NoError(t, err)
	assert.Contains(t, entryNames(t, resultData(t, res)["files"]), "b.txt")
}

func TestSearchFilesTool_MaxResultsAcceptsGoIntegers(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "x")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "x")

	for name, limit := range map[string]interface{}{"int": 1, "int64": int64(1), "float64": float64(1)} {
		res, err := NewSearchFilesTool().Execute(context.Background(), toolInput{"path": dir, "pattern": "*.txt", "max_results": limit})
		require.NoError(t, err, name)
		assert.Equal(t, 1, resultData(t, res)["count"], name)
	}
}

func TestHTTPRequestTool_TimeoutAcceptsGoIntegers(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	start := time.Now()
	_, err := NewHTTPRequestTool().Execute(context.Background(), toolInput{"url": server.URL, "timeout": 1})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second, "应使用传入的 1 秒超时，而不是默认的 30 秒")
}

func TestJSONParseTool_Path(t *testing.T) {
	tool := NewJSONParseTool()
	doc := `{"user":{"name":"Ann","tags":["a","b"]},"items":[{"id":7}]}`

	for path, want := range map[string]interface{}{
		"$":              map[string]interface{}{"user": map[string]interface{}{"name": "Ann", "tags": []interface{}{"a", "b"}}, "items": []interface{}{map[string]interface{}{"id": float64(7)}}},
		"$.user.name":    "Ann",
		"$.user.tags[1]": "b",
		"$.items[0].id":  float64(7),
	} {
		res, err := tool.Execute(context.Background(), toolInput{"json": doc, "path": path})
		require.NoError(t, err, path)
		assert.Equal(t, want, resultData(t, res)["value"], path)
	}

	for _, path := range []string{"$.missing", "$.user.tags[5]", "$.user.name.x", "$.user.tags[x]", "user.name", "$.items[0"} {
		res, err := tool.Execute(context.Background(), toolInput{"json": doc, "path": path})
		require.Error(t, err, path)
		assert.Equal(t, "PATH_ERROR", res.ErrorCode, path)
	}

	res, err := tool.Execute(context.Background(), toolInput{"json": doc})
	require.NoError(t, err)
	assert.NotContains(t, resultData(t, res), "value", "未传 path 时只返回 parsed")
}
