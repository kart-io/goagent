package parsers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBooleanOutputParser_MatchesWholeWords(t *testing.T) {
	p := NewBooleanOutputParser()
	ctx := context.Background()

	for text, want := range map[string]bool{
		"incorrect":          false,
		"not correct":        false,
		"it is not true":     false,
		"No, that is wrong":  false,
		"Yes, that is right": true,
		"correct":            true,
		"不是":                 false,
		"不对":                 false,
		"是的":                 true,
		"没错":                 true,
		"错误":                 false,
	} {
		got, err := p.Parse(ctx, text)
		require.NoError(t, err, text)
		assert.Equal(t, want, got, text)
	}

	for _, text := range []string{"sorry, unknown", "nothing", "10", "maybe"} {
		_, err := p.Parse(ctx, text)
		assert.ErrorIs(t, err, ErrParseFailed, "%q 中只有子串命中，不应判定", text)
	}
}

func TestJSONOutputParser_BracketsInsideStrings(t *testing.T) {
	ctx := context.Background()
	p := NewJSONOutputParser[map[string]any](true)

	for text, want := range map[string]string{
		`{"a":"}"}`:         "}",
		`{"a":"{"}`:         "{",
		`前缀 {"a":"\"}"} 后缀`: `"}`,
	} {
		got, err := p.Parse(ctx, text)
		require.NoError(t, err, text)
		assert.Equal(t, want, got["a"], text)
	}

	arr, err := NewJSONOutputParser[[]string](true).Parse(ctx, `["]", "x"]`)
	require.NoError(t, err)
	assert.Equal(t, []string{"]", "x"}, arr)
}

func TestListOutputParser_KeepsLeadingContent(t *testing.T) {
	ctx := context.Background()

	got, err := NewListOutputParser("").Parse(ctx, "3D printing\n1.5 apples\n-1 degrees\n2) 第二项\n3、第三项\n4.第四项\n- 第五项")
	require.NoError(t, err)
	assert.Equal(t, []string{"3D printing", "1.5 apples", "-1 degrees", "第二项", "第三项", "第四项", "第五项"}, got)

	got, err = NewListOutputParser(",").Parse(ctx, "1, 2, 3")
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2", "3"}, got)
}

func TestJSONOutputParser_FormatInstructionsForInterfaceType(t *testing.T) {
	assert.NotPanics(t, func() {
		assert.Contains(t, NewJSONOutputParser[any](false).GetFormatInstructions(), "JSON")
	})
}

func TestStructuredOutputParser_ConvertsBySchemaType(t *testing.T) {
	type output struct {
		Name   string  `json:"name"`
		Age    int     `json:"age"`
		Score  float64 `json:"score"`
		Active bool    `json:"active"`
	}
	schema := map[string]FieldSchema{
		"name":   {Name: "name", Type: "string"},
		"age":    {Name: "age", Type: "int"},
		"score":  {Name: "score", Type: "number"},
		"active": {Name: "active", Type: "boolean"},
	}
	p := NewStructuredOutputParser[output](schema)
	ctx := context.Background()

	got, err := p.Parse(ctx, "name: 42\nage: 30\nscore: 1.5\nactive: true")
	require.NoError(t, err)
	assert.Equal(t, output{Name: "42", Age: 30, Score: 1.5, Active: true}, got)

	_, err = p.Parse(ctx, "age: 三十")
	assert.ErrorIs(t, err, ErrTypeConversion)
}
