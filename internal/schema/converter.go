// Package schema converts analysis types into OpenAPI schemas
package schema

import (
	"fmt"
	"go/types"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/swaggest/openapi-go/openapi3"

	"github.com/topiaco/doc-gen/internal/analyzer"
	"github.com/topiaco/doc-gen/internal/ir"
)

// Result 保存 Schema 转换结果
type Result struct {
	Schema      openapi3.SchemaOrRef // OpenAPI Schema
	Confidence  ir.Confidence        // Schema 可信度
	Diagnostics []ir.Diagnostic      // 转换诊断
}

// Builder 维护一次文档构建共享的 Schema 组件
type Builder struct {
	program              *analyzer.Program               // 已加载的源码与类型信息
	components           map[string]openapi3.SchemaOrRef // 已生成组件
	building             map[string]bool                 // 正在生成的组件
	componentConfidence  map[string]ir.Confidence        // 组件可信度
	componentDiagnostics map[string][]ir.Diagnostic
}

type converter struct {
	builder *Builder
	source  ir.SourceRef
	request bool
}

// NewBuilder 创建共享 Component 的 Schema 构建器
func NewBuilder(program *analyzer.Program) *Builder {
	return &Builder{
		program:              program,
		components:           make(map[string]openapi3.SchemaOrRef),
		building:             make(map[string]bool),
		componentConfidence:  make(map[string]ir.Confidence),
		componentDiagnostics: make(map[string][]ir.Diagnostic),
	}
}

// Build 按响应序列化规则转换类型，生成的组件由 Builder 统一保存
func (b *Builder) Build(value ir.TypeRef, source ir.SourceRef) (Result, error) {
	return (converter{builder: b, source: source}).convert(value)
}

// Components 返回当前已生成的组件
func (b *Builder) Components() map[string]openapi3.SchemaOrRef {
	return b.components
}

// convert 根据 TypeRef 类别选择基础转换规则
func (c converter) convert(value ir.TypeRef) (result Result, err error) {
	defer func() {
		if err == nil && value.Unresolved != "" {
			schema := editableSchema(&result.Schema)
			if schema.MapOfAnything == nil {
				schema.MapOfAnything = map[string]any{}
			}
			schema.MapOfAnything["x-apidoc-unresolved"] = value.Unresolved
			result.Confidence = ir.ConfidenceUnknown
			result.Diagnostics = append(result.Diagnostics, c.diagnostic("schema.dynamic_map", value.Unresolved))
		}
		if err == nil && (value.Format != "" || (value.Description != "" && (value.Kind != ir.TypeNamed || value.SchemaType != ""))) {
			schema := editableSchema(&result.Schema)
			if value.Format != "" {
				schema.WithFormat(value.Format)
			}
			if value.Description != "" {
				schema.WithDescription(value.Description)
			}
		}
	}()
	if value.SchemaType != "" {
		return exactSchema(openapi3.SchemaType(value.SchemaType), value.Format), nil
	}
	if value.CustomEncoding {
		return c.freeForm("schema.custom_encoding", "custom or dynamic serialization needs a source type annotation"), nil
	}
	switch value.Kind {
	case ir.TypeNull:
		result := Result{Schema: freeFormSchema(), Confidence: ir.ConfidenceExact}
		result.Schema.Schema.WithNullable(true).WithEnum(nil)
		return result, nil
	case ir.TypeUnion:
		result := Result{Schema: freeFormSchema(), Confidence: ir.ConfidenceExact}
		for _, variant := range value.Variants {
			converted, err := c.convert(variant)
			if err != nil {
				return Result{}, err
			}
			result.Schema.Schema.AnyOf = append(result.Schema.Schema.AnyOf, converted.Schema)
			result.Confidence = mergeConfidence(result.Confidence, converted.Confidence)
			result.Diagnostics = append(result.Diagnostics, converted.Diagnostics...)
		}
		c.builder.PrioritizeNonNullVariants(result.Schema.Schema.AnyOf)
		return result, nil
	case ir.TypeBasic:
		return c.basic(value.Name), nil
	case ir.TypePointer:
		return c.pointer(value)
	case ir.TypeSlice, ir.TypeArray:
		if value.Kind == ir.TypeSlice && value.Element != nil && value.Element.Kind == ir.TypeBasic && (value.Element.Name == "uint8" || value.Element.Name == "byte") {
			return exactSchema(openapi3.SchemaTypeString, "byte"), nil
		}
		return c.array(value)
	case ir.TypeMap:
		return c.mapSchema(value)
	case ir.TypeNamed:
		return c.named(value)
	case ir.TypeInterface:
		return c.freeForm("schema.free_form", "interface response uses a free-form schema"), nil
	case ir.TypeUnknown, ir.TypeParameter:
		return c.freeForm("schema.unknown_type", "type has no concrete schema"), nil
	case ir.TypeStruct:
		return c.structSchema(value.Fields)
	default:
		return c.freeForm("schema.unknown_type", "type has no concrete schema"), nil
	}
}

// basic 转换 Go 基础类型
func (c converter) basic(name string) Result {
	switch name {
	case "string":
		return exactSchema(openapi3.SchemaTypeString, "")
	case "bool", "boolean":
		return exactSchema(openapi3.SchemaTypeBoolean, "")
	case "int":
		return exactSchema(openapi3.SchemaTypeInteger, "")
	case "int8", "int16", "int32", "rune":
		return exactSchema(openapi3.SchemaTypeInteger, "int32")
	case "int64":
		return exactSchema(openapi3.SchemaTypeInteger, "int64")
	case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte":
		result := exactSchema(openapi3.SchemaTypeInteger, "")
		result.Schema.Schema.WithMinimum(0)
		return result
	case "float32":
		return exactSchema(openapi3.SchemaTypeNumber, "float")
	case "float64":
		return exactSchema(openapi3.SchemaTypeNumber, "double")
	case "integer":
		return exactSchema(openapi3.SchemaTypeInteger, "")
	case "number":
		return exactSchema(openapi3.SchemaTypeNumber, "")
	default:
		return c.freeForm("schema.unsupported_basic", "unsupported basic type uses a free-form schema")
	}
}

// pointer 将目标 Schema 标记为 nullable
func (c converter) pointer(value ir.TypeRef) (Result, error) {
	if value.Element == nil {
		return c.freeForm("schema.unknown_type", "pointer element type is unknown"), nil
	}
	result, err := c.convert(*value.Element)
	if err != nil {
		return Result{}, err
	}
	if result.Schema.Schema != nil {
		result.Schema.Schema.WithNullable(true)
		return result, nil
	}
	reference := result.Schema
	wrapper := openapi3.Schema{AllOf: []openapi3.SchemaOrRef{reference}}
	wrapper.WithNullable(true)
	result.Schema = openapi3.SchemaOrRef{Schema: &wrapper}
	return result, nil
}

// array 转换切片和定长数组
func (c converter) array(value ir.TypeRef) (Result, error) {
	if value.Element == nil {
		return c.freeForm("schema.unknown_type", "array element type is unknown"), nil
	}
	element, err := c.convert(*value.Element)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Schema:      typedSchema(openapi3.SchemaTypeArray),
		Confidence:  element.Confidence,
		Diagnostics: element.Diagnostics,
	}
	result.Schema.Schema.WithItems(element.Schema)
	if value.Kind == ir.TypeArray {
		result.Schema.Schema.WithMinItems(value.Length).WithMaxItems(value.Length)
	}
	return result, nil
}

// mapSchema 转换 Map 和动态 Map
func (c converter) mapSchema(value ir.TypeRef) (Result, error) {
	result := Result{Schema: typedSchema(openapi3.SchemaTypeObject), Confidence: ir.ConfidenceExact}
	if value.Element == nil {
		return c.freeForm("schema.unknown_type", "map element type is unknown"), nil
	}
	if isFreeFormType(*value.Element) {
		free := freeFormSchema()
		result.Schema.Schema.WithAdditionalProperties(*(&openapi3.SchemaAdditionalProperties{}).WithSchemaOrRef(free))
		result.Confidence = ir.ConfidenceUnknown
		result.Diagnostics = []ir.Diagnostic{c.diagnostic("schema.dynamic_map", "map values use a free-form schema")}
		return result, nil
	}

	element, err := c.convert(*value.Element)
	if err != nil {
		return Result{}, err
	}
	result.Schema.Schema.WithAdditionalProperties(*(&openapi3.SchemaAdditionalProperties{}).WithSchemaOrRef(element.Schema))
	result.Confidence = element.Confidence
	result.Diagnostics = element.Diagnostics
	return result, nil
}

// named 将内置序列化类型映射到协议类型，其余具名类型由共享组件表达
func (c converter) named(value ir.TypeRef) (Result, error) {
	if value.PackagePath == "time" && value.Name == "Time" {
		return exactSchema(openapi3.SchemaTypeString, "date-time"), nil
	}
	if value.PackagePath == "encoding/json" && value.Name == "Number" {
		return exactSchema(openapi3.SchemaTypeNumber, ""), nil
	}
	if value.PackagePath == "encoding/json" && value.Name == "RawMessage" {
		return c.freeForm("schema.raw_json", "raw JSON has no declared structure"), nil
	}
	if c.builder.program == nil {
		return c.freeForm("schema.named_type_deferred", "named type requires loaded source"), nil
	}
	return c.namedComponent(value)
}

// structSchema 将已确定的结构体字段、说明和约束转换为 Schema
func (c converter) structSchema(fields []ir.Field) (Result, error) {
	result := exactSchema(openapi3.SchemaTypeObject, "")
	for _, field := range fields {
		fieldConverter := c
		if field.Source.File != "" {
			fieldConverter.source = field.Source
		}
		fieldType := field.Type
		if strings.Contains(reflect.StructTag(field.Tag).Get("json"), ",string") {
			fieldType.SchemaType = "string"
			fieldType.Format = ""
		}
		fieldResult, err := fieldConverter.convert(fieldType)
		if err != nil {
			return Result{}, fmt.Errorf("convert field %s: %w", field.Name, err)
		}
		result.Confidence = mergeConfidence(result.Confidence, fieldResult.Confidence)
		result.Diagnostics = append(result.Diagnostics, fieldResult.Diagnostics...)

		if field.Description != "" {
			editableSchema(&fieldResult.Schema).WithDescription(field.Description)
		}
		required := field.Required
		// 请求字段是否必填由绑定校验决定，而响应字段是否始终存在由 JSON 省略规则决定
		if c.request {
			if err := c.builder.applyValidation(&fieldResult.Schema, field); err != nil {
				return Result{}, err
			}
		} else {
			tag := reflect.StructTag(field.Tag).Get("json")
			required = !strings.Contains(tag, ",omitempty") && !strings.Contains(tag, ",omitzero")
		}
		if field.Description == "" && field.Source.File != "" {
			result.Diagnostics = append(result.Diagnostics, fieldConverter.diagnostic("description.field_missing", "field has no business description: "+field.Name))
		}
		name := field.JSONName
		if name == "" {
			name = field.Name
		}
		result.Schema.Schema.WithPropertiesItem(name, fieldResult.Schema)
		if required {
			result.Schema.Schema.Required = append(result.Schema.Schema.Required, name)
		}
	}
	return result, nil
}

// namedComponent 生成具名类型组件并用引用处理循环
func (c converter) namedComponent(value ir.TypeRef) (Result, error) {
	name := ComponentName(value)
	// 同一 Go 模型的请求校验与响应省略规则不同，因此分别缓存组件
	if c.request {
		name += "Input"
	}
	reference := componentReference(name)
	// 递归类型的组件已经在生成中，因此返回组件引用，避免再次解析同一类型
	if c.builder.building[name] {
		return Result{Schema: reference, Confidence: ir.ConfidenceExact}, nil
	}
	// 使用缓存时同时返回已有问题记录，以便报告列出所有受影响接口
	if confidence, found := c.builder.componentConfidence[name]; found {
		return Result{Schema: reference, Confidence: confidence, Diagnostics: c.builder.componentDiagnostics[name]}, nil
	}

	c.builder.building[name] = true
	definitionType, err := c.namedDefinition(value)
	if err != nil {
		delete(c.builder.building, name)
		return Result{}, err
	}
	definition, err := c.convert(definitionType)
	delete(c.builder.building, name)
	if err != nil {
		return Result{}, err
	}
	if value.Description != "" {
		editableSchema(&definition.Schema).WithDescription(value.Description)
	}
	if value.Description == "" {
		if pkg := c.builder.program.Package(value.PackagePath); pkg != nil && len(pkg.Syntax) > 0 {
			if object := pkg.Types.Scope().Lookup(value.Name); object != nil {
				definition.Diagnostics = append(definition.Diagnostics, ir.Diagnostic{Code: "description.type_missing", Severity: ir.SeverityWarning, Message: "type has no business description: " + value.Name, Source: c.builder.program.SourceRef(object.Pos())})
			}
		}
	}
	c.builder.components[name] = definition.Schema
	c.builder.componentConfidence[name] = definition.Confidence
	c.builder.componentDiagnostics[name] = definition.Diagnostics
	return Result{
		Schema:      reference,
		Confidence:  definition.Confidence,
		Diagnostics: definition.Diagnostics,
	}, nil
}

// namedDefinition 解析具名类型及其泛型实例的底层结构
func (c converter) namedDefinition(value ir.TypeRef) (ir.TypeRef, error) {
	if len(value.Fields) > 0 {
		return ir.TypeRef{Kind: ir.TypeStruct, Fields: value.Fields}, nil
	}
	pkg := c.builder.program.Package(value.PackagePath)
	if pkg == nil || pkg.Types == nil {
		return ir.TypeRef{}, fmt.Errorf("package %s is not loaded", value.PackagePath)
	}
	object := pkg.Types.Scope().Lookup(value.Name)
	if object == nil {
		return ir.TypeRef{}, fmt.Errorf("type %s was not found in %s", value.Name, value.PackagePath)
	}
	named, ok := types.Unalias(object.Type()).(*types.Named)
	if !ok {
		return ir.TypeRef{}, fmt.Errorf("symbol %s.%s is not a named type", value.PackagePath, value.Name)
	}

	resolved := types.Type(named)
	if len(value.TypeArguments) > 0 {
		arguments := make([]types.Type, len(value.TypeArguments))
		for i, argument := range value.TypeArguments {
			argumentType, err := c.goType(argument)
			if err != nil {
				return ir.TypeRef{}, fmt.Errorf("resolve type argument %s: %w", argument.String(), err)
			}
			arguments[i] = argumentType
		}
		instance, err := types.Instantiate(types.NewContext(), named, arguments, true)
		if err != nil {
			return ir.TypeRef{}, fmt.Errorf("instantiate %s: %w", value.String(), err)
		}
		resolved = instance
	}
	return analyzer.TypeRefFromGoType(c.builder.program, resolved.Underlying(), false), nil
}

// goType 将泛型实参还原为 go/types 类型
func (c converter) goType(value ir.TypeRef) (types.Type, error) {
	switch value.Kind {
	case ir.TypeBasic:
		object := types.Universe.Lookup(value.Name)
		if object == nil {
			return nil, fmt.Errorf("basic type %s was not found", value.Name)
		}
		return object.Type(), nil
	case ir.TypePointer:
		element, err := c.goType(*value.Element)
		if err != nil {
			return nil, err
		}
		return types.NewPointer(element), nil
	case ir.TypeSlice:
		element, err := c.goType(*value.Element)
		if err != nil {
			return nil, err
		}
		return types.NewSlice(element), nil
	case ir.TypeArray:
		element, err := c.goType(*value.Element)
		if err != nil {
			return nil, err
		}
		return types.NewArray(element, value.Length), nil
	case ir.TypeMap:
		key, err := c.goType(*value.Key)
		if err != nil {
			return nil, err
		}
		element, err := c.goType(*value.Element)
		if err != nil {
			return nil, err
		}
		return types.NewMap(key, element), nil
	case ir.TypeNamed:
		pkg := c.builder.program.Package(value.PackagePath)
		if pkg == nil || pkg.Types == nil {
			return nil, fmt.Errorf("package %s is not loaded", value.PackagePath)
		}
		object := pkg.Types.Scope().Lookup(value.Name)
		if object == nil {
			return nil, fmt.Errorf("type %s was not found in %s", value.Name, value.PackagePath)
		}
		named, ok := types.Unalias(object.Type()).(*types.Named)
		if len(value.TypeArguments) == 0 {
			return object.Type(), nil
		}
		if !ok {
			return nil, fmt.Errorf("type %s.%s does not accept type arguments", value.PackagePath, value.Name)
		}
		arguments := make([]types.Type, len(value.TypeArguments))
		for i, argument := range value.TypeArguments {
			argumentType, err := c.goType(argument)
			if err != nil {
				return nil, err
			}
			arguments[i] = argumentType
		}
		return types.Instantiate(types.NewContext(), named, arguments, true)
	case ir.TypeInterface:
		return types.NewInterfaceType(nil, nil).Complete(), nil
	default:
		return nil, fmt.Errorf("type %s cannot be used as a generic argument", value.String())
	}
}

// ComponentName 返回具名类型实例的稳定组件名
func ComponentName(value ir.TypeRef) string {
	return sanitizeComponentName(componentToken(value))
}

// componentToken 保留泛型实参的容器差异
func componentToken(value ir.TypeRef) string {
	switch value.Kind {
	case ir.TypeNamed:
		name := value.PackagePath + "_" + value.Name
		for _, argument := range value.TypeArguments {
			name += "__" + componentToken(argument)
		}
		return name
	case ir.TypePointer:
		return "ptr_" + componentToken(*value.Element)
	case ir.TypeSlice:
		return "slice_" + componentToken(*value.Element)
	case ir.TypeArray:
		return "array_" + strconv.FormatInt(value.Length, 10) + "_" + componentToken(*value.Element)
	case ir.TypeMap:
		return "map_" + componentToken(*value.Key) + "_" + componentToken(*value.Element)
	default:
		return value.String()
	}
}

// sanitizeComponentName 将组件名限制为稳定的字母数字形式
func sanitizeComponentName(value string) string {
	var result strings.Builder
	lastUnderscore := false
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			result.WriteRune(character)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			result.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(result.String(), "_")
}

// componentReference 创建 OpenAPI Component 引用
func componentReference(name string) openapi3.SchemaOrRef {
	reference := openapi3.SchemaReference{}
	reference.WithRef("#/components/schemas/" + name)
	return openapi3.SchemaOrRef{SchemaReference: &reference}
}

// editableSchema 使用包含引用的 allOf Schema 保存字段说明和约束
func editableSchema(value *openapi3.SchemaOrRef) *openapi3.Schema {
	if value.Schema != nil {
		return value.Schema
	}
	reference := *value
	wrapper := openapi3.Schema{AllOf: []openapi3.SchemaOrRef{reference}}
	value.SchemaReference = nil
	value.Schema = &wrapper
	return value.Schema
}

// mergeConfidence 合并嵌套 Schema 的最低可信度
func mergeConfidence(left, right ir.Confidence) ir.Confidence {
	rank := map[ir.Confidence]int{
		ir.ConfidenceUnknown:  0,
		ir.ConfidenceInferred: 1,
		ir.ConfidenceOverride: 3,
		ir.ConfidenceExact:    4,
	}
	if rank[right] < rank[left] {
		return right
	}
	return left
}

// freeForm 创建未限定结构的 Schema，并记录无法确定结构的原因
func (c converter) freeForm(code, message string) Result {
	result := Result{
		Schema:      freeFormSchema(),
		Confidence:  ir.ConfidenceUnknown,
		Diagnostics: []ir.Diagnostic{c.diagnostic(code, message)},
	}
	result.Schema.Schema.MapOfAnything = map[string]any{"x-apidoc-unresolved": message}
	return result
}

// diagnostic 创建 Schema 转换诊断
func (c converter) diagnostic(code, message string) ir.Diagnostic {
	return ir.Diagnostic{Code: code, Severity: ir.SeverityWarning, Message: message, Source: c.source}
}

// exactSchema 创建具有确定类型和格式的 Schema
func exactSchema(schemaType openapi3.SchemaType, format string) Result {
	schema := typedSchema(schemaType)
	if format != "" {
		schema.Schema.WithFormat(format)
	}
	return Result{Schema: schema, Confidence: ir.ConfidenceExact}
}

// typedSchema 创建指定 OpenAPI 类型的 Schema
func typedSchema(schemaType openapi3.SchemaType) openapi3.SchemaOrRef {
	schema := openapi3.Schema{}
	schema.WithType(schemaType)
	return openapi3.SchemaOrRef{Schema: &schema}
}

// freeFormSchema 创建不限制字段结构的 Schema
func freeFormSchema() openapi3.SchemaOrRef {
	return openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
}

// isFreeFormType 判断类型是否直接表示动态值
func isFreeFormType(value ir.TypeRef) bool {
	return value.Kind == ir.TypeInterface || value.Kind == ir.TypeUnknown || value.Kind == ir.TypeParameter
}
