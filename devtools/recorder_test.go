package devtools

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core/observe"
	"github.com/stretchr/testify/require"
)

func TestRecorderBoundedAndRedacted(t *testing.T) {
	var out bytes.Buffer
	r, err := NewRecorder(RecorderConfig{Writer: &out, MaxEvents: 2, QueueCapacity: 4})
	require.NoError(t, err)
	for range 5 {
		r.Observe(context.Background(), observe.Event{Type: "node_start", Path: "public-node"})
	}
	require.NoError(t, r.Close(context.Background()))
	stats := r.Stats()
	require.Equal(t, uint64(3), stats.Dropped)
	require.Equal(t, 2, strings.Count(out.String(), "node_start"))
	require.Contains(t, out.String(), "events_dropped")
}

type badWriter struct{}

func (badWriter) Write([]byte) (int, error) { return 0, errors.New("disk failed") }
func TestRecorderReportsDiskFailure(t *testing.T) {
	r, err := NewRecorder(RecorderConfig{Writer: badWriter{}})
	require.NoError(t, err)
	r.Observe(context.Background(), observe.Event{Type: "node_start"})
	require.ErrorContains(t, r.Close(context.Background()), "disk failed")
}
