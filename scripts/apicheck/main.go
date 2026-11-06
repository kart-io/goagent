// apicheck performs syntax-based import enforcement, including that format conversion goes through codec.
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
	source := strings.Split(filepath.ToSlash(file), "/")[0]
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
		return true
	case "codec":
		// codec 及其子包是统一的格式转换入口，只依赖标准库、格式库与彼此，任何包都能导入它而不成环。
		return dest != "codec"
	case "utils":
		// errors 已被上面的规则限定为不导入任何内部包的叶子包，utils 依赖它不会成环。
		return dest != "utils" && dep != "errors" && dest != "codec"
	case "cache":
		return dest != "cache" && dest != "codec"
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
	return append(out, formatFindings(fset, file, node)...)
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
