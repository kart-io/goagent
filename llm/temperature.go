package llm

import (
	"bytes"

	"github.com/kart-io/goagent/codec/json"
)

// temperatureField 保留显式零；未设置时不生成字段，避免把默认行为变为零温。
func temperatureField(value float64, set bool) *float64 {
	if set || value != 0 {
		return &value
	}
	return nil
}

func decodeTemperature(raw json.RawMessage, value *float64, set *bool) error {
	if raw == nil {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*value, *set = 0, false
		return nil
	}
	var decoded float64
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*value, *set = decoded, true
	return nil
}

// MarshalJSON keeps an explicit zero and an unset value semantically distinct
// in persistence and request copies.
func (r CompletionRequest) MarshalJSON() ([]byte, error) {
	type plain CompletionRequest
	// 保持正数及未设置请求的原有 JSON 字段顺序，避免改变其持久意图指纹。
	if r.Temperature != 0 || !r.TemperatureSet {
		return json.Marshal(plain(r))
	}
	return json.Marshal(struct {
		*plain
		Temperature *float64 `json:"temperature,omitzero"`
	}{(*plain)(&r), temperatureField(r.Temperature, r.TemperatureSet)})
}

func (r *CompletionRequest) UnmarshalJSON(data []byte) error {
	type plain CompletionRequest
	next := plain(*r)
	payload := struct {
		*plain
		Temperature json.RawMessage `json:"temperature"`
	}{plain: &next}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if err := decodeTemperature(payload.Temperature, &next.Temperature, &next.TemperatureSet); err != nil {
		return err
	}
	*r = CompletionRequest(next)
	return nil
}

// MarshalJSON does not lose a zero temperature specified by options or JSON
// configuration.
func (c LLMOptions) MarshalJSON() ([]byte, error) {
	type plain LLMOptions
	if c.Temperature != 0 || c.TemperatureSet {
		return json.Marshal(plain(c))
	}
	return json.Marshal(struct {
		*plain
		Temperature *float64 `json:"temperature,omitzero"`
	}{(*plain)(&c), temperatureField(c.Temperature, c.TemperatureSet)})
}

func (c *LLMOptions) UnmarshalJSON(data []byte) error {
	return c.UnmarshalJSONFrom(json.NewDecoder(bytes.NewReader(data)))
}

// UnmarshalJSONFrom follows the caller's decoding options (such as the config
// package's unknown-field errors and duration strings); a v1-style
// UnmarshalJSON cannot access these options.
func (c *LLMOptions) UnmarshalJSONFrom(dec *json.Decoder) error {
	type plain LLMOptions
	next := plain(*c)
	payload := struct {
		*plain
		Temperature json.RawMessage `json:"temperature"`
	}{plain: &next}
	if err := json.DecodeNext(dec, &payload); err != nil {
		return err
	}
	if err := decodeTemperature(payload.Temperature, &next.Temperature, &next.TemperatureSet); err != nil {
		return err
	}
	*c = LLMOptions(next)
	return nil
}
