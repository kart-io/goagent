package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
	"github.com/kart-io/goagent/workflow"
)

// Recording a receipt IS the simulated effect. No shell, deployment, publishing
// API or messaging API is called. A real adapter needs transactional business
// idempotency at the destination; writing this receipt beside an API is not enough.
type effectRecord struct {
	Kind, BusinessID, Payload, ExecutionKey, Receipt string
}

type demoBackend struct {
	model  *modelJudge
	store  durable.Store
	before func(context.Context) error // failure/cancellation injection in tests
	after  func() error                // ambiguous result after a committed effect
}

func openBackend(dir string) (*demoBackend, error) {
	store, err := durable.NewFileStore(filepath.Join(dir, "effects"))
	return &demoBackend{store: store}, err
}

func effectID(kind, id string) string { return kind + ":" + id }

func (b *demoBackend) record(ctx context.Context, kind, id, payload string) (record effectRecord, err error) {
	key := durable.ExecutionKey(ctx)
	if id == "" || key == "" {
		return record, fmt.Errorf("business ID and host execution key are required")
	}
	if b.before != nil {
		if err = b.before(ctx); err != nil {
			return record, err
		}
	}
	entry := effectID(kind, id)
	unlock, err := b.store.Lock(ctx, entry)
	if err != nil {
		return record, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	saved, err := b.store.Load(ctx, entry)
	if err == nil {
		if err = json.Unmarshal(saved.Data, &record); err != nil {
			return record, err
		}
		if saved.RunID != entry || saved.Version != "demo-effects-v1" || record.Kind != kind || record.BusinessID != id || record.Receipt == "" || record.ExecutionKey == "" {
			return record, fmt.Errorf("invalid effect record")
		}
		if record.Payload != payload {
			return record, fmt.Errorf("business ID reused with different payload")
		}
		return record, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return record, err
	}
	sum := sha256.Sum256([]byte(entry))
	record = effectRecord{Kind: kind, BusinessID: id, Payload: payload, ExecutionKey: key, Receipt: "demo-" + hex.EncodeToString(sum[:])}
	data, err := json.Marshal(record)
	if err != nil {
		return record, err
	}
	if err = b.store.Save(ctx, entry, &durable.Snapshot{RunID: entry, Version: "demo-effects-v1", Data: data}); err != nil {
		return record, err
	}
	if b.after != nil {
		err = b.after()
	}
	return record, err
}

type effectTool struct {
	name, schema string
	backend      *demoBackend
}

func (t *effectTool) Name() string        { return t.name }
func (t *effectTool) Description() string { return "Record a local simulated " + t.name + " effect" }
func (t *effectTool) ArgsSchema() string  { return t.schema }
func (t *effectTool) Invoke(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	id, ok := input.Args["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id must be a string")
	}
	payload, err := json.Marshal(input.Args)
	if err != nil {
		return nil, err
	}
	record, err := t.backend.record(ctx, t.name, id, string(payload))
	if err != nil {
		return nil, err
	}
	return &interfaces.ToolOutput{Success: true, Result: record.Receipt}, nil
}

func dispatcher(b *demoBackend, name, schema string) (*tools.Dispatcher, error) {
	r := tools.NewRegistry()
	if err := r.Register(&effectTool{name: name, schema: schema, backend: b}); err != nil {
		return nil, err
	}
	return tools.NewDispatcher(r, tools.DispatchConfig{MaxConcurrency: 1})
}

func addEffect[T any](g *workflow.Graph[T], d *tools.Dispatcher, node, tool string, args func(T) map[string]any, result func(T, string) T) error {
	r, err := workflow.Adapt(d.AsRunnable(), func(s T) (tools.ToolRequest, error) {
		return tools.ToolRequest{Name: tool, Input: &interfaces.ToolInput{Args: args(s)}}, nil
	}, func(s T, output *interfaces.ToolOutput) (T, error) {
		receipt, ok := output.Result.(string)
		if !ok || receipt == "" {
			return s, fmt.Errorf("missing effect receipt")
		}
		return result(s, receipt), nil
	})
	if err != nil {
		return err
	}
	return g.AddRunnable(node, r)
}

func edges[T any](g *workflow.Graph[T], pairs ...[2]string) error {
	for _, pair := range pairs {
		if err := g.AddEdge(pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}
