package yaml

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/codec/json"
)

type yamlOut struct {
	Name    string            `json:"name"`
	Short   string            `json:"short"`
	Retries int               `json:"retries"`
	Ratio   float64           `json:"ratio"`
	Timeout time.Duration     `json:"timeout"`
	Labels  map[string]string `json:"labels"`
	Tools   []string          `json:"tools"`
	Note    string            `json:"note,omitempty"`
}

// YAML 编码先按 json 标签与框架规则得到 JSON，再转为块风格 YAML，字段保持结构体顺序。
func TestYAMLMarshalUsesJSONTagsAndRoundTrips(t *testing.T) {
	in := yamlOut{Name: "客服 <a&b>", Short: "y", Retries: 3, Ratio: 0.5, Timeout: 30 * time.Second, Labels: map[string]string{"z": "1", "a": "2"}, Tools: []string{"get_order"}}
	data, err := Marshal(in)
	require.NoError(t, err)
	text := string(data)
	require.Equal(t, []string{"name", "short", "retries", "ratio", "timeout", "labels", "tools"}, topLevelKeys(text))
	require.NotContains(t, text, "{", "输出块风格而非 JSON 流风格")
	require.NotContains(t, text, "note", "omitempty 与 JSON 一致")
	require.Contains(t, text, "timeout: 30000000000", "时长与 JSON 一致按整数纳秒")
	require.Contains(t, text, `short: "y"`, "YAML 1.1 读取方会把 y 当布尔值，须加引号")
	require.Less(t, strings.Index(text, "a: \"2\""), strings.Index(text, "z: \"1\""), "map 键排序")

	var out yamlOut
	require.NoError(t, Unmarshal(data, &out))
	require.Equal(t, in, out, "y 等字符串须加引号，往返后仍为字符串")
}

func topLevelKeys(text string) []string {
	var keys []string
	for _, line := range strings.Split(text, "\n") {
		if line != "" && line[0] != ' ' && line[0] != '-' {
			keys = append(keys, strings.SplitN(line, ":", 2)[0])
		}
	}
	return keys
}

// 编码结果对 YAML 1.1 读取方也无歧义，且能被本包读回。
func TestMarshalQuotesAmbiguousStrings(t *testing.T) {
	in := map[string]string{"a": "no", "b": "On", "c": "YES", "d": "<<", "e": "tab\there\nnext", "f": "plain", "<<": "merge-like key"}
	data, err := Marshal(in)
	require.NoError(t, err)
	text := string(data)
	for _, want := range []string{`a: "no"`, `b: "On"`, `c: "YES"`, `d: "<<"`, `"<<": merge-like key`, `e: "tab\there\nnext"`, "f: plain"} {
		require.Contains(t, text, want)
	}
	var out map[string]string
	require.NoError(t, Unmarshal(data, &out))
	require.Equal(t, in, out)
}

// 超出 int64 的整数与浮点数照常往返。
func TestMarshalLargeNumbers(t *testing.T) {
	in := map[string]any{"big": 1e20, "huge": 1e300, "neg": -12.5}
	data, err := Marshal(in)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, Unmarshal(data, &out))
	require.Equal(t, in, out)
}

// JSON 允许而 YAML 输入不允许的控制字符也能编码并读回。
func TestMarshalControlCharacters(t *testing.T) {
	in := map[string]string{"del": "\x7f", "c1": "\u0085\u009f", "bom": "\ufeffx"}
	data, err := Marshal(in)
	require.NoError(t, err)
	var out map[string]string
	require.NoError(t, Unmarshal(data, &out))
	require.Equal(t, in, out)
}

// float64 无法精确表示的数字按原文传给 JSON 阶段；可精确表示的仍按原规则，1.0、1e3 照常解到整数字段。
func TestUnmarshalPreservesNumberPrecision(t *testing.T) {
	src := []byte("rate: 0.1234567890123456789\nbig: 123456789012345678901234567890\nanchored: &n 0.10000000000000000001\nalias: *n\nshort: .30000000000000000001\nplus: +1.00000000000000000001e+5\nlist: [12345678901234567890123]\nbase: &b {x: 1.00000000000000000001}\nmerged: {<<: *b}\n")
	var out map[string]any
	require.NoError(t, Unmarshal(src, &out, json.UseNumber))
	require.Equal(t, map[string]any{
		"rate":     json.Number("0.1234567890123456789"),
		"big":      json.Number("123456789012345678901234567890"),
		"anchored": json.Number("0.10000000000000000001"),
		"alias":    json.Number("0.10000000000000000001"),
		"short":    json.Number("0.30000000000000000001"),
		"plus":     json.Number("1.00000000000000000001e+5"),
		"list":     []any{json.Number("12345678901234567890123")},
		"base":     map[string]any{"x": json.Number("1.00000000000000000001")},
		"merged":   map[string]any{"x": json.Number("1.00000000000000000001")},
	}, out)

	var ints struct {
		A int     `json:"a"`
		B int     `json:"b"`
		C float64 `json:"c"`
	}
	require.NoError(t, Unmarshal([]byte("a: 1.0\nb: 1e3\nc: 0.1\n"), &ints))
	require.Equal(t, 1, ints.A)
	require.Equal(t, 1000, ints.B)
	require.Equal(t, 0.1, ints.C)
}
