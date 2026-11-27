package management_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	mt "github.com/kart-io/goagent/management/internal/managementtest"
)

// appendOnly 是不支持删除的会话日志后端。
type appendOnly struct{ session.Store }

// failingDelete 的删除先失败 n 次，模拟删日志途中的故障。
type failingDelete struct {
	*session.MemoryStore
	left int
}

func (f *failingDelete) Delete(ctx context.Context, id string) error {
	if f.left > 0 {
		f.left--
		return errors.New("存储不可用")
	}
	return f.MemoryStore.Delete(ctx, id)
}

func TestDeleteSessionRemovesRowLogAndCommands(t *testing.T) {
	ctx := context.Background()
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	require.NoError(t, f.Service.DeleteSession(ctx, f.Actor, se.ID))

	_, err := f.Service.GetSession(ctx, f.Actor, se.ID)
	require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
	_, err = f.Sessions.Read(ctx, se.LogID, 0)
	require.ErrorIs(t, err, agentErrors.ErrSessionNotFound)
	require.Empty(t, offlineExecutionDocuments(t, f, "commands", mg.Query{SessionID: se.ID}), "会话的准备命令随会话删除")
	require.ErrorIs(t, f.Service.DeleteSession(ctx, f.Actor, se.ID), agentErrors.ErrManagementNotFound)
}

func TestDeleteSessionRefusals(t *testing.T) {
	ctx := context.Background()
	t.Run("还有任务", func(t *testing.T) {
		f := mt.NewFixture(t)
		se := f.ReadySession(t)
		task, err := submit(t, f, se, "t1", nil)
		require.NoError(t, err)
		f.StartWorker(t)
		offlineExecutionWaitTask(t, f, task.ID, mg.Succeeded)
		require.ErrorIs(t, f.Service.DeleteSession(ctx, f.Actor, se.ID), agentErrors.ErrManagementConflict, "任务记录须先由 Prune 回收")
		got, err := f.Service.GetSession(ctx, f.Actor, se.ID)
		require.NoError(t, err)
		require.Equal(t, "ready", got.Status)
	})
	t.Run("有未完成的分叉", func(t *testing.T) {
		f := mt.NewFixture(t)
		se := f.ReadySession(t)
		start, err := session.NewEvent(constants.SessionEventTurnStart, session.TurnStart{Input: "一"})
		require.NoError(t, err)
		end, err := session.NewEvent(constants.SessionEventTurnEnd, session.TurnEnd{})
		require.NoError(t, err)
		_, err = f.Sessions.Append(ctx, se.LogID, 1, start, end)
		require.NoError(t, err)
		_, err = f.Service.ForkSession(ctx, f.Actor, mg.ForkSessionRequest{IdempotencyKey: "fork", SessionID: se.ID, ExpectedRevision: se.Revision})
		require.NoError(t, err)
		require.ErrorIs(t, f.Service.DeleteSession(ctx, f.Actor, se.ID), agentErrors.ErrManagementConflict, "分叉完成前仍要读来源日志")
	})
	t.Run("日志后端不支持删除", func(t *testing.T) {
		f := mt.NewFixture(t)
		se := f.ReadySession(t)
		withConfig(t, f, func(c *mg.Config) { c.Sessions = appendOnly{f.Sessions} })
		require.ErrorIs(t, f.Service.DeleteSession(ctx, f.Actor, se.ID), agentErrors.ErrManagementUnsupported)
		got, err := f.Service.GetSession(ctx, f.Actor, se.ID)
		require.NoError(t, err)
		require.Equal(t, "ready", got.Status)
	})
}

func TestDeleteSessionResumesAfterLogFailure(t *testing.T) {
	ctx := context.Background()
	f := mt.NewFixture(t)
	se := f.ReadySession(t)
	withConfig(t, f, func(c *mg.Config) { c.Sessions = &failingDelete{MemoryStore: f.Sessions, left: 1} })
	require.Error(t, f.Service.DeleteSession(ctx, f.Actor, se.ID))

	got, err := f.Service.GetSession(ctx, f.Actor, se.ID)
	require.NoError(t, err)
	require.Equal(t, "deleting", got.Status)
	_, err = submit(t, f, got, "late", nil)
	require.ErrorIs(t, err, agentErrors.ErrManagementConflict, "删除中的会话不再接收任务")

	require.NoError(t, f.Service.DeleteSession(ctx, f.Actor, se.ID))
	_, err = f.Service.GetSession(ctx, f.Actor, se.ID)
	require.ErrorIs(t, err, agentErrors.ErrManagementNotFound)
}
