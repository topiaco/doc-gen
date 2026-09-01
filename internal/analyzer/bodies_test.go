package analyzer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/topiaco/doc-gen/internal/ir"
)

// TestAnalyzeJSONBodyShapesAndDynamicType 验证直接 Body 形状和动态类型诊断
func TestAnalyzeJSONBodyShapesAndDynamicType(t *testing.T) {
	fixtureRoot := filepath.Join(packageDir(t), "testdata", "bodies")
	program, err := Load(context.Background(), fixtureRoot, []string{"./..."})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	discovery := DiscoverGinRoutes(program)
	resolutions := ResolveHandlers(program, discovery.Operations)
	analyzed := AnalyzeJSONBodies(program, resolutions)

	object := findResolution(t, analyzed, "POST /bodies/object")
	assertBodyType(t, object, "*fixture/bodies.ObjectRequest")
	fields := object.Operation.RequestBody.Type.Element.Fields
	if len(fields) != 3 || fields[0].JSONName != "name" || !fields[0].Required || !fields[1].Required || fields[2].Required {
		t.Fatalf("object fields = %#v", fields)
	}

	array := findResolution(t, analyzed, "POST /bodies/array")
	assertBodyType(t, array, "*[]*fixture/bodies.ObjectRequest")
	arrayItem := array.Operation.RequestBody.Type.Element.Element.Element
	if arrayItem == nil || len(arrayItem.Fields) != 3 {
		t.Fatalf("array item = %#v", arrayItem)
	}

	anonymous := findResolution(t, analyzed, "POST /bodies/anonymous")
	assertBodyType(t, anonymous, "*struct{id uint;labels []string}")
	if fields := anonymous.Operation.RequestBody.Type.Element.Fields; len(fields) != 2 || !fields[0].Required {
		t.Fatalf("anonymous fields = %#v", fields)
	}

	dynamic := findResolution(t, analyzed, "POST /bodies/dynamic")
	assertBodyType(t, dynamic, "any")
	if dynamic.Operation.RequestBody.Confidence != ir.ConfidenceUnknown || len(dynamic.Operation.Diagnostics) != 1 || dynamic.Operation.Diagnostics[0].Code != "request_body.dynamic_type" {
		t.Fatalf("dynamic body = %#v, diagnostics = %#v", dynamic.Operation.RequestBody, dynamic.Operation.Diagnostics)
	}
}

// assertBodyType 验证请求体公共属性和类型表达式
func assertBodyType(t *testing.T, result HandlerResolution, want string) {
	t.Helper()
	body := result.Operation.RequestBody
	if body == nil {
		t.Fatalf("request body for %s was not found", result.Operation.Key())
	}
	if body.Type.String() != want || body.ContentType != "application/json" || !body.Required || body.Source.Line == 0 {
		t.Fatalf("request body for %s = %#v, want type %s", result.Operation.Key(), body, want)
	}
}
