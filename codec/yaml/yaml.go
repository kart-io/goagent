// Package yaml 是 goagent 统一的 YAML 编解码入口，按 YAML 1.2 解析，经 JSON 共用 json 标签与编解码规则。
package yaml

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"math/big"
	"strconv"
	"strings"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/kart-io/goagent/codec/json"
)

// Marshal 先按 JSON 规则编码，再转为两空格缩进的块风格 YAML；字段顺序与 JSON 相同。
// opts 作用于 JSON 阶段。
func Marshal(v any, opts ...json.Option) ([]byte, error) {
	data, err := json.Marshal(v, opts...)
	if err != nil {
		return nil, err
	}
	// 按 JSON 词法单元构造节点而不是把 JSON 当 YAML 解析：JSON 允许字符串中出现 DEL、C1 控制字符，
	// YAML 输入不允许；节点不带样式，由编码器按需加引号与转义，键序与 JSON 相同。
	root, err := jsonNode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	doc := yamlv3.Node{Kind: yamlv3.DocumentNode, Content: []*yamlv3.Node{root}}
	var buf bytes.Buffer
	enc := yamlv3.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unmarshal 把 data 转为 JSON 后按 json 标签解码到 v，opts 作用于 JSON 阶段（配置文件传 json.Strict）。
// 只有 true、false 是布尔值，y、no、on 等仍是字符串；只接受一个文档，空文档视为未配置并保留 v 的原值。
func Unmarshal(data []byte, v any, opts ...json.Option) error {
	doc, err := yamlToJSON(data)
	if err != nil {
		return err
	}
	if doc == nil {
		return nil
	}
	return json.Unmarshal(doc, v, opts...)
}

// yamlToJSON 只接受一个 YAML 文档，空文档返回 nil；重复键由 yaml 解析报错，
// 序列等非标量键无法作为对象成员名而报错。
func yamlToJSON(data []byte) ([]byte, error) {
	dec := yamlv3.NewDecoder(bytes.NewReader(data))
	var doc yamlv3.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	var extra yamlv3.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("只能包含一个 YAML 文档")
	}
	var nums exactNumbers
	keepScalarText(&doc, &nums)
	var v any
	if err := doc.Decode(&v); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	if nums.marker != "" {
		v = nums.restore(v)
	}
	return json.Marshal(v)
}

// keepScalarText 让隐式识别的时间戳、映射的标量键与 float64 表示不了的数字保持原文：解到 any 时 yaml 会把
// 2026-11-05 转成 time.Time，把 true、1.5 键转成非字符串键，把数字转成 float64；Kubernetes 同样把标量键转为字符串。
func keepScalarText(n *yamlv3.Node, nums *exactNumbers) {
	if n.Kind == yamlv3.ScalarNode {
		switch n.ShortTag() {
		case "!!timestamp":
			n.Tag = "!!str"
		case "!!float":
			nums.keep(n)
		}
	}
	for i, c := range n.Content {
		if n.Kind == yamlv3.MappingNode && i%2 == 0 && c.Kind == yamlv3.ScalarNode && c.ShortTag() != "!!merge" {
			c.Tag = "!!str"
		}
		keepScalarText(c, nums)
	}
}

// exactNumbers 把 float64 会丢精度的数字（超出 uint64 的整数在 yaml 中也按浮点数识别）暂存为带随机前缀的
// 字符串，经 yaml 解码（含锚点、合并键展开）后再还原为 JSON 数字原文，由 JSON 阶段按目标类型与选项解码。
// float64 能精确表示的数字不处理，1.0、1e3 仍可解到整数字段。
type exactNumbers struct{ marker string }

func (e *exactNumbers) keep(n *yamlv3.Node) {
	text, ok := lossyNumberText(n.Value)
	if !ok {
		return
	}
	if e.marker == "" {
		e.marker = "\x00number:" + rand.Text() + ":"
	}
	n.Tag, n.Value = "!!str", e.marker+text
}

func (e *exactNumbers) restore(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			t[k] = e.restore(c)
		}
	case []any:
		for i, c := range t {
			t[i] = e.restore(c)
		}
	case string:
		if text, ok := strings.CutPrefix(t, e.marker); ok {
			return json.Number(text)
		}
	}
	return v
}

// lossyNumberText 在 yaml 浮点数 value 经 float64 会丢精度时返回其 JSON 数字原文。
// 与 yaml 一致去掉下划线；.inf、溢出等 float64 无法解析的值沿用 yaml 原有处理。
func lossyNumberText(value string) (string, bool) {
	plain := strings.ReplaceAll(value, "_", "")
	f, err := strconv.ParseFloat(plain, 64)
	if err != nil {
		return "", false
	}
	exact, ok := new(big.Rat).SetString(plain)
	if !ok {
		return "", false
	}
	shortest, _ := new(big.Rat).SetString(strconv.FormatFloat(f, 'g', -1, 64))
	if exact.Cmp(shortest) == 0 {
		return "", false
	}
	text := jsonNumberText(plain)
	if !json.Valid([]byte(text)) {
		return "", false
	}
	return text, true
}

// jsonNumberText 把 yaml 的 +1.5、.5、5.、007.5 等写法改成 JSON 数字语法，数值不变。
func jsonNumberText(s string) string {
	sign := ""
	switch {
	case strings.HasPrefix(s, "-"):
		sign, s = "-", s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	exp := ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		s, exp = s[:i], s[i:]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	if frac != "" {
		frac = "." + frac
	}
	return sign + intPart + frac + exp
}

// jsonNode 把一个 JSON 值转为 YAML 节点，数字保留原文。
func jsonNode(r io.Reader) (*yamlv3.Node, error) {
	dec := json.NewDecoder(r)
	n, err := readNode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON 编码结果含多个值")
	}
	return n, nil
}

func stringNode(value string) *yamlv3.Node {
	n := &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!str", Value: value}
	// 编码器会把含制表符的多行文本写成块标量，制表符落在缩进位置后无法再解析；
	// 不加引号的 << 会被读作合并键；y、no、on 等在 YAML 1.1 读取方（如 PyYAML、yaml.v2）中是布尔值。
	if strings.Contains(value, "\t") || value == "<<" || yaml11Bool[value] {
		n.Style = yamlv3.DoubleQuotedStyle
	}
	return n
}

var yaml11Bool = map[string]bool{}

func init() {
	for _, w := range []string{"y", "yes", "n", "no", "on", "off"} {
		yaml11Bool[w] = true
		yaml11Bool[strings.ToUpper(w)] = true
		yaml11Bool[strings.ToUpper(w[:1])+w[1:]] = true
	}
}

func readNode(dec *json.Decoder) (*yamlv3.Node, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}
	scalar := func(tag, value string) *yamlv3.Node {
		return &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: tag, Value: value}
	}
	switch tok.Kind() {
	case '{', '[':
		n := &yamlv3.Node{Kind: yamlv3.MappingNode, Tag: "!!map"}
		end := byte('}')
		if tok.Kind() == '[' {
			n.Kind, n.Tag, end = yamlv3.SequenceNode, "!!seq", ']'
		}
		for byte(dec.PeekKind()) != end {
			if n.Kind == yamlv3.MappingNode {
				key, err := dec.ReadToken()
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, stringNode(key.String()))
			}
			child, err := readNode(dec)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, child)
		}
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		return n, nil
	case '"':
		return stringNode(tok.String()), nil
	case '0':
		// 不标注类型，由解析器按原文识别：超出 int64 的整数原文按浮点数读回，显式 !!int 会报错。
		return scalar("", tok.String()), nil
	case 't', 'f':
		return scalar("!!bool", tok.String()), nil
	default:
		return scalar("!!null", "null"), nil
	}
}
