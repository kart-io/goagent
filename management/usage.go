package management

import (
	"context"
	"errors"
	agentErrors "github.com/kart-io/goagent/errors"
	"os"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
)

// usageSnapshot 只读取一次源快照。预算构造器的初始化仅作用于内存副本，
// 避免查询写入会话日志、获取执行租约或在缺失时创建虚假的零账本。
// 每个查询独占该对象；核心预算控制器负责副本内的并发访问。
type usageSnapshot struct {
	ctx       context.Context
	source    durable.Store
	snapshots map[string]*durable.Snapshot
}

func (s *usageSnapshot) Lock(context.Context, string) (func() error, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	return func() error { return nil }, nil
}
func cloneUsageSnapshot(snapshot *durable.Snapshot) *durable.Snapshot {
	copy := *snapshot
	copy.Data = append([]byte(nil), snapshot.Data...)
	return &copy
}
func (s *usageSnapshot) Load(_ context.Context, id string) (*durable.Snapshot, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if snapshot := s.snapshots[id]; snapshot != nil {
		return cloneUsageSnapshot(snapshot), nil
	}
	snapshot, err := s.source.Load(s.ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, agentErrors.ErrManagementUsageMissing
	}
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, agentErrors.ErrManagementInvalid
	}
	s.snapshots[id] = cloneUsageSnapshot(snapshot)
	return cloneUsageSnapshot(snapshot), nil
}
func (s *usageSnapshot) Save(ctx context.Context, id string, snapshot *durable.Snapshot) error {
	return s.SaveAll(ctx, durable.Write{ID: id, Snapshot: snapshot})
}
func (s *usageSnapshot) SaveAll(_ context.Context, writes ...durable.Write) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if err := durable.ValidateWrites(writes); err != nil {
		return err
	}
	for _, write := range writes {
		s.snapshots[write.ID] = cloneUsageSnapshot(write.Snapshot)
	}
	return nil
}

func readUsage(ctx context.Context, config budget.Config, source durable.Store) (*budget.AttributionReport, string, error) {
	view := &usageSnapshot{ctx: ctx, source: source, snapshots: map[string]*durable.Snapshot{}}
	controller, err := budget.NewDurable(config, view)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "unavailable", ctx.Err()
		}
		if errors.Is(err, agentErrors.ErrManagementUsageMissing) {
			return nil, "missing", nil
		}
		return nil, "unavailable", nil
	}
	report, err := controller.Attribution(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "unavailable", ctx.Err()
		}
		return nil, "unavailable", nil
	}
	status := "partial"
	if report.Complete {
		status = "available"
	}
	return &report, status, nil
}
