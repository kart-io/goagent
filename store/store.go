package store

import (
	"context"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

// SimpleStore is a type alias for the canonical Store interface.
//
// For new code, use interfaces.Store directly.
// This alias provides a migration path for existing code.
type SimpleStore = interfaces.Store

// Store defines the interface for namespace-based persistent storage.
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
