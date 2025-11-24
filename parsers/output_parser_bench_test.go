package parsers

import (
	"context"
	"strings"
	"testing"
)

// BenchmarkExtractJSON_Original benchmarks the original extractJSON implementation
func BenchmarkExtractJSON_Original(b *testing.B) {
	parser := NewJSONOutputParser[map[string]interface{}](false)

	testCases := []struct {
		name string
		text string
	}{
		{
			name: "SmallJSON",
			text: `{"name": "test", "value": 123}`,
		},
		{
			name: "MarkdownCodeBlock",
			text: "Here is the JSON:\n```json\n" + `{"name": "test", "value": 123}` + "\n```\nEnd of output",
		},
		{
			name: "LargeTextWithSmallJSON",
			text: strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 100) +
				`{"result": "success"}` +
				strings.Repeat(" More text after JSON. ", 100),
		},
		{
			name: "NestedJSON",
			text: `{"user": {"name": "Alice", "details": {"age": 30, "address": {"city": "NYC"}}}}`,
		},
		{
			name: "JSONArray",
			text: `[{"id": 1}, {"id": 2}, {"id": 3}]`,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result := parser.extractJSON(tc.text)
				if result == "" {
					b.Fatal("extractJSON returned empty result")
				}
			}
		})
	}
}

// BenchmarkExtractJSON_Optimized benchmarks the optimized extractJSON implementation
func BenchmarkExtractJSON_Optimized(b *testing.B) {
	parser := NewJSONOutputParser[map[string]interface{}](false)

	testCases := []struct {
		name string
		text string
	}{
		{
			name: "SmallJSON",
			text: `{"name": "test", "value": 123}`,
		},
		{
			name: "MarkdownCodeBlock",
			text: "Here is the JSON:\n```json\n" + `{"name": "test", "value": 123}` + "\n```\nEnd of output",
		},
		{
			name: "LargeTextWithSmallJSON",
			text: strings.Repeat("Lorem ipsum dolor sit amet, consectetur adipiscing elit. ", 100) +
				`{"result": "success"}` +
				strings.Repeat(" More text after JSON. ", 100),
		},
		{
			name: "NestedJSON",
			text: `{"user": {"name": "Alice", "details": {"age": 30, "address": {"city": "NYC"}}}}`,
		},
		{
			name: "JSONArray",
			text: `[{"id": 1}, {"id": 2}, {"id": 3}]`,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result := parser.extractJSONOptimized(tc.text)
				if result == "" {
					b.Fatal("extractJSONOptimized returned empty result")
				}
			}
		})
	}
}

// BenchmarkJSONParser_Parse tests the full Parse path
func BenchmarkJSONParser_Parse(b *testing.B) {
	type TestStruct struct {
		Name   string `json:"name"`
		Value  int    `json:"value"`
		Active bool   `json:"active"`
	}

	parser := NewJSONOutputParser[TestStruct](false)
	ctx := context.Background()

	testCases := []struct {
		name string
		text string
	}{
		{
			name: "DirectJSON",
			text: `{"name": "test", "value": 123, "active": true}`,
		},
		{
			name: "MarkdownJSON",
			text: "Response:\n```json\n" + `{"name": "test", "value": 123, "active": true}` + "\n```",
		},
		{
			name: "LargeOutputWithJSON",
			text: strings.Repeat("Thinking... ", 200) + "\n\n" +
				`{"name": "result", "value": 999, "active": false}` + "\n\n" +
				strings.Repeat("Done. ", 200),
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := parser.Parse(ctx, tc.text)
				if err != nil {
					b.Fatalf("Parse failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkStringOperations tests different string trimming approaches
func BenchmarkStringOperations(b *testing.B) {
	largeText := strings.Repeat("Lorem ipsum dolor sit amet. ", 500)
	smallSubstr := largeText[1000:1100] // Small 100-byte substring from 15KB text

	b.Run("DirectSubstring", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = smallSubstr
		}
	})

	b.Run("StringsClone", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = strings.Clone(smallSubstr)
		}
	})

	b.Run("ByteConversion", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = string([]byte(smallSubstr))
		}
	})
}
