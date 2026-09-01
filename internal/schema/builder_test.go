package schema

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/topiaco/doc-gen/internal/analyzer"
	"github.com/topiaco/doc-gen/internal/ir"
)

// TestBuildPaginationProductItemGolden 验证泛型、嵌入、嵌套和字段元数据
func TestBuildPaginationProductItemGolden(t *testing.T) {
	program := loadSchemaFixture(t)
	item := ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "fixture/schema", Name: "ProductItem"}
	pointerItem := ir.TypeRef{Kind: ir.TypePointer, Element: &item}
	page := ir.TypeRef{
		Kind:          ir.TypeNamed,
		PackagePath:   "fixture/schema",
		Name:          "Pagination",
		TypeArguments: []ir.TypeRef{pointerItem},
	}

	result, err := NewBuilder(program, nil).Build(page, ir.SourceRef{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	actual, err := json.MarshalIndent(struct {
		Schema     any `json:"schema"`
		Components any `json:"components"`
	}{Schema: result.Schema, Components: result.Components}, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	expected, err := os.ReadFile(filepath.Join("testdata", "pagination_product_item.golden.json"))
	if err != nil {
		t.Fatalf("read golden file: %v\nactual:\n%s", err, actual)
	}
	if strings.TrimSpace(string(actual)) != strings.TrimSpace(string(expected)) {
		t.Fatalf("schema golden mismatch\nactual:\n%s\nexpected:\n%s", actual, expected)
	}
}

// TestBuildRecursiveComponent 验证循环 Struct 通过 Component 引用终止
func TestBuildRecursiveComponent(t *testing.T) {
	program := loadSchemaFixture(t)
	node := ir.TypeRef{Kind: ir.TypeNamed, PackagePath: "fixture/schema", Name: "Node"}
	result, err := NewBuilder(program, nil).Build(node, ir.SourceRef{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	name := ComponentName(node)
	component, found := result.Components[name]
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
