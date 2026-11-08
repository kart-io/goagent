package main

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// retiredAPIEntries 记录已退役入口，禁止通过类型、函数或变量重新建立旧API。
var retiredAPIEntries = func() map[string]bool {
	entries := map[string][]string{
		"tools":            {"ToolCall"},
		"cache":            {"CacheEntry", "InMemoryCache", "NewInMemoryCache", "LRUCache", "NewLRUCache", "MultiTierCache", "NewMultiTierCache"},
		"tools/middleware": {"ToolMiddleware", "BaseToolMiddleware", "NewBaseToolMiddleware", "Chain", "createMiddlewareInvoker"},
		"core":             {"RetryPolicy", "BackoffPolicy"},
		"core/execution":   {"RuntimeConfig", "DefaultRuntimeConfig", "RuntimeMetrics", "NewRuntimeMetrics"},
		"core/durable":     {"operationVersionV1"},
	}
	streams := []string{"StreamingAgent", "StreamOutput", "LegacyStreamChunk", "ChunkType", "ChunkMetadata", "StreamOptions", "ChunkTransformFunc", "DefaultStreamOptions", "StreamStatus", "StreamState", "StreamWriter", "StreamController", "StreamConsumer", "StreamMultiplexer", "NewStreamChunk", "NewTextChunk", "NewProgressChunk", "NewErrorChunk"}
	entries["core"] = append(entries["core"], streams...)
	entries["core/execution"] = append(entries["core/execution"], streams...)
	for _, name := range []string{"ChunkTypeText", "ChunkTypeError", "ChunkTypeJSON", "ChunkTypeProgress", "ChunkTypeBinary", "ChunkTypeControl", "ChunkTypeStatus", "ChunkTypeMetadata", "StreamStateIdle", "StreamStateRunning", "StreamStatePaused", "StreamStateError", "StreamStateComplete", "StreamStateClosed"} {
		entries["core"] = append(entries["core"], name)
		entries["core/execution"] = append(entries["core/execution"], name)
	}
	out := map[string]bool{}
	for owner, names := range entries {
		for _, name := range names {
			out[owner+"/"+name] = true
		}
	}
	return out
}()

var retiredConfigFields = map[string]map[string]bool{
	"core/RunnableConfig":                           {"RetryPolicy": true},
	"cache/CacheConfig":                             {"Type": true, "MaxSize": true, "CleanupInterval": true},
	"core/checkpoint/DistributedCheckpointerConfig": {"FailbackDelay": true},
}

func retiredAPIFindings(fset *token.FileSet, file string, node *ast.File, types *constantTypeContext) []finding {
	owner := filepath.ToSlash(filepath.Dir(file))
	var out []finding
	reject := func(pos token.Pos, name string) {
		out = append(out, finding{fset.Position(pos).Line, "已退役的兼容或无执行接口不得重建：" + name})
	}
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for _, name := range s.Names {
						if retiredAPIEntries[owner+"/"+name.Name] || fixedConstructor(owner, name.Name) {
							reject(name.Pos(), name.Name)
						}
					}
				case *ast.TypeSpec:
					if retiredAPIEntries[owner+"/"+s.Name.Name] || fixedConstructor(owner, s.Name.Name) {
						reject(s.Pos(), s.Name.Name)
					}
					fields := retiredConfigFields[owner+"/"+s.Name.Name]
					for name := range fields {
						if retiredFieldExposed(s.Type, name, types, map[*ast.TypeSpec]bool{s: true}) {
							reject(s.Pos(), s.Name.Name+"."+name)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil {
				if retiredAPIEntries[owner+"/"+d.Name.Name] {
					reject(d.Pos(), d.Name.Name)
				}
				if owner == "core/execution" && d.Name.Name == "NewRuntimeManager" && d.Type.Params.NumFields() != 0 {
					reject(d.Pos(), "NewRuntimeManager 的无效配置参数")
				}
				if owner == "tools" && d.Name.Name == "WithMiddleware" && !typedMiddlewareSignature(d, node) {
					reject(d.Pos(), "WithMiddleware 必须只接收强类型 ToolMiddlewareFunc")
				}
			} else {
				receiver := receiverTypeName(d.Recv.List[0].Type)
				manager := receiverIs(d.Recv.List[0].Type, "RuntimeManager", types, map[*ast.TypeSpec]bool{})
				checkpointer := receiverIs(d.Recv.List[0].Type, "DistributedCheckpointer", types, map[*ast.TypeSpec]bool{})
				if (owner == "core/execution" && manager && d.Name.Name == "Metrics") || (owner == "core/checkpoint" && checkpointer && d.Name.Name == "tryFailback") {
					reject(d.Pos(), receiver+"."+d.Name.Name)
				}
			}
		}
	}
	// 历史版本只能出现在独立迁移组件；在线路径不得重新加入双版本分派。
	if owner == "core/durable" {
		ast.Inspect(node, func(n ast.Node) bool {
			literal, ok := n.(*ast.BasicLit)
			if ok && literal.Kind == token.STRING {
				value, _ := strconv.Unquote(literal.Value)
				if value == "goagent-operation-v1" {
					reject(literal.Pos(), "在线 operation-v1 读取")
				}
			}
			return true
		})
	}
	return out
}

// 固定装配入口只能是函数声明，不能改用变量或类型继续接收旧调用形状。
func fixedConstructor(owner, name string) bool {
	return (owner == "tools" && name == "WithMiddleware") || (owner == "core/execution" && name == "NewRuntimeManager")
}

// 只追踪本包结构体、底层定义与匿名嵌入；具名普通字段不向外提升配置项。
func retiredFieldExposed(expr ast.Expr, name string, types *constantTypeContext, seen map[*ast.TypeSpec]bool) bool {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return retiredFieldExposed(e.X, name, types, seen)
	case *ast.StarExpr:
		return retiredFieldExposed(e.X, name, types, seen)
	case *ast.IndexExpr:
		return retiredFieldExposed(e.X, name, types, seen)
	case *ast.IndexListExpr:
		return retiredFieldExposed(e.X, name, types, seen)
	case *ast.Ident:
		for _, declaration := range types.declarations[e.Name] {
			if seen[declaration.spec] {
				continue
			}
			seen[declaration.spec] = true
			found := retiredFieldExposed(declaration.spec.Type, name, types, seen)
			delete(seen, declaration.spec)
			if found {
				return true
			}
		}
	case *ast.StructType:
		for _, field := range e.Fields.List {
			for _, fieldName := range field.Names {
				if fieldName.Name == name {
					return true
				}
			}
			if len(field.Names) == 0 && retiredFieldExposed(field.Type, name, types, seen) {
				return true
			}
		}
	}
	return false
}

func receiverTypeName(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.Ident:
			return e.Name
		default:
			return ""
		}
	}
}

// 只有透明别名共享方法集；新定义类型不因底层相同被当作已退役接收者。
func receiverIs(expr ast.Expr, target string, types *constantTypeContext, seen map[*ast.TypeSpec]bool) bool {
	name := receiverTypeName(expr)
	if name == target {
		return true
	}
	for _, declaration := range types.declarations[name] {
		if !declaration.spec.Assign.IsValid() || seen[declaration.spec] {
			continue
		}
		seen[declaration.spec] = true
		found := receiverIs(declaration.spec.Type, target, types, seen)
		delete(seen, declaration.spec)
		if found {
			return true
		}
	}
	return false
}

func typedMiddlewareSignature(fn *ast.FuncDecl, file *ast.File) bool {
	if len(fn.Type.Params.List) != 2 {
		return false
	}
	variadic, ok := fn.Type.Params.List[1].Type.(*ast.Ellipsis)
	if !ok {
		return false
	}
	selector, ok := variadic.Elt.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "ToolMiddlewareFunc" {
		return false
	}
	alias, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == alias.Name && path == module+"tools/middleware" {
			return true
		}
	}
	return false
}
