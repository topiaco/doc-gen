package override

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
	"github.com/topiaco/doc-gen/internal/schema"
)

// TestApplyValidOverrides 验证接口、参数、Body、响应和组件覆盖
func TestApplyValidOverrides(t *testing.T) {
	minimum := float64(1)
	required := true
	files := []config.OverrideFile{{
		Path: "overrides/valid.yaml",
		Document: config.OverrideDocument{
			Operations: map[string]config.OperationOverride{
				"GET /orders/{id}": {
					OperationID: "Order_Get",
					Description: "查询订单",
					Tags:        []string{"订单"},
					Parameters: []config.ParameterOverride{
						{
							Name: "id", Location: "path", Description: "订单 ID",
							Schema: config.SchemaPatch{Type: "integer", Minimum: &minimum},
						},
						{
							Name: "limit", Location: "query", Description: "返回数量", Required: &required,
							Schema: config.SchemaPatch{Type: "integer", Minimum: &minimum},
						},
					},
					Response: &config.ResponseOverride{
						Status: 200,
						Data:   &config.SchemaPatch{Ref: "#/components/schemas/OrderDetail"},
					},
				},
				"POST /orders": {
					Body: &config.SchemaPatch{Properties: map[string]config.SchemaPatch{
						"id":   {Description: "订单标识"},
						"name": {Description: "订单名称"},
					}},
					Response: &config.ResponseOverride{
						Status: 200,
						Content: map[string]config.SchemaPatch{
							"application/json": {Properties: map[string]config.SchemaPatch{
								"state": {Description: "订单状态"},
							}},
						},
					},
				},
			},
			Schemas: map[string]config.SchemaPatch{
				"OrderDetail": {
					Type:     "object",
					Required: []string{"state"},
					Properties: map[string]config.SchemaPatch{
						"state": {Ref: "#/components/schemas/OrderState"},
					},
				},
				"OrderState": {
					Type: "integer",
					Enum: []config.NamedEnumValue{
						{Value: 1, Name: "新建"},
						{Value: 2, Name: "完成", Description: "订单已完成"},
					},
				},
			},
		},
	}}

	operations := testOperations()
	operations[0].Diagnostics = []ir.Diagnostic{
		{Code: "response.dynamic_data", Severity: ir.SeverityWarning},
		{Code: "response.map_keys_unknown", Severity: ir.SeverityWarning},
	}
	result := Apply(operations, files, schema.NewBuilder(nil, nil))
	if result.HasErrors() || len(result.Diagnostics) != 0 {
		t.Fatalf("Apply() diagnostics = %#v", result.Diagnostics)
	}
	valid, warnings, errors := result.Counts()
	if valid != 4 || warnings != 0 || errors != 0 {
		t.Fatalf("counts = %d, %d, %d", valid, warnings, errors)
	}

	operation := findOperation(t, result.Operations, "GET /orders/{id}")
	if operation.OperationID != "Order_Get" || operation.Description != "查询订单" || len(operation.Tags) != 1 {
		t.Fatalf("operation metadata = %#v", operation)
	}
	limit := findParameter(t, operation, "query", "limit")
	if limit.Description != "返回数量" || !limit.Required || limit.Minimum == nil || *limit.Minimum != 1 || limit.Confidence != ir.ConfidenceOverride {
		t.Fatalf("limit parameter = %#v", limit)
	}
	id := findParameter(t, operation, "path", "id")
	if id.Type.Name != "integer" || id.Minimum == nil || *id.Minimum != 1 || id.Confidence != ir.ConfidenceOverride {
		t.Fatalf("id parameter = %#v", id)
	}
	if findOperation(t, result.Operations, "POST /orders").RequestBody.Confidence != ir.ConfidenceOverride {
		t.Fatal("body override confidence was not applied")
	}
	if len(operation.Diagnostics) != 0 {
		t.Fatalf("resolved operation diagnostics = %#v", operation.Diagnostics)
	}

	state, found := result.Schemas["OrderState"]
	if !found {
		t.Fatal("OrderState schema was not applied")
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"enum":[1,2]`) || !strings.Contains(string(data), `"x-apifox-enum"`) {
		t.Fatalf("OrderState schema = %s", data)
	}
}

// TestApplyReportsErrorsWarningsAndValidEntries 验证错误、warning 和有效项可区分
func TestApplyReportsErrorsWarningsAndValidEntries(t *testing.T) {
	files := []config.OverrideFile{
		{
			Path: "overrides/a.yaml",
			Document: config.OverrideDocument{
				Operations: map[string]config.OperationOverride{
					"GET /orders/{id}": {},
					"GET /files":       {Summary: "ignored"},
					"GET /manual":      {Summary: "manual"},
					"GET /missing":     {},
					"POST /orders": {
						Parameters: []config.ParameterOverride{
							{Name: "missing", Location: "query", Schema: config.SchemaPatch{}},
						},
						Body: &config.SchemaPatch{Properties: map[string]config.SchemaPatch{
							"name": {Type: "integer"},
						}},
						Response: &config.ResponseOverride{Status: 201},
					},
				},
				Schemas: map[string]config.SchemaPatch{
					"Duplicate": {Type: "object"},
					"BrokenRef": {Ref: "#/components/schemas/Missing", Type: "object"},
					"Valid":     {Type: "string"},
				},
			},
		},
		{
			Path: "overrides/b.yaml",
			Document: config.OverrideDocument{
				Operations: map[string]config.OperationOverride{"GET /orders/{id}": {}},
				Schemas:    map[string]config.SchemaPatch{"Duplicate": {Type: "string"}},
			},
		},
	}

	result := Apply(testOperations(), files, schema.NewBuilder(nil, nil))
	for _, code := range []string{
		"override.duplicate_operation",
		"override.duplicate_schema",
		"override.inactive_operation",
		"override.orphan_operation",
		"override.orphan_parameter",
		"override.incompatible_type",
		"override.orphan_response",
		"override.stale_ref",
	} {
		if !hasDiagnostic(result.Diagnostics, code) {
			t.Fatalf("diagnostics do not contain %s: %#v", code, result.Diagnostics)
		}
	}
	if _, found := result.OperationOverrides["GET /files"]; found {
		t.Fatal("ignored operation override must not apply")
	}
	if _, found := result.Schemas["Valid"]; !found {
		t.Fatal("valid schema must remain available")
	}
	valid, warnings, errors := result.Counts()
	if valid != 1 || warnings != 2 || errors < 6 {
		t.Fatalf("counts = %d, %d, %d", valid, warnings, errors)
	}
}

// TestApplyRejectsCrossFileDuplicates 验证跨文件重复定义均不生效
func TestApplyRejectsCrossFileDuplicates(t *testing.T) {
	files := []config.OverrideFile{
		{Path: "z.yaml", Document: config.OverrideDocument{Operations: map[string]config.OperationOverride{"GET /orders/{id}": {}}, Schemas: map[string]config.SchemaPatch{"Value": {Type: "string"}}}},
		{Path: "a.yaml", Document: config.OverrideDocument{Operations: map[string]config.OperationOverride{"GET /orders/{id}": {}}, Schemas: map[string]config.SchemaPatch{"Value": {Type: "string"}}}},
	}
	result := Apply(testOperations(), files, schema.NewBuilder(nil, nil))
	if !hasDiagnostic(result.Diagnostics, "override.duplicate_operation") || !hasDiagnostic(result.Diagnostics, "override.duplicate_schema") {
		t.Fatalf("duplicate diagnostics = %#v", result.Diagnostics)
	}
	if len(result.OperationOverrides) != 0 || len(result.Schemas) != 0 {
		t.Fatalf("duplicate definitions were applied = %#v, %#v", result.OperationOverrides, result.Schemas)
	}
}

// testOperations 返回 Override 校验测试接口
func testOperations() []ir.Operation {
	stringType := ir.TypeRef{Kind: ir.TypeBasic, Name: "string"}
	integerType := ir.TypeRef{Kind: ir.TypeBasic, Name: "int"}
	uintType := ir.TypeRef{Kind: ir.TypeBasic, Name: "uint"}
	orderType := ir.TypeRef{Kind: ir.TypeStruct, Fields: []ir.Field{
		{Name: "ID", JSONName: "id", Type: uintType, Confidence: ir.ConfidenceExact},
		{Name: "State", JSONName: "state", Type: integerType, Confidence: ir.ConfidenceExact},
	}}
	bodyType := ir.TypeRef{Kind: ir.TypeStruct, Fields: []ir.Field{
		{Name: "Base", Embedded: true, Type: ir.TypeRef{Kind: ir.TypeStruct, Fields: []ir.Field{
			{Name: "ID", JSONName: "id", Type: uintType, Confidence: ir.ConfidenceExact},
		}}, Confidence: ir.ConfidenceExact},
		{Name: "Name", JSONName: "name", Type: stringType, Confidence: ir.ConfidenceExact},
	}}
	return []ir.Operation{
		{
			Method: "GET", Path: "/orders/{id}", Mode: ir.ModeManaged,
			Parameters: []ir.Parameter{
				{Name: "id", Location: ir.ParameterPath, Required: true, Type: stringType, Confidence: ir.ConfidenceExact},
				{Name: "limit", Location: ir.ParameterQuery, Type: integerType, Confidence: ir.ConfidenceExact},
			},
			Responses: []ir.Response{{Status: 200, Content: []ir.ResponseContent{{MediaType: "application/json", Wrapper: "envelope", DataTarget: true, Type: &orderType}}}},
		},
		{
			Method: "POST", Path: "/orders", Mode: ir.ModeManaged,
			RequestBody: &ir.RequestBody{ContentType: "application/json", Required: true, Type: bodyType, Confidence: ir.ConfidenceExact},
			Responses:   []ir.Response{{Status: 200, Content: []ir.ResponseContent{{MediaType: "application/json", Wrapper: "envelope", DataTarget: true, Type: &orderType}}}},
		},
		{Method: "GET", Path: "/files", Mode: ir.ModeIgnored},
		{Method: "GET", Path: "/manual", Mode: ir.ModeManual},
	}
}

// findOperation 返回指定接口
func findOperation(t *testing.T, operations []ir.Operation, key string) ir.Operation {
	t.Helper()
	for _, operation := range operations {
		if operation.Key().String() == key {
			return operation
		}
	}
	t.Fatalf("operation %s was not found", key)
	return ir.Operation{}
}

// findParameter 返回指定参数
func findParameter(t *testing.T, operation ir.Operation, location, name string) ir.Parameter {
	t.Helper()
	for _, parameter := range operation.Parameters {
		if string(parameter.Location) == location && parameter.Name == name {
			return parameter
		}
	}
	t.Fatalf("parameter %s %s was not found", location, name)
	return ir.Parameter{}
}

// hasDiagnostic 判断诊断代码是否存在
func hasDiagnostic(diagnostics []ir.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
