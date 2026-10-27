package agentbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
)

// LookupCase adds argument extraction to a read-only task.
// Only Parameters is exposed to the model. ExpectedArguments is a scoring label.
type LookupCase struct {
	Parameters        json.RawMessage     `json:"parameters"`
	ExpectedArguments json.RawMessage     `json:"expected_arguments"`
	Orders            *OrderLookupBackend `json:"orders,omitempty"`
}

// ValidateLookupCase rejects incomplete labels before any model requests.
// It checks the schema envelope, not the full JSON Schema specification.
func ValidateLookupCase(c Case) error {
	if c.Lookup == nil {
		return nil
	}
	var schema struct {
		Type                 string                     `json:"type"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
		AdditionalProperties json.RawMessage            `json:"additionalProperties"`
	}
	if !hasLookupTool(c) || json.Unmarshal(c.Lookup.Parameters, &schema) != nil || schema.Type != "object" || schema.Properties == nil {
		return fmt.Errorf("case %q: lookup requires evidence and an object schema with properties", c.ID)
	}
	var expected map[string]json.RawMessage
	if json.Unmarshal(c.Lookup.ExpectedArguments, &expected) != nil || expected == nil {
		return fmt.Errorf("case %q: expected_arguments must be a JSON object", c.ID)
	}
	if c.Lookup.Orders != nil {
		var property struct {
			Type string `json:"type"`
		}
		var id string
		if c.Evidence != "" || len(schema.Properties) != 1 || json.Unmarshal(schema.Properties["order_id"], &property) != nil || property.Type != "string" ||
			len(schema.Required) != 1 || schema.Required[0] != "order_id" || string(bytes.TrimSpace(schema.AdditionalProperties)) != "false" ||
			len(expected) != 1 || json.Unmarshal(expected["order_id"], &id) != nil || id == "" {
			return fmt.Errorf("case %q: orders backend requires only a required string order_id, no extra arguments or fixed evidence", c.ID)
		}
		if err := c.Lookup.Orders.validate(); err != nil {
			return fmt.Errorf("case %q: %w", c.ID, err)
		}
	}
	return nil
}

func hasLookupTool(c Case) bool {
	return c.Evidence != "" || (c.Lookup != nil && c.Lookup.Orders != nil)
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
