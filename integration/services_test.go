package integration_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/store"
	pgstore "github.com/kart-io/goagent/store/postgres"
	redisstore "github.com/kart-io/goagent/store/redis"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

func serviceEndpoint(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Skip("real service endpoint not configured: " + name)
	}
	return value
}

// This suite uses only explicitly supplied test services. It never chooses a
// production/default endpoint and isolates every run using a unique namespace.
func exerciseStore(t *testing.T, s store.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	namespace := []string{"validation", uuid.NewString()}
	other := []string{"validation", uuid.NewString()}
	defer func() {
		require.NoError(t, s.Clear(context.Background(), namespace))
		require.NoError(t, s.Clear(context.Background(), other))
	}()
	require.NoError(t, s.Put(ctx, namespace, "first", map[string]any{"text": "甲乙\n原文", "count": 3}))
	require.NoError(t, s.Put(ctx, other, "first", map[string]any{"text": "other tenant"}))
	value, err := s.Get(ctx, namespace, "first")
	require.NoError(t, err)
	require.Equal(t, "甲乙\n原文", value.Value.(map[string]any)["text"])
	require.Equal(t, float64(3), value.Value.(map[string]any)["count"])
	require.NoError(t, s.Put(ctx, namespace, "first", map[string]any{"text": "updated"}))
	value, err = s.Get(ctx, namespace, "first")
	require.NoError(t, err)
	require.Equal(t, "updated", value.Value.(map[string]any)["text"])
	require.NoError(t, s.Put(ctx, namespace, "second", "value"))
	canceled, cancelOperation := context.WithCancel(ctx)
	cancelOperation()
	require.ErrorIs(t, s.Put(canceled, namespace, "canceled", "must not be stored"), context.Canceled)
	_, err = s.Get(canceled, namespace, "first")
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, s.Delete(canceled, namespace, "first"), context.Canceled)
	_, err = s.List(canceled, namespace)
	require.ErrorIs(t, err, context.Canceled)
	_, err = s.Search(canceled, namespace, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, s.Clear(canceled, namespace), context.Canceled)
	keys, err := s.List(ctx, namespace)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"first", "second"}, keys)
	values, err := s.Search(ctx, namespace, nil)
	require.NoError(t, err)
	require.Len(t, values, 2)
	require.NoError(t, s.Delete(ctx, namespace, "first"))
	keys, err = s.List(ctx, namespace)
	require.NoError(t, err)
	require.Equal(t, []string{"second"}, keys)
	require.NoError(t, s.Clear(ctx, namespace))
	keys, err = s.List(ctx, namespace)
	require.NoError(t, err)
	require.Empty(t, keys)
	value, err = s.Get(ctx, other, "first")
	require.NoError(t, err)
	require.Equal(t, "other tenant", value.Value.(map[string]any)["text"])
}

func TestRealPostgresNamespaceStore(t *testing.T) {
	dsn := serviceEndpoint(t, "GOAGENT_TEST_POSTGRES_DSN")
	table := "goagent_validation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	defer func() { _, err := db.Exec(`DROP TABLE IF EXISTS "` + table + `"`); require.NoError(t, err) }()
	persistent, err := pgstore.New(dsn, pgstore.WithTableName(table), pgstore.WithLogLevel(logger.Silent))
	require.NoError(t, err)
	t.Cleanup(func() { _ = persistent.Close() })
	exerciseStore(t, persistent)
	namespace := []string{uuid.NewString()}
	require.NoError(t, persistent.Put(context.Background(), namespace, "restart", "persisted"))
	require.NoError(t, persistent.Close())
	reopened, err := pgstore.New(dsn, pgstore.WithTableName(table), pgstore.WithLogLevel(logger.Silent))
	require.NoError(t, err)
	defer reopened.Close()
	value, err := reopened.Get(context.Background(), namespace, "restart")
	require.NoError(t, err)
	require.Equal(t, "persisted", value.Value)
}

func TestRealRedisNamespaceStoreAndCheckpoints(t *testing.T) {
	address := serviceEndpoint(t, "GOAGENT_TEST_REDIS_ADDR")
	persistent, err := redisstore.New(address, redisstore.WithPrefix("validation:"+uuid.NewString()+":"))
	require.NoError(t, err)
	defer persistent.Close()
	exerciseStore(t, persistent)
	config := checkpoint.DefaultRedisCheckpointerConfig()
	config.Addr = address
	config.Prefix = "validation:" + uuid.NewString() + ":"
	config.TTL = 0
	config.EnableLock = true
	saver, err := checkpoint.NewRedisCheckpointer(config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = saver.Close() })
	id := uuid.NewString()
	t.Cleanup(func() {
		cleanup, err := checkpoint.NewRedisCheckpointer(config)
		if err != nil {
			t.Errorf("checkpoint cleanup connection: %v", err)
			return
		}
		defer cleanup.Close()
		require.NoError(t, cleanup.Delete(context.Background(), id))
	})
	snapshot := state.NewAgentState()
	snapshot.Set("answer", "甲乙")
	snapshot.Set("step", 2)
	require.NoError(t, saver.Save(context.Background(), id, snapshot))
	require.NoError(t, saver.Close())
	reopened, err := checkpoint.NewRedisCheckpointer(config)
	require.NoError(t, err)
	defer reopened.Close()
	defer reopened.Delete(context.Background(), id)
	loaded, err := reopened.Load(context.Background(), id)
	require.NoError(t, err)
	answer, ok := loaded.Get("answer")
	require.True(t, ok)
	require.Equal(t, "甲乙", answer)
	exists, err := reopened.Exists(context.Background(), id)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, reopened.Delete(context.Background(), id))
	exists, err = reopened.Exists(context.Background(), id)
	require.NoError(t, err)
	require.False(t, exists)
}
