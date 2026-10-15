package analyzer

import "github.com/topiaco/doc-gen/internal/ir"

// refineType 只用具体值确定动态结构，已声明的标量类型和源码指令继续生效
func refineType(declared, actual ir.TypeRef) ir.TypeRef {
	if declared.SchemaType != "" || actual.Kind == ir.TypeUnknown || containsTypeParameter(actual) {
		return declared
	}
	result := declared
	switch declared.Kind {
	case ir.TypeUnknown, ir.TypeInterface, ir.TypeParameter:
		result = actual
	case ir.TypePointer:
		if declared.Element != nil {
			if actual.Kind == ir.TypePointer && actual.Element != nil {
				actual = *actual.Element
			}
			element := refineType(*declared.Element, actual)
			result.Element = &element
		}
	case ir.TypeArray, ir.TypeSlice:
		if declared.Element != nil && actual.Element != nil {
			element := refineType(*declared.Element, *actual.Element)
			result.Element = &element
		}
	case ir.TypeNamed, ir.TypeStruct, ir.TypeMap:
		if actual.Kind == ir.TypeStruct || actual.Kind == ir.TypeUnion {
			result = actual
		}
	}
	if declared.Description != "" {
		result.Description = declared.Description
	}
	if declared.Format != "" {
		result.Format = declared.Format
	}
	return result
}
