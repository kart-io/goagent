package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/kart-io/goagent/codec/json"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
)

// DurableConfig binds the graph to a host-controlled schema/code version.
// Bump Version when functions, state schemas, policies or nested runnables change;
// topology is fingerprinted automatically, Go closures cannot be fingerprinted.
type DurableConfig struct {
	// StateVersion和Components声明宿主状态与组件版本，变化会阻止旧运行恢复。
	StateVersion  string            `json:"state_version,omitempty"`
	Components    map[string]string `json:"components,omitempty"`
	Store         durable.Store     `json:"-"`
	Version       string            `json:"version"`
	ApprovalNodes []string          `json:"approval_nodes"`
}

// DurableGraph resumes at node boundaries. Branch routing is checkpointed
// separately from successful node output. Parallel branches checkpoint their
// individual results; recovery runs only branches without a committed result.
type DurableGraph[T any] struct {
	graph         *compiled[T]
	store         durable.Store
	version, plan string
	stateVersion  string
	components    map[string]string
	approvals     map[string]bool
	beforeApprove func(context.Context, string, *GraphSnapshot[T]) error
	resumeHandler func(context.Context, string, *GraphSnapshot[T], json.RawMessage) (T, error)
}

// DurableOption 配置与状态类型 T 相关的持久图行为。
type DurableOption[T any] func(*DurableGraph[T])

// WithBeforeApprove 在运行锁内、批准生效前调用 fn，用于核对暂停期间外部数据是否被改动
// （先 Snapshot 再 Run 的两次加锁之间存在窗口）。fn 返回错误或 panic 时拒绝本次批准且不改动状态，
// 之后仍可再次以正确条件批准。fn 拿到的是锁内最新检查点的独立副本，修改它不影响运行。
// 配置 WithResumeHandler 时，先处理人工输入，再用包含拟保存状态的副本执行此校验。
func WithBeforeApprove[T any](fn func(ctx context.Context, id string, snap *GraphSnapshot[T]) error) DurableOption[T] {
	return func(d *DurableGraph[T]) { d.beforeApprove = fn }
}

// ErrApprovalMismatch 表示提供的审批键与等待中的节点不符；检查点不变，仍在等待。
var ErrApprovalMismatch = errors.New("approval does not match waiting node")

// ApprovalRejectedError 是 WithBeforeApprove 的钩子返回错误时的拒绝；检查点不变，仍在等待。
// 钩子 panic 时返回 *errors.PanicError，不属于拒绝。
type ApprovalRejectedError struct{ Err error }

func (e *ApprovalRejectedError) Error() string { return "批准前置校验拒绝: " + e.Err.Error() }
func (e *ApprovalRejectedError) Unwrap() error { return e.Err }

// GraphSnapshot exposes continuation and business state without the storage envelope.
type GraphSnapshot[T any] struct {
	// Preview marks a transient node chunk. Its State is not committed and must
	// not be used as a resume checkpoint or a completed business result.
	Preview bool
	State   T
	Node    string
	Steps   int
	Routing bool
	// Key 是审批等待时 Resume 要用的键；Interrupts 非空时它只是节点意图键，不能用来回应，须用 Interrupts[i].Key。
	Key     string
	Waiting bool
	Done    bool
	// Interrupts 是节点执行中途等待人工输入的中断，按 Key 排序；审批等待时为空。
	Interrupts []InterruptPoint
}

type graphState[T any] struct {
	Parallel   *parallelCheckpoint `json:"parallel,omitzero"`
	Node       string              `json:"node"`
	Routing    bool                `json:"routing"`
	State      T                   `json:"state"`
	Interrupts []InterruptPoint    `json:"interrupts,omitempty"`
}

// DurableKey identifies this node/branch visit across retries. External effect
// services must deduplicate by this key or a stronger business request ID.
func DurableKey(ctx context.Context) string { return durable.ExecutionKey(ctx) }

func graphDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (g *Graph[T]) CompileDurable(ctx context.Context, cfg DurableConfig, opts ...DurableOption[T]) (*DurableGraph[T], error) {
	if cfg.Store == nil || cfg.Version == "" {
		return nil, fmt.Errorf("durable graph requires store and version")
	}
	if strings.TrimSpace(cfg.StateVersion) != cfg.StateVersion {
		return nil, fmt.Errorf("状态版本不能含首尾空白")
	}
	components := maps.Clone(cfg.Components)
	for name, version := range components {
		if name == "" || version == "" || strings.TrimSpace(name) != name || strings.TrimSpace(version) != version {
			return nil, fmt.Errorf("组件名和版本必须非空且无首尾空白")
		}
	}
	graph, err := g.compile(ctx)
	if err != nil {
		return nil, err
	}
	approvals := map[string]bool{}
	for _, node := range cfg.ApprovalNodes {
		if graph.nodes[node] == nil || approvals[node] {
			return nil, fmt.Errorf("unknown or duplicate approval node %q", node)
		}
		approvals[node] = true
	}
	branches := map[string][]string{}
	for name, branch := range graph.branches {
		branches[name] = slices.Sorted(maps.Keys(branch.targets))
	}
	parallel := map[string][]string{}
	for name, cfg := range graph.parallel {
		for _, branch := range cfg.Branches {
			parallel[name] = append(parallel[name], branch.Name)
		}
	}
	descriptor := struct {
		Format       string
		Version      string
		Config       Config
		Nodes        []string
		Edges        map[string]string
		Branches     map[string][]string
		Approvals    map[string]bool
		Parallel     map[string][]string `json:",omitempty"`
		StateVersion string              `json:",omitempty"`
		Components   map[string]string   `json:",omitempty"`
	}{"goagent-graph-v1", cfg.Version, graph.config, slices.Sorted(maps.Keys(graph.nodes)), graph.edges, branches, approvals, parallel, cfg.StateVersion, components}
	d := &DurableGraph[T]{graph: graph, store: cfg.Store, version: cfg.Version, plan: graphDigest(descriptor), stateVersion: cfg.StateVersion, components: components, approvals: approvals}
	for _, opt := range opts {
		opt(d)
	}
	return d, nil
}

func publicGraphSnapshot[T any](s *durable.Snapshot, state graphState[T]) *GraphSnapshot[T] {
	return &GraphSnapshot[T]{State: state.State, Node: state.Node, Steps: s.Next, Routing: state.Routing, Key: s.Key, Waiting: s.Waiting, Done: state.Node == END, Interrupts: slices.Clone(state.Interrupts)}
}

// Run holds the Store's run lock until completion, failure or approval wait.
// initial is ignored on resume. The host must authorize approvalKey before
// supplying it; the key only binds the approval to this precise node visit.
// Effects are at least once if a process dies after the effect but before Save.
// Routers must be pure; they may repeat after a crash before routing is saved.
func (d *DurableGraph[T]) Run(ctx context.Context, id string, initial T, approvalKey string) (*GraphSnapshot[T], error) {
	return d.run(ctx, d.store, id, initial, approvalKey, nil, func(core.StreamChunk[*GraphSnapshot[T]]) error { return nil })
}

// run 把检查点与节点内持久操作都记在 store 中：独立运行时是 DurableConfig.Store，嵌套运行时是父操作日志的 Store。
func (d *DurableGraph[T]) run(ctx context.Context, store durable.Store, id string, initial T, approvalKey string, resumeInput json.RawMessage, emitChunk func(core.StreamChunk[*GraphSnapshot[T]]) error) (result *GraphSnapshot[T], err error) {
	ctx = observe.WithRunID(ctx, id)
	ctx, endGraph := observe.Start(ctx, observe.Event{Type: "graph", Path: "graph"})
	defer func() { endGraph(err, false) }()
	defer func() {
		if r := recover(); r != nil {
			err = agentErrors.NewPanicError("durable graph storage or serialization", r)
		}
	}()
	emit := func(event *core.StreamEvent) error {
		return emitChunk(core.StreamChunk[*GraphSnapshot[T]]{Event: event})
	}
	if d == nil || store == nil || id == "" {
		return nil, fmt.Errorf("compiled durable graph and run ID are required")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	unlock, err := store.Lock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := unlock(); e != nil {
			err = errors.Join(err, e)
		}
	}()
	checkpoint, err := store.Load(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		if approvalKey != "" {
			return nil, fmt.Errorf("cannot approve a run that does not exist")
		}
		data, e := json.Marshal(graphState[T]{Node: d.graph.entry, State: initial})
		if e != nil {
			return nil, e
		}
		checkpoint = &durable.Snapshot{Version: d.version, Plan: d.plan, RunID: id, Data: data}
		if err = store.Save(ctx, id, checkpoint); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	state, err := d.decodeCheckpoint(id, checkpoint)
	if err != nil {
		return nil, err
	}
	result = publicGraphSnapshot(checkpoint, state)
	// Save using fresh snapshots; failed writes leave the last acknowledged state.
	// commit 保存快照，调用方保证 next.Data 是 nextState 的编码；save 用于状态变化后，先编码 nextState。
	commit := func(next durable.Snapshot, nextState graphState[T]) error {
		if e := store.Save(ctx, id, &next); e != nil {
			return e
		}
		checkpoint = &next
		state = nextState
		result = publicGraphSnapshot(checkpoint, state)
		return nil
	}
	save := func(next durable.Snapshot, nextState graphState[T]) error {
		data, e := json.Marshal(nextState)
		if e != nil {
			return e
		}
		next.Data = data
		return commit(next, nextState)
	}
	// 中断等待恢复期间检查点保持等待：重新执行中途崩溃时仍可按“已回应”规则恢复。
	answered := ""
	if len(state.Interrupts) > 0 {
		if ctx, answered, err = d.prepareInterruptResume(ctx, store, state.Interrupts, approvalKey, resumeInput); err != nil {
			return result, err
		}
		approvalKey = ""
	} else {
		if approvalKey != "" && (!checkpoint.Waiting || checkpoint.Key != approvalKey) {
			return result, ErrApprovalMismatch
		}
		if resumeInput != nil || (approvalKey != "" && d.resumeHandler != nil) {
			if approvalKey == "" || (resumeInput != nil && !json.Valid(resumeInput)) {
				return result, &ResumeRejectedError{Err: fmt.Errorf("human input requires a waiting key and valid JSON")}
			}
			next, updated, e := d.applyResume(ctx, id, checkpoint, resumeInput)
			if e != nil {
				return result, e
			}
			if d.beforeApprove != nil {
				if err = d.checkBeforeApprove(ctx, id, &next); err != nil {
					return result, err
				}
			}
			if err = ctx.Err(); err != nil {
				return result, err
			}
			next.Approved, next.Waiting = true, false
			if err = save(next, updated); err != nil {
				return result, err
			}
			approvalKey = ""
		} else if approvalKey != "" && d.beforeApprove != nil {
			if err = d.checkBeforeApprove(ctx, id, checkpoint); err != nil {
				return result, err
			}
		}
	}
	for state.Node != END {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if state.Routing {
			var routingInput graphState[T]
			if err = json.Unmarshal(checkpoint.Data, &routingInput); err != nil {
				return result, err
			}
			target, e := d.route(ctx, state.Node, routingInput.State)
			if e != nil {
				return result, e
			}
			nextState := state
			nextState.Node = target
			nextState.Routing = false
			if err = save(*checkpoint, nextState); err != nil {
				return result, err
			}
			continue
		}
		if checkpoint.Next >= d.graph.config.MaxSteps {
			return result, fmt.Errorf("%w: limit %d", ErrMaxSteps, d.graph.config.MaxSteps)
		}
		// 意图快照只改位置与键，状态沿用 checkpoint.Data，不重新编码。
		intent := *checkpoint
		intent.Key = graphDigest([]any{id, d.plan, state.Node, checkpoint.Next})
		if d.approvals[state.Node] && !intent.Approved {
			if approvalKey == intent.Key {
				intent.Approved = true
				intent.Waiting = false
				approvalKey = ""
			} else {
				intent.Waiting = true
				if err = commit(intent, state); err != nil {
					return result, err
				}
				if err = emit(&core.StreamEvent{Type: "approval_required", Step: state.Node}); err != nil {
					return result, err
				}
				return result, durable.ErrApprovalRequired
			}
		}
		if err = commit(intent, state); err != nil {
			return result, err
		}
		// Decode afresh to prevent mutable node inputs from modifying checkpoint data
		// or the state returned on a failed execution.
		var input graphState[T]
		if err = json.Unmarshal(checkpoint.Data, &input); err != nil {
			return result, err
		}
		if err = emit(&core.StreamEvent{Type: "node_start", Step: state.Node}); err != nil {
			return result, err
		}
		node := d.graph.nodes[state.Node]
		nodeName := state.Node
		runner := core.NewStreamRunnable(core.StreamModeSteps, func(ctx context.Context, in T, done func(core.StreamChunk[T]) error) error {
			var out T
			var e error
			if cfg, ok := d.graph.parallel[nodeName]; ok {
				out, e = runDurableParallel(ctx, in, cfg, d.graph.config.MaxConcurrency, state.Parallel,
					func(progress *parallelCheckpoint) error {
						nextState := state
						nextState.Parallel = progress
						return save(*checkpoint, nextState)
					}, func(event *core.StreamEvent) error {
						copy := *event
						copy.Step = nodeName + "/" + event.Step
						return emit(&copy)
					})
			} else {
				out, e = node(ctx, in, func(chunk core.StreamChunk[T]) error {
					var event *core.StreamEvent
					if chunk.Event != nil {
						copy := *chunk.Event
						copy.Step = nodeName
						if chunk.Event.Step != "" {
							copy.Step += "/" + chunk.Event.Step
						}
						event = &copy
					}
					preview := publicGraphSnapshot(checkpoint, state)
					preview.State = chunk.Data
					preview.Preview, preview.Done, preview.Waiting = true, false, false
					return emitChunk(core.StreamChunk[*GraphSnapshot[T]]{Data: preview, Event: event})
				})
			}
			if e != nil {
				return e
			}
			return done(core.StreamChunk[T]{Done: true, Data: out})
		})
		nodeCtx, endNode := observe.Start(durable.AllowInterrupt(durable.WithJournal(durable.WithExecutionKey(ctx, intent.Key), store)), observe.Event{Type: "node", Path: nodeName, OperationKey: intent.Key})
		output, e := runner.Invoke(nodeCtx, input.State)
		endNode(e, false)
		if e != nil {
			if points, ok := interruptPoints(durable.Waits(e)); ok && ctx.Err() == nil {
				waiting := *checkpoint
				waiting.Waiting = true
				paused := state
				paused.Interrupts = points
				if err = save(waiting, paused); err != nil {
					return result, err
				}
				if i := slices.IndexFunc(points, func(p InterruptPoint) bool { return p.Key == answered && p.Rejected }); answered != "" && i >= 0 {
					rejected := fmt.Errorf("中断输入被拒绝：%s", points[i].Reason)
					if cause := waitCause(durable.Waits(e)); cause != nil {
						rejected = fmt.Errorf("%w: %w", rejected, cause)
					}
					return result, &ResumeRejectedError{Err: rejected}
				}
				if err = emit(&core.StreamEvent{Type: "interrupt_required", Step: nodeName}); err != nil {
					return result, err
				}
				return result, durable.ErrApprovalRequired
			}
			return result, fmt.Errorf("node %q: %w", state.Node, e)
		}
		next := *checkpoint
		next.Next++
		next.Key = ""
		next.Approved = false
		next.Waiting = false
		if err = save(next, graphState[T]{Node: state.Node, Routing: true, State: output}); err != nil {
			return result, err
		}
		if err = emit(&core.StreamEvent{Type: "node_end", Step: nodeName}); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (d *DurableGraph[T]) checkBeforeApprove(ctx context.Context, id string, checkpoint *durable.Snapshot) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = agentErrors.NewPanicError("批准前置校验", r)
		}
	}()
	// 重新解码出独立副本，避免钩子修改的状态随后续 Save 写入检查点。
	state, err := d.decodeCheckpoint(id, checkpoint)
	if err != nil {
		return err
	}
	if err = d.beforeApprove(ctx, id, publicGraphSnapshot(checkpoint, state)); err != nil {
		return &ApprovalRejectedError{Err: err}
	}
	return nil
}

func (d *DurableGraph[T]) decodeCheckpoint(id string, checkpoint *durable.Snapshot) (state graphState[T], err error) {
	defer func() {
		if r := recover(); r != nil {
			err = agentErrors.NewPanicError("checkpoint decoding", r)
		}
	}()
	if checkpoint == nil || checkpoint.Plan != d.plan || checkpoint.Version != d.version || checkpoint.RunID != id || checkpoint.Next < 0 || checkpoint.Next > d.graph.config.MaxSteps || json.Unmarshal(checkpoint.Data, &state) != nil {
		return state, fmt.Errorf("checkpoint does not match versioned graph or is corrupt")
	}
	if state.Node != END && d.graph.nodes[state.Node] == nil {
		return state, fmt.Errorf("checkpoint contains unknown node")
	}
	if err = d.validateContinuation(id, checkpoint, state); err != nil {
		return state, err
	}
	return state, nil
}

// Snapshot loads and validates a continuation without executing it. It takes the
// run lock and may wait for active work; use a bounded context for status reads.
func (d *DurableGraph[T]) Snapshot(ctx context.Context, id string) (out *GraphSnapshot[T], err error) {
	if d == nil {
		return nil, fmt.Errorf("compiled durable graph and run ID are required")
	}
	return d.snapshot(ctx, d.store, id)
}

func (d *DurableGraph[T]) snapshot(ctx context.Context, store durable.Store, id string) (out *GraphSnapshot[T], err error) {
	if store == nil || id == "" {
		return nil, fmt.Errorf("compiled durable graph and run ID are required")
	}
	unlock, err := store.Lock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := unlock(); e != nil {
			err = errors.Join(err, e)
		}
	}()
	cp, err := store.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	state, err := d.decodeCheckpoint(id, cp)
	if err != nil {
		return nil, err
	}
	return publicGraphSnapshot(cp, state), nil
}

func (d *DurableGraph[T]) validateContinuation(id string, cp *durable.Snapshot, state graphState[T]) error {
	interrupted := len(state.Interrupts) > 0
	if state.Parallel != nil {
		cfg, ok := d.graph.parallel[state.Node]
		if !ok || state.Routing || cp.Key == "" || (cp.Waiting && !interrupted) || (d.approvals[state.Node] && !cp.Approved) {
			return fmt.Errorf("corrupt parallel continuation")
		}
		if err := validateParallel[T](cfg, state.Parallel); err != nil {
			return err
		}
	}
	if state.Routing && (state.Node == END || cp.Next == 0) {
		return fmt.Errorf("corrupt graph routing position")
	}
	if state.Node == END && cp.Next == 0 {
		return fmt.Errorf("corrupt graph completion position")
	}
	if state.Routing || state.Node == END {
		if cp.Key != "" || cp.Waiting || cp.Approved || interrupted {
			return fmt.Errorf("corrupt completed node intent")
		}
		return nil
	}
	if interrupted {
		// 中断等待：节点已开始执行；审批节点必须已获批准。
		if !cp.Waiting || cp.Key == "" || (d.approvals[state.Node] && !cp.Approved) {
			return fmt.Errorf("corrupt graph interrupt state")
		}
		for _, p := range state.Interrupts {
			if p.Key == "" || p.ID == "" || !json.Valid(p.Info) {
				return fmt.Errorf("corrupt graph interrupt state")
			}
		}
	} else {
		if cp.Waiting && cp.Approved {
			return fmt.Errorf("corrupt graph approval state")
		}
		if cp.Waiting || cp.Approved {
			if !d.approvals[state.Node] || cp.Key == "" {
				return fmt.Errorf("corrupt graph approval binding")
			}
		}
	}
	expected := graphDigest([]any{id, d.plan, state.Node, cp.Next})
	if cp.Key != "" && cp.Key != expected {
		return fmt.Errorf("corrupt graph execution key")
	}
	return nil
}

func (d *DurableGraph[T]) route(ctx context.Context, name string, state T) (target string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = agentErrors.NewPanicError(fmt.Sprintf("branch %q", name), r)
		}
	}()
	target = d.graph.edges[name]
	if branch, ok := d.graph.branches[name]; ok {
		target, err = branch.choose(ctx, state)
		if err != nil {
			return "", fmt.Errorf("branch %q: %w", name, err)
		}
		if !branch.targets[target] {
			return "", fmt.Errorf("branch %q selected undeclared destination %q", name, target)
		}
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	return target, nil
}
