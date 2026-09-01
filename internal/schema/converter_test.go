package schema

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
)

// TestConvertBasicTypes 验证每类基础类型映射
func TestConvertBasicTypes(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		want     map[string]any
	}{
		{name: "string", typeName: "string", want: map[string]any{"type": "string"}},
		{name: "boolean", typeName: "bool", want: map[string]any{"type": "boolean"}},
		{name: "int", typeName: "int", want: map[string]any{"type": "integer", "format": "int32"}},
		{name: "int32", typeName: "int32", want: map[string]any{"type": "integer", "format": "int32"}},
		{name: "int64", typeName: "int64", want: map[string]any{"type": "integer", "format": "int64"}},
		{name: "uint", typeName: "uint", want: map[string]any{"type": "integer", "minimum": float64(0)}},
		{name: "uint64", typeName: "uint64", want: map[string]any{"type": "integer", "minimum": float64(0)}},
		{name: "float32", typeName: "float32", want: map[string]any{"type": "number", "format": "float"}},
		{name: "float64", typeName: "float64", want: map[string]any{"type": "number", "format": "double"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Convert(ir.TypeRef{Kind: ir.TypeBasic, Name: test.typeName}, nil, ir.SourceRef{})
			if err != nil {
				t.Fatalf("Convert() error = %v", err)
			}
			assertSchemaJSON(t, result, test.want)
			if result.Confidence != ir.ConfidenceExact || len(result.Diagnostics) != 0 {
				t.Fatalf("conversion result = %#v", result)
			}
		})
	}
}

// TestConvertTimePointerAndContainers 验证时间、nullable 和递归容器
func TestConvertTimePointerAndContainers(t *testing.T) {
	int64Type := ir.TypeRef{Kind: ir.TypeBasic, Name: "int64"}
	mapType := ir.TypeRef{
		Kind:    ir.TypeMap,
		Key:     &ir.TypeRef{Kind: ir.TypeBasic, Name: "string"},
		Element: &int64Type,
	}
	sliceType := ir.TypeRef{Kind: ir.TypeSlice, Element: &mapType}
	pointerType := ir.TypeRef{Kind: ir.TypePointer, Element: &sliceType}

	result, err := Convert(pointerType, nil, ir.SourceRef{})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	assertSchemaJSON(t, result, map[string]any{
		"type":     "array",
		"nullable": true,
		"items": map[string]any{
			"type": "object",
			"additionalProperties": map[string]any{
				"type":   "integer",
				"format": "int64",
			},
		},
	})

	timeResult, err := Convert(ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "time", Name: "Time"}, nil, ir.SourceRef{})
	if err != nil {
		t.Fatalf("Convert(time.Time) error = %v", err)
	}
	assertSchemaJSON(t, timeResult, map[string]any{"type": "string", "format": "date-time"})

	arrayResult, err := Convert(ir.TypeRef{Kind: ir.TypeArray, Length: 3, Element: &int64Type}, nil, ir.SourceRef{})
	if err != nil {
		t.Fatalf("Convert(array) error = %v", err)
	}
	assertSchemaJSON(t, arrayResult, map[string]any{
		"type":     "array",
		"minItems": float64(3),
		"maxItems": float64(3),
		"items":    map[string]any{"type": "integer", "format": "int64"},
	})
}

// TestConvertFreeFormAndSpecialMappings 验证自由 Schema、动态 Map 和特殊类型
func TestConvertFreeFormAndSpecialMappings(t *testing.T) {
	source := ir.SourceRef{File: "model/example.go", Line: 12}
	anyType := ir.TypeRef{Kind: ir.TypeInterface, Name: "any"}
	anyResult, err := Convert(anyType, nil, source)
	if err != nil {
		t.Fatalf("Convert(any) error = %v", err)
	}
	assertSchemaJSON(t, anyResult, map[string]any{})
	assertDiagnostic(t, anyResult, "schema.free_form", source)
	if anyResult.Confidence != ir.ConfidenceUnknown {
		t.Fatalf("any confidence = %s", anyResult.Confidence)
	}

	dynamicMap := ir.TypeRef{
		Kind:    ir.TypeMap,
		Key:     &ir.TypeRef{Kind: ir.TypeBasic, Name: "string"},
		Element: &anyType,
	}
	mapResult, err := Convert(dynamicMap, nil, source)
	if err != nil {
		t.Fatalf("Convert(map[string]any) error = %v", err)
	}
	assertSchemaJSON(t, mapResult, map[string]any{
		"type":                 "object",
		"additionalProperties": map[string]any{},
	})
	assertDiagnostic(t, mapResult, "schema.dynamic_map", source)
	if mapResult.Confidence != ir.ConfidenceUnknown {
		t.Fatalf("dynamic map confidence = %s", mapResult.Confidence)
	}

	mappings := map[string]config.SchemaMapping{
		"gorm.io/datatypes.JSON": {Schema: map[string]any{}, Diagnostic: true},
		"example.UUID":           {Schema: map[string]any{"type": "string", "format": "uuid"}},
	}
	jsonResult, err := Convert(ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "gorm.io/datatypes", Name: "JSON"}, mappings, source)
	if err != nil {
		t.Fatalf("Convert(datatypes.JSON) error = %v", err)
	}
	assertSchemaJSON(t, jsonResult, map[string]any{})
	assertDiagnostic(t, jsonResult, "schema.configured_mapping", source)
	if jsonResult.Confidence != ir.ConfidenceConfigured {
		t.Fatalf("configured confidence = %s", jsonResult.Confidence)
	}

	uuidResult, err := Convert(ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "example", Name: "UUID"}, mappings, source)
	if err != nil {
		t.Fatalf("Convert(UUID) error = %v", err)
	}
	assertSchemaJSON(t, uuidResult, map[string]any{"type": "string", "format": "uuid"})
	if len(uuidResult.Diagnostics) != 0 || uuidResult.Confidence != ir.ConfidenceConfigured {
		t.Fatalf("UUID result = %#v", uuidResult)
	}
}

// assertSchemaJSON 验证 Schema 序列化结构
func assertSchemaJSON(t *testing.T, result Result, want map[string]any) {
	t.Helper()
	data, err := json.Marshal(result.Schema)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var actual map[string]any
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("schema = %#v, want %#v", actual, want)
	}
}

// assertDiagnostic 验证单条 Schema 诊断
func assertDiagnostic(t *testing.T, result Result, code string, source ir.SourceRef) {
	t.Helper()
	if len(result.Diagnostics) != 1 {
		t.Fatalf("conversion result = %#v", result)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != code || diagnostic.Severity != ir.SeverityWarning || diagnostic.Source != source {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}
