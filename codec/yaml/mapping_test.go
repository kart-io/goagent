package yaml

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
)

func TestDecodeMappingPreservesTypesAndSmallAliases(t *testing.T) {
	data := []byte(`name: sample
description: |
  First line.
  Second line.
enabled: true
disabled: false
missing: null
implicit-null:
text: "null"
number: 123456789012345678901234567890
precise: 0.10000000000000000001
date: 2026-11-09
base: &base {flag: true, value: null, list: [one, 2]}
copy: *base
key: &key quoted-key
*key: alias key value
`)
	out, err := DecodeMapping(data, 8192)
	require.NoError(t, err)
	base := map[string]any{"flag": true, "value": nil, "list": []any{"one", json.Number("2")}}
	require.Equal(t, map[string]any{
		"name": "sample", "description": "First line.\nSecond line.\n",
		"enabled": true, "disabled": false, "missing": nil, "implicit-null": nil, "text": "null",
		"number": json.Number("123456789012345678901234567890"), "precise": json.Number("0.10000000000000000001"),
		"date": "2026-11-09", "base": base, "copy": base,
		"key": "quoted-key", "quoted-key": "alias key value",
	}, out)
}

func TestDecodeMappingRejectsNonStringKeysRecursively(t *testing.T) {
	for _, key := range []string{"1", "1.5", "null", "~", "true", "false", "2026-11-09", "!!int '12'", "[a, b]", "{a: b}", "<<"} {
		for _, data := range []string{
			key + ": value\n",
			"nested:\n  " + key + ": value\n",
			"list:\n  - " + key + ": value\n",
		} {
			t.Run(data, func(t *testing.T) {
				out, err := DecodeMapping([]byte(data), 4096)
				require.ErrorContains(t, err, "keys must have type !!str")
				require.Nil(t, out)
			})
		}
	}
	for _, data := range []string{"?\n: value\n", "key: &key 42\n*key: value\n", "key: &key null\n*key: value\n"} {
		_, err := DecodeMapping([]byte(data), 4096)
		require.ErrorContains(t, err, "keys must have type !!str")
	}
	out, err := DecodeMapping([]byte("'null': null\n'1': true\n!!str false: false\n'<<': literal\n"), 1024)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"null": nil, "1": true, "false": false, "<<": "literal"}, out)
}

func TestDecodeMappingRejectsCyclesBeforeMaterialization(t *testing.T) {
	for _, data := range []string{
		"value: &loop [*loop]\n",
		"value: &loop {again: *loop}\n",
		"value: &outer {inner: &inner [*outer]}\n",
	} {
		t.Run(data, func(t *testing.T) {
			out, err := DecodeMapping([]byte(data), 4096)
			require.ErrorContains(t, err, "cyclic YAML alias")
			require.Nil(t, out)
		})
	}
}

func TestDecodeMappingBoundsAliasExpansionBeforeMaterialization(t *testing.T) {
	// Tiny input expands exponentially. Assert our budget error, not the YAML
	// decoder's alias-ratio error, so a guard moved after Node.Decode fails.
	var bomb strings.Builder
	bomb.WriteString("a0: &a0 [payload]\n")
	for i := 1; i < 24; i++ {
		fmt.Fprintf(&bomb, "a%d: &a%d [*a%d, *a%d, *a%d, *a%d]\n", i, i, i-1, i-1, i-1, i-1)
	}
	for name, data := range map[string]string{
		"nested aliases":     bomb.String(),
		"repeated scalar":    "value: &value " + strings.Repeat("x", 128) + "\ncopies: [" + strings.Repeat("*value, ", 100) + "]\n",
		"repeated container": "value: &value [a, b, c, d]\ncopies: [" + strings.Repeat("*value, ", 100) + "]\n",
	} {
		t.Run(name, func(t *testing.T) {
			require.Less(t, len(data), 4096, "input fits; expansion must be rejected")
			out, err := DecodeMapping([]byte(data), 4096)
			require.ErrorContains(t, err, "expanded YAML mapping exceeds byte budget")
			require.Nil(t, out)
		})
	}
}

func TestDecodeMappingRequiresSingleMappingDocument(t *testing.T) {
	for _, data := range []string{"", "# comment\n", "---\n", "null", "value", "[]", "- one\n- two", "a: b\n---\nc: d\n", "a: b\n---\n", "a: [unterminated"} {
		t.Run(data, func(t *testing.T) {
			out, err := DecodeMapping([]byte(data), 4096)
			require.Error(t, err)
			require.Nil(t, out)
		})
	}
	out, err := DecodeMapping([]byte("---\n{}\n...\n"), 64)
	require.NoError(t, err)
	require.Equal(t, map[string]any{}, out)
	_, err = DecodeMapping([]byte("a: one\na: two\n"), 4096)
	require.EqualError(t, err, "duplicate YAML mapping key")
}

func TestDecodeMappingRejectsBinaryScalars(t *testing.T) {
	// 二进制标签并非文本类型，即使字节恰好为 UTF-8 也不应隐式变成字符串。
	for _, data := range []string{
		"description: !!binary dXNlZnVs\n",
		"description: !!binary /w==\n",
		"metadata: {value: !!binary /w==}\n",
		"values: [!!binary /w==]\n",
		"first: &binary !!binary /w==\nsecond: *binary\n",
	} {
		t.Run(data, func(t *testing.T) {
			out, err := DecodeMapping([]byte(data), 4096)
			require.EqualError(t, err, "YAML 映射不支持二进制标量")
			require.Nil(t, out)
		})
	}
	var legacy map[string]any
	require.NoError(t, Unmarshal([]byte("value: !!binary dXNlZnVs\n"), &legacy))
	require.Equal(t, "useful", legacy["value"], "原有解码入口保持兼容")
}

func TestDecodeMappingRejectsDuplicateKeysBeforeSharedDecode(t *testing.T) {
	var reusedAnchor strings.Builder
	for i := range 600 {
		fmt.Fprintf(&reusedAnchor, "source%d: &key resolved%d\n*key: value\n", i, i)
	}
	for name, data := range map[string]string{
		"reused anchor names": reusedAnchor.String(),
		"600 duplicates":      strings.Repeat("a: b\n", 600),
		"alias first":         "anchor: &key duplicate\n*key: one\nduplicate: two\n",
		"alias last":          "anchor: &key duplicate\nduplicate: one\n*key: two\n",
		"different aliases":   "first: &first duplicate\nsecond: &second duplicate\n*first: one\n*second: two\n",
		"escaped key":         "x: one\n\"\\u0078\": two\n",
		"nested map":          "outer: {duplicate: one, duplicate: two}\n",
		"map in sequence":     "outer: [{duplicate: one, duplicate: two}]\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := DecodeMapping([]byte(data), 1<<20)
			require.Nil(t, out)
			// A constant error proves the guard ran before the shared decoder's
			// quadratic duplicate check could allocate a multi-megabyte error.
			require.EqualError(t, err, "duplicate YAML mapping key")
		})
	}
	out, err := DecodeMapping([]byte("first: {same: one}\nsecond: {same: two}\n"), 4096)
	require.NoError(t, err, "uniqueness is local to each mapping")
	require.Equal(t, map[string]any{
		"first": map[string]any{"same": "one"}, "second": map[string]any{"same": "two"},
	}, out)
}

func TestDecodeMappingBoundsFloatExponentWorkBeforeSharedDecode(t *testing.T) {
	var repeated strings.Builder
	for i := range 50 {
		fmt.Fprintf(&repeated, "field%d: 1e-1000000\n", i)
	}
	for name, data := range map[string]string{
		"repeated underflow": repeated.String(),
		"uppercase exponent": "value: 1E-1000000\n",
		"underscores":        "value: 1e-1_000_000\n",
		"hex underflow":      "value: !!float 0x1p-1000000\n",
		"hex uppercase":      "value: !!float -0X1P-1000000\n",
		"positive exponent":  "value: !!float 1e+1000000\n",
		"min int exponent":   "value: !!float 1e-9223372036854775808\n",
		"max int exponent":   "value: !!float 1e9223372036854775807\n",
	} {
		t.Run(name, func(t *testing.T) {
			require.Less(t, len(data), 4096)
			out, err := DecodeMapping([]byte(data), 1<<20)
			require.Nil(t, out)
			require.EqualError(t, err, "expanded YAML mapping exceeds byte budget")
		})
	}
	for _, value := range []string{"1e-9223372036854775809", "1e9223372036854775808", "1e-" + strings.Repeat("9", 100), "0x1p-9223372036854775809", "1e-", "1e+oops"} {
		t.Run(value, func(t *testing.T) {
			out, err := DecodeMapping([]byte("value: !!float "+value+"\n"), 4096)
			require.Nil(t, out)
			require.EqualError(t, err, "invalid or overflowing YAML float exponent")
		})
	}
}

func TestDecodeMappingFloatExponentBudgetIsShared(t *testing.T) {
	// One moderate underflow fits. Repeating or aliasing it must spend the same
	// budget again, rather than enforcing only a per-scalar exponent limit.
	out, err := DecodeMapping([]byte("value: 1e-400\n"), 4096)
	require.NoError(t, err)
	require.Equal(t, json.Number("1e-400"), out["value"])
	for _, data := range []string{
		"first: 1e-400\nsecond: 1e-400\n",
		"first: &number 1e-400\nsecond: *number\n",
	} {
		_, err := DecodeMapping([]byte(data), 4096)
		require.EqualError(t, err, "expanded YAML mapping exceeds byte budget")
	}
	// Scalar and container charges (90) and exponent work (2400) share a limit.
	_, err = DecodeMapping([]byte("a: 1e-400"), 2489)
	require.EqualError(t, err, "expanded YAML mapping exceeds byte budget")
	_, err = DecodeMapping([]byte("a: 1e-400"), 2490)
	require.NoError(t, err)
}

func TestDecodeMappingOrdinaryFloatsRetainSharedCodecSemantics(t *testing.T) {
	data := []byte(`ordinary: 1.25
positive: 2.5e3
underflow: 1e-400
precision: 1.00000000000000000001e-4
hex_integer: !!float 0xdead
text: "1e-1000000"
`)
	out, err := DecodeMapping(data, 8192)
	require.NoError(t, err)
	var shared map[string]any
	require.NoError(t, Unmarshal(data, &shared, json.UseNumber))
	require.Equal(t, shared, out)
	require.Equal(t, json.Number("1e-400"), out["underflow"])
	require.Equal(t, json.Number("1.00000000000000000001e-4"), out["precision"])
	require.Equal(t, json.Number("57005"), out["hex_integer"])
	require.Equal(t, "1e-1000000", out["text"])
}

func TestDecodeMappingByteBudgets(t *testing.T) {
	for _, limit := range []int{0, -1} {
		_, err := DecodeMapping([]byte("{}"), limit)
		require.ErrorContains(t, err, "maxBytes must be positive")
	}
	// Comments count towards input even though they do not expand into JSON.
	data := []byte("{} #" + strings.Repeat("x", 64))
	_, err := DecodeMapping(data, len(data)-1)
	require.ErrorContains(t, err, "input exceeds byte budget")
	_, err = DecodeMapping(data, len(data))
	require.NoError(t, err)

	// Exact conservative boundary: map 16 + two scalars (6*1 + 16) = 60.
	_, err = DecodeMapping([]byte("a: b"), 59)
	require.ErrorContains(t, err, "expanded YAML mapping exceeds byte budget")
	_, err = DecodeMapping([]byte("a: b"), 60)
	require.NoError(t, err)
	_, err = DecodeMapping([]byte("a: b"), int(^uint(0)>>1))
	require.NoError(t, err, "budget arithmetic must not overflow at MaxInt")
}
