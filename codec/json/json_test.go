package json

import (
	jsonv1 "encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sample struct {
	Tags    []string       `json:"tags"`
	Meta    map[string]int `json:"meta"`
	Timeout time.Duration  `json:"timeout"`
}

func TestStableRoundTripsNilAndDuration(t *testing.T) {
	in := sample{Timeout: 1500 * time.Millisecond}
	data, err := Marshal(in)
	require.NoError(t, err)
	require.JSONEq(t, `{"tags":null,"meta":null,"timeout":1500000000}`, string(data))
	var out sample
	require.NoError(t, Unmarshal(data, &out))
	require.Equal(t, in, out, "nil 经编解码仍为 nil，回放与首次执行相同")
}

func TestStableSortsMapKeys(t *testing.T) {
	m := map[string]int{"z": 1, "a": 2, "m": 3, "b": 4, "y": 5, "c": 6}
	for range 20 {
		data, err := Marshal(m)
		require.NoError(t, err)
		require.Equal(t, `{"a":2,"b":4,"c":6,"m":3,"y":5,"z":1}`, string(data))
	}
}

// 编码字节进入意图指纹与幂等键，必须与 v1 逐字节一致，升级前后才算出相同的键。
func TestStableEncodesSameBytesAsV1(t *testing.T) {
	for _, v := range []any{
		"<tool>a&b</tool>\u2028\u2029",
		"二进制输出\xff\xfe截断\xe4\xb8",
		map[string]any{"cmd": "a && b > c", "args": []string{"<x>"}},
		[]string{"parent", "id&<>"},
	} {
		want, err := jsonv1.Marshal(v)
		require.NoError(t, err)
		got, err := Marshal(v)
		require.NoError(t, err, "非法 UTF-8 不能让编码失败")
		require.Equal(t, string(want), string(got))
	}
}

// 模型生成的工具参数与用户配置按 v1 的宽容规则解码。
func TestStableDecodesLikeV1(t *testing.T) {
	var cfg struct {
		MaxRetries int `json:"max_retries"`
		Name       string
	}
	require.NoError(t, Unmarshal([]byte("{\"MAX_RETRIES\":1,\"max_retries\":2,\"name\":\"\xff\"}"), &cfg))
	require.Equal(t, 2, cfg.MaxRetries, "重复键后者生效")
	require.Equal(t, "\uFFFD", cfg.Name)

	var fold struct{ FooBar int }
	require.NoError(t, Unmarshal([]byte(`{"foo_bar":1}`), &fold))
	require.Zero(t, fold.FooBar, "与 v1 相同，大小写不敏感但不忽略下划线")

	require.True(t, Valid([]byte("{\"a\":1,\"a\":\"\xff\"}")))
}

// 调用方自定义类型上的 v1 标签语义不变：数字、布尔的 omitempty 仍省略零值。
func TestJSONKeepsV1OmitEmpty(t *testing.T) {
	type custom struct {
		N    int     `json:"n,omitempty"`
		B    bool    `json:"b,omitempty"`
		F    float64 `json:"f,omitempty"`
		S    string  `json:"s,omitempty"`
		Keep int     `json:"keep"`
		St   struct {
			A int `json:"a,omitempty"`
		} `json:"st,omitempty"`
		P *struct {
			A int `json:"a,omitempty"`
		} `json:"p,omitempty"`
	}
	for _, v := range []custom{{}, {P: &struct {
		A int `json:"a,omitempty"`
	}{}}} {
		data, err := Marshal(v)
		require.NoError(t, err)
		want, err := jsonv1.Marshal(v)
		require.NoError(t, err)
		require.Equal(t, string(want), string(data))
	}
}

// 解码到已有值时 null 保留原值（v1 语义），配置合并到默认值时不会被清零。
func TestJSONNullKeepsExistingValue(t *testing.T) {
	v := struct {
		N int    `json:"n"`
		S string `json:"s"`
	}{N: 5, S: "默认"}
	require.NoError(t, Unmarshal([]byte(`{"n":null,"s":null}`), &v))
	require.Equal(t, 5, v.N)
	require.Equal(t, "默认", v.S)
}
