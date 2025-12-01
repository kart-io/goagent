package state

import (
	"testing"

	"github.com/kart-io/goagent/utils/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Schema Tests
// =============================================================================

func TestNewStateSchema(t *testing.T) {
	schema := NewStateSchema("test-schema", "1.0.0")

	assert.Equal(t, "test-schema", schema.Name)
	assert.Equal(t, "1.0.0", schema.Version)
	assert.NotNil(t, schema.Fields)
	assert.False(t, schema.StrictMode)
}

func TestStateSchema_AddField(t *testing.T) {
	schema := NewStateSchema("test", "1.0").
		AddField(&FieldSchema{
			Name:     "username",
			Type:     FieldTypeString,
			Required: true,
		}).
		AddField(&FieldSchema{
			Name:    "age",
			Type:    FieldTypeInt,
			Default: 0,
		})

	assert.Len(t, schema.Fields, 2)
	assert.NotNil(t, schema.Fields["username"])
	assert.NotNil(t, schema.Fields["age"])
}

func TestStateSchema_WithStrictMode(t *testing.T) {
	schema := NewStateSchema("test", "1.0").WithStrictMode()
	assert.True(t, schema.StrictMode)
}

func TestStateSchema_Validate(t *testing.T) {
	schema := NewStateSchema("user", "1.0").
		AddField(&FieldSchema{
			Name:     "name",
			Type:     FieldTypeString,
			Required: true,
		}).
		AddField(&FieldSchema{
			Name: "age",
			Type: FieldTypeInt,
		})

	t.Run("Valid data", func(t *testing.T) {
		data := map[string]interface{}{
			"name": "Alice",
			"age":  30,
		}
		err := schema.Validate(data)
		assert.NoError(t, err)
	})

	t.Run("Missing required field", func(t *testing.T) {
		data := map[string]interface{}{
			"age": 30,
		}
		err := schema.Validate(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing required field")
	})

	t.Run("Type mismatch", func(t *testing.T) {
		data := map[string]interface{}{
			"name": 123, // Should be string
			"age":  30,
		}
		err := schema.Validate(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type validation failed")
	})

	t.Run("Extra field in non-strict mode", func(t *testing.T) {
		data := map[string]interface{}{
			"name":  "Alice",
			"extra": "value",
		}
		err := schema.Validate(data)
		assert.NoError(t, err) // Non-strict allows extra fields
	})
}

func TestStateSchema_ValidateStrictMode(t *testing.T) {
	schema := NewStateSchema("user", "1.0").
		WithStrictMode().
		AddField(&FieldSchema{
			Name:     "name",
			Type:     FieldTypeString,
			Required: true,
		})

	t.Run("Undefined field in strict mode", func(t *testing.T) {
		data := map[string]interface{}{
			"name":  "Alice",
			"extra": "not allowed",
		}
		err := schema.Validate(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undefined field in strict mode")
	})
}

func TestStateSchema_CustomValidator(t *testing.T) {
	schema := NewStateSchema("user", "1.0").
		AddField(&FieldSchema{
			Name:     "age",
			Type:     FieldTypeInt,
			Required: true,
			Validator: func(value interface{}) error {
				// Age must be positive
				switch v := value.(type) {
				case int:
					if v < 0 {
						return assert.AnError
					}
				case float64:
					if v < 0 {
						return assert.AnError
					}
				}
				return nil
			},
		})

	t.Run("Valid custom validation", func(t *testing.T) {
		data := map[string]interface{}{"age": 25}
		err := schema.Validate(data)
		assert.NoError(t, err)
	})

	t.Run("Failed custom validation", func(t *testing.T) {
		data := map[string]interface{}{"age": -5}
		err := schema.Validate(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "custom validation failed")
	})
}

func TestStateSchema_NestedObject(t *testing.T) {
	addressSchema := NewStateSchema("address", "1.0").
		AddField(&FieldSchema{
			Name:     "city",
			Type:     FieldTypeString,
			Required: true,
		})

	userSchema := NewStateSchema("user", "1.0").
		AddField(&FieldSchema{
			Name:         "address",
			Type:         FieldTypeObject,
			ObjectSchema: addressSchema,
		})

	t.Run("Valid nested object", func(t *testing.T) {
		data := map[string]interface{}{
			"address": map[string]interface{}{
				"city": "New York",
			},
		}
		err := userSchema.Validate(data)
		assert.NoError(t, err)
	})

	t.Run("Invalid nested object", func(t *testing.T) {
		data := map[string]interface{}{
			"address": map[string]interface{}{
				// Missing required 'city'
			},
		}
		err := userSchema.Validate(data)
		assert.Error(t, err)
	})
}

// =============================================================================
// TypedState Tests
// =============================================================================

func TestNewTypedState(t *testing.T) {
	t.Run("Without schema", func(t *testing.T) {
		ts := NewTypedState(nil)
		assert.NotNil(t, ts)
		assert.Nil(t, ts.Schema())
	})

	t.Run("With schema and defaults", func(t *testing.T) {
		schema := NewStateSchema("test", "1.0").
			AddField(&FieldSchema{
				Name:    "count",
				Type:    FieldTypeInt,
				Default: 10,
			})

		ts := NewTypedState(schema)
		count, err := ts.GetInt("count")
		require.NoError(t, err)
		assert.Equal(t, int64(10), count)
	})
}

func TestTypedState_SetAndGet(t *testing.T) {
	ts := NewTypedState(nil)

	t.Run("Set and Get string", func(t *testing.T) {
		err := ts.Set("name", "Alice")
		require.NoError(t, err)

		value, exists := ts.Get("name")
		assert.True(t, exists)
		assert.Equal(t, "Alice", value)

		str, err := ts.GetString("name")
		require.NoError(t, err)
		assert.Equal(t, "Alice", str)
	})

	t.Run("Set and Get int", func(t *testing.T) {
		err := ts.Set("age", 30)
		require.NoError(t, err)

		num, err := ts.GetInt("age")
		require.NoError(t, err)
		assert.Equal(t, int64(30), num)
	})

	t.Run("Set and Get float", func(t *testing.T) {
		err := ts.Set("score", 95.5)
		require.NoError(t, err)

		f, err := ts.GetFloat("score")
		require.NoError(t, err)
		assert.Equal(t, 95.5, f)
	})

	t.Run("Set and Get bool", func(t *testing.T) {
		err := ts.Set("active", true)
		require.NoError(t, err)

		b, err := ts.GetBool("active")
		require.NoError(t, err)
		assert.True(t, b)
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		_, exists := ts.Get("nonexistent")
		assert.False(t, exists)

		str, err := ts.GetString("nonexistent")
		require.NoError(t, err)
		assert.Equal(t, "", str)
	})

	t.Run("Type mismatch on get", func(t *testing.T) {
		err := ts.Set("number", 42)
		require.NoError(t, err)

		_, err = ts.GetString("number")
		assert.Error(t, err)
	})
}

func TestTypedState_SetWithSchema(t *testing.T) {
	schema := NewStateSchema("user", "1.0").
		WithStrictMode().
		AddField(&FieldSchema{
			Name: "name",
			Type: FieldTypeString,
		}).
		AddField(&FieldSchema{
			Name: "age",
			Type: FieldTypeInt,
		})

	ts := NewTypedState(schema)

	t.Run("Set valid field", func(t *testing.T) {
		err := ts.Set("name", "Bob")
		assert.NoError(t, err)
	})

	t.Run("Set undefined field in strict mode", func(t *testing.T) {
		err := ts.Set("unknown", "value")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undefined field")
	})

	t.Run("Set wrong type", func(t *testing.T) {
		err := ts.Set("age", "not an int")
		assert.Error(t, err)
	})
}

func TestTypedState_SetBatch(t *testing.T) {
	schema := NewStateSchema("test", "1.0").
		AddField(&FieldSchema{
			Name: "a",
			Type: FieldTypeString,
		}).
		AddField(&FieldSchema{
			Name: "b",
			Type: FieldTypeInt,
		})

	ts := NewTypedState(schema)

	t.Run("Valid batch set", func(t *testing.T) {
		err := ts.SetBatch(map[string]interface{}{
			"a": "hello",
			"b": 42,
		})
		assert.NoError(t, err)

		a, _ := ts.GetString("a")
		assert.Equal(t, "hello", a)
	})

	t.Run("Invalid batch set", func(t *testing.T) {
		ts2 := NewTypedState(schema)
		err := ts2.SetBatch(map[string]interface{}{
			"a": 123, // Wrong type
		})
		assert.Error(t, err)
	})
}

func TestTypedState_Delete(t *testing.T) {
	schema := NewStateSchema("test", "1.0").
		AddField(&FieldSchema{
			Name:     "required",
			Type:     FieldTypeString,
			Required: true,
		}).
		AddField(&FieldSchema{
			Name: "optional",
			Type: FieldTypeString,
		})

	ts := NewTypedState(schema)
	ts.Set("required", "value")
	ts.Set("optional", "value")

	t.Run("Delete optional field", func(t *testing.T) {
		err := ts.Delete("optional")
		assert.NoError(t, err)

		_, exists := ts.Get("optional")
		assert.False(t, exists)
	})

	t.Run("Delete required field fails", func(t *testing.T) {
		err := ts.Delete("required")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot delete required field")
	})
}

func TestTypedState_Keys(t *testing.T) {
	ts := NewTypedState(nil)
	ts.Set("a", 1)
	ts.Set("b", 2)
	ts.Set("c", 3)

	keys := ts.Keys()
	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "a")
	assert.Contains(t, keys, "b")
	assert.Contains(t, keys, "c")
}

func TestTypedState_ToMap(t *testing.T) {
	ts := NewTypedState(nil)
	ts.Set("x", 10)
	ts.Set("y", 20)

	m := ts.ToMap()
	assert.Equal(t, 10, m["x"])
	assert.Equal(t, 20, m["y"])

	// Verify it's a copy
	m["z"] = 30
	_, exists := ts.Get("z")
	assert.False(t, exists)
}

func TestTypedState_FromMap(t *testing.T) {
	schema := NewStateSchema("test", "1.0").
		AddField(&FieldSchema{
			Name:     "value",
			Type:     FieldTypeString,
			Required: true,
		})

	ts := NewTypedState(schema)

	t.Run("Valid FromMap", func(t *testing.T) {
		err := ts.FromMap(map[string]interface{}{
			"value": "test",
		})
		assert.NoError(t, err)

		v, _ := ts.GetString("value")
		assert.Equal(t, "test", v)
	})

	t.Run("Invalid FromMap", func(t *testing.T) {
		err := ts.FromMap(map[string]interface{}{
			// Missing required field
		})
		assert.Error(t, err)
	})
}

func TestTypedState_Clone(t *testing.T) {
	schema := NewStateSchema("test", "1.0").
		AddField(&FieldSchema{
			Name: "data",
			Type: FieldTypeString,
		})

	original := NewTypedState(schema)
	original.Set("data", "original")

	clone := original.Clone()

	// Verify clone has same data
	v, _ := clone.GetString("data")
	assert.Equal(t, "original", v)

	// Verify clone is independent
	clone.Set("data", "modified")
	origV, _ := original.GetString("data")
	assert.Equal(t, "original", origV)

	cloneV, _ := clone.GetString("data")
	assert.Equal(t, "modified", cloneV)
}

func TestTypedState_Dirty(t *testing.T) {
	ts := NewTypedState(nil)

	assert.False(t, ts.IsDirty())

	ts.Set("key", "value")
	assert.True(t, ts.IsDirty())

	ts.MarkClean()
	assert.False(t, ts.IsDirty())
}

// =============================================================================
// Serialization Tests
// =============================================================================

func TestTypedState_JSONSerialization(t *testing.T) {
	schema := NewStateSchema("test-schema", "2.0").
		AddField(&FieldSchema{
			Name: "message",
			Type: FieldTypeString,
		}).
		AddField(&FieldSchema{
			Name: "count",
			Type: FieldTypeInt,
		})

	original := NewTypedState(schema)
	original.Set("message", "hello")
	original.Set("count", 42)

	// Marshal
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Verify envelope structure
	var envelope StateEnvelope
	err = json.Unmarshal(data, &envelope)
	require.NoError(t, err)
	assert.Equal(t, "test-schema", envelope.SchemaName)
	assert.Equal(t, "2.0", envelope.SchemaVersion)
	assert.Equal(t, "hello", envelope.Data["message"])

	// Unmarshal to new state with same schema
	restored := NewTypedState(schema)
	err = json.Unmarshal(data, restored)
	require.NoError(t, err)

	msg, _ := restored.GetString("message")
	assert.Equal(t, "hello", msg)
}

func TestTypedState_VersionMismatch(t *testing.T) {
	schemaV1 := NewStateSchema("test", "1.0")
	schemaV2 := NewStateSchema("test", "2.0")

	original := NewTypedState(schemaV1)
	original.Set("data", "value")

	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Try to unmarshal with different version
	restored := NewTypedState(schemaV2)
	err = json.Unmarshal(data, restored)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "schema version mismatch")
}

// =============================================================================
// SchemaRegistry Tests
// =============================================================================

func TestSchemaRegistry_Register(t *testing.T) {
	registry := NewSchemaRegistry()

	schema := NewStateSchema("user", "1.0")

	t.Run("Register new schema", func(t *testing.T) {
		err := registry.Register(schema)
		assert.NoError(t, err)
	})

	t.Run("Duplicate registration fails", func(t *testing.T) {
		err := registry.Register(schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})
}

func TestSchemaRegistry_Get(t *testing.T) {
	registry := NewSchemaRegistry()
	schema := NewStateSchema("test", "1.0")
	registry.Register(schema)

	t.Run("Get existing schema", func(t *testing.T) {
		found, err := registry.Get("test", "1.0")
		require.NoError(t, err)
		assert.Equal(t, schema, found)
	})

	t.Run("Get non-existent schema", func(t *testing.T) {
		_, err := registry.Get("nonexistent", "1.0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestSchemaRegistry_CreateState(t *testing.T) {
	registry := NewSchemaRegistry()
	schema := NewStateSchema("config", "1.0").
		AddField(&FieldSchema{
			Name:    "timeout",
			Type:    FieldTypeInt,
			Default: 30,
		})
	registry.Register(schema)

	state, err := registry.CreateState("config", "1.0")
	require.NoError(t, err)

	// Should have default value
	timeout, _ := state.GetInt("timeout")
	assert.Equal(t, int64(30), timeout)
}

func TestGlobalSchemaRegistry(t *testing.T) {
	registry1 := GlobalSchemaRegistry()
	registry2 := GlobalSchemaRegistry()

	// Should return same instance
	assert.Same(t, registry1, registry2)
}

// =============================================================================
// Type Validation Tests
// =============================================================================

func TestValidateType(t *testing.T) {
	testCases := []struct {
		name        string
		fieldType   FieldType
		value       interface{}
		shouldError bool
	}{
		{"string valid", FieldTypeString, "hello", false},
		{"string invalid", FieldTypeString, 123, true},
		{"int valid", FieldTypeInt, 42, false},
		{"int from float64", FieldTypeInt, float64(42), false},
		{"int invalid", FieldTypeInt, "not int", true},
		{"float valid", FieldTypeFloat, 3.14, false},
		{"float invalid", FieldTypeFloat, "not float", true},
		{"bool valid", FieldTypeBool, true, false},
		{"bool invalid", FieldTypeBool, "not bool", true},
		{"object valid", FieldTypeObject, map[string]interface{}{}, false},
		{"object invalid", FieldTypeObject, "not object", true},
		{"array valid", FieldTypeArray, []int{1, 2, 3}, false},
		{"array invalid", FieldTypeArray, "not array", true},
		{"any accepts string", FieldTypeAny, "anything", false},
		{"any accepts int", FieldTypeAny, 123, false},
		{"any accepts nil", FieldTypeAny, nil, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateType(tc.fieldType, tc.value)
			if tc.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestTypedState_Concurrency(t *testing.T) {
	ts := NewTypedState(nil)

	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(n int) {
			key := "key"
			ts.Set(key, n)
			ts.Get(key)
			ts.Keys()
			ts.ToMap()
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	// Should not panic or race
}

func TestSchemaRegistry_Concurrency(t *testing.T) {
	registry := NewSchemaRegistry()

	done := make(chan bool)
	for i := 0; i < 50; i++ {
		go func(n int) {
			schema := NewStateSchema("schema", "1.0")
			registry.Register(schema) // Only first will succeed
			registry.Get("schema", "1.0")
			done <- true
		}(i)
	}

	for i := 0; i < 50; i++ {
		<-done
	}
}
