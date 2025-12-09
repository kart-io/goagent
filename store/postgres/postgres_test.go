package postgres

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) (*Store, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	// Create mock database
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	// Create GORM DB
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)

	config := &Config{
		TableName:   "agent_stores",
		AutoMigrate: false, // Skip migration for tests
	}

	store, err := NewFromDB(gormDB, config)
	require.NoError(t, err)

	return store, mock, db
}

func TestNew(t *testing.T) {
	// This test would require a real PostgreSQL instance
	// For unit testing, we use mock instead
	t.Skip("Requires real PostgreSQL connection")
}

func TestStore_Put_Create(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()
	namespace := []string{"users"}
	key := "user1"
	value := map[string]interface{}{"name": "Alice"}

	nsKey := namespaceToKey(namespace)

	// Expect SELECT to check if exists (not found)
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT * FROM "agent_stores" WHERE namespace = $1 AND key = $2 ORDER BY "agent_stores"."id" LIMIT $3`,
	)).
		WithArgs(nsKey, key, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	// Expect INSERT (without explicit transaction since SkipDefaultTransaction is true)
	mock.ExpectQuery(regexp.QuoteMeta(
		`INSERT INTO "agent_stores" ("namespace","key","value","metadata","created_at","updated_at") VALUES ($1,$2,$3,$4,$5,$6) RETURNING "id"`,
	)).
		WithArgs(nsKey, key, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	err := store.Put(ctx, namespace, key, value)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStore_Put_Update(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()
	namespace := []string{"users"}
	key := "user1"
	value := map[string]interface{}{"name": "Alice Updated"}

	nsKey := namespaceToKey(namespace)
	now := time.Now()

	// Expect SELECT to check if exists (found)
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT * FROM "agent_stores" WHERE namespace = $1 AND key = $2 ORDER BY "agent_stores"."id" LIMIT $3`,
	)).
		WithArgs(nsKey, key, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "key", "value", "metadata", "created_at", "updated_at"}).
			AddRow(1, nsKey, key, `{"name":"Alice"}`, `{}`, now, now))

	// Expect UPDATE (without explicit transaction)
	mock.ExpectExec(regexp.QuoteMeta(
		`UPDATE "agent_stores" SET "namespace"=$1,"key"=$2,"value"=$3,"metadata"=$4,"created_at"=$5,"updated_at"=$6 WHERE "id" = $7`,
	)).
		WithArgs(nsKey, key, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := store.Put(ctx, namespace, key, value)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStore_Get(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()
	namespace := []string{"users"}
	key := "user1"

	nsKey := namespaceToKey(namespace)
	now := time.Now()

	// Expect SELECT
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT * FROM "agent_stores" WHERE namespace = $1 AND key = $2 ORDER BY "agent_stores"."id" LIMIT $3`,
	)).
		WithArgs(nsKey, key, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "key", "value", "metadata", "created_at", "updated_at"}).
			AddRow(1, nsKey, key, `{"name":"Alice"}`, `{"type":"user"}`, now, now))

	result, err := store.Get(ctx, namespace, key)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, key, result.Key)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStore_Get_NotFound(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()
	namespace := []string{"users"}
	key := "nonexistent"

	nsKey := namespaceToKey(namespace)

	// Expect SELECT (not found)
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT * FROM "agent_stores" WHERE namespace = $1 AND key = $2 ORDER BY "agent_stores"."id" LIMIT $3`,
	)).
		WithArgs(nsKey, key, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := store.Get(ctx, namespace, key)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStore_Delete(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()
	namespace := []string{"users"}
	key := "user1"

	nsKey := namespaceToKey(namespace)

	// Expect DELETE (without explicit transaction)
	mock.ExpectExec(regexp.QuoteMeta(
		`DELETE FROM "agent_stores" WHERE namespace = $1 AND key = $2`,
	)).
		WithArgs(nsKey, key).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.Delete(ctx, namespace, key)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStore_List(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()
	namespace := []string{"users"}

	nsKey := namespaceToKey(namespace)

	// Expect SELECT
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT "key" FROM "agent_stores" WHERE namespace = $1`,
	)).
		WithArgs(nsKey).
		WillReturnRows(sqlmock.NewRows([]string{"key"}).
			AddRow("user1").
			AddRow("user2").
			AddRow("user3"))

	keys, err := store.List(ctx, namespace)
	require.NoError(t, err)
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "user1")
	assert.Contains(t, keys, "user2")
	assert.Contains(t, keys, "user3")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStore_Search(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()
	namespace := []string{"products"}
	filter := map[string]interface{}{"category": "electronics"}

	nsKey := namespaceToKey(namespace)
	now := time.Now()

	// Expect SELECT with JSONB filter
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT * FROM "agent_stores" WHERE namespace = $1 AND metadata @> $2`,
	)).
		WithArgs(nsKey, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "key", "value", "metadata", "created_at", "updated_at"}).
			AddRow(1, nsKey, "prod1", `{"name":"Product 1"}`, `{"category":"electronics"}`, now, now).
			AddRow(2, nsKey, "prod2", `{"name":"Product 2"}`, `{"category":"electronics"}`, now, now))

	results, err := store.Search(ctx, namespace, filter)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStore_Clear(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()
	namespace := []string{"temp"}

	nsKey := namespaceToKey(namespace)

	// Expect DELETE (without explicit transaction)
	mock.ExpectExec(regexp.QuoteMeta(
		`DELETE FROM "agent_stores" WHERE namespace = $1`,
	)).
		WithArgs(nsKey).
		WillReturnResult(sqlmock.NewResult(0, 5))

	err := store.Clear(ctx, namespace)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStore_Size(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()

	// Expect COUNT
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT count(*) FROM "agent_stores"`,
	)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	size, err := store.Size(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(42), size)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStore_Ping(t *testing.T) {
	store, mock, db := setupTestStore(t)
	defer db.Close()

	ctx := context.Background()

	// Expect Ping (without MonitorPingsOption, this won't work in sqlmock)
	// Just test that the method doesn't panic
	_ = store.Ping(ctx)
	_ = mock // Suppress unused variable warning
}

// TestNew_WithOptions 测试新的 Option 模式 API
func TestNew_WithOptions(t *testing.T) {
	// This test would require a real PostgreSQL connection
	// We're testing the option application logic here
	t.Run("default options", func(t *testing.T) {
		config := DefaultConfig()
		config.DSN = "test_dsn"

		opts := []PostgresOption{}
		for _, opt := range opts {
			opt(config)
		}

		assert.Equal(t, "test_dsn", config.DSN)
		assert.Equal(t, "agent_stores", config.TableName)
		assert.Equal(t, 25, config.MaxIdleConns)               // Updated default
		assert.Equal(t, 100, config.MaxOpenConns)              // Unchanged
		assert.Equal(t, 5*time.Minute, config.ConnMaxLifetime) // Updated default
		assert.Equal(t, 5*time.Minute, config.ConnMaxIdleTime) // New field
		assert.True(t, config.AutoMigrate)
	})

	t.Run("with custom options", func(t *testing.T) {
		config := DefaultConfig()
		config.DSN = "test_dsn"

		opts := []PostgresOption{
			WithTableName("custom_table"),
			WithMaxIdleConns(20),
			WithMaxOpenConns(200),
			WithConnMaxLifetime(2 * time.Hour),
			WithConnMaxIdleTime(10 * time.Minute),
			WithAutoMigrate(false),
		}

		for _, opt := range opts {
			opt(config)
		}

		assert.Equal(t, "custom_table", config.TableName)
		assert.Equal(t, 20, config.MaxIdleConns)
		assert.Equal(t, 200, config.MaxOpenConns)
		assert.Equal(t, 2*time.Hour, config.ConnMaxLifetime)
		assert.Equal(t, 10*time.Minute, config.ConnMaxIdleTime)
		assert.False(t, config.AutoMigrate)
	})

	t.Run("option validation", func(t *testing.T) {
		config := DefaultConfig()

		// Test that invalid values are ignored
		WithMaxIdleConns(-1)(config)
		assert.Equal(t, 25, config.MaxIdleConns) // Should keep updated default

		WithMaxOpenConns(0)(config)
		assert.Equal(t, 100, config.MaxOpenConns) // Should keep default

		WithConnMaxLifetime(-1 * time.Hour)(config)
		assert.Equal(t, 5*time.Minute, config.ConnMaxLifetime) // Should keep updated default

		WithConnMaxIdleTime(-1 * time.Minute)(config)
		assert.Equal(t, 5*time.Minute, config.ConnMaxIdleTime) // Should keep updated default

		WithTableName("")(config)
		assert.Equal(t, "agent_stores", config.TableName) // Should keep default
	})
}

// TestApplyPostgresOptions 测试 ApplyPostgresOptions 函数
func TestApplyPostgresOptions(t *testing.T) {
	t.Run("apply to nil config", func(t *testing.T) {
		config := ApplyPostgresOptions(nil,
			WithTableName("custom_table"),
			WithMaxOpenConns(50),
		)

		assert.NotNil(t, config)
		assert.Equal(t, "custom_table", config.TableName)
		assert.Equal(t, 50, config.MaxOpenConns)
	})

	t.Run("apply to existing config", func(t *testing.T) {
		config := &Config{
			DSN:          "test_dsn",
			TableName:    "old_table",
			MaxIdleConns: 5,
		}

		config = ApplyPostgresOptions(config,
			WithTableName("new_table"),
			WithMaxIdleConns(15),
		)

		assert.Equal(t, "test_dsn", config.DSN) // Unchanged
		assert.Equal(t, "new_table", config.TableName)
		assert.Equal(t, 15, config.MaxIdleConns)
	})
}
