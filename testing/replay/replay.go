// Package replay 把会话日志中记录的持久运行转为回归测试（cut-point replay）：重放一轮时，持久操作（模型回合、工具调用、
// 循环扩展点）按边界逐个决定返回记录的结果，还是用当前代码实跑；实跑结果与记录不同（分歧）之后的边界一律实跑，
// 因为它们的输入已经改变。重放只读原日志，写入留在内存中。设计见 docs/design/HARNESS_CORE_DESIGN.md 第 10.3、17 节。
package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
)

// Boundary 是一个记录过的持久操作。
type Boundary struct {
	// ID 是操作名，如 model/turn/0、tool/turn/0/call/c1、loop/pre/1；不是持久操作的快照时为快照 ID。
	ID string
	// Key 是操作的执行键（边界 + 次数）。
	Key string
	// Kind 由 ID 推断：model、tool、loop 或 other。
	Kind string
	// Recorded 是记录的结果。
	Recorded json.RawMessage
}

// Mode 是一个边界在重放中的处理方式。
type Mode string

const (
	// Replayed 表示返回记录的结果。
	Replayed Mode = "replayed"
	// Live 表示用当前代码实跑。
	Live Mode = "live"
)

// Result 是一个边界的重放结果。
type Result struct {
	Boundary
	Mode Mode
	// Value 是实跑的结果（Mode 为 Live 且操作结束时）。
	Value json.RawMessage
	// Diverged 表示实跑结果与记录不同，或这是记录中没有的新边界。
	Diverged bool
}

// Report 汇总一次重放中各边界的处理，按首次访问的顺序排列。
type Report struct {
	mu      sync.Mutex
	order   []string
	results map[string]*Result
}

// Results 返回各边界的结果。
func (r *Report) Results() []Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Result, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, *r.results[id])
	}
	return out
}

// Diverged 返回第一个发生分歧的边界。
func (r *Report) Diverged() (Result, bool) {
	for _, res := range r.Results() {
		if res.Diverged {
			return res, true
		}
	}
	return Result{}, false
}

func (r *Report) entry(id string, b Boundary, mode Mode) *Result {
	if res, ok := r.results[id]; ok {
		return res
	}
	res := &Result{Boundary: b, Mode: mode}
	r.results[id] = res
	r.order = append(r.order, id)
	return res
}

// Turn 是日志中一轮已结束的持久运行。
type Turn struct {
	// Seq 是 turn/start 事件的序号。
	Seq          int64
	Input        string
	ExecutionKey string
}

// Replayer 从 Source 中会话 SessionID 的日志重放持久运行。
type Replayer struct {
	Source    session.Store
	SessionID string
	// Cut 选中的第一个边界及其之后的全部边界实跑；nil 表示不切。
	Cut func(Boundary) bool
	// Live 选中的边界单独实跑，结果与记录一致时之后的边界继续回放；nil 表示不选。
	Live func(Boundary) bool
}

// Turns 返回日志中已结束（未中止）的持久运行轮次。
func (rp *Replayer) Turns(ctx context.Context) ([]Turn, error) {
	events, err := rp.Source.Read(ctx, rp.SessionID, 0)
	if err != nil {
		return nil, err
	}
	var turns []Turn
	var open *Turn
	for _, ev := range events {
		switch ev.Type {
		case session.TypeTurnStart:
			var d session.TurnStart
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				return nil, fmt.Errorf("事件 %d：%w", ev.Seq, err)
			}
			open = nil
			if d.ExecutionKey != "" {
				open = &Turn{Seq: ev.Seq, Input: d.Input, ExecutionKey: d.ExecutionKey}
			}
		case session.TypeTurnEnd:
			var d session.TurnEnd
			if open != nil && json.Unmarshal(ev.Data, &d) == nil && !d.Aborted {
				turns = append(turns, *open)
			}
			open = nil
		}
	}
	return turns, nil
}

// Prepare 为重放一轮做准备，返回：带该轮执行键与重放用 durable 日志的 ctx；该轮开始前的会话（内存副本，交给
// builder.WithSessionLog，使历史与记录时相同，重放产生的对话也记在其中）；以及记录各边界处理的 Report。
// 用同一 DurableScope、同一会话 ID 构造 Agent，以 turn.Input 调用。
func (rp *Replayer) Prepare(ctx context.Context, turn Turn) (context.Context, session.Store, *Report, error) {
	events, err := rp.Source.Read(ctx, rp.SessionID, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	recorded := map[string]json.RawMessage{}
	var before []session.Event
	for _, ev := range events {
		if ev.Seq < turn.Seq {
			before = append(before, ev)
		}
		if ev.Type == session.TypeBoundarySnapshot {
			var d session.BoundaryData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				return nil, nil, nil, fmt.Errorf("事件 %d：%w", ev.Seq, err)
			}
			recorded[d.ID] = d.Snapshot
		}
	}
	log := session.NewMemoryStore()
	if len(before) > 0 {
		if _, err := log.Append(ctx, rp.SessionID, 0, before...); err != nil {
			return nil, nil, nil, err
		}
	}
	report := &Report{results: map[string]*Result{}}
	store := &overlay{rp: rp, recorded: recorded, saved: map[string]*durable.Snapshot{}, decided: map[string]Mode{}, report: report, locks: session.NewMemoryStore()}
	ctx = durable.WithJournal(durable.WithExecutionKey(ctx, turn.ExecutionKey), store)
	return ctx, log, report, nil
}

// overlay 是重放用的 durable.Store：未被选中实跑的边界读到记录，写入只进内存。
type overlay struct {
	rp       *Replayer
	recorded map[string]json.RawMessage
	locks    *session.MemoryStore

	mu      sync.Mutex
	saved   map[string]*durable.Snapshot
	decided map[string]Mode
	cut     bool // 已切换为全部实跑（Cut 命中或发生分歧）
	report  *Report
}

func (o *overlay) Lock(ctx context.Context, id string) (func() error, error) {
	return o.locks.Lock(ctx, "replay", id)
}

// boundary 解析记录的快照；持久操作的快照中是 durable.OperationRecord。
func boundary(id string, raw json.RawMessage) (Boundary, *durable.Snapshot, error) {
	var snap durable.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Boundary{}, nil, fmt.Errorf("快照 %s 损坏: %w", id, err)
	}
	b := Boundary{ID: id, Kind: "other", Recorded: snap.Data}
	var rec durable.OperationRecord
	if strings.HasPrefix(id, "goagent/operation/") && json.Unmarshal(snap.Data, &rec) == nil && rec.ID != "" {
		b.ID, b.Key, b.Recorded = rec.ID, rec.Key, rec.Value
		switch {
		case strings.HasPrefix(rec.ID, "model/"):
			b.Kind = "model"
		case strings.HasPrefix(rec.ID, "tool/"):
			b.Kind = "tool"
		case strings.HasPrefix(rec.ID, "loop/"):
			b.Kind = "loop"
		}
	}
	return b, &snap, nil
}

func (o *overlay) Load(ctx context.Context, id string) (*durable.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if snap, ok := o.saved[id]; ok {
		cp := *snap
		return &cp, nil
	}
	raw, ok := o.recorded[id]
	if !ok {
		return nil, fmt.Errorf("快照 %s: %w", id, os.ErrNotExist)
	}
	b, snap, err := boundary(id, raw)
	if err != nil {
		return nil, err
	}
	mode, ok := o.decided[id]
	if !ok {
		switch {
		case o.cut:
			mode = Live
		case o.rp.Cut != nil && o.rp.Cut(b):
			o.cut, mode = true, Live
		case o.rp.Live != nil && o.rp.Live(b):
			mode = Live
		default:
			mode = Replayed
		}
		o.decided[id] = mode
		o.report.mu.Lock()
		o.report.entry(id, b, mode)
		o.report.mu.Unlock()
	}
	if mode == Live {
		return nil, fmt.Errorf("快照 %s 按重放设置实跑: %w", id, os.ErrNotExist)
	}
	return snap, nil
}

// Save 记下写入；实跑的持久操作结束时与记录比较，不同或没有记录即为分歧，之后的边界一律实跑。
func (o *overlay) Save(ctx context.Context, id string, snap *durable.Snapshot) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	cp := *snap
	o.saved[id] = &cp
	var rec durable.OperationRecord
	if !strings.HasPrefix(id, "goagent/operation/") || json.Unmarshal(snap.Data, &rec) != nil || (rec.Status != "completed" && rec.Status != "failed") {
		return nil
	}
	b := Boundary{ID: rec.ID, Key: rec.Key, Kind: "other"}
	if raw, ok := o.recorded[id]; ok {
		var err error
		if b, _, err = boundary(id, raw); err != nil {
			return err
		}
	} else if parsed, _, err := boundary(id, mustJSON(snap)); err == nil {
		b.Kind = parsed.Kind
	}
	o.report.mu.Lock()
	defer o.report.mu.Unlock()
	res := o.report.entry(id, b, Live)
	res.Value = rec.Value
	_, had := o.recorded[id]
	res.Diverged = !had || rec.Status != "completed" || !sameJSON(rec.Value, b.Recorded)
	if res.Diverged {
		o.cut = true
	}
	return nil
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// sameJSON 按值比较两段 JSON，与键序、空白无关。
func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return string(mustJSON(x)) == string(mustJSON(y))
}

// ModelTurn 选中第 n 次模型请求（model/turn/n）。
func ModelTurn(n int) func(Boundary) bool {
	id := fmt.Sprintf("model/turn/%d", n)
	return func(b Boundary) bool { return b.ID == id }
}

// Kind 选中某一类边界，如 Kind("tool") 让全部工具调用实跑。
func Kind(kind string) func(Boundary) bool {
	return func(b Boundary) bool { return b.Kind == kind }
}
