package schema

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/topiaco/doc-gen/internal/analyzer"
	"github.com/topiaco/doc-gen/internal/ir"
)

// TestBuildPaginationProductItem 验证泛型、嵌入、嵌套和字段元数据
func TestBuildPaginationProductItem(t *testing.T) {
	program := loadSchemaFixture(t)
	item := ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "fixture/schema", Name: "ProductItem"}
	pointerItem := ir.TypeRef{Kind: ir.TypePointer, Element: &item}
	page := ir.TypeRef{
		Kind:          ir.TypeNamed,
		PackagePath:   "fixture/schema",
		Name:          "Pagination",
		TypeArguments: []ir.TypeRef{pointerItem},
	}

	builder := NewBuilder(program)
	_, err := builder.Build(page, ir.SourceRef{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	product := builder.Components()["fixture_schema_ProductItem"].Schema
	if product == nil || product.Properties["id"].Schema == nil || len(product.AllOf) != 0 {
		t.Fatalf("embedded JSON fields were not flattened: %#v", product)
	}
	if got := *product.Properties["explicit"].Schema.Description; got != "显式说明" {
		t.Fatalf("description priority: %s", got)
	}
	if got := *product.Properties["occurred_at"].Schema.Format; got != "date-time" {
		t.Fatalf("time format: %s", got)
	}
	for _, name := range product.Required {
		if name == "optional" {
			t.Fatal("omitempty response property became required")
		}
	}
	encoded, err := json.Marshal(builder.Components()[ComponentName(page)])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"$ref":"#/components/schemas/fixture_schema_ProductItem"`) || !strings.Contains(string(encoded), `"nullable":true`) {
		t.Fatalf("generic pointer item: %s", encoded)
	}
}

// TestBuildRecursiveComponent 验证循环 Struct 通过 Component 引用终止
func TestBuildRecursiveComponent(t *testing.T) {
	program := loadSchemaFixture(t)
	node := ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "fixture/schema", Name: "Node"}
	builder := NewBuilder(program)
	_, err := builder.Build(node, ir.SourceRef{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	name := ComponentName(node)
	component, found := builder.Components()[name]
	if !found {
		t.Fatalf("component %s was not generated", name)
	}
	data, err := json.Marshal(component)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"$ref":"#/components/schemas/fixture_schema_Node"`) {
		t.Fatalf("recursive component = %s", data)
	}
}

// TestComponentNameDistinguishesGenericArguments 验证组件名保留泛型容器差异
func TestComponentNameDistinguishesGenericArguments(t *testing.T) {
	item := ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "fixture/schema", Name: "Item"}
	pointer := ir.TypeRef{Kind: ir.TypePointer, Element: &item}
	directPage := ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "fixture/schema", Name: "Pagination", TypeArguments: []ir.TypeRef{item}}
	pointerPage := ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "fixture/schema", Name: "Pagination", TypeArguments: []ir.TypeRef{pointer}}
	if ComponentName(directPage) == ComponentName(pointerPage) {
		t.Fatalf("component names must distinguish T and *T")
	}
}

// loadSchemaFixture 加载 Schema 测试模型
func loadSchemaFixture(t *testing.T) *analyzer.Program {
	t.Helper()
	root := filepath.Join("testdata", "models")
	program, err := analyzer.Load(context.Background(), root, []string{"./..."})
	if err != nil {
		t.Fatalf("analyzer.Load() error = %v", err)
	}
	return program
}
