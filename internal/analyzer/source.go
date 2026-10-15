package analyzer

import (
	"fmt"
	"go/ast"
	"go/types"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/topiaco/doc-gen/internal/ir"
	"golang.org/x/tools/go/packages"
)

// SourceAnalyzer 在路由发现、存量快照和接口分析之间共享同一次源码加载结果
type SourceAnalyzer struct {
	Program  *Program
	handlers map[string]*sourceValue
}

// NewSourceAnalyzer 创建共享同一份源码信息的路由与 Handler 分析器
func NewSourceAnalyzer(program *Program) *SourceAnalyzer {
	return &SourceAnalyzer{Program: program, handlers: map[string]*sourceValue{}}
}

// Discover 按函数调用关系和实参确定路由前缀与 Handler，无法确定路径时记录源码位置
func (a *SourceAnalyzer) Discover() RouteDiscovery {
	e := newSourceEngine(a.Program, "routes")
	// 辅助函数需要使用调用处的实参，因此先分析没有被其他函数直接调用的函数声明
	called := map[*types.Func]bool{}
	functions := make([]*sourceFunction, 0, len(e.functions))
	for _, fn := range e.functions {
		functions = append(functions, fn)
		ast.Inspect(fn.body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if obj, ok := expressionObject(fn.pkg.TypesInfo, call.Fun).(*types.Func); ok {
					called[obj.Origin()] = true
				}
			}
			return true
		})
	}
	sort.Slice(functions, func(i, j int) bool { return functions[i].pos < functions[j].pos })
	for _, fn := range functions {
		if called[fn.object] {
			continue
		}
		args := make([]*sourceValue, fn.signature.Params().Len())
		for i := range args {
			args[i] = e.value(fn.signature.Params().At(i).Type())
		}
		receiver := (*sourceValue)(nil)
		if fn.signature.Recv() != nil {
			receiver = e.value(fn.signature.Recv().Type())
		}
		e.invoke(&sourceValue{function: fn, receiver: receiver}, args)
	}
	result := RouteDiscovery{}
	for _, event := range e.events {
		methods := []string{event.name}
		offset := 0
		switch event.name {
		case "Handle":
			if len(event.args) < 3 {
				continue
			}
			method, ok := stringValue(event.args[0])
			if !ok {
				e.warning("route.dynamic_method", "route method cannot be determined", event.call.Pos())
				continue
			}
			methods = []string{strings.ToUpper(method)}
			offset = 1
		case "Any":
			methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
		}
		if len(event.args) < offset+2 {
			continue
		}
		path, exact := stringValue(event.args[offset])
		if event.receiver.router == nil || !event.receiver.router.exact {
			e.warning("route.dynamic_group_path", "route group prefix cannot be determined", event.call.Pos())
			continue
		}
		if !exact {
			e.warning("route.dynamic_path", "route path cannot be determined", event.call.Pos())
			continue
		}
		path, err := ir.NormalizePath(joinRoutePath(event.receiver.router.path, path))
		if err != nil {
			e.warning("route.invalid_path", err.Error(), event.call.Pos())
			continue
		}
		doc := a.routeDocumentation(event.pkg, event.call)
		selection := ""
		for _, directive := range doc.Directives {
			if selection != "" && selection != directive.Name {
				a.Program.AnnotationErrors = append(a.Program.AnnotationErrors, ir.Diagnostic{Code: "annotation.conflict", Severity: ir.SeverityError, Message: "route has both include and ignore directives", Source: a.Program.SourceRef(directive.Pos)})
			}
			selection = directive.Name
		}
		handler := event.args[len(event.args)-1]
		ref := ir.SymbolRef{Source: a.Program.SourceRef(event.call.Args[len(event.call.Args)-1].Pos())}
		if handler.function != nil {
			ref = ir.SymbolRef{PackagePath: handler.function.pkg.PkgPath, Name: handler.function.name, Source: a.Program.SourceRef(handler.function.pos)}
		}
		for _, method := range methods {
			operation := ir.Operation{Method: method, Path: path, Handler: ref, Summary: doc.Text, Selection: selection, Confidence: ir.ConfidenceExact, Source: a.Program.SourceRef(event.call.Pos())}
			result.Operations = append(result.Operations, operation)
			a.handlers[operation.Key().String()] = handler
		}
	}
	for _, diagnostic := range e.diagnostics {
		if strings.HasPrefix(diagnostic.Code, "route.") {
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
	}
	sort.Slice(result.Operations, func(i, j int) bool { return result.Operations[i].Key().String() < result.Operations[j].Key().String() })
	return result
}

// routeDocumentation 只读取紧邻路由的注释，并排除属于上一条语句的行尾注释
func (a *SourceAnalyzer) routeDocumentation(pkg *packages.Package, call *ast.CallExpr) Documentation {
	var doc Documentation
	start := a.Program.FileSet.Position(call.Pos()).Line
	end := a.Program.FileSet.Position(call.End()).Line
	for _, file := range pkg.Syntax {
		if call.Pos() < file.Pos() || call.End() > file.End() {
			continue
		}
		for _, group := range file.Comments {
			begin := a.Program.FileSet.Position(group.Pos()).Line
			finish := a.Program.FileSet.Position(group.End()).Line
			leading := finish == start-1 && !a.trailingComment(file, group)
			trailing := begin == end && group.Pos() >= call.End()
			if leading || trailing {
				next := a.Program.parseDocumentation(group, "", "route")
				if trailing && next.Text != "" {
					doc.Text = next.Text
				}
				doc.Directives = append(doc.Directives, next.Directives...)
			}
		}
	}
	return doc
}

// trailingComment 判断注释是否属于同一行中已经结束的语句
func (a *SourceAnalyzer) trailingComment(file *ast.File, group *ast.CommentGroup) bool {
	line := a.Program.FileSet.Position(group.Pos()).Line
	trailing := false
	ast.Inspect(file, func(node ast.Node) bool {
		if trailing || node == nil || node.Pos() > group.Pos() {
			return false
		}
		if _, ok := node.(ast.Stmt); ok && node.End() <= group.Pos() && a.Program.FileSet.Position(node.End()).Line == line {
			trailing = true
			return false
		}
		return true
	})
	return trailing
}

// Analyze 分析需要生成文档的接口，保留已知结构，并分别报告缺少描述和无法确定结构的问题
func (a *SourceAnalyzer) Analyze(operations []ir.Operation) ([]ir.Operation, error) {
	if len(a.Program.AnnotationErrors) > 0 {
		d := a.Program.AnnotationErrors[0]
		return nil, fmt.Errorf("%s: %s", d.Source, d.Message)
	}
	results := append([]ir.Operation{}, operations...)
	for i := range results {
		op := &results[i]
		if op.Mode != ir.ModeManaged {
			continue
		}
		handler := a.handlers[op.Key().String()]
		if handler == nil || handler.function == nil {
			op.Diagnostics = append(op.Diagnostics, ir.Diagnostic{Code: "handler.unresolved", Severity: ir.SeverityWarning, Message: "handler cannot be resolved", Source: op.Handler.Source})
		} else {
			e := newSourceEngine(a.Program, "handler")
			fn := handler.function
			doc := a.Program.Documentation(fn.pos)
			if doc.Text != "" {
				op.Summary, op.Description = splitSummary(doc.Text)
			}
			args := make([]*sourceValue, fn.signature.Params().Len())
			for j := range args {
				args[j] = e.value(fn.signature.Params().At(j).Type())
			}
			e.invoke(handler, args)
			for _, event := range e.events {
				a.applyEvent(op, e, event)
			}
			op.Diagnostics = append(op.Diagnostics, e.diagnostics...)
			if err := a.applyDirectives(op, fn, doc); err != nil {
				return nil, err
			}
		}
		for _, name := range pathParameterNames(op.Path) {
			found := false
			for _, param := range op.Parameters {
				if param.Location == ir.ParameterPath && param.Name == name {
					found = true
				}
			}
			if !found {
				op.Parameters = append(op.Parameters, ir.Parameter{Name: name, Location: ir.ParameterPath, Origin: "route.path", Required: true, Type: ir.TypeRef{Kind: ir.TypeBasic, Name: "string"}, Confidence: ir.ConfidenceExact, Source: op.Source})
			}
		}
		if op.Summary == "" {
			op.Diagnostics = append(op.Diagnostics, ir.Diagnostic{Code: "description.operation_missing", Severity: ir.SeverityWarning, Message: "interface has no business description", Source: op.Handler.Source})
		}
		if len(op.Responses) == 0 {
			op.Responses = []ir.Response{{Status: 0, Description: "无法从源码确定响应结构", Source: op.Handler.Source}}
			op.Diagnostics = append(op.Diagnostics, ir.Diagnostic{Code: "response.unknown", Severity: ir.SeverityWarning, Message: "response cannot be determined; add apidoc:response with an existing Go type", Source: op.Handler.Source})
		}
		sortOperationResponses(op)
	}
	return results, nil
}

// applyEvent 将源码中的 Gin 调用转换为请求参数、绑定类型或响应分支
func (a *SourceAnalyzer) applyEvent(op *ir.Operation, e *sourceEngine, event *sourceEvent) {
	source := a.Program.SourceRef(event.call.Pos())
	args := event.args
	if reader, ok := ginParameterReaders[event.name]; ok && len(args) > 0 {
		name, known := stringValue(args[0])
		if !known {
			op.Diagnostics = append(op.Diagnostics, ir.Diagnostic{Code: "parameter.dynamic_name", Severity: ir.SeverityWarning, Message: "parameter name cannot be determined", Source: source})
			return
		}
		if reader.location == ir.ParameterPath {
			found := false
			for _, registered := range pathParameterNames(op.Path) {
				if name == registered {
					found = true
				}
			}
			if !found {
				op.Diagnostics = append(op.Diagnostics, ir.Diagnostic{Code: "parameter.unregistered_path", Severity: ir.SeverityWarning, Message: "handler reads an unregistered path parameter: " + name, Source: source})
				return
			}
		}
		typ := ir.TypeRef{Kind: ir.TypeBasic, Name: "string"}
		if event.parameterType != nil {
			typ = *event.parameterType
		}
		addSourceParameter(op, ir.Parameter{Name: name, Location: reader.location, Origin: "gin.Context." + event.name, Required: event.required || reader.location == ir.ParameterPath, Type: typ, Format: reader.format, Confidence: ir.ConfidenceExact, Source: source})
		return
	}
	switch event.name {
	case "BindJSON", "ShouldBindJSON", "ShouldBindBodyWithJSON":
		if len(args) == 0 {
			return
		}
		typ := bindingType(e, args[0])
		if op.RequestBody != nil {
			typ = mergeTypes(op.RequestBody.Type, typ)
		}
		op.RequestBody = &ir.RequestBody{ContentType: "application/json", Required: true, Type: typ, Confidence: ir.ConfidenceExact, Source: source}
	case "ShouldBindQuery", "BindQuery", "ShouldBindUri", "BindUri", "ShouldBindHeader", "BindHeader":
		if len(args) == 0 {
			return
		}
		location := ir.ParameterQuery
		tag := "form"
		if strings.Contains(event.name, "Uri") {
			location = ir.ParameterPath
			tag = "uri"
		}
		if strings.Contains(event.name, "Header") {
			location = ir.ParameterHeader
			tag = "header"
		}
		if structure, ok := underlying(args[0].typ).(*types.Struct); ok {
			for _, field := range bindingFields(a.Program, structure, tag) {
				name := fieldTagName(field, tag)
				if name == "-" {
					continue
				}
				addSourceParameter(op, ir.Parameter{Name: name, Location: location, Origin: "gin.Context." + event.name, Description: field.Description, Required: location == ir.ParameterPath || field.Required, Type: field.Type, Tag: field.Tag, Confidence: ir.ConfidenceExact, Source: field.Source})
			}
		}
	case "Bind", "ShouldBind":
		if op.Method == http.MethodGet {
			query := *event
			query.name = "ShouldBindQuery"
			a.applyEvent(op, e, &query)
			return
		}
		op.Diagnostics = append(op.Diagnostics, ir.Diagnostic{Code: "request.dynamic_binding", Severity: ir.SeverityWarning, Message: "binding depends on Content-Type; use a specific binder or apidoc:request", Source: source})
	case "JSON", "IndentedJSON", "PureJSON", "SecureJSON", "AbortWithStatusJSON", "JSONP":
		if len(args) < 2 {
			return
		}
		status, known := intValue(args[0])
		if !known {
			status = 0
			op.Diagnostics = append(op.Diagnostics, ir.Diagnostic{Code: "response.dynamic_status", Severity: ir.SeverityWarning, Message: "HTTP status cannot be determined", Source: source})
		}
		typ := e.concreteType(args[1])
		confidence := ir.ConfidenceExact
		if containsDynamic(typ) || typ.CustomEncoding {
			confidence = ir.ConfidenceUnknown
		}
		mergeOperationResponse(op, ir.Response{Status: status, Description: responseDescription(status), Content: []ir.ResponseContent{{MediaType: "application/json", Type: &typ, Confidence: confidence}}, Source: source})
	case "Status", "AbortWithStatus":
		if len(args) < 1 {
			return
		}
		status, ok := intValue(args[0])
		if !ok {
			status = 0
		}
		mergeOperationResponse(op, ir.Response{Status: status, Description: responseDescription(status), Source: source})
	case "String", "Data":
		if len(args) < 2 {
			return
		}
		status, _ := intValue(args[0])
		media := "text/plain"
		typ := ir.TypeRef{Kind: ir.TypeBasic, Name: "string"}
		binary := false
		if event.name == "Data" {
			media, _ = stringValue(args[1])
			if media == "" {
				media = "application/octet-stream"
			}
			binary = true
		}
		mergeOperationResponse(op, ir.Response{Status: status, Description: responseDescription(status), Content: []ir.ResponseContent{{MediaType: media, Type: &typ, Binary: binary, Confidence: ir.ConfidenceExact}}, Source: source})
	case "File", "FileAttachment":
		mergeOperationResponse(op, ir.Response{Status: 200, Description: "OK", Content: []ir.ResponseContent{{MediaType: "application/octet-stream", Binary: true, Confidence: ir.ConfidenceInferred}}, Source: source})
	case "Redirect":
		if len(args) < 2 {
			return
		}
		status, _ := intValue(args[0])
		location, ok := stringValue(args[1])
		confidence := ir.ConfidenceExact
		if !ok {
			confidence = ir.ConfidenceUnknown
		}
		mergeOperationResponse(op, ir.Response{Status: status, Description: responseDescription(status), RedirectLocation: location, RedirectConfidence: confidence, Source: source})
	}
}

// responseDescription 返回 HTTP 状态说明，状态未知时明确说明原因
func responseDescription(status int) string {
	if status == 0 {
		return "无法从源码确定响应状态码"
	}
	return http.StatusText(status)
}

// bindingType 去除绑定目标的指针层级，读取实际请求类型
func bindingType(e *sourceEngine, v *sourceValue) ir.TypeRef {
	t := v.typ
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	return TypeRefFromGoType(e.program, t, false)
}

// bindingFields 根据绑定结构体读取参数字段及说明，JSON 忽略规则不影响其他绑定方式
func bindingFields(program *Program, structure *types.Struct, tag string) []ir.Field {
	// JSON exclusion must not hide URI/query/header fields
	var fields []ir.Field
	for i := 0; i < structure.NumFields(); i++ {
		field := structure.Field(i)
		if !field.Exported() {
			continue
		}
		if field.Anonymous() {
			if nested, ok := underlying(field.Type()).(*types.Struct); ok {
				fields = append(fields, bindingFields(program, nested, tag)...)
				continue
			}
		}
		raw := structure.Tag(i)
		typ := TypeRefFromGoType(program, field.Type(), false)
		doc := program.Documentation(field.Pos())
		doc.Text = ""
		typ = applyTypeDocumentation(typ, doc)
		fields = append(fields, ir.Field{Name: field.Name(), Type: typ, Tag: raw, Description: fieldDescription(program, field, raw), Required: bindingRequired(reflect.StructTag(raw).Get("binding")), Source: program.SourceRef(field.Pos())})
	}
	return fields
}

// addSourceParameter 按参数位置和名称合并同一参数的分析结果
func addSourceParameter(op *ir.Operation, param ir.Parameter) {
	for i, existing := range op.Parameters {
		if existing.Name == param.Name && existing.Location == param.Location {
			param.Required = param.Required || existing.Required
			if param.Description == "" {
				param.Description = existing.Description
			}
			op.Parameters[i] = param
			return
		}
	}
	op.Parameters = append(op.Parameters, param)
}

// applyDirectives 在 Handler 所在包的作用域内解析类型，只修改指令指定的请求体或响应状态码
func (a *SourceAnalyzer) applyDirectives(op *ir.Operation, fn *sourceFunction, doc Documentation) error {
	for _, d := range doc.Directives {
		status := 0
		expression := d.Value
		if d.Name == "response" {
			statusText, typeText := splitDirective(d.Value)
			if typeText == "" {
				return fmt.Errorf("%s: expected apidoc:response STATUS GoType", a.Program.SourceRef(d.Pos))
			}
			var err error
			status, err = strconv.Atoi(statusText)
			if err != nil || status < 100 || status > 599 {
				return fmt.Errorf("%s: invalid response status %q", a.Program.SourceRef(d.Pos), statusText)
			}
			expression = typeText
		}
		if d.Name != "request" && d.Name != "response" {
			continue
		}
		evaluated, err := types.Eval(a.Program.FileSet, fn.pkg.Types, fn.pos, expression)
		if err != nil || !evaluated.IsType() {
			return fmt.Errorf("%s: cannot resolve Go type %q", a.Program.SourceRef(d.Pos), expression)
		}
		typ := TypeRefFromGoType(a.Program, evaluated.Type, false)
		if d.Name == "request" {
			op.RequestBody = &ir.RequestBody{ContentType: "application/json", Required: true, Type: typ, Confidence: ir.ConfidenceOverride, Source: a.Program.SourceRef(d.Pos)}
			diagnostics := op.Diagnostics[:0]
			for _, diagnostic := range op.Diagnostics {
				if diagnostic.Code != "request.dynamic_binding" {
					diagnostics = append(diagnostics, diagnostic)
				}
			}
			op.Diagnostics = diagnostics
		} else {
			responses := op.Responses[:0]
			for _, response := range op.Responses {
				if response.Status != status {
					responses = append(responses, response)
				}
			}
			op.Responses = responses
			mergeOperationResponse(op, ir.Response{Status: status, Description: responseDescription(status), Content: []ir.ResponseContent{{MediaType: "application/json", Type: &typ, Confidence: ir.ConfidenceOverride}}, Source: a.Program.SourceRef(d.Pos)})
		}
	}
	return nil
}
