package migrate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/durable/migrate"
)

func ExampleOperationsV1() {
	dir, err := os.MkdirTemp("", "operation-migration-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	store, err := durable.NewFileStore(dir)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	ref := migrate.OperationRef{Key: durable.ChildKey("node-key", "refund"), ID: "refund"}
	storageID := durable.OperationStorageID(ref.Key)
	// 示例准备一条历史记录；实际迁移直接使用已备份且停写的原 Store/Scope。
	hash := sha256.Sum256([]byte(`{}`))
	data, err := json.Marshal(durable.OperationRecord{ID: ref.ID, Key: ref.Key, InputHash: hex.EncodeToString(hash[:]), Status: "completed", Attempts: 1, MaxAttempts: 1, Value: json.RawMessage(`"receipt"`)})
	if err != nil {
		panic(err)
	}
	unlock, err := store.Lock(ctx, storageID)
	if err != nil {
		panic(err)
	}
	if err := store.Save(ctx, storageID, &durable.Snapshot{RunID: storageID, Version: "goagent-operation-v1", Data: data}); err != nil {
		panic(err)
	}
	if err := unlock(); err != nil {
		panic(err)
	}
	preview, err := migrate.OperationsV1(ctx, store, []migrate.OperationRef{ref}, migrate.OperationOptions{})
	if err != nil {
		panic(err)
	}
	fmt.Println("待迁移", len(preview.Pending), "已应用", preview.Applied)
	applied, err := migrate.OperationsV1(ctx, store, []migrate.OperationRef{ref}, migrate.OperationOptions{Apply: true})
	if err != nil {
		panic(err)
	}
	fmt.Println("已应用", applied.Applied)
	// Output:
	// 待迁移 1 已应用 false
	// 已应用 true
}
