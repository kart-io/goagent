package management

import (
	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIdempotencyHashPreservesNumbers(t *testing.T) {
	a, err := CanonicalJSON([]byte(`{"b":2,"a":9007199254740992}`))
	require.NoError(t, err)
	b, err := CanonicalJSON([]byte(`{"a":9007199254740992,"b":2}`))
	require.NoError(t, err)
	require.Equal(t, string(a), string(b))
	c, err := CanonicalJSON([]byte(`{"a":9007199254740993,"b":2}`))
	require.NoError(t, err)
	require.NotEqual(t, string(a), string(c))
	_, err = CanonicalJSON([]byte(`{"a":1,"a":2}`))
	require.Error(t, err)
	_, err = CanonicalJSON(json.RawMessage(`{} {}`))
	require.Error(t, err)
}
