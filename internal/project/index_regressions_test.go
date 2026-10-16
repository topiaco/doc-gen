package project

import (
	"path/filepath"
	"testing"
)

// TestExistingBaselineHandlesRuntimeMapIndexes 验证存量初始化能分析运行时填充的字符串键 map
func TestExistingBaselineHandlesRuntimeMapIndexes(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "columns.go"), []byte(`package source
func imageColumn(headers []string) int {
 columns := make(map[string]int)
 for index, header := range headers { columns[header] = index }
 return columns["product_image"] + 1
}
`))
	state, created, _, err := Initialize(root, true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !created || len(state.Baseline) != 6 {
		t.Fatalf("baseline: created=%t state=%+v", created, state)
	}
	result, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Summary.Generated != 0 || result.Report.Summary.Excluded != 6 {
		t.Fatalf("existing endpoints were not preserved: %+v", result.Report.Summary)
	}
}

// TestMapIndexFallsBackToElementType 验证未知 map 索引保留元素类型且不会将非整数键传给整数转换
func TestMapIndexFallsBackToElementType(t *testing.T) {
	spec := generateFixture(t, `
func imageColumn(columns map[string]int) int { return columns["product_image"] }
func IndexRoutes(e *gin.Engine) {
 e.GET("/map-parameter", func(c *gin.Context) { c.JSON(200, imageColumn(make(map[string]int))) })
 e.GET("/map-missing", func(c *gin.Context) { c.JSON(200, map[string]string{"other": "value"}["product_image"]) })
 e.GET("/map-bool", func(c *gin.Context) { c.JSON(200, make(map[bool]string)[true]) })
 e.GET("/map-float", func(c *gin.Context) { c.JSON(200, make(map[float64]string)[1.5]) })
 e.GET("/map-complex", func(c *gin.Context) { c.JSON(200, make(map[complex128]string)[1i]) })
 e.GET("/map-int", func(c *gin.Context) { c.JSON(200, make(map[int]string)[1]) })
 e.GET("/map-large-int", func(c *gin.Context) { c.JSON(200, make(map[uint64]string)[1 << 63]) })
}
`)
	for path, want := range map[string]string{
		"/map-parameter": "integer",
		"/map-missing":   "string",
		"/map-bool":      "string",
		"/map-float":     "string",
		"/map-complex":   "string",
		"/map-int":       "string",
		"/map-large-int": "string",
	} {
		if got := responseSchema(spec, path)["type"]; got != want {
			t.Errorf("%s response type = %v, want %s", path, got, want)
		}
	}
}

// TestKnownIndexPreservesConcreteValues 验证已知字符串键和集合下标仍能确定 any 中的具体响应类型
func TestKnownIndexPreservesConcreteValues(t *testing.T) {
	spec := generateFixture(t, `
func KnownIndexRoutes(e *gin.Engine) {
 e.GET("/known-map", func(c *gin.Context) { c.JSON(200, map[string]any{"product_image": Order{}}["product_image"]) })
 e.GET("/known-slice", func(c *gin.Context) { c.JSON(200, []any{"other", Order{}}[1]) })
 e.GET("/known-array", func(c *gin.Context) { c.JSON(200, [2]any{"other", Order{}}[1]) })
}
`)
	for _, path := range []string{"/known-map", "/known-slice", "/known-array"} {
		properties, _ := responseSchema(spec, path)["properties"].(map[string]any)
		if properties["amountCents"] == nil {
			t.Errorf("%s lost the concrete Order response: %#v", path, properties)
		}
	}
}
