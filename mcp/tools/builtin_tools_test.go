package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/mcp/core"
	"github.com/kart-io/goagent/mcp/toolbox"
)

type toolInput = map[string]interface{}

func resultData(t *testing.T, res *core.ToolResult) toolInput {
	t.Helper()
	require.NotNil(t, res)
	data, ok := res.Data.(toolInput)
	require.True(t, ok, "%T", res.Data)
	return data
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func entryNames(t *testing.T, files interface{}) []string {
	t.Helper()
	entries, ok := files.([]toolInput)
	require.True(t, ok, "%T", files)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry["name"].(string))
	}
	return names
}

func TestReadFileTool(t *testing.T) {
	ctx := context.Background()
	tool := NewReadFileTool()
	path := filepath.Join(t.TempDir(), "a.txt")
	writeTestFile(t, path, "内容")

	res, err := tool.Execute(ctx, toolInput{"path": path})
	require.NoError(t, err)
	assert.True(t, res.Success)
	data := resultData(t, res)
	assert.Equal(t, "内容", data["content"])
	assert.Equal(t, len("内容"), data["size"])
	assert.Equal(t, int64(len("内容")), res.Metadata["file_size"])

	res, err = tool.Execute(ctx, toolInput{"path": 1})
	require.Error(t, err)
	assert.Equal(t, "INVALID_INPUT", res.ErrorCode)

	res, err = tool.Execute(ctx, toolInput{"path": path + ".missing"})
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Equal(t, "FILE_READ_ERROR", res.ErrorCode)

	assert.NoError(t, tool.Validate(toolInput{"path": path}))
	assert.Error(t, tool.Validate(toolInput{"path": ""}))
	assert.Error(t, tool.Validate(toolInput{}))
	assert.False(t, tool.IsDangerous())
}

func TestWriteFileTool_Modes(t *testing.T) {
	ctx := context.Background()
	tool := NewWriteFileTool()
	path := filepath.Join(t.TempDir(), "a.txt")

	res, err := tool.Execute(ctx, toolInput{"path": path, "content": "一"})
	require.NoError(t, err)
	assert.Equal(t, "overwrite", resultData(t, res)["mode"], "默认覆盖写")

	res, err = tool.Execute(ctx, toolInput{"path": path, "content": "二", "mode": "append"})
	require.NoError(t, err)
	assert.Equal(t, len("二"), resultData(t, res)["bytes_written"])
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "一二", string(got))
	assert.Equal(t, int64(len("一二")), res.Metadata["file_size"])

	_, err = tool.Execute(ctx, toolInput{"path": path, "content": "三", "mode": "overwrite"})
	require.NoError(t, err)
	got, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "三", string(got))
}

func TestWriteFileTool_Directories(t *testing.T) {
	ctx := context.Background()
	tool := NewWriteFileTool()
	dir := t.TempDir()

	nested := filepath.Join(dir, "x", "y", "a.txt")
	_, err := tool.Execute(ctx, toolInput{"path": nested, "content": "c"})
	assert.ErrorIs(t, err, os.ErrNotExist, "默认不创建目录")

	_, err = tool.Execute(ctx, toolInput{"path": nested, "content": "c", "create_dirs": true})
	require.NoError(t, err)
	assert.FileExists(t, nested)

	blocker := filepath.Join(dir, "blocker")
	writeTestFile(t, blocker, "")
	res, err := tool.Execute(ctx, toolInput{"path": filepath.Join(blocker, "a.txt"), "content": "c", "create_dirs": true})
	require.Error(t, err)
	assert.Equal(t, "DIR_CREATE_ERROR", res.ErrorCode)

	res, err = tool.Execute(ctx, toolInput{"path": dir, "content": "c"})
	require.Error(t, err)
	assert.Equal(t, "FILE_WRITE_ERROR", res.ErrorCode)

	res, err = tool.Execute(ctx, toolInput{"path": dir, "content": "c", "mode": "append"})
	require.Error(t, err)
	assert.Equal(t, "FILE_OPEN_ERROR", res.ErrorCode)
}

func TestWriteFileTool_Validate(t *testing.T) {
	tool := NewWriteFileTool()
	assert.True(t, tool.IsDangerous())
	assert.True(t, tool.RequiresAuth())

	assert.NoError(t, tool.Validate(toolInput{"path": "a", "content": ""}), "允许写入空内容")
	assert.NoError(t, tool.Validate(toolInput{"path": "a", "content": nil}))
	for name, input := range map[string]toolInput{
		"缺少路径":   {"content": "c"},
		"路径类型错误": {"path": 1, "content": "c"},
		"路径为空":   {"path": "", "content": "c"},
		"缺少内容":   {"path": "a"},
		"内容类型错误": {"path": "a", "content": 1},
	} {
		assert.Error(t, tool.Validate(input), name)
	}
}

func TestListDirectoryTool(t *testing.T) {
	ctx := context.Background()
	tool := NewListDirectoryTool()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "x")
	writeTestFile(t, filepath.Join(dir, ".hidden"), "x")
	writeTestFile(t, filepath.Join(dir, "sub", "b.txt"), "x")
	writeTestFile(t, filepath.Join(dir, ".git", "config"), "x")

	res, err := tool.Execute(ctx, toolInput{"path": dir})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a.txt", "sub"}, entryNames(t, resultData(t, res)["files"]))

	res, err = tool.Execute(ctx, toolInput{"path": dir, "include_hidden": true})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a.txt", ".hidden", "sub", ".git"}, entryNames(t, resultData(t, res)["files"]))

	res, err = tool.Execute(ctx, toolInput{"path": dir, "recursive": true})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{filepath.Base(dir), "a.txt", "sub", "b.txt"},
		entryNames(t, resultData(t, res)["files"]), "递归时跳过隐藏文件和隐藏目录的内容")

	res, err = tool.Execute(ctx, toolInput{"path": dir, "recursive": true, "include_hidden": true})
	require.NoError(t, err)
	assert.Contains(t, entryNames(t, resultData(t, res)["files"]), "config")

	for _, recursive := range []bool{false, true} {
		res, err = tool.Execute(ctx, toolInput{"path": filepath.Join(dir, "missing"), "recursive": recursive})
		require.Error(t, err)
		assert.Equal(t, "DIR_LIST_ERROR", res.ErrorCode)
	}

	assert.NoError(t, tool.Validate(toolInput{"path": dir}))
	assert.Error(t, tool.Validate(toolInput{"path": ""}))
}

func TestSearchFilesTool(t *testing.T) {
	ctx := context.Background()
	tool := NewSearchFilesTool()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.go"), "package needle")
	writeTestFile(t, filepath.Join(dir, "sub", "b.go"), "package other")
	writeTestFile(t, filepath.Join(dir, "c.txt"), "needle")

	res, err := tool.Execute(ctx, toolInput{"path": dir, "pattern": "*.go"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a.go", "b.go"}, entryNames(t, resultData(t, res)["matches"]))

	res, err = tool.Execute(ctx, toolInput{"path": dir, "pattern": "*.go", "content": "needle"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a.go"}, entryNames(t, resultData(t, res)["matches"]), "同时按内容过滤")

	res, err = tool.Execute(ctx, toolInput{"path": dir, "pattern": "*.go", "max_results": float64(1)})
	require.NoError(t, err)
	assert.Equal(t, 1, resultData(t, res)["count"], "达到上限后停止遍历")

	res, err = tool.Execute(ctx, toolInput{"path": filepath.Join(dir, "missing"), "pattern": "*"})
	require.NoError(t, err, "遍历错误被跳过")
	assert.Equal(t, 0, resultData(t, res)["count"])

	assert.NoError(t, tool.Validate(toolInput{"path": dir, "pattern": "*"}))
	assert.Error(t, tool.Validate(toolInput{"pattern": "*"}))
	assert.Error(t, tool.Validate(toolInput{"path": dir}))
}

func TestHTTPRequestTool_Methods(t *testing.T) {
	type received struct{ method, body, header string }
	requests := make(chan received, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- received{r.Method, string(body), r.Header.Get("X-Test")}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	ctx := context.Background()
	tool := NewHTTPRequestTool()

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "post"} {
		// timeout 分支会重建请求，须同样带上请求头和请求体
		for _, extra := range []toolInput{{}, {"timeout": float64(5)}} {
			input := toolInput{
				"url": server.URL, "method": method, "body": "payload",
				"headers": toolInput{"X-Test": "v", "X-Ignored": 1},
			}
			for k, v := range extra {
				input[k] = v
			}

			res, err := tool.Execute(ctx, input)
			require.NoError(t, err, method)
			data := resultData(t, res)
			assert.Equal(t, http.StatusCreated, data["status_code"])
			assert.Equal(t, map[string]interface{}{"ok": true}, data["json"])

			got := <-requests
			assert.Equal(t, strings.ToUpper(method), got.method)
			wantBody := "payload"
			if got.method == http.MethodGet {
				wantBody = "" // resty 默认不发送 GET 请求体
			}
			assert.Equal(t, wantBody, got.body)
			assert.Equal(t, "v", got.header)
		}
	}

	_, err := tool.Execute(ctx, toolInput{"url": server.URL})
	require.NoError(t, err)
	assert.Equal(t, "GET", (<-requests).method, "默认使用 GET")
}

func TestHTTPRequestTool_NonJSONAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("plain"))
	}))
	defer server.Close()

	ctx := context.Background()
	tool := NewHTTPRequestTool()

	res, err := tool.Execute(ctx, toolInput{"url": server.URL})
	require.NoError(t, err)
	data := resultData(t, res)
	assert.Equal(t, "plain", data["body"])
	assert.Nil(t, data["json"])
	assert.Equal(t, len("plain"), data["size"])

	res, err = tool.Execute(ctx, toolInput{"url": server.URL, "method": "TRACE"})
	require.Error(t, err)
	assert.Equal(t, "METHOD_ERROR", res.ErrorCode)

	assert.NoError(t, tool.Validate(toolInput{"url": server.URL}))
	assert.Error(t, tool.Validate(toolInput{"url": ""}))
}

func TestHTTPRequestTool_RespectsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	res, err := NewHTTPRequestTool().Execute(ctx, toolInput{"url": server.URL})
	require.Error(t, err)
	assert.Equal(t, "HTTP_ERROR", res.ErrorCode)
}

func TestJSONParseTool(t *testing.T) {
	ctx := context.Background()
	tool := NewJSONParseTool()

	res, err := tool.Execute(ctx, toolInput{"json": `{"a":[1]}`})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"a": []interface{}{float64(1)}}, resultData(t, res)["parsed"])

	res, err = tool.Execute(ctx, toolInput{"json": `{`})
	require.Error(t, err)
	assert.Equal(t, "PARSE_ERROR", res.ErrorCode)

	assert.NoError(t, tool.Validate(toolInput{"json": "{}"}))
	assert.Error(t, tool.Validate(toolInput{"json": ""}))
}

// 防止该工具意外执行命令。
func TestShellExecuteTool_DoesNotRunCommand(t *testing.T) {
	tool := NewShellExecuteTool()
	marker := filepath.Join(t.TempDir(), "marker")

	res, err := tool.Execute(context.Background(), toolInput{"command": "touch " + marker})
	require.Error(t, err)
	assert.Equal(t, "touch "+marker, resultData(t, res)["command"])
	assert.NoFileExists(t, marker)

	assert.True(t, tool.IsDangerous())
	assert.True(t, tool.RequiresAuth())
	assert.NoError(t, tool.Validate(toolInput{"command": "ls"}))
	assert.Error(t, tool.Validate(toolInput{"command": ""}))
}

func TestRegisterBuiltinTools(t *testing.T) {
	box := toolbox.NewStandardToolBox()
	require.NoError(t, RegisterBuiltinTools(box))
	assert.Len(t, box.List(), len(BuiltinTools))

	for _, tool := range BuiltinTools {
		got, err := box.Get(tool.Name())
		require.NoError(t, err)
		assert.Equal(t, tool.Name(), got.Name())
		assert.Contains(t, ToolCategories, tool.Category())
	}

	assert.Error(t, RegisterBuiltinTools(box), "重复注册应失败")
}

func TestGetToolsByCategory(t *testing.T) {
	assert.Len(t, GetToolsByCategory("filesystem"), 4)
	assert.Len(t, GetToolsByCategory("network"), 1)
	assert.Empty(t, GetToolsByCategory("database"), "该分类没有内置工具")
	assert.Empty(t, GetToolsByCategory("unknown"))

	for _, category := range ToolCategories {
		assert.NotEmpty(t, CategoryDescriptions[category], category)
	}
}
