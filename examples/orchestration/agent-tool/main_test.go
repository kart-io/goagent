package main

import (
	"context"
	"github.com/google/uuid"
	"github.com/kart-io/goagent/core/budget"
	"github.com/kart-io/goagent/interfaces"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestOwnedAgentSessionIsolation(t *testing.T) {
	h := newHost()
	tool, err := h.tool(uuid.NewString)
	require.NoError(t, err)
	c, err := budget.New(budget.Config{RootKey: "root", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	ctx := budget.WithController(context.Background(), c)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, question := range []string{"question-A", "question-B"} {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			_, e := tool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"question": q}})
			errs <- e
		}(question)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.Len(t, h.sessions, 2)
	users := map[string]bool{}
	for session := range h.sessions {
		history, e := h.memory.GetConversationHistory(ctx, session, 0)
		require.NoError(t, e)
		require.Len(t, history, 2)
		users[history[0].Content] = true
	}
	require.True(t, users["question-A"])
	require.True(t, users["question-B"])
	require.Equal(t, int64(2), h.closed.Load())
	s, err := c.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, s.ReservedRequests)
	require.Equal(t, 4, s.KnownTokens.TotalTokens)
	reused, err := h.tool(func() string { return "same-session" })
	require.NoError(t, err)
	c, err = budget.New(budget.Config{RootKey: "negative", Version: "v1", MaxModelRequests: 2})
	require.NoError(t, err)
	ctx = budget.WithController(context.Background(), c)
	_, err = reused.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"question": "first"}})
	require.NoError(t, err)
	_, err = reused.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{"question": "second"}})
	require.ErrorContains(t, err, "reused")
}
