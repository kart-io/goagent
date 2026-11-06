package codec

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
)

type appConfig struct {
	Name     string            `json:"name"`
	Retries  int               `json:"retries"`
	Enabled  bool              `json:"enabled"`
	Timeout  time.Duration     `json:"timeout"`
	Interval time.Duration     `json:"interval,omitzero"`
	Labels   map[string]string `json:"labels,omitempty"`
	Tools    []string          `json:"tools,omitempty"`
}

func TestDecodeYAMLAndJSONShareJSONTags(t *testing.T) {
	yamlDoc := "name: 客服\nretries: 3\nenabled: true\ntimeout: 30s\nlabels:\n  team: agent\ntools: [get_order, refund]\n"
	jsonDoc := `{"name":"客服","retries":3,"enabled":true,"timeout":"30s","labels":{"team":"agent"},"tools":["get_order","refund"]}`
	want := appConfig{Name: "客服", Retries: 3, Enabled: true, Timeout: 30 * time.Second, Labels: map[string]string{"team": "agent"}, Tools: []string{"get_order", "refund"}}
	for format, doc := range map[Codec]string{yamlCodec: yamlDoc, jsonCodec: jsonDoc} {
		t.Run(format.Name(), func(t *testing.T) {
			var got appConfig
			require.NoError(t, decodeStrict(format, []byte(doc), &got))
			require.Equal(t, want, got)
		})
	}
}

func TestDecodeDurations(t *testing.T) {
	for _, tc := range []struct {
		doc  string
		want time.Duration
	}{
		{"timeout: 30s", 30 * time.Second},
		{"timeout: 1m30s", 90 * time.Second},
		{"timeout: 250ms", 250 * time.Millisecond},
		{`timeout: "2h"`, 2 * time.Hour},
		{"timeout: 1500000000", 1500 * time.Millisecond}, // 整数按纳秒，与 goagent 编码输出一致
	} {
		var got appConfig
		require.NoError(t, decodeStrict(yamlCodec, []byte(tc.doc), &got), tc.doc)
		require.Equal(t, tc.want, got.Timeout, tc.doc)
	}
	var got appConfig
	require.ErrorContains(t, decodeStrict(yamlCodec, []byte("timeout: 30x"), &got), "30x")
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	for format, doc := range map[Codec]string{
		yamlCodec: "name: a\nretires: 3\n",
		jsonCodec: `{"name":"a","retires":3}`,
	} {
		t.Run(format.Name(), func(t *testing.T) {
			var got appConfig
			require.ErrorContains(t, decodeStrict(format, []byte(doc), &got), "retires")
		})
	}
	var got appConfig
	require.ErrorContains(t, decodeStrict(yamlCodec, []byte("NAME: a\n"), &got), "NAME", "字段名区分大小写")
}

func TestDecodeRejectsDuplicateKeys(t *testing.T) {
	for format, doc := range map[Codec]string{
		yamlCodec: "name: a\nname: b\n",
		jsonCodec: `{"name":"a","name":"b"}`,
	} {
		t.Run(format.Name(), func(t *testing.T) {
			var got appConfig
			require.Error(t, decodeStrict(format, []byte(doc), &got))
		})
	}
}

func TestDecodeMergesIntoDefaults(t *testing.T) {
	got := appConfig{Name: "默认", Retries: 5, Timeout: time.Minute}
	require.NoError(t, decodeStrict(yamlCodec, []byte("retries: 1\n"), &got))
	require.Equal(t, appConfig{Name: "默认", Retries: 1, Timeout: time.Minute}, got)

	for _, empty := range []string{"", "\n# 只有注释\n", "null"} {
		got := appConfig{Name: "默认"}
		require.NoError(t, decodeStrict(yamlCodec, []byte(empty), &got), "%q", empty)
		require.Equal(t, appConfig{Name: "默认"}, got, "空文档不能清空默认值")
	}
}

func TestLoadByExtension(t *testing.T) {
	dir := t.TempDir()
	for name, doc := range map[string]string{
		"app.yaml": "name: y\ntimeout: 1s\n",
		"app.YML":  "name: y\ntimeout: 1s\n",
		"app.json": `{"name":"y","timeout":"1s"}`,
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(doc), 0o600))
		var got appConfig
		require.NoError(t, ReadFile(path, &got, json.Strict), name)
		require.Equal(t, appConfig{Name: "y", Timeout: time.Second}, got, name)
	}

	bad := filepath.Join(dir, "app.toml")
	require.NoError(t, os.WriteFile(bad, []byte("name = 'y'"), 0o600))
	require.ErrorContains(t, ReadFile(bad, &appConfig{}, json.Strict), ".toml")

	unknown := filepath.Join(dir, "typo.yaml")
	require.NoError(t, os.WriteFile(unknown, []byte("nmae: y\n"), 0o600))
	err := ReadFile(unknown, &appConfig{}, json.Strict)
	require.ErrorContains(t, err, unknown, "错误需指明文件")
	require.ErrorContains(t, err, "nmae")

	require.Error(t, ReadFile(filepath.Join(dir, "missing.yaml"), &appConfig{}, json.Strict))
}

// YAML 1.1 会把 y、no、on 解析为布尔值；配置按 1.2 解析，它们仍是字符串。
func TestDecodeYAMLKeepsStringsLikeYesNo(t *testing.T) {
	for _, v := range []string{"y", "n", "yes", "no", "on", "off"} {
		var got appConfig
		require.NoError(t, decodeStrict(yamlCodec, []byte("name: "+v+"\n"), &got), v)
		require.Equal(t, v, got.Name)
	}
	var got appConfig
	require.Error(t, decodeStrict(yamlCodec, []byte("enabled: yes\n"), &got), "布尔字段只接受 true、false")
}

func TestDecodeRejectsMultipleYAMLDocuments(t *testing.T) {
	require.ErrorContains(t, decodeStrict(yamlCodec, []byte("name: a\n---\nname: b\n"), &appConfig{}), "一个 YAML 文档")
}

func TestDecodeYAMLEdgeValues(t *testing.T) {
	var got appConfig
	require.NoError(t, decodeStrict(yamlCodec, []byte("name: 2026-11-05\nlabels:\n  since: 2026-11-05T08:00:00Z\n"), &got))
	require.Equal(t, "2026-11-05", got.Name, "形如日期的值保持原文")
	require.Equal(t, "2026-11-05T08:00:00Z", got.Labels["since"])
	got = appConfig{}
	require.NoError(t, decodeStrict(yamlCodec, []byte("labels:\n  1: a\n  true: b\n  1.5: c\n"), &got))
	require.Equal(t, map[string]string{"1": "a", "true": "b", "1.5": "c"}, got.Labels, "标量键按原文转为字符串")
	require.Error(t, decodeStrict(yamlCodec, []byte("labels:\n  [1, 2]: a\n"), &appConfig{}))

	got = appConfig{}
	require.NoError(t, decodeStrict(yamlCodec, []byte("base: &base\n  team: agent\nlabels:\n  <<: *base\n  env: prod\nname: x\n"), &struct {
		*appConfig
		Base map[string]string `json:"base"`
	}{appConfig: &got}))
	require.Equal(t, map[string]string{"team": "agent", "env": "prod"}, got.Labels, "锚点与合并键照常生效")
}

// decodeStrict 以配置文件的严格规则解码，与 ReadFile 一致。
func decodeStrict(c Codec, data []byte, v any) error { return c.Unmarshal(data, v, json.Strict) }
