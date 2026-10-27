// Package devtools records and displays execution observations without controlling runs.
package devtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/kart-io/goagent/core/observe"
)

// RecorderConfig sets bounded asynchronous recording. Supply Writer or Path.
type RecorderConfig struct {
	Writer                   io.Writer
	Path                     string
	QueueCapacity, MaxEvents int
}

// RecorderStats reports accepted and dropped events independently of business output.
type RecorderStats struct{ Recorded, Dropped uint64 }

// Recorder is a nonblocking Observer. A custom Writer must eventually return.
type Recorder struct {
	mu               sync.Mutex
	queue            chan observe.Event
	done             chan struct{}
	closed           bool
	max, accepted    int
	dropped, written atomic.Uint64
	err              error
	writer           io.Writer
	owned            io.Closer
}

// NewRecorder starts an isolated writer; it never configures a tracer or model.
func NewRecorder(cfg RecorderConfig) (*Recorder, error) {
	if cfg.QueueCapacity == 0 {
		cfg.QueueCapacity = 64
	}
	if cfg.MaxEvents == 0 {
		cfg.MaxEvents = 10000
	}
	if cfg.QueueCapacity < 1 || cfg.MaxEvents < 1 {
		return nil, fmt.Errorf("positive recorder limits required")
	}
	if cfg.Writer != nil && cfg.Path != "" {
		return nil, fmt.Errorf("choose writer or path")
	}
	r := &Recorder{queue: make(chan observe.Event, cfg.QueueCapacity), done: make(chan struct{}), max: cfg.MaxEvents, writer: cfg.Writer}
	if cfg.Path != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(cfg.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		r.writer = f
		r.owned = f
	}
	if r.writer == nil {
		return nil, fmt.Errorf("recorder writer or new path required")
	}
	go r.write()
	return r, nil
}

// Observe never waits for the writer. Overflow is visible via Stats and a trailer.
func (r *Recorder) Observe(_ context.Context, e observe.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.accepted >= r.max {
		r.dropped.Add(1)
		return
	}
	select {
	case r.queue <- e:
		r.accepted++
	default:
		r.dropped.Add(1)
	}
}
func (r *Recorder) write() {
	defer close(r.done)
	encoder := json.NewEncoder(r.writer)
	for e := range r.queue {
		if r.err == nil {
			if err := encoder.Encode(e); err != nil {
				r.err = err
			} else {
				r.written.Add(1)
			}
		}
	}
	if n := r.dropped.Load(); n > 0 && r.err == nil {
		r.err = encoder.Encode(observe.Event{Type: "events_dropped", Dropped: n})
	}
	if r.owned != nil {
		r.err = errors.Join(r.err, r.owned.Close())
	}
}

// Close drains already accepted events or returns ctx.Err. It cannot abort a stuck Writer.
func (r *Recorder) Close(ctx context.Context) error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	select {
	case <-r.done:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stats can be read concurrently with the writer.
func (r *Recorder) Stats() RecorderStats {
	return RecorderStats{Recorded: r.written.Load(), Dropped: r.dropped.Load()}
}
