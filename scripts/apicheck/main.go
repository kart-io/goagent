// apicheck 检查导入层级、格式转换入口、错误定义归属与业务错误构造。
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const module = "github.com/kart-io/goagent/"

type finding struct {
	line    int
	message string
}

func forbidden(file, path string) bool {
	source := strings.Split(filepath.ToSlash(file), "/")[0]
	// 常量是叶子层，只能依赖标准库，不能借常量包反向导入实现或第三方 SDK。
	if source == "constants" {
		return strings.Contains(strings.Split(path, "/")[0], ".")
	}
	// 核心不得反向依赖上层 Harness，示例也不能把它重新带回主模块依赖图。
	const harnessModule = "github.com/kart-io/goagent-harness"
	if path == harnessModule || strings.HasPrefix(path, harnessModule+"/") {
		return true
	}
	if !strings.HasPrefix(path, module) {
		return false
	}
	dep := strings.TrimPrefix(path, module)
	dest := strings.Split(dep, "/")[0]
	// execution 是宿主装配薄层，基础和执行机制不得反向依赖它。
	if dest == "execution" {
		switch source {
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability", "devtools", "harness", "multiagent":
			return true
		}
	}
	// multiagent 组合 workflow、builder 与 tools 的预置模式，基础与执行机制不得反向依赖它。
	if dest == "multiagent" {
		switch source {
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability", "devtools", "harness":
			return true
		}
	}
	// plugin 在 builder 之上组合能力，执行机制不得反向依赖它。
	if dest == "plugin" {
		switch source {
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability", "multiagent":
			return true
		}
	}
	if dest == "examples" && source != "examples" {
		return true
	}
	switch source {
	case "interfaces", "errors":
		return dep != "constants"
	case "codec":
		// codec 及其子包是统一的格式转换入口，只依赖标准库、格式库与彼此，任何包都能导入它而不成环。
		return dest != "codec"
	case "utils":
		// errors 只可能依赖常量叶子层，utils 依赖它不会成环。
		return dest != "utils" && dep != "errors" && dest != "codec" && dep != "constants"
	case "cache":
		return dest != "cache" && dest != "codec" && dep != "constants" && dep != "errors"
	case "core":
		return dest == "tools" || dest == "middleware" || dest == "parsers" || dest == "builder"
	case "builder", "parsers":
		return dest == "middleware"
	}
	return false
}

// formatLibs 是只允许 codec 及其子包导入的格式库（含测试，含 JSON/YAML/TOML/XML 的常见第三方实现与 mapstructure）：
// 数据格式转换统一经 codec/json、codec/yaml 与 codec，其余代码通过它们的类型、选项与函数拿到结果。
var formatLibs = []string{
	"encoding/json", "encoding/xml",
	"go.yaml.in/yaml/", "gopkg.in/yaml.", "sigs.k8s.io/yaml", "github.com/goccy/go-yaml",
	"github.com/go-viper/mapstructure", "github.com/mitchellh/mapstructure",
	"github.com/bytedance/sonic", "github.com/json-iterator/go", "github.com/goccy/go-json", "github.com/mailru/easyjson",
	"github.com/tidwall/gjson", "github.com/tidwall/sjson", "github.com/buger/jsonparser",
	"github.com/pelletier/go-toml", "github.com/BurntSushi/toml",
}

func inspect(fset *token.FileSet, file string, node *ast.File) []finding {
	var out []finding
	for _, imp := range node.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if forbidden(file, path) {
			out = append(out, finding{fset.Position(imp.Pos()).Line, "disallowed import " + path})
		}
	}
	out = append(out, errorConstantFindings(fset, file, node)...)
	out = append(out, forwardingConstantFindings(fset, file, node)...)
	out = append(out, errorConstructionFindings(fset, file, node)...)
	out = append(out, formattedErrorFindings(fset, file, node)...)
	return append(out, formatFindings(fset, file, node)...)
}

// 外层包不得为中央常量建立重复入口；本地领域枚举、派生策略值不在此限制内。
func forwardingConstantFindings(fset *token.FileSet, file string, node *ast.File) []finding {
	source := strings.Split(filepath.ToSlash(file), "/")[0]
	if source == "constants" || source == "errors" {
		return nil
	}
	aliases := map[string]bool{}
	for _, imp := range node.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path != module+"constants" && path != module+"errors" {
			continue
		}
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		aliases[name] = true
	}
	var out []finding
	for _, decl := range node.Decls {
		g, ok := decl.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, spec := range g.Specs {
			for _, value := range spec.(*ast.ValueSpec).Values {
				for {
					p, ok := value.(*ast.ParenExpr)
					if !ok {
						break
					}
					value = p.X
				}
				s, ok := value.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				id, ok := s.X.(*ast.Ident)
				if ok && id.Obj == nil && aliases[id.Name] {
					out = append(out, finding{fset.Position(value.Pos()).Line, "共享常量和错误码应直接引用 constants/errors，禁止重复转发入口"})
				}
			}
		}
	}
	return out
}

// 独立格式库、通用环境接口、命令行与测试工具不依赖框架业务错误模型。
func allowsPlainErrors(file string) bool {
	file = filepath.ToSlash(file)
	if strings.HasSuffix(file, "_test.go") {
		return true
	}
	parts := strings.Split(file, "/")
	switch parts[0] {
	case "errors", "codec", "examples", "scripts", "testing", "cmd":
		return true
	case "harness":
		return len(parts) > 2 && parts[1] == "env"
	case "core":
		return strings.HasPrefix(file, "core/durable/storetest/")
	case "evaluation":
		for _, part := range parts[1 : len(parts)-1] {
			if part == "cmd" {
				return true
			}
		}
	}
	return false
}

// 格式化的新业务错误使用框架构造器，真实 %w 包装保留标准错误树。
func formattedErrorFindings(fset *token.FileSet, file string, node *ast.File) []finding {
	if allowsPlainErrors(file) {
		return nil
	}
	aliases := map[string]bool{}
	var out []finding
	for _, imp := range node.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path != "fmt" {
			continue
		}
		name := "fmt"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			out = append(out, finding{fset.Position(imp.Pos()).Line, "框架代码不得点导入 fmt；业务错误须使用框架错误构造器"})
		} else if name != "_" {
			aliases[name] = true
		}
	}
	calls := map[*ast.SelectorExpr]*ast.CallExpr{}
	ast.Inspect(node, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			fun := c.Fun
			for {
				p, ok := fun.(*ast.ParenExpr)
				if !ok {
					break
				}
				fun = p.X
			}
			if s, ok := fun.(*ast.SelectorExpr); ok {
				calls[s] = c
			}
		}
		return true
	})
	ast.Inspect(node, func(n ast.Node) bool {
		s, ok := n.(*ast.SelectorExpr)
		if !ok || s.Sel.Name != "Errorf" {
			return true
		}
		id, ok := s.X.(*ast.Ident)
		if !ok || id.Obj != nil || !aliases[id.Name] {
			return true
		}
		c := calls[s]
		allowed := false
		if c != nil && len(c.Args) > 0 {
			if format, ok := constantFormat(c.Args[0]); ok {
				allowed = hasWrappingVerb(format)
			}
		}
		if !allowed {
			out = append(out, finding{fset.Position(s.Pos()).Line, "无 %w 的业务错误须使用框架 Newf/专用构造器；fmt.Errorf 仅保留明确的错误链包装"})
		}
		return true
	})
	return out
}

func constantFormat(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		}
	case *ast.ParenExpr:
		return constantFormat(e.X)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			a, okA := constantFormat(e.X)
			b, okB := constantFormat(e.Y)
			return a + b, okA && okB
		}
	}
	return "", false
}

// 跳过转义百分号、标志、宽度、精度与参数索引，只把真正的 w 动词视为包装。
func hasWrappingVerb(format string) bool {
	index := func(i int) (int, bool, bool) {
		if i >= len(format) || format[i] != '[' {
			return i, false, true
		}
		j := i + 1
		for j < len(format) && format[j] >= '0' && format[j] <= '9' {
			j++
		}
		if j == i+1 || j >= len(format) || format[j] != ']' {
			return i, false, false
		}
		n, err := strconv.Atoi(format[i+1 : j])
		return j + 1, true, err == nil && n > 0
	}
	digits := func(i int) int {
		value := 0
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			// 与 fmt.parsenum 一样，在读取下一位前检查上限；超限会终止剩余格式解析。
			if value > 1_000_000 {
				return len(format)
			}
			value = value*10 + int(format[i]-'0')
			i++
		}
		return i
	}
	for i := 0; i < len(format); {
		if format[i] != '%' {
			i++
			continue
		}
		i++
		if i < len(format) && format[i] == '%' {
			i++
			continue
		}
		for i < len(format) && strings.ContainsRune("+-# 0", rune(format[i])) {
			i++
		}
		var afterIndex, valid bool
		i, afterIndex, valid = index(i)
		if i < len(format) && format[i] == '*' {
			i++
			afterIndex = false
		} else {
			start := i
			i = digits(i)
			if afterIndex && i > start {
				valid = false
			}
		}
		if i+1 < len(format) && format[i] == '.' {
			if afterIndex {
				valid = false
			}
			i++
			var ok bool
			i, afterIndex, ok = index(i)
			valid = valid && ok
			if i < len(format) && format[i] == '*' {
				i++
				afterIndex = false
			} else {
				i = digits(i)
			}
		}
		if !afterIndex {
			var ok bool
			i, afterIndex, ok = index(i)
			valid = valid && ok
		}
		if i < len(format) {
			if format[i] == 'w' && valid {
				return true
			}
			i++
		}
	}
	return false
}

// 只约束标准库构造入口；Is/As/Join 及框架 errors.New 不受影响。
func errorConstructionFindings(fset *token.FileSet, file string, node *ast.File) []finding {
	if allowsPlainErrors(file) {
		return nil
	}
	var out []finding
	aliases := map[string]bool{}
	for _, imp := range node.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path != "errors" {
			continue
		}
		name := "errors"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		switch name {
		case ".":
			out = append(out, finding{fset.Position(imp.Pos()).Line, "框架代码不得点导入标准库 errors；业务错误须由框架 errors 包构造"})
		case "_":
		default:
			aliases[name] = true
		}
	}
	ast.Inspect(node, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "New" {
			return true
		}
		name, ok := selector.X.(*ast.Ident)
		// 局部变量的同名方法不是导入包调用，不能按字符串名称误报。
		if ok && name.Obj == nil && aliases[name.Name] {
			out = append(out, finding{fset.Position(selector.Pos()).Line, "框架错误须由 github.com/kart-io/goagent/errors 构造，禁止直接使用标准库 errors.New"})
		}
		return true
	})
	return out
}

// 错误分类、提示及错误上下文字段由 errors 维护，避免再次混入协议常量。
func errorConstantFindings(fset *token.FileSet, file string, node *ast.File) []finding {
	if strings.Split(filepath.ToSlash(file), "/")[0] != "constants" {
		return nil
	}
	var out []finding
	for _, decl := range node.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				owned := strings.HasPrefix(name.Name, "Err") || strings.HasPrefix(name.Name, "Code")
				switch name.Name {
				case "StatusBadRequest", "StatusInvalidAPIKey", "StatusAPIKeyLacksPermissions", "StatusModelNotFound", "StatusEndpointNotFound", "StatusRateLimitExceeded", "StatusServerError":
					owned = true
				}
				if owned {
					out = append(out, finding{fset.Position(name.Pos()).Line, "错误常量须由 errors 包管理: " + name.Name})
				}
			}
		}
	}
	return out
}

func formatFindings(fset *token.FileSet, file string, node *ast.File) []finding {
	if strings.Split(filepath.ToSlash(file), "/")[0] == "codec" {
		return nil
	}
	var out []finding
	for _, imp := range node.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if formatLib(path) {
			out = append(out, finding{fset.Position(imp.Pos()).Line, "数据格式转换须经 github.com/kart-io/goagent/codec 及其子包，不得直接导入 " + path})
		}
	}
	return out
}

func formatLib(path string) bool {
	for _, lib := range formatLibs {
		if path == lib || strings.HasPrefix(path, strings.TrimSuffix(lib, "/")+"/") || strings.HasSuffix(lib, ".") && strings.HasPrefix(path, lib) {
			return true
		}
	}
	return false
}

func scan(root string) (int, error) {
	fset := token.NewFileSet()
	count := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		check := inspect
		if strings.HasSuffix(path, "_test.go") {
			check = formatFindings
		}
		for _, f := range check(fset, rel, node) {
			fmt.Printf("%s:%d: %s\n", path, f.line, f.message)
			count++
		}
		return nil
	})
	return count, err
}
func main() {
	root := flag.String("root", ".", "source root")
	mode := flag.String("mode", "imports", "imports")
	verbose := flag.Bool("verbose", false, "print checked root")
	flag.Parse()
	if *mode != "imports" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "expected -mode imports -root directory")
		os.Exit(2)
	}
	count, err := scan(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *verbose {
		fmt.Println("checked", *root)
	}
	fmt.Printf("%s: %d findings\n", *mode, count)
	if count > 0 {
		os.Exit(1)
	}
}
