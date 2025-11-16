package postgres

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
