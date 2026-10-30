package workflow

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/observe"
)

// Branch is one named function inside an explicit parallel fork/join.
type Branch[T any] struct {
	Name string
	Run  Node[T]
}

// ParallelConfig requires explicit input isolation and a deterministic join.
// Clone must deep copy all fields that branches may mutate; for immutable value
// states it can return its input. Merge runs only after every branch succeeds.
type ParallelConfig[T any] struct {
	Branches []Branch[T]
	Clone    func(T) T
	Merge    func(context.Context, T, []T) (T, error)
}

// AddParallel registers one graph node whose branches run with bounded workers.
// Results reach Merge in registration order. Branches must honor cancellation;
// the node waits for all started workers before returning, including on failure.
func (g *Graph[T]) AddParallel(name string, config ParallelConfig[T]) error {
	if g == nil || g.nodes == nil {
		return fmt.Errorf("use workflow.NewGraph to initialize the builder")
	}
	if len(config.Branches) == 0 || config.Clone == nil || config.Merge == nil {
		return fmt.Errorf("parallel node %q requires branches, Clone and Merge", name)
	}
	names := map[string]bool{}
	for _, branch := range config.Branches {
		if !validName(branch.Name) || names[branch.Name] || branch.Run == nil {
			return fmt.Errorf("invalid or duplicate parallel branch %q", branch.Name)
		}
		names[branch.Name] = true
	}
	config.Branches = slices.Clone(config.Branches)
	limit := g.config.MaxConcurrency
	err := g.addRunner(name, func(ctx context.Context, in T, emit func(core.StreamChunk[T]) error) (T, error) {
		return runParallel(ctx, in, config, limit, emit)
	})
	if err == nil {
		g.parallel[name] = config
	}
	return err
}

func runParallel[T any](ctx context.Context, in T, config ParallelConfig[T], limit int, emit func(core.StreamChunk[T]) error) (result T, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Clone before starting any worker so cloning cannot race branch mutations.
	inputs := make([]T, len(config.Branches))
	for i := range inputs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		inputs[i] = config.Clone(in)
	}
	outputs := make([]T, len(inputs))
	jobs := make(chan int)
	events := make(chan core.StreamChunk[T], min(limit, len(inputs)))
	done := make(chan struct{})
	var workers sync.WaitGroup
	var firstFailure error
	var failureOnce sync.Once
	fail := func(err error) { failureOnce.Do(func() { firstFailure = err; cancel() }) }
	send := func(chunk core.StreamChunk[T]) error {
		select {
		case events <- chunk:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for i := 0; i < min(limit, len(inputs)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				branch := config.Branches[index]
				if err := send(core.StreamChunk[T]{Event: &core.StreamEvent{Type: "branch_start", Step: branch.Name}}); err != nil {
					return
				}
				// StreamRunnable's synchronous Invoke protects the worker from user panics.
				runner := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, in T, emit func(core.StreamChunk[T]) error) error {
					out, err := branch.Run(ctx, in)
					if err != nil {
						return err
					}
					return emit(core.StreamChunk[T]{Done: true, Data: out})
				})
				branchCtx, endBranch := observe.Start(ctx, observe.Event{Type: "branch", Path: branch.Name})
				out, err := runner.Invoke(branchCtx, inputs[index])
				endBranch(err, false)
				if err != nil {
					fail(fmt.Errorf("parallel branch %q: %w", branch.Name, err))
					return
				}
				outputs[index] = out
				if err := send(core.StreamChunk[T]{Event: &core.StreamEvent{Type: "branch_end", Step: branch.Name}}); err != nil {
					return
				}
			}
		}()
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer close(jobs)
		for index := range inputs {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(events); close(done) }()
	// Always drain, even after emit fails. Otherwise a worker blocked on sending
	// events could outlive the graph or prevent the join from completing.
	var emitErr error
	for event := range events {
		if emitErr != nil {
			continue
		}
		if err := emit(event); err != nil {
			emitErr = err
			cancel()
		}
	}
	<-done
	if firstFailure != nil {
		return result, firstFailure
	}
	if emitErr != nil {
		return result, emitErr
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return config.Merge(ctx, in, outputs)
}
