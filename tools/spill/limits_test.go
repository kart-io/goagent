package spill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

func TestSpillZeroLimitsDoNotTruncate(t *testing.T) {
	dir := t.TempDir()
	s, err := NewWithConfig(dir, Config{MaxBytes: 1000})
	require.NoError(t, err)
	big := strings.Repeat("中", 3000)
	d := dispatcher(t, s, fixed("fetch", big))
	ctx := tools.WithSessionID(context.Background(), "s")
	out, err := d.Invoke(ctx, tools.ToolRequest{Name: "fetch", Input: &interfaces.ToolInput{CallID: "c", Args: map[string]any{}}})
	require.NoError(t, err)
	res := out.Result.(map[string]any)
	require.Equal(t, true, res["truncated"])
	require.Len(t, []rune(res["preview"].(string)), 3000, "旧预设截到 1500")

	lines := strings.Repeat(strings.Repeat("长", 2500)+"\n", constants.DefaultSpillReadLimitLines+5)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "page.txt"), []byte(lines), 0o600))
	read, err := s.ReadTool().Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"path": "page.txt"}})
	require.NoError(t, err)
	result := read.Result.(map[string]any)
	require.Equal(t, constants.DefaultSpillReadLimitLines+5, result["end_line"], "省略 limit 时读到末尾")
	require.NotContains(t, result["content"], "…", "单行不截断")

	_, err = NewWithConfig(dir, Config{MaxBytes: 1, PreviewRunes: -1})
	require.Error(t, err)
}
