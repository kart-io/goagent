// Package workflow compiles typed, bounded control-flow graphs into Runnables.
// Graph definitions are built serially; compiled graphs may run concurrently
// when the supplied nodes and caller-owned state support concurrent use.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/kart-io/goagent/core"
)

const (
	// START is the graph entry, with exactly one outgoing edge.
	START = "__start__"
	// END terminates the selected execution path.
	END = "__end__"
)

// ErrMaxSteps means a graph exhausted its node execution budget.
var ErrMaxSteps = errors.New("workflow maximum steps exceeded")

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
		g.configErr = fmt.Errorf("provide at most one workflow config")
	}
	return g
}

// AddNode registers a function, converting panics and cancellation to errors.
func (g *Graph[T]) AddNode(name string, run Node[T]) error {
	if run == nil {
		return fmt.Errorf("node %q has no function", name)
	}
	return g.addRunner(name, func(ctx context.Context, in T, _ func(core.StreamChunk[T]) error) (T, error) {
		return run(ctx, in)
	})
}

// AddRunnable registers an existing node. Its intermediate events are forwarded
// and only its successful final state advances the graph.
func (g *Graph[T]) AddRunnable(name string, run core.Runnable[T, T]) error {
	if isNil(run) {
		return fmt.Errorf("node %q has no runnable", name)
	}
	return g.addRunner(name, func(ctx context.Context, in T, emit func(core.StreamChunk[T]) error) (T, error) {
		return consumeNode(ctx, run, in, emit)
	})
}

func (g *Graph[T]) addRunner(name string, run nodeRunner[T]) error {
	if g == nil || g.nodes == nil {
		return fmt.Errorf("use workflow.NewGraph to initialize the builder")
	}
	if !validName(name) || name == START || name == END {
		return fmt.Errorf("invalid or reserved node name %q", name)
	}
	if isNil(run) {
		return fmt.Errorf("node %q has no runnable", name)
	}
	if _, exists := g.nodes[name]; exists {
		return fmt.Errorf("duplicate node %q", name)
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
	case reflect.Ptr, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// AddEdge adds the sole unconditional successor. References are checked at Compile.
func (g *Graph[T]) AddEdge(from, to string) error {
	if g == nil || g.edges == nil {
		return fmt.Errorf("use workflow.NewGraph to initialize the builder")
	}
	if !validName(from) || !validName(to) || from == END || to == START {
		return fmt.Errorf("invalid workflow edge %q -> %q", from, to)
	}
	if _, ok := g.edges[from]; ok {
		return fmt.Errorf("node %q already has an edge", from)
	}
	if _, ok := g.branches[from]; ok {
		return fmt.Errorf("node %q already has a branch", from)
	}
	g.edges[from] = to
	return nil
}

// AddBranch declares the complete set of possible successors for a router.
// The router sees the completed node's output; only its selected path executes.
func (g *Graph[T]) AddBranch(from string, choose Router[T], targets ...string) error {
	if g == nil || g.branches == nil {
		return fmt.Errorf("use workflow.NewGraph to initialize the builder")
	}
	if !validName(from) || from == START || from == END || choose == nil || len(targets) == 0 {
		return fmt.Errorf("invalid branch on %q", from)
	}
	if _, ok := g.edges[from]; ok {
		return fmt.Errorf("node %q already has an edge", from)
	}
	if _, ok := g.branches[from]; ok {
		return fmt.Errorf("node %q already has a branch", from)
	}
	allowed := map[string]bool{}
	for _, target := range targets {
		if !validName(target) || target == START || allowed[target] {
			return fmt.Errorf("invalid or duplicate branch target %q", target)
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
	return core.NewStreamRunnable(core.StreamModeSteps, snapshot.run), nil
}

func (g *Graph[T]) compile(ctx context.Context) (*compiled[T], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g == nil || g.nodes == nil {
		return nil, fmt.Errorf("use workflow.NewGraph to initialize the builder")
	}
	if g.configErr != nil {
		return nil, g.configErr
	}
	if g.config.MaxSteps < 1 || g.config.MaxConcurrency < 1 {
		return nil, fmt.Errorf("workflow step and concurrency limits must be positive")
	}
	entry, ok := g.edges[START]
	if !ok || len(g.nodes) == 0 {
		return nil, fmt.Errorf("workflow requires nodes and exactly one START edge")
	}
	exists := func(name string) bool { return name == END || g.nodes[name] != nil }
	if !exists(entry) {
		return nil, fmt.Errorf("unknown entry node %q", entry)
	}
	reverse := map[string][]string{}
	adjacency := map[string][]string{}
	keys := slices.Sorted(maps.Keys(g.nodes))
	for from, to := range g.edges {
		if from != START && g.nodes[from] == nil {
			return nil, fmt.Errorf("unknown edge source %q", from)
		}
		if !exists(to) {
			return nil, fmt.Errorf("unknown edge target %q", to)
		}
		adjacency[from] = []string{to}
		reverse[to] = append(reverse[to], from)
	}
	for from, branch := range g.branches {
		if g.nodes[from] == nil {
			return nil, fmt.Errorf("unknown branch source %q", from)
		}
		for to := range branch.targets {
			if !exists(to) {
				return nil, fmt.Errorf("unknown branch target %q", to)
			}
			adjacency[from] = append(adjacency[from], to)
			reverse[to] = append(reverse[to], from)
		}
	}
	reachable := walk(entry, adjacency)
	exits := walk(END, reverse)
	for _, name := range keys {
		if len(adjacency[name]) == 0 {
			return nil, fmt.Errorf("node %q has no successor", name)
		}
		if !reachable[name] {
			return nil, fmt.Errorf("node %q is unreachable", name)
		}
		if !exits[name] {
			return nil, fmt.Errorf("node %q has no path to END", name)
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
