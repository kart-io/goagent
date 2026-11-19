package memstore

import (
	"context"
	"strings"

	"github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/session"
	mg "github.com/kart-io/goagent/management"
)

// History 读取会话日志，并遵守业务历史隐藏内部事件的边界。
type History struct{ Sessions session.Store }

func (h History) History(ctx context.Context, id string, after int64, limit int, types []string) (mg.HistoryPage, error) {
	events, err := h.Sessions.Read(ctx, id, after)
	if err != nil {
		return mg.HistoryPage{}, err
	}
	p := mg.HistoryPage{Items: []session.Event{}, NextSeq: after}
	for _, ev := range events {
		if strings.HasPrefix(ev.Type, "management/") || ev.Type == constants.SessionEventBoundarySnapshot {
			continue
		}
		match := len(types) == 0
		for _, typ := range types {
			if typ == ev.Type {
				match = true
			}
		}
		if !match {
			continue
		}
		p.Items = append(p.Items, ev)
		p.NextSeq = ev.Seq
		if len(p.Items) == limit {
			break
		}
	}
	return p, nil
}

var _ mg.HistoryReader = History{}
