package decorator

import (
	"context"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/internal/snapshot"
	"github.com/kart-io/goagent/store"
)

// WithMetadata wraps a store.Store to automatically add metadata to all operations.
type WithMetadata struct {
	store    store.Store
	metadata map[string]interface{}
	initErr  error
}

// NewWithMetadata creates a Store that adds metadata to all Put operations.
func NewWithMetadata(s store.Store, metadata map[string]interface{}) *WithMetadata {
	d := &WithMetadata{store: s}
	copy, err := snapshot.Clone(metadata)
	if err != nil {
		d.initErr = agentErrors.Wrap(err, agentErrors.CodeStoreSerialization, "存储元数据快照失败").WithComponent("store_metadata").WithOperation("configure")
		return d
	}
	d.metadata = copy.(map[string]interface{})
	return d
}

// Put stores a value and adds the configured metadata.
func (d *WithMetadata) Put(ctx context.Context, namespace []string, key string, value interface{}) error {
	return d.PutWithMetadata(ctx, namespace, key, value, nil)
}

// PutWithMetadata 将所有装饰层的元数据合并后以一次后端写入提交。
func (d *WithMetadata) PutWithMetadata(ctx context.Context, namespace []string, key string, value interface{}, metadata map[string]interface{}) error {
	if d.initErr != nil {
		return d.initErr
	}
	writer, ok := d.store.(store.MetadataWriter)
	if !ok {
		return agentErrors.New(agentErrors.CodeInvalidConfig, "存储后端不支持原子元数据写入：需要实现 store.MetadataWriter").WithComponent("store_metadata").WithOperation("put")
	}
	merged := make(map[string]interface{}, len(d.metadata)+len(metadata))
	for k, v := range d.metadata {
		merged[k] = v
	}
	for k, v := range metadata {
		merged[k] = v
	}
	return writer.PutWithMetadata(ctx, namespace, key, value, merged)
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
