package session

import (
	"context"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
)

func TestRecorderPersistsClearedConfiguration(t *testing.T) {
	for _, backend := range []string{"memory", "file"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			var store Store = NewMemoryStore()
			if backend == "file" {
				var err error
				store, err = NewFileStore(t.TempDir())
				require.NoError(t, err)
			}
			recorder, _, err := OpenRecorder(ctx, store, "s")
			require.NoError(t, err)
			require.NoError(t, recorder.SyncSystem(ctx, ""))
			require.NoError(t, recorder.SyncTools(ctx, nil))
			require.Zero(t, recorder.Seq(), "初始空配置不需要额外事件")
			tools := []ToolDefinition{{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}
			require.NoError(t, recorder.SyncSystem(ctx, "旧提示词"))
			require.NoError(t, recorder.SyncTools(ctx, tools))
			recorder, _, err = OpenRecorder(ctx, store, "s")
			require.NoError(t, err)
			require.NoError(t, recorder.SyncSystem(ctx, ""))
			require.NoError(t, recorder.SyncTools(ctx, nil))
			events, err := store.Read(ctx, "s", 0)
			require.NoError(t, err)
			trajectory, err := BuildTrajectory(events)
			require.NoError(t, err)
			require.Empty(t, trajectory.System)
			require.Empty(t, trajectory.Tools)

			recorder, _, err = OpenRecorder(ctx, store, "s")
			require.NoError(t, err)
			clearedAt := recorder.Seq()
			require.NoError(t, recorder.SyncSystem(ctx, ""))
			require.NoError(t, recorder.SyncTools(ctx, []ToolDefinition{}))
			require.Equal(t, clearedAt, recorder.Seq(), "重开后重复空值不应追加事件")
			require.NoError(t, recorder.SyncTools(ctx, nil))
			require.Equal(t, clearedAt, recorder.Seq(), "nil 与空工具集含义一致")

			require.NoError(t, recorder.SyncSystem(ctx, "新提示词"))
			require.NoError(t, recorder.SyncTools(ctx, tools))
			events, err = store.Read(ctx, "s", 0)
			require.NoError(t, err)
			trajectory, err = BuildTrajectory(events)
			require.NoError(t, err)
			require.Equal(t, "新提示词", trajectory.System)
			require.Equal(t, tools, trajectory.Tools)
		})
	}
}
