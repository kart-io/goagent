package llm

import (
	"bytes"
	"encoding/json"
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

// MarshalJSON 让显式零与未设置在持久化和请求副本中保持不同语义。
func (r CompletionRequest) MarshalJSON() ([]byte, error) {
	type plain CompletionRequest
	// 保持正数及未设置请求的原有 JSON 字段顺序，避免改变其持久意图指纹。
	if r.Temperature != 0 || !r.TemperatureSet {
		return json.Marshal(plain(r))
	}
	return json.Marshal(struct {
		*plain
		Temperature *float64 `json:"temperature,omitempty"`
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

// MarshalJSON 不丢失由选项或 JSON 配置指定的零温。
func (c LLMOptions) MarshalJSON() ([]byte, error) {
	type plain LLMOptions
	if c.Temperature != 0 || c.TemperatureSet {
		return json.Marshal(plain(c))
	}
	return json.Marshal(struct {
		*plain
		Temperature *float64 `json:"temperature,omitempty"`
	}{(*plain)(&c), temperatureField(c.Temperature, c.TemperatureSet)})
}
func (c *LLMOptions) UnmarshalJSON(data []byte) error {
	type plain LLMOptions
	next := plain(*c)
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
	*c = LLMOptions(next)
	return nil
}
