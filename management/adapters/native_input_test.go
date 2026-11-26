package adapters

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/llm"
)

func TestInputValue(t *testing.T) {
	number := func(s string) any {
		var v any
		require.NoError(t, json.Unmarshal([]byte(s), &v, json.UseNumber))
		return v
	}
	tests := []struct {
		name    string
		raw     string
		want    any
		invalid bool
	}{
		{name: "string", raw: `"a"`, want: "a"},
		{
			name: "text with images",
			raw:  `{"text":"a","images":[{"ref":"image:1","mime":"image/png","sha256":"aa"}]}`,
			want: builder.UserInput{Text: "a", Images: []llm.ImagePart{{Ref: "image:1", MIME: "image/png", SHA256: "aa"}}},
		},
		{name: "ref only", raw: `{"images":[{"ref":"image:1"}]}`, want: builder.UserInput{Images: []llm.ImagePart{{Ref: "image:1"}}}},
		{name: "empty images", raw: `{"text":"a","images":[]}`, want: builder.UserInput{Text: "a", Images: []llm.ImagePart{}}},
		{name: "url in image", raw: `{"text":"a","images":[{"ref":"image:1","url":"https://x"}]}`, invalid: true},
		{name: "unknown top-level key", raw: `{"text":"a","images":[],"extra":1}`, invalid: true},
		{name: "missing ref", raw: `{"text":"a","images":[{"mime":"image/png"}]}`, invalid: true},
		{name: "empty ref", raw: `{"images":[{"ref":""}]}`, invalid: true},
		{name: "text not string", raw: `{"text":1,"images":[]}`, invalid: true},
		{name: "images null", raw: `{"text":"a","images":null}`, invalid: true},
		{name: "object without images", raw: `{"text":"a","n":1}`, want: number(`{"text":"a","n":1}`)},
		{name: "array", raw: `[1,"a"]`, want: number(`[1,"a"]`)},
		{name: "number", raw: `42`, want: number(`42`)},
		{name: "null", raw: `null`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inputValue(json.RawMessage(tt.raw))
			if tt.invalid {
				require.Error(t, err)
				require.True(t, agentErrors.IsCode(err, agentErrors.CodeInvalidInput), "%v", err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
