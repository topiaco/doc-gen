package project

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	apiopenapi "github.com/topiaco/doc-gen/internal/openapi"
	"gopkg.in/yaml.v3"
)

// fixture 创建包含源码和 Gin 测试替身的临时 Module
func fixture(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	base := filepath.Join(filepath.Dir(file), "..", "analyzer", "testdata")
	root := t.TempDir()
	source, err := os.ReadFile(filepath.Join(base, "source", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "main.go"), source)
	gin, _ := filepath.Abs(filepath.Join(base, "routes", "gin"))
	write(t, filepath.Join(root, "go.mod"), []byte("module fixture/source\n\ngo 1.25.0\n\nrequire github.com/gin-gonic/gin v0.0.0\nreplace github.com/gin-gonic/gin => "+filepath.ToSlash(gin)+"\n"))
	return root
}

// write 写入测试文件，失败时结束当前用例
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSourceArtifactsContainBusinessDescriptionsAndConstraints 验证业务说明、约束及三个生成文件的稳定性
func TestSourceArtifactsContainBusinessDescriptionsAndConstraints(t *testing.T) {
	root := fixture(t)
	if _, _, _, err := Initialize(root, false, "Orders", "1.0"); err != nil {
		t.Fatal(err)
	}
	first, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Report.Summary.Generated != 5 {
		t.Fatalf("summary: %+v", first.Report.Summary)
	}
	if err := apiopenapi.Validate(first.OpenAPI); err != nil {
		t.Fatal(err)
	}
	text := string(first.OpenAPI)
	for _, want := range []string{"订单实付金额", "单位：分", "订单状态：pending=待支付，paid=已支付", "查询订单详情", "创建订单的请求参数", `"maxLength": 200`, `"minimum": 1`, `"date"`, `"default"`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "InternalNote") {
		t.Fatal("internal model field leaked")
	}
	var spec map[string]any
	if err := json.Unmarshal(first.OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	order := schemas["fixture_source_Order"].(map[string]any)
	if order["description"] != "订单详情\n用于展示实付金额及支付状态" {
		t.Fatalf("type description: %#v", order)
	}
	for _, required := range order["required"].([]any) {
		if required == "remark" {
			t.Fatal("omitempty response field marked required")
		}
	}
	input := schemas["fixture_source_CreateOrderRequestInput"].(map[string]any)
	if required := input["required"].([]any); len(required) != 1 || required[0] != "customerId" {
		t.Fatalf("request required: %#v", required)
	}
	if !bytes.Contains(first.HTML, []byte("查询订单详情")) {
		t.Fatal("offline HTML is missing embedded document")
	}
	second, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.OpenAPI, second.OpenAPI) || !bytes.Equal(first.HTML, second.HTML) || !bytes.Equal(first.ReportJSON, second.ReportJSON) {
		t.Fatal("generation is not deterministic")
	}
	if err := first.Write(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"openapi.json", "index.html", "report.json"} {
		if _, err := os.Stat(filepath.Join(root, OutputDir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

// TestBaselineIsImmutableAndNewEndpointsRemainManaged 验证存量快照不变，新增接口的修改和删除反映在文档中
func TestBaselineIsImmutableAndNewEndpointsRemainManaged(t *testing.T) {
	root := fixture(t)
	state, created, _, err := Initialize(root, true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !created || len(state.Baseline) != 6 {
		t.Fatalf("baseline: %+v", state)
	}
	initial, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Report.Summary.Generated != 0 {
		t.Fatal("existing endpoints were included")
	}
	original, _ := os.ReadFile(filepath.Join(root, "main.go"))
	updated := strings.Replace(string(original), `group.POST("/orders", Create)`, `group.POST("/orders", Create)`+"\n group.GET(\"/new/:id\", Handler{}.Get)", 1)
	write(t, filepath.Join(root, "main.go"), []byte(updated))
	next, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if next.Report.Summary.Generated != 1 {
		t.Fatalf("new endpoint summary: %+v", next.Report.Summary)
	}
	second, created, _, err := Initialize(root, true, "changed", "2.0")
	if err != nil || created || len(second.Baseline) != 6 || second.Title != state.Title {
		t.Fatalf("baseline changed: %+v, %t, %v", second, created, err)
	}
	updated = strings.Replace(updated, "订单实付金额", "已更新的订单金额", 1)
	write(t, filepath.Join(root, "main.go"), []byte(updated))
	changed, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(changed.OpenAPI, []byte("已更新的订单金额")) {
		t.Fatal("managed endpoint did not refresh shared model")
	}
	write(t, filepath.Join(root, "main.go"), original)
	deleted, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Report.Summary.Generated != 0 {
		t.Fatal("deleted endpoint remained")
	}
}

// TestWorkflowAndModuleDiscovery 验证 Module 定位与固定版本的 GitHub 工作流生成
func TestWorkflowAndModuleDiscovery(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repo, "server")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "go.mod"), []byte("module example/server\ngo 1.25.0\n"))
	found, err := Locate(repo)
	if err != nil || found != root {
		t.Fatalf("locate: %q %v", found, err)
	}
	workflow, err := Workflow(root, "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if workflow.Path != filepath.Join(repo, ".github", "workflows", "api-doc.yml") {
		t.Fatalf("workflow location: %s", workflow.Path)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(workflow.Data, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pull_request:", "github.event.repository.default_branch", "server/.apidoc/output", `go install "github.com/topiaco/doc-gen/cmd/apidoc-gen@$APIDOC_VERSION"`, "include-hidden-files: true"} {
		if !strings.Contains(string(workflow.Data), want) {
			t.Errorf("workflow missing %q", want)
		}
	}
	if _, err := Workflow(root, "dev"); err == nil {
		t.Fatal("development version should not become an unusable CI pin")
	}
	other := filepath.Join(repo, "other")
	os.Mkdir(other, 0o755)
	write(t, filepath.Join(other, "go.mod"), []byte("module example/other\ngo 1.25.0\n"))
	if _, err := Locate(repo); err == nil {
		t.Fatal("ambiguous module selection should ask for project root")
	}
}

// TestSourceFlowVariantsFactoriesAndCallSiteValues 验证泛型、闭包、多个响应分支和固定键对象
func TestSourceFlowVariantsFactoriesAndCallSiteValues(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "flows.go"), []byte(`package source
import "github.com/gin-gonic/gin"

type routeSpec struct {Path string; Handler gin.HandlerFunc}
func mount(g *gin.RouterGroup,s routeSpec){g.GET(s.Path,s.Handler)}
func wrap[T any](data T) Response[T]{return Response[T]{Data:data}}
func empty[T any]() Response[T]{var result Response[T];return result}
func factory(data any)gin.HandlerFunc{return func(c *gin.Context){c.JSON(200,wrap(data))}}
func prepared(e *gin.Engine)*gin.RouterGroup{g:=e.Group("/prepared");g.GET("/first",Missing);return g}
// Outer 外层响应
type Outer struct {Inner Response[any] `+"`json:\"inner\"`"+` /* 内层响应 */}
// Scalar 状态文本
type Scalar string
// QueryScalar 查询条件
type QueryScalar struct {Status Scalar `+"`form:\"status\" binding:\"oneof=active disabled\"`"+` /* 状态 */}
// DateAlias 日期
// apidoc:type string
// apidoc:format date
type DateAlias = Day

func AdditionalRoutes(engine *gin.Engine){
 prepared(engine).GET("/second",Missing)
 g:=engine.Group("/flow")
 mount(g,routeSpec{"/one",factory(Order{})})
 mount(g,routeSpec{"/two",factory([]Order{})})
 g.GET("/empty-order",func(c *gin.Context){c.JSON(200,empty[Order]())})
 g.GET("/empty-request",func(c *gin.Context){c.JSON(200,empty[CreateOrderRequest]())})
 g.GET("/branch",func(c *gin.Context){
  var data any
  if c.Query("view")=="list" {data=[]Order{}}else{data=Order{}}
  respond(c,data)
 })
 g.GET("/nested",func(c *gin.Context){c.JSON(200,Outer{Inner:Response[any]{Data:Order{}}})})
 g.GET("/alias",func(c *gin.Context){var day DateAlias;c.JSON(200,day)})
 g.GET("/query",func(c *gin.Context){var q QueryScalar;c.ShouldBindQuery(&q);c.JSON(200,q)})
 g.GET("/map",func(c *gin.Context){m:=gin.H{"known":Order{}};m[c.Query("key")]=1;c.JSON(200,m)})
}
`))
	if _, _, _, err := Initialize(root, false, "", ""); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	json.Unmarshal(result.OpenAPI, &spec)
	paths := spec["paths"].(map[string]any)
	for _, path := range []string{"/flow/one", "/flow/two", "/flow/branch", "/flow/nested", "/flow/alias", "/flow/query", "/flow/map", "/prepared/first", "/prepared/second"} {
		if paths[path] == nil {
			t.Errorf("missing %s", path)
		}
	}
	response := func(path string) string {
		data, _ := json.Marshal(paths[path].(map[string]any)["get"].(map[string]any)["responses"])
		return string(data)
	}
	if !strings.Contains(response("/flow/empty-order"), "Response_fixture_source_Order") || !strings.Contains(response("/flow/empty-request"), "Response_fixture_source_CreateOrderRequest") {
		t.Fatalf("generic return types were not instantiated independently: %s / %s", response("/flow/empty-order"), response("/flow/empty-request"))
	}
	if !strings.Contains(response("/flow/two"), `"type":"array"`) {
		t.Fatalf("factory capture: %s", response("/flow/two"))
	}
	if !strings.Contains(response("/flow/branch"), `"anyOf"`) {
		t.Fatalf("branch variants: %s", response("/flow/branch"))
	}
	if strings.Contains(response("/flow/nested"), "unresolved") {
		t.Fatalf("nested interface was not resolved: %s", response("/flow/nested"))
	}
	if !strings.Contains(response("/flow/alias"), `"format":"date"`) {
		t.Fatalf("alias: %s", response("/flow/alias"))
	}
	if !strings.Contains(response("/flow/map"), `"known"`) || !strings.Contains(response("/flow/map"), "x-apidoc-unresolved") {
		t.Fatalf("partial map: %s", response("/flow/map"))
	}
	for _, issue := range result.Report.Issues {
		for _, operation := range issue.Operations {
			if operation == "GET /flow/nested" && strings.HasPrefix(issue.Code, "schema.") {
				t.Fatalf("nested shape unresolved: %+v", issue)
			}
		}
	}
}

// TestInvalidAnnotationsAndConflictsDoNotReplaceArtifacts 验证无效注释或接口冲突不会替换上次成功生成的文件
func TestInvalidAnnotationsAndConflictsDoNotReplaceArtifacts(t *testing.T) {
	for _, test := range []struct{ name, extra, want string }{
		{"unknown type", "// Broken 文档错误\n// apidoc:response 200 Nonexistent\nfunc Broken(c *gin.Context){}\nfunc Extra(e *gin.Engine){e.GET(\"/broken\",Broken)}", "cannot resolve Go type"},
		{"wrong directive", "// apidoc:respone 200 Order\nfunc Broken(c *gin.Context){}", "invalid apidoc:respone"},
		{"duplicate route", "func Extra(e *gin.Engine){e.GET(\"/api/orders/:id\",Handler{}.Get)}", "duplicate route"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			if _, _, _, err := Initialize(root, false, "", ""); err != nil {
				t.Fatal(err)
			}
			good, err := Generate(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := good.Write(root); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(root, "broken.go"), []byte("package source\nimport \"github.com/gin-gonic/gin\"\n"+test.extra))
			if _, err := Generate(root); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("wanted %q, got %v", test.want, err)
			}
			retained, _ := os.ReadFile(filepath.Join(root, OutputDir, "openapi.json"))
			if !bytes.Equal(retained, good.OpenAPI) {
				t.Fatal("failed generation replaced successful output")
			}
		})
	}
}

// TestSharedMissingDescriptionsAreGroupedAndIncludeCanAdoptExisting 验证显式选择存量接口及共享模型问题合并
func TestSharedMissingDescriptionsAreGroupedAndIncludeCanAdoptExisting(t *testing.T) {
	root := fixture(t)
	path := filepath.Join(root, "main.go")
	data, _ := os.ReadFile(path)
	source := strings.Replace(string(data), "// AmountCents 订单实付金额\n\t// 单位：分，已扣除优惠金额", "", 1)
	write(t, path, []byte(source))
	if _, _, _, err := Initialize(root, true, "", ""); err != nil {
		t.Fatal(err)
	}
	source = strings.Replace(source, `group.GET("/orders/:id", h.Get)`, `// apidoc:include`+"\n"+`group.GET("/orders/:id", h.Get)`, 1)
	source = strings.Replace(source, `group.POST("/orders", Create)`, `// apidoc:include`+"\n"+`group.POST("/orders", Create)`, 1)
	write(t, path, []byte(source))
	result, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Summary.Generated != 2 {
		t.Fatalf("include: %+v", result.Report.Summary)
	}
	found := 0
	for _, issue := range result.Report.Issues {
		if issue.Code == "description.field_missing" && strings.Contains(issue.Message, "AmountCents") {
			found++
			if len(issue.Operations) != 2 {
				t.Fatalf("affected operations: %+v", issue)
			}
		}
	}
	if found != 1 {
		t.Fatalf("expected one shared model issue, got %d", found)
	}
}

// TestSerializationMatchesEmbeddedFieldsStringTagsAndByteSlices 验证嵌入字段、字符串编码标签和字节切片
func TestSerializationMatchesEmbeddedFieldsStringTagsAndByteSlices(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "encoding.go"), []byte(`package source
import "github.com/gin-gonic/gin"
// Base 基础模型
type Base struct {ID int64 `+"`json:\"id\"`"+` /* 基础标识 */; Label string `+"`json:\"label\"`"+` /* 标签 */}
// Encoded 输出模型
type Encoded struct {
 Base
 ID string `+"`json:\"id\"`"+` // 外部标识
 Number int64 `+"`json:\"number,string\"`"+` // 字符串形式的整数
 Bytes []byte `+"`json:\"bytes\"`"+` // 原始字节
}

func EncodingRoute(e *gin.Engine){e.GET("/encoded",func(c *gin.Context){c.JSON(200,Encoded{})})}
`))
	if _, _, _, err := Initialize(root, false, "", ""); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	json.Unmarshal(result.OpenAPI, &spec)
	model := spec["components"].(map[string]any)["schemas"].(map[string]any)["fixture_source_Encoded"].(map[string]any)
	properties := model["properties"].(map[string]any)
	if len(properties) != 4 {
		t.Fatalf("promoted fields: %#v", properties)
	}
	for _, field := range []string{"id", "number", "bytes"} {
		if properties[field].(map[string]any)["type"] != "string" {
			t.Fatalf("encoded field %s: %#v", field, properties[field])
		}
	}
	if properties["bytes"].(map[string]any)["format"] != "byte" {
		t.Fatal("byte slice was not documented as base64")
	}
}

// TestRouteCommentsStayAttachedAndGETBindingUsesQuery 验证路由注释只对对应路由生效，以及 GET 绑定使用查询参数
func TestRouteCommentsStayAttachedAndGETBindingUsesQuery(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "comments.go"), []byte(`package source
import "github.com/gin-gonic/gin"
// Filters 查询条件
type Filters struct {
 Term string `+"`form:\"term\" binding:\"gt=2,lt=5\"`"+` // 关键词
 Untouched string `+"`form:\"untouched\" validate:\"required,max=1\"`"+` // 未启用的校验器标签
}
// QueryAuto 查询接口
func QueryAuto(c *gin.Context){var q Filters;c.ShouldBind(&q);c.JSON(200,Order{})}
func CommentRoutes(e *gin.Engine){
 e.GET("/hidden",Missing) // apidoc:ignore
 e.GET("/shown",Missing)
 e.GET("/query",QueryAuto)
}
`))
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
	paths := spec["paths"].(map[string]any)
	if paths["/hidden"] != nil || paths["/shown"] == nil {
		t.Fatalf("route comment leaked to its neighbor: %#v", paths)
	}
	query := paths["/query"].(map[string]any)["get"].(map[string]any)
	params := query["parameters"].([]any)
	if len(params) != 2 || query["requestBody"] != nil {
		t.Fatalf("GET binding: %#v", query)
	}
	for _, value := range params {
		param := value.(map[string]any)
		schema := param["schema"].(map[string]any)
		if param["in"] != "query" {
			t.Fatalf("unexpected parameter location: %#v", param)
		}
		if param["name"] == "term" && (schema["minLength"] != float64(3) || schema["maxLength"] != float64(4)) {
			t.Fatalf("exclusive length bounds: %#v", schema)
		}
		if param["name"] == "untouched" && (param["required"] == true || schema["maxLength"] != nil) {
			t.Fatalf("unused validator tag affected documentation: %#v", param)
		}
	}
}

// TestUnknownRoutesWarnWithoutBlockingInitializationOrGeneration 验证未知路由产生提示但不使初始化和生成失败
func TestUnknownRoutesWarnWithoutBlockingInitializationOrGeneration(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "dynamic.go"), []byte(`package source
import "github.com/gin-gonic/gin"
var configuredPath string
func DynamicRoute(e *gin.Engine){e.GET(configuredPath,Missing)}
`))
	state, created, diagnostics, err := Initialize(root, true, "", "")
	if err != nil || !created || len(diagnostics) != 1 || len(state.Baseline) != 6 {
		t.Fatalf("initialization: %#v, %t, %#v, %v", state, created, diagnostics, err)
	}
	result, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Summary.Generated != 0 || len(result.Report.Issues) != 1 || result.Report.Issues[0].Code != "route.dynamic_path" {
		t.Fatalf("unresolved route report: %+v", result.Report)
	}
}

// TestDescriptionPrecedenceAndDatabaseTagsDoNotDefineAPIConstraints 验证描述优先级，以及数据库标签不决定接口约束
func TestDescriptionPrecedenceAndDatabaseTagsDoNotDefineAPIConstraints(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "descriptions.go"), []byte(`package source
import "github.com/gin-gonic/gin"
// Documented 业务输入和输出
type Documented struct {
 // Tagged 被显式描述覆盖的前注释
 Tagged string `+"`json:\"tagged\" description:\"显式字段说明\" gorm:\"not null;default:db;comment:被覆盖的数据库说明\"`"+` // 被覆盖的行尾说明
 // Leading 字段前业务说明
 Leading string `+"`json:\"leading\" gorm:\"comment:被覆盖的数据库说明\"`"+` // 被覆盖的行尾说明
 Tail string `+"`json:\"tail\" gorm:\"comment:被覆盖的数据库说明\"`"+` // 行尾业务说明
 Database string `+"`json:\"database\" gorm:\"not null;default:db;comment:数据库业务说明\"`"+`
}
// DescriptionHandler 描述优先级示例
func DescriptionHandler(c *gin.Context){var dto Documented;c.ShouldBindJSON(&dto);c.JSON(200,dto)}
func DescriptionRoutes(e *gin.Engine){e.POST("/descriptions",DescriptionHandler)}
`))
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
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	input := schemas["fixture_source_DocumentedInput"].(map[string]any)
	if input["required"] != nil {
		t.Fatalf("database nullability became input requirements: %#v", input)
	}
	properties := input["properties"].(map[string]any)
	for field, description := range map[string]string{"tagged": "显式字段说明", "leading": "字段前业务说明", "tail": "行尾业务说明", "database": "数据库业务说明"} {
		property := properties[field].(map[string]any)
		if property["description"] != description || property["default"] != nil {
			t.Errorf("field %s: %#v", field, property)
		}
	}
	if strings.Contains(string(result.OpenAPI), "被覆盖") || strings.Contains(string(result.OpenAPI), "被显式描述覆盖") {
		t.Fatal("lower priority descriptions leaked into the document")
	}
}
