// apicheck performs syntax-based import enforcement.
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
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability", "devtools", "harness":
			return true
		}
	}
	// plugin 在 builder 之上组合能力，执行机制不得反向依赖它。
	if dest == "plugin" {
		switch source {
		case "interfaces", "errors", "utils", "cache", "core", "builder", "tools", "parsers", "llm", "workflow", "memory", "store", "middleware", "observability":
			return true
		}
	}
	if dest == "examples" && source != "examples" {
		return true
	}
	switch source {
	case "interfaces", "errors":
		return true
	case "utils":
		// errors 已被上面的规则限定为不导入任何内部包的叶子包，utils 依赖它不会成环。
		return dest != "utils" && dep != "errors"
	case "cache":
		return dest != "cache" && dep != "utils/json"
	case "core":
		return dest == "tools" || dest == "middleware" || dest == "parsers" || dest == "builder"
	case "builder", "parsers":
		return dest == "middleware"
	}
	return false
}

func inspect(fset *token.FileSet, file string, node *ast.File) []finding {
	var out []finding
	for _, imp := range node.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if forbidden(file, path) {
			out = append(out, finding{fset.Position(imp.Pos()).Line, "disallowed import " + path})
		}
	}
	return out
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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
		for _, f := range inspect(fset, rel, node) {
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
