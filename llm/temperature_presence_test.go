package llm

import (
	stdjson "encoding/json"
	repositoryjson "github.com/kart-io/goagent/utils/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTemperatureZeroJSONRoundTrip(t *testing.T) {
	for name, codec := range map[string]struct {
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		"标准JSON": {stdjson.Marshal, stdjson.Unmarshal},
		"项目JSON": {repositoryjson.Marshal, repositoryjson.Unmarshal},
	} {
		t.Run(name, func(t *testing.T) {
			var req CompletionRequest
			require.NoError(t, codec.unmarshal([]byte(`{"temperature":0,"messages":[{"role":"user","content":"你好"}]}`), &req))
			data, err := codec.marshal(req)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, stdjson.Unmarshal(data, &payload))
			require.Contains(t, payload, "temperature", "显式零不能在序列化时丢失")
			require.Equal(t, float64(0), payload["temperature"])
			var defaults CompletionRequest
			data, err = codec.marshal(defaults)
			require.NoError(t, err)
			// 重新建 map，避免 Unmarshal 保留旧字段。
			payload = map[string]any{}
			require.NoError(t, stdjson.Unmarshal(data, &payload))
			require.NotContains(t, payload, "temperature", "未设置仍保持可回落语义")
		})
	}
}

func TestTemperaturePresenceAndStableJSON(t *testing.T) {
	for _, raw := range []string{`{"temperature":0}`, `{"temperature":0.3}`, `{}`, `{"temperature":null}`} {
		t.Run(raw, func(t *testing.T) {
			var req CompletionRequest
			require.NoError(t, stdjson.Unmarshal([]byte(raw), &req))
			require.Equal(t, raw != `{}` && raw != `{"temperature":null}`, req.TemperatureSet)
			data, err := repositoryjson.Marshal(req)
			require.NoError(t, err)
			var restored CompletionRequest
			require.NoError(t, repositoryjson.Unmarshal(data, &restored))
			require.Equal(t, req, restored)
		})
	}
	for _, temp := range []float64{0, 0.3} {
		req := CompletionRequest{Messages: []Message{{Role: "user", Content: "内容"}}, Temperature: temp, MaxTokens: 17, Model: "model", Stop: []string{"stop"}, TopP: 0.8}
		type oldJSON CompletionRequest
		before, err := stdjson.Marshal(oldJSON(req))
		require.NoError(t, err)
		after, err := stdjson.Marshal(req)
		require.NoError(t, err)
		require.Equal(t, string(before), string(after), "非显式零的 JSON 必须保持不变")
	}
	var cfg LLMOptions
	require.NoError(t, repositoryjson.Unmarshal([]byte(`{"temperature":0,"model":"x"}`), &cfg))
	require.True(t, cfg.TemperatureSet)
	data, err := repositoryjson.Marshal(cfg)
	require.NoError(t, err)
	var restored LLMOptions
	require.NoError(t, repositoryjson.Unmarshal(data, &restored))
	require.Equal(t, cfg, restored)
}
