package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderErrorRetryable(t *testing.T) {
	for _, tc := range []struct {
		err  *ProviderError
		want bool
	}{
		{&ProviderError{StatusCode: 429, Err: errors.New("x")}, true},
		{&ProviderError{StatusCode: 503, Err: errors.New("x")}, true},
		{&ProviderError{StatusCode: 400, Err: errors.New("x")}, false},
		{&ProviderError{StatusCode: 401, Err: errors.New("x")}, false},
		{&ProviderError{StatusCode: 404, Err: errors.New("x")}, false},
		{&ProviderError{Err: &net.OpError{Op: "dial", Err: errors.New("refused")}}, true},
		{&ProviderError{Err: fmt.Errorf("read: %w", io.ErrUnexpectedEOF)}, true},
		{&ProviderError{Err: context.Canceled}, false},
		{&ProviderError{Err: fmt.Errorf("wrapped: %w", context.DeadlineExceeded)}, false},
		{&ProviderError{Err: errors.New("decode failure")}, false},
	} {
		require.Equal(t, tc.want, tc.err.Retryable(), tc.err.Err.Error())
	}
	e := &ProviderError{StatusCode: 500, Err: errors.New("server")}
	require.Equal(t, "server", e.Error())
	var target *ProviderError
	require.True(t, errors.As(fmt.Errorf("outer: %w", e), &target))
}
