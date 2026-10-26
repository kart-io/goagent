package einocompare

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kart-io/goagent/core"
	"github.com/stretchr/testify/require"
)

func TestWorkflowRoutesGoAgentRunnablesAndPropagatesErrors(t *testing.T) {
	primary := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, in string, emit func(core.StreamChunk[string]) error) error {
		if in == "fail" {
			return errors.New("business failure")
		}
		return emit(core.StreamChunk[string]{Done: true, Data: strings.ToUpper(in)})
	})
	fallback := core.NewStreamRunnable(core.StreamModeFinal, func(ctx context.Context, in string, emit func(core.StreamChunk[string]) error) error {
		return emit(core.StreamChunk[string]{Done: true, Data: "fallback:" + in})
	})
	workflow, err := BuildWorkflow(context.Background(), primary, fallback)
	require.NoError(t, err)
	result, err := workflow.Invoke(context.Background(), " hello ")
	require.NoError(t, err)
	require.Equal(t, "HELLO", result)
	result, err = workflow.Invoke(context.Background(), "fallback:question")
	require.NoError(t, err)
	require.Equal(t, "fallback:question", result)
	_, err = workflow.Invoke(context.Background(), " ")
	require.Error(t, err)
	_, err = workflow.Invoke(context.Background(), "fail")
	require.ErrorContains(t, err, "business failure")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = workflow.Invoke(ctx, "hello")
	require.Error(t, err)
}
