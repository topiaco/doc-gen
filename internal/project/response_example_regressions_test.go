package project

import "testing"

// TestResponseExamplesPreferConcreteData 验证默认示例优先展示数据结构，同时保留 HTTP 200 的业务错误和服务返回的 nil 分支。
func TestResponseExamplesPreferConcreteData(t *testing.T) {
	spec := generateFixture(t, `
type ExportData struct {
 PaymentTitle string `+"`json:\"payment_title\"`"+`
 ApplicationAmount float64 `+"`json:\"application_amount\"`"+`
}
type ExportEnvelope struct {
 Code int `+"`json:\"code\"`"+`
 Data any `+"`json:\"data\"`"+`
 Message string `+"`json:\"message\"`"+`
}
func loadExport(missing bool) (*ExportData, error) {
 if missing { return nil, nil }
 return &ExportData{}, nil
}
func exportOK(c *gin.Context, data any) { c.JSON(200, ExportEnvelope{Data: data}) }
func exportError(c *gin.Context) { c.JSON(200, ExportEnvelope{Code: 1}) }
func ExportRoutes(e *gin.Engine) {
 e.GET("/export", func(c *gin.Context) {
  data, err := loadExport(c.Query("missing") != "")
  if err != nil { exportError(c); return }
  exportOK(c, data)
 })
 e.GET("/direct-export", func(c *gin.Context) {
  if c.Query("missing") != "" { exportError(c); return }
  exportOK(c, &ExportData{})
 })
}
`)
	for _, path := range []string{"/export", "/direct-export"} {
		t.Run(path, func(t *testing.T) {
			variants := responseSchema(spec, path)["anyOf"].([]any)
			if len(variants) != 2 {
				t.Fatalf("lost response branches: %#v", variants)
			}
			envelope := resolveSchema(spec, variants[0])
			data := resolveSchema(spec, envelope["properties"].(map[string]any)["data"])
			if path == "/export" {
				dataVariants, ok := data["anyOf"].([]any)
				if !ok || len(dataVariants) != 2 {
					t.Fatalf("lost nullable service return branches: %#v", data)
				}
				assertNullExampleVariant(t, resolveSchema(spec, dataVariants[1]))
				data = resolveSchema(spec, dataVariants[0])
			}
			properties, _ := data["properties"].(map[string]any)
			if properties["payment_title"] == nil || properties["application_amount"] == nil {
				t.Fatalf("default example selects null instead of export data: %#v", data)
			}
			failure := resolveSchema(spec, variants[1])
			assertNullExampleVariant(t, resolveSchema(spec, failure["properties"].(map[string]any)["data"]))
		})
	}
}

func assertNullExampleVariant(t *testing.T, value map[string]any) {
	t.Helper()
	enum, _ := value["enum"].([]any)
	if value["nullable"] != true || len(enum) != 1 || enum[0] != nil {
		t.Fatalf("null branch was removed or changed: %#v", value)
	}
}
