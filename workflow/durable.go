package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
)

// DurableConfig binds the graph to a host-controlled schema/code version.
// Bump Version when functions, state schemas, policies or nested runnables change;
// topology is fingerprinted automatically, Go closures cannot be fingerprinted.
type DurableConfig struct {
	Store         durable.Store
	Version       string
	ApprovalNodes []string
}

// DurableGraph resumes at node boundaries. Branch routing is checkpointed
// separately from successful node output. AddParallel is intentionally rejected:
// this executor does not yet persist individual fork/join branch results.
type DurableGraph[T any] struct {
	graph         *compiled[T]
	store         durable.Store
	version, plan string
	approvals     map[string]bool
}

// GraphSnapshot exposes continuation and business state without the storage envelope.
type GraphSnapshot[T any] struct {
	State   T
	Node    string
	Steps   int
	Routing bool
	Key     string
	Waiting bool
	Done    bool
}

type graphState[T any] struct {
	Node    string `json:"node"`
	Routing bool   `json:"routing"`
	State   T      `json:"state"`
}

type durableKeyContext struct{}

// DurableKey is stable for retries of the same run/plan/node visit, distinct for
// separate loop visits. Side-effect services must deduplicate by this key (or a
// stronger business key). It is not an authentication token.
func DurableKey(ctx context.Context) string {
	key, _ := ctx.Value(durableKeyContext{}).(string)
	return key
}
func graphDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (g *Graph[T]) CompileDurable(ctx context.Context, cfg DurableConfig) (*DurableGraph[T], error) {
	if cfg.Store == nil || cfg.Version == "" {
		return nil, fmt.Errorf("durable graph requires store and version")
	}
	graph, err := g.compile(ctx)
	if err != nil {
		return nil, err
	}
	if len(g.parallel) != 0 {
		return nil, fmt.Errorf("durable graph does not yet support AddParallel; use sequential nodes")
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
	descriptor := struct {
		Format    string
		Version   string
		Config    Config
		Nodes     []string
		Edges     map[string]string
		Branches  map[string][]string
		Approvals map[string]bool
	}{"goagent-graph-v1", cfg.Version, graph.config, slices.Sorted(maps.Keys(graph.nodes)), graph.edges, branches, approvals}
	return &DurableGraph[T]{graph: graph, store: cfg.Store, version: cfg.Version, plan: graphDigest(descriptor), approvals: approvals}, nil
}

func publicGraphSnapshot[T any](s *durable.Snapshot, state graphState[T]) *GraphSnapshot[T] {
	return &GraphSnapshot[T]{State: state.State, Node: state.Node, Steps: s.Next, Routing: state.Routing, Key: s.Key, Waiting: s.Waiting, Done: state.Node == END}
}

// Run holds the Store's run lock until completion, failure or approval wait.
// initial is ignored on resume. The host must authorize approvalKey before
// supplying it; the key only binds the approval to this precise node visit.
// Effects are at least once if a process dies after the effect but before Save.
// Routers must be pure; they may repeat after a crash before routing is saved.
func (d *DurableGraph[T]) Run(ctx context.Context, id string, initial T, approvalKey string) (result *GraphSnapshot[T], err error) {
	if d == nil || d.store == nil || id == "" {
		return nil, fmt.Errorf("compiled durable graph and run ID are required")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	unlock, err := d.store.Lock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	checkpoint, err := d.store.Load(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		if approvalKey != "" {
			return nil, fmt.Errorf("cannot approve a run that does not exist")
		}
		data, e := json.Marshal(graphState[T]{Node: d.graph.entry, State: initial})
		if e != nil {
			return nil, e
		}
		checkpoint = &durable.Snapshot{Version: d.version, Plan: d.plan, RunID: id, Data: data}
		if err = d.store.Save(ctx, id, checkpoint); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	var state graphState[T]
	if checkpoint == nil || checkpoint.Plan != d.plan || checkpoint.Version != d.version || checkpoint.RunID != id || checkpoint.Next < 0 || checkpoint.Next > d.graph.config.MaxSteps || json.Unmarshal(checkpoint.Data, &state) != nil {
		return nil, fmt.Errorf("checkpoint does not match versioned graph or is corrupt")
	}
	if state.Node != END && d.graph.nodes[state.Node] == nil {
		return nil, fmt.Errorf("checkpoint contains unknown node")
	}
	if err = d.validateContinuation(id, checkpoint, state); err != nil {
		return nil, err
	}
	result = publicGraphSnapshot(checkpoint, state)
	if approvalKey != "" && (!checkpoint.Waiting || checkpoint.Key != approvalKey) {
		return result, fmt.Errorf("approval does not match waiting node")
	}
	// Save using fresh snapshots; failed writes leave the last acknowledged state.
	save := func(next durable.Snapshot, nextState graphState[T]) error {
		data, e := json.Marshal(nextState)
		if e != nil {
			return e
		}
		next.Data = data
		if e = d.store.Save(ctx, id, &next); e != nil {
			return e
		}
		checkpoint = &next
		state = nextState
		result = publicGraphSnapshot(checkpoint, state)
		return nil
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
		intent := *checkpoint
		intent.Key = graphDigest([]any{id, d.plan, state.Node, checkpoint.Next})
		if d.approvals[state.Node] && !intent.Approved {
			if approvalKey == intent.Key {
				intent.Approved = true
				intent.Waiting = false
				approvalKey = ""
			} else {
				intent.Waiting = true
				if err = save(intent, state); err != nil {
					return result, err
				}
				return result, durable.ErrApprovalRequired
			}
		}
		if err = save(intent, state); err != nil {
			return result, err
		}
		// Decode afresh to prevent mutable node inputs from modifying checkpoint data
		// or the state returned on a failed execution.
		var input graphState[T]
		if err = json.Unmarshal(checkpoint.Data, &input); err != nil {
			return result, err
		}
		node := d.graph.nodes[state.Node]
		runner := core.NewStreamRunnable(core.StreamModeSteps, func(ctx context.Context, in T, emit func(core.StreamChunk[T]) error) error {
			out, e := node(ctx, in, emit)
			if e != nil {
				return e
			}
			return emit(core.StreamChunk[T]{Done: true, Data: out})
		})
		output, e := runner.Invoke(context.WithValue(ctx, durableKeyContext{}, intent.Key), input.State)
		if e != nil {
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
	}
	return result, nil
}

func (d *DurableGraph[T]) validateContinuation(id string, cp *durable.Snapshot, state graphState[T]) error {
	if state.Routing && (state.Node == END || cp.Next == 0) {
		return fmt.Errorf("corrupt graph routing position")
	}
	if state.Node == END && cp.Next == 0 {
		return fmt.Errorf("corrupt graph completion position")
	}
	if state.Routing || state.Node == END {
		if cp.Key != "" || cp.Waiting || cp.Approved {
			return fmt.Errorf("corrupt completed node intent")
		}
		return nil
	}
	if cp.Waiting && cp.Approved {
		return fmt.Errorf("corrupt graph approval state")
	}
	if cp.Waiting || cp.Approved {
		if !d.approvals[state.Node] || cp.Key == "" {
			return fmt.Errorf("corrupt graph approval binding")
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
		if recover() != nil {
			err = fmt.Errorf("branch %q panicked", name)
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
