package management

import "context"

// finishTerminal 把任务置为终态并在同一事务内释放会话、作废待决审批、写入一条终态投递。
// 所有终态路径都经过这里，宿主收到的投递与任务终态一一对应。调用方负责保存任务本身。
func (s *Service) finishTerminal(ctx context.Context, tx Tx, t *Task, state TaskState) error {
	now := s.cfg.Now().UTC()
	t.State = state
	t.FinishedAt = &now
	if err := s.releaseSession(ctx, tx, *t); err != nil {
		return err
	}
	if err := s.supersedeApprovals(ctx, tx, *t); err != nil {
		return err
	}
	raw, err := encode(map[string]any{"task_id": t.ID, "state": t.State})
	if err != nil {
		return err
	}
	return insert(ctx, tx, "outbox", Delivery{Meta: s.meta(t.Actor), Kind: "task." + string(state), ResourceID: t.ID, Payload: raw, State: "pending", AvailableAt: now})
}
