package analyzer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
)

// TestAnalyzeJSONResponseShapesAndMerging 验证响应形状、状态码和同状态合并
func TestAnalyzeJSONResponseShapesAndMerging(t *testing.T) {
	fixtureRoot := filepath.Join(packageDir(t), "testdata", "responses")
	program, err := Load(context.Background(), fixtureRoot, []string{"./..."})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	discovery := DiscoverGinRoutes(program)
	resolutions := ResolveHandlers(program, discovery.Operations)
	analyzed := AnalyzeJSONResponses(program, resolutions, fixtureResponseFunctions())

	shapes := findResolution(t, analyzed, "GET /responses/shapes")
	response := findResponse(t, shapes.Operation.Responses, 200)
	if len(response.Content) != 6 {
		t.Fatalf("shape response content count = %d, want 6", len(response.Content))
	}
	for _, typeName := range []string{"*fixture/responses.Item", "[]*fixture/responses.Item", "*fixture/responses.Pagination[*fixture/responses.Item]", "uint", "", "map[string]any"} {
		findResponseContent(t, response.Content, "success", typeName)
	}
	if !hasDiagnostic(shapes.Operation.Diagnostics, "response.dynamic_data") {
		t.Fatalf("shape diagnostics = %#v", shapes.Operation.Diagnostics)
	}

	merge := findResolution(t, analyzed, "GET /responses/merge")
	merged := findResponse(t, merge.Operation.Responses, 200)
	if len(merged.Content) != 3 {
		t.Fatalf("merged response content = %#v", merged.Content)
	}
	findResponseContent(t, merged.Content, "success", "*fixture/responses.Item")
	findResponseContent(t, merged.Content, "error", "")
	findResponseContent(t, merged.Content, "business", "")

	statuses := findResolution(t, analyzed, "GET /responses/statuses")
	if len(statuses.Operation.Responses) != 2 || statuses.Operation.Responses[0].Status != 400 || statuses.Operation.Responses[1].Status != 500 {
		t.Fatalf("status responses = %#v", statuses.Operation.Responses)
	}

	dynamicStatus := findResolution(t, analyzed, "GET /responses/dynamic-status")
	if len(dynamicStatus.Operation.Responses) != 0 || !hasDiagnostic(dynamicStatus.Operation.Diagnostics, "response.dynamic_status") {
		t.Fatalf("dynamic status = %#v, diagnostics = %#v", dynamicStatus.Operation.Responses, dynamicStatus.Operation.Diagnostics)
	}
}

// fixtureResponseFunctions 返回测试用统一响应配置
func fixtureResponseFunctions() []config.ResponseFunction {
	statusArgument := 1
	dataArgument := 1
	return []config.ResponseFunction{
		{Symbol: "fixture/responses.Send", Kind: "json", Status: 200, DataArgument: &dataArgument, Wrapper: "success", DataTarget: true},
		{Symbol: "fixture/responses.SendStatus", Kind: "json", StatusArgument: &statusArgument, Wrapper: "error"},
		{Symbol: "fixture/responses.SendMessage", Kind: "json", Status: 200, Wrapper: "business"},
		{Symbol: "fixture/responses.SendBinary", Kind: "binary", Status: 200, MediaType: "application/octet-stream"},
	}
}

// assertResponseType 验证指定响应变体并返回内容
func assertResponseType(t *testing.T, result HandlerResolution, status int, wrapper, typeName string) ir.ResponseContent {
	t.Helper()
	response := findResponse(t, result.Operation.Responses, status)
	return findResponseContent(t, response.Content, wrapper, typeName)
}

// findResponse 返回指定状态码响应
func findResponse(t *testing.T, responses []ir.Response, status int) ir.Response {
	t.Helper()
	for _, response := range responses {
		if response.Status == status {
			return response
		}
	}
	t.Fatalf("response status %d was not found", status)
	return ir.Response{}
}

// findResponseContent 返回指定包装和数据类型的响应内容
func findResponseContent(t *testing.T, contents []ir.ResponseContent, wrapper, typeName string) ir.ResponseContent {
	t.Helper()
	for _, content := range contents {
		actualType := ""
		if content.Type != nil {
			actualType = content.Type.String()
		}
		if content.Wrapper == wrapper && actualType == typeName {
			return content
		}
	}
	t.Fatalf("response content %s %s was not found in %#v", wrapper, typeName, contents)
	return ir.ResponseContent{}
}

// hasDiagnostic 判断诊断列表是否包含指定代码
func hasDiagnostic(diagnostics []ir.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
