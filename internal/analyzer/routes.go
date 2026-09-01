package analyzer

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/topiaco/doc-gen/internal/ir"
)

var ginRouteMethods = map[string]string{
	"GET":     "GET",
	"POST":    "POST",
	"PUT":     "PUT",
	"PATCH":   "PATCH",
	"DELETE":  "DELETE",
	"OPTIONS": "OPTIONS",
	"HEAD":    "HEAD",
}

// RouteDiscovery 保存已发现路由和阻断诊断
type RouteDiscovery struct {
	Operations  []ir.Operation
	Diagnostics []ir.Diagnostic
}

// DiscoverGinRoutes 从已加载源码中发现 Gin 路由
func DiscoverGinRoutes(program *Program) RouteDiscovery {
	result := RouteDiscovery{}
	for _, pkg := range program.Packages {
		for _, file := range pkg.Syntax {
			discoverFileRoutes(program, pkg, file, &result)
		}
	}

	sort.Slice(result.Operations, func(i, j int) bool {
		return result.Operations[i].Key().String() < result.Operations[j].Key().String()
	})
	sort.Slice(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Source.String() < result.Diagnostics[j].Source.String()
	})
	return result
}

// groupPrefix 表示 RouterGroup 已知或动态的完整前缀
type groupPrefix struct {
	path  string
	exact bool
}

// routeVisitor 在单个源码文件内跟踪 Group 赋值和路由注册
type routeVisitor struct {
	program *Program
	pkg     *packages.Package
	file    *ast.File
	groups  map[types.Object]groupPrefix
	result  *RouteDiscovery
}

// discoverFileRoutes 分析单个源码文件
func discoverFileRoutes(program *Program, pkg *packages.Package, file *ast.File, result *RouteDiscovery) {
	visitor := &routeVisitor{
		program: program,
		pkg:     pkg,
		file:    file,
		groups:  make(map[types.Object]groupPrefix),
		result:  result,
	}
	ast.Walk(visitor, file)
}

// Visit 按源码顺序记录 Group 并处理路由调用
func (v *routeVisitor) Visit(node ast.Node) ast.Visitor {
	switch node := node.(type) {
	case *ast.AssignStmt:
		v.recordAssignment(node.Lhs, node.Rhs)
	case *ast.ValueSpec:
		lhs := make([]ast.Expr, len(node.Names))
		for i, name := range node.Names {
			lhs[i] = name
		}
		v.recordAssignment(lhs, node.Values)
	case *ast.CallExpr:
		v.discoverRoute(node)
	}
	return v
}

// recordAssignment 保存变量对应的静态 Group 前缀
func (v *routeVisitor) recordAssignment(lhs, rhs []ast.Expr) {
	if len(lhs) != len(rhs) {
		return
	}
	for i, expression := range rhs {
		prefix, recognized := v.resolveGroupCall(expression)
		if !recognized {
			continue
		}
		identifier, ok := lhs[i].(*ast.Ident)
		if !ok {
			continue
		}
		if object := v.pkg.TypesInfo.ObjectOf(identifier); object != nil {
			v.groups[object] = prefix
		}
	}
}

// discoverRoute 将一个 Gin 路由注册转换为中间模型
func (v *routeVisitor) discoverRoute(call *ast.CallExpr) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	method, ok := ginRouteMethods[selector.Sel.Name]
	if !ok || len(call.Args) < 2 {
		return
	}
	prefix, recognized := v.resolveGroup(selector.X)
	if !recognized {
		return
	}
	if !prefix.exact {
		v.addRouteDiagnostic("route.dynamic_group_path", "Gin route group prefix is not a compile-time string constant", call.Pos())
		return
	}

	routePath, exact := constantString(v.pkg.TypesInfo, call.Args[0])
	if !exact {
		v.addRouteDiagnostic("route.dynamic_path", "Gin route path is not a compile-time string constant", call.Args[0].Pos())
		return
	}
	fullPath, err := ir.NormalizePath(joinRoutePath(prefix.path, routePath))
	if err != nil {
		v.addRouteDiagnostic("route.invalid_path", err.Error(), call.Args[0].Pos())
		return
	}

	v.result.Operations = append(v.result.Operations, ir.Operation{
		Method:     method,
		Path:       fullPath,
		Handler:    v.handlerRef(call.Args[len(call.Args)-1]),
		Summary:    v.trailingComment(call),
		Confidence: ir.ConfidenceExact,
		Source:     v.program.SourceRef(call.Pos()),
	})
}

// resolveGroupCall 解析 Group 调用形成的完整前缀
func (v *routeVisitor) resolveGroupCall(expression ast.Expr) (groupPrefix, bool) {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return groupPrefix{}, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Group" {
		return groupPrefix{}, false
	}
	parent, recognized := v.resolveGroup(selector.X)
	if !recognized {
		return groupPrefix{}, false
	}
	segment, exact := constantString(v.pkg.TypesInfo, call.Args[0])
	if !parent.exact || !exact {
		return groupPrefix{exact: false}, true
	}
	return groupPrefix{path: joinRoutePath(parent.path, segment), exact: true}, true
}

// resolveGroup 解析 Engine、RouterGroup 变量或链式 Group 调用
func (v *routeVisitor) resolveGroup(expression ast.Expr) (groupPrefix, bool) {
	if identifier, ok := expression.(*ast.Ident); ok {
		if prefix, found := v.groups[v.pkg.TypesInfo.ObjectOf(identifier)]; found {
			return prefix, true
		}
	}
	if prefix, recognized := v.resolveGroupCall(expression); recognized {
		return prefix, true
	}

	typeOf := v.pkg.TypesInfo.TypeOf(expression)
	if isGinType(typeOf, "Engine") {
		return groupPrefix{exact: true}, true
	}
	if isGinType(typeOf, "RouterGroup") {
		return groupPrefix{exact: false}, true
	}
	return groupPrefix{}, false
}

// constantString 读取编译期可确定的字符串表达式
func constantString(info *types.Info, expression ast.Expr) (string, bool) {
	value := info.Types[expression].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(value), true
}

// handlerRef 解析最后一个 Handler 表达式的 Go 符号
func (v *routeVisitor) handlerRef(expression ast.Expr) ir.SymbolRef {
	object := expressionObject(v.pkg.TypesInfo, expression)
	if object == nil {
		return ir.SymbolRef{Source: v.program.SourceRef(expression.Pos())}
	}
	ref := ir.SymbolRef{Name: object.Name(), Source: v.program.SourceRef(object.Pos())}
	if object.Pkg() != nil {
		ref.PackagePath = object.Pkg().Path()
	}
	return ref
}

// trailingComment 返回路由注册同行的说明注释
func (v *routeVisitor) trailingComment(call *ast.CallExpr) string {
	line := v.program.FileSet.PositionFor(call.End(), true).Line
	for _, comment := range v.file.Comments {
		if comment.Pos() > call.End() && v.program.FileSet.PositionFor(comment.Pos(), true).Line == line {
			return strings.TrimSpace(comment.Text())
		}
	}
	return ""
}

// addRouteDiagnostic 添加阻断路由发现的错误
func (v *routeVisitor) addRouteDiagnostic(code, message string, pos token.Pos) {
	v.result.Diagnostics = append(v.result.Diagnostics, ir.Diagnostic{
		Code:     code,
		Severity: ir.SeverityError,
		Message:  message,
		Source:   v.program.SourceRef(pos),
	})
}

// expressionObject 返回标识符或选择器引用的类型对象
func expressionObject(info *types.Info, expression ast.Expr) types.Object {
	switch expression := expression.(type) {
	case *ast.Ident:
		return info.ObjectOf(expression)
	case *ast.SelectorExpr:
		return info.ObjectOf(expression.Sel)
	case *ast.IndexExpr:
		return expressionObject(info, expression.X)
	case *ast.IndexListExpr:
		return expressionObject(info, expression.X)
	default:
		return nil
	}
}

// isGinType 判断类型是否为 Gin 指定具名类型
func isGinType(value types.Type, name string) bool {
	value = types.Unalias(value)
	if pointer, ok := value.(*types.Pointer); ok {
		value = types.Unalias(pointer.Elem())
	}
	named, ok := value.(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "github.com/gin-gonic/gin" && named.Obj().Name() == name
}

// joinRoutePath 拼接 Group 前缀和当前路由路径
func joinRoutePath(prefix, routePath string) string {
	if prefix == "" {
		return routePath
	}
	if routePath == "" {
		return prefix
	}
	return strings.TrimSuffix(prefix, "/") + "/" + strings.TrimPrefix(routePath, "/")
}
