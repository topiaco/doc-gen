package analyzer

import (
	"go/ast"
	"go/types"
	"reflect"
	"strings"

	"github.com/topiaco/doc-gen/internal/ir"
)

var ginJSONBinders = map[string]struct{}{
	"BindJSON":       {},
	"ShouldBindJSON": {},
}

// AnalyzeJSONBodies 分析每个已定位 Handler 的直接 JSON 绑定调用
func AnalyzeJSONBodies(program *Program, resolutions []HandlerResolution) []HandlerResolution {
	results := make([]HandlerResolution, len(resolutions))
	copy(results, resolutions)
	for i := range results {
		if results[i].Declaration == nil || results[i].Diagnostic != nil {
			continue
		}
		analyzeHandlerJSONBody(program, &results[i])
	}
	return results
}

// analyzeHandlerJSONBody 将首个直接 JSON 绑定调用写入 Operation
func analyzeHandlerJSONBody(program *Program, result *HandlerResolution) {
	info := result.Package.TypesInfo
	ast.Inspect(result.Declaration.Body, func(node ast.Node) bool {
		if result.Operation.RequestBody != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok || !isJSONBindingCall(info, call) {
			return true
		}

		argument := call.Args[0]
		bodyType := TypeRefFromGoType(program, info.TypeOf(argument), true)
		confidence := ir.ConfidenceExact
		if isDynamicBodyType(bodyType) {
			confidence = ir.ConfidenceUnknown
			result.Operation.Diagnostics = append(result.Operation.Diagnostics, ir.Diagnostic{
				Code:     "request_body.dynamic_type",
				Severity: ir.SeverityWarning,
				Message:  "JSON request body has no concrete static type",
				Source:   program.SourceRef(argument.Pos()),
			})
		}
		result.Operation.RequestBody = &ir.RequestBody{
			ContentType: "application/json",
			Required:    true,
			Type:        bodyType,
			Confidence:  confidence,
			Source:      program.SourceRef(call.Pos()),
		}
		return false
	})
}

// isJSONBindingCall 判断调用是否为 Gin JSON Body 绑定
func isJSONBindingCall(info *types.Info, call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 || !isGinType(info.TypeOf(selector.X), "Context") {
		return false
	}
	_, found := ginJSONBinders[selector.Sel.Name]
	return found
}

// TypeRefFromGoType 将 go/types 类型转换为中间类型引用
func TypeRefFromGoType(program *Program, value types.Type, expandNamed bool) ir.TypeRef {
	if value == nil {
		return ir.TypeRef{Kind: ir.TypeUnknown}
	}
	value = types.Unalias(value)
	switch value := value.(type) {
	case *types.Basic:
		if value.Kind() == types.UntypedNil {
			return ir.TypeRef{Kind: ir.TypeUnknown}
		}
		return ir.TypeRef{Kind: ir.TypeBasic, Name: value.Name()}
	case *types.Pointer:
		element := TypeRefFromGoType(program, value.Elem(), expandNamed)
		return ir.TypeRef{Kind: ir.TypePointer, Element: &element}
	case *types.Slice:
		element := TypeRefFromGoType(program, value.Elem(), expandNamed)
		return ir.TypeRef{Kind: ir.TypeSlice, Element: &element}
	case *types.Array:
		element := TypeRefFromGoType(program, value.Elem(), expandNamed)
		return ir.TypeRef{Kind: ir.TypeArray, Element: &element, Length: value.Len()}
	case *types.Map:
		key := TypeRefFromGoType(program, value.Key(), false)
		element := TypeRefFromGoType(program, value.Elem(), expandNamed)
		return ir.TypeRef{Kind: ir.TypeMap, Key: &key, Element: &element}
	case *types.Named:
		return namedTypeRef(program, value, expandNamed)
	case *types.Struct:
		return ir.TypeRef{Kind: ir.TypeStruct, Fields: structFields(program, value)}
	case *types.Interface:
		return ir.TypeRef{Kind: ir.TypeInterface, Name: "any"}
	case *types.TypeParam:
		return ir.TypeRef{Kind: ir.TypeParameter, Name: value.Obj().Name()}
	default:
		return ir.TypeRef{Kind: ir.TypeUnknown}
	}
}

// namedTypeRef 转换具名类型并按需保留顶层字段元数据
func namedTypeRef(program *Program, value *types.Named, expand bool) ir.TypeRef {
	result := ir.TypeRef{Kind: ir.TypeNamed, Name: value.Obj().Name()}
	if value.Obj().Pkg() != nil {
		result.PackagePath = value.Obj().Pkg().Path()
	}
	for i := 0; i < value.TypeArgs().Len(); i++ {
		result.TypeArguments = append(result.TypeArguments, TypeRefFromGoType(program, value.TypeArgs().At(i), false))
	}
	if structure, ok := value.Underlying().(*types.Struct); expand && ok {
		result.Fields = structFields(program, structure)
	}
	return result
}

// structFields 提取可参与 JSON 编码的字段和必填标签
func structFields(program *Program, structure *types.Struct) []ir.Field {
	fields := make([]ir.Field, 0, structure.NumFields())
	for i := 0; i < structure.NumFields(); i++ {
		field := structure.Field(i)
		if !field.Exported() {
			continue
		}
		tag := structure.Tag(i)
		jsonName := jsonFieldName(field.Name(), reflect.StructTag(tag).Get("json"))
		if jsonName == "-" {
			continue
		}
		fields = append(fields, ir.Field{
			Name:        field.Name(),
			JSONName:    jsonName,
			Type:        TypeRefFromGoType(program, field.Type(), false),
			Tag:         tag,
			Description: fieldDescription(program, field, tag),
			Required:    requiredFieldTag(tag),
			Embedded:    field.Anonymous(),
			Confidence:  ir.ConfidenceExact,
			Source:      program.SourceRef(field.Pos()),
		})
	}
	return fields
}

// fieldDescription 按显式标签、行尾注释和 GORM 注释读取字段说明
func fieldDescription(program *Program, field *types.Var, tag string) string {
	structureTag := reflect.StructTag(tag)
	if description := structureTag.Get("description"); description != "" {
		return description
	}
	if description := program.FieldComment(field.Pos()); description != "" {
		return description
	}
	for _, item := range strings.Split(structureTag.Get("gorm"), ";") {
		if strings.HasPrefix(item, "comment:") {
			return strings.TrimPrefix(item, "comment:")
		}
	}
	return ""
}

// jsonFieldName 返回 JSON 标签指定的字段名
func jsonFieldName(defaultName, tag string) string {
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return defaultName
	}
	return name
}

// requiredFieldTag 判断 binding 或 validate 标签是否声明 required
func requiredFieldTag(tag string) bool {
	structureTag := reflect.StructTag(tag)
	return tagContains(structureTag.Get("binding"), "required") || tagContains(structureTag.Get("validate"), "required")
}

// tagContains 判断逗号分隔标签是否包含目标规则
func tagContains(tag, target string) bool {
	for _, item := range strings.Split(tag, ",") {
		if item == target {
			return true
		}
	}
	return false
}

// isDynamicBodyType 判断请求体顶层是否缺少具体静态类型
func isDynamicBodyType(value ir.TypeRef) bool {
	for value.Kind == ir.TypePointer {
		if value.Element == nil {
			return true
		}
		value = *value.Element
	}
	return value.Kind == ir.TypeUnknown || value.Kind == ir.TypeInterface || value.Kind == ir.TypeParameter
}
