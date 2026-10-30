package preset

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"gopkg.in/yaml.v3"
)

// Needs 是组件配置需要的能力，用于信任检查：来自不可信层的组件超出 Limits 的部分属于扩权项。
// 配置中的 Secret 引用自动收集，无需列出。
type Needs struct {
	Sandbox  Sandbox
	Executor string
	Image    string
	Network  bool
	// HostProcesses 是要在宿主机启动的进程，不受沙箱约束。
	HostProcesses []string
	// Grants 是放宽权限的描述（如 allow 规则），来自不可信层时总是扩权项。
	Grants []string
}

// Spec 描述一种组件。C 是配置类型，Load 以严格模式解码（未知字段即报错）。
type Spec[C any] struct {
	Name    string
	Version string
	// Singleton 表示一个 preset 中最多一行。
	Singleton bool
	// Requires 是必须同时存在的组件名；Build 先初始化被依赖的组件。
	Requires []string
	// Validate 在 Load 时校验配置，可在其中填充默认值。
	Validate func(*C) error
	Needs    func(*C) Needs
	// Resolve 在 Build 时、信任检查之前调用，可读取工作区内的文件补全配置（如钩子定义），不得启动进程或连网。
	// 返回的 Needs 来自工作区内容，无论组件行来自哪一层都按不可信处理；补全后的配置计入授权摘要。
	Resolve func(root string, c *C) (Needs, error)
	// References 返回配置引用的其他 preset，Load 检查它们存在且没有环。
	References func(*C) []string
	// FilterTools 在作为子 Agent 初始化之前按工具过滤收窄配置；返回 false 表示组件不会提供保留的工具，不必初始化。
	// 未提供时组件照常初始化，工具在装配后按名称过滤。
	FilterTools func(c *C, rowID string, f ToolFilter) bool
	// Build 向装配器贡献内容。失败时已登记的资源按逆序关闭。
	Build func(context.Context, *Assembler, *C) error
}

// ToolFilter 限定子 Agent 的工具：Allow 非 nil 时只保留其中的工具，Deny 中的工具总被去掉。
type ToolFilter struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
}

// Keeps 报告 name 是否保留。
func (f ToolFilter) Keeps(name string) bool {
	return (f.Allow == nil || slices.Contains(f.Allow, name)) && !slices.Contains(f.Deny, name)
}

type component struct {
	name, version string
	singleton     bool
	requires      []string
	newConfig     func() any
	validate      func(any) error
	needs         func(any) Needs
	resolve       func(root string, cfg any) (any, Needs, error)
	references    func(any) []string
	filterTools   func(cfg any, rowID string, f ToolFilter) (any, bool)
	build         func(context.Context, *Assembler, any) error
}

var registry = struct {
	sync.RWMutex
	m map[string]*component
}{m: map[string]*component{}}

// Register 注册组件，通常在 init 中调用；名称重复或缺少 Build 时 panic。
func Register[C any](s Spec[C]) {
	if s.Name == "" || s.Version == "" || s.Build == nil {
		panic("preset: component name, version and build are required")
	}
	c := &component{
		name: s.Name, version: s.Version, singleton: s.Singleton, requires: s.Requires,
		newConfig: func() any { return new(C) },
		validate: func(cfg any) error {
			if s.Validate == nil {
				return nil
			}
			return s.Validate(cfg.(*C))
		},
		needs: func(cfg any) Needs {
			if s.Needs == nil {
				return Needs{}
			}
			return s.Needs(cfg.(*C))
		},
		build: func(ctx context.Context, a *Assembler, cfg any) error { return s.Build(ctx, a, cfg.(*C)) },
	}
	// 补全与过滤作用于配置的副本，Load 得到的配置在多次 Build 之间保持不变。
	if s.Resolve != nil {
		c.resolve = func(root string, cfg any) (any, Needs, error) {
			cp := *cfg.(*C)
			n, err := s.Resolve(root, &cp)
			return &cp, n, err
		}
	}
	if s.References != nil {
		c.references = func(cfg any) []string { return s.References(cfg.(*C)) }
	}
	if s.FilterTools != nil {
		c.filterTools = func(cfg any, rowID string, f ToolFilter) (any, bool) {
			cp := *cfg.(*C)
			return &cp, s.FilterTools(&cp, rowID, f)
		}
	}
	registry.Lock()
	defer registry.Unlock()
	if _, ok := registry.m[s.Name]; ok {
		panic(fmt.Sprintf("preset: component %q registered twice", s.Name))
	}
	registry.m[s.Name] = c
}

func lookup(name string) *component {
	registry.RLock()
	defer registry.RUnlock()
	return registry.m[name]
}

// Secret 是对宿主秘密的引用，配置中写作 {secret: NAME}。只有组件声明为 Secret 类型的字段接受它，
// 值在 Build 时经 Assembler.Secret 解析；Dump、错误与装配清单中只出现引用。
type Secret struct {
	Name string `yaml:"secret" json:"secret"`
}

var secretType = reflect.TypeOf(Secret{})

// secretsIn 收集配置中的全部 Secret 引用。
func secretsIn(cfg any) []string {
	var names []string
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			if v.Type() == secretType {
				names = append(names, v.Interface().(Secret).Name)
				return
			}
			for i := range v.NumField() {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(v.MapIndex(k))
			}
		}
	}
	walk(reflect.ValueOf(cfg))
	// map 的遍历顺序随机，排序使扩权项与授权摘要稳定。
	slices.Sort(names)
	return slices.Compact(names)
}

// Value 是字面量或秘密引用，配置中写作字符串或 {secret: NAME}，用于环境变量、请求头等可能含秘密的字段。
type Value struct {
	Literal string  `yaml:"-"`
	Secret  *Secret `yaml:"-"`
}

// UnmarshalYAML 只接受字符串或只含 secret 键的映射。
func (v *Value) UnmarshalYAML(n *yaml.Node) error {
	switch {
	case n.Kind == yaml.ScalarNode:
		return n.Decode(&v.Literal)
	case n.Kind == yaml.MappingNode && len(n.Content) == 2 && n.Content[0].Value == "secret" && n.Content[1].Kind == yaml.ScalarNode:
		v.Secret = &Secret{Name: n.Content[1].Value}
		return nil
	}
	return fmt.Errorf("第 %d 行：值只能是字符串或 {secret: NAME}", n.Line)
}
