package management_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
	"github.com/stretchr/testify/require"
)

func offlineDocument(t *testing.T, kind string, value any) mg.Document {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	var m mg.Meta
	require.NoError(t, json.Unmarshal(raw, &m))
	return mg.Document{Kind: kind, ID: m.ID, TenantID: m.TenantID, Revision: m.Revision, CreatedAt: m.CreatedAt, Data: raw}
}

// 替身自身也需回归；否则服务的原子性测试可能在无回滚的 map 上假通过。
func TestOfflineStoreTransactionContract(t *testing.T) {
	ctx := context.Background()
	st := mt.NewStore()
	a := mg.Agent{Meta: mg.Meta{ID: "a", TenantID: "tenant", Revision: 1, CreatedAt: time.Now().UTC()}, Name: "原记录"}
	doc := offlineDocument(t, "agents", a)
	require.NoError(t, st.WithinTx(ctx, func(tx mg.Tx) error { return tx.Insert(ctx, doc) }))
	doc.Data[0] = 'x'
	stored, err := st.Get(ctx, "agents", "tenant", "a")
	require.NoError(t, err)
	require.True(t, json.Valid(stored.Data), "写入后修改调用方切片不得污染存储")
	stored.Data[0] = 'x'
	stored, err = st.Get(ctx, "agents", "tenant", "a")
	require.NoError(t, err)
	require.True(t, json.Valid(stored.Data), "读取必须返回独立副本")
	sentinel := errors.New("模拟事务失败")
	err = st.WithinTx(ctx, func(tx mg.Tx) error {
		changed := a
		changed.Revision++
		changed.Name = "必须回滚"
		if e := tx.CAS(ctx, offlineDocument(t, "agents", changed), 1); e != nil {
			return e
		}
		inside, e := tx.Get(ctx, "agents", "tenant", "a")
		require.NoError(t, e)
		require.Equal(t, int64(2), inside.Revision, "事务内必须读到自己的写入")
		if e := tx.Delete(ctx, "agents", "tenant", "a", 2); e != nil {
			return e
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	stored, err = st.Get(ctx, "agents", "tenant", "a")
	require.NoError(t, err)
	require.Equal(t, int64(1), stored.Revision)
	require.JSONEq(t, string(offlineDocument(t, "agents", a).Data), string(stored.Data))
	require.ErrorIs(t, st.WithinTx(ctx, func(tx mg.Tx) error { return tx.Insert(ctx, stored) }), agentErrors.ErrManagementConflict)
	require.ErrorIs(t, st.WithinTx(ctx, func(tx mg.Tx) error { return tx.Delete(ctx, "agents", "tenant", "a", 2) }), agentErrors.ErrManagementConflict)
	_, err = st.Get(ctx, "agents", "other", "a")
	require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)

	// 同一修订号只有一个成功写入者，并发测试不在协程中使用 Fatal。
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			changed := a
			changed.Revision = 2
			raw, e := json.Marshal(changed)
			if e != nil {
				results <- e
				return
			}
			d := stored
			d.Revision = 2
			d.Data = raw
			results <- st.WithinTx(ctx, func(tx mg.Tx) error { return tx.CAS(ctx, d, 1) })
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for e := range results {
		if e == nil {
			winners++
		} else {
			require.ErrorIs(t, e, agentErrors.ErrManagementConflict)
		}
	}
	require.Equal(t, 1, winners)

	canceled, cancel := context.WithCancel(ctx)
	err = st.WithinTx(canceled, func(tx mg.Tx) error {
		if e := tx.Delete(canceled, "agents", "tenant", "a", 2); e != nil {
			return e
		}
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	_, err = st.Get(ctx, "agents", "tenant", "a")
	require.NoError(t, err, "提交前取消也必须回滚")
}

func TestOfflineStoreFiltersBeforePagination(t *testing.T) {
	ctx := context.Background()
	st := mt.NewStore()
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		task := mg.Task{Meta: mg.Meta{ID: fmt.Sprint(i), TenantID: "tenant", Revision: 1, CreatedAt: base}, AgentID: "a", SessionID: "s", State: mg.Queued}
		switch i {
		case 0:
			task.TenantID = "other"
		case 1:
			task.AgentID = "hidden"
		case 2:
			task.State = mg.Succeeded
		case 5:
			task.CreatedAt = base.Add(time.Hour)
		}
		require.NoError(t, st.Put(ctx, "tasks", task))
	}
	sel := mg.Selection{TenantID: "tenant", Query: mg.Query{AgentID: "a", SessionID: "s", Statuses: []string{"queued"}, CreatedFrom: base, CreatedTo: base, Limit: 1}, Visibility: mg.Visibility{AgentIDs: []string{"a"}}}
	docs, err := st.List(ctx, "tasks", sel)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "3", docs[0].ID)
	sel.Query.Cursor = base.Format(time.RFC3339Nano) + "|3"
	docs, err = st.List(ctx, "tasks", sel)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "4", docs[0].ID, "同时间记录按 ID 稳定翻页")
	sel.Query.Cursor = base.Format(time.RFC3339Nano) + "|4"
	docs, err = st.List(ctx, "tasks", sel)
	require.NoError(t, err)
	require.Empty(t, docs)
	sel.Query.Cursor = "损坏游标"
	_, err = st.List(ctx, "tasks", sel)
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid)
	sel.Query.Cursor = ""
	sel.Visibility = mg.Visibility{}
	docs, err = st.List(ctx, "tasks", sel)
	require.NoError(t, err)
	require.Empty(t, docs, "空授权范围必须拒绝全部结果")
	sel.TenantID = ""
	_, err = st.List(ctx, "tasks", sel)
	require.ErrorIs(t, err, agentErrors.ErrManagementForbidden)
}
