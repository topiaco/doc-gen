package analyzer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
)

// TestAnalyzeConfiguredAndFormParameters 验证直接读取、配置 Helper 和未读取 Path 诊断
func TestAnalyzeConfiguredAndFormParameters(t *testing.T) {
	fixtureRoot := filepath.Join(packageDir(t), "testdata", "parameters")
	program, err := Load(context.Background(), fixtureRoot, []string{"./..."})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	discovery := DiscoverGinRoutes(program)
	resolutions := ResolveHandlers(program, discovery.Operations)
	minimum := 1.0
	helpers := []config.ParameterHelper{
		{Symbol: "fixture/parameters/helper.QueryInt", Location: "query", NameArgument: 1, Type: "integer", Minimum: &minimum},
		{Symbol: "fixture/parameters/helper.QueryArray", Location: "query", NameArgument: 1, Type: "array", CollectionFormat: "multi"},
	}
	analyzed := AnalyzeParameters(program, resolutions, helpers)

	exact := findResolution(t, analyzed, "GET /parameters/{id}")
	if len(exact.Operation.Parameters) != 8 {
		t.Fatalf("parameter count = %d, want 8", len(exact.Operation.Parameters))
	}
	required := findParameter(t, exact.Operation.Parameters, ir.ParameterQuery, "required")
	if !required.Required || required.Confidence != ir.ConfidenceInferred {
		t.Fatalf("required query = %#v", required)
	}
	file := findParameter(t, exact.Operation.Parameters, ir.ParameterForm, "upload")
	if file.Format != "binary" || file.Origin != "gin.Context.FormFile" {
		t.Fatalf("file parameter = %#v", file)
	}
	header := findParameter(t, exact.Operation.Parameters, ir.ParameterHeader, "X-Token")
	if header.Origin != "gin.Context.GetHeader" {
		t.Fatalf("header parameter = %#v", header)
	}
	findParameter(t, exact.Operation.Parameters, ir.ParameterForm, "name")
	findParameter(t, exact.Operation.Parameters, ir.ParameterQuery, "optional")
	page := findParameter(t, exact.Operation.Parameters, ir.ParameterQuery, "page")
	if page.Type.String() != "integer" || page.Minimum == nil || *page.Minimum != 1 || page.Confidence != ir.ConfidenceConfigured {
		t.Fatalf("page parameter = %#v", page)
	}
	tags := findParameter(t, exact.Operation.Parameters, ir.ParameterQuery, "tag")
	if tags.Type.String() != "[]string" || tags.Collection != "multi" {
		t.Fatalf("tag parameter = %#v", tags)
	}

	missing := findResolution(t, analyzed, "POST /parameters/{unused}")
	unused := findParameter(t, missing.Operation.Parameters, ir.ParameterPath, "unused")
	if !unused.Required || unused.Confidence != ir.ConfidenceUnknown || unused.Origin != "route.path" {
		t.Fatalf("unused path parameter = %#v", unused)
	}
	codes := map[string]int{}
	for _, diagnostic := range missing.Operation.Diagnostics {
		codes[diagnostic.Code]++
	}
	if codes["parameter.path_not_read"] != 1 || codes["parameter.dynamic_name"] != 1 {
		t.Fatalf("missing parameter diagnostics = %#v", missing.Operation.Diagnostics)
	}
}

// findParameter 返回指定名称和位置的参数
func findParameter(t *testing.T, parameters []ir.Parameter, location ir.ParameterLocation, name string) ir.Parameter {
	t.Helper()
	for _, parameter := range parameters {
		if parameter.Location == location && parameter.Name == name {
			return parameter
		}
	}
	t.Fatalf("parameter %s %s was not found", location, name)
	return ir.Parameter{}
}
