package selection

import (
	"strings"
	"testing"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
)

// TestApplyUsesDefaultMode 验证未显式配置的路由继承默认状态
func TestApplyUsesDefaultMode(t *testing.T) {
	operations := []ir.Operation{{Method: "GET", Path: "/items"}}
	policy := config.Selection{DefaultMode: "manual", Operations: map[string]config.SelectionOperation{}}

	applied, err := Apply(operations, policy)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if applied[0].Mode != ir.ModeManual || applied[0].ModeReason != "selection defaultMode" {
		t.Fatalf("default selection = %#v", applied[0])
	}
}

// TestApplyRejectsOrphanSelection 验证删除或改名后的配置键会被发现
func TestApplyRejectsOrphanSelection(t *testing.T) {
	operations := []ir.Operation{{Method: "GET", Path: "/items/{id}"}}
	policy := config.Selection{
		DefaultMode: "manual",
		Operations: map[string]config.SelectionOperation{
			"GET /items/{old_id}": {Mode: "managed"},
			"GET /removed":        {Mode: "ignored"},
		},
	}

	_, err := Apply(operations, policy)
	if err == nil || !strings.Contains(err.Error(), "GET /items/{old_id}") || !strings.Contains(err.Error(), "GET /removed") {
		t.Fatalf("Apply() error = %v", err)
	}
}

// TestApplyRejectsUnnormalizedSelection 验证 Gin 参数写法不能用于 Selection 精确键
func TestApplyRejectsUnnormalizedSelection(t *testing.T) {
	operations := []ir.Operation{{Method: "GET", Path: "/items/{id}"}}
	policy := config.Selection{
		DefaultMode: "manual",
		Operations: map[string]config.SelectionOperation{
			"GET /items/:id": {Mode: "managed"},
		},
	}

	_, err := Apply(operations, policy)
	if err == nil || !strings.Contains(err.Error(), "must be normalized as") {
		t.Fatalf("Apply() error = %v", err)
	}
}

// TestFilter 验证状态过滤保持路由顺序
func TestFilter(t *testing.T) {
	operations := []ir.Operation{
		{Method: "GET", Path: "/managed", Mode: ir.ModeManaged},
		{Method: "GET", Path: "/manual", Mode: ir.ModeManual},
		{Method: "GET", Path: "/ignored", Mode: ir.ModeIgnored},
	}
	filtered := Filter(operations, ir.ModeManual)
	if len(filtered) != 1 || filtered[0].Path != "/manual" {
		t.Fatalf("Filter() = %#v", filtered)
	}
}
