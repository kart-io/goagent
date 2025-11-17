package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
)

// Store is a PostgreSQL-backed implementation of the store.Store interface.
//
// Features:
//   - JSONB storage for flexible data types
//   - Efficient indexing on namespace and key
//   - ACID compliance for data integrity
//   - Powerful search with JSONB queries
//   - Connection pooling
//
// Suitable for:
//   - Production deployments
//   - Large-scale data storage
//   - Complex queries
//   - Distributed systems with shared database
type Store struct {
	db     *gorm.DB
	config *Config
}

// storeModel represents the database schema for store values
type storeModel struct {
	ID        uint           `gorm:"primaryKey"`
	Namespace string         `gorm:"index;not null"`
	Key       string         `gorm:"index;not null"`
	Value     datatypes.JSON `gorm:"type:jsonb;not null"`
	Metadata  datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt time.Time      `gorm:"not null"`
	UpdatedAt time.Time      `gorm:"not null"`

	// Composite unique index
	// Note: This is defined in the model but applied via AutoMigrate
}

// TableName returns the table name for the store model
func (storeModel) TableName() string {
	// This will be overridden by the store's config
	return "agent_stores"
}

// New creates a new PostgreSQL-backed store
func New(config *Config) (*Store, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Open database connection
	db, err := gorm.Open(postgres.Open(config.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(config.LogLevel),
	})
	if err != nil {
		return nil, agentErrors.NewStoreConnectionError("postgres", config.DSN, err)
	}

	// Get underlying SQL database
	sqlDB, err := db.DB()
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to get SQL database").WithComponent("postgres_store").WithOperation("new")
	}

	// Set connection pool settings
	sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(config.ConnMaxLifetime)

	store := &Store{
		db:     db,
		config: config,
	}

	// Auto-migrate if enabled
	if config.AutoMigrate {
		if err := store.migrate(); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to migrate database").WithComponent("postgres_store").WithOperation("new")
		}
	}

	return store, nil
}

// NewFromDB creates a Store from an existing GORM DB
func NewFromDB(db *gorm.DB, config *Config) (*Store, error) {
	if config == nil {
		config = DefaultConfig()
	}

	store := &Store{
		db:     db,
		config: config,
	}

	if config.AutoMigrate {
		if err := store.migrate(); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to migrate database").WithComponent("postgres_store").WithOperation("new_from_db")
		}
	}

	return store, nil
}

// migrate creates the table and indexes
func (s *Store) migrate() error {
	// Set custom table name
	if s.config.TableName != "" {
		model := storeModel{}
		_ = s.db.Table(s.config.TableName).AutoMigrate(&model)
	} else {
		_ = s.db.AutoMigrate(&storeModel{})
	}

	// Create composite unique index
	return s.db.Exec(fmt.Sprintf(
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_%s_namespace_key ON %s (namespace, key)",
		s.config.TableName, s.config.TableName,
	)).Error
}

// Put stores a value with the given namespace and key
func (s *Store) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	nsKey := namespaceToKey(namespace)

	// Serialize value to JSON
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "failed to marshal value").
			WithComponent("postgres_store").
			WithOperation("put").
			WithContext("namespace", nsKey).
			WithContext("key", key)
	}

	// Check if exists
	var existing storeModel
	result := s.getDB().Where("namespace = ? AND key = ?", nsKey, key).First(&existing)

	now := time.Now()

	if result.Error == nil {
		// Update existing
		existing.Value = valueJSON
		existing.UpdatedAt = now

		if err := s.getDB().Save(&existing).Error; err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to update value").
				WithComponent("postgres_store").
				WithOperation("put").
				WithContext("namespace", nsKey).
				WithContext("key", key)
		}
	} else if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		// Create new
		model := storeModel{
			Namespace: nsKey,
			Key:       key,
			Value:     valueJSON,
			Metadata:  []byte("{}"),
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := s.getDB().Create(&model).Error; err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to create value").
				WithComponent("postgres_store").
				WithOperation("put").
				WithContext("namespace", nsKey).
				WithContext("key", key)
		}
	} else {
		return agentErrors.Wrap(result.Error, agentErrors.CodeStoreConnection, "failed to query existing value").
			WithComponent("postgres_store").
			WithOperation("put").
			WithContext("namespace", nsKey).
			WithContext("key", key)
	}

	return nil
}

// Get retrieves a value by namespace and key
func (s *Store) Get(ctx context.Context, namespace []string, key string) (*store.Value, error) {
	nsKey := namespaceToKey(namespace)

	var model storeModel
	result := s.getDB().Where("namespace = ? AND key = ?", nsKey, key).First(&model)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, agentErrors.NewStoreNotFoundError(namespace, key)
		}
		return nil, agentErrors.Wrap(result.Error, agentErrors.CodeStoreConnection, "failed to get value").
			WithComponent("postgres_store").
			WithOperation("get").
			WithContext("namespace", nsKey).
			WithContext("key", key)
	}

	// Deserialize value
	var value interface{}
	if err := json.Unmarshal(model.Value, &value); err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "failed to unmarshal value").
			WithComponent("postgres_store").
			WithOperation("get").
			WithContext("namespace", nsKey).
			WithContext("key", key)
	}

	// Deserialize metadata
	var metadata map[string]interface{}
	if len(model.Metadata) > 0 {
		if err := json.Unmarshal(model.Metadata, &metadata); err != nil {
			metadata = make(map[string]interface{})
		}
	} else {
		metadata = make(map[string]interface{})
	}

	return &store.Value{
		Value:     value,
		Metadata:  metadata,
		Created:   model.CreatedAt,
		Updated:   model.UpdatedAt,
		Namespace: namespace,
		Key:       key,
	}, nil
}

// Delete removes a value by namespace and key
func (s *Store) Delete(ctx context.Context, namespace []string, key string) error {
	nsKey := namespaceToKey(namespace)

	result := s.getDB().Where("namespace = ? AND key = ?", nsKey, key).Delete(&storeModel{})
	if result.Error != nil {
		return agentErrors.Wrap(result.Error, agentErrors.CodeStoreConnection, "failed to delete value").
			WithComponent("postgres_store").
			WithOperation("delete").
			WithContext("namespace", nsKey).
			WithContext("key", key)
	}

	return nil
}

// Search finds values matching the filter within a namespace
func (s *Store) Search(ctx context.Context, namespace []string, filter map[string]interface{}) ([]*store.Value, error) {
	nsKey := namespaceToKey(namespace)

	query := s.getDB().Where("namespace = ?", nsKey)

	// Apply metadata filters using JSONB queries
	for key, value := range filter {
		// Use JSONB contains query
		filterJSON := map[string]interface{}{key: value}
		filterBytes, err := json.Marshal(filterJSON)
		if err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "failed to marshal filter").
				WithComponent("postgres_store").
				WithOperation("search").
				WithContext("namespace", nsKey)
		}

		query = query.Where("metadata @> ?", filterBytes)
	}

	var models []storeModel
	if err := query.Find(&models).Error; err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to search values").
			WithComponent("postgres_store").
			WithOperation("search").
			WithContext("namespace", nsKey)
	}

	// Convert models to store.Values
	results := make([]*store.Value, 0, len(models))
	for _, model := range models {
		var value interface{}
		if err := json.Unmarshal(model.Value, &value); err != nil {
			continue // Skip invalid values
		}

		var metadata map[string]interface{}
		if len(model.Metadata) > 0 {
			if err := json.Unmarshal(model.Metadata, &metadata); err != nil {
				continue // Skip invalid metadata
			}
		} else {
			metadata = make(map[string]interface{})
		}

		results = append(results, &store.Value{
			Value:     value,
			Metadata:  metadata,
			Created:   model.CreatedAt,
			Updated:   model.UpdatedAt,
			Namespace: namespace,
			Key:       model.Key,
		})
	}

	return results, nil
}

// List returns all keys within a namespace
func (s *Store) List(ctx context.Context, namespace []string) ([]string, error) {
	nsKey := namespaceToKey(namespace)

	var keys []string
	err := s.getDB().
		Model(&storeModel{}).
		Where("namespace = ?", nsKey).
		Pluck("key", &keys).
		Error
	if err != nil {
		return nil, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to list keys").
			WithComponent("postgres_store").
			WithOperation("list").
			WithContext("namespace", nsKey)
	}

	return keys, nil
}

// Clear removes all values within a namespace
func (s *Store) Clear(ctx context.Context, namespace []string) error {
	nsKey := namespaceToKey(namespace)

	result := s.getDB().Where("namespace = ?", nsKey).Delete(&storeModel{})
	if result.Error != nil {
		return agentErrors.Wrap(result.Error, agentErrors.CodeStoreConnection, "failed to clear namespace").
			WithComponent("postgres_store").
			WithOperation("clear").
			WithContext("namespace", nsKey)
	}

	return nil
}

// Close closes the database connection
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// getDB returns the DB instance with custom table name if configured
func (s *Store) getDB() *gorm.DB {
	if s.config.TableName != "" {
		return s.db.Table(s.config.TableName)
	}
	return s.db
}

// Ping tests the connection to PostgreSQL
func (s *Store) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Size returns the total number of stored values
func (s *Store) Size(ctx context.Context) (int64, error) {
	var count int64
	err := s.getDB().Model(&storeModel{}).Count(&count).Error
	if err != nil {
		return 0, agentErrors.Wrap(err, agentErrors.CodeStoreConnection, "failed to count values").
			WithComponent("postgres_store").
			WithOperation("size")
	}
	return count, nil
}

// namespaceToKey converts a namespace slice to a string key.
func namespaceToKey(namespace []string) string {
	if len(namespace) == 0 {
		return "/"
	}
	return "/" + joinNamespace(namespace)
}

// joinNamespace joins namespace components with "/".
func joinNamespace(namespace []string) string {
	if len(namespace) == 0 {
		return ""
	}
	result := namespace[0]
	for i := 1; i < len(namespace); i++ {
		result += "/" + namespace[i]
	}
	return result
}
