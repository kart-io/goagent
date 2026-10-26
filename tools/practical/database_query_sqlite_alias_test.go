//go:build cgo

package practical

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
)

func TestDatabaseQueryAdvertisedSQLiteDriver(t *testing.T) {
	tool := NewDatabaseQueryTool()
	defer tool.Close()
	output, err := tool.Execute(context.Background(), &interfaces.ToolInput{Args: map[string]any{
		"connection": map[string]any{"driver": "sqlite", "dsn": ":memory:", "connection_id": "sqlite-alias"},
		"operation":  "query", "query": "SELECT 42 AS answer",
	}})
	require.NoError(t, err, "the driver advertised by ArgsSchema must resolve to the registered sqlite3 driver")
	require.True(t, output.Success)
	require.Equal(t, [][]any{{int64(42)}}, output.Result.(map[string]any)["rows"])
}
