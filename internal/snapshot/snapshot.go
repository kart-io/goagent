package snapshot

import (
	"reflect"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
)

// Cloner 为包含私有可变状态的业务类型提供快照协议。
// 实现须返回同类型、完全独立的副本，且不得修改接收者；该方法必须可重复调用。
// 协议须为纯复制，不得重入存储、缓存或状态操作，也不得获取可能与调用方形成锁反转的锁。
// 部分原子更新与通知边界会在锁内调用协议；返回错误或panic不能遗留协议自己持有的资源。
type Cloner interface {
	CloneSnapshot() (interface{}, error)
}

// Clone 保留具体类型并复制 map、slice、指针、数组和导出结构字段。
// 支持循环引用；不透明的可变私有字段及非空函数、通道、unsafe.Pointer 必须由 Cloner 处理。
// 调用期间，调用方不得并发修改输入。time.Time 按其不可变值契约复制。
// 保留同一可寻址结构体、数组的导出内联字段的内部指针关系；不承诺跨独立分配对象或重叠切片视图的内部地址关系。
func Clone(value interface{}) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	v, err := cloneReflect(reflect.ValueOf(value), make(map[cloneVisit]reflect.Value))
	if err != nil {
		return nil, err
	}
	return v.Interface(), nil
}

type cloneVisit struct {
	typ reflect.Type
	ptr uintptr
	len int
}

func cloneReflect(v reflect.Value, seen map[cloneVisit]reflect.Value) (reflect.Value, error) {
	return cloneReflectInto(v, seen, reflect.Value{})
}

func cloneReflectInto(v reflect.Value, seen map[cloneVisit]reflect.Value, destination reflect.Value) (reflect.Value, error) {
	if synchronizationType(v.Type()) {
		return reflect.Value{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "同步类型 %v 不可复制为快照", v.Type()).WithComponent("snapshot").WithOperation("clone")
	}
	if v.Kind() == reflect.Interface {
		out := reflect.New(v.Type()).Elem()
		if v.IsNil() {
			return out, nil
		}
		item, err := cloneReflect(v.Elem(), seen)
		if err != nil {
			return reflect.Value{}, err
		}
		out.Set(item)
		return out, nil
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		return v, nil
	}
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice) && v.IsNil() {
		return reflect.Zero(v.Type()), nil
	}
	var visit cloneVisit
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		visit = cloneVisit{typ: v.Type(), ptr: v.Pointer()}
		// 同一元素类型的命名/普通指针使用同一地址身份，返回时恢复请求的具体类型。
		if v.Kind() == reflect.Pointer {
			visit.typ = reflect.PointerTo(v.Type().Elem())
		}
		if v.Kind() == reflect.Slice {
			visit.len = v.Len()
		}
		if prior, ok := seen[visit]; ok {
			if prior.Type() != v.Type() {
				return prior.Convert(v.Type()), nil
			}
			return prior, nil
		}
	}
	if v.CanInterface() {
		if cloner, ok := v.Interface().(Cloner); ok {
			result, err := cloner.CloneSnapshot()
			if err != nil {
				return reflect.Value{}, err
			}
			copy := reflect.ValueOf(result)
			if !copy.IsValid() || copy.Type() != v.Type() {
				return reflect.Value{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "存储快照协议必须返回同类型副本：%v", v.Type()).WithComponent("snapshot").WithOperation("clone")
			}
			if visit.typ != nil {
				seen[visit] = copy
			}
			return copy, nil
		}
	}
	copyChild := func(dst, src reflect.Value) error {
		item, err := cloneReflectInto(src, seen, dst)
		if err == nil {
			dst.Set(item)
		}
		return err
	}
	switch v.Kind() {
	case reflect.Pointer:
		out := reflect.New(v.Type().Elem())
		if out.Type() != v.Type() {
			out = out.Convert(v.Type())
		}
		seen[visit] = out
		if err := copyChild(out.Elem(), v.Elem()); err != nil {
			return reflect.Value{}, err
		}
		return out, nil
	case reflect.Map:
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		seen[visit] = out
		iter := v.MapRange()
		for iter.Next() {
			key, err := cloneReflect(iter.Key(), seen)
			if err != nil {
				return reflect.Value{}, err
			}
			item, err := cloneReflect(iter.Value(), seen)
			if err != nil {
				return reflect.Value{}, err
			}
			out.SetMapIndex(key, item)
		}
		return out, nil
	case reflect.Slice:
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		seen[visit] = out
		for i := 0; i < v.Len(); i++ {
			registerInterior(v.Index(i), out.Index(i), seen)
		}
		for i := 0; i < v.Len(); i++ {
			if err := copyChild(out.Index(i), v.Index(i)); err != nil {
				return reflect.Value{}, err
			}
		}
		return out, nil
	case reflect.Array:
		out := destination
		if !out.IsValid() {
			out = reflect.New(v.Type()).Elem()
		}
		registerInterior(v, out, seen)
		for i := 0; i < v.Len(); i++ {
			if err := copyChild(out.Index(i), v.Index(i)); err != nil {
				return reflect.Value{}, err
			}
		}
		return out, nil
	case reflect.Struct:
		out := destination
		if !out.IsValid() {
			out = reflect.New(v.Type()).Elem()
		}
		registerInterior(v, out, seen)
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath != "" {
				if hasMutableState(v.Field(i)) {
					return reflect.Value{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "快照类型 %v 的私有字段 %s 需要实现 CloneSnapshot", v.Type(), v.Type().Field(i).Name).WithComponent("snapshot").WithOperation("clone")
				}
				continue
			}
			if err := copyChild(out.Field(i), v.Field(i)); err != nil {
				return reflect.Value{}, err
			}
		}
		return out, nil
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		if v.IsNil() {
			return reflect.Zero(v.Type()), nil
		}
		return reflect.Value{}, agentErrors.Newf(agentErrors.CodeInvalidInput, "类型 %v 不支持快照，需要实现 CloneSnapshot", v.Type()).WithComponent("snapshot").WithOperation("clone")
	default:
		return v, nil
	}
}

// registerInterior 先登记内联字段的最终地址，避免Alias先于目标字段时生成独立副本。
// 结构体与数组直接写入最终目标位置；指针指向的独立对象仍由普通图遍历分配。
func registerInterior(src, dst reflect.Value, seen map[cloneVisit]reflect.Value) {
	if !src.CanAddr() || !src.CanInterface() || !dst.CanAddr() || !dst.CanSet() {
		return
	}
	seen[cloneVisit{typ: src.Addr().Type(), ptr: src.Addr().Pointer()}] = dst.Addr()
	if src.Type() == reflect.TypeFor[time.Time]() {
		return
	}
	if _, custom := src.Interface().(Cloner); custom {
		return
	}
	switch src.Kind() {
	case reflect.Struct:
		for i := 0; i < src.NumField(); i++ {
			if src.Type().Field(i).PkgPath == "" {
				registerInterior(src.Field(i), dst.Field(i), seen)
			}
		}
	case reflect.Array:
		for i := 0; i < src.Len(); i++ {
			registerInterior(src.Index(i), dst.Index(i), seen)
		}
	}
}

func hasMutableState(v reflect.Value) bool {
	if synchronizationType(v.Type()) {
		return true
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return !v.IsNil()
	case reflect.Interface:
		return !v.IsNil() && hasMutableState(v.Elem())
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			return false
		}
		for i := 0; i < v.NumField(); i++ {
			if hasMutableState(v.Field(i)) {
				return true
			}
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if hasMutableState(v.Index(i)) {
				return true
			}
		}
	}
	return false
}

func synchronizationType(t reflect.Type) bool {
	return t.PkgPath() == "sync" || t.PkgPath() == "sync/atomic" || t.PkgPath() == "internal/sync"
}
