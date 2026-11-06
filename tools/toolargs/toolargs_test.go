package toolargs

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
)

// 只有 float64 丢精度的数字保留为 json.Number，其余与默认解码相同。
func TestParseKeepsOnlyLossyNumbers(t *testing.T) {
	args, ok := Parse(`{"id":1234567890123456789,"rate":0.1234567890123456789,"n":1,"f":0.1,"big":1e300,"nested":{"id":9007199254740993,"k":2},"list":[9007199254740993,2.5],"note":"x"}`)
	require.True(t, ok)
	require.Equal(t, map[string]any{
		"id": json.Number("1234567890123456789"), "rate": json.Number("0.1234567890123456789"), "n": 1.0, "f": 0.1, "big": 1e300,
		"nested": map[string]any{"id": json.Number("9007199254740993"), "k": 2.0},
		"list":   []any{json.Number("9007199254740993"), 2.5}, "note": "x",
	}, args)

	var plain map[string]any
	raw := `{"n":1,"f":0.1,"big":1e300,"list":[1,2.5],"m":{"a":-3}}`
	require.NoError(t, json.Unmarshal([]byte(raw), &plain))
	args, ok = Parse(raw)
	require.True(t, ok)
	require.Equal(t, plain, args, "原本正确的数字编码不变，durable 意图与缓存键不受影响")
}

// 9007199254740992 与 9007199254740993 解成 float64 相同，Parse 的结果须可区分。
func TestParseDistinguishesCollidingIntegers(t *testing.T) {
	a, _ := Parse(`{"id":9007199254740992}`)
	b, _ := Parse(`{"id":9007199254740993}`)
	require.NotEqual(t, a, b)
}

func TestParseRejectsNonObjects(t *testing.T) {
	for _, raw := range []string{"null", "[]", "1", `{"a":`, "", `{"x":1e400}`, `{"o":{"x":-1e400}}`, `{"l":[1e400]}`} {
		_, ok := Parse(raw)
		require.False(t, ok, raw)
	}
}

// floatView 不修改原参数；不含 json.Number 时原样返回。
func TestFloatViewConvertsNumbers(t *testing.T) {
	args, _ := Parse(`{"id":9007199254740993,"list":[9007199254740993],"n":1}`)
	view, err := floatView(args)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"id": 9007199254740992.0, "list": []any{9007199254740992.0}, "n": 1.0}, view)
	require.Equal(t, json.Number("9007199254740993"), args["id"])
	plain := map[string]any{"n": 1.0}
	view, err = floatView(plain)
	require.NoError(t, err)
	require.Equal(t, plain, view)
}

// 无法转为 float64 的 json.Number 底层是字符串，不得被当作字符串通过校验。
func TestValidateRejectsOverflowingNumbers(t *testing.T) {
	r := resolve(t, `{"type":"object","properties":{"x":{"type":"string"}}}`)
	for _, args := range []map[string]any{
		{"x": json.Number("1e400")},
		{"x": []any{json.Number("1e400")}},
		{"o": map[string]any{"x": json.Number("-1e400")}},
	} {
		require.Error(t, Validate(r, args))
	}
}

func resolve(t *testing.T, schema string) *jsonschema.Resolved {
	t.Helper()
	var s jsonschema.Schema
	require.NoError(t, json.Unmarshal([]byte(schema), &s))
	r, err := s.Resolve(nil)
	require.NoError(t, err)
	return r
}

// 精确数字按原文检查数值约束，舍入后才满足约束的值须被拒绝。
func TestValidateChecksExactNumbers(t *testing.T) {
	cases := []struct {
		schema, args string
		ok           bool
	}{
		{`{"type":"object","properties":{"id":{"type":"integer"}}}`, `{"id":9007199254740993}`, true},
		{`{"type":"object","properties":{"id":{"type":"integer"}}}`, `{"id":1.0000000000000001}`, false},
		{`{"type":"object","properties":{"v":{"type":"number","maximum":1}}}`, `{"v":1.0000000000000001}`, false},
		{`{"type":"object","properties":{"v":{"type":"number","minimum":1}}}`, `{"v":0.99999999999999999}`, false},
		{`{"type":"object","properties":{"v":{"type":"number","exclusiveMaximum":9007199254740993}}}`, `{"v":9007199254740993}`, false},
		{`{"type":"object","properties":{"v":{"type":"number","maximum":0.1}}}`, `{"v":0.1000000000000000001}`, false},
		{`{"type":"object","properties":{"v":{"type":"number","multipleOf":2}}}`, `{"v":9007199254740993}`, false},
		{`{"type":"object","properties":{"v":{"type":"number","multipleOf":2}}}`, `{"v":9007199254740994}`, true},
		{`{"type":"object","properties":{"v":{"type":["integer","number"]}}}`, `{"v":1.0000000000000001}`, true},
		{`{"type":"object","properties":{"list":{"type":"array","items":{"type":"integer","maximum":9007199254740992}}}}`, `{"list":[1,9007199254740993]}`, false},
		{`{"type":"object","additionalProperties":{"type":"integer"}}`, `{"x":1.0000000000000001}`, false},
		{`{"type":"object","properties":{"o":{"$ref":"#/$defs/O"}},"$defs":{"O":{"type":"object","properties":{"id":{"type":"integer","minimum":9007199254740993}}}}}`, `{"o":{"id":9007199254740993}}`, true},
		{`{"type":"object","properties":{"o":{"$ref":"#/$defs/O"}},"$defs":{"O":{"type":"object","properties":{"id":{"type":"integer"}}}}}`, `{"o":{"id":1.0000000000000001}}`, false},
		{`{"type":"object","properties":{"v":{"anyOf":[{"type":"integer"},{"type":"string"}]}}}`, `{"v":9007199254740993}`, false},
		{`{"type":"object","properties":{"v":{"anyOf":[{"type":"integer"},{"type":"string"}]}}}`, `{"v":1}`, true},
		{`{"type":"object","properties":{"v":{"type":"integer","maximum":1}}}`, `{"v":2}`, false},
		{`{"type":"object","properties":{"v":{"type":"number"}},"patternProperties":{"^v$":{"maximum":1}}}`, `{"v":1.0000000000000001}`, false},
		{`{"type":"object","properties":{"v":{"type":"number"}},"patternProperties":{"^v$":{"maximum":1}}}`, `{"v":0.5}`, true},
		{`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"values":{"type":"array","items":[{"type":"integer"}]}}}`, `{"values":[1.0000000000000001]}`, false},
		{`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"values":{"type":"array","items":[{"type":"integer"}]}}}`, `{"values":[1]}`, true},
		{`{"type":"object","properties":{"values":{"type":"array","prefixItems":[{"type":"integer"}],"items":{"type":"number","maximum":1}}}}`, `{"values":[9007199254740993,1.0000000000000001]}`, false},
		{`{"type":"object","properties":{"values":{"type":"array","prefixItems":[{"type":"integer"}],"items":{"type":"number"}}}}`, `{"values":[9007199254740993,1.0000000000000001]}`, true},
		{`{"type":"object","properties":{"v":{"type":"number","title":"t","description":"d","default":1}},"required":["v"]}`, `{"v":1.0000000000000001}`, true},
		{`{"type":"object","properties":{"v":{"$ref":"#/$defs/V","description":"d"}},"$defs":{"V":{"type":"integer"}}}`, `{"v":1.0000000000000001}`, false},
		{`{"type":"object","properties":{"v":{"$ref":"#/$defs/V","maximum":5}},"$defs":{"V":{"type":"number"}}}`, `{"v":1.0000000000000001}`, false},
		{`{"type":"object","properties":{"v":{"type":"integer","enum":[1,2]}}}`, `{"v":1.0000000000000001}`, false},
		// 已知的保守误拒：舍入值不满足约束而原值满足时，结构校验先拒绝。
		{`{"type":"object","properties":{"x":{"type":"integer","multipleOf":3}}}`, `{"x":9007199254740993}`, false},
		{`{"type":"object","properties":{"x":{"type":"number","exclusiveMinimum":1}}}`, `{"x":1.0000000000000001}`, false},
	}
	for _, c := range cases {
		args, ok := Parse(c.args)
		require.True(t, ok, c.args)
		err := Validate(resolve(t, c.schema), args)
		if c.ok {
			require.NoError(t, err, "%s %s", c.schema, c.args)
		} else {
			require.Error(t, err, "%s %s", c.schema, c.args)
		}
	}
}

func TestFloat64AcceptsJSONNumbers(t *testing.T) {
	f, ok := Float64(json.Number("3.141592653589793238"))
	require.True(t, ok)
	require.Equal(t, 3.141592653589793, f)
	f, ok = Float64(2.5)
	require.True(t, ok)
	require.Equal(t, 2.5, f)
	_, ok = Float64("1")
	require.False(t, ok)
}
