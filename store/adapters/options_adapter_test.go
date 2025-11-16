package adapters

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/k8s-agent/common/options"
)

func TestRedisStoreAdapter(t *testing.T) {
	// Create Redis options
	redisOpts := options.NewRedisOptions()
	redisOpts.Addr = "localhost:6379"

	// Create adapter
	adapter := NewRedisStoreAdapter(redisOpts, "test:")

	// Verify adapter is created
	assert.NotNil(t, adapter)
	assert.Equal(t, "test:", adapter.prefix)
	assert.Equal(t, redisOpts, adapter.options)
}

func TestStoreOptions_Memory(t *testing.T) {
	opts := NewStoreOptions()
	opts.Type = "memory"

	store, err := NewStore(opts)
	require.NoError(t, err)
	require.NotNil(t, store)

	// Test basic operations
	ctx := context.Background()
	err = store.Put(ctx, []string{"test"}, "key1", "value1")
	assert.NoError(t, err)

	value, err := store.Get(ctx, []string{"test"}, "key1")
	assert.NoError(t, err)
	assert.Equal(t, "value1", value.Value)
}

func TestStoreOptions_Validation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*StoreOptions)
		wantErr bool
	}{
		{
			name: "valid memory store",
			setup: func(opts *StoreOptions) {
				opts.Type = "memory"
			},
			wantErr: false,
		},
		{
			name: "redis without options",
			setup: func(opts *StoreOptions) {
				opts.Type = "redis"
				opts.Redis = nil
			},
			wantErr: true,
		},
		{
			name: "redis with invalid port",
			setup: func(opts *StoreOptions) {
				opts.Type = "redis"
				opts.Redis.Addr = "localhost:99999" // Invalid port
			},
			wantErr: true,
		},
		{
			name: "postgres without options",
			setup: func(opts *StoreOptions) {
				opts.Type = "postgres"
				opts.Postgres = nil
			},
			wantErr: true,
		},
		{
			name: "postgres with valid options",
			setup: func(opts *StoreOptions) {
				opts.Type = "postgres"
				opts.Postgres = options.NewPostgresOptions()
				// Set a non-existent host to avoid real connection attempts
				opts.Postgres.Host = "non-existent-host-for-testing"
			},
			wantErr: true, // Will fail because it can't connect
		},
		{
			name: "unsupported store type",
			setup: func(opts *StoreOptions) {
				opts.Type = "unsupported"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := NewStoreOptions()
			tt.setup(opts)

			_, err := NewStore(opts)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConvertLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"silent", 1},
		{"error", 2},
		{"warn", 3},
		{"info", 4},
		{"unknown", 1}, // Should default to Silent
		{"", 1},        // Should default to Silent
	}

	for _, tt := range tests {
		result := convertLogLevel(tt.input)
		assert.Equal(t, tt.expected, int(result))
	}
}

func TestMySQLStoreAdapter(t *testing.T) {
	mysqlOpts := options.NewMySQLOptions()
	mysqlOpts.Host = "localhost"
	mysqlOpts.Port = 3306
	mysqlOpts.User = "root"
	mysqlOpts.Database = "test"

	adapter := NewMySQLStoreAdapter(mysqlOpts, "custom_stores")

	assert.NotNil(t, adapter)
	assert.Equal(t, "custom_stores", adapter.tableName)
	assert.Equal(t, mysqlOpts, adapter.options)

	// CreateStore should return not implemented error for now
	_, err := adapter.CreateStore()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet fully implemented")
}

func TestStoreOptions_DefaultValues(t *testing.T) {
	opts := NewStoreOptions()

	assert.Equal(t, "memory", opts.Type)
	assert.Equal(t, "agent:store:", opts.Prefix)
	assert.Equal(t, "agent_stores", opts.TableName)
	assert.NotNil(t, opts.Redis)
	assert.NotNil(t, opts.MySQL)
	assert.NotNil(t, opts.Postgres)
}
