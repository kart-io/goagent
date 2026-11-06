package json

import (
	"bytes"
	jsonv1 "encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type parityErr struct{ Code int }

func (e *parityErr) Error() string { return "e" }

type parityMarshaler struct {
	out string
	err error
}

func (m parityMarshaler) MarshalJSON() ([]byte, error) { return []byte(m.out), m.err }

type parityPtrMarshaler struct{}

func (*parityPtrMarshaler) MarshalJSON() ([]byte, error) { return []byte(`"ptr"`), nil }

type parityKey string

func (parityKey) MarshalJSON() ([]byte, error) { return []byte(`"J"`), nil }

type parityTextKey struct{ s string }

func (k *parityTextKey) MarshalText() ([]byte, error) { return []byte("T" + k.s), nil }

type parityNode struct{ Next *parityNode }

func parityNest(n int) any {
	var v any = 1
	for range n {
		v = []any{v}
	}
	return v
}

// requireSameError 要求两个错误同为 nil，或类型与文本都相同。
func requireSameError(t *testing.T, want, got error, msg string) {
	t.Helper()
	if want == nil || got == nil {
		require.Equal(t, want == nil, got == nil, "%s：v1 %v，本包 %v", msg, want, got)
		return
	}
	require.Equal(t, fmt.Sprintf("%T", want), fmt.Sprintf("%T", got), msg)
	require.Equal(t, want.Error(), got.Error(), msg)
}

// 编码与 v1 逐项相同：字节、是否失败、错误类型与文本；覆盖 v2 默认规则与 v1 不同的边界。
func TestMarshalMatchesV1AtBoundaries(t *testing.T) {
	one := 1
	var typedNil *parityErr
	cyclic := &parityNode{}
	cyclic.Next = cyclic
	selfMap := map[string]any{}
	selfMap["self"] = selfMap
	deepList := (*parityNode)(nil)
	for range 1001 {
		deepList = &parityNode{Next: deepList}
	}
	type stringTags struct {
		I  int             `json:"i,string"`
		B  bool            `json:"b,string"`
		S  string          `json:"s,string"`
		P  *int            `json:"p,string"`
		L  []int           `json:"l,string"`
		M  map[string]int  `json:"m,string"`
		St struct{ A int } `json:"st,string"`
	}
	type errFields struct {
		E  error `json:"e"`
		EZ error `json:"ez,omitzero"`
		EO error `json:"eo,omitempty"`
	}
	for _, c := range []struct {
		name string
		v    any
	}{
		{"errors.New", errors.New("x")},
		{"包装的 error", fmt.Errorf("w: %w", errors.Join(errors.New("a"), errors.New("b")))},
		{"有导出字段的 error", &parityErr{Code: 7}},
		{"error 字段 nil", errFields{}},
		{"error 字段非 nil", errFields{E: errors.New("a"), EZ: errors.New("b"), EO: errors.New("c")}},
		{"error 字段为带类型的 nil 指针", errFields{E: typedNil, EZ: typedNil, EO: typedNil}},
		{"map 与切片中的 error", map[string]any{"e": []any{errors.New("x"), nil}}},
		{"空结构体", struct{}{}},
		{"只有未导出字段", struct{ n, m int }{1, 2}},
		{"嵌入未导出结构体的导出字段", struct{ parityNode }{parityNode{}}},
		{"nil 指针指向无导出字段结构体", (*struct{ n int })(nil)},
		{",string 各类型", stringTags{I: 1, B: true, S: "s", P: &one, L: []int{1}, M: map[string]int{"a": 1}}},
		{",string 零值", stringTags{}},
		{"指针接收者 MarshalJSON 不可寻址", parityPtrMarshaler{}},
		{"指针接收者 MarshalJSON 在结构体字段", struct{ P parityPtrMarshaler }{}},
		{"指针接收者 MarshalJSON 可寻址", &parityPtrMarshaler{}},
		{"map 键的 MarshalJSON 被忽略", map[parityKey]int{"a": 1}},
		{"map 键 nil 指针 MarshalText", map[*parityTextKey]int{nil: 1}},
		{"map 键指针 MarshalText", map[*parityTextKey]int{{"x"}: 1}},
		{"MarshalJSON 输出带空白", parityMarshaler{out: ` { "a" : "<&>" } `}},
		{"MarshalJSON 输出非法 UTF-8", parityMarshaler{out: "\"\xff\""}},
		{"MarshalJSON 返回错误", parityMarshaler{err: errors.New("坏")}},
		{"MarshalJSON 输出非法 JSON", parityMarshaler{out: "{"}},
		{"MarshalJSON 输出为空", parityMarshaler{}},
		{"MarshalJSON 输出多个值", parityMarshaler{out: "1 2"}},
		{"NaN", math.NaN()},
		{"map 中的 -Inf", map[string]float64{"x": math.Inf(-1)}},
		{"float32 +Inf", float32(math.Inf(1))},
		{"通道", make(chan int)},
		{"函数字段", struct{ F func() }{}},
		{"复数", complex(1, 2)},
		{"指针循环", cyclic},
		{"map 循环", selfMap},
		{"非循环深链表 1001 层", deepList},
		{"嵌套 10000 层", parityNest(10000)},
		{"嵌套 10001 层", parityNest(10001)},
	} {
		t.Run(c.name, func(t *testing.T) {
			want, errV1 := jsonv1.Marshal(c.v)
			got, err := Marshal(c.v)
			requireSameError(t, errV1, err, "Marshal")
			require.Equal(t, string(want), string(got))

			var buf bytes.Buffer
			err = Encode(&buf, c.v)
			requireSameError(t, errV1, err, "Encode")
			if errV1 == nil {
				require.Equal(t, string(want)+"\n", buf.String(), "Encode")
			}
		})
	}
}

// 导出的错误类型就是 v1 的错误类型，调用方无需导入 encoding/json 即可判断。
func TestErrorTypesAreV1Types(t *testing.T) {
	_, err := Marshal(math.Inf(1))
	require.ErrorAs(t, err, new(*UnsupportedValueError))
	_, err = Marshal(make(chan int))
	require.ErrorAs(t, err, new(*UnsupportedTypeError))
	_, err = Marshal(parityMarshaler{err: errors.New("坏")})
	require.ErrorAs(t, err, new(*MarshalerError))
	require.ErrorAs(t, Unmarshal([]byte(`{`), new(any)), new(*SyntaxError))
	require.ErrorAs(t, Unmarshal([]byte(`"x"`), new(int)), new(*UnmarshalTypeError))
	require.ErrorAs(t, Unmarshal([]byte(`1`), nil), new(*InvalidUnmarshalError))
}

type parityTarget struct {
	N     int            `json:"n"`
	S     string         `json:"s"`
	B     bool           `json:"b"`
	NS    int            `json:"ns,string"`
	BS    bool           `json:"bs,string"`
	SS    string         `json:"ss,string"`
	PS    *int           `json:"ps,string"`
	Arr   [2]int         `json:"arr"`
	Bin   []byte         `json:"bin"`
	BArr  [3]byte        `json:"barr"`
	Named []parityByte   `json:"named"`
	T     time.Time      `json:"t"`
	D     time.Duration  `json:"d"`
	M     map[string]int `json:"m"`
	A     any            `json:"a"`
	Sub   *parityTarget  `json:"sub"`
	Iface fmt.Stringer   `json:"iface"`
}

type parityByte byte

// 解码与 v1 逐项相同：是否失败、错误类型与文本，以及出错时已写入的部分值。
func TestUnmarshalMatchesV1AtBoundaries(t *testing.T) {
	deep := strings.Repeat("[", 10001) + strings.Repeat("]", 10001)
	for _, c := range []struct{ name, in string }{
		{"类型错误后继续解码后续字段", `{"n":"x","s":"after"}`},
		{"多个类型错误只报第一个", `{"n":"x","b":1,"s":"after"}`},
		{"嵌套对象中的类型错误", `{"sub":{"n":"x","s":"in"},"s":"after"}`},
		{"语法错误不修改目标", `{"s":"x","n":`},
		{"尾部多余的值", `{"s":"x"} 1`},
		{"空输入", ``},
		{",string 数字与指针", `{"ns":"12","ps":"3"}`},
		{",string 布尔与字符串", `{"bs":"true","ss":"\"q\""}`},
		{",string 中的 null", `{"ps":"null","ns":"null"}`},
		{",string 非 JSON 数字语法", `{"ns":"0x10"}`},
		{",string 未加引号", `{"ns":12}`},
		{"数组短于 Go 数组", `{"arr":[1]}`},
		{"数组长于 Go 数组", `{"arr":[1,2,3]}`},
		{"base64 含换行", `{"bin":"aGVs\nbG8="}`},
		{"非法 base64", `{"bin":"***"}`},
		{"[N]byte 按数组解码", `{"barr":[1,2,3]}`},
		{"命名 byte 切片按 base64", `{"named":"AQI="}`},
		{"命名 byte 切片按数组", `{"named":[1,2]}`},
		{"宽松 RFC 3339 时间", `{"t":"2026-11-05T8:00:00Z"}`},
		{"默认规则不接受时长字符串", `{"d":"30s"}`},
		{"字段名大小写不敏感", `{"N":1,"S":"x"}`},
		{"重复键取最后一个", `{"n":1,"n":2}`},
		{"非法 UTF-8 替换", "{\"s\":\"\xff\"}"},
		{"未知字段忽略", `{"zzz":1,"n":1}`},
		{"null 保留原值", `{"n":null,"m":null}`},
		{"非空接口字段收到对象", `{"iface":{}}`},
		{"超出 int 范围", `{"n":1e100}`},
		{"小数解到 int", `{"n":1.5}`},
		{"嵌套 10001 层", `{"a":` + deep + `}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			one := 7
			fresh := func() *parityTarget { return &parityTarget{N: 9, S: "原值", M: map[string]int{"z": 1}, PS: &one} }
			want, got := fresh(), fresh()
			errV1 := jsonv1.Unmarshal([]byte(c.in), want)
			err := Unmarshal([]byte(c.in), got)
			requireSameError(t, errV1, err, "Unmarshal")
			require.True(t, reflect.DeepEqual(want, got), "v1  %+v\n本包 %+v", *want, *got)
		})
	}
}
