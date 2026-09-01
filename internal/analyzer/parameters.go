package analyzer

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
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

// AnalyzeParameters 分析每个已定位 Handler 的参数读取
func AnalyzeParameters(program *Program, resolutions []HandlerResolution, helpers []config.ParameterHelper) []HandlerResolution {
	helperBySymbol := make(map[string]config.ParameterHelper, len(helpers))
	for _, helper := range helpers {
		helperBySymbol[helper.Symbol] = helper
	}

	results := make([]HandlerResolution, len(resolutions))
	copy(results, resolutions)
	for i := range results {
		if results[i].Declaration == nil || results[i].Diagnostic != nil {
			continue
		}
		analyzeHandlerParameters(program, &results[i], helperBySymbol)
	}
	return results
}

// analyzeHandlerParameters 分析单个 Handler 并补齐未读取的 Path 参数
func analyzeHandlerParameters(program *Program, result *HandlerResolution, helpers map[string]config.ParameterHelper) {
	info := result.Package.TypesInfo
	requiredCalls := immediateRequiredCalls(result.Declaration.Body, info)
	parameterIndexes := map[string]int{}

	ast.Inspect(result.Declaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		parameter, found, diagnostic := parameterFromCall(program, result, call, helpers)
		if diagnostic != nil {
			result.Operation.Diagnostics = append(result.Operation.Diagnostics, *diagnostic)
		}
		if !found {
			return true
		}
		if parameter.Location == ir.ParameterPath {
			parameter.Required = true
		} else if requiredCalls[call] {
			parameter.Required = true
			parameter.Confidence = ir.ConfidenceInferred
		}

		key := string(parameter.Location) + "\x00" + parameter.Name
		if index, exists := parameterIndexes[key]; exists {
			if parameter.Required {
				result.Operation.Parameters[index].Required = true
				result.Operation.Parameters[index].Confidence = parameter.Confidence
			}
			return true
		}
		parameterIndexes[key] = len(result.Operation.Parameters)
		result.Operation.Parameters = append(result.Operation.Parameters, parameter)
		return true
	})

	for _, name := range pathParameterNames(result.Operation.Path) {
		key := string(ir.ParameterPath) + "\x00" + name
		if _, found := parameterIndexes[key]; found {
			continue
		}
		result.Operation.Parameters = append(result.Operation.Parameters, ir.Parameter{
			Name:       name,
			Location:   ir.ParameterPath,
			Origin:     "route.path",
			Required:   true,
			Type:       ir.TypeRef{Kind: ir.TypeBasic, Name: "string"},
			Confidence: ir.ConfidenceUnknown,
			Source:     result.Operation.Source,
		})
		result.Operation.Diagnostics = append(result.Operation.Diagnostics, ir.Diagnostic{
			Code:     "parameter.path_not_read",
			Severity: ir.SeverityWarning,
			Message:  "route path parameter is not read by handler: " + name,
			Source:   result.Operation.Source,
		})
	}
}

// parameterFromCall 将直接读取或配置 Helper 调用转换为参数
func parameterFromCall(program *Program, result *HandlerResolution, call *ast.CallExpr, helpers map[string]config.ParameterHelper) (ir.Parameter, bool, *ir.Diagnostic) {
	info := result.Package.TypesInfo
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok && isGinType(info.TypeOf(selector.X), "Context") {
		reader, found := ginParameterReaders[selector.Sel.Name]
		if !found || len(call.Args) == 0 {
			return ir.Parameter{}, false, nil
		}
		name, exact := constantString(info, call.Args[0])
		if !exact {
			return ir.Parameter{}, false, dynamicParameterDiagnostic(program, call.Args[0])
		}
		return ir.Parameter{
			Name:       name,
			Location:   reader.location,
			Origin:     "gin.Context." + selector.Sel.Name,
			Type:       ir.TypeRef{Kind: ir.TypeBasic, Name: "string"},
			Format:     reader.format,
			Confidence: ir.ConfidenceExact,
			Source:     program.SourceRef(call.Pos()),
		}, true, nil
	}

	object := expressionObject(info, call.Fun)
	if object == nil || object.Pkg() == nil {
		return ir.Parameter{}, false, nil
	}
	symbol := object.Pkg().Path() + "." + object.Name()
	helper, found := helpers[symbol]
	if !found || helper.NameArgument >= len(call.Args) {
		return ir.Parameter{}, false, nil
	}
	name, exact := constantString(info, call.Args[helper.NameArgument])
	if !exact {
		return ir.Parameter{}, false, dynamicParameterDiagnostic(program, call.Args[helper.NameArgument])
	}
	return ir.Parameter{
		Name:       name,
		Location:   ir.ParameterLocation(helper.Location),
		Origin:     symbol,
		Required:   helper.Location == string(ir.ParameterPath),
		Type:       helperType(helper.Type),
		Format:     helper.Format,
		Collection: helper.CollectionFormat,
		Minimum:    helper.Minimum,
		Confidence: ir.ConfidenceConfigured,
		Source:     program.SourceRef(call.Pos()),
	}, true, nil
}

// helperType 将配置类型转换为中间类型
func helperType(name string) ir.TypeRef {
	if name == "array" {
		item := ir.TypeRef{Kind: ir.TypeBasic, Name: "string"}
		return ir.TypeRef{Kind: ir.TypeSlice, Element: &item}
	}
	return ir.TypeRef{Kind: ir.TypeBasic, Name: name}
}

// dynamicParameterDiagnostic 创建动态参数名诊断
func dynamicParameterDiagnostic(program *Program, expression ast.Expr) *ir.Diagnostic {
	return &ir.Diagnostic{
		Code:     "parameter.dynamic_name",
		Severity: ir.SeverityWarning,
		Message:  "parameter name is not a compile-time string constant",
		Source:   program.SourceRef(expression.Pos()),
	}
}

// immediateRequiredCalls 标记赋值后立即检查为空的参数读取
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
			if !ok {
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

// conditionChecksMissing 判断条件是否检查目标为空字符串或长度为零
func conditionChecksMissing(condition, target ast.Expr, info *types.Info) bool {
	found := false
	ast.Inspect(condition, func(node ast.Node) bool {
		comparison, ok := node.(*ast.BinaryExpr)
		if !ok || comparison.Op.String() != "==" {
			return true
		}
		if missingComparison(comparison.X, comparison.Y, target, info) || missingComparison(comparison.Y, comparison.X, target, info) {
			found = true
			return false
		}
		return true
	})
	return found
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
