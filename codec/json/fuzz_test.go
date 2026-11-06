package json

import (
	"bytes"
	jsonv1 "encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
)

var fuzzSeeds = []string{
	`{"a":1,"b":[true,null,"x"],"c":{"d":1.5e-7}}`,
	`{"a":1,"A":2,"a":3}`,
	"\"\xff\xfe<tool>& \"",
	`[1e21, 1e20, -0, 0.000001, 123456789012345678901234567890]`,
	`{"max_retries":1,"MAX_RETRIES":2,"name":"n"}`,
	`{"nested":{"x":{"y":[{"z":"<>&"}]}}}`,
	`"\ud800"`,
	`{"t":1500000000}`,
}

// 任意 JSON 先按 v1 解出，再分别用 v1 与本包编码，字节必须相同：持久数据与指纹依赖这一点。
func FuzzMarshalMatchesV1(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var v any
		if jsonv1.Unmarshal(data, &v) != nil {
			return
		}
		want, err := jsonv1.Marshal(v)
		if err != nil {
			t.Skip("v1 无法编码")
		}
		got, err := Marshal(v)
		if err != nil {
			t.Fatalf("v1 能编码而本包失败: %v，输入 %q", err, data)
		}
		if string(got) != string(want) {
			t.Fatalf("编码与 v1 不同\n输入 %q\nv1  %s\n本包 %s", data, want, got)
		}
	})
}

// 任意字符串（含非法 UTF-8、HTML 字符、U+2028）的编码与 v1 相同。
func FuzzMarshalStringMatchesV1(f *testing.F) {
	for _, s := range []string{"", "<a&b>", "\xff", "  ", "二进制\xe4\xb8", "\x00\x1f\"\\"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		want, _ := jsonv1.Marshal(s)
		got, err := Marshal(s)
		if err != nil || string(got) != string(want) {
			t.Fatalf("字符串 %q：v1 %s，本包 %s（%v）", s, want, got, err)
		}
	})
}

type fuzzTarget struct {
	MaxRetries int               `json:"max_retries,omitempty"`
	Name       string            `json:"name"`
	Enabled    bool              `json:"enabled,omitempty"`
	Ratio      float64           `json:"ratio,omitempty"`
	Labels     map[string]string `json:"labels"`
	Tools      []string          `json:"tools"`
	Next       *fuzzTarget       `json:"next,omitempty"`
	Raw        jsonv1.RawMessage `json:"raw,omitempty"`
	Skip       string            `json:"-"`
	FooBar     int
}

// 解码到 any 与结构体时，是否成功、错误类型与文本、解出的值（含出错时已写入的部分）都与 v1 相同：
// 模型生成的工具参数按 v1 的宽容规则解码。
func FuzzUnmarshalMatchesV1(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s))
	}
	f.Add([]byte(`{"name":"a","labels":{"k":"v"},"tools":["x"],"next":{"name":"b"},"raw":{"z":1},"-":"x","foobar":3}`))
	f.Add([]byte(`{"Name":"a","NAME":"b","max_retries":"1"}`))
	f.Add([]byte(`{"max_retries":"x","name":"after","enabled":1,"next":{"ratio":"y"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var wantAny, gotAny any
		errV1 := jsonv1.Unmarshal(data, &wantAny)
		err := Unmarshal(data, &gotAny)
		if (errV1 == nil) != (err == nil) {
			t.Fatalf("解码到 any 是否成功与 v1 不同：v1 %v，本包 %v，输入 %q", errV1, err, data)
		}
		if err == nil && !reflect.DeepEqual(wantAny, gotAny) {
			t.Fatalf("解码到 any 的值与 v1 不同\n输入 %q\nv1  %#v\n本包 %#v", data, wantAny, gotAny)
		}

		var want, got fuzzTarget
		errV1 = jsonv1.Unmarshal(data, &want)
		err = Unmarshal(data, &got)
		if (errV1 == nil) != (err == nil) || (err != nil && (fmt.Sprintf("%T", errV1) != fmt.Sprintf("%T", err) || errV1.Error() != err.Error())) {
			t.Fatalf("解码到结构体的错误与 v1 不同\n输入 %q\nv1  %T %v\n本包 %T %v", data, errV1, errV1, err, err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("解码到结构体的值与 v1 不同\n输入 %q\nv1  %#v\n本包 %#v", data, want, got)
		}
	})
}

// Compact 与 Valid 对任意输入的判断和结果与 v1 json.Compact、json.Valid 相同。
func FuzzCompactValidMatchV1(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add([]byte(s))
	}
	f.Add([]byte(" { \"a\" :\n 1 } "))
	f.Add([]byte(`{"a":`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if got, want := Valid(data), jsonv1.Valid(data); got != want {
			t.Fatalf("Valid(%q) = %v，v1 为 %v", data, got, want)
		}
		var buf bytes.Buffer
		errV1 := jsonv1.Compact(&buf, data)
		got, err := Compact(data)
		if (errV1 == nil) != (err == nil) {
			t.Fatalf("Compact(%q) 是否成功与 v1 不同：v1 %v，本包 %v", data, errV1, err)
		}
		if err == nil && string(got) != buf.String() {
			t.Fatalf("Compact(%q)：v1 %q，本包 %q", data, buf.String(), got)
		}
	})
}

type fuzzRecord struct {
	ID      string         `json:"id"`
	Score   float64        `json:"score,omitempty"`
	Count   int            `json:"count,string"`
	Flag    bool           `json:"flag,string"`
	Label   string         `json:"label,string"`
	Err     error          `json:"err,omitzero"`
	Any     any            `json:"any"`
	Meta    map[string]any `json:"meta"`
	Bytes   []byte         `json:"bytes"`
	Hidden  int            `json:"-"`
	private int
}

// 由任意字段值组成的结构体（含 NaN、±Inf、error、,string 字段）编码与 v1 相同：字节、是否失败、错误类型与文本。
func FuzzMarshalStructMatchesV1(f *testing.F) {
	f.Add("a", 1.5, 3, true, "<x>", "", []byte("hi"), uint8(0))
	f.Add("", math.NaN(), -1, false, "\xff", "失败", []byte(nil), uint8(1))
	f.Add("\u2028", math.Inf(-1), 0, true, `"q"`, "e", []byte{0xff}, uint8(2))
	f.Fuzz(func(t *testing.T, id string, score float64, count int, flag bool, label, errText string, raw []byte, pick uint8) {
		r := fuzzRecord{ID: id, Score: score, Count: count, Flag: flag, Label: label, Bytes: raw, Hidden: count, private: count,
			Meta: map[string]any{id: score, label: []any{flag, errText}}}
		if errText != "" {
			r.Err = errors.New(errText)
		}
		switch pick % 4 {
		case 1:
			r.Any = score
		case 2:
			r.Any = r.Err
		case 3:
			r.Any = map[string]any{"nested": []any{id, count, r.Meta}}
		}
		for _, v := range []any{r, &r, []any{r, nil}} {
			want, errV1 := jsonv1.Marshal(v)
			got, err := Marshal(v)
			if (errV1 == nil) != (err == nil) || (err != nil && (fmt.Sprintf("%T", errV1) != fmt.Sprintf("%T", err) || errV1.Error() != err.Error())) {
				t.Fatalf("编码错误与 v1 不同：v1 %T %v，本包 %T %v，值 %#v", errV1, errV1, err, err, v)
			}
			if string(want) != string(got) {
				t.Fatalf("编码与 v1 不同\nv1  %s\n本包 %s", want, got)
			}
		}
	})
}
