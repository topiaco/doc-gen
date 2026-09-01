package analyzer

import (
	"context"
	"path/filepath"
	"testing"
)

// TestResolveHandlersReportsUnsupportedExpressions 验证闭包和动态 Handler 产生诊断
func TestResolveHandlersReportsUnsupportedExpressions(t *testing.T) {
	fixtureRoot := filepath.Join(packageDir(t), "testdata", "handlers")
	program, err := Load(context.Background(), fixtureRoot, []string{"./..."})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	discovery := DiscoverGinRoutes(program)
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("route diagnostics = %#v", discovery.Diagnostics)
	}

	resolutions := ResolveHandlers(program, discovery.Operations)
	if len(resolutions) != 3 {
		t.Fatalf("resolution count = %d, want 3", len(resolutions))
	}
	exact := findResolution(t, resolutions, "GET /handlers/exact")
	if exact.Diagnostic != nil || exact.Declaration.Name.Name != "Exact" {
		t.Fatalf("exact handler = %#v", exact)
	}
	dynamic := findResolution(t, resolutions, "GET /handlers/dynamic")
	if dynamic.Diagnostic == nil || dynamic.Diagnostic.Code != "handler.declaration_not_found" {
		t.Fatalf("dynamic handler diagnostic = %#v", dynamic.Diagnostic)
	}
	closure := findResolution(t, resolutions, "GET /handlers/closure")
	if closure.Diagnostic == nil || closure.Diagnostic.Code != "handler.unsupported_expression" {
		t.Fatalf("closure handler diagnostic = %#v", closure.Diagnostic)
	}
}

// findResolution 返回指定精确键的 Handler 解析结果
func findResolution(t *testing.T, resolutions []HandlerResolution, key string) HandlerResolution {
	t.Helper()
	for _, resolution := range resolutions {
		if resolution.Operation.Key().String() == key {
			return resolution
		}
	}
	t.Fatalf("handler resolution %s was not found", key)
	return HandlerResolution{}
}
