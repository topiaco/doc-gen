package openapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
	"github.com/topiaco/doc-gen/internal/override"
	"github.com/topiaco/doc-gen/internal/schema"
)

// TestAssembleBuildsManagedDocument 验证核心 OpenAPI 结构和稳定输出
func TestAssembleBuildsManagedDocument(t *testing.T) {
	identifier := ir.TypeRef{Kind: ir.TypeBasic, Name: "int64"}
	name := ir.TypeRef{Kind: ir.TypeBasic, Name: "string"}
	body := ir.TypeRef{Kind: ir.TypeStruct, Fields: []ir.Field{
		{Name: "Name", JSONName: "name", Type: name, Required: true},
	}}
	item := ir.TypeRef{Kind: ir.TypeStruct, Fields: []ir.Field{
		{Name: "ID", JSONName: "id", Type: identifier, Required: true},
		{Name: "Name", JSONName: "name", Type: name},
	}}

	managed := ir.Operation{
		Method:      "POST",
		Path:        "/items/{id}",
		OperationID: "Item_Save",
		Tags:        []string{"Items", "Core", "Items"},
		Mode:        ir.ModeManaged,
		Parameters: []ir.Parameter{
			{Name: "trace", Location: ir.ParameterHeader, Type: name},
			{Name: "id", Location: ir.ParameterPath, Type: identifier},
			{Name: "status", Location: ir.ParameterQuery, Type: identifier},
		},
		RequestBody: &ir.RequestBody{ContentType: "application/json", Required: true, Type: body},
		Responses: []ir.Response{
			{
				Status:      200,
				Description: "Success",
				Content: []ir.ResponseContent{
					{MediaType: "application/json", Type: &item, Wrapper: "standardSuccess"},
					{MediaType: "application/json", Wrapper: "businessError"},
				},
			},
			{
				Status:      206,
				Description: "Partial Content",
				Content: []ir.ResponseContent{
					{MediaType: "application/json", Type: &item},
					{MediaType: "application/octet-stream", Binary: true},
				},
			},
		},
	}
	applied := override.Result{
		Operations: []ir.Operation{
			managed,
			{Method: "GET", Path: "/manual", Mode: ir.ModeManual},
			{Method: "GET", Path: "/exports/report.xlsx", Mode: ir.ModeIgnored},
		},
		OperationOverrides: map[string]config.OperationOverride{
			managed.Key().String(): {
				Parameters: []config.ParameterOverride{{
					Name: "status", Location: "query",
					Schema: config.SchemaPatch{Enum: []config.NamedEnumValue{{Value: int64(1), Name: "Active"}}},
				}},
				Response: &config.ResponseOverride{
					Status:  200,
					Data:    &config.SchemaPatch{Description: "Saved item"},
					Example: map[string]any{"code": 0, "data": map[string]any{"id": 1}, "message": "success"},
					Content: map[string]config.SchemaPatch{
						"text/plain": {Type: "string"},
					},
				},
			},
		},
	}
	project := config.Project{Name: "Test API", Version: "1.0.0", OpenAPIVersion: "3.0.3"}
	builder := schema.NewBuilder(nil, nil)

	document, err := Assemble(project, applied, builder)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Assemble(project, applied, builder)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(document.JSON, second.JSON) {
		t.Fatal("repeated assembly produced different JSON")
	}
	if !bytes.HasSuffix(document.JSON, []byte("\n")) {
		t.Fatal("OpenAPI JSON does not end with a newline")
	}

	decoded := decodeDocument(t, document.JSON)
	paths := decoded["paths"].(map[string]any)
	if len(paths) != 1 || paths["/manual"] != nil || paths["/exports/report.xlsx"] != nil {
		t.Fatalf("paths = %#v, want only managed path", paths)
	}
	operation := paths["/items/{id}"].(map[string]any)["post"].(map[string]any)
	if operation["operationId"] != "Item_Save" {
		t.Fatalf("operationId = %v", operation["operationId"])
	}
	tags := operation["tags"].([]any)
	if len(tags) != 2 || tags[0] != "Core" || tags[1] != "Items" {
		t.Fatalf("tags = %#v", tags)
	}
	parameters := operation["parameters"].([]any)
	assertParameter(t, parameters, "path", "id", true)
	assertParameter(t, parameters, "query", "status", false)
	assertParameter(t, parameters, "header", "trace", false)
	statusParameter := findParameter(t, parameters, "query", "status")
	statusSchema := statusParameter["schema"].(map[string]any)
	if values := statusSchema["enum"].([]any); len(values) != 1 || values[0] != float64(1) {
		t.Fatalf("status enum = %#v", values)
	}

	responses := operation["responses"].(map[string]any)
	successContent := responses["200"].(map[string]any)["content"].(map[string]any)
	if _, found := successContent["text/plain"]; !found {
		t.Fatalf("200 content = %#v, want text/plain override", successContent)
	}
	successSchema := successContent["application/json"].(map[string]any)["schema"].(map[string]any)
	successExample := successContent["application/json"].(map[string]any)["example"].(map[string]any)
	if successExample["code"] != float64(0) || successExample["data"].(map[string]any)["id"] != float64(1) {
		t.Fatalf("200 response example = %#v", successExample)
	}
	if variants := successSchema["oneOf"].([]any); len(variants) != 2 {
		t.Fatalf("200 response variants = %d, want 2", len(variants))
	} else if !hasDataDescription(variants, "Saved item") {
		t.Fatalf("200 response does not contain overridden data schema: %#v", variants)
	}
	partialContent := responses["206"].(map[string]any)["content"].(map[string]any)
	if len(partialContent) != 2 {
		t.Fatalf("206 content = %#v, want JSON and binary", partialContent)
	}
	binarySchema := partialContent["application/octet-stream"].(map[string]any)["schema"].(map[string]any)
	if binarySchema["type"] != "string" || binarySchema["format"] != "binary" {
		t.Fatalf("binary schema = %#v", binarySchema)
	}
	if err := Validate(document.JSON); err != nil {
		t.Fatal(err)
	}
}

// TestAssembleBuildsFormBody 验证表单参数转换为 OpenAPI 请求体
func TestAssembleBuildsFormBody(t *testing.T) {
	text := ir.TypeRef{Kind: ir.TypeBasic, Name: "string"}
	operation := ir.Operation{
		Method: "POST",
		Path:   "/upload",
		Mode:   ir.ModeManaged,
		Parameters: []ir.Parameter{
			{Name: "name", Location: ir.ParameterForm, Type: text, Required: true},
			{Name: "file", Location: ir.ParameterForm, Type: text, Format: "binary", Required: true},
		},
		Responses: []ir.Response{{Status: 204, Description: "No Content"}},
	}
	document, err := Assemble(
		config.Project{Name: "Upload API", Version: "1", OpenAPIVersion: "3.0.3"},
		override.Result{Operations: []ir.Operation{operation}},
		schema.NewBuilder(nil, nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.JSON), `"multipart/form-data"`) || !strings.Contains(string(document.JSON), `"format": "binary"`) {
		t.Fatalf("form request body was not generated: %s", document.JSON)
	}
}

// TestOperationID 验证默认 OperationID 规范化
func TestOperationID(t *testing.T) {
	if got, want := OperationID("POST", "/api/order/{id}/node-save"), "post_api_order_id_node_save"; got != want {
		t.Fatalf("OperationID() = %q, want %q", got, want)
	}
}

// decodeDocument 将测试文档解码为通用 JSON 结构
func decodeDocument(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// assertParameter 校验一个 OpenAPI 参数
func assertParameter(t *testing.T, parameters []any, location, name string, required bool) {
	t.Helper()
	parameter := findParameter(t, parameters, location, name)
	if parameter["required"] != required {
		t.Fatalf("%s %s required = %v, want %v", location, name, parameter["required"], required)
	}
}

// findParameter 查找一个 OpenAPI 参数
func findParameter(t *testing.T, parameters []any, location, name string) map[string]any {
	t.Helper()
	for _, candidate := range parameters {
		parameter := candidate.(map[string]any)
		if parameter["in"] == location && parameter["name"] == name {
			return parameter
		}
	}
	t.Fatalf("parameter %s %s was not found", location, name)
	return nil
}

// hasDataDescription 判断统一响应是否包含指定 data 说明
func hasDataDescription(variants []any, description string) bool {
	for _, candidate := range variants {
		variant := candidate.(map[string]any)
		properties, found := variant["properties"].(map[string]any)
		if !found {
			continue
		}
		data, found := properties["data"].(map[string]any)
		if found && data["description"] == description {
			return true
		}
	}
	return false
}
