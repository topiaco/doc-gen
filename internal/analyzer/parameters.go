package analyzer

import (
	"github.com/topiaco/doc-gen/internal/ir"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

var ginParameterReaders = map[string]struct {
	location ir.ParameterLocation
	format   string
}{
	"Param":        {location: ir.ParameterPath},
	"Query":        {location: ir.ParameterQuery},
	"DefaultQuery": {location: ir.ParameterQuery},
	"GetHeader":    {location: ir.ParameterHeader},
	"PostForm":     {location: ir.ParameterForm},
	"FormFile":     {location: ir.ParameterForm, format: "binary"},
}

// immediateRequiredCalls 仅在缺失分支返回 HTTP 客户端错误并结束 Handler 时标记参数必填
func immediateRequiredCalls(body *ast.BlockStmt, info *types.Info) map[*ast.CallExpr]bool {
	required := map[*ast.CallExpr]bool{}
	ast.Inspect(body, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i := 0; i+1 < len(block.List); i++ {
			assignment, ok := block.List[i].(*ast.AssignStmt)
			if !ok || len(assignment.Lhs) != len(assignment.Rhs) {
				continue
			}
			check, ok := block.List[i+1].(*ast.IfStmt)
			if !ok || !rejectsMissing(check.Body, info) {
				continue
			}
			for j, expression := range assignment.Rhs {
				call, ok := expression.(*ast.CallExpr)
				if ok && conditionChecksMissing(check.Cond, assignment.Lhs[j], info) {
					required[call] = true
				}
			}
		}
		return true
	})
	return required
}

// rejectsMissing 判断分支是否明确返回客户端错误并结束当前函数
func rejectsMissing(body *ast.BlockStmt, info *types.Info) bool {
	if len(body.List) == 0 {
		return false
	}
	if _, ok := body.List[len(body.List)-1].(*ast.ReturnStmt); !ok {
		return false
	}
	for _, statement := range body.List {
		expression, ok := statement.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expression.X.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			continue
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !isGinType(info.TypeOf(selector.X), "Context") {
			continue
		}
		switch selector.Sel.Name {
		case "JSON", "AbortWithStatusJSON", "Status", "AbortWithStatus", "String":
			value := info.Types[call.Args[0]].Value
			if value != nil && value.Kind() == constant.Int {
				status, ok := constant.Int64Val(value)
				if ok && status >= 400 && status < 500 {
					return true
				}
			}
		}
	}
	return false
}

// conditionChecksMissing 只接受直接判断缺失的条件，附带其他条件时不能确定参数始终必填
func conditionChecksMissing(condition, target ast.Expr, info *types.Info) bool {
	if parentheses, ok := condition.(*ast.ParenExpr); ok {
		return conditionChecksMissing(parentheses.X, target, info)
	}
	comparison, ok := condition.(*ast.BinaryExpr)
	return ok && comparison.Op == token.EQL && (missingComparison(comparison.X, comparison.Y, target, info) || missingComparison(comparison.Y, comparison.X, target, info))
}

// missingComparison 判断比较式一侧是否检查目标缺失
func missingComparison(left, right, target ast.Expr, info *types.Info) bool {
	return sameExpression(left, target, info) && isEmptyOrZero(right, info) ||
		isTargetLength(left, target, info) && isZero(right, info)
}

// isEmptyOrZero 判断常量是否为空字符串或零
func isEmptyOrZero(expression ast.Expr, info *types.Info) bool {
	value := info.Types[expression].Value
	if value == nil {
		return false
	}
	if value.Kind() == constant.String {
		return constant.StringVal(value) == ""
	}
	return value.Kind() != constant.Bool && constant.Sign(value) == 0
}

// isTargetLength 判断表达式是否为目标的 len 调用
func isTargetLength(expression, target ast.Expr, info *types.Info) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	identifier, ok := call.Fun.(*ast.Ident)
	return ok && identifier.Name == "len" && sameExpression(call.Args[0], target, info)
}

// isZero 判断表达式是否为数值零
func isZero(expression ast.Expr, info *types.Info) bool {
	value := info.Types[expression].Value
	return value != nil && value.Kind() != constant.String && value.Kind() != constant.Bool && constant.Sign(value) == 0
}

// sameExpression 比较缺失检查中的变量或字段表达式
func sameExpression(left, right ast.Expr, info *types.Info) bool {
	switch left := left.(type) {
	case *ast.Ident:
		right, ok := right.(*ast.Ident)
		return ok && info.ObjectOf(left) == info.ObjectOf(right)
	case *ast.SelectorExpr:
		right, ok := right.(*ast.SelectorExpr)
		return ok && left.Sel.Name == right.Sel.Name && sameExpression(left.X, right.X, info)
	default:
		return false
	}
}

// pathParameterNames 返回规范化路由中的 Path 参数名
func pathParameterNames(path string) []string {
	var names []string
	for _, segment := range strings.Split(path, "/") {
		if len(segment) > 2 && segment[0] == '{' && segment[len(segment)-1] == '}' {
			names = append(names, segment[1:len(segment)-1])
		}
	}
	return names
}
