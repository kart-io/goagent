package json_test

import (
	"bytes"
	"testing"

	"github.com/kart-io/goagent/utils/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:  "simple map",
			input: map[string]interface{}{"name": "test", "age": 25},
			want:  `{"age":25,"name":"test"}`,
		},
		{
			name:  "nested structure",
			input: map[string]interface{}{"user": map[string]interface{}{"name": "test"}},
			want:  `{"user":{"name":"test"}}`,
		},
		{
			name:  "array",
			input: []int{1, 2, 3},
			want:  `[1,2,3]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    interface{}
		wantErr bool
	}{
		{
			name:  "simple map",
			input: `{"name":"test","age":25}`,
			want:  map[string]interface{}{"name": "test", "age": float64(25)},
		},
		{
			name:  "array",
			input: `[1,2,3]`,
			want:  []interface{}{float64(1), float64(2), float64(3)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got interface{}
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMarshalToString(t *testing.T) {
	input := map[string]string{"key": "value"}
	got, err := json.MarshalToString(input)
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"value"}`, got)
}

func TestUnmarshalFromString(t *testing.T) {
	input := `{"key":"value"}`
	var got map[string]string
	err := json.UnmarshalFromString(input, &got)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"key": "value"}, got)
}

func TestMarshalIndent(t *testing.T) {
	input := map[string]interface{}{"name": "test"}
	got, err := json.MarshalIndent(input, "", "  ")
	require.NoError(t, err)
	want := `{
  "name": "test"
}`
	assert.Equal(t, want, string(got))
}

func TestEncoderDecoder(t *testing.T) {
	input := map[string]string{"key": "value"}

	// Test Encoder
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	err := enc.Encode(input)
	require.NoError(t, err)

	// Test Decoder
	var got map[string]string
	dec := json.NewDecoder(&buf)
	err = dec.Decode(&got)
	require.NoError(t, err)
	assert.Equal(t, input, got)
}

func TestValid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "valid JSON",
			input: `{"name":"test"}`,
			want:  true,
		},
		{
			name:  "invalid JSON",
			input: `{"name":}`,
			want:  false,
		},
		{
			name:  "empty",
			input: ``,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := json.Valid([]byte(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGet(t *testing.T) {
	data := []byte(`{"user":{"name":"test","age":25},"items":[1,2,3]}`)

	t.Run("get nested string", func(t *testing.T) {
		got, err := json.GetString(data, "user", "name")
		require.NoError(t, err)
		assert.Equal(t, "test", got)
	})

	t.Run("get nested int", func(t *testing.T) {
		got, err := json.GetInt64(data, "user", "age")
		require.NoError(t, err)
		assert.Equal(t, int64(25), got)
	})

	t.Run("get array element", func(t *testing.T) {
		got, err := json.Get(data, "items", 0)
		require.NoError(t, err)
		assert.Equal(t, float64(1), got)
	})
}

func TestMarshalWithOptions(t *testing.T) {
	input := map[string]interface{}{
		"name":   "test",
		"script": "<script>alert('xss')</script>",
	}

	t.Run("with HTML escape", func(t *testing.T) {
		got, err := json.MarshalWithOptions(input, json.MarshalOptions{
			EscapeHTML: true,
		})
		require.NoError(t, err)
		assert.Contains(t, string(got), "\\u003c")
	})

	t.Run("without HTML escape", func(t *testing.T) {
		got, err := json.MarshalWithOptions(input, json.MarshalOptions{
			EscapeHTML: false,
		})
		require.NoError(t, err)
		assert.NotContains(t, string(got), "\\u003c")
		assert.Contains(t, string(got), "<script>")
	})
}

func TestUnmarshalWithOptions(t *testing.T) {
	type TestStruct struct {
		Name string `json:"name"`
	}

	t.Run("allow unknown fields", func(t *testing.T) {
		input := `{"name":"test","extra":"field"}`
		var result TestStruct
		err := json.UnmarshalWithOptions([]byte(input), &result, json.UnmarshalOptions{
			DisallowUnknownFields: false,
		})
		require.NoError(t, err)
		assert.Equal(t, "test", result.Name)
	})

	t.Run("disallow unknown fields", func(t *testing.T) {
		input := `{"name":"test","extra":"field"}`
		var result TestStruct
		err := json.UnmarshalWithOptions([]byte(input), &result, json.UnmarshalOptions{
			DisallowUnknownFields: true,
		})
		assert.Error(t, err)
	})
}

func TestCompact(t *testing.T) {
	input := []byte(`{
  "name": "test",
  "age": 25
}`)
	var dst []byte
	err := json.Compact(&dst, input)
	require.NoError(t, err)
	assert.Equal(t, `{"name":"test","age":25}`, string(dst))
}

func TestIndent(t *testing.T) {
	input := []byte(`{"name":"test","age":25}`)
	var dst []byte
	err := json.Indent(&dst, input, "", "  ")
	require.NoError(t, err)
	want := `{
  "name": "test",
  "age": 25
}`
	assert.Equal(t, want, string(dst))
}

// Benchmark tests
func BenchmarkMarshal(b *testing.B) {
	input := map[string]interface{}{
		"name": "test",
		"age":  25,
		"tags": []string{"go", "json", "performance"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal(b *testing.B) {
	data := []byte(`{"name":"test","age":25,"tags":["go","json","performance"]}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result map[string]interface{}
		err := json.Unmarshal(data, &result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalStruct(b *testing.B) {
	type User struct {
		Name string   `json:"name"`
		Age  int      `json:"age"`
		Tags []string `json:"tags"`
	}

	input := User{
		Name: "test",
		Age:  25,
		Tags: []string{"go", "json", "performance"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalStruct(b *testing.B) {
	type User struct {
		Name string   `json:"name"`
		Age  int      `json:"age"`
		Tags []string `json:"tags"`
	}

	data := []byte(`{"name":"test","age":25,"tags":["go","json","performance"]}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result User
		err := json.Unmarshal(data, &result)
		if err != nil {
			b.Fatal(err)
		}
	}
}
