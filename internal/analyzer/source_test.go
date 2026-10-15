package analyzer

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/topiaco/doc-gen/internal/ir"
)

// TestSourceAnalysisFollowsRoutesHandlersAndWrappers 验证跨函数路由、Handler 和统一响应函数的分析结果
func TestSourceAnalysisFollowsRoutesHandlersAndWrappers(t *testing.T) {
	program, err := Load(context.Background(), filepath.Join(packageDir(t), "testdata", "source"), []string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	a := NewSourceAnalyzer(program)
	routes := a.Discover()
	if len(routes.Diagnostics) != 0 {
		t.Fatalf("discovery: %+v", routes.Diagnostics)
	}
	if len(routes.Operations) != 6 {
		t.Fatalf("routes: %+v", routes.Operations)
	}
	for i := range routes.Operations {
		routes.Operations[i].Mode = ir.ModeManaged
	}
	operations, err := a.Analyze(routes.Operations)
	if err != nil {
		t.Fatal(err)
	}
	get := findOperation(t, operations, "GET /api/orders/{id}")
	if get.Summary != "查询订单详情" || get.Description != "返回订单金额和当前支付状态" {
		t.Fatalf("description: %+v", get)
	}
	if len(get.Responses) != 1 || get.Responses[0].Status != 200 {
		t.Fatalf("responses: %+v", get.Responses)
	}
	response := get.Responses[0].Content[0].Type
	if response.Kind != ir.TypeStruct || len(response.Fields) != 1 || response.Fields[0].Type.Name != "Order" {
		t.Fatalf("wrapper: %+v", response)
	}
	if len(get.Parameters) != 1 || get.Parameters[0].Name != "id" {
		t.Fatalf("parameters: %+v", get.Parameters)
	}
	create := findOperation(t, operations, "POST /api/orders")
	if create.RequestBody == nil || create.RequestBody.Type.Name != "CreateOrderRequest" {
		t.Fatalf("body: %+v", create.RequestBody)
	}
	closure := findOperation(t, operations, "GET /api/closure")
	if closure.Summary != "闭包查询" || len(closure.Parameters) != 2 {
		t.Fatalf("closure: %+v", closure)
	}
	for _, p := range closure.Parameters {
		if p.Name == "page" && (!p.Required || p.Type.Name != "int") {
			t.Fatalf("bound query: %+v", p)
		}
		if p.Name == "limit" && p.Type.Name != "int" {
			t.Fatalf("converted query: %+v", p)
		}
	}
	fields := closure.Responses[0].Content[0].Type.Fields
	if len(fields) != 3 {
		t.Fatalf("gin.H fields: %+v", fields)
	}
	for _, field := range fields {
		if field.Name == "date" && field.Type.SchemaType != "string" {
			t.Fatalf("date: %+v", field)
		}
	}
	dynamic := findOperation(t, operations, "GET /api/dynamic")
	if dynamic.Responses[0].Status != 202 || dynamic.Responses[0].Content[0].Type.TypeArguments[0].Name != "Order" {
		t.Fatalf("annotation: %+v", dynamic.Responses)
	}
	missing := findOperation(t, operations, "GET /api/missing")
	if missing.Responses[0].Status != 0 {
		t.Fatalf("unknown: %+v", missing)
	}
	ignored := findOperation(t, operations, "GET /api/internal")
	if ignored.Selection != "ignore" {
		t.Fatalf("selection: %+v", ignored)
	}
	order := program.Package("fixture/source").Types.Scope().Lookup("Order")
	typ := TypeRefFromGoType(program, order.Type(), true)
	if len(typ.Fields) != 4 || !strings.Contains(typ.Fields[1].Description, "单位：分") {
		t.Fatalf("model comments: %+v", typ)
	}
}

// findOperation 按稳定接口键查找生成结果
func findOperation(t *testing.T, operations []ir.Operation, key string) ir.Operation {
	t.Helper()
	for _, operation := range operations {
		if operation.Key().String() == key {
			return operation
		}
	}
	t.Fatalf("missing operation %s", key)
	return ir.Operation{}
}
