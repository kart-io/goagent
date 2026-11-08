package migrate_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/durable/migrate"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T) (*durable.FileStore, context.Context, migrate.OperationRef, *durable.Snapshot, *int) {
	t.Helper()
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), "parent"), store)
	calls := new(int)
	_, err = durable.Do(ctx, "effect", json.RawMessage(`{"amount":7}`), durable.OperationPolicy{MaxAttempts: 3}, func(context.Context) (json.RawMessage, error) {
		*calls++
		return json.RawMessage(`{"receipt":"saved"}`), nil
	})
	require.NoError(t, err)
	ref := migrate.OperationRef{Key: durable.ChildKey("parent", "effect"), ID: "effect"}
	id := durable.OperationStorageID(ref.Key)
	snap, err := store.Load(ctx, id)
	require.NoError(t, err)
	snap.Version = "goagent-operation-v1"
	snap.Plan, snap.Next, snap.Key, snap.Approved = "original-metadata", 3, "retained", true
	snap.Data = append([]byte(" \n"), snap.Data...)
	put(t, store, id, snap)
	snap, err = store.Load(ctx, id)
	require.NoError(t, err)
	return store, ctx, ref, snap, calls
}

func put(t *testing.T, store durable.Store, id string, snap *durable.Snapshot) {
	t.Helper()
	unlock, err := store.Lock(context.Background(), id)
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), id, snap))
	require.NoError(t, unlock())
}

func TestOperationsV1PreviewApplyAndReplay(t *testing.T) {
	store, ctx, ref, original, calls := fixture(t)
	preview, err := migrate.OperationsV1(ctx, store, []migrate.OperationRef{ref}, migrate.OperationOptions{})
	require.NoError(t, err)
	require.False(t, preview.Applied)
	require.Equal(t, []migrate.OperationRef{ref}, preview.Pending)
	before, err := store.Load(ctx, durable.OperationStorageID(ref.Key))
	require.NoError(t, err)
	require.Equal(t, original, before, "预览不改快照")
	applied, err := migrate.OperationsV1(ctx, store, []migrate.OperationRef{ref}, migrate.OperationOptions{Apply: true})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	after, err := store.Load(ctx, durable.OperationStorageID(ref.Key))
	require.NoError(t, err)
	expected := *original
	expected.Version = "goagent-operation-v2"
	require.Equal(t, &expected, after, "只改版本；Data原字节和全部快照字段保持")
	_, err = durable.Do(ctx, "effect", json.RawMessage(`{"amount":7}`), durable.OperationPolicy{MaxAttempts: 3}, func(context.Context) (json.RawMessage, error) { *calls++; return nil, nil })
	require.NoError(t, err)
	require.Equal(t, 1, *calls, "迁移后的completed必须回放，不重复副作用")
	again, err := migrate.OperationsV1(ctx, store, []migrate.OperationRef{ref}, migrate.OperationOptions{Apply: true})
	require.NoError(t, err)
	require.Empty(t, again.Pending)
	require.Equal(t, []migrate.OperationRef{ref}, again.Current)
}

func TestOperationsV1RejectsInvalidBatchWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*durable.Snapshot)
	}{
		{"身份不符", func(s *durable.Snapshot) { s.RunID = "wrong" }},
		{"未知版本", func(s *durable.Snapshot) { s.Version = "unknown" }},
		{"损坏载荷", func(s *durable.Snapshot) { s.Data = json.RawMessage(`false`) }},
		{"v1等待", func(s *durable.Snapshot) {
			s.Data = json.RawMessage(`{"id":"bad","key":"other","input_hash":"h","status":"waiting","attempts":1,"max_attempts":1,"value":null,"continuation":[{}]}`)
		}},
		{"v1核对", func(s *durable.Snapshot) {
			s.Data = json.RawMessage(`{"id":"bad","key":"other","input_hash":"h","status":"completed","attempts":1,"max_attempts":1,"value":null,"reconciled":true}`)
		}},
		{"v2损坏", func(s *durable.Snapshot) {
			s.Version = "goagent-operation-v2"
			s.Data = json.RawMessage(`{"id":"bad","key":"other","status":"completed"}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx, ref, original, _ := fixture(t)
			bad := *original
			bad.RunID = durable.OperationStorageID("other")
			bad.Data = json.RawMessage(`{"id":"bad","key":"other","input_hash":"h","status":"completed","attempts":1,"max_attempts":1,"value":null}`)
			tc.mutate(&bad)
			put(t, store, durable.OperationStorageID("other"), &bad)
			_, err := migrate.OperationsV1(ctx, store, []migrate.OperationRef{ref, {Key: "other", ID: "bad"}}, migrate.OperationOptions{Apply: true})
			require.Error(t, err)
			still, err := store.Load(ctx, durable.OperationStorageID(ref.Key))
			require.NoError(t, err)
			require.Equal(t, original, still, "任何目标无效时整批不写")
		})
	}
}

type faultStore struct {
	durable.Store
	lockErrAt         int
	loadErr           error
	writeErr          error
	commitBeforeError bool
	padLoadedData     bool
	cancelLoad        context.CancelFunc
	seenWrites        []durable.Write
	locked, released  []string
	writes            int
}

func (s *faultStore) Lock(ctx context.Context, id string) (func() error, error) {
	if s.lockErrAt > 0 && len(s.locked)+1 == s.lockErrAt {
		return nil, errors.New("锁失败")
	}
	u, err := s.Store.Lock(ctx, id)
	if err != nil {
		return nil, err
	}
	s.locked = append(s.locked, id)
	return func() error { s.released = append(s.released, id); return u() }, nil
}
func (s *faultStore) Load(ctx context.Context, id string) (*durable.Snapshot, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	snapshot, err := s.Store.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.padLoadedData {
		snapshot.Data = append([]byte(" \n"), snapshot.Data...)
	}
	if s.cancelLoad != nil {
		s.cancelLoad()
	}
	return snapshot, nil
}
func (s *faultStore) SaveAll(ctx context.Context, writes ...durable.Write) error {
	s.writes++
	s.seenWrites = append(s.seenWrites, writes...)
	if s.writeErr != nil && !s.commitBeforeError {
		return s.writeErr
	}
	if err := s.Store.SaveAll(ctx, writes...); err != nil {
		return err
	}
	return s.writeErr
}

func TestOperationsV1PreservesPayloadAndMixedCurrentRecords(t *testing.T) {
	store, ctx, ref, original, _ := fixture(t)
	current := *original
	current.Version = "goagent-operation-v2"
	current.RunID = durable.OperationStorageID("current")
	current.Data = json.RawMessage(`{"id":"current-op","key":"current","input_hash":"h","status":"waiting","attempts":1,"max_attempts":1,"value":null,"continuation":[{"continuation":{"reason":"approval"}}]}`)
	put(t, store, current.RunID, &current)
	before, err := store.Load(ctx, current.RunID)
	require.NoError(t, err)
	capture := &faultStore{Store: store, padLoadedData: true}
	report, err := migrate.OperationsV1(ctx, capture, []migrate.OperationRef{ref, {Key: "current", ID: "current-op"}}, migrate.OperationOptions{Apply: true})
	require.NoError(t, err)
	require.Len(t, report.Pending, 1)
	require.Len(t, report.Current, 1)
	require.Len(t, capture.seenWrites, 1)
	require.Equal(t, append([]byte(" \n"), original.Data...), []byte(capture.seenWrites[0].Snapshot.Data), "不解码重编码原Data")
	after, err := store.Load(ctx, current.RunID)
	require.NoError(t, err)
	require.Equal(t, before, after, "当前格式不被改写")
}

func TestOperationsV1CancellationDuringPreviewDoesNotWrite(t *testing.T) {
	store, base, ref, original, _ := fixture(t)
	ctx, cancel := context.WithCancel(base)
	fault := &faultStore{Store: store, cancelLoad: cancel}
	_, err := migrate.OperationsV1(ctx, fault, []migrate.OperationRef{ref}, migrate.OperationOptions{Apply: true})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, fault.writes)
	require.Equal(t, len(fault.locked), len(fault.released))
	after, err := store.Load(base, durable.OperationStorageID(ref.Key))
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestOperationsV1PreservesPendingAndFailedRecords(t *testing.T) {
	for _, status := range []string{"pending", "failed"} {
		t.Run(status, func(t *testing.T) {
			store, ctx, ref, snap, _ := fixture(t)
			var record durable.OperationRecord
			require.NoError(t, json.Unmarshal(snap.Data, &record))
			record.Status, record.Attempts, record.FailureKind = status, 2, "interrupted"
			snap.Data, _ = json.Marshal(record)
			put(t, store, durable.OperationStorageID(ref.Key), snap)
			before, err := store.Load(ctx, durable.OperationStorageID(ref.Key))
			require.NoError(t, err)
			_, err = migrate.OperationsV1(ctx, store, []migrate.OperationRef{ref}, migrate.OperationOptions{Apply: true})
			require.NoError(t, err)
			after, err := store.Load(ctx, durable.OperationStorageID(ref.Key))
			require.NoError(t, err)
			require.Equal(t, before.Data, after.Data)
			decoded, err := durable.DecodeOperationRecord(after, ref.Key, ref.ID)
			require.NoError(t, err)
			require.Equal(t, record, *decoded, "未完成结果、错误分类、指纹和尝试上限原样保持")
		})
	}
}

func TestOperationsV1FailureAndUnknownCommit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fault func(*faultStore)
	}{
		{"加载失败", func(s *faultStore) { s.loadErr = errors.New("读取失败") }},
		{"提交前失败", func(s *faultStore) { s.writeErr = errors.New("写入失败") }},
		{"提交结果未知", func(s *faultStore) { s.writeErr = errors.New("连接中断"); s.commitBeforeError = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx, ref, original, _ := fixture(t)
			fault := &faultStore{Store: store}
			tc.fault(fault)
			report, err := migrate.OperationsV1(ctx, fault, []migrate.OperationRef{ref}, migrate.OperationOptions{Apply: true})
			require.Error(t, err)
			require.False(t, report.Applied)
			require.Equal(t, len(fault.locked), len(fault.released))
			after, err := store.Load(ctx, durable.OperationStorageID(ref.Key))
			require.NoError(t, err)
			if !fault.commitBeforeError {
				require.Equal(t, original, after)
			} else {
				require.Equal(t, "goagent-operation-v2", after.Version)
				preview, err := migrate.OperationsV1(ctx, store, []migrate.OperationRef{ref}, migrate.OperationOptions{})
				require.NoError(t, err)
				require.Empty(t, preview.Pending)
				require.Equal(t, []migrate.OperationRef{ref}, preview.Current)
			}
		})
	}
}

func TestOperationsV1LocksInOrderAndReleasesOnFailure(t *testing.T) {
	store, ctx, ref, original, _ := fixture(t)
	second := *original
	second.RunID = durable.OperationStorageID("z")
	second.Data = json.RawMessage(`{"id":"other","key":"z","input_hash":"h","status":"completed","attempts":1,"max_attempts":1,"value":null}`)
	put(t, store, durable.OperationStorageID("z"), &second)
	refs := []migrate.OperationRef{{Key: "z", ID: "other"}, ref}
	fault := &faultStore{Store: store}
	report, err := migrate.OperationsV1(ctx, fault, refs, migrate.OperationOptions{Apply: true})
	require.NoError(t, err)
	require.True(t, report.Applied)
	require.True(t, slices.IsSorted(fault.locked))
	reversed := slices.Clone(fault.locked)
	slices.Reverse(reversed)
	require.Equal(t, reversed, fault.released)
	broken := &faultStore{Store: store, lockErrAt: 2}
	_, err = migrate.OperationsV1(ctx, broken, refs, migrate.OperationOptions{Apply: true})
	require.Error(t, err)
	require.Equal(t, len(broken.locked), len(broken.released))
	require.Zero(t, broken.writes)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = migrate.OperationsV1(canceled, store, refs, migrate.OperationOptions{Apply: true})
	require.ErrorIs(t, err, context.Canceled)
	_, err = migrate.OperationsV1(ctx, store, []migrate.OperationRef{ref, ref}, migrate.OperationOptions{})
	require.Error(t, err)
}
