package analyzer

import (
	"github.com/topiaco/doc-gen/internal/ir"
	"go/ast"
	"go/types"
	"strings"
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

// RouteDiscovery 保存已识别接口以及无法确定路由身份的位置
type RouteDiscovery struct {
	Operations  []ir.Operation
	Diagnostics []ir.Diagnostic
}

// groupPrefix 表示 RouterGroup 已知或动态的完整前缀
type groupPrefix struct {
	path  string
	exact bool
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
