package httpapi

import (
	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

func TestOpenAPIRoutesMatchHandler(t *testing.T) {
	raw, e := os.ReadFile("openapi.yaml")
	require.NoError(t, e)
	var spec struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(raw, &spec))
	require.Equal(t, "3.1.0", spec.OpenAPI)
	count := 0
	for _, methods := range spec.Paths {
		count += len(methods)
	}
	require.Equal(t, len(Routes), count)
	for _, route := range Routes {
		method, path, _ := strings.Cut(route, " ")
		require.Contains(t, spec.Paths, path)
		require.Contains(t, spec.Paths[path], strings.ToLower(method))
	}
}
