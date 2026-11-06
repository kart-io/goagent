package providers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kart-io/goagent/codec/json"

	"github.com/stretchr/testify/require"
)

func TestZeroTemperatureRedirectPreservesPayload(t *testing.T) {
	received := make(chan map[string]json.RawMessage, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]json.RawMessage
		if err := json.Decode(r.Body, &payload); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		received <- payload
		if r.URL.Path == "/chat/completions" {
			w.Header().Set("Location", "/redirect/chat/completions")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	req, err := http.NewRequestWithContext(withOpenAITemperature(context.Background(), 0), http.MethodPost, server.URL+"/chat/completions", bytes.NewBufferString(`{"model":"x","metadata":{"integer":9007199254740993}}`))
	require.NoError(t, err)
	response, err := (zeroTemperatureDoer{next: http.DefaultClient}).Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	for i := 0; i < 2; i++ {
		payload := <-received
		require.Equal(t, "0", string(payload["temperature"]))
		require.Equal(t, `{"integer":9007199254740993}`, string(payload["metadata"]))
	}
}
func TestZeroTemperatureDoesNotChangeOtherEndpoints(t *testing.T) {
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.Decode(r.Body, &payload); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		received <- payload
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	req, err := http.NewRequestWithContext(withOpenAITemperature(context.Background(), 0), http.MethodPost, server.URL+"/embeddings", bytes.NewBufferString(`{"input":"内容"}`))
	require.NoError(t, err)
	response, err := (zeroTemperatureDoer{next: http.DefaultClient}).Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, <-received, "temperature")
}
