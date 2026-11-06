// Package toolargs 解析模型给出的工具参数：float64 表示不了的数字保留为 json.Number，
// 使校验、授权、审批、durable 意图、缓存与执行看到同一个值。
package toolargs

import (
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/kart-io/goagent/codec/json"
)

// Parse 解析参数 JSON 文本，只接受对象（null 不算）。float64 能精确表示的数字解为 float64；
// 大整数、高精度小数等会丢精度的数字保留为 json.Number，避免舍入后操作错误的对象。
// 超出 float64 范围的数字（如 1e400）与默认解码一样视为无效参数。
func Parse(raw string) (map[string]any, bool) {
	var args map[string]any
	if json.Unmarshal([]byte(raw), &args, json.UseNumber) != nil || args == nil {
		return nil, false
	}
	for k, v := range args {
		var ok bool
		if args[k], ok = restore(v); !ok {
			return nil, false
		}
	}
	return args, true
}

// floatView 返回把 json.Number 换成 float64 的参数，供只认 float64 的 jsonschema 校验结构；
// 不含 json.Number 时返回 args 本身。json.Number 底层是字符串，转换失败时不能原样交给 jsonschema，
// 否则会被当作字符串通过校验。
func floatView(args map[string]any) (map[string]any, error) {
	if !hasNumber(args) {
		return args, nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		f, err := toFloat(v, "/"+k)
		if err != nil {
			return nil, err
		}
		out[k] = f
	}
	return out, nil
}

// Float64 取 JSON 数字参数的值，接受 float64 与 json.Number；json.Number 会舍入到最近的 float64。
func Float64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// Compare 按原文比较 n 与 schema 中的十进制界限 f，返回 -1、0 或 1；n 不是合法数字时返回 0。
func Compare(n json.Number, f float64) int {
	x, ok := new(big.Rat).SetString(string(n))
	if !ok {
		return 0
	}
	return x.Cmp(schemaRat(f))
}

// IsInteger 按原文报告 n 是否为整数：1.0000000000000001 舍入成 float64 是 1，但不是整数。
func IsInteger(n json.Number) bool {
	x, ok := new(big.Rat).SetString(string(n))
	return ok && x.IsInt()
}

// Validate 按 schema 校验参数。jsonschema 只认 float64，json.Number 先转为 float64 校验结构，
// 再按原文精确检查 type integer、minimum、maximum、exclusiveMinimum、exclusiveMaximum 与 multipleOf，
// 避免 1.0000000000000001 舍入成 1 后绕过约束。json.Number 所在路径上的 schema 只要用到 exactKeywords
// 以外的关键字（anyOf、patternProperties、元组 items、非本地 $ref 等）就无法精确判断，直接报错，
// 由调用方交还模型改正。结构校验先看舍入值，舍入后才越界的合法值（如 9007199254740993 对 multipleOf 3、
// 1.0000000000000001 对 exclusiveMinimum 1）会被保守拒绝；这类误拒只会交还模型，不会执行越界的值。
func Validate(r *jsonschema.Resolved, args map[string]any) error {
	view, err := floatView(args)
	if err != nil {
		return err
	}
	if err := r.Validate(view); err != nil {
		return err
	}
	if !hasNumber(args) {
		return nil
	}
	root := r.Schema()
	return checkExact(root, root, args, "")
}

func checkExact(root, s *jsonschema.Schema, v any, path string) error {
	if s == nil || !hasNumber(v) {
		return nil
	}
	for range 32 {
		if s.Ref == "" {
			break
		}
		next := localRef(root, s.Ref)
		if next == nil || !usesOnly(s, annotationKeywords, "Ref") {
			return unchecked(path)
		}
		s = next
	}
	if s.Ref != "" || !usesOnly(s, annotationKeywords, exactKeywords...) {
		return unchecked(path)
	}
	switch t := v.(type) {
	case json.Number:
		return checkNumber(s, t, path)
	case map[string]any:
		for k, c := range t {
			sub, ok := s.Properties[k]
			if !ok {
				sub = s.AdditionalProperties
			}
			if err := checkExact(root, sub, c, path+"/"+k); err != nil {
				return err
			}
		}
	case []any:
		for i, c := range t {
			sub := s.Items
			if i < len(s.PrefixItems) {
				sub = s.PrefixItems[i]
			}
			if err := checkExact(root, sub, c, fmt.Sprintf("%s/%d", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkNumber(s *jsonschema.Schema, n json.Number, path string) error {
	x, ok := new(big.Rat).SetString(string(n))
	if !ok {
		return fmt.Errorf("validating %s: invalid number %s", path, n)
	}
	types := s.Types
	if s.Type != "" {
		types = []string{s.Type}
	}
	if slices.Contains(types, "integer") && !slices.Contains(types, "number") && !x.IsInt() {
		return fmt.Errorf("validating %s: %s is not an integer", path, n)
	}
	bound := func(f *float64, bad func(int) bool, name string) error {
		if f != nil && bad(x.Cmp(schemaRat(*f))) {
			return fmt.Errorf("validating %s: %s violates %s %v", path, n, name, *f)
		}
		return nil
	}
	if err := bound(s.Minimum, func(c int) bool { return c < 0 }, "minimum"); err != nil {
		return err
	}
	if err := bound(s.Maximum, func(c int) bool { return c > 0 }, "maximum"); err != nil {
		return err
	}
	if err := bound(s.ExclusiveMinimum, func(c int) bool { return c <= 0 }, "exclusiveMinimum"); err != nil {
		return err
	}
	if err := bound(s.ExclusiveMaximum, func(c int) bool { return c >= 0 }, "exclusiveMaximum"); err != nil {
		return err
	}
	if s.MultipleOf != nil {
		if m := schemaRat(*s.MultipleOf); m.Sign() != 0 && !new(big.Rat).Quo(x, m).IsInt() {
			return fmt.Errorf("validating %s: %s is not a multiple of %v", path, n, *s.MultipleOf)
		}
	}
	return nil
}

// schemaRat 按 schema 作者写下的十进制值比较：0.1 取 1/10，而不是 float64 0.1 的二进制近似值。
func schemaRat(f float64) *big.Rat {
	r, _ := new(big.Rat).SetString(strconv.FormatFloat(f, 'g', -1, 64))
	return r
}

func localRef(root *jsonschema.Schema, ref string) *jsonschema.Schema {
	if name, ok := strings.CutPrefix(ref, "#/$defs/"); ok {
		return root.Defs[name]
	}
	if name, ok := strings.CutPrefix(ref, "#/definitions/"); ok {
		return root.Definitions[name]
	}
	if ref == "#" {
		return root
	}
	return nil
}

// annotationKeywords 不约束取值，或只约束字符串、数组长度、属性名等与数字精度无关的方面；
// uniqueItems 按 float64 比较只会更严格，不会放过原文不同的重复值。
var annotationKeywords = map[string]bool{
	"ID": true, "Schema": true, "Comment": true, "Defs": true, "Definitions": true, "Anchor": true, "DynamicAnchor": true,
	"Vocabulary": true, "Title": true, "Description": true, "Default": true, "Deprecated": true, "ReadOnly": true,
	"WriteOnly": true, "Examples": true, "PropertyOrder": true, "Format": true, "ContentEncoding": true,
	"ContentMediaType": true, "Extra": true, "MinLength": true, "MaxLength": true, "Pattern": true, "MinItems": true,
	"MaxItems": true, "UniqueItems": true, "MinProperties": true, "MaxProperties": true, "Required": true,
	"DependentRequired": true, "DependencyStrings": true, "PropertyNames": true,
}

// exactKeywords 是 checkExact 按原文检查或逐层下探的关键字。
var exactKeywords = []string{"Type", "Types", "MultipleOf", "Minimum", "Maximum", "ExclusiveMinimum", "ExclusiveMaximum",
	"Properties", "AdditionalProperties", "PrefixItems", "Items"}

// usesOnly 报告 s 是否只用了 allowed 与 extra 中的关键字；采用白名单，jsonschema 新增关键字时默认拒绝。
func usesOnly(s *jsonschema.Schema, allowed map[string]bool, extra ...string) bool {
	v := reflect.ValueOf(s).Elem()
	for i := range v.NumField() {
		f := v.Type().Field(i)
		if !f.IsExported() || allowed[f.Name] || slices.Contains(extra, f.Name) {
			continue
		}
		if !v.Field(i).IsZero() {
			return false
		}
	}
	return true
}

func unchecked(path string) error {
	return fmt.Errorf("validating %s: number exceeds float64 precision and the schema here cannot be checked exactly", path)
}

// restore 把 float64 能精确表示的 json.Number 换回 float64；数字超出 float64 范围时返回 false。
func restore(v any) (any, bool) {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil, false
		}
		if exactFloat(string(t), f) {
			return f, true
		}
	case map[string]any:
		for k, c := range t {
			var ok bool
			if t[k], ok = restore(c); !ok {
				return nil, false
			}
		}
	case []any:
		for i, c := range t {
			var ok bool
			if t[i], ok = restore(c); !ok {
				return nil, false
			}
		}
	}
	return v, true
}

func toFloat(v any, path string) (any, error) {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil, fmt.Errorf("validating %s: %s is not a finite float64 number", path, t)
		}
		return f, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, c := range t {
			f, err := toFloat(c, path+"/"+k)
			if err != nil {
				return nil, err
			}
			out[k] = f
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, c := range t {
			f, err := toFloat(c, fmt.Sprintf("%s/%d", path, i))
			if err != nil {
				return nil, err
			}
			out[i] = f
		}
		return out, nil
	}
	return v, nil
}

func hasNumber(v any) bool {
	switch t := v.(type) {
	case json.Number:
		return true
	case map[string]any:
		for _, c := range t {
			if hasNumber(c) {
				return true
			}
		}
	case []any:
		for _, c := range t {
			if hasNumber(c) {
				return true
			}
		}
	}
	return false
}

// exactFloat 报告 f 的最短十进制表示是否与 text 数值相等，即解成 float64 不丢信息。
func exactFloat(text string, f float64) bool {
	want, ok := new(big.Rat).SetString(text)
	if !ok {
		return false
	}
	got, ok := new(big.Rat).SetString(strconv.FormatFloat(f, 'g', -1, 64))
	return ok && want.Cmp(got) == 0
}
