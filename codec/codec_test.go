package codec

import (
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/kart-io/goagent/codec/yaml"
	"github.com/stretchr/testify/require"
)

func TestLookupCodec(t *testing.T) {
	for name, want := range map[string]Codec{"json": jsonCodec, "yaml": yamlCodec, "YAML": yamlCodec, "yml": yamlCodec} {
		got, ok := ByName(name)
		require.True(t, ok, name)
		require.Equal(t, want.Name(), got.Name(), name)
	}
	_, ok := ByName("toml")
	require.False(t, ok)

	for path, want := range map[string]Codec{"a.json": jsonCodec, "dir/a.yaml": yamlCodec, "A.YML": yamlCodec} {
		got, ok := ByExt(path)
		require.True(t, ok, path)
		require.Equal(t, want.Name(), got.Name(), path)
	}
	_, ok = ByExt("a.toml")
	require.False(t, ok)
}

func TestConvertMapToStructByJSONTags(t *testing.T) {
	type target struct {
		Name    string            `json:"name"`
		Retries int               `json:"retries"`
		Enabled bool              `json:"enabled"`
		Labels  map[string]string `json:"labels"`
		Skip    string            `json:"-"`
	}
	var out target
	require.NoError(t, Convert(map[string]any{
		"name": "a", "retries": "3", "enabled": 1, "labels": map[string]any{"k": "v"}, "-": "x", "Skip": "y",
	}, &out))
	require.Equal(t, target{Name: "a", Retries: 3, Enabled: true, Labels: map[string]string{"k": "v"}}, out, "弱类型：字符串数字、整数布尔自动转换；json:\"-\" 字段与 JSON 一样不赋值")

	require.Error(t, Convert(map[string]any{"retries": "x"}, &out), "无法转换时报错")
	require.Error(t, Convert(map[string]any{}, out), "目标须为指针")
}

func TestConvertSkipsDashFieldsInNestedStructs(t *testing.T) {
	type inner struct {
		Secret string `json:"-"`
		Value  int    `json:"value"`
	}
	type outer struct {
		Inner inner  `json:"inner"`
		Ptr   *inner `json:"ptr"`
	}
	var out outer
	require.NoError(t, Convert(map[string]any{
		"inner": map[string]any{"-": "x", "value": "1"},
		"ptr":   map[string]any{"-": "y", "value": 2},
		"-":     "dash",
	}, &out))
	require.Equal(t, inner{Value: 1}, out.Inner)
	require.Equal(t, &inner{Value: 2}, out.Ptr)

	// 与 JSON 解码结果一致
	var viaJSON outer
	require.NoError(t, json.Unmarshal([]byte(`{"inner":{"-":"x","value":1},"ptr":{"-":"y","value":2},"-":"dash"}`), &viaJSON))
	require.Equal(t, viaJSON, out)
}

// 经 Codec 接口编解码与直接调用子包函数结果相同。
func TestCodecDispatchMatchesSubpackages(t *testing.T) {
	v := map[string]any{"name": "y", "n": 1, "tools": []string{"<a>"}}
	for name, direct := range map[string]func(any, ...json.Option) ([]byte, error){"json": json.Marshal, "yaml": yaml.Marshal} {
		c, _ := ByName(name)
		got, err := c.Marshal(v, json.NoEscapeHTML)
		require.NoError(t, err)
		want, err := direct(v, json.NoEscapeHTML)
		require.NoError(t, err)
		require.Equal(t, string(want), string(got), name)

		var back map[string]any
		require.NoError(t, c.Unmarshal(got, &back, json.Strict), name)
		require.Equal(t, "y", back["name"], name)
	}
	_, err := yamlCodec.Marshal(make(chan int))
	require.Error(t, err, "无法编码的值报错而不是 panic")
}
