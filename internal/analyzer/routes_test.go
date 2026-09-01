package analyzer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/topiaco/doc-gen/internal/ir"
)

// TestDiscoverNestedGroupsAndDynamicPaths 验证嵌套 Group、常量路径和动态路径诊断
func TestDiscoverNestedGroupsAndDynamicPaths(t *testing.T) {
	fixtureRoot := filepath.Join(packageDir(t), "testdata", "routes")
	program, err := Load(context.Background(), fixtureRoot, []string{"./..."})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	discovery := DiscoverGinRoutes(program)
	if len(discovery.Operations) != 3 {
		t.Fatalf("operation count = %d, want 3", len(discovery.Operations))
	}
	if len(discovery.Diagnostics) != 2 {
		t.Fatalf("diagnostic count = %d, want 2", len(discovery.Diagnostics))
	}

	getItem := findOperation(t, discovery.Operations, "GET /api/v1/items/{id}")
	if getItem.Handler.PackagePath != "fixture/routes" || getItem.Handler.Name != "GetItem" {
		t.Fatalf("GET handler = %#v", getItem.Handler)
	}
	if getItem.Summary != "查询项目" {
		t.Fatalf("GET summary = %q", getItem.Summary)
	}
	findOperation(t, discovery.Operations, "POST /api/v1/items")
	findOperation(t, discovery.Operations, "DELETE /admin/items/{id}")

	codes := map[string]int{}
	for _, diagnostic := range discovery.Diagnostics {
		codes[diagnostic.Code]++
		if diagnostic.Severity != "error" || diagnostic.Source.Line == 0 {
			t.Fatalf("dynamic path diagnostic = %#v", diagnostic)
		}
	}
	if codes["route.dynamic_path"] != 1 || codes["route.dynamic_group_path"] != 1 {
		t.Fatalf("diagnostic codes = %#v", codes)
	}
}

// findOperation 返回指定精确键对应的路由
func findOperation(t *testing.T, operations []ir.Operation, key string) ir.Operation {
	t.Helper()
	for _, operation := range operations {
		if operation.Key().String() == key {
			return operation
		}
	}
	t.Fatalf("operation %s was not discovered", key)
	return ir.Operation{}
}
