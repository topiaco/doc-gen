package openapi

import (
	"bytes"
	"encoding/json"
	"github.com/topiaco/doc-gen/internal/ir"
	"github.com/topiaco/doc-gen/internal/schema"
	"strings"
	"testing"
)

// TestAssembleSourceResponses 验证源码响应分支、未知响应、参数与稳定输出
func TestAssembleSourceResponses(t *testing.T) {
	text := ir.TypeRef{Kind: ir.TypeBasic, Name: "string"}
	number := ir.TypeRef{Kind: ir.TypeBasic, Name: "int64"}
	operation := ir.Operation{Method: "GET", Path: "/items/{id}", Mode: ir.ModeManaged,
		Parameters: []ir.Parameter{{Name: "id", Location: ir.ParameterPath, Type: text}},
		Responses: []ir.Response{
			{Status: 200, Content: []ir.ResponseContent{{MediaType: "application/json", Type: &text}, {MediaType: "application/json", Type: &number}}},
			{Status: 206, Content: []ir.ResponseContent{{MediaType: "application/octet-stream", Binary: true}}},
			{Status: 0, Description: "无法从源码确定响应结构"},
		}}
	operations := []ir.Operation{operation, {Method: "GET", Path: "/existing", Mode: ir.ModeManual}}
	info := Info{Title: "Example", Version: "unversioned"}
	doc, err := Assemble(info, operations, schema.NewBuilder(nil))
	if err != nil {
		t.Fatal(err)
	}
	paths := decodeDocument(t, doc.JSON)["paths"].(map[string]any)
	if len(paths) != 1 {
		t.Fatalf("unexpected paths: %#v", paths)
	}
	get := paths[operation.Path].(map[string]any)["get"].(map[string]any)
	if !get["parameters"].([]any)[0].(map[string]any)["required"].(bool) {
		t.Fatal("path parameter must be required")
	}
	responses := get["responses"].(map[string]any)
	if responses["default"] == nil {
		t.Fatal("unknown status was not retained")
	}
	variants := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["anyOf"].([]any)
	if len(variants) != 2 {
		t.Fatalf("response variants: %#v", variants)
	}
	if !strings.Contains(string(doc.JSON), `"format": "binary"`) {
		t.Fatal("binary content missing")
	}
	repeated, err := Assemble(info, operations, schema.NewBuilder(nil))
	if err != nil || !bytes.Equal(doc.JSON, repeated.JSON) {
		t.Fatalf("unstable document: %v", err)
	}
	if _, err := Assemble(info, []ir.Operation{operation, operation}, schema.NewBuilder(nil)); err == nil {
		t.Fatal("duplicate route accepted")
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
		Info{Title: "Upload API", Version: "1"},
		[]ir.Operation{operation},
		schema.NewBuilder(nil),
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
	if got, want := OperationID("POST", "/widgets/{id}/save"), "post_widgets_id_save"; got != want {
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
