package analyzer

import (
	"github.com/topiaco/doc-gen/internal/ir"
	"go/types"
	"reflect"
	"strings"
)

// TypeRefFromGoType 将 go/types 类型转换为中间类型引用
func TypeRefFromGoType(program *Program, value types.Type, expandNamed bool) (result ir.TypeRef) {
	if value == nil {
		return ir.TypeRef{Kind: ir.TypeUnknown}
	}
	if program != nil {
		doc := program.typeDocumentation(types.Unalias(value))
		if alias, ok := value.(*types.Alias); ok {
			own := program.typeDocumentation(value)
			if own.Text != "" {
				doc.Text = own.Text
				// 别名可以有独立业务说明，因此为具名类型的别名保留自己的组件身份
				defer func() {
					if result.Kind == ir.TypeNamed {
						result.Name = alias.Obj().Name()
						result.PackagePath = alias.Obj().Pkg().Path()
						result.TypeArguments = nil
						for i := 0; i < alias.TypeArgs().Len(); i++ {
							result.TypeArguments = append(result.TypeArguments, TypeRefFromGoType(program, alias.TypeArgs().At(i), false))
						}
					}
				}()
			}
			doc.Directives = append(doc.Directives, own.Directives...)
		}
		defer func() { result = applyTypeDocumentation(result, doc) }()
	}
	value = types.Unalias(value)
	switch value := value.(type) {
	case *types.Basic:
		if program != nil && value.Info()&types.IsUntyped != 0 && value.Kind() != types.UntypedNil {
			value = types.Default(value).(*types.Basic)
		}
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
	if program != nil && !(result.PackagePath == "time" && result.Name == "Time") && !(result.PackagePath == "encoding/json" && (result.Name == "RawMessage" || result.Name == "Number")) {
		methods := types.NewMethodSet(types.NewPointer(value))
		for i := 0; i < methods.Len(); i++ {
			method := methods.At(i).Obj()
			signature, ok := method.Type().(*types.Signature)
			if method.Name() == "MarshalJSON" && ok && signature.Params().Len() == 0 && signature.Results().Len() == 2 && types.Identical(signature.Results().At(0).Type(), types.NewSlice(types.Typ[types.Byte])) && types.Identical(signature.Results().At(1).Type(), types.Universe.Lookup("error").Type()) {
				result.CustomEncoding = true
			}
		}
	}
	return result
}

// fieldDescription 按 description、已合并的字段注释、GORM comment 的顺序读取说明
func fieldDescription(program *Program, field *types.Var, tag string) string {
	structureTag := reflect.StructTag(tag)
	if description := structureTag.Get("description"); description != "" {
		return description
	}
	if program != nil {
		if description := program.Documentation(field.Pos()).Text; description != "" {
			return description
		}
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

// bindingRequired 只读取字段本身的必填规则，dive 之后的 required 属于集合元素
func bindingRequired(tag string) bool {
	for _, rule := range strings.Split(tag, ",") {
		switch rule {
		case "dive", "omitempty", "omitnil":
			return false
		case "required":
			return true
		}
	}
	return false
}
