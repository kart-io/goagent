package contextpolicy

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	agentErrors "github.com/kart-io/goagent/errors"
)

func errorCodeStores(t *testing.T) map[string]*resultStore {
	t.Helper()
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	persist, err := NewDurableResultStore(file)
	require.NoError(t, err)
	return map[string]*resultStore{"内存": NewMemoryStore().(*resultStore), "文件": persist.(*resultStore)}
}

// 摘要与写入内容不一致属于调用参数错误，拒绝后不能留下证据。
func TestResultStoreRejectsMismatchedInputDigest(t *testing.T) {
	for name, store := range errorCodeStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := WithResultScope(context.Background(), "run")
			ref := NewRef("run", "policy", "call", []byte("expected"))
			err := store.PutIfAbsent(ctx, ref, []byte("different"))
			require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err))
			require.ErrorContains(t, err, "result content digest mismatch")
			_, err = store.Get(ctx, ref)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

// 已存数据自身损坏必须先于请求身份比较归类，读与重复写入都应拒绝。
func TestResultStoreReportsCorruptSavedDigest(t *testing.T) {
	for name, store := range errorCodeStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := WithResultScope(context.Background(), "run")
			data := []byte("expected")
			ref := NewRef("run", "policy", "call", data)
			require.NoError(t, store.PutIfAbsent(ctx, ref, data))
			id := resultID(ref)
			broken := entry{Ref: ref, Data: []byte("corrupted")}
			if store.store == nil {
				store.memory[ref.RunKey][id] = broken
			} else {
				payload, err := json.Marshal(broken)
				require.NoError(t, err)
				require.NoError(t, store.runStore(ref.RunKey).Save(ctx, id, &durable.Snapshot{RunID: id, Version: resultVersion, Data: payload}))
			}
			for _, requested := range []Ref{ref, NewRef("run", "policy", "call", []byte("another"))} {
				got, err := store.Get(ctx, requested)
				require.Nil(t, got)
				require.Equal(t, agentErrors.CodeStateValidation, agentErrors.GetCode(err))
				require.ErrorContains(t, err, "result identity or content conflict")
			}
			err := store.PutIfAbsent(ctx, ref, data)
			require.Equal(t, agentErrors.CodeStateValidation, agentErrors.GetCode(err))
			require.ErrorContains(t, err, "result identity or content conflict")
		})
	}
}

// 合法记录与另一个合法请求的冲突不会被误判成状态损坏，也不会覆盖已有证据。
func TestResultStorePreservesValidContentConflict(t *testing.T) {
	for name, store := range errorCodeStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := WithResultScope(context.Background(), "run")
			data := []byte("expected")
			ref := NewRef("run", "policy", "call", data)
			require.NoError(t, store.PutIfAbsent(ctx, ref, data))
			otherData := []byte("different")
			other := NewRef("run", "policy", "call", otherData)
			err := store.PutIfAbsent(ctx, other, otherData)
			require.Equal(t, agentErrors.CodeConflict, agentErrors.GetCode(err))
			require.ErrorContains(t, err, "result identity or content conflict")
			_, err = store.Get(ctx, other)
			require.Equal(t, agentErrors.CodeConflict, agentErrors.GetCode(err))
			got, err := store.Get(ctx, ref)
			require.NoError(t, err)
			require.Equal(t, data, got)
		})
	}
}
