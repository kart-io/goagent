package yaml

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
)

// 任意 YAML 解码不 panic；解码成功的值再编码、解码后不变。
func FuzzUnmarshalRoundTrips(f *testing.F) {
	for _, s := range []string{
		"name: 客服\nretries: 3\ntimeout: 30s\n",
		"a: [1, 2.5, true, null, y, no, '2026-11-05']\n",
		"base: &b\n  k: v\nm:\n  <<: *b\n  x: 1\n",
		"1: a\ntrue: b\n1.5: c\n",
		"s: \"<a&b>\\u2028\"\nmulti: |\n  line1\n  line2\n",
		"- - 1\n  - 2\n- {}\n- []\n",
		"a: 2026-11-05T08:00:00Z\nb: ~\nc: 0x1F\nd: 1e3\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var v any
		if Unmarshal(data, &v) != nil {
			return
		}
		out, err := Marshal(v)
		if err != nil {
			t.Fatalf("解码成功的值无法编码: %v，输入 %q", err, data)
		}
		var back any
		if err := Unmarshal(out, &back); err != nil {
			t.Fatalf("编码结果无法再解码: %v\n输入 %q\n编码 %q", err, data, out)
		}
		if !reflect.DeepEqual(v, back) {
			t.Fatalf("往返后值改变\n输入 %q\n编码 %q\n首次 %#v\n往返 %#v", data, out, v, back)
		}
	})
}

type fuzzConfig struct {
	Name    string            `json:"name"`
	Retries int               `json:"retries"`
	Ratio   float64           `json:"ratio"`
	Enabled bool              `json:"enabled"`
	Timeout time.Duration     `json:"timeout"`
	Labels  map[string]string `json:"labels"`
	Tools   []string          `json:"tools"`
}

// 任意配置值编码为 YAML 后按配置文件的严格规则解码，结果与原值相同。
func FuzzStructRoundTripsStrict(f *testing.F) {
	f.Add("y", "no", "2026-11-05", 3, 0.5, true, int64(30*time.Second))
	f.Add("", "~", "null", -1, 1e300, false, int64(-1))
	f.Add("a: b", "- x", "#c", 0, -0.0, true, int64(math.MaxInt64))
	f.Fuzz(func(t *testing.T, name, key, tool string, retries int, ratio float64, enabled bool, timeout int64) {
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			return
		}
		in := fuzzConfig{Name: name, Retries: retries, Ratio: ratio, Enabled: enabled, Timeout: time.Duration(timeout),
			Labels: map[string]string{key: tool}, Tools: []string{tool, name}}
		out, err := Marshal(in)
		if err != nil {
			// 非法 UTF-8 在 JSON 阶段替换为 U+FFFD 后仍可编码；其余错误不应出现。
			t.Fatalf("编码失败: %v，值 %#v", err, in)
		}
		var back fuzzConfig
		if err := Unmarshal(out, &back, json.Strict); err != nil {
			t.Fatalf("严格解码失败: %v\n值 %#v\n编码 %q", err, in, out)
		}
		var want fuzzConfig
		// 期望值按 JSON 规则规整（如非法 UTF-8 替换为 U+FFFD），再与 YAML 往返结果比较。
		data, _ := json.Marshal(in)
		if err := json.Unmarshal(data, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want, back) {
			t.Fatalf("往返后值改变\n编码 %q\n期望 %#v\n得到 %#v", out, want, back)
		}
	})
}
