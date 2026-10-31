package plugin

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
)

// Env 是插件可见的宿主环境，每个插件一个。
type Env struct {
	name      string
	resources *Resources
	starting  atomic.Bool
}

func newEnv(name string, r *Resources) *Env { return &Env{name: name, resources: r} }

// Plugin 返回当前插件名。
func (e *Env) Plugin() string { return e.name }

// Resources 返回 Host 内所有插件共享的资源。
func (e *Env) Resources() *Resources { return e.resources }

// Provide 发布本插件的共享资源，只能在本插件 Start 执行期间调用；key 须以 "<插件名>." 开头，避免冒用其他插件的命名空间。
func (e *Env) Provide(key string, value any) error {
	if !e.starting.Load() {
		return fmt.Errorf("插件 %q 只能在 Start 中提供资源 %q", e.name, key)
	}
	prefix := e.name + "."
	if !strings.HasPrefix(key, prefix) || len(key) == len(prefix) {
		return fmt.Errorf("插件 %q 的资源名须以 %q 开头：%q", e.name, prefix, key)
	}
	if value == nil {
		return fmt.Errorf("资源 %q 为 nil", key)
	}
	return e.resources.put(key, value)
}

// Resources 保存插件在 Start 中发布的实例，并发安全；Host 停止后清空。
type Resources struct {
	mu     sync.RWMutex
	values map[string]any
}

func newResources() *Resources { return &Resources{values: map[string]any{}} }

func (r *Resources) put(key string, value any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.values[key]; ok {
		return fmt.Errorf("资源 %q 已存在", key)
	}
	r.values[key] = value
	return nil
}

// Resolve 返回资源及其是否存在。
func (r *Resources) Resolve(key string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.values[key]
	return v, ok
}

func (r *Resources) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.values)
}

// Lookup 按类型取出资源；不存在或类型不符时报错。
func Lookup[T any](r *Resources, key string) (T, error) {
	var zero T
	v, ok := r.Resolve(key)
	if !ok {
		return zero, fmt.Errorf("资源 %q 不存在", key)
	}
	typed, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("资源 %q 的类型是 %T，不是 %s", key, v, reflect.TypeFor[T]())
	}
	return typed, nil
}
