package decorator

import (
	"context"

	"github.com/kart-io/goagent/store"
)

// WithMetadata wraps a store.Store to automatically add metadata to all operations.
type WithMetadata struct {
	store    store.Store
	metadata map[string]interface{}
}

// NewWithMetadata creates a Store that adds metadata to all Put operations.
func NewWithMetadata(s store.Store, metadata map[string]interface{}) *WithMetadata {
	return &WithMetadata{
		store:    s,
		metadata: metadata,
	}
}

// Put stores a value and adds the configured metadata.
func (d *WithMetadata) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	// First put the value
	err := d.store.Put(ctx, namespace, key, value)
	if err != nil {
		return err
	}

	// Then update with metadata
	storeValue, err := d.store.Get(ctx, namespace, key)
	if err != nil {
		return err
	}

	// Add configured metadata
	for k, v := range d.metadata {
		storeValue.Metadata[k] = v
	}

	return d.store.Put(ctx, namespace, key, storeValue.Value)
}

// Get retrieves a value by namespace and key.
func (d *WithMetadata) Get(ctx context.Context, namespace []string, key string) (*store.Value, error) {
	return d.store.Get(ctx, namespace, key)
}

// Delete removes a value by namespace and key.
func (d *WithMetadata) Delete(ctx context.Context, namespace []string, key string) error {
	return d.store.Delete(ctx, namespace, key)
}

// Search finds values matching the filter within a namespace.
func (d *WithMetadata) Search(ctx context.Context, namespace []string, filter map[string]interface{}) ([]*store.Value, error) {
	return d.store.Search(ctx, namespace, filter)
}

// List returns all keys within a namespace.
func (d *WithMetadata) List(ctx context.Context, namespace []string) ([]string, error) {
	return d.store.List(ctx, namespace)
}

// Clear removes all values within a namespace.
func (d *WithMetadata) Clear(ctx context.Context, namespace []string) error {
	return d.store.Clear(ctx, namespace)
}
