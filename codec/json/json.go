// Package json 是 goagent 统一的 JSON 编解码入口，规则只在这里定义一次：编码字节与 v1 一致，
// 解码按 v1 的宽容规则；配置文件传 Strict。除 codec 及其子包外，代码不直接导入 JSON 库（由 apicheck 检查）。
package json

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"sync"
	"time"
)

// stable 是 JSON 的默认规则，即 v1 的全部默认选项（GOEXPERIMENT=jsonv2 下 v1 本身就是 v2 加这组选项）：
// 编码字节进入意图指纹、幂等键与持久记录，升级前后必须相同；解码对象含模型生成的工具参数，按 v1 的宽容规则。
var stable = jsonv1.DefaultOptionsV1()

// Strict 是配置文件的解码规则，与 Kubernetes 一致：未知字段、重复键、非法 UTF-8 报错，字段名区分大小写，
// 遇到第一个错误即停止并返回带 JSON 路径的 SemanticError；time.Duration 另接受 "30s"、"1m30s" 等 Go 时长字符串。
var Strict = jsonv2.JoinOptions(
	jsonv2.RejectUnknownMembers(true),
	jsontext.AllowDuplicateNames(false),
	jsontext.AllowInvalidUTF8(false),
	jsonv2.MatchCaseInsensitiveNames(false),
	jsonv1.ReportErrorsWithLegacySemantics(false),
	jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(decodeDurationString)),
)

// UseNumber 使解码到 any 的数字保留为 Number 原文（等价于 v1 的 Decoder.UseNumber），大整数不经 float64 丢精度。
var UseNumber = jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *any) error {
	if dec.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}
	*v = Number(val)
	return nil
}))

// Option 是编解码选项，由本包的 Strict、UseNumber、Indent 等提供。
type Option = jsonv2.Options

// RawMessage 是原样保留的 JSON 文本，编码时直接写出、解码时不解析。
type RawMessage = jsontext.Value

// Decoder 是流式 JSON 解码器；实现 UnmarshalJSONFrom(*Decoder) 的类型可沿用调用方选项解码自身。
type Decoder = jsontext.Decoder

// SemanticError 报告 Strict 解码时 JSON 与 Go 类型不匹配（如字符串解到整数字段、未知字段），含 JSON 路径。
type SemanticError = jsonv2.SemanticError

// 默认规则下的错误与 v1 相同。编码：UnsupportedValueError（NaN、±Inf、循环引用）、UnsupportedTypeError
// （通道、函数等）、MarshalerError（MarshalJSON 等方法出错）；解码：SyntaxError、UnmarshalTypeError、InvalidUnmarshalError。
type (
	UnsupportedValueError = jsonv1.UnsupportedValueError
	UnsupportedTypeError  = jsonv1.UnsupportedTypeError
	MarshalerError        = jsonv1.MarshalerError
	SyntaxError           = jsonv1.SyntaxError
	UnmarshalTypeError    = jsonv1.UnmarshalTypeError
	InvalidUnmarshalError = jsonv1.InvalidUnmarshalError
)

// Number 是 JSON 数字原文；v2 没有对应类型，沿用 v1 的 json.Number，编解码都原样支持。
type Number = jsonv1.Number

// NoEscapeHTML 关闭 <、>、& 的转义；用于交给模型或人阅读的文本，不用于进入指纹的编码。
var NoEscapeHTML = jsontext.EscapeForHTML(false)

// Indent 让编码结果按 indent 缩进；indent 只能含空格与制表符，否则 panic（v1 MarshalIndent 不限制）。
func Indent(indent string) Option { return jsontext.WithIndent(indent) }

// Pretty 按两空格缩进编码，用于写给人看的文件与终端输出。
var Pretty = Indent("  ")

// IndentPrefix 在缩进输出的每一行（首行除外）前加 prefix（只能是空白字符），与 Indent 一起使用。
func IndentPrefix(prefix string) Option { return jsontext.WithIndentPrefix(prefix) }

// Marshal 按默认规则编码 v；opts 在默认规则之后生效。
func Marshal(v any, opts ...Option) ([]byte, error) {
	return jsonv2.Marshal(v, withStable(opts)...)
}

// Unmarshal 把 data 解码到 v（须为非 nil 指针），合并到已有值；只能含一个 JSON 值。
func Unmarshal(data []byte, v any, opts ...Option) error {
	return jsonv2.Unmarshal(data, v, withStable(joinUnmarshalers(opts))...)
}

// pooledEncoder 复用编码器及其缓冲区：逐行写事件时每次新建编码器约多 10 次分配。
type pooledEncoder struct {
	buf bytes.Buffer
	enc *jsontext.Encoder
}

var encoders = sync.Pool{New: func() any { return &pooledEncoder{enc: jsontext.NewEncoder(io.Discard)} }}

// maxPooledBuffer 以上的缓冲区不放回池中，避免偶发的大值长期占用内存。
const maxPooledBuffer = 64 << 10

// Encode 把 v 编码写入 w 并追加换行（与 v1 Encoder.Encode 一致），适合逐行输出 JSON。
// 先完整编码到缓冲区再写入：编码器在缓冲区较大时会提前写出，失败时会在逐行输出中留下半条记录。
func Encode(w io.Writer, v any, opts ...Option) error {
	p := encoders.Get().(*pooledEncoder)
	p.buf.Reset()
	p.enc.Reset(&p.buf, withStable(opts)...)
	err := jsonv2.MarshalEncode(p.enc, v)
	if err == nil {
		_, err = w.Write(p.buf.Bytes())
	}
	p.enc.Reset(io.Discard)
	if p.buf.Cap() <= maxPooledBuffer {
		encoders.Put(p)
	}
	return err
}

// Decode 读取 r 的全部内容并解码到 v；只能含一个 JSON 值。
func Decode(r io.Reader, v any, opts ...Option) error {
	return jsonv2.UnmarshalRead(r, v, withStable(joinUnmarshalers(opts))...)
}

// DecodeNext 从 dec 读取下一个 JSON 值解码到 v，沿用 dec 上的选项（opts 优先），time.Duration
// 缺省按整数纳秒。供实现 UnmarshalJSONFrom 的类型使用，调用方的选项（如 Strict）由此传入自定义解码。
func DecodeNext(dec *Decoder, v any, opts ...Option) error {
	return jsonv2.UnmarshalDecode(dec, v, append([]Option{jsonv1.FormatDurationAsNano(true)}, joinUnmarshalers(opts)...)...)
}

// NewDecoder 返回按默认规则读取 r 的 Decoder。
func NewDecoder(r io.Reader, opts ...Option) *Decoder {
	return jsontext.NewDecoder(r, withStable(joinUnmarshalers(opts))...)
}

// Compact 返回去掉空白后的 data 副本，键序、数字写法与字符串原文（含转义与非法字节）原样保留，容忍重复键与非法 UTF-8。
// 直接用 v1 json.Compact：jsontext 的 Compact 会按编码选项重写字符串转义。
func Compact(data []byte) (RawMessage, error) {
	var buf bytes.Buffer
	buf.Grow(len(data))
	if err := jsonv1.Compact(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Valid 报告 data 是否为语法正确的 JSON，与 v1 json.Valid 同样允许重复键与非法 UTF-8。
func Valid(data []byte) bool { return len(data) > 0 && jsontext.Value(data).IsValid(stable) }

func withStable(opts []Option) []Option {
	return append([]Option{stable}, opts...)
}

// joinUnmarshalers 合并各选项的解码钩子：v2 中后一个 WithUnmarshalers 会整体替换前一个，
// 同时传 Strict 与 UseNumber 时其中一个钩子会失效。
func joinUnmarshalers(opts []Option) []Option {
	var hooks []*jsonv2.Unmarshalers
	for _, opt := range opts {
		if u, ok := jsonv2.GetOption(opt, jsonv2.WithUnmarshalers); ok && u != nil {
			hooks = append(hooks, u)
		}
	}
	if len(hooks) < 2 {
		return opts
	}
	return append(opts[:len(opts):len(opts)], jsonv2.WithUnmarshalers(jsonv2.JoinUnmarshalers(hooks...)))
}

// decodeDurationString 解析 Go 时长字符串；数字交回默认的整数纳秒解码。
func decodeDurationString(dec *jsontext.Decoder, d *time.Duration) error {
	if dec.PeekKind() != '"' {
		return errors.ErrUnsupported
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	parsed, err := time.ParseDuration(tok.String())
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
