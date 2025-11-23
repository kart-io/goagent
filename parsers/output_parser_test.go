package parsers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONOutputParser_ExtractJSON(t *testing.T) {
	parser := NewJSONOutputParser[map[string]interface{}](false)
	ctx := context.Background()

	tests := []struct {
		name     string
		input    string
		wantJSON string
		wantErr  bool
	}{
		{
			name:     "Simple JSON object",
			input:    `{"name": "test", "value": 123}`,
			wantJSON: `{"name": "test", "value": 123}`,
		},
		{
			name:     "JSON in markdown code block",
			input:    "Here is the result:\n```json\n{\"status\": \"success\"}\n```\nDone",
			wantJSON: `{"status": "success"}`,
		},
		{
			name:     "JSON with leading text",
			input:    "The answer is: {\"result\": 42}",
			wantJSON: `{"result": 42}`,
		},
		{
			name:     "JSON with trailing text",
			input:    `{"data": "value"} and some more text`,
			wantJSON: `{"data": "value"}`,
		},
		{
			name:     "Nested JSON",
			input:    `{"user": {"name": "Alice", "age": 30}}`,
			wantJSON: `{"user": {"name": "Alice", "age": 30}}`,
		},
		{
			name:     "JSON array",
			input:    `[{"id": 1}, {"id": 2}]`,
			wantJSON: `[{"id": 1}, {"id": 2}]`,
			// Note: This will be extracted correctly but won't parse to map[string]interface{}
			// That's expected - arrays need []map[string]interface{} type
		},
		{
			name:     "Large text with small JSON",
			input:    "Lorem ipsum dolor sit amet. " + `{"key": "val"}` + " More text here.",
			wantJSON: `{"key": "val"}`,
		},
		{
			name:    "No JSON",
			input:   "This is just plain text without any JSON",
			wantErr: true,
		},
		{
			name:     "JSON with extra spaces",
			input:    "\n\n  {\"trimmed\": true}  \n\n",
			wantJSON: `{"trimmed": true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extracted := parser.extractJSON(tt.input)

			if tt.wantErr {
				assert.Empty(t, extracted, "Expected empty result")
			} else {
				assert.NotEmpty(t, extracted, "Expected non-empty result")

				// For arrays, we need to use the right type or just check extraction
				// Skip full parse test for array types when using map type parser
				if tt.name == "JSON array" {
					// Just verify extraction worked
					assert.Equal(t, tt.wantJSON, extracted)
					return
				}

				// Verify it's valid JSON by parsing
				result, err := parser.Parse(ctx, tt.input)
				if err != nil {
					t.Logf("Extracted: %s", extracted)
				}
				require.NoError(t, err, "Should parse successfully")
				assert.NotNil(t, result)
			}
		})
	}
}

func TestJSONOutputParser_Parse(t *testing.T) {
	type TestStruct struct {
		Name   string `json:"name"`
		Value  int    `json:"value"`
		Active bool   `json:"active"`
	}

	parser := NewJSONOutputParser[TestStruct](false)
	ctx := context.Background()

	tests := []struct {
		name    string
		input   string
		want    TestStruct
		wantErr bool
	}{
		{
			name:  "Valid JSON",
			input: `{"name": "test", "value": 42, "active": true}`,
			want:  TestStruct{Name: "test", Value: 42, Active: true},
		},
		{
			name:  "JSON in markdown",
			input: "```json\n{\"name\": \"md\", \"value\": 99, \"active\": false}\n```",
			want:  TestStruct{Name: "md", Value: 99, Active: false},
		},
		{
			name:  "JSON with text around",
			input: "Response: {\"name\": \"text\", \"value\": 123, \"active\": true} Done!",
			want:  TestStruct{Name: "text", Value: 123, Active: true},
		},
		{
			name:    "Invalid JSON",
			input:   `{"name": "bad", "value": }`,
			wantErr: true,
		},
		{
			name:    "No JSON",
			input:   "Plain text",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.Parse(ctx, tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			}
		})
	}
}

func TestJSONOutputParser_StrictMode(t *testing.T) {
	strictParser := NewJSONOutputParser[map[string]string](true)
	lenientParser := NewJSONOutputParser[map[string]string](false)
	ctx := context.Background()

	// Incomplete JSON
	incompleteJSON := `{"key": "value"`

	// Lenient mode should work (or at least try)
	_, err := lenientParser.Parse(ctx, incompleteJSON)
	// May or may not error depending on JSON parser, but shouldn't panic
	_ = err

	// Strict mode should fail gracefully
	_, err = strictParser.Parse(ctx, incompleteJSON)
	assert.Error(t, err, "Strict mode should reject incomplete JSON")
}

func TestJSONOutputParser_MemoryEfficiency(t *testing.T) {
	parser := NewJSONOutputParser[map[string]interface{}](false)

	// Create a large text with a small JSON
	largePrefix := make([]byte, 10000)
	for i := range largePrefix {
		largePrefix[i] = 'A'
	}

	smallJSON := `{"result": "small"}`
	largeSuffix := make([]byte, 10000)
	for i := range largeSuffix {
		largeSuffix[i] = 'B'
	}

	input := string(largePrefix) + smallJSON + string(largeSuffix)

	// Extract JSON
	extracted := parser.extractJSON(input)

	// Verify the extracted JSON is correct and doesn't reference the large string
	assert.Equal(t, smallJSON, extracted)

	// The extracted string should be a clone, not a substring
	// (This is important for GC - we want the large string to be collectable)
	// We can't directly test this, but we can verify the behavior is correct
	ctx := context.Background()
	result, err := parser.Parse(ctx, input)
	require.NoError(t, err)
	assert.NotNil(t, result)
}
