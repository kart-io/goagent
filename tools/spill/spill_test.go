package spill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

func dispatcher(t *testing.T, s *Store, extra ...interfaces.Tool) *tools.Dispatcher {
	t.Helper()
	registry := tools.NewRegistry()
	for _, tool := range append(extra, s.ReadTool()) {
		require.NoError(t, registry.Register(tool))
	}
	d, err := tools.NewDispatcher(registry, tools.DispatchConfig{Pipeline: &tools.Pipeline{Post: []tools.PostHook{s.Hook()}}})
	require.NoError(t, err)
	return d
}

func fixed(name string, result any) interfaces.Tool {
	return tools.NewBaseTool(name, "test", `{"type":"object"}`, func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
		return &interfaces.ToolOutput{Success: true, Result: result}, nil
	})
}

// 模型看到的分页默认值必须与省略 limit 时的实际读取边界一致。
func TestReadToolSchemaDefaultMatchesPagination(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 1)
	require.NoError(t, err)
	tool := s.ReadTool()
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal([]byte(tool.ArgsSchema()), &schema))
	var describedLimit int
	_, err = fmt.Sscanf(schema.Properties["limit"].Description, "最多读取的行数，默认 %d", &describedLimit)
	require.NoError(t, err)
	require.Equal(t, constants.DefaultSpillReadLimitLines, describedLimit)

	total := describedLimit + 2
	require.NoError(t, os.WriteFile(filepath.Join(dir, "page.txt"), []byte(strings.Repeat("一行\n", total)), 0o600))
	for _, tc := range []struct {
		name    string
		args    map[string]any
		wantEnd int
	}{
		{name: "省略上限", args: map[string]any{"path": "page.txt"}, wantEnd: describedLimit},
		{name: "显式覆盖上限", args: map[string]any{"path": "page.txt", "limit": 1}, wantEnd: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tool.Invoke(context.Background(), &interfaces.ToolInput{Args: tc.args})
			require.NoError(t, err)
			require.True(t, out.Success)
			result := out.Result.(map[string]any)
			require.Equal(t, tc.wantEnd, result["end_line"])
			require.Equal(t, total, result["total_lines"])
			require.Equal(t, tc.wantEnd, strings.Count(result["content"].(string), "\n"))
		})
	}
}

func TestSpill(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 1000)
	require.NoError(t, err)
	big := strings.Repeat("中", 3000)
	d := dispatcher(t, s, fixed("fetch", map[string]any{"body": big}), fixed("small", "ok"))
	ctx := tools.WithSessionID(context.Background(), "s/1")

	out, err := d.Invoke(ctx, tools.ToolRequest{Name: "fetch", Input: &interfaces.ToolInput{CallID: "c/1", Args: map[string]any{}}})
	require.NoError(t, err)
	res := out.Result.(map[string]any)
	require.Equal(t, true, res["truncated"])
	require.Equal(t, "s_1/fetch-c_1.txt", res["spill_path"])
	require.Equal(t, constants.SpillPreviewRunes, len([]rune(res["preview"].(string))))
	data, err := os.ReadFile(filepath.Join(dir, "s_1/fetch-c_1.txt"))
	require.NoError(t, err)
	require.Contains(t, string(data), big)

	read, err := d.Invoke(ctx, tools.ToolRequest{Name: constants.ToolReadSpill, Input: &interfaces.ToolInput{Args: map[string]any{"path": res["spill_path"], "limit": float64(1)}}})
	require.NoError(t, err)
	require.Equal(t, "     1\t== body ==\n", read.Result.(map[string]any)["content"])

	out, err = d.Invoke(ctx, tools.ToolRequest{Name: "small", Input: &interfaces.ToolInput{Args: map[string]any{}}})
	require.NoError(t, err)
	require.Equal(t, "ok", out.Result)

	// 目录外与已清理的文件交给模型，不中止运行。
	for _, p := range []string{"../outside.txt", "s_1/missing.txt"} {
		read, err = d.Invoke(ctx, tools.ToolRequest{Name: constants.ToolReadSpill, Input: &interfaces.ToolInput{Args: map[string]any{"path": p}}})
		require.NoError(t, err)
		_, rejected := tools.RejectedResult(read)
		require.True(t, rejected, p)
	}

	require.NoError(t, os.Chtimes(filepath.Join(dir, "s_1/fetch-c_1.txt"), time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))
	require.NoError(t, s.Prune(time.Hour))
	_, err = os.Stat(filepath.Join(dir, "s_1/fetch-c_1.txt"))
	require.True(t, os.IsNotExist(err))
}

// 写入失败时仍返回预览与原因，调用不失败。
func TestSpillWriteFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 10)
	require.NoError(t, err)
	// 会话目录的位置被普通文件占据，写入必然失败。
	require.NoError(t, os.WriteFile(filepath.Join(dir, "default"), nil, 0o600))
	d := dispatcher(t, s, fixed("fetch", strings.Repeat("x", 100)))
	out, err := d.Invoke(context.Background(), tools.ToolRequest{Name: "fetch", Input: &interfaces.ToolInput{CallID: "c", Args: map[string]any{}}})
	require.NoError(t, err)
	res := out.Result.(map[string]any)
	require.Nil(t, res["spill_path"])
	require.Contains(t, res["note"], "could not be saved")
}
