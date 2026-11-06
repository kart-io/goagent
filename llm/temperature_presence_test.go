package llm

import (
	"testing"

	"github.com/kart-io/goagent/codec/json"

	"github.com/stretchr/testify/require"
)

func TestTemperatureZeroJSONRoundTrip(t *testing.T) {
	for name, c := range map[string]struct {
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		"JSON v2": {func(v any) ([]byte, error) { return json.Marshal(v) }, func(b []byte, v any) error { return json.Unmarshal(b, v) }},
	} {
		t.Run(name, func(t *testing.T) {
			var req CompletionRequest
			require.NoError(t, c.unmarshal([]byte(`{"temperature":0,"messages":[{"role":"user","content":"你好"}]}`), &req))
			data, err := c.marshal(req)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(data, &payload))
			require.Contains(t, payload, "temperature", "显式零不能在序列化时丢失")
			require.Equal(t, float64(0), payload["temperature"])
			var defaults CompletionRequest
			data, err = c.marshal(defaults)
			require.NoError(t, err)
			// 重新建 map，避免 Unmarshal 保留旧字段。
			payload = map[string]any{}
			require.NoError(t, json.Unmarshal(data, &payload))
			require.NotContains(t, payload, "temperature", "未设置仍保持可回落语义")
		})
	}
}

func TestTemperaturePresenceAndStableJSON(t *testing.T) {
	for _, raw := range []string{`{"temperature":0}`, `{"temperature":0.3}`, `{}`, `{"temperature":null}`} {
		t.Run(raw, func(t *testing.T) {
			var req CompletionRequest
			require.NoError(t, json.Unmarshal([]byte(raw), &req))
			require.Equal(t, raw != `{}` && raw != `{"temperature":null}`, req.TemperatureSet)
			data, err := json.Marshal(req)
			require.NoError(t, err)
			var restored CompletionRequest
			require.NoError(t, json.Unmarshal(data, &restored))
			require.Equal(t, req, restored)
		})
	}
	for _, temp := range []float64{0, 0.3} {
		req := CompletionRequest{Messages: []Message{{Role: "user", Content: "内容"}}, Temperature: temp, MaxTokens: 17, Model: "model", Stop: []string{"stop"}, TopP: 0.8}
		type oldJSON CompletionRequest
		before, err := json.Marshal(oldJSON(req))
		require.NoError(t, err)
		after, err := json.Marshal(req)
		require.NoError(t, err)
		require.Equal(t, string(before), string(after), "非显式零的 JSON 必须保持不变")
	}
	var cfg LLMOptions
	require.NoError(t, json.Unmarshal([]byte(`{"temperature":0,"model":"x"}`), &cfg))
	require.True(t, cfg.TemperatureSet)
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	var restored LLMOptions
	require.NoError(t, json.Unmarshal(data, &restored))
	require.Equal(t, cfg, restored)
}
