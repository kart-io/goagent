package multiagent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/codec/json"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

var errInjected = errors.New("注入的保存失败")

// fault 让匹配的一次（repeat 为真时为每次）Save 失败，用来精确模拟“效果已发生、记录未落盘”时崩溃。
// 它跨“重启”保留，重启只重建被包装的 Store。
type fault struct {
	mu     sync.Mutex
	match  func(id string, s *durable.Snapshot) bool
	repeat bool
	hits   int
}

func (f *fault) arm(repeat bool, match func(id string, s *durable.Snapshot) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.match, f.repeat, f.hits = match, repeat, 0
}

func (f *fault) Hits() int { f.mu.Lock(); defer f.mu.Unlock(); return f.hits }

type faultStore struct {
	durable.Store
	f *fault
}

func (s faultStore) Save(ctx context.Context, id string, snap *durable.Snapshot) error {
	s.f.mu.Lock()
	hit := s.f.match != nil && s.f.match(id, snap)
	if hit {
		s.f.hits++
		if !s.f.repeat {
			s.f.match = nil
		}
	}
	s.f.mu.Unlock()
	if hit {
		return errInjected
	}
	return s.Store.Save(ctx, id, snap)
}

// operation 解码持久操作记录；不是操作记录时返回 false。
func operation(s *durable.Snapshot) (durable.OperationRecord, bool) {
	var r durable.OperationRecord
	if s == nil || s.Version != "goagent-operation-v2" || json.Unmarshal(s.Data, &r) != nil {
		return r, false
	}
	return r, true
}

// checkpoint 解码图检查点的位置字段；不是图检查点时返回 false。
type checkpointView struct {
	Node    string `json:"node"`
	Routing bool   `json:"routing"`
}

func checkpoint(s *durable.Snapshot) (checkpointView, bool) {
	var v checkpointView
	if s == nil || s.Plan == "" || json.Unmarshal(s.Data, &v) != nil {
		return v, false
	}
	return v, true
}

// env 是一次持久运行的存储环境：图存储与预算存储各占一个目录，“重启”时重建全部存储对象与预算。
type env struct {
	t                   *testing.T
	id                  string // 运行 ID，同时是根预算的 RootKey（builder 要求两者一致）
	graphDir, budgetDir string
	budget              budget.Config
	fault               *fault
	store               durable.Store
	controller          *budget.Controller
}

func newEnv(t *testing.T, root string) *env {
	t.Helper()
	e := &env{
		t: t, id: root, graphDir: t.TempDir(), budgetDir: t.TempDir(), fault: &fault{},
		budget: budget.Config{RootKey: root, Version: "v1", MaxModelRequests: 50},
	}
	e.restart()
	return e
}

// restart 重建存储对象与持久根预算，复用同一组目录。
func (e *env) restart() {
	e.t.Helper()
	inner, err := durable.NewFileStore(e.graphDir)
	require.NoError(e.t, err)
	e.store = faultStore{Store: inner, f: e.fault}
	budgetStore, err := durable.NewFileStore(e.budgetDir)
	require.NoError(e.t, err)
	e.controller, err = budget.NewDurable(e.budget, budgetStore)
	require.NoError(e.t, err)
}

// renew 换用新的运行 ID 与新的根预算（RootKey 随之改变），图存储与预算存储目录不变。
func (e *env) renew(id string) {
	e.id, e.budget.RootKey = id, id
	e.restart()
}

// ctx 返回带持久根预算的运行上下文。
func (e *env) ctx() context.Context { return budget.WithController(context.Background(), e.controller) }

// compile 以本环境的存储编译持久图。
func compile[T any](t *testing.T, e *env, g *workflow.Graph[T], version string, approvals ...string) *workflow.DurableGraph[T] {
	t.Helper()
	d, err := g.CompileDurable(context.Background(), workflow.DurableConfig{Store: e.store, Version: version, ApprovalNodes: approvals})
	require.NoError(t, err)
	return d
}

// inspect 读取节点 nodeKey 下 Agent scope 的持久操作记录；先取操作锁，会等仍在运行的 Do 释放。
func inspect(t *testing.T, e *env, nodeKey, scope, id string) *durable.OperationRecord {
	t.Helper()
	return inspectAt(t, e, durable.ChildKey(nodeKey, "agent/"+scope), id)
}

// inspectAt 读取执行键 key 下直接调用 durable.Do 的操作记录。
func inspectAt(t *testing.T, e *env, key, id string) *durable.OperationRecord {
	t.Helper()
	ctx := durable.WithJournal(durable.WithExecutionKey(context.Background(), key), e.store)
	r, err := durable.InspectOperation(ctx, id)
	require.NoError(t, err)
	return r
}

// keyed 记录工厂被调用时的节点执行键，供 inspect 使用。
func keyed(factory AgentFactory, key *atomic.Value) AgentFactory {
	return func(ctx context.Context) (tools.OwnedRunnable[any, *builder.AgentOutput], error) {
		key.Store(workflow.DurableKey(ctx))
		return factory(ctx)
	}
}
