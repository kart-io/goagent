package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/interfaces"
)

func TestAPIToolAntiAbuseCaps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n, err := strconv.Atoi(r.URL.Query().Get("hop")); err == nil && n > 0 {
			http.Redirect(w, r, "/?hop="+strconv.Itoa(n-1), http.StatusFound)
			return
		}
		_, _ = w.Write(make([]byte, DefaultMaxResponseBytes+1))
	}))
	defer server.Close()
	call := func(b *APIToolBuilder, query string) error {
		_, err := b.WithPrivateNetwork().Build().Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{"url": server.URL + "/?" + query}})
		return err
	}
	require.Error(t, call(NewAPIToolBuilder(), ""), "0 仍是默认 10MiB 上限")
	require.NoError(t, call(NewAPIToolBuilder().WithMaxResponseBytes(Unlimited), ""))
	require.Error(t, call(NewAPIToolBuilder().WithMaxResponseBytes(Unlimited), "hop=11"), "0 仍是默认 10 次重定向")
	require.NoError(t, call(NewAPIToolBuilder().WithMaxResponseBytes(Unlimited).WithMaxRedirects(Unlimited), "hop=11"))
	require.Error(t, call(NewAPIToolBuilder().WithMaxRedirects(-2), "hop=0"), "其他负数拒绝执行")
}
