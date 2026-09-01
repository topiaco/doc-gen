package analyzer

import (
	"go/ast"
	"go/constant"
	"go/types"
	"net/http"
	"sort"
	"strconv"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
)

// AnalyzeJSONResponses 分析配置化统一 JSON 响应调用
func AnalyzeJSONResponses(program *Program, resolutions []HandlerResolution, functions []config.ResponseFunction) []HandlerResolution {
	functionBySymbol := make(map[string]config.ResponseFunction, len(functions))
	for _, function := range functions {
		if function.Kind == "json" {
			functionBySymbol[function.Symbol] = function
		}
	}

	results := make([]HandlerResolution, len(resolutions))
	copy(results, resolutions)
	for i := range results {
		if results[i].Declaration == nil || results[i].Diagnostic != nil {
			continue
		}
		analyzeHandlerJSONResponses(program, &results[i], functionBySymbol)
	}
	return results
}

// analyzeHandlerJSONResponses 合并单个 Handler 中相同状态码的响应变体
func analyzeHandlerJSONResponses(program *Program, result *HandlerResolution, functions map[string]config.ResponseFunction) {
	info := result.Package.TypesInfo
	ast.Inspect(result.Declaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		function, found := configuredResponseFunction(info, call, functions)
		if !found {
			return true
		}

		status, diagnostic := responseStatus(program, info, call, function)
		if diagnostic != nil {
			result.Operation.Diagnostics = append(result.Operation.Diagnostics, *diagnostic)
			return true
		}
		content, diagnostic := responseContent(program, info, call, function)
		if diagnostic != nil {
			result.Operation.Diagnostics = append(result.Operation.Diagnostics, *diagnostic)
		}

		mergeOperationResponse(&result.Operation, ir.Response{
			Status:      status,
			Description: http.StatusText(status),
			Content:     []ir.ResponseContent{content},
			Source:      program.SourceRef(call.Pos()),
		})
		return true
	})
	sortOperationResponses(&result.Operation)
}

// configuredResponseFunction 返回调用对应的响应函数配置
func configuredResponseFunction(info *types.Info, call *ast.CallExpr, functions map[string]config.ResponseFunction) (config.ResponseFunction, bool) {
	object := expressionObject(info, call.Fun)
	if object == nil || object.Pkg() == nil {
		return config.ResponseFunction{}, false
	}
	function, found := functions[object.Pkg().Path()+"."+object.Name()]
	return function, found
}

// responseStatus 读取固定状态码或状态码参数常量
func responseStatus(program *Program, info *types.Info, call *ast.CallExpr, function config.ResponseFunction) (int, *ir.Diagnostic) {
	if function.Status != 0 {
		return function.Status, nil
	}
	if function.StatusArgument == nil || *function.StatusArgument >= len(call.Args) {
		return 0, responseConfigurationDiagnostic(program, call, "response function has no readable status")
	}
	return responseStatusExpression(program, info, call.Args[*function.StatusArgument])
}

// responseStatusExpression 读取状态码表达式的整数常量
func responseStatusExpression(program *Program, info *types.Info, expression ast.Expr) (int, *ir.Diagnostic) {
	value := info.Types[expression].Value
	status, exact := int64(0), false
	if value != nil {
		status, exact = constant.Int64Val(value)
	}
	if !exact {
		return 0, &ir.Diagnostic{
			Code:     "response.dynamic_status",
			Severity: ir.SeverityWarning,
			Message:  "response status is not a compile-time integer constant",
			Source:   program.SourceRef(expression.Pos()),
		}
	}
	return int(status), nil
}

// responseContent 构造统一 JSON 响应内容
func responseContent(program *Program, info *types.Info, call *ast.CallExpr, function config.ResponseFunction) (ir.ResponseContent, *ir.Diagnostic) {
	content := ir.ResponseContent{
		MediaType:  "application/json",
		Wrapper:    function.Wrapper,
		DataTarget: function.DataTarget,
		Confidence: ir.ConfidenceConfigured,
	}
	if function.DataArgument == nil {
		return content, nil
	}
	if *function.DataArgument >= len(call.Args) {
		return content, responseConfigurationDiagnostic(program, call, "response data argument is outside the call arguments")
	}

	expression := call.Args[*function.DataArgument]
	if isNilType(info.TypeOf(expression)) {
		return content, nil
	}
	typeRef := TypeRefFromGoType(program, info.TypeOf(expression), false)
	content.Type = &typeRef
	if responseDataIsDynamic(typeRef) {
		content.Confidence = ir.ConfidenceUnknown
		return content, &ir.Diagnostic{
			Code:     "response.dynamic_data",
			Severity: ir.SeverityWarning,
			Message:  "response data contains values without a concrete static type",
			Source:   program.SourceRef(expression.Pos()),
		}
	}
	if responseDataHasMap(typeRef) {
		content.Confidence = ir.ConfidenceUnknown
		return content, mapResponseKeysDiagnostic(program, expression)
	}
	return content, nil
}

// responseConfigurationDiagnostic 创建响应函数配置诊断
func responseConfigurationDiagnostic(program *Program, call *ast.CallExpr, message string) *ir.Diagnostic {
	return &ir.Diagnostic{
		Code:     "response.invalid_configuration",
		Severity: ir.SeverityError,
		Message:  message,
		Source:   program.SourceRef(call.Pos()),
	}
}

// isNilType 判断表达式是否为无类型 nil
func isNilType(value types.Type) bool {
	value = types.Unalias(value)
	basic, ok := value.(*types.Basic)
	return ok && basic.Kind() == types.UntypedNil
}

// responseDataIsDynamic 判断响应数据是否包含动态类型
func responseDataIsDynamic(value ir.TypeRef) bool {
	switch value.Kind {
	case ir.TypeUnknown, ir.TypeInterface, ir.TypeParameter:
		return true
	case ir.TypePointer, ir.TypeSlice, ir.TypeArray:
		return value.Element == nil || responseDataIsDynamic(*value.Element)
	case ir.TypeMap:
		return value.Element == nil || responseDataIsDynamic(*value.Element)
	case ir.TypeNamed:
		for _, argument := range value.TypeArguments {
			if responseDataIsDynamic(argument) {
				return true
			}
		}
	case ir.TypeStruct:
		for _, field := range value.Fields {
			if responseDataIsDynamic(field.Type) {
				return true
			}
		}
	}
	return false
}

// responseDataHasMap 判断顶层响应或其集合元素是否为 Map
func responseDataHasMap(value ir.TypeRef) bool {
	switch value.Kind {
	case ir.TypeMap:
		return true
	case ir.TypePointer, ir.TypeSlice, ir.TypeArray:
		return value.Element != nil && responseDataHasMap(*value.Element)
	default:
		return false
	}
}

// mapResponseKeysDiagnostic 创建 Map 键集合未知诊断
func mapResponseKeysDiagnostic(program *Program, expression ast.Expr) *ir.Diagnostic {
	return &ir.Diagnostic{
		Code:     "response.map_keys_unknown",
		Severity: ir.SeverityWarning,
		Message:  "response map keys are not part of its static Go type; add an override for fixed keys",
		Source:   program.SourceRef(expression.Pos()),
	}
}

// appendResponseContent 去重追加同一状态码的响应变体
func appendResponseContent(response *ir.Response, content ir.ResponseContent) {
	key := responseContentKey(content)
	for _, existing := range response.Content {
		if responseContentKey(existing) == key {
			return
		}
	}
	response.Content = append(response.Content, content)
}

// mergeOperationResponse 合并相同状态码的内容和重定向信息
func mergeOperationResponse(operation *ir.Operation, incoming ir.Response) {
	for i := range operation.Responses {
		if operation.Responses[i].Status != incoming.Status {
			continue
		}
		for _, content := range incoming.Content {
			appendResponseContent(&operation.Responses[i], content)
		}
		if incoming.RedirectConfidence != "" {
			operation.Responses[i].RedirectLocation = incoming.RedirectLocation
			operation.Responses[i].RedirectConfidence = incoming.RedirectConfidence
		}
		return
	}
	operation.Responses = append(operation.Responses, incoming)
}

// sortOperationResponses 稳定排序状态码和内容变体
func sortOperationResponses(operation *ir.Operation) {
	sort.Slice(operation.Responses, func(i, j int) bool {
		return operation.Responses[i].Status < operation.Responses[j].Status
	})
	for i := range operation.Responses {
		sort.Slice(operation.Responses[i].Content, func(left, right int) bool {
			return responseContentKey(operation.Responses[i].Content[left]) < responseContentKey(operation.Responses[i].Content[right])
		})
	}
}

// responseContentKey 返回响应变体的稳定去重键
func responseContentKey(content ir.ResponseContent) string {
	typeName := ""
	if content.Type != nil {
		typeName = content.Type.String()
	}
	return content.MediaType + "\x00" + content.Wrapper + "\x00" + typeName + "\x00" + strconv.FormatBool(content.Binary) + "\x00" + strconv.FormatBool(content.DataTarget)
}
