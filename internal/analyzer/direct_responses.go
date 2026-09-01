package analyzer

import (
	"go/ast"
	"net/http"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
)

// AnalyzeDirectResponses 分析 Gin 直接响应和配置化二进制 Helper
func AnalyzeDirectResponses(program *Program, resolutions []HandlerResolution, functions []config.ResponseFunction) []HandlerResolution {
	binaryFunctions := map[string]config.ResponseFunction{}
	for _, function := range functions {
		if function.Kind == "binary" {
			binaryFunctions[function.Symbol] = function
		}
	}

	results := make([]HandlerResolution, len(resolutions))
	copy(results, resolutions)
	for i := range results {
		if results[i].Declaration == nil || results[i].Diagnostic != nil {
			continue
		}
		analyzeHandlerDirectResponses(program, &results[i], binaryFunctions)
	}
	return results
}

// analyzeHandlerDirectResponses 分析单个 Handler 的直接响应调用
func analyzeHandlerDirectResponses(program *Program, result *HandlerResolution, binaryFunctions map[string]config.ResponseFunction) {
	info := result.Package.TypesInfo
	ast.Inspect(result.Declaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if function, found := configuredResponseFunction(info, call, binaryFunctions); found {
			analyzeBinaryHelper(program, result, call, function)
			return true
		}

		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !isGinType(info.TypeOf(selector.X), "Context") {
			return true
		}
		switch selector.Sel.Name {
		case "JSON":
			analyzeDirectJSON(program, result, call)
		case "Data":
			analyzeDirectData(program, result, call)
		case "File", "FileAttachment":
			analyzeDirectFile(program, result, call)
		case "Redirect":
			analyzeRedirect(program, result, call)
		}
		return true
	})
	sortOperationResponses(&result.Operation)
}

// analyzeBinaryHelper 添加配置化二进制响应
func analyzeBinaryHelper(program *Program, result *HandlerResolution, call *ast.CallExpr, function config.ResponseFunction) {
	status, diagnostic := responseStatus(program, result.Package.TypesInfo, call, function)
	if diagnostic != nil {
		result.Operation.Diagnostics = append(result.Operation.Diagnostics, *diagnostic)
		return
	}
	mergeOperationResponse(&result.Operation, ir.Response{
		Status:      status,
		Description: http.StatusText(status),
		Content: []ir.ResponseContent{{
			MediaType:  function.MediaType,
			Binary:     true,
			Confidence: ir.ConfidenceConfigured,
		}},
		Source: program.SourceRef(call.Pos()),
	})
}

// analyzeDirectJSON 添加 c.JSON 响应
func analyzeDirectJSON(program *Program, result *HandlerResolution, call *ast.CallExpr) {
	info := result.Package.TypesInfo
	status, diagnostic := responseStatusExpression(program, info, call.Args[0])
	if diagnostic != nil {
		result.Operation.Diagnostics = append(result.Operation.Diagnostics, *diagnostic)
		return
	}
	content := ir.ResponseContent{MediaType: "application/json", Confidence: ir.ConfidenceExact}
	if !isNilType(info.TypeOf(call.Args[1])) {
		typeRef := TypeRefFromGoType(program, info.TypeOf(call.Args[1]), false)
		content.Type = &typeRef
		if responseDataIsDynamic(typeRef) {
			content.Confidence = ir.ConfidenceUnknown
			result.Operation.Diagnostics = append(result.Operation.Diagnostics, dynamicResponseDataDiagnostic(program, call.Args[1]))
		} else if responseDataHasMap(typeRef) {
			content.Confidence = ir.ConfidenceUnknown
			result.Operation.Diagnostics = append(result.Operation.Diagnostics, *mapResponseKeysDiagnostic(program, call.Args[1]))
		}
	}
	mergeOperationResponse(&result.Operation, ir.Response{
		Status:      status,
		Description: http.StatusText(status),
		Content:     []ir.ResponseContent{content},
		Source:      program.SourceRef(call.Pos()),
	})
}

// analyzeDirectData 添加 c.Data 二进制响应
func analyzeDirectData(program *Program, result *HandlerResolution, call *ast.CallExpr) {
	info := result.Package.TypesInfo
	status, diagnostic := responseStatusExpression(program, info, call.Args[0])
	if diagnostic != nil {
		result.Operation.Diagnostics = append(result.Operation.Diagnostics, *diagnostic)
		return
	}
	mediaType, exact := constantString(info, call.Args[1])
	confidence := ir.ConfidenceExact
	if !exact {
		mediaType = "application/octet-stream"
		confidence = ir.ConfidenceUnknown
		result.Operation.Diagnostics = append(result.Operation.Diagnostics, ir.Diagnostic{
			Code:     "response.dynamic_media_type",
			Severity: ir.SeverityWarning,
			Message:  "response media type is not a compile-time string constant",
			Source:   program.SourceRef(call.Args[1].Pos()),
		})
	}
	mergeOperationResponse(&result.Operation, ir.Response{
		Status:      status,
		Description: http.StatusText(status),
		Content: []ir.ResponseContent{{
			MediaType:  mediaType,
			Binary:     true,
			Confidence: confidence,
		}},
		Source: program.SourceRef(call.Pos()),
	})
}

// analyzeDirectFile 添加 c.File 和 c.FileAttachment 响应
func analyzeDirectFile(program *Program, result *HandlerResolution, call *ast.CallExpr) {
	mergeOperationResponse(&result.Operation, ir.Response{
		Status:      http.StatusOK,
		Description: http.StatusText(http.StatusOK),
		Content: []ir.ResponseContent{{
			MediaType:  "application/octet-stream",
			Binary:     true,
			Confidence: ir.ConfidenceInferred,
		}},
		Source: program.SourceRef(call.Pos()),
	})
}

// analyzeRedirect 添加重定向状态和 Location
func analyzeRedirect(program *Program, result *HandlerResolution, call *ast.CallExpr) {
	info := result.Package.TypesInfo
	status, diagnostic := responseStatusExpression(program, info, call.Args[0])
	if diagnostic != nil {
		result.Operation.Diagnostics = append(result.Operation.Diagnostics, *diagnostic)
		return
	}
	location, exact := constantString(info, call.Args[1])
	confidence := ir.ConfidenceExact
	if !exact {
		confidence = ir.ConfidenceUnknown
		result.Operation.Diagnostics = append(result.Operation.Diagnostics, ir.Diagnostic{
			Code:     "response.dynamic_redirect",
			Severity: ir.SeverityWarning,
			Message:  "redirect location is not a compile-time string constant",
			Source:   program.SourceRef(call.Args[1].Pos()),
		})
	}
	mergeOperationResponse(&result.Operation, ir.Response{
		Status:             status,
		Description:        http.StatusText(status),
		RedirectLocation:   location,
		RedirectConfidence: confidence,
		Source:             program.SourceRef(call.Pos()),
	})
}

// dynamicResponseDataDiagnostic 创建动态响应数据诊断
func dynamicResponseDataDiagnostic(program *Program, expression ast.Expr) ir.Diagnostic {
	return ir.Diagnostic{
		Code:     "response.dynamic_data",
		Severity: ir.SeverityWarning,
		Message:  "response data contains values without a concrete static type",
		Source:   program.SourceRef(expression.Pos()),
	}
}
