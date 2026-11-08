package workflow

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	agentErrors "github.com/kart-io/goagent/errors"
)

// Inputs are frozen before dispatch. Outputs are committed independently in the
// parent run's snapshot while its lock remains held. Only the coordinator saves.
type parallelCheckpoint struct {
	Inputs  map[string]json.RawMessage `json:"inputs"`
	Outputs map[string]json.RawMessage `json:"outputs"`
}

func validateParallel[T any](cfg ParallelConfig[T], cp *parallelCheckpoint) error {
	if err := validateParallelStructure(cfg, cp); err != nil {
		return err
	}
	for name, data := range cp.Inputs {
		var input T
		if err := json.Unmarshal(data, &input); err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeStateValidation, "并行输入与业务类型不匹配").WithComponent("workflow").WithOperation("validate_parallel").WithContext("branch", name)
		}
	}
	for name, data := range cp.Outputs {
		var output T
		if err := json.Unmarshal(data, &output); err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeStateValidation, "并行输出与业务类型不匹配").WithComponent("workflow").WithOperation("validate_parallel").WithContext("branch", name)
		}
	}
	return nil
}

// validateParallelStructure 仅验证原始 JSON，不调用宿主业务反序列化。
func validateParallelStructure[T any](cfg ParallelConfig[T], cp *parallelCheckpoint) error {
	if len(cp.Inputs) != len(cfg.Branches) || cp.Outputs == nil {
		return agentErrors.Newf(agentErrors.CodeStateValidation, "corrupt parallel checkpoint").WithComponent("workflow").WithOperation("validate_parallel")
	}
	allowed := map[string]bool{}
	for _, branch := range cfg.Branches {
		allowed[branch.Name] = true
		if !json.Valid(cp.Inputs[branch.Name]) {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "invalid checkpoint input for branch %q", branch.Name).WithComponent("workflow").WithOperation("validate_parallel")
		}
	}
	for name, data := range cp.Outputs {
		if !allowed[name] || !json.Valid(data) {
			return agentErrors.Newf(agentErrors.CodeStateValidation, "invalid checkpoint output for branch %q", name).WithComponent("workflow").WithOperation("validate_parallel")
		}
	}
	return nil
}

func runDurableParallel[T any](ctx context.Context, input T, cfg ParallelConfig[T], limit int, cp *parallelCheckpoint, save func(*parallelCheckpoint) error, emit func(*core.StreamEvent) error) (result T, err error) {
	if cp == nil {
		cp = &parallelCheckpoint{Inputs: map[string]json.RawMessage{}, Outputs: map[string]json.RawMessage{}}
		// All cloning and serialization happens before any branch effect can start.
		for _, b := range cfg.Branches {
			if err = ctx.Err(); err != nil {
				return result, err
			}
			data, e := json.Marshal(cfg.Clone(input))
			if e != nil {
				return result, e
			}
			cp.Inputs[b.Name] = data
		}
		if err = save(cp); err != nil {
			return result, err
		}
	}
	if err = validateParallel(cfg, cp); err != nil {
		return result, err
	}
	type completion struct {
		index int
		data  json.RawMessage
		err   error
	}
	pending := make([]int, 0, len(cfg.Branches))
	for i, b := range cfg.Branches {
		if _, done := cp.Outputs[b.Name]; !done {
			pending = append(pending, i)
		}
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	inputs := cp.Inputs
	jobs := make(chan int)
	results := make(chan completion, len(pending))
	var workers sync.WaitGroup
	for worker := 0; worker < min(limit, len(pending)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				b := cfg.Branches[i]
				// Use a source runner for panic/cancellation handling, just like normal nodes.
				runner := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, _ T, done func(core.StreamChunk[json.RawMessage]) error) error {
					var in T
					if e := json.Unmarshal(inputs[b.Name], &in); e != nil {
						return e
					}
					out, e := b.Run(ctx, in)
					if e != nil {
						return e
					}
					data, e := json.Marshal(out)
					if e != nil {
						return e
					}
					return done(core.StreamChunk[json.RawMessage]{Done: true, Data: data})
				})
				key := graphDigest([]string{DurableKey(ctx), "branch", b.Name})
				var zero T
				branchCtx, endBranch := observe.Start(durable.WithExecutionKey(workCtx, key), observe.Event{Type: "branch", Path: b.Name, OperationKey: key})
				out, e := runner.Invoke(branchCtx, zero)
				endBranch(e, false)
				results <- completion{index: i, data: out, err: e}
			}
		}()
	}
	jobsClosed := false
	closeJobs := func() {
		if !jobsClosed {
			close(jobs)
			jobsClosed = true
		}
	}
	// On an observer/storage/serialization panic, cancel and join workers before
	// the enclosing node runner converts that panic to an error.
	defer func() { cancel(); closeJobs(); workers.Wait() }()
	// Dispatch on the coordinator so event delivery and snapshot writes are serial.
	sent, received := 0, 0
	var firstErr error
	var waits []error
	for received < len(pending) {
		var dispatch chan int
		index := 0
		if firstErr == nil && sent < len(pending) && sent-received < limit {
			dispatch = jobs
			index = pending[sent]
		}
		if received == sent && dispatch == nil {
			break
		}
		select {
		case dispatch <- index:
			sent++
			if e := emit(&core.StreamEvent{Type: "branch_start", Step: cfg.Branches[index].Name}); e != nil {
				firstErr = e
				cancel()
			}
		case done := <-results:
			received++
			name := cfg.Branches[done.index].Name
			if done.err != nil {
				// 等待不是失败：不取消兄弟分支，等全部结束后汇总。
				if durable.Waits(done.err) != nil {
					waits = append(waits, fmt.Errorf("parallel branch %q: %w", name, done.err))
					continue
				}
				if firstErr == nil {
					firstErr = fmt.Errorf("parallel branch %q: %w", name, done.err)
					cancel()
				}
				continue
			}
			// Save successful siblings even after another branch fails. Parent context,
			// not the canceled work context, controls storage availability.
			next := &parallelCheckpoint{Inputs: cp.Inputs, Outputs: maps.Clone(cp.Outputs)}
			next.Outputs[name] = done.data
			if e := save(next); e != nil {
				if firstErr == nil {
					firstErr = e
				}
				cancel()
				continue
			}
			cp = next
			if firstErr == nil {
				if e := emit(&core.StreamEvent{Type: "branch_end", Step: name}); e != nil {
					firstErr = e
					cancel()
				}
			}
		case <-workCtx.Done():
			if firstErr == nil {
				firstErr = workCtx.Err()
			}
			// Disable cancellation selection and drain all started work below.
		}
		if firstErr != nil {
			break
		}
	}
	closeJobs()
	// Drain without selecting a canceled context; never abandon started workers.
	for received < sent {
		done := <-results
		received++
		name := cfg.Branches[done.index].Name
		if done.err != nil {
			if durable.Waits(done.err) != nil {
				waits = append(waits, fmt.Errorf("parallel branch %q: %w", name, done.err))
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("parallel branch %q: %w", name, done.err)
			}
			continue
		}
		next := &parallelCheckpoint{Inputs: cp.Inputs, Outputs: maps.Clone(cp.Outputs)}
		next.Outputs[name] = done.data
		if e := save(next); e != nil {
			if firstErr == nil {
				firstErr = e
			}
		} else {
			cp = next
		}
	}
	workers.Wait()
	if firstErr != nil {
		return result, firstErr
	}
	if len(waits) > 0 {
		return result, errors.Join(waits...)
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	outputs := make([]T, len(cfg.Branches))
	for i, b := range cfg.Branches {
		if err = json.Unmarshal(cp.Outputs[b.Name], &outputs[i]); err != nil {
			return result, err
		}
	}
	// Merge may repeat if it fails or its output checkpoint is lost. It must be pure.
	return cfg.Merge(ctx, input, outputs)
}
