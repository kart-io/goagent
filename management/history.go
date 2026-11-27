package management

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
)

// HistoryHiddenPrefix is the type prefix of the events management writes for
// itself; business history never returns them.
const HistoryHiddenPrefix = "management/"

// HistoryVisible reports whether an event type belongs to business history.
// A HistoryReader that filters in its own storage must apply the same rule.
func HistoryVisible(typ string) bool {
	return typ != constants.SessionEventBoundarySnapshot && !strings.HasPrefix(typ, HistoryHiddenPrefix)
}

// FilterHistory builds a history page from events ordered by Seq. It keeps
// the visible events with Seq > after whose type is in types (empty types
// means every visible type), stops at limit (0 means no limit) and sets
// NextSeq to the Seq of the last returned event, or after when none is
// returned. Items is never nil. A negative limit returns
// errors.ErrManagementInvalid.
func FilterHistory(events []session.Event, after int64, limit int, types []string) (HistoryPage, error) {
	page := HistoryPage{Items: []session.Event{}, NextSeq: after}
	if limit < 0 {
		return page, agentErrors.ErrManagementInvalid
	}
	for _, ev := range events {
		if ev.Seq <= after || !HistoryVisible(ev.Type) || len(types) > 0 && !slices.Contains(types, ev.Type) {
			continue
		}
		page.Items = append(page.Items, ev)
		page.NextSeq = ev.Seq
		if len(page.Items) == limit {
			break
		}
	}
	return page, nil
}

// SessionHistory implements HistoryReader on any session.Store by reading the
// log and filtering in memory. A store that can filter and page in its own
// storage may implement HistoryReader itself; management/storetest checks
// both against the same contract.
type SessionHistory struct{ Sessions session.Store }

// History returns an empty page for a session that has no log yet.
func (h SessionHistory) History(ctx context.Context, id string, after int64, limit int, types []string) (HistoryPage, error) {
	if limit < 0 {
		return HistoryPage{Items: []session.Event{}, NextSeq: after}, agentErrors.ErrManagementInvalid
	}
	events, err := h.Sessions.Read(ctx, id, after)
	if err != nil && !errors.Is(err, agentErrors.ErrSessionNotFound) {
		return HistoryPage{}, err
	}
	return FilterHistory(events, after, limit, types)
}

var _ HistoryReader = SessionHistory{}
