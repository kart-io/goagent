package json

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeJSONWritesOneValuePerLine(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, map[string]any{"b": "<", "a": 1}))
	require.NoError(t, Encode(&buf, []int(nil)))
	require.Equal(t, "{\"a\":1,\"b\":\"\\u003c\"}\nnull\n", buf.String(), "与 v1 Encoder.Encode 一致：键排序、转义、每值一行")

	buf.Reset()
	require.NoError(t, Encode(&buf, map[string]int{"a": 1}, Indent("  ")))
	require.Equal(t, "{\n  \"a\": 1\n}\n", buf.String())
}

func TestDecodeJSONReadsWholeInput(t *testing.T) {
	var v map[string]int
	require.NoError(t, Decode(strings.NewReader(`{"a":1}`), &v))
	require.Equal(t, map[string]int{"a": 1}, v)
	require.Error(t, Decode(strings.NewReader(`{"a":1} {"a":2}`), &v), "拒绝尾随数据")
}

func TestUseNumberKeepsNumberText(t *testing.T) {
	var v map[string]any
	require.NoError(t, Unmarshal([]byte(`{"big":9007199254740993,"s":"x"}`), &v, UseNumber))
	require.Equal(t, Number("9007199254740993"), v["big"])
	require.Equal(t, "x", v["s"])
	data, err := Marshal(v)
	require.NoError(t, err)
	require.Equal(t, `{"big":9007199254740993,"s":"x"}`, string(data), "原文写回")
}

func TestValidJSON(t *testing.T) {
	require.True(t, Valid([]byte(`{"a":[1,2]}`)))
	require.False(t, Valid([]byte(`{"a":`)))
	require.False(t, Valid(nil))
}

func TestRawMessageIsJSONValue(t *testing.T) {
	type wrapper struct {
		Data RawMessage `json:"data"`
	}
	var w wrapper
	require.NoError(t, Unmarshal([]byte(`{"data": {"b": 1, "a": [1, 2]}}`), &w))
	require.Equal(t, `{"b": 1, "a": [1, 2]}`, string(w.Data), "原文保留，不重新编码")
	data, err := Marshal(w)
	require.NoError(t, err)
	require.Equal(t, `{"data":{"b":1,"a":[1,2]}}`, string(data))
}

func TestNoEscapeHTML(t *testing.T) {
	data, err := Marshal("a<b && c>d", NoEscapeHTML)
	require.NoError(t, err)
	require.Equal(t, `"a<b && c>d"`, string(data))
}

func TestIndentPrefix(t *testing.T) {
	data, err := Marshal(map[string]int{"a": 1}, IndentPrefix("  "), Indent("  "))
	require.NoError(t, err)
	require.Equal(t, "{\n    \"a\": 1\n  }", string(data))
}

func TestSemanticErrorReportsTypeMismatch(t *testing.T) {
	var v struct {
		N int `json:"n"`
	}
	err := Unmarshal([]byte(`{"n":"x"}`), &v)
	var typeErr *UnmarshalTypeError
	require.ErrorAs(t, err, &typeErr, "默认规则返回 v1 错误类型")
	require.Equal(t, "n", typeErr.Field)

	err = Unmarshal([]byte(`{"n":"x"}`), &v, Strict)
	var semantic *SemanticError
	require.ErrorAs(t, err, &semantic, "Strict 返回带 JSON 路径的 SemanticError")
	require.Equal(t, "/n", string(semantic.JSONPointer))
}

type fromDecoder struct{ N int }

func (f *fromDecoder) UnmarshalJSONFrom(dec *Decoder) error {
	var raw struct {
		N int `json:"n"`
	}
	if err := DecodeNext(dec, &raw); err != nil {
		return err
	}
	f.N = raw.N * 10
	return nil
}

func TestDecoderAliasSatisfiesUnmarshalerFrom(t *testing.T) {
	var f fromDecoder
	require.NoError(t, Unmarshal([]byte(`{"n":2}`), &f))
	require.Equal(t, 20, f.N, "签名使用 codec.Decoder 的 UnmarshalJSONFrom 会被调用")
}

func TestCompactJSON(t *testing.T) {
	in := RawMessage("{ \"b\" : 1,\n \"a\": [1, 2] }")
	out, err := Compact(in)
	require.NoError(t, err)
	require.Equal(t, `{"b":1,"a":[1,2]}`, string(out), "只去空白，不改键序")
	require.Equal(t, "{ \"b\" : 1,\n \"a\": [1, 2] }", string(in), "不修改输入")

	out, err = Compact([]byte("{\"a\":1, \"a\":\"\xff\"}"))
	require.NoError(t, err, "与 v1 json.Compact 一样容忍重复键与非法 UTF-8")
	require.Equal(t, "{\"a\":1,\"a\":\"\xff\"}", string(out), "与 v1 一样原样保留非法字节")

	out, err = Compact([]byte("{\"s\": \"<a&b>\u2028\\u003c\"}"))
	require.NoError(t, err)
	require.Equal(t, "{\"s\":\"<a&b>\u2028\\u003c\"}", string(out), "与 v1 一样不改写字符串：不新增转义，也不还原已有转义")

	_, err = Compact([]byte(`{"a":`))
	require.Error(t, err)
}

func TestPrettyIndentsWithTwoSpaces(t *testing.T) {
	data, err := Marshal(map[string][]int{"a": {1}}, Pretty)
	require.NoError(t, err)
	require.Equal(t, "{\n  \"a\": [\n    1\n  ]\n}", string(data))
}
