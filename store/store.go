package store

import (
	"context"
	"time"
)

// Store 定义按命名空间组织的存储接口。
// memory.Store 在输入和 Get/Search 输出处取得独立快照；修改返回值不会写回。
// 元数据通过可选 MetadataWriter 写入，不得通过修改 Get 返回值设置。
//
// NOTE: This is a legacy interface with namespace support.
// For new code, consider using interfaces.Store for simpler key-value storage,
// or this NamespaceStore for namespace-organized storage.
//
// Inspired by LangChain's Store, it provides:
//   - Namespace-based organization
//   - Key-value storage with metadata
//   - Search capabilities
//   - Timestamp tracking
//
// Use cases:
//   - User preferences and settings
//   - Historical conversation data
//   - Application-specific persistent data
type Store interface {
	// Put stores a value with the given namespace and key.
	Put(ctx context.Context, namespace []string, key string, value interface{}) error

	// Get retrieves a value by namespace and key.
	Get(ctx context.Context, namespace []string, key string) (*Value, error)

	// Delete removes a value by namespace and key.
	Delete(ctx context.Context, namespace []string, key string) error

	// Search finds values matching the filter within a namespace.
	Search(ctx context.Context, namespace []string, filter map[string]interface{}) ([]*Value, error)

	// List returns all keys within a namespace.
	List(ctx context.Context, namespace []string) ([]string, error)

	// Clear removes all values within a namespace.
	Clear(ctx context.Context, namespace []string) error
}

// MetadataWriter 在一次写入中保存值与附加元数据，供 metadata 装饰器使用。
// 元数据按字段合并；Get/Search 返回的快照修改不会写回存储。
type MetadataWriter interface {
	PutWithMetadata(ctx context.Context, namespace []string, key string, value interface{}, metadata map[string]interface{}) error
}

// Value represents a value stored in the Store.
type Value struct {
	// Value is the stored data
	Value interface{} `json:"value"`

	// Metadata holds additional information about the value
	Metadata map[string]interface{} `json:"metadata"`

	// Created is when the value was first created
	Created time.Time `json:"created"`

	// Updated is when the value was last updated
	Updated time.Time `json:"updated"`

	// Namespace is the hierarchical namespace path
	Namespace []string `json:"namespace"`

	// Key is the unique identifier within the namespace
	Key string `json:"key"`
}
