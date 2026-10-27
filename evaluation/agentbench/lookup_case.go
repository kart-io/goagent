package agentbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
)

// LookupCase adds argument extraction to a fixed-evidence, read-only task.
// Only Parameters is exposed to the model. ExpectedArguments is a scoring label.
type LookupCase struct {
	Parameters        json.RawMessage `json:"parameters"`
	ExpectedArguments json.RawMessage `json:"expected_arguments"`
}

// ValidateLookupCase rejects incomplete labels before any model requests.
// It checks the schema envelope, not the full JSON Schema specification.
func ValidateLookupCase(c Case) error {
	if c.Lookup == nil {
		return nil
	}
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if c.Evidence == "" || json.Unmarshal(c.Lookup.Parameters, &schema) != nil || schema.Type != "object" || schema.Properties == nil {
		return fmt.Errorf("case %q: lookup requires evidence and an object schema with properties", c.ID)
	}
	var expected map[string]json.RawMessage
	if json.Unmarshal(c.Lookup.ExpectedArguments, &expected) != nil || expected == nil {
		return fmt.Errorf("case %q: expected_arguments must be a JSON object", c.ID)
	}
	return nil
}

// Compare structural JSON exactly, including missing/extra keys and array order.
// UseNumber avoids silently rounding large numeric labels during comparison.
func equalArguments(expected, actual json.RawMessage) bool {
	decode := func(raw json.RawMessage) (any, error) {
		if !json.Valid(raw) {
			return nil, fmt.Errorf("invalid JSON")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		err := decoder.Decode(&value)
		return value, err
	}
	want, err := decode(expected)
	if err != nil {
		return false
	}
	got, err := decode(actual)
	return err == nil && equalJSONValue(want, got)
}

func equalJSONValue(want, got any) bool {
	switch value := want.(type) {
	case json.Number:
		other, ok := got.(json.Number)
		if !ok {
			return false
		}
		left, ok := new(big.Rat).SetString(string(value))
		if !ok {
			return false
		}
		right, ok := new(big.Rat).SetString(string(other))
		return ok && left.Cmp(right) == 0
	case map[string]any:
		other, ok := got.(map[string]any)
		if !ok || len(value) != len(other) {
			return false
		}
		for key, item := range value {
			peer, exists := other[key]
			if !exists || !equalJSONValue(item, peer) {
				return false
			}
		}
		return true
	case []any:
		other, ok := got.([]any)
		if !ok || len(value) != len(other) {
			return false
		}
		for i, item := range value {
			if !equalJSONValue(item, other[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(want, got)
	}
}
