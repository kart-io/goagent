package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

type modelScopeKey struct{}

func ensureModelScope(ctx context.Context) context.Context {
	if ctx.Value(modelScopeKey{}) == nil {
		return context.WithValue(ctx, modelScopeKey{}, new(atomic.Int64))
	}
	return ctx
}
func modelInvocations(ctx context.Context) int64 {
	n, _ := ctx.Value(modelScopeKey{}).(*atomic.Int64)
	if n == nil {
		return 0
	}
	return n.Load()
}
func validateModelBudget(ctx context.Context) error {
	if c, ok := budget.FromContext(ctx); ok && durable.HasJournal(ctx) && !c.IsDurable() {
		return fmt.Errorf("durable model calls require a durable root budget")
	}
	return nil
}
func recordTurnUsage(ctx context.Context, key string, turn *llm.StreamChunk) error {
	c, ok := budget.FromContext(ctx)
	if !ok || turn == nil {
		return nil
	}
	var usage *interfaces.TokenUsage
	if turn.Usage != nil {
		usage = &interfaces.TokenUsage{PromptTokens: turn.Usage.PromptTokens, CompletionTokens: turn.Usage.CompletionTokens, TotalTokens: turn.Usage.TotalTokens}
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return c.RecordUsage(saveCtx, key, usage)
}
func modelOperation(ctx context.Context, id string, intent json.RawMessage, invoke func(context.Context) (*llm.StreamChunk, error), validate func(*llm.StreamChunk) error, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
	if err := validateModelBudget(ctx); err != nil {
		return nil, err
	}
	c, metered := budget.FromContext(ctx)
	key := durable.ChildKey(durable.ExecutionKey(ctx), id)
	if key == "" {
		key = uuid.NewString()
	}
	call := func(callCtx context.Context) (*llm.StreamChunk, error) {
		if metered {
			if err := c.Reserve(callCtx, key); err != nil {
				return nil, err
			}
		}
		if n, ok := ctx.Value(modelScopeKey{}).(*atomic.Int64); ok {
			n.Add(1)
		}
		return invoke(callCtx)
	}
	if !durable.HasJournal(ctx) {
		turn, err := call(ctx)
		return turn, errors.Join(err, recordTurnUsage(ctx, key, turn))
	}
	if metered {
		var err error
		intent, err = json.Marshal(struct {
			Original json.RawMessage
			Budget   budget.Config
		}{intent, c.Configuration()})
		if err != nil {
			return nil, err
		}
	}
	attempted := false
	record, opErr := durable.Do(ctx, id, intent, durable.OperationPolicy{}, func(callCtx context.Context) (json.RawMessage, error) {
		attempted = true
		turn, err := call(callCtx)
		if turn == nil {
			return nil, err
		}
		copy := *turn
		copy.Error = nil
		if err == nil && validate != nil {
			err = validate(&copy)
		}
		data, e := json.Marshal(&copy)
		return data, errors.Join(err, e)
	})
	var turn *llm.StreamChunk
	// Failed operations retain known usage. An intent mismatch is not a replay.
	if record != nil && (opErr == nil || record.Replayed || attempted) && len(record.Value) > 0 && string(record.Value) != "null" {
		var saved llm.StreamChunk
		if err := json.Unmarshal(record.Value, &saved); err != nil {
			return nil, errors.Join(opErr, err)
		}
		turn = &saved
	}
	if turn != nil {
		opErr = errors.Join(opErr, recordTurnUsage(ctx, key, turn))
	}
	if opErr != nil {
		return turn, opErr
	}
	if turn == nil {
		return nil, fmt.Errorf("model operation has no result")
	}
	if record.Replayed {
		if err := emit(AgentEvent{Type: "step", Step: "model_replayed"}); err != nil {
			return turn, err
		}
		if turn.Content != "" {
			if err := emit(AgentEvent{Type: "text", Delta: turn.Content}); err != nil {
				return turn, err
			}
		}
	}
	return turn, nil
}
func readPlainModelTurn(ctx context.Context, id string, client llm.StreamClient, req *llm.CompletionRequest, emit func(AgentEvent) error) (*llm.StreamChunk, error) {
	intent, err := json.Marshal(struct {
		Provider string
		Request  *llm.CompletionRequest
	}{string(client.Provider()), req})
	if err != nil {
		return nil, err
	}
	return modelOperation(ctx, id, intent, func(callCtx context.Context) (*llm.StreamChunk, error) {
		stream, e := client.CompleteStream(callCtx, req)
		if e != nil {
			return nil, e
		}
		return readToolTurn(callCtx, stream, emit)
	}, nil, emit)
}
