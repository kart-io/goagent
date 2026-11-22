package memory_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/memory"
)

func TestFileStoreRollbackReplaysDeletions(t *testing.T) {
	for _, action := range []string{"Forget", "Purge"} {
		t.Run(action, func(t *testing.T) {
			ctx := memory.WithNamespace(t.Context(), "rollback", "alice")
			liveDir := t.TempDir()
			live := openRollbackMemory(t, liveDir)
			deleted, err := live.Remember(ctx, memory.Record{Content: "coffee origin Colombia", Importance: 0.8})
			require.NoError(t, err)
			retained, err := live.Remember(ctx, memory.Record{Content: "coffee origin Ethiopia", Importance: 0.8})
			require.NoError(t, err)
			query := memory.Query{Text: "coffee"}
			snapshot, err := live.Snapshot(ctx, query.Text)
			require.NoError(t, err)
			require.Len(t, snapshot.Refs, 2)

			backupDir := t.TempDir()
			require.NoError(t, os.CopyFS(backupDir, os.DirFS(liveDir)))
			requireRollbackV4Head(t, backupDir)

			switch action {
			case "Forget":
				require.NoError(t, live.Forget(ctx, deleted.ID))
			case "Purge":
				require.NoError(t, live.Purge(ctx))
			}
			// 宿主在覆盖存储之前从持久化的删除日志取出待重放条目。
			log, err := openRollbackMemory(t, liveDir).Deletions(ctx)
			require.NoError(t, err)
			var want []memory.Record
			switch action {
			case "Forget":
				require.Empty(t, log.Purges)
				require.Len(t, log.Forgotten, 1)
				require.Equal(t, deleted.ID, log.Forgotten[0].ID)
				require.False(t, log.Forgotten[0].At.IsZero())
				want = []memory.Record{retained}
			case "Purge":
				require.Empty(t, log.Forgotten)
				require.Len(t, log.Purges, 1)
				require.Equal(t, snapshot.Turn.Scope, log.Purges[0].ID)
				require.False(t, log.Purges[0].At.IsZero())
			}

			restoreDir := t.TempDir()
			require.NoError(t, os.CopyFS(restoreDir, os.DirFS(backupDir)))
			restored := openRollbackMemory(t, restoreDir)
			checkRecall := func(m *memory.Memory, expected ...memory.Record) {
				t.Helper()
				hits, err := m.Recall(ctx, query)
				require.NoError(t, err)
				gotRecords := make(map[string]string, len(hits))
				for _, hit := range hits {
					gotRecords[hit.ID] = hit.Content
				}
				retrieval, refs, err := m.Hydrate(ctx, snapshot, snapshot.Refs)
				require.NoError(t, err)
				gotIDs := make([]string, 0, len(refs))
				for _, ref := range refs {
					gotIDs = append(gotIDs, ref.ID)
				}
				wantRecords := make(map[string]string, len(expected))
				wantIDs := make([]string, 0, len(expected))
				for _, record := range expected {
					wantRecords[record.ID] = record.Content
					wantIDs = append(wantIDs, record.ID)
					require.Contains(t, retrieval.Text, record.Content)
				}
				require.Len(t, hits, len(expected))
				require.Equal(t, wantRecords, gotRecords)
				require.ElementsMatch(t, wantIDs, gotIDs)
				for _, record := range []memory.Record{deleted, retained} {
					if _, keep := wantRecords[record.ID]; !keep {
						require.NotContains(t, retrieval.Text, record.Content)
					}
				}
			}
			checkRecall(restored, deleted, retained)

			for _, forgotten := range log.Forgotten {
				require.NoError(t, restored.Forget(ctx, forgotten.ID))
			}
			for _, purged := range log.Purges {
				turn, err := restored.Turn(ctx)
				require.NoError(t, err)
				require.Equal(t, purged.ID, turn.Scope)
				require.NoError(t, restored.Purge(ctx))
			}
			checkRecall(openRollbackMemory(t, restoreDir), want...)
		})
	}
}

func openRollbackMemory(t *testing.T, dir string) *memory.Memory {
	t.Helper()
	store, err := durable.NewFileStore(dir)
	require.NoError(t, err)
	m, err := memory.New(memory.Config{
		Durable:         store,
		RollbackWindow:  time.Hour,
		StateCacheBytes: -1,
	})
	require.NoError(t, err)
	return m
}

func requireRollbackV4Head(t *testing.T, dir string) {
	t.Helper()
	found := false
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var header struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			return err
		}
		if header.Version == "memory-head/4" {
			found = true
		}
		return nil
	}))
	require.True(t, found, "备份包含 memory-head/4")
}
