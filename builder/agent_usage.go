package builder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/observe"
	"github.com/kart-io/goagent/core/usage"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
)

// WithAgentID names a logical role independently of its durable namespace.
// Concurrent calls have separate call keys; role labels must not contain secrets.
func (b *AgentBuilder[C, S]) WithAgentID(id string) *AgentBuilder[C, S] {
	b.config.AgentID = id
	return b
}

func (a *ConfigurableAgent[C, S]) prepareAgentUsage(ctx context.Context) (context.Context, error) {
	c, metered := budget.FromContext(ctx)
	attributed := metered && c.AttributionEnabled()
	if a.config.AgentID == "" && !attributed && !observe.Enabled(ctx) {
		return ctx, nil
	}
	id := a.config.AgentID
	if id != "" && len(a.tools) > 0 {
		if _, ok := a.llmClient.(llm.ToolStreamClient); !ok {
			return nil, fmt.Errorf("Agent usage requires native ToolStreamClient for tool selection")
		}
	}
	if id == "" {
		if attributed {
			return nil, fmt.Errorf("attribution requires WithAgentID")
		}
		id = "anonymous"
	}
	if strings.TrimSpace(id) != id || len(id) > 256 {
		return nil, fmt.Errorf("invalid AgentID")
	}
	parent, nested := usage.FromContext(ctx)
	runID := observe.RunIDFromContext(ctx)
	if nested {
		if runID != "" && runID != parent.RunID {
			return nil, fmt.Errorf("agent observation root conflict")
		}
		runID = parent.RunID
	}
	if metered {
		root := c.Configuration().RootKey
		if runID != "" && runID != root {
			return nil, fmt.Errorf("agent budget root conflict")
		}
		runID = root
	}
	if runID == "" {
		runID = uuid.NewString()
	}
	if observe.Enabled(ctx) && observe.RunIDFromContext(ctx) == "" {
		ctx = observe.WithRunID(ctx, runID)
	}
	key := durable.ExecutionKey(ctx)
	if !durable.HasJournal(ctx) || key == "" {
		key = uuid.NewString()
	}
	in := usage.Invocation{Owner: usage.Owner{RunID: runID, AgentID: id, AgentCallKey: key}, VisitID: uuid.NewString()}
	if nested {
		in.ParentAgentCallKey, in.ParentVisitID = parent.AgentCallKey, parent.VisitID
	}
	ctx = usage.WithInvocation(ctx, in)
	if attributed {
		if err := c.RegisterAgent(ctx, usage.Lineage(ctx)); err != nil {
			return nil, err
		}
	}
	return ctx, nil
}

type modelMetadataKey struct{}
type modelMetadata struct{ Provider, Model string }

func withModelMetadata(ctx context.Context, client llm.Client, req *llm.CompletionRequest) context.Context {
	c, _ := budget.FromContext(ctx)
	if !observe.Enabled(ctx) && !c.AttributionEnabled() {
		return ctx
	}
	m := modelMetadata{Provider: string(client.Provider()), Model: "unknown"}
	if req.Model != "" {
		m.Model = req.Model
	} else if named, ok := client.(interface{ ModelName() string }); ok && named.ModelName() != "" {
		m.Model = named.ModelName()
	}
	return context.WithValue(ctx, modelMetadataKey{}, m)
}

type evidenceSourceKey struct{}

// publishModelUsage records immutable evidence before notifying optional sinks.
// Replay delivery does not change the original source or revision.
func publishModelUsage(ctx context.Context, operation, key string, attempt int, turn *llm.StreamChunk, replayed bool) error {
	e := usage.Evidence{}
	if turn != nil && turn.Usage != nil {
		e = usage.Evidence{Tokens: turn.Usage.Tokens(), KnownFields: turn.Usage.Fields(), Source: "provider"}
		if source, ok := ctx.Value(evidenceSourceKey{}).(string); ok {
			e.Source = source
		}
	}
	meta, _ := ctx.Value(modelMetadataKey{}).(modelMetadata)
	in, _ := usage.FromContext(ctx)
	notification := usage.ModelEvidence{Owner: in.Owner, OperationKey: operation, AttemptKey: key, Attempt: attempt, Provider: meta.Provider, Model: meta.Model, Evidence: e, Replayed: replayed}
	var err error
	if err = e.Validate(); err != nil {
		notification.Evidence = usage.Evidence{}
		if observe.Enabled(ctx) {
			observe.Emit(ctx, observe.Event{Type: "model_usage", Path: "usage", OperationKey: key, Replayed: replayed, ErrorKind: "usage_invalid", Usage: &notification})
		}
		return err
	}
	if c, ok := budget.FromContext(ctx); ok {
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if c.AttributionEnabled() {
			notification.Evidence, err = c.RecordEvidence(saveCtx, key, usage.Lineage(ctx), e)
		} else {
			var tokens *interfaces.TokenUsage
			if turn != nil && turn.Usage != nil && e.KnownFields&usage.BasicFields == usage.BasicFields {
				copy := e.Tokens
				tokens = &copy
			}
			err = c.RecordUsage(saveCtx, key, tokens)
		}
		notification.AccountingCommitted = err == nil && (c.AttributionEnabled() || e.KnownFields == 0 || e.KnownFields == usage.BasicFields)
	}
	if observe.Enabled(ctx) {
		observe.Emit(ctx, observe.Event{Type: "model_usage", Path: "usage", OperationKey: key, Replayed: replayed, Usage: &notification})
	}
	return err
}
