package analyzer

import (
	"go/types"

	"github.com/topiaco/doc-gen/internal/ir"
)

// selectedField 按 Go 的字段选择路径读取成员，嵌套字段赋值因此会更新所属对象
func (f *sourceFrame) selectedField(value *sourceValue, indices []int) *sourceValue {
	for _, index := range indices {
		value = indirectValue(value)
		structure, ok := underlying(value.typ).(*types.Struct)
		if !ok {
			return nil
		}
		field := structure.Field(index)
		if value.fields == nil {
			value.fields = map[string]*sourceValue{}
		}
		if value.fields[field.Name()] == nil {
			value.fields[field.Name()] = f.value(field.Type())
		}
		value = value.fields[field.Name()]
	}
	return value
}

// knownField 按字段声明位置查找已知值，因此嵌入字段在 JSON 提升后仍保留具体类型
func (e *sourceEngine) knownField(value *sourceValue, target ir.Field, seen map[*sourceValue]bool) *sourceValue {
	value = indirectValue(value)
	if seen[value] {
		return nil
	}
	seen[value] = true
	structure, ok := underlying(value.typ).(*types.Struct)
	if !ok {
		return nil
	}
	for i := 0; i < structure.NumFields(); i++ {
		field := structure.Field(i)
		member := value.fields[field.Name()]
		if member == nil {
			continue
		}
		if field.Name() == target.Name && e.program.SourceRef(field.Pos()) == target.Source {
			return member
		}
		if field.Anonymous() {
			if nested := e.knownField(member, target, seen); nested != nil {
				return nested
			}
		}
	}
	return nil
}
