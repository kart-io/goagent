package json

import (
	"bytes"
	stdjson "encoding/json"
	"io"

	"github.com/bytedance/sonic"
)

// JSON 抽象层，使用 sonic 作为底层实现，提供兼容标准库的 API
//
// 使用 sonic.ConfigStd 以确保与 encoding/json 完全兼容
// sonic 性能比标准库快 5-10 倍，特别适合高频的工具参数解析场景

var (
	// 使用标准兼容配置
	api = sonic.ConfigStd

	// Marshal 将 Go 值编码为 JSON
	Marshal = api.Marshal

	// MarshalIndent 将 Go 值编码为格式化的 JSON
	MarshalIndent = api.MarshalIndent

	// MarshalToString 将 Go 值编码为 JSON 字符串
	MarshalToString = api.MarshalToString

	// Unmarshal 将 JSON 解码为 Go 值
	Unmarshal = api.Unmarshal

	// UnmarshalFromString 从 JSON 字符串解码为 Go 值
	UnmarshalFromString = api.UnmarshalFromString

	// NewEncoder 创建一个将数据写入 w 的编码器
	NewEncoder = api.NewEncoder

	// NewDecoder 创建一个从 r 读取数据的解码器
	NewDecoder = api.NewDecoder

	// Valid 检查 JSON 数据是否有效
	Valid = api.Valid
)

// Encoder 将 Go 值编码为 JSON 并写入输出流
type Encoder = sonic.Encoder

// Decoder 从输入流读取并解码 JSON 数据
type Decoder = sonic.Decoder

// RawMessage 是原始编码的 JSON 值
// 它实现了 Marshaler 和 Unmarshaler 接口
type RawMessage = stdjson.RawMessage

// Number 代表 JSON 数字字面量
type Number = stdjson.Number

// Marshaler 是由可以将自身序列化为有效 JSON 的类型实现的接口
type Marshaler = stdjson.Marshaler

// Unmarshaler 是由可以从 JSON 描述反序列化自身的类型实现的接口
type Unmarshaler = stdjson.Unmarshaler

// MarshalOptions JSON 序列化选项
type MarshalOptions struct {
	// EscapeHTML 指定是否在 JSON 引号字符串中转义有问题的 HTML 字符
	// 默认值为 true，与标准库保持一致
	EscapeHTML bool

	// SortMapKeys 指定是否对 map 的键进行排序
	// 默认值为 false
	SortMapKeys bool

	// ValidateString 指定是否验证字符串为有效的 UTF-8
	// 默认值为 false
	ValidateString bool

	// NoNullSliceOrMap 指定是否将 nil slice/map 编码为空 slice/map
	// 默认值为 false
	NoNullSliceOrMap bool

	// NoQuoteTextMarshaler 指定是否对实现 encoding.TextMarshaler 的类型不加引号
	// 默认值为 false
	NoQuoteTextMarshaler bool
}

// UnmarshalOptions JSON 反序列化选项
type UnmarshalOptions struct {
	// UseNumber 指定是否将数字解码为 Number 而不是 float64
	// 默认值为 false
	UseNumber bool

	// DisallowUnknownFields 指定是否在遇到未知字段时返回错误
	// 默认值为 false
	DisallowUnknownFields bool

	// CopyString 指定是否复制 JSON 字符串而不是引用
	// 默认值为 false
	CopyString bool
}

// Pre-frozen configurations for common marshal options.
// These are computed once at package initialization to avoid repeated allocations.
var (
	// marshalConfigCache caches frozen sonic.API instances for marshal options.
	// Key is computed from MarshalOptions boolean flags as a bitmask.
	marshalConfigCache = func() map[uint8]sonic.API {
		cache := make(map[uint8]sonic.API, 32) // 2^5 combinations for 5 boolean flags
		for i := uint8(0); i < 32; i++ {
			config := sonic.Config{
				EscapeHTML:           i&1 != 0,
				SortMapKeys:          i&2 != 0,
				ValidateString:       i&4 != 0,
				NoNullSliceOrMap:     i&8 != 0,
				NoQuoteTextMarshaler: i&16 != 0,
			}
			cache[i] = config.Froze()
		}
		return cache
	}()

	// unmarshalConfigCache caches frozen sonic.API instances for unmarshal options.
	// Key is computed from UnmarshalOptions boolean flags as a bitmask.
	unmarshalConfigCache = func() map[uint8]sonic.API {
		cache := make(map[uint8]sonic.API, 8) // 2^3 combinations for 3 boolean flags
		for i := uint8(0); i < 8; i++ {
			config := sonic.Config{
				UseNumber:             i&1 != 0,
				DisallowUnknownFields: i&2 != 0,
				CopyString:            i&4 != 0,
			}
			cache[i] = config.Froze()
		}
		return cache
	}()
)

// marshalOptionsKey computes a cache key from MarshalOptions.
func marshalOptionsKey(opts MarshalOptions) uint8 {
	var key uint8
	if opts.EscapeHTML {
		key |= 1
	}
	if opts.SortMapKeys {
		key |= 2
	}
	if opts.ValidateString {
		key |= 4
	}
	if opts.NoNullSliceOrMap {
		key |= 8
	}
	if opts.NoQuoteTextMarshaler {
		key |= 16
	}
	return key
}

// unmarshalOptionsKey computes a cache key from UnmarshalOptions.
func unmarshalOptionsKey(opts UnmarshalOptions) uint8 {
	var key uint8
	if opts.UseNumber {
		key |= 1
	}
	if opts.DisallowUnknownFields {
		key |= 2
	}
	if opts.CopyString {
		key |= 4
	}
	return key
}

// MarshalWithOptions 使用指定选项将 Go 值编码为 JSON
//
// This function uses pre-frozen configurations for optimal performance.
// All 32 possible combinations of options are cached at package initialization.
func MarshalWithOptions(v interface{}, opts MarshalOptions) ([]byte, error) {
	key := marshalOptionsKey(opts)
	return marshalConfigCache[key].Marshal(v)
}

// UnmarshalWithOptions 使用指定选项将 JSON 解码为 Go 值
//
// This function uses pre-frozen configurations for optimal performance.
// All 8 possible combinations of options are cached at package initialization.
func UnmarshalWithOptions(data []byte, v interface{}, opts UnmarshalOptions) error {
	key := unmarshalOptionsKey(opts)
	return unmarshalConfigCache[key].Unmarshal(data, v)
}

// EncodeToWriter 将 Go 值编码为 JSON 并写入 writer
func EncodeToWriter(w io.Writer, v interface{}) error {
	enc := NewEncoder(w)
	return enc.Encode(v)
}

// DecodeFromReader 从 reader 读取并解码 JSON 数据
func DecodeFromReader(r io.Reader, v interface{}) error {
	dec := NewDecoder(r)
	return dec.Decode(v)
}

// Get 从 JSON 数据中获取指定路径的值
//
// 使用 sonic 的高性能查询 API
func Get(data []byte, path ...interface{}) (interface{}, error) {
	node, err := sonic.Get(data, path...)
	if err != nil {
		return nil, err
	}
	return node.Interface()
}

// GetString 从 JSON 数据中获取字符串值
func GetString(data []byte, path ...interface{}) (string, error) {
	node, err := sonic.Get(data, path...)
	if err != nil {
		return "", err
	}
	return node.String()
}

// GetInt64 从 JSON 数据中获取整数值
func GetInt64(data []byte, path ...interface{}) (int64, error) {
	node, err := sonic.Get(data, path...)
	if err != nil {
		return 0, err
	}
	return node.Int64()
}

// GetFloat64 从 JSON 数据中获取浮点数值
func GetFloat64(data []byte, path ...interface{}) (float64, error) {
	node, err := sonic.Get(data, path...)
	if err != nil {
		return 0, err
	}
	return node.Float64()
}

// GetBool 从 JSON 数据中获取布尔值
func GetBool(data []byte, path ...interface{}) (bool, error) {
	node, err := sonic.Get(data, path...)
	if err != nil {
		return false, err
	}
	return node.Bool()
}

// Compact 将 JSON 数据压缩为最小表示形式
func Compact(dst *[]byte, src []byte) error {
	var buf bytes.Buffer
	if err := stdjson.Compact(&buf, src); err != nil {
		return err
	}
	*dst = buf.Bytes()
	return nil
}

// Indent 将 JSON 数据格式化为易读的缩进形式
func Indent(dst *[]byte, src []byte, prefix, indent string) error {
	var buf bytes.Buffer
	if err := stdjson.Indent(&buf, src, prefix, indent); err != nil {
		return err
	}
	*dst = buf.Bytes()
	return nil
}

// HTMLEscape 将 JSON 数据中的 HTML 特殊字符转义
func HTMLEscape(dst *[]byte, src []byte) {
	var buf bytes.Buffer
	stdjson.HTMLEscape(&buf, src)
	*dst = buf.Bytes()
}
