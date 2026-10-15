package ir

import "testing"

// TestTypeRefString 验证复合类型和泛型类型的稳定表达式
func TestTypeRefString(t *testing.T) {
	stringType := TypeRef{Kind: TypeBasic, Name: "string"}
	itemType := TypeRef{
		Kind:        TypeNamed,
		PackagePath: "fixture/models",
		Name:        "ProductItem",
	}
	pointerItemType := TypeRef{Kind: TypePointer, Element: &itemType}

	tests := []struct {
		name    string
		typeRef TypeRef
		want    string
	}{
		{
			name: "generic pointer",
			typeRef: TypeRef{
				Kind: TypePointer,
				Element: &TypeRef{
					Kind:          TypeNamed,
					PackagePath:   "fixture/models",
					Name:          "Pagination",
					TypeArguments: []TypeRef{pointerItemType},
				},
			},
			want: "*fixture/models.Pagination[*fixture/models.ProductItem]",
		},
		{
			name: "slice",
			typeRef: TypeRef{
				Kind:    TypeSlice,
				Element: &itemType,
			},
			want: "[]fixture/models.ProductItem",
		},
		{
			name: "map",
			typeRef: TypeRef{
				Kind:    TypeMap,
				Key:     &stringType,
				Element: &pointerItemType,
			},
			want: "map[string]*fixture/models.ProductItem",
		},
		{
			name: "array",
			typeRef: TypeRef{
				Kind:    TypeArray,
				Length:  3,
				Element: &stringType,
			},
			want: "[3]string",
		},
		{
			name: "anonymous struct",
			typeRef: TypeRef{
				Kind: TypeStruct,
				Fields: []Field{
					{Name: "ID", JSONName: "id", Type: stringType},
					{Name: "Items", Type: TypeRef{Kind: TypeSlice, Element: &itemType}},
				},
			},
			want: "struct{id string;Items []fixture/models.ProductItem}",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.typeRef.String(); got != test.want {
				t.Fatalf("TypeRef.String() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestSourceAndSymbolString 验证源码位置和符号的展示格式
func TestSourceAndSymbolString(t *testing.T) {
	source := SourceRef{File: "handler/item.go", Line: 42, Column: 7}
	if got := source.String(); got != "handler/item.go:42:7" {
		t.Fatalf("SourceRef.String() = %q", got)
	}

	symbol := SymbolRef{PackagePath: "fixture/handlers", Name: "CatalogList", Source: source}
	if got := symbol.String(); got != "fixture/handlers.CatalogList" {
		t.Fatalf("SymbolRef.String() = %q", got)
	}
}

// TestOperationKey 验证 Operation 返回由 HTTP 方法和路径组成的接口标识
func TestOperationKey(t *testing.T) {
	operation := Operation{Method: "POST", Path: "/items"}
	if got := operation.Key().String(); got != "POST /items" {
		t.Fatalf("Operation.Key() = %q", got)
	}
}
