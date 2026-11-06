package codec

import (
	"reflect"

	"github.com/go-viper/mapstructure/v2"
)

// Convert 把 map、切片等通用值按 json 标签转换到 out（须为非 nil 指针），不经过 JSON 编码。
// 转换是弱类型的：字符串数字、整数布尔等自动转换，适合插件、动态配置传来的 map[string]any。
// 与 JSON 一致，json:"-" 的字段不赋值。
func Convert(in, out any) error {
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           out,
		TagName:          "json",
		WeaklyTypedInput: true,
		DecodeHook:       mapstructure.DecodeHookFuncValue(dropDashKey),
	})
	if err != nil {
		return err
	}
	return dec.Decode(in)
}

// dropDashKey 在 map 解到含 json:"-" 字段的结构体前去掉键 "-"：mapstructure 把标签 "-" 当作字段名。
func dropDashKey(from, to reflect.Value) (any, error) {
	if from.Kind() != reflect.Map || from.Type().Key().Kind() != reflect.String || !from.MapIndex(reflect.ValueOf("-").Convert(from.Type().Key())).IsValid() {
		return from.Interface(), nil
	}
	target := to.Type()
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if target.Kind() != reflect.Struct || !hasDashField(target) {
		return from.Interface(), nil
	}
	kept := reflect.MakeMapWithSize(from.Type(), from.Len())
	for iter := from.MapRange(); iter.Next(); {
		if iter.Key().String() != "-" {
			kept.SetMapIndex(iter.Key(), iter.Value())
		}
	}
	return kept.Interface(), nil
}

func hasDashField(t reflect.Type) bool {
	for f := range t.Fields() {
		if f.Tag.Get("json") == "-" {
			return true
		}
	}
	return false
}
