package main

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// migratedConstantOwners 记录已归并的公开协议及默认策略，禁止按旧名字重新建立入口。
var migratedConstantOwners = map[string]string{
	"builder/inboxWriteTimeout":              "constants.DefaultInboxWriteTimeout",
	"builder/maxRetryAfter":                  "constants.MaxModelRetryAfter",
	"builder/modelResendAttempts":            "constants.DefaultModelResendAttempts",
	"builder/contextpolicy/ReadToolName":     "constants.ToolReadContextResult",
	"core/budget/DefaultMaxAgentCalls":       "constants.DefaultMaxAgentCalls",
	"core/budget/Unlimited":                  "constants.BudgetUnlimited",
	"core/durable/ContinuationKindInterrupt": "constants.ContinuationKindInterrupt",
	"core/durable/ReasonInvalidJSON":         "constants.InterruptReasonInvalidJSON",
	"core/durable/ReasonRejected":            "constants.InterruptReasonRejected",
	"core/durable/ReasonValidatePanic":       "constants.InterruptReasonValidatePanic",
	"core/durable/inboxSeenLimit":            "constants.DurableInboxSeenLimit",
	"core/durable/interruptAttempts":         "constants.DefaultInterruptAttempts",
	"core/durable/maxReasonRunes":            "constants.InterruptMaxReasonRunes",
	"core/middleware/defaultNumShards":       "constants.DefaultMiddlewareCacheShards",
	"core/session/TypeAssistantMessage":      "constants.SessionEventAssistantMessage",
	"core/session/TypeBoundarySnapshot":      "constants.SessionEventBoundarySnapshot",
	"core/session/TypeHistoryRewrite":        "constants.SessionEventHistoryRewrite",
	"core/session/TypeRewind":                "constants.SessionEventRewind",
	"core/session/TypeSystemMessage":         "constants.SessionEventSystemMessage",
	"core/session/TypeToolDefinitions":       "constants.SessionEventToolDefinitions",
	"core/session/TypeToolResult":            "constants.SessionEventToolResult",
	"core/session/TypeTurnEnd":               "constants.SessionEventTurnEnd",
	"core/session/TypeTurnStart":             "constants.SessionEventTurnStart",
	"core/session/TypeUserMessage":           "constants.SessionEventUserMessage",
	"llm/providers/OllamaBaseURL":            "constants.OllamaBaseURL",
	"multiagent/Finish":                      "constants.MultiagentFinish",
	"tools/defaultApprovalTimeout":           "constants.DefaultApprovalTimeout",
	"tools/resumableAttempts":                "constants.DefaultResumableToolAttempts",
	"tools/spill/ToolReadSpill":              "constants.ToolReadSpill",
	"tools/spill/defaultReadLimit":           "constants.DefaultSpillReadLimitLines",
	"tools/spill/maxLineRunes":               "constants.SpillMaxLineRunes",
	"tools/spill/previewRunes":               "constants.SpillPreviewRunes",
	"workflow/END":                           "constants.WorkflowEnd",
	"workflow/RejectApprovalMismatch":        "constants.WorkflowRejectApprovalMismatch",
	"workflow/RejectBeforeApprove":           "constants.WorkflowRejectBeforeApprove",
	"workflow/RejectResumeInput":             "constants.WorkflowRejectResumeInput",
	"workflow/START":                         "constants.WorkflowStart",
}

// sharedConstantFindings 约束包级共享默认值，不干预领域类型枚举、内部格式和算法参数。
func sharedConstantFindings(fset *token.FileSet, file string, node *ast.File, types *constantTypeContext) []finding {
	file = filepath.ToSlash(file)
	owner := filepath.ToSlash(filepath.Dir(file))
	if owner == "constants" || allowsPlainErrors(file) || strings.HasPrefix(file, "core/session/sessiontest/") {
		return nil
	}
	var out []finding
	for _, decl := range node.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || (group.Tok != token.CONST && group.Tok != token.VAR) {
			continue
		}
		for _, spec := range group.Specs {
			value := spec.(*ast.ValueSpec)
			for _, name := range value.Names {
				target, migrated := migratedConstantOwners[owner+"/"+name.Name]
				defaultPolicy := group.Tok == token.CONST && strings.HasPrefix(strings.ToLower(name.Name), "default") && !types.domainConstant(name.Name, map[*constantValueDeclaration]bool{})
				if !migrated && !defaultPolicy {
					continue
				}
				message := "共享默认参数应归属 constants，禁止在领域包重新声明"
				if migrated {
					message = "共享常量应直接引用 " + target + "，禁止重建原包入口"
				}
				out = append(out, finding{fset.Position(name.Pos()).Line, message})
			}
		}
	}
	return out
}

// constantTypeContext 只索引同目录、同包的生产声明，不加载依赖或要求测试夹具可完整类型检查。
type constantTypeContext struct {
	declarations map[string][]constantTypeDeclaration
	values       map[string][]*constantValueDeclaration
	imports      map[*ast.File]constantTypeImports
}

type constantTypeDeclaration struct {
	spec *ast.TypeSpec
	file *ast.File
}

type constantTypeImports struct {
	paths   map[string]string
	dotTime bool
}

type constantValueDeclaration struct {
	typeExpr, value ast.Expr
	file            *ast.File
}

func newConstantTypeContext(files ...*ast.File) *constantTypeContext {
	context := &constantTypeContext{
		declarations: map[string][]constantTypeDeclaration{},
		values:       map[string][]*constantValueDeclaration{},
		imports:      map[*ast.File]constantTypeImports{},
	}
	for _, file := range files {
		imports := constantTypeImports{paths: map[string]string{}}
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			alias := filepath.Base(path)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			imports.paths[alias] = path
			imports.dotTime = imports.dotTime || (alias == "." && path == "time")
		}
		context.imports[file] = imports
		for _, decl := range file.Decls {
			group, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			switch group.Tok {
			case token.TYPE:
				for _, item := range group.Specs {
					spec := item.(*ast.TypeSpec)
					context.declarations[spec.Name.Name] = append(context.declarations[spec.Name.Name], constantTypeDeclaration{spec: spec, file: file})
				}
			case token.CONST:
				context.addConstantDeclarations(group, file)
			}
		}
	}
	return context
}

func (context *constantTypeContext) addConstantDeclarations(group *ast.GenDecl, file *ast.File) {
	var typeExpr ast.Expr
	var values []ast.Expr
	for _, item := range group.Specs {
		spec := item.(*ast.ValueSpec)
		if len(spec.Values) > 0 {
			typeExpr, values = spec.Type, spec.Values
		}
		for i, name := range spec.Names {
			declaration := &constantValueDeclaration{typeExpr: typeExpr, file: file}
			if i < len(values) {
				declaration.value = values[i]
			}
			context.values[name.Name] = append(context.values[name.Name], declaration)
		}
	}
}

func constantPackageKey(file string, node *ast.File) string {
	return filepath.ToSlash(filepath.Dir(file)) + "\x00" + node.Name.Name
}

func (context *constantTypeContext) domainType(expr ast.Expr, file *ast.File, visiting map[*ast.TypeSpec]bool) bool {
	for {
		p, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = p.X
	}
	switch value := expr.(type) {
	case *ast.Ident:
		if declarations := context.declarations[value.Name]; len(declarations) > 0 {
			for _, declaration := range declarations {
				spec := declaration.spec
				if !spec.Assign.IsValid() {
					continue // 真正的定义类型保留领域身份，即便底层类型是 int 或 time.Duration。
				}
				if visiting[spec] {
					return false // 无效的循环别名不能成为豁免入口，也不能让扫描陷入递归。
				}
				visiting[spec] = true
				domain := context.domainType(spec.Type, declaration.file, visiting)
				delete(visiting, spec)
				if !domain {
					return false
				}
			}
			return true
		}
		switch value.Name {
		case "bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune", "float32", "float64", "complex64", "complex128":
			return false
		default:
			return value.Name != "Duration" || !context.imports[file].dotTime
		}
	case *ast.SelectorExpr:
		// Duration是有单位的默认参数，其他领域具名枚举保留类型归属。
		alias, ok := value.X.(*ast.Ident)
		return !ok || context.imports[file].paths[alias.Name] != "time" || value.Sel.Name != "Duration"
	case *ast.IndexExpr:
		return context.domainType(value.X, file, visiting)
	case *ast.IndexListExpr:
		return context.domainType(value.X, file, visiting)
	}
	return false
}

func (context *constantTypeContext) domainConstant(name string, visiting map[*constantValueDeclaration]bool) bool {
	declarations := context.values[name]
	if len(declarations) == 0 {
		return false
	}
	for _, declaration := range declarations {
		if visiting[declaration] {
			return false
		}
		visiting[declaration] = true
		domain := false
		if declaration.typeExpr != nil {
			// 显式类型优先，不能因原始值来自领域枚举就把 int/time.Duration 也当作领域类型。
			domain = context.domainType(declaration.typeExpr, declaration.file, map[*ast.TypeSpec]bool{})
		} else {
			domain = context.domainExpression(declaration.value, declaration.file, visiting)
		}
		delete(visiting, declaration)
		if !domain {
			return false
		}
	}
	return true
}

// domainExpression 只追踪本包可确定的绑定，不加载外部依赖，也不代替编译器检查常量合法性。
func (context *constantTypeContext) domainExpression(expr ast.Expr, file *ast.File, visiting map[*constantValueDeclaration]bool) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		return context.domainConstant(value.Name, visiting)
	case *ast.ParenExpr:
		return context.domainExpression(value.X, file, visiting)
	case *ast.CallExpr:
		return len(value.Args) == 1 && context.domainConversion(value.Fun, file)
	case *ast.UnaryExpr:
		return context.domainExpression(value.X, file, visiting)
	case *ast.BinaryExpr:
		switch value.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return false // 常量比较的结果是无类型 bool，不继承被比较的领域类型。
		case token.SHL, token.SHR:
			return context.domainExpression(value.X, file, visiting)
		default:
			return context.domainExpression(value.X, file, visiting) || context.domainExpression(value.Y, file, visiting)
		}
	}
	return false
}

func (context *constantTypeContext) domainConversion(expr ast.Expr, file *ast.File) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		if len(context.declarations[value.Name]) == 0 {
			return false // len/real 等内置函数及未知调用不能伪装成领域类型转换。
		}
	case *ast.ParenExpr:
		return context.domainConversion(value.X, file)
	case *ast.IndexExpr:
		return context.domainConversion(value.X, file)
	case *ast.IndexListExpr:
		return context.domainConversion(value.X, file)
	case *ast.SelectorExpr:
		alias, ok := value.X.(*ast.Ident)
		if !ok || context.imports[file].paths[alias.Name] == "" {
			return false
		}
	default:
		return false
	}
	return context.domainType(expr, file, map[*ast.TypeSpec]bool{})
}
