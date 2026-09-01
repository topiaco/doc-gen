package report

import (
	"bytes"
	"testing"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
)

// TestBuildSummarizesAllOperations 验证全路由报告、覆盖率和稳定 JSON
func TestBuildSummarizesAllOperations(t *testing.T) {
	stringType := ir.TypeRef{Kind: ir.TypeBasic, Name: "string"}
	operations := []ir.Operation{
		{
			Method:     "GET",
			Path:       "/items/{id}",
			Mode:       ir.ModeManaged,
			Confidence: ir.ConfidenceExact,
			Parameters: []ir.Parameter{{
				Name: "id", Location: ir.ParameterPath, Type: stringType, Confidence: ir.ConfidenceUnknown,
			}},
			RequestBody: &ir.RequestBody{Type: stringType, Confidence: ir.ConfidenceExact},
			Responses: []ir.Response{{Status: 200, Content: []ir.ResponseContent{
				{MediaType: "application/json", Type: &stringType, Confidence: ir.ConfidenceExact},
				{MediaType: "application/json", Confidence: ir.ConfidenceUnknown},
			}}},
			Diagnostics: []ir.Diagnostic{{Code: "parameter.path_not_read", Severity: ir.SeverityWarning}},
		},
		{Method: "GET", Path: "/manual", Mode: ir.ModeManual, Confidence: ir.ConfidenceExact, Responses: []ir.Response{{Status: 204}}},
		{Method: "GET", Path: "/ignored", Mode: ir.ModeIgnored, Confidence: ir.ConfidenceExact, Responses: []ir.Response{{Status: 204}}},
	}
	selection := config.Selection{Operations: map[string]config.SelectionOperation{
		"GET /items/{id}": {Mode: "managed"},
	}}
	diagnostics := []ir.Diagnostic{{Code: "route.dynamic_path", Severity: ir.SeverityWarning}}

	document := Build(operations, selection, "/project/api-doc/selection.yaml", nil, diagnostics, 1)
	if document.Summary.DiscoveredRoutes != 3 || document.Summary.Managed != 1 || document.Summary.Manual != 1 || document.Summary.Ignored != 1 {
		t.Fatalf("route summary = %#v", document.Summary)
	}
	if document.Summary.RouteDiscoveryCoverage != 75 || document.Summary.ManagedSchemaCoverage != 50 {
		t.Fatalf("coverage summary = %#v", document.Summary)
	}
	if document.Summary.UnknownManagedResponses != 1 || document.Summary.WarningDiagnostics != 2 {
		t.Fatalf("diagnostic summary = %#v", document.Summary)
	}
	managed, found := FindOperation(document, ir.OperationKey{Method: "GET", Path: "/items/{id}"})
	if !found || managed.ModeSource != "selection.yaml" || managed.RequestConfidence != ir.ConfidenceUnknown || managed.ResponseConfidence != ir.ConfidenceUnknown {
		t.Fatalf("managed operation = %#v", managed)
	}
	manual, found := FindOperation(document, ir.OperationKey{Method: "GET", Path: "/manual"})
	if !found || manual.ModeSource != "defaultMode" {
		t.Fatalf("manual operation = %#v", manual)
	}

	first, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || !bytes.HasSuffix(first, []byte("\n")) {
		t.Fatal("report JSON is not stable or lacks trailing newline")
	}
	if issues := Unresolved(document); len(issues) != 3 {
		t.Fatalf("unresolved issue count = %d, want 3", len(issues))
	}
}

// TestEvaluateAppliesQualityConfiguration 验证质量门禁开关和阈值
func TestEvaluateAppliesQualityConfiguration(t *testing.T) {
	document := Document{
		Summary: Summary{
			RouteDiscoveryCoverage:  90,
			ManagedSchemaCoverage:   70,
			UnknownManagedResponses: 1,
		},
		Operations: []Operation{
			{Method: "GET", Path: "/a/{id}", Mode: ir.ModeManaged, OperationID: "duplicate", Parameters: []ir.Parameter{{Location: ir.ParameterPath, Confidence: ir.ConfidenceUnknown}}},
			{Method: "GET", Path: "/b", Mode: ir.ModeManaged, OperationID: "duplicate"},
		},
		Diagnostics: []ir.Diagnostic{{Code: "override.orphan_operation", Severity: ir.SeverityError}},
	}
	quality := config.Quality{
		MinimumRouteDiscoveryCoverage:     100,
		MinimumManagedExactSchemaCoverage: 80,
		FailOnDuplicateOperationID:        true,
		FailOnInvalidOpenAPI:              true,
		FailOnUnresolvedManagedPath:       true,
		FailOnUnknownManagedResponse:      false,
		FailOnOrphanSelection:             true,
		FailOnOrphanOverride:              true,
	}

	failures := Evaluate(&document, quality, false)
	if len(failures) != 6 {
		t.Fatalf("quality failures = %#v, want 6", failures)
	}
	if len(document.Quality) != 8 || document.Quality[5].Name != "unknownManagedResponse" || !document.Quality[5].Passed {
		t.Fatalf("quality gates = %#v", document.Quality)
	}
}
