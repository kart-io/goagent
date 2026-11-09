package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
	mg "github.com/kart-io/goagent/management"
)

func TestBodyLimitAndDuplicateKeys(t *testing.T) {
	for _, body := range []string{`{"input":1,"input":2}`, `{} {}`, `{"state":"succeeded"}`, strings.Repeat(" ", 1025) + `{}`} {
		var dst struct {
			Input int `json:"input"`
		}
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		require.Error(t, decode(httptest.NewRecorder(), r, 1024, &dst))
	}
	var dst struct {
		Input int `json:"input"`
	}
	require.NoError(t, decode(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(`{"input":2}`)), 1024, &dst))
	require.Equal(t, 2, dst.Input)
}

func TestPublicProjectionPreservesBusinessKeys(t *testing.T) {
	value, e := public(mg.Task{RevisionID: strings.Repeat("a", 64), Input: json.RawMessage(`{"key":"业务字段","execution_key":"用户内容"}`)})
	require.NoError(t, e)
	raw, e := json.Marshal(value)
	require.NoError(t, e)
	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &out))
	require.NotContains(t, out, "execution_key")
	require.JSONEq(t, `"`+strings.Repeat("a", 64)+`"`, string(out["revision_id"]))
	require.JSONEq(t, `{"key":"业务字段","execution_key":"用户内容"}`, string(out["input"]))
}
