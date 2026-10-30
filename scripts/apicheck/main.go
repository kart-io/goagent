// apicheck performs syntax-based import enforcement and migration inventory.
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
	if !strings.HasPrefix(path, module) {
		return false
	}
	dep := strings.TrimPrefix(path, module)
	dest := strings.Split(dep, "/")[0]
	source := strings.Split(filepath.ToSlash(file), "/")[0]
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
		return dest == "agents" || dest == "tools" || dest == "middleware" || dest == "parsers" || dest == "builder"
	case "builder":
		return dest == "middleware" || dest == "agents"
	case "tools":
		return dest == "agents"
	case "parsers":
		return dest == "agents" || dest == "middleware"
	}
	return false
}

func inspect(fset *token.FileSet, file string, node *ast.File, mode string) []finding {
	var out []finding
	aliases := map[string]string{}
	hasBuilder := false
	for _, imp := range node.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		alias := filepath.Base(path)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		aliases[alias] = path
		if path == module+"builder" {
			hasBuilder = true
		}
		if mode == "imports" && forbidden(file, path) {
			out = append(out, finding{fset.Position(imp.Pos()).Line, "disallowed import " + path})
		}
		if mode == "migration" && alias == "." && (path == module+"builder" || path == module+"interfaces") {
			out = append(out, finding{fset.Position(imp.Pos()).Line, "dot import needs manual migration review"})
		}
	}
	if mode != "migration" {
		return out
	}
	ast.Inspect(node, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && aliases[pkg.Name] == module+"interfaces" && (sel.Sel.Name == "Agent" || sel.Sel.Name == "Runnable") {
			out = append(out, finding{fset.Position(sel.Pos()).Line, "legacy interfaces." + sel.Sel.Name + ": review migration to core.Runnable"})
		}
		if hasBuilder && (sel.Sel.Name == "Execute" || sel.Sel.Name == "ExecuteWithTools") {
			out = append(out, finding{fset.Position(sel.Pos()).Line, "candidate " + sel.Sel.Name + ": review receiver and migrate Builder calls to AsRunnable().Invoke; check streaming, tools and budget semantics"})
		}
		return true
	})
	return out
}

func scan(root, mode string) (int, error) {
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
		for _, f := range inspect(fset, rel, node, mode) {
			fmt.Printf("%s:%d: %s\n", path, f.line, f.message)
			count++
		}
		return nil
	})
	return count, err
}
func main() {
	root := flag.String("root", ".", "source root")
	mode := flag.String("mode", "imports", "imports or migration")
	strict := flag.Bool("strict", false, "fail on migration candidates (imports always fail on violations)")
	verbose := flag.Bool("verbose", false, "print checked root")
	flag.Parse()
	if (*mode != "imports" && *mode != "migration") || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "expected -mode imports|migration -root directory")
		os.Exit(2)
	}
	count, err := scan(*root, *mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *verbose {
		fmt.Println("checked", *root)
	}
	fmt.Printf("%s: %d findings\n", *mode, count)
	if count > 0 && (*strict || *mode == "imports") {
		os.Exit(1)
	}
}
