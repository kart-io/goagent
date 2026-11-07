// Package workflow compiles typed, bounded control-flow graphs into Runnables.
// Graph definitions are built serially; compiled graphs may run concurrently
// when the supplied nodes and caller-owned state support concurrent use.
package workflow

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
)

const (
	// START is the graph entry, with exactly one outgoing edge.
	START = "__start__"
	// END terminates the selected execution path.
	END = "__end__"
)

// Config bounds one graph execution. Zero fields use the defaults.
type Config struct {
	MaxSteps       int // Default 100; includes loop iterations, excludes START/END.
	MaxConcurrency int // Default 4 workers per parallel node.
}

// Node transforms the graph state. Reference-valued fields are not deep copied.
type Node[T any] func(context.Context, T) (T, error)

// Router selects exactly one of the destinations declared by AddBranch.
type Router[T any] func(context.Context, T) (string, error)

type nodeRunner[T any] func(context.Context, T, func(core.StreamChunk[T]) error) (T, error)

type routing[T any] struct {
	choose  Router[T]
	targets map[string]bool
}

// Graph is a mutable builder. Use NewGraph; Compile snapshots its definition.
// Edges select a control path. Use AddParallel for an explicit fork/join.
type Graph[T any] struct {
	config    Config
	configErr error
	nodes     map[string]nodeRunner[T]
	edges     map[string]string
	branches  map[string]routing[T]
	parallel  map[string]ParallelConfig[T]
}

func NewGraph[T any](configs ...Config) *Graph[T] {
	config := Config{}
	if len(configs) > 0 {
		config = configs[0]
	}
	if config.MaxSteps == 0 {
		config.MaxSteps = 100
	}
	if config.MaxConcurrency == 0 {
		config.MaxConcurrency = 4
	}
	g := &Graph[T]{config: config, nodes: map[string]nodeRunner[T]{}, edges: map[string]string{}, branches: map[string]routing[T]{}, parallel: map[string]ParallelConfig[T]{}}
	if len(configs) > 1 {
		g.configErr = agentErrors.Newf(agentErrors.CodeInvalidConfig, "provide at most one workflow config").WithComponent("workflow").WithOperation("new_graph")
	}
	return g
}

// AddNode registers a function, converting panics and cancellation to errors.
func (g *Graph[T]) AddNode(name string, run Node[T]) error {
	if run == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q has no function", name).WithComponent("workflow").WithOperation("add_node")
	}
	return g.addRunner(name, func(ctx context.Context, in T, _ func(core.StreamChunk[T]) error) (T, error) {
		return run(ctx, in)
	})
}

// AddRunnable registers an existing node. Its intermediate events are forwarded
// and only its successful final state advances the graph.
func (g *Graph[T]) AddRunnable(name string, run core.Runnable[T, T]) error {
	if isNil(run) {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q has no runnable", name).WithComponent("workflow").WithOperation("add_runnable")
	}
	return g.addRunner(name, func(ctx context.Context, in T, emit func(core.StreamChunk[T]) error) (T, error) {
		return consumeNode(ctx, run, in, emit)
	})
}

func (g *Graph[T]) addRunner(name string, run nodeRunner[T]) error {
	if g == nil || g.nodes == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "use workflow.NewGraph to initialize the builder").WithComponent("workflow").WithOperation("add_runner")
	}
	if !validName(name) || name == START || name == END {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "invalid or reserved node name %q", name).WithComponent("workflow").WithOperation("add_runner")
	}
	if isNil(run) {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q has no runnable", name).WithComponent("workflow").WithOperation("add_runner")
	}
	if _, exists := g.nodes[name]; exists {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "duplicate node %q", name).WithComponent("workflow").WithOperation("add_runner")
	}
	g.nodes[name] = run
	return nil
}

func validName(name string) bool { return strings.TrimSpace(name) == name && name != "" }
func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// AddEdge adds the sole unconditional successor. References are checked at Compile.
func (g *Graph[T]) AddEdge(from, to string) error {
	if g == nil || g.edges == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "use workflow.NewGraph to initialize the builder").WithComponent("workflow").WithOperation("add_edge")
	}
	if !validName(from) || !validName(to) || from == END || to == START {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "invalid workflow edge %q -> %q", from, to).WithComponent("workflow").WithOperation("add_edge")
	}
	if _, ok := g.edges[from]; ok {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q already has an edge", from).WithComponent("workflow").WithOperation("add_edge")
	}
	if _, ok := g.branches[from]; ok {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q already has a branch", from).WithComponent("workflow").WithOperation("add_edge")
	}
	g.edges[from] = to
	return nil
}

// AddBranch declares the complete set of possible successors for a router.
// The router sees the completed node's output; only its selected path executes.
func (g *Graph[T]) AddBranch(from string, choose Router[T], targets ...string) error {
	if g == nil || g.branches == nil {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "use workflow.NewGraph to initialize the builder").WithComponent("workflow").WithOperation("add_branch")
	}
	if !validName(from) || from == START || from == END || choose == nil || len(targets) == 0 {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "invalid branch on %q", from).WithComponent("workflow").WithOperation("add_branch")
	}
	if _, ok := g.edges[from]; ok {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q already has an edge", from).WithComponent("workflow").WithOperation("add_branch")
	}
	if _, ok := g.branches[from]; ok {
		return agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q already has a branch", from).WithComponent("workflow").WithOperation("add_branch")
	}
	allowed := map[string]bool{}
	for _, target := range targets {
		if !validName(target) || target == START || allowed[target] {
			return agentErrors.Newf(agentErrors.CodeInvalidConfig, "invalid or duplicate branch target %q", target).WithComponent("workflow").WithOperation("add_branch")
		}
		allowed[target] = true
	}
	g.branches[from] = routing[T]{choose: choose, targets: allowed}
	return nil
}

type compiled[T any] struct {
	parallel map[string]ParallelConfig[T]
	config   Config
	entry    string
	nodes    map[string]nodeRunner[T]
	edges    map[string]string
	branches map[string]routing[T]
}

// Compile validates reachability, exits and references, then returns an immutable
// definition backed by core.StreamRunnable. Cycles require an exit path and are
// bounded by MaxSteps at runtime; validation cannot prove a router takes its exit.
func (g *Graph[T]) Compile(ctx context.Context) (*core.StreamRunnable[T, T], error) {
	snapshot, err := g.compile(ctx)
	if err != nil {
		return nil, err
	}
	return core.NewStreamRunnable(core.StreamModeSteps, snapshot.run).WithDescription(func() any { return describe(snapshot) }), nil
}

func (g *Graph[T]) compile(ctx context.Context) (*compiled[T], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g == nil || g.nodes == nil {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "use workflow.NewGraph to initialize the builder").WithComponent("workflow").WithOperation("compile")
	}
	if g.configErr != nil {
		return nil, g.configErr
	}
	if g.config.MaxSteps < 1 || g.config.MaxConcurrency < 1 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "workflow step and concurrency limits must be positive").WithComponent("workflow").WithOperation("compile")
	}
	entry, ok := g.edges[START]
	if !ok || len(g.nodes) == 0 {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "workflow requires nodes and exactly one START edge").WithComponent("workflow").WithOperation("compile")
	}
	exists := func(name string) bool { return name == END || g.nodes[name] != nil }
	if !exists(entry) {
		return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "unknown entry node %q", entry).WithComponent("workflow").WithOperation("compile")
	}
	reverse := map[string][]string{}
	adjacency := map[string][]string{}
	keys := slices.Sorted(maps.Keys(g.nodes))
	for from, to := range g.edges {
		if from != START && g.nodes[from] == nil {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "unknown edge source %q", from).WithComponent("workflow").WithOperation("compile")
		}
		if !exists(to) {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "unknown edge target %q", to).WithComponent("workflow").WithOperation("compile")
		}
		adjacency[from] = []string{to}
		reverse[to] = append(reverse[to], from)
	}
	for from, branch := range g.branches {
		if g.nodes[from] == nil {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "unknown branch source %q", from).WithComponent("workflow").WithOperation("compile")
		}
		for to := range branch.targets {
			if !exists(to) {
				return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "unknown branch target %q", to).WithComponent("workflow").WithOperation("compile")
			}
			adjacency[from] = append(adjacency[from], to)
			reverse[to] = append(reverse[to], from)
		}
	}
	reachable := walk(entry, adjacency)
	exits := walk(END, reverse)
	for _, name := range keys {
		if len(adjacency[name]) == 0 {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q has no successor", name).WithComponent("workflow").WithOperation("compile")
		}
		if !reachable[name] {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q is unreachable", name).WithComponent("workflow").WithOperation("compile")
		}
		if !exits[name] {
			return nil, agentErrors.Newf(agentErrors.CodeInvalidConfig, "node %q has no path to END", name).WithComponent("workflow").WithOperation("compile")
		}
	}
	snapshot := &compiled[T]{parallel: maps.Clone(g.parallel), config: g.config, entry: entry, nodes: maps.Clone(g.nodes), edges: maps.Clone(g.edges), branches: map[string]routing[T]{}}
	for name, branch := range g.branches {
		snapshot.branches[name] = routing[T]{choose: branch.choose, targets: maps.Clone(branch.targets)}
	}
	return snapshot, nil
}

func walk(start string, adjacency map[string][]string) map[string]bool {
	seen := map[string]bool{}
	todo := []string{start}
	for len(todo) > 0 {
		next := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if seen[next] {
			continue
		}
		seen[next] = true
		todo = append(todo, adjacency[next]...)
	}
	return seen
}
