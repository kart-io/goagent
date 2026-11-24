// exposurecheck 检查稳定包导出 API 传递暴露的非稳定类型是否登记，并补上 apidiff 逐包比较的盲区：
// 实验接口新增方法、实验函数类型改签名、别名改指向、标准库类型互换。
//
// 两棵源码树的稳定 API 与登记条目各自展开为「事实」字符串（别名一律解开），
// 基线中存在而 HEAD 中缺失的事实即为不兼容变更。
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/types"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const module = "github.com/kart-io/goagent"

type mode string

// 模式含义见 docs/API_STABILITY.md「传递暴露」。
const (
	modeFull     mode = "full"
	modeIdentity mode = "identity"
	modeAlias    mode = "alias"
)

var validModes = map[mode]bool{modeFull: true, modeIdentity: true, modeAlias: true}

func readEntries(name string) ([][]string, error) {
	data, err := os.ReadFile(name) // #nosec G304 -- 路径来自命令行参数，由 check_api_compat.sh 传入
	if err != nil {
		return nil, err
	}
	var out [][]string
	for _, raw := range strings.Split(string(data), "\n") {
		if i := strings.Index(raw, "#"); i >= 0 {
			raw = raw[:i]
		}
		if fields := strings.Fields(raw); len(fields) > 0 {
			out = append(out, fields)
		}
	}
	return out, nil
}

func loadStable(name string) ([]string, error) {
	lines, err := readEntries(name)
	if err != nil {
		return nil, fmt.Errorf("读取稳定包清单: %w", err)
	}
	out := make([]string, 0, len(lines))
	for _, f := range lines {
		out = append(out, f[0])
	}
	return out, nil
}

// loadRegistry 读取「标识符 模式」条目；标识符为相对模块根的包路径加类型名，如 memory.Memory。
func loadRegistry(name string) (map[string]mode, error) {
	lines, err := readEntries(name)
	if err != nil {
		return nil, fmt.Errorf("读取暴露登记表: %w", err)
	}
	reg := map[string]mode{}
	for _, f := range lines {
		if len(f) != 2 {
			return nil, fmt.Errorf("登记条目须为「标识符 模式」: %q", strings.Join(f, " "))
		}
		m := mode(f[1])
		if !validModes[m] {
			return nil, fmt.Errorf("登记条目 %s 的模式无效: %q", f[0], f[1])
		}
		if _, dup := reg[f[0]]; dup {
			return nil, fmt.Errorf("登记条目 %s 重复", f[0])
		}
		reg[f[0]] = m
	}
	return reg, nil
}

func rel(path string) string {
	if path == module {
		return "."
	}
	return strings.TrimPrefix(path, module+"/")
}

func inModule(p *types.Package) bool {
	return p != nil && (p.Path() == module || strings.HasPrefix(p.Path(), module+"/"))
}

type tree struct {
	stable map[string]bool
	roots  []*types.Package
	all    map[string]*types.Package
}

// load 加载稳定包及其依赖的类型信息。基线中尚不存在的稳定包（allowMissing）跳过。
func load(dir string, stable []string, allowMissing bool) (*tree, error) {
	t := &tree{stable: map[string]bool{}, all: map[string]*types.Package{}}
	patterns := make([]string, 0, len(stable))
	for _, p := range stable {
		t.stable[p] = true
		if p == "." {
			patterns = append(patterns, module)
		} else {
			patterns = append(patterns, module+"/"+p)
		}
	}
	cfg := &packages.Config{
		Dir:  dir,
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps,
		Env:  append(os.Environ(), "GOWORK=off"),
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("加载 %s: %w", dir, err)
	}
	var loadErrs []error
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			if allowMissing {
				continue
			}
			for _, e := range p.Errors {
				loadErrs = append(loadErrs, fmt.Errorf("%s: %s", p.PkgPath, e))
			}
			continue
		}
		t.roots = append(t.roots, p.Types)
	}
	if len(loadErrs) > 0 {
		return nil, errors.Join(loadErrs...)
	}
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.Types != nil {
			t.all[p.PkgPath] = p.Types
		}
	})
	sort.Slice(t.roots, func(i, j int) bool { return t.roots[i].Path() < t.roots[j].Path() })
	return t, nil
}

func (t *tree) nonStable(p *types.Package) bool {
	return inModule(p) && !t.stable[rel(p.Path())]
}

func (t *tree) lookup(ident string) *types.TypeName {
	dot := strings.LastIndex(ident, ".")
	if dot <= 0 {
		return nil
	}
	path := module + "/" + ident[:dot]
	if ident[:dot] == "." {
		path = module
	}
	p := t.all[path]
	if p == nil {
		return nil
	}
	tn, _ := p.Scope().Lookup(ident[dot+1:]).(*types.TypeName)
	return tn
}

func identOf(obj *types.TypeName) string { return rel(obj.Pkg().Path()) + "." + obj.Name() }

// finder 从稳定包导出 API 出发，收集可达的非稳定类型；full 与未登记的类型继续展开其内部。
type finder struct {
	t     *tree
	reg   map[string]mode
	from  string
	found map[string]string
	seen  map[types.Type]bool
	queue []*types.TypeName
}

func (f *finder) record(obj *types.TypeName) {
	id := identOf(obj)
	if _, ok := f.found[id]; ok {
		return
	}
	f.found[id] = f.from
	f.queue = append(f.queue, obj)
}

func (f *finder) walk(t types.Type) {
	if t == nil || f.seen[t] {
		return
	}
	f.seen[t] = true
	switch t := t.(type) {
	case *types.Alias:
		if f.t.nonStable(t.Obj().Pkg()) {
			f.record(t.Obj())
		}
		f.typeArgs(t.TypeArgs())
		f.walk(types.Unalias(t))
	case *types.Named:
		f.typeArgs(t.TypeArgs())
		obj := t.Obj()
		switch {
		case f.t.nonStable(obj.Pkg()):
			f.record(obj)
		case inModule(obj.Pkg()) && !obj.Exported():
			// 稳定包的未导出类型经导出 API 可达时，其导出成员同样对外可见。
			f.walk(t.Underlying())
			f.methods(t)
		}
	case *types.Pointer:
		f.walk(t.Elem())
	case *types.Slice:
		f.walk(t.Elem())
	case *types.Array:
		f.walk(t.Elem())
	case *types.Map:
		f.walk(t.Key())
		f.walk(t.Elem())
	case *types.Chan:
		f.walk(t.Elem())
	case *types.Signature:
		f.tparams(t.TypeParams())
		f.tuple(t.Params())
		f.tuple(t.Results())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if fld := t.Field(i); fld.Exported() || fld.Embedded() {
				f.walk(fld.Type())
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumEmbeddeds(); i++ {
			f.walk(t.EmbeddedType(i))
		}
		for i := 0; i < t.NumExplicitMethods(); i++ {
			if m := t.ExplicitMethod(i); m.Exported() {
				f.walk(m.Type())
			}
		}
	case *types.TypeParam:
		f.walk(t.Constraint())
	case *types.Union:
		for i := 0; i < t.Len(); i++ {
			f.walk(t.Term(i).Type())
		}
	}
}

func (f *finder) tuple(t *types.Tuple) {
	for i := 0; t != nil && i < t.Len(); i++ {
		f.walk(t.At(i).Type())
	}
}

func (f *finder) typeArgs(l *types.TypeList) {
	for i := 0; l != nil && i < l.Len(); i++ {
		f.walk(l.At(i))
	}
}

func (f *finder) tparams(l *types.TypeParamList) {
	for i := 0; l != nil && i < l.Len(); i++ {
		f.walk(l.At(i).Constraint())
	}
}

func (f *finder) methods(n *types.Named) {
	for i := 0; i < n.NumMethods(); i++ {
		if m := n.Method(i); m.Exported() {
			f.walk(m.Type())
		}
	}
}

func (f *finder) object(obj types.Object) {
	switch o := obj.(type) {
	case *types.TypeName:
		if _, ok := o.Type().(*types.Alias); ok {
			f.walk(o.Type())
			return
		}
		n, ok := o.Type().(*types.Named)
		if !ok {
			return
		}
		f.tparams(n.TypeParams())
		f.walk(n.Underlying())
		f.methods(n)
	default:
		f.walk(obj.Type())
	}
}

// exposures 返回可达的非稳定类型及其首次出现位置。
func exposures(t *tree, reg map[string]mode) map[string]string {
	f := &finder{t: t, reg: reg, found: map[string]string{}, seen: map[types.Type]bool{}}
	for _, p := range t.roots {
		for _, name := range p.Scope().Names() {
			obj := p.Scope().Lookup(name)
			if !obj.Exported() {
				continue
			}
			f.from = rel(p.Path()) + "." + name
			f.object(obj)
		}
	}
	for len(f.queue) > 0 {
		obj := f.queue[0]
		f.queue = f.queue[1:]
		id := identOf(obj)
		if f.reg[id] == modeIdentity {
			continue
		}
		f.from = id
		f.object(obj)
	}
	return f.found
}

// typeString 打印类型的结构身份：解开别名、模块内包写作 ./相对路径、忽略参数名与类型参数名。
func typeString(t types.Type) string {
	var b strings.Builder
	writeType(&b, t)
	return b.String()
}

func qualified(obj *types.TypeName) string {
	if obj.Pkg() == nil {
		return obj.Name()
	}
	if inModule(obj.Pkg()) {
		return "./" + rel(obj.Pkg().Path()) + "." + obj.Name()
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

func writeType(b *strings.Builder, t types.Type) {
	switch t := t.(type) {
	case *types.Alias:
		writeType(b, types.Unalias(t))
	case *types.Basic:
		b.WriteString(t.Name())
	case *types.Named:
		b.WriteString(qualified(t.Obj()))
		if args := t.TypeArgs(); args != nil && args.Len() > 0 {
			b.WriteByte('[')
			for i := 0; i < args.Len(); i++ {
				if i > 0 {
					b.WriteString(", ")
				}
				writeType(b, args.At(i))
			}
			b.WriteByte(']')
		}
	case *types.Pointer:
		b.WriteByte('*')
		writeType(b, t.Elem())
	case *types.Slice:
		b.WriteString("[]")
		writeType(b, t.Elem())
	case *types.Array:
		fmt.Fprintf(b, "[%d]", t.Len())
		writeType(b, t.Elem())
	case *types.Map:
		b.WriteString("map[")
		writeType(b, t.Key())
		b.WriteByte(']')
		writeType(b, t.Elem())
	case *types.Chan:
		b.WriteString(map[types.ChanDir]string{types.SendRecv: "chan ", types.SendOnly: "chan<- ", types.RecvOnly: "<-chan "}[t.Dir()])
		writeType(b, t.Elem())
	case *types.Signature:
		b.WriteString("func")
		writeSignature(b, t)
	case *types.Struct:
		b.WriteString("struct{")
		for i := 0; i < t.NumFields(); i++ {
			if i > 0 {
				b.WriteString("; ")
			}
			f := t.Field(i)
			if !f.Embedded() {
				b.WriteString(f.Name() + " ")
			}
			writeType(b, f.Type())
			if tag := t.Tag(i); tag != "" {
				fmt.Fprintf(b, " %q", tag)
			}
		}
		b.WriteByte('}')
	case *types.Interface:
		var parts []string
		for i := 0; i < t.NumMethods(); i++ {
			m := t.Method(i)
			var mb strings.Builder
			mb.WriteString(m.Name())
			writeSignature(&mb, m.Type().(*types.Signature))
			parts = append(parts, mb.String())
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if _, isIface := t.EmbeddedType(i).Underlying().(*types.Interface); !isIface {
				parts = append(parts, typeString(t.EmbeddedType(i)))
			}
		}
		sort.Strings(parts)
		b.WriteString("interface{" + strings.Join(parts, "; ") + "}")
	case *types.TypeParam:
		fmt.Fprintf(b, "$%d", t.Index())
	case *types.Union:
		for i := 0; i < t.Len(); i++ {
			if i > 0 {
				b.WriteString("|")
			}
			if t.Term(i).Tilde() {
				b.WriteByte('~')
			}
			writeType(b, t.Term(i).Type())
		}
	default:
		b.WriteString(t.String())
	}
}

func writeSignature(b *strings.Builder, s *types.Signature) {
	if tp := s.TypeParams(); tp != nil && tp.Len() > 0 {
		b.WriteByte('[')
		writeTypeParams(b, tp)
		b.WriteByte(']')
	}
	b.WriteByte('(')
	for i := 0; i < s.Params().Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		pt := s.Params().At(i).Type()
		if s.Variadic() && i == s.Params().Len()-1 {
			b.WriteString("...")
			pt = pt.(*types.Slice).Elem()
		}
		writeType(b, pt)
	}
	b.WriteByte(')')
	switch r := s.Results(); r.Len() {
	case 0:
	case 1:
		b.WriteByte(' ')
		writeType(b, r.At(0).Type())
	default:
		b.WriteString(" (")
		for i := 0; i < r.Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			writeType(b, r.At(i).Type())
		}
		b.WriteByte(')')
	}
}

func writeTypeParams(b *strings.Builder, tp *types.TypeParamList) {
	for i := 0; i < tp.Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		writeType(b, tp.At(i).Constraint())
	}
}

// typeFacts 展开类型的可兼容比较事实：结构体逐字段、方法逐个，接口与其他底层类型整体比较。
// 值接收者方法同时满足指针与值，故改为指针接收者才缺失事实。
func typeFacts(add func(string), name string, obj *types.TypeName) {
	if _, ok := obj.Type().(*types.Alias); ok {
		add("type " + name + " = " + typeString(obj.Type()))
		return
	}
	n, ok := obj.Type().(*types.Named)
	if !ok {
		return
	}
	head := "type " + name
	if tp := n.TypeParams(); tp != nil && tp.Len() > 0 {
		var b strings.Builder
		writeTypeParams(&b, tp)
		head += "[" + b.String() + "]"
	}
	if st, ok := n.Underlying().(*types.Struct); ok {
		add(head + " struct")
		for i := 0; i < st.NumFields(); i++ {
			if f := st.Field(i); f.Exported() {
				kind := "field"
				if f.Embedded() {
					kind = "embedded"
				}
				add(kind + " " + name + "." + f.Name() + " " + typeString(f.Type()))
			}
		}
	} else {
		add(head + " " + typeString(n.Underlying()))
	}
	for i := 0; i < n.NumMethods(); i++ {
		m := n.Method(i)
		if !m.Exported() {
			continue
		}
		sig := m.Type().(*types.Signature)
		var b strings.Builder
		writeSignature(&b, sig)
		add("method " + name + "." + m.Name() + b.String() + " on *T")
		if _, ptr := sig.Recv().Type().(*types.Pointer); !ptr {
			add("method " + name + "." + m.Name() + b.String() + " on T")
		}
	}
}

// facts 展开稳定包导出 API 与登记条目；登记条目按 HEAD 的模式解释。
func facts(t *tree, reg map[string]mode) map[string]bool {
	out := map[string]bool{}
	add := func(s string) { out[s] = true }
	for _, p := range t.roots {
		prefix := rel(p.Path()) + "."
		for _, name := range p.Scope().Names() {
			obj := p.Scope().Lookup(name)
			if !obj.Exported() {
				continue
			}
			switch o := obj.(type) {
			case *types.TypeName:
				typeFacts(add, prefix+name, o)
			case *types.Func:
				add("func " + prefix + name + " " + typeString(o.Type()))
			case *types.Var:
				add("var " + prefix + name + " " + typeString(o.Type()))
			case *types.Const:
				add("const " + prefix + name + " " + typeString(o.Type()) + " = " + o.Val().ExactString())
			}
		}
	}
	for id, m := range reg {
		obj := t.lookup(id)
		if obj == nil {
			continue
		}
		if m == modeIdentity {
			add("identity " + id)
			continue
		}
		typeFacts(add, id, obj)
		// 具名类型的导出常量是调用方可用的取值，同样冻结。
		scope := obj.Pkg().Scope()
		for _, name := range scope.Names() {
			if c, ok := scope.Lookup(name).(*types.Const); ok && c.Exported() && types.Identical(c.Type(), obj.Type()) {
				add("const " + rel(obj.Pkg().Path()) + "." + name + " " + typeString(c.Type()) + " = " + c.Val().ExactString())
			}
		}
	}
	return out
}

// validate 要求 HEAD 中的暴露与登记表一一对应，且模式与类型种类相符。
func validate(t *tree, reg map[string]mode) error {
	found := exposures(t, reg)
	var errs []string
	for id, from := range found {
		if _, ok := reg[id]; !ok {
			errs = append(errs, fmt.Sprintf("未登记的传递暴露: %s（经 %s 暴露）", id, from))
		}
	}
	for id, m := range reg {
		if _, ok := found[id]; !ok {
			errs = append(errs, fmt.Sprintf("登记已过期: %s 不再被稳定包导出 API 引用", id))
			continue
		}
		_, isAlias := t.lookup(id).Type().(*types.Alias)
		if isAlias != (m == modeAlias) {
			errs = append(errs, fmt.Sprintf("登记模式不符: %s 登记为 %s，但%s类型别名", id, m, map[bool]string{true: "是", false: "不是"}[isAlias]))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	sort.Strings(errs)
	return errors.New(strings.Join(errs, "\n"))
}

type options struct{ head, base, stable, registry string }

// run 校验登记表，并返回基线中存在而 HEAD 中缺失的事实（已排序）。
func run(o options) ([]string, error) {
	stable, err := loadStable(o.stable)
	if err != nil {
		return nil, err
	}
	reg, err := loadRegistry(o.registry)
	if err != nil {
		return nil, err
	}
	head, err := load(o.head, stable, false)
	if err != nil {
		return nil, err
	}
	if err := validate(head, reg); err != nil {
		return nil, err
	}
	if o.base == "" {
		return nil, nil
	}
	base, err := load(o.base, stable, true)
	if err != nil {
		return nil, err
	}
	headFacts := facts(head, reg)
	var missing []string
	for f := range facts(base, reg) {
		if !headFacts[f] {
			missing = append(missing, f)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

func report(w io.Writer, missing []string) error {
	for _, f := range missing {
		if _, err := fmt.Fprintf(w, "- [exposurecheck] 基线有而 HEAD 缺失: %s\n", f); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	var o options
	flag.StringVar(&o.head, "head", ".", "当前源码树（模块根）")
	flag.StringVar(&o.base, "base", "", "基线源码树；为空时只校验登记表")
	flag.StringVar(&o.stable, "stable", "scripts/stable-packages.txt", "稳定包清单")
	flag.StringVar(&o.registry, "registry", "scripts/api-exposure.txt", "传递暴露登记表")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "用法: exposurecheck [-head 目录] [-base 目录] [-stable 文件] [-registry 文件]")
		os.Exit(2)
	}
	missing, err := run(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := report(os.Stdout, missing); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
