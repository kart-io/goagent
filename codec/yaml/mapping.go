package yaml

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/kart-io/goagent/codec/json"
	yamlv3 "go.yaml.in/yaml/v3"
)

// DecodeMapping decodes one untrusted YAML mapping with string keys at every
// depth. Null and boolean values retain their types; numbers use json.Number.
// maxBytes must be positive and bounds both the input and a conservative estimate
// of the expanded JSON tree: 6*len(Value)+16 per scalar, plus 16 per container or
// alias dereference. Float exponents additionally cost 6*abs(exponent), bounding
// exact-number conversion work. This deliberately rejects some documents whose
// actual JSON would fit. Cycles, duplicate keys, and non-string keys (including
// YAML merge keys) are rejected before materialization. Unmarshal is unchanged.
// 二进制标量不能无损表示为 JSON 文本，本入口会拒绝该类型。
func DecodeMapping(data []byte, maxBytes int) (map[string]any, error) {
	if maxBytes <= 0 {
		return nil, errors.New("YAML mapping maxBytes must be positive")
	}
	if len(data) > maxBytes {
		return nil, errors.New("YAML mapping input exceeds byte budget")
	}
	dec := yamlv3.NewDecoder(bytes.NewReader(data))
	var doc yamlv3.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("YAML document must be a mapping")
		}
		return nil, err
	}
	var extra yamlv3.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("YAML mapping must contain a single document")
	}
	if doc.Kind != yamlv3.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yamlv3.MappingNode {
		return nil, errors.New("YAML document must be a mapping")
	}
	guard := mappingGuard{remaining: maxBytes, active: make(map[*yamlv3.Node]bool)}
	if err := guard.visit(doc.Content[0], false); err != nil {
		return nil, err
	}

	// Only the guarded document reaches Node.Decode through the shared converter.
	// Reuse its timestamp and exact-number handling without changing Unmarshal.
	encoded, err := yamlToJSON(data)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out, json.UseNumber); err != nil {
		return nil, err
	}
	return out, nil
}

type mappingGuard struct {
	remaining int
	active    map[*yamlv3.Node]bool
}

func (g *mappingGuard) visit(n *yamlv3.Node, key bool) error {
	if n == nil {
		return errors.New("invalid YAML alias target")
	}
	if g.active[n] {
		return errors.New("cyclic YAML alias")
	}
	// Subtract before multiplying so even a caller-provided MaxInt budget cannot
	// overflow. Every visit costs at least 16, bounding traversal work as well.
	if g.remaining < 16 {
		return errors.New("expanded YAML mapping exceeds byte budget")
	}
	g.remaining -= 16
	g.active[n] = true
	defer delete(g.active, n)

	if n.Kind == yamlv3.AliasNode {
		// Do not memoize aliases: each occurrence contributes its whole target tree.
		return g.visit(n.Alias, key)
	}
	if key && (n.Kind != yamlv3.ScalarNode || n.ShortTag() != "!!str") {
		return errors.New("YAML mapping keys must have type !!str")
	}
	switch n.Kind {
	case yamlv3.ScalarNode:
		// YAML 将 !!binary 解码成任意字节的 string，共享 JSON 编码会替换
		// 非 UTF-8 字节；在转换前拒绝，避免内容改变后仍报告成功。
		if n.ShortTag() == "!!binary" {
			return errors.New("YAML 映射不支持二进制标量")
		}
		if len(n.Value) > g.remaining/6 {
			return errors.New("expanded YAML mapping exceeds byte budget")
		}
		g.remaining -= 6 * len(n.Value)
		if n.ShortTag() == "!!float" {
			return g.chargeExponent(n.Value)
		}
	case yamlv3.MappingNode, yamlv3.SequenceNode:
		var keys map[string]struct{}
		var aliasNames map[string]struct{}
		if n.Kind == yamlv3.MappingNode {
			keys = make(map[string]struct{})
		}
		for i, child := range n.Content {
			isKey := n.Kind == yamlv3.MappingNode && i%2 == 0
			if err := g.visit(child, isKey); err != nil {
				return err
			}
			if isKey {
				if child.Kind == yamlv3.AliasNode {
					// Reusing an anchor name can give aliases different resolved
					// strings, but yaml also compares alias names for duplicates.
					// Reject that case here rather than let it amplify errors.
					if _, exists := aliasNames[child.Value]; exists {
						return errors.New("duplicate YAML mapping key")
					}
					if aliasNames == nil {
						aliasNames = make(map[string]struct{})
					}
					aliasNames[child.Value] = struct{}{}
				}
				// visit has already checked cycles and resolved the key's type.
				// Compare decoded scalar text, including aliases and quoted keys,
				// before yaml's quadratic duplicate check can accumulate errors.
				for child.Kind == yamlv3.AliasNode {
					child = child.Alias
				}
				if _, exists := keys[child.Value]; exists {
					return errors.New("duplicate YAML mapping key")
				}
				keys[child.Value] = struct{}{}
			}
		}
	default:
		return errors.New("invalid YAML mapping node")
	}
	return nil
}

func (g *mappingGuard) chargeExponent(value string) error {
	// The shared exact-number converter strips underscores before ParseFloat and
	// big.Rat.SetString. A short underflow literal can otherwise build a gigantic
	// denominator. Hexadecimal mantissas contain e/E digits, so only p/P denotes
	// their exponent (explicit !!float values reach the converter too).
	plain := strings.ReplaceAll(value, "_", "")
	mantissa := strings.TrimLeft(plain, "+-")
	markers := "eE"
	if strings.HasPrefix(mantissa, "0x") || strings.HasPrefix(mantissa, "0X") {
		markers = "pP"
	}
	i := strings.LastIndexAny(plain, markers)
	if i < 0 {
		return nil
	}
	exponent, err := strconv.ParseInt(plain[i+1:], 10, 64)
	if err != nil {
		return errors.New("invalid or overflowing YAML float exponent")
	}
	limit := int64(g.remaining / 6)
	// Check both signs before negating, including MinInt64; conversion to int
	// and multiplication are safe only after these checks.
	if exponent > limit || exponent < -limit {
		return errors.New("expanded YAML mapping exceeds byte budget")
	}
	if exponent < 0 {
		exponent = -exponent
	}
	g.remaining -= 6 * int(exponent)
	return nil
}
