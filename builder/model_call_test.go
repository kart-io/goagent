package builder

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestBudgetCoversParentChildAndSummary(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	ctx := budget.WithController(context.Background(), c)
	parent := nativeModel([]*llm.StreamChunk{{Delta: "p"}, {Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 3}}})
	_, err = readJournaledTurn(ctx, 0, parent, &llm.CompletionRequest{}, nil, false, func(AgentEvent) error { return nil })
	require.NoError(t, err)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			m := nativeModel([]*llm.StreamChunk{{Delta: "child"}, {Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 4}}})
			_, e := readJournaledTurn(ctx, 0, m, &llm.CompletionRequest{}, nil, false, func(AgentEvent) error { return nil })
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else {
			require.ErrorIs(t, e, budget.ErrBudgetExceeded)
		}
	}
	require.Equal(t, 1, success)
	s, err := c.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, s.ReservedRequests)
	require.Equal(t, 7, s.KnownTokens.TotalTokens)
}
func TestReplayUsageIsNotNewCost(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	cfg := budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1}
	c, err := budget.NewDurable(cfg, store)
	require.NoError(t, err)
	ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store), c)
	model := nativeModel([]*llm.StreamChunk{{Delta: "answer"}, {Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 8}}})
	for range 2 {
		_, err = readJournaledTurn(ctx, 0, model, &llm.CompletionRequest{}, nil, false, func(AgentEvent) error { return nil })
		require.NoError(t, err)
	}
	require.Len(t, model.requests, 1)
	s, err := c.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, s.ReservedRequests)
	require.Equal(t, 8, s.KnownTokens.TotalTokens)
}
func TestFailedModelUsageReconciliation(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	c, err := budget.NewDurable(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1}, store)
	require.NoError(t, err)
	ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store), c)
	m := nativeModel([]*llm.StreamChunk{{Done: true, FinishReason: "length", Usage: &llm.Usage{TotalTokens: 19}}})
	for range 2 {
		_, err = readJournaledTurn(ctx, 0, m, &llm.CompletionRequest{}, nil, false, func(AgentEvent) error { return nil })
		require.Error(t, err)
	}
	require.Len(t, m.requests, 1)
	s, err := c.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 19, s.KnownTokens.TotalTokens)
}
func TestDurableModelRejectsMemoryBudget(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1})
	require.NoError(t, err)
	ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store), c)
	m := nativeModel()
	_, err = readJournaledTurn(ctx, 0, m, &llm.CompletionRequest{}, nil, false, func(AgentEvent) error { return nil })
	require.ErrorContains(t, err, "durable")
	require.Empty(t, m.requests)
}

type usageSaveFailure struct {
	durable.Store
	budgetSaves int
}

func (s *usageSaveFailure) Save(ctx context.Context, id string, snapshot *durable.Snapshot) error {
	if strings.HasPrefix(id, "goagent/budget/") {
		s.budgetSaves++
		if s.budgetSaves == 3 {
			return errors.New("usage save failed")
		}
	}
	return s.Store.Save(ctx, id, snapshot)
}
func TestUsageRegistrationFailureReplaysSavedModel(t *testing.T) {
	file, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	store := &usageSaveFailure{Store: file}
	c, err := budget.NewDurable(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 1}, store)
	require.NoError(t, err)
	ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store), c)
	m := nativeModel([]*llm.StreamChunk{{Delta: "answer"}, {Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 8}}})
	_, err = readJournaledTurn(ctx, 0, m, &llm.CompletionRequest{}, nil, false, func(AgentEvent) error { return nil })
	require.ErrorContains(t, err, "usage save failed")
	_, err = readJournaledTurn(ctx, 0, m, &llm.CompletionRequest{}, nil, false, func(AgentEvent) error { return nil })
	require.NoError(t, err)
	require.Len(t, m.requests, 1)
	s, err := c.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 8, s.KnownTokens.TotalTokens)
}
func TestMemoryBudgetDoesNotTreatHostKeyAsReplay(t *testing.T) {
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	ctx := budget.WithController(durable.WithExecutionKey(context.Background(), "host-node"), c)
	m := nativeModel([]*llm.StreamChunk{{Done: true, Content: "one", FinishReason: "stop"}}, []*llm.StreamChunk{{Done: true, Content: "two", FinishReason: "stop"}})
	for range 2 {
		_, err = readJournaledTurn(ctx, 0, m, &llm.CompletionRequest{}, nil, false, func(AgentEvent) error { return nil })
		require.NoError(t, err)
	}
	require.Len(t, m.requests, 2)
	s, err := c.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, s.ReservedRequests)
}

// A plain StreamClient must keep R2 budget accounting when scope isolation is added.
func TestScopedPlainAgentsShareBudgetAndReplay(t *testing.T) {
	store, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	cfg := budget.Config{RootKey: "plain-agents", Version: "v1", MaxModelRequests: 2}
	for _, replay := range []bool{false, true} {
		controller, err := budget.NewDurable(cfg, store)
		require.NoError(t, err)
		ctx := budget.WithController(durable.WithJournal(durable.WithExecutionKey(context.Background(), "node"), store), controller)
		for _, answer := range []string{"A", "B"} {
			model := &eventModel{MockLLMClient: NewMockLLMClient()}
			if !replay {
				model.chunks = []*llm.StreamChunk{{Delta: answer}, {Done: true, FinishReason: "stop", Usage: &llm.Usage{TotalTokens: 7}}}
			}
			agent, err := NewSimpleBuilder(model).WithDurableScope(answer).Build()
			require.NoError(t, err)
			out, err := agent.AsRunnable().Invoke(ctx, "same question")
			require.NoError(t, err)
			require.Equal(t, answer, out.Result)
			if replay {
				require.Nil(t, model.request, "completed calls must not issue a new model request")
			}
		}
		if !replay {
			model := &eventModel{MockLLMClient: NewMockLLMClient()}
			agent, err := NewSimpleBuilder(model).WithDurableScope("over-budget").Build()
			require.NoError(t, err)
			_, err = agent.AsRunnable().Invoke(ctx, "same question")
			require.ErrorIs(t, err, budget.ErrBudgetExceeded)
			require.Nil(t, model.request)
		}
		ledger, err := controller.Snapshot(ctx)
		require.NoError(t, err)
		require.Equal(t, 2, ledger.ReservedRequests)
		require.Equal(t, 14, ledger.KnownTokens.TotalTokens)
	}
}
