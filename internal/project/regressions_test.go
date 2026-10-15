package project

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// generateFixture 分析可编译的业务源码，以实际生成的 OpenAPI 验证推断结果
func generateFixture(t *testing.T, source string) map[string]any {
	t.Helper()
	root := fixture(t)
	write(t, filepath.Join(root, "regression.go"), []byte("package source\nimport \"github.com/gin-gonic/gin\"\n"+source))
	if _, _, _, err := Initialize(root, false, "", ""); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(result.OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

// operationAt 从测试文档读取指定方法和路径的接口
func operationAt(spec map[string]any, path, method string) map[string]any {
	return spec["paths"].(map[string]any)[path].(map[string]any)[method].(map[string]any)
}

// resolveSchema 解析测试文档的组件引用与 allOf，便于检查实际字段和约束
func resolveSchema(spec map[string]any, value any) map[string]any {
	schema := value.(map[string]any)
	if ref, ok := schema["$ref"].(string); ok {
		return resolveSchema(spec, spec["components"].(map[string]any)["schemas"].(map[string]any)[strings.TrimPrefix(ref, "#/components/schemas/")])
	}
	if parts, ok := schema["allOf"].([]any); ok {
		merged := map[string]any{}
		for _, part := range parts {
			for key, value := range resolveSchema(spec, part) {
				merged[key] = value
			}
		}
		for key, value := range schema {
			if key != "allOf" {
				merged[key] = value
			}
		}
		return merged
	}
	return schema
}

// responseSchema 读取 GET 接口的 HTTP 200 JSON 响应结构
func responseSchema(spec map[string]any, path string) map[string]any {
	response := operationAt(spec, path, "get")["responses"].(map[string]any)["200"].(map[string]any)
	return resolveSchema(spec, response["content"].(map[string]any)["application/json"].(map[string]any)["schema"])
}

// TestGenericHandlerUsesCallSiteTypesInsideBody 验证泛型 Handler 内的请求和响应使用各自的类型实参
func TestGenericHandlerUsesCallSiteTypesInsideBody(t *testing.T) {
	spec := generateFixture(t, `
func genericHandler[T any](c *gin.Context) {
 var input T
 c.ShouldBindJSON(&input)
 c.JSON(200, Response[T]{Data: input})
}
func GenericRoutes(e *gin.Engine) {
 e.POST("/generic-order", genericHandler[Order])
 e.POST("/generic-request", genericHandler[CreateOrderRequest])
}
`)
	for path, field := range map[string]string{"/generic-order": "amountCents", "/generic-request": "customerId"} {
		op := operationAt(spec, path, "post")
		body := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)
		request := resolveSchema(spec, body["schema"])
		properties, _ := request["properties"].(map[string]any)
		if properties[field] == nil {
			t.Errorf("%s request type was not instantiated: %#v", path, request)
		}
		response := op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)
		wrapper := resolveSchema(spec, response["schema"])
		data := resolveSchema(spec, wrapper["properties"].(map[string]any)["data"])
		properties, _ = data["properties"].(map[string]any)
		if properties[field] == nil {
			t.Errorf("%s response type was not instantiated: %#v", path, data)
		}
	}
}

// TestConcreteValuesPreserveDeclaredTypesAndFieldAnnotations 验证具体值推断保留字段声明类型和格式指令
func TestConcreteValuesPreserveDeclaredTypesAndFieldAnnotations(t *testing.T) {
	spec := generateFixture(t, `
// Envelope 订单响应
type Envelope struct {
 // Count 订单数量
 Count int64 `+"`json:\"count\"`"+`
 // Day 业务日期
 // apidoc:format date
 Day string `+"`json:\"day\"`"+`
 // Data 订单数据
 Data any `+"`json:\"data\"`"+`
}
func ValueRoutes(e *gin.Engine) {
 e.GET("/converted", func(c *gin.Context) { c.JSON(200, any(Order{})) })
 e.GET("/fields", func(c *gin.Context) { c.JSON(200, Envelope{Count: 1, Day: "2026-10-15", Data: Order{}}) })
 e.GET("/collection", func(c *gin.Context) { c.JSON(200, []Envelope{{Data: Order{}}}) })
}
`)
	converted := responseSchema(spec, "/converted")
	if converted["properties"] == nil {
		t.Errorf("conversion lost concrete value: %#v", converted)
	}
	fields := responseSchema(spec, "/fields")["properties"].(map[string]any)
	if count := resolveSchema(spec, fields["count"]); count["format"] != "int64" {
		t.Errorf("literal changed declared integer type: %#v", count)
	}
	if day := resolveSchema(spec, fields["day"]); day["format"] != "date" {
		t.Errorf("literal erased field format: %#v", day)
	}
	collection := responseSchema(spec, "/collection")
	item := resolveSchema(spec, collection["items"])
	data := resolveSchema(spec, item["properties"].(map[string]any)["data"])
	if data["properties"] == nil {
		t.Errorf("collection lost concrete field type: %#v", data)
	}
}

// TestStructCopiesAndHelperMutationsMatchGoAssignments 验证结构体复制与跨函数指针修改符合 Go 赋值语义
func TestStructCopiesAndHelperMutationsMatchGoAssignments(t *testing.T) {
	spec := generateFixture(t, `
type Box struct { Data any `+"`json:\"data\"`"+` }
func changeBox(b *Box) { b.Data = Order{} }
func callChange(b *Box) { changeBox(b) }
func AssignmentRoutes(e *gin.Engine) {
 e.GET("/copies", func(c *gin.Context) {
  original := Box{Data: Order{}}
  copied := original
  copied.Data = "different"
  c.JSON(200, original)
 })
 e.GET("/mutations", func(c *gin.Context) {
  first, second := Box{}, Box{}
  callChange(&first)
  callChange(&second)
  c.JSON(200, second)
 })
}
`)
	for _, path := range []string{"/copies", "/mutations"} {
		box := responseSchema(spec, path)
		data := resolveSchema(spec, box["properties"].(map[string]any)["data"])
		if data["properties"] == nil {
			t.Errorf("%s did not preserve assignment semantics: %#v", path, data)
		}
	}
}

// TestAliasDescriptionsRemainLocal 验证类型别名和原类型分别保留自己的业务说明
func TestAliasDescriptionsRemainLocal(t *testing.T) {
	spec := generateFixture(t, `
// DetailedOrder 审核用订单
type DetailedOrder = Order
func CommentRoutes2(e *gin.Engine) {
 e.GET("/alias-description", func(c *gin.Context) { var v DetailedOrder; c.JSON(200, v) })
 e.GET("/original-description", func(c *gin.Context) { c.JSON(200, Order{}) })
}
`)
	if got := responseSchema(spec, "/alias-description")["description"]; got != "审核用订单" {
		t.Errorf("alias description: %v", got)
	}
	if got := responseSchema(spec, "/original-description")["description"]; got != "订单详情\n用于展示实付金额及支付状态" {
		t.Errorf("alias changed original type description: %v", got)
	}
}

// TestOptionalQueryDefaultIsNotRequired 区分默认值赋值和明确拒绝缺失参数的校验
func TestOptionalQueryDefaultIsNotRequired(t *testing.T) {
	spec := generateFixture(t, `
func QueryDefaults(e *gin.Engine) {
 e.GET("/query-default", func(c *gin.Context) {
  page := c.Query("page")
  if page == "" { page = "1" }
  c.JSON(200, page)
 })
 e.GET("/required-query", func(c *gin.Context) {
  id := c.Query("id")
  if id == "" { c.JSON(400, "missing id"); return }
  c.JSON(200, id)
 })
}
`)
	param := operationAt(spec, "/query-default", "get")["parameters"].([]any)[0].(map[string]any)
	if param["required"] == true {
		t.Fatal("an empty-value default was incorrectly documented as required")
	}
	required := operationAt(spec, "/required-query", "get")["parameters"].([]any)[0].(map[string]any)
	if required["required"] != true {
		t.Fatal("an explicit missing-parameter rejection was not documented")
	}
}

// TestGenericMethodsClosuresAndVariadicHelpers 验证泛型方法、闭包变量和可变参数函数中的具体类型
func TestGenericMethodsClosuresAndVariadicHelpers(t *testing.T) {
	spec := generateFixture(t, `
type GenericService[T any] struct{}
func (GenericService[T]) Get(c *gin.Context) { var value T; c.JSON(200, value) }
func genericBody[T any](c *gin.Context) { genericHandler2[T](c) }
func genericHandler2[T any](c *gin.Context) { var value T; c.JSON(200, value) }
func many(c *gin.Context, values ...any) { c.JSON(200, values) }
func FunctionRoutes(e *gin.Engine) {
 e.GET("/generic-method", GenericService[Order]{}.Get)
 e.GET("/generic-helper", genericBody[Order])
 var value any
 handler := func(c *gin.Context) { c.JSON(200, value) }
 value = Order{}
 e.GET("/captured", handler)
 e.GET("/variadic", func(c *gin.Context) { many(c, Order{}, Order{}) })
}
`)
	for _, path := range []string{"/generic-method", "/generic-helper", "/captured"} {
		if value := responseSchema(spec, path); value["properties"] == nil {
			t.Errorf("%s lost its concrete type: %#v", path, value)
		}
	}
	collection := responseSchema(spec, "/variadic")
	if collection["type"] != "array" {
		t.Fatalf("variadic arguments were not a slice: %#v", collection)
	}
	if item := resolveSchema(spec, collection["items"]); item["properties"] == nil {
		t.Errorf("variadic element type: %#v", item)
	}
}

// TestBusinessCommentsKeepDirectiveWhitespaceAndPriority 验证块注释、指令空白和字段说明优先级
func TestBusinessCommentsKeepDirectiveWhitespaceAndPriority(t *testing.T) {
	spec := generateFixture(t, `
/* Annotated 提交记录
 * 用于说明多行注释
 */
type Annotated struct {
 // Name
 Name string `+"`json:\"name\"`"+` // 操作人姓名
}
// Explicit 查询提交记录
// apidoc:response	200	Annotated
func Explicit(c *gin.Context) {}
func AnnotationRoutes(e *gin.Engine) {
 // 此处注册业务路由
 e.GET("/undocumented", func(c *gin.Context) { c.JSON(200, Annotated{}) })
 e.GET("/annotated", Explicit)
}
`)
	value := responseSchema(spec, "/annotated")
	if value["description"] != "提交记录\n用于说明多行注释" {
		t.Errorf("block comment description: %#v", value)
	}
	name := resolveSchema(spec, value["properties"].(map[string]any)["name"])
	if name["description"] != "操作人姓名" {
		t.Errorf("empty leading description hid trailing comment: %#v", name)
	}
	if summary := operationAt(spec, "/undocumented", "get")["summary"]; summary != nil {
		t.Errorf("implementation comment became a business title: %v", summary)
	}
}

// TestCollectionValidationAppliesToElements 验证集合本身与元素分别使用对应的校验规则
func TestCollectionValidationAppliesToElements(t *testing.T) {
	spec := generateFixture(t, `
type CollectionInput struct {
 IDs []int64 `+"`json:\"ids\" binding:\"dive,required,min=1\"`"+`
 Names []string `+"`json:\"names\" binding:\"required,min=1,dive,max=20\"`"+`
}
func CollectionRoute(e *gin.Engine) {
 e.POST("/collection-input", func(c *gin.Context) { var input CollectionInput; c.ShouldBindJSON(&input); c.JSON(200, input) })
}
`)
	op := operationAt(spec, "/collection-input", "post")
	body := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)
	input := resolveSchema(spec, body["schema"])
	if required := input["required"].([]any); len(required) != 1 || required[0] != "names" {
		t.Errorf("element required became collection required: %#v", required)
	}
	fields := input["properties"].(map[string]any)
	ids := resolveSchema(spec, fields["ids"])
	if item := resolveSchema(spec, ids["items"]); item["minimum"] != float64(1) {
		t.Errorf("element constraint missing: %#v", item)
	}
	names := resolveSchema(spec, fields["names"])
	if names["minItems"] != float64(1) || resolveSchema(spec, names["items"])["maxLength"] != float64(20) {
		t.Errorf("collection and element constraints: %#v", names)
	}
}

// TestEmbeddedAndNestedFieldsRetainConcreteAssignments 验证嵌入、嵌套及指针赋值能够确定实际响应类型
func TestEmbeddedAndNestedFieldsRetainConcreteAssignments(t *testing.T) {
	spec := generateFixture(t, `
type Payload struct { Data any `+"`json:\"data\"`"+` }
type Embedded struct { Payload }
type Nested struct { Payload Payload `+"`json:\"payload\"`"+` }
func EmbeddedRoutes(e *gin.Engine) {
 e.GET("/embedded", func(c *gin.Context) { c.JSON(200, Embedded{Payload: Payload{Data: Order{}}}) })
 e.GET("/nested-assignment", func(c *gin.Context) { var result Nested; result.Payload.Data = Order{}; c.JSON(200, result) })
 e.GET("/pointer-reassignment", func(c *gin.Context) { var result Payload; pointer := &result; result = Payload{Data: Order{}}; c.JSON(200, pointer) })
}
`)
	for _, path := range []string{"/embedded", "/nested-assignment", "/pointer-reassignment"} {
		object := responseSchema(spec, path)
		fields := object["properties"].(map[string]any)
		if path == "/nested-assignment" {
			fields = resolveSchema(spec, fields["payload"])["properties"].(map[string]any)
		}
		data := resolveSchema(spec, fields["data"])
		if data["properties"] == nil {
			t.Errorf("%s lost nested assignment: %#v", path, data)
		}
	}
}

// TestSwitchBranchesExcludeUnreachableInitialValue 验证所有 switch 分支赋值后不再使用初始值
func TestSwitchBranchesExcludeUnreachableInitialValue(t *testing.T) {
	spec := generateFixture(t, `
func SwitchRoutes(e *gin.Engine) {
 e.GET("/switch", func(c *gin.Context) {
  var data any
  switch c.Query("view") {
  case "one": data = Order{}
  default: data = []Order{}
  }
  c.JSON(200, data)
 })
}
`)
	response := responseSchema(spec, "/switch")
	variants := response["anyOf"].([]any)
	if len(variants) != 2 {
		t.Errorf("switch included a value replaced in every branch: %#v", response)
	}
}

// TestCachedHelperPreservesReturnedMapAliases 验证重复调用辅助函数后返回的 map 仍引用调用方数据
func TestCachedHelperPreservesReturnedMapAliases(t *testing.T) {
	spec := generateFixture(t, `
func sameMap(value gin.H) gin.H { return value }
func MapAliasRoute(e *gin.Engine) {
 e.GET("/map-alias", func(c *gin.Context) {
  first := gin.H{"data": Order{}}
  second := gin.H{"data": Order{}}
  _ = sameMap(first)
  result := sameMap(second)
  result["data"] = "changed"
  c.JSON(200, second)
 })
}
`)
	fields := responseSchema(spec, "/map-alias")["properties"].(map[string]any)
	if data := resolveSchema(spec, fields["data"]); data["type"] != "string" {
		t.Errorf("cached return no longer references the input map: %#v", data)
	}
}

// TestCollectionConstructionPreservesElementsAndMapAliases 验证切片展开追加与 make 创建的 map 引用关系
func TestCollectionConstructionPreservesElementsAndMapAliases(t *testing.T) {
	spec := generateFixture(t, `
func ConstructionRoutes(e *gin.Engine) {
 e.GET("/append", func(c *gin.Context) {
  values := []any{Order{}}
  result := append([]any{}, values...)
  c.JSON(200, result)
 })
 e.GET("/make-map", func(c *gin.Context) {
  original := make(gin.H)
  alias := original
  alias["data"] = Order{}
  c.JSON(200, original)
 })
}
`)
	collection := responseSchema(spec, "/append")
	if item := resolveSchema(spec, collection["items"]); item["type"] != "object" {
		t.Errorf("slice expansion became a nested collection: %#v", item)
	}
	object := responseSchema(spec, "/make-map")
	if fields, ok := object["properties"].(map[string]any); !ok || fields["data"] == nil {
		t.Errorf("make map lost shared assignments: %#v", object)
	}
}
