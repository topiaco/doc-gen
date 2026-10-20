package project

import "testing"

// TestOmittedInterfaceResponseFieldIsNull 验证错误响应未赋值的接口字段按 nil 生成，成功响应保留具体结构
func TestOmittedInterfaceResponseFieldIsNull(t *testing.T) {
	spec := generateFixture(t, `
// ErrorEnvelope 接口响应
type ErrorEnvelope struct {
 Code int `+"`json:\"code\" description:\"业务编码\"`"+`
 Data any `+"`json:\"data\" description:\"接口结果\"`"+`
}
func sendFailure(c *gin.Context, status int) {
 response := ErrorEnvelope{Code: -1}
 c.JSON(status, response)
}
func NilResponseRoutes(e *gin.Engine) {
 e.GET("/nil-error-data", func(c *gin.Context) {
  if c.Query("bad") != "" { sendFailure(c, 400); return }
  if c.Query("failed") != "" { sendFailure(c, 500); return }
  c.JSON(200, ErrorEnvelope{Data: Order{}})
 })
 e.GET("/empty-envelope", func(c *gin.Context) { c.JSON(200, ErrorEnvelope{}) })
 e.GET("/explicit-nil", func(c *gin.Context) { c.JSON(200, ErrorEnvelope{Data: nil}) })
 e.GET("/assigned-data", func(c *gin.Context) {
  response := ErrorEnvelope{}
  response.Data = Order{}
  c.JSON(200, response)
 })
 e.GET("/dynamic-data", func(c *gin.Context) {
  var data any
  c.ShouldBindJSON(&data)
  c.JSON(200, ErrorEnvelope{Data: data})
 })
}
`)
	op := operationAt(spec, "/nil-error-data", "get")
	for _, status := range []string{"400", "500"} {
		response := op["responses"].(map[string]any)[status].(map[string]any)
		media := response["content"].(map[string]any)["application/json"].(map[string]any)
		envelope := resolveSchema(spec, media["schema"])
		data := resolveSchema(spec, envelope["properties"].(map[string]any)["data"])
		assertNullResponseField(t, status, data)
	}
	for _, path := range []string{"/empty-envelope", "/explicit-nil"} {
		envelope := responseSchema(spec, path)
		data := resolveSchema(spec, envelope["properties"].(map[string]any)["data"])
		assertNullResponseField(t, path, data)
	}
	for _, path := range []string{"/nil-error-data", "/assigned-data"} {
		envelope := responseSchema(spec, path)
		data := resolveSchema(spec, envelope["properties"].(map[string]any)["data"])
		properties, _ := data["properties"].(map[string]any)
		if properties["amountCents"] == nil {
			t.Errorf("%s lost the concrete Order response: %#v", path, data)
		}
	}
	for _, path := range []string{"/nil-error-data", "/empty-envelope", "/explicit-nil", "/assigned-data", "/dynamic-data"} {
		operation := operationAt(spec, path, "get")
		diagnostics, _ := operation["x-apidoc-diagnostics"].([]any)
		freeForm := false
		for _, diagnostic := range diagnostics {
			if diagnostic.(map[string]any)["code"] == "schema.free_form" {
				freeForm = true
			}
		}
		if want := path == "/dynamic-data"; freeForm != want {
			t.Errorf("%s free-form warning = %t, want %t", path, freeForm, want)
		}
	}
}

// assertNullResponseField 检查字段只允许 null 且保留业务说明
func assertNullResponseField(t *testing.T, label string, schema map[string]any) {
	t.Helper()
	values, _ := schema["enum"].([]any)
	if schema["nullable"] != true || len(values) != 1 || values[0] != nil {
		t.Errorf("%s data should only allow null: %#v", label, schema)
	}
	if schema["description"] != "接口结果" {
		t.Errorf("%s lost the field description: %#v", label, schema)
	}
	if _, unresolved := schema["x-apidoc-unresolved"]; unresolved {
		t.Errorf("%s null data was treated as unresolved: %#v", label, schema)
	}
}
