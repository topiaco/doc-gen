package analyzer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/topiaco/doc-gen/internal/ir"
)

// TestAnalyzeDirectResponseKinds 验证直接 JSON、二进制、文件和重定向响应
func TestAnalyzeDirectResponseKinds(t *testing.T) {
	fixtureRoot := filepath.Join(packageDir(t), "testdata", "responses")
	program, err := Load(context.Background(), fixtureRoot, []string{"./..."})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	discovery := DiscoverGinRoutes(program)
	resolutions := ResolveHandlers(program, discovery.Operations)
	analyzed := AnalyzeDirectResponses(program, resolutions, fixtureResponseFunctions())

	directJSON := findResolution(t, analyzed, "GET /responses/json")
	content := findMediaContent(t, findResponse(t, directJSON.Operation.Responses, 201).Content, "application/json", false)
	if content.Type == nil || content.Type.String() != "*fixture/responses.Item" || content.Confidence != ir.ConfidenceExact {
		t.Fatalf("direct JSON content = %#v", content)
	}

	pdf := findResolution(t, analyzed, "GET /responses/pdf")
	pdfResponse := findResponse(t, pdf.Operation.Responses, 200)
	if len(pdfResponse.Content) != 2 {
		t.Fatalf("conditional PDF content = %#v", pdfResponse.Content)
	}
	assertBinaryContent(t, pdf, 200, "application/pdf", ir.ConfidenceExact)
	jsonContent := findMediaContent(t, pdfResponse.Content, "application/json", false)
	if jsonContent.Type == nil || jsonContent.Type.String() != "map[string]string" || jsonContent.Confidence != ir.ConfidenceUnknown ||
		!hasDiagnostic(pdf.Operation.Diagnostics, "response.map_keys_unknown") {
		t.Fatalf("conditional JSON content = %#v", jsonContent)
	}

	excel := findResolution(t, analyzed, "GET /responses/excel")
	assertBinaryContent(t, excel, 200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ir.ConfidenceExact)
	file := findResolution(t, analyzed, "GET /responses/file")
	assertBinaryContent(t, file, 200, "application/octet-stream", ir.ConfidenceInferred)
	attachment := findResolution(t, analyzed, "GET /responses/attachment")
	assertBinaryContent(t, attachment, 200, "application/octet-stream", ir.ConfidenceInferred)
	helper := findResolution(t, analyzed, "GET /responses/binary-helper")
	assertBinaryContent(t, helper, 200, "application/octet-stream", ir.ConfidenceConfigured)

	redirect := findResolution(t, analyzed, "GET /responses/redirect")
	redirectResponse := findResponse(t, redirect.Operation.Responses, 302)
	if redirectResponse.RedirectLocation != "/target" || redirectResponse.RedirectConfidence != ir.ConfidenceExact || len(redirectResponse.Content) != 0 {
		t.Fatalf("redirect response = %#v", redirectResponse)
	}
}

// assertBinaryContent 验证指定二进制响应内容
func assertBinaryContent(t *testing.T, result HandlerResolution, status int, mediaType string, confidence ir.Confidence) {
	t.Helper()
	content := findMediaContent(t, findResponse(t, result.Operation.Responses, status).Content, mediaType, true)
	if content.Confidence != confidence {
		t.Fatalf("binary response %s = %#v", result.Operation.Key(), content)
	}
}

// findMediaContent 返回指定媒体类型和二进制标记的响应内容
func findMediaContent(t *testing.T, contents []ir.ResponseContent, mediaType string, binary bool) ir.ResponseContent {
	t.Helper()
	for _, content := range contents {
		if content.MediaType == mediaType && content.Binary == binary {
			return content
		}
	}
	t.Fatalf("response content %s binary=%t was not found in %#v", mediaType, binary, contents)
	return ir.ResponseContent{}
}
