package memory

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
)

func TestContextLayoutBoundsAndZero(t *testing.T) {
	h := utf8.RuneCountInString(RecallPrefix + recallHeader)
	for _, v := range []struct {
		total, core int
		valid       bool
	}{{0, 10, true}, {h, 0, true}, {h - 1, 0, false}, {h + 1, -1, false}, {h + 1, 2, false}, {h + 1, 1, true}} {
		_, err := New(Config{Durable: newHookStore(t), RecallRunes: v.total, ContextLayout: &ContextLayout{CoreRunes: v.core}})
		require.Equal(t, v.valid, err == nil, "%+v", v)
	}
}

func TestNilLayoutPreservesOldIdentity(t *testing.T) {
	cfg := Config{RecallK: 5, RecallRunes: 300}
	old := newMemory(t, cfg).Identity()
	require.Equal(t, "memory:23ccadf9f68868a79fc0181f5f28f0500514d04cbe266a05206f60a0801efa6d", old)
	cfg.ContextLayout = &ContextLayout{}
	require.NotEqual(t, old, newMemory(t, cfg).Identity())
}

func TestPinnedCannotBypassCoreLimitThroughRecall(t *testing.T) {
	layout := &ContextLayout{}
	m := newMemory(t, Config{ContextLayout: layout})
	layout.CoreRunes = 500
	ctx := WithNamespace(t.Context(), "alice")
	_, err := m.Remember(ctx, Record{ID: "pin", Content: "coffee private", Pinned: true})
	require.NoError(t, err)
	snap, ex, err := m.ExplainSnapshot(ctx, "coffee")
	require.NoError(t, err)
	require.Empty(t, snap.Refs)
	require.Zero(t, ex.TotalRunes)
	require.Len(t, ex.Omitted, 1)
	require.Equal(t, "core_budget", ex.Omitted[0].Reason)
}

func TestExplainUsesSameSelectionAndContainsNoBody(t *testing.T) {
	store := newHookStore(t)
	h := utf8.RuneCountInString(RecallPrefix + recallHeader)
	m := newMemory(t, Config{Durable: store, ContextLayout: &ContextLayout{CoreRunes: 90}, RecallRunes: h + 120})
	ctx := WithNamespace(t.Context(), "alice")
	for _, r := range []Record{{ID: "a", Content: "coffee 私密甲", Pinned: true}, {ID: "b", Content: strings.Repeat("coffee 私密乙", 12), Pinned: true}, {ID: "c", Content: "coffee 私密丙"}} {
		_, err := m.Remember(ctx, r)
		require.NoError(t, err)
	}
	turn, err := m.Turn(ctx)
	require.NoError(t, err)
	ctx = WithTurn(ctx, turn)
	reads := 0
	store.h.before = func(op, scope, id string) error {
		if op == "load" && id == headID {
			reads++
		}
		return nil
	}
	snap, ex, err := m.ExplainSnapshot(ctx, "coffee")
	require.NoError(t, err)
	require.Equal(t, 1, reads)
	require.Len(t, snap.Refs, 2)
	require.Len(t, ex.Selected, 2)
	require.Len(t, ex.Omitted, 1)
	for i, id := range []string{"a", "c"} {
		require.Equal(t, id, snap.Refs[i].ID)
		require.Equal(t, snap.Refs[i], ex.Selected[i].Ref)
	}
	plain, err := m.Snapshot(ctx, "coffee")
	require.NoError(t, err)
	require.Equal(t, snap, plain)
	out, _, err := m.Hydrate(ctx, snap, snap.Refs)
	require.NoError(t, err)
	require.Equal(t, utf8.RuneCountInString(out.Text), ex.TotalRunes)
	require.LessOrEqual(t, ex.TotalRunes, h+120)
	data, err := json.Marshal(ex)
	require.NoError(t, err)
	require.NotContains(t, string(data), "私密")
	require.NoError(t, m.Forget(ctx, "a"))
	_, err = m.Remember(ctx, Record{Content: "coffee 新", Pinned: true})
	require.NoError(t, err)
	_, refs, err := m.Hydrate(ctx, snap, snap.Refs)
	require.NoError(t, err)
	require.Equal(t, snap.Refs[1:], refs)
}

func TestContextLayoutCountsPinnedSeparators(t *testing.T) {
	ctx := WithNamespace(t.Context(), "alice")
	store := newHookStore(t)
	cfg := Config{Durable: store, RecallRunes: 300}
	base := newMemory(t, cfg)
	cost := 0
	for _, id := range []string{"a", "b"} {
		r, err := base.Remember(ctx, Record{ID: id, Content: "coffee", Pinned: true})
		require.NoError(t, err)
		cost += utf8.RuneCountInString(recordItem(nil, "", r).line)
	}
	for _, extra := range []int{0, 1} {
		cfg.ContextLayout = &ContextLayout{CoreRunes: cost + extra}
		m := newMemory(t, cfg)
		s, ex, err := m.ExplainSnapshot(ctx, "coffee")
		require.NoError(t, err)
		require.Len(t, s.Refs, 1+extra)
		require.Len(t, ex.Omitted, 1-extra)
	}
}
