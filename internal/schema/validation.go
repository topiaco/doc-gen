package schema

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/swaggest/openapi-go/openapi3"
	"github.com/topiaco/doc-gen/internal/ir"
)

// BuildRequest 按实际绑定校验生成输入结构，与响应序列化规则分别处理
func (b *Builder) BuildRequest(value ir.TypeRef, source ir.SourceRef) (Result, error) {
	return (converter{builder: b, source: source, request: true}).convert(value)
}

// BuildParameter 为查询、路径、请求头和表单参数应用相同的绑定约束
func (b *Builder) BuildParameter(param ir.Parameter) (Result, error) {
	result, err := b.BuildRequest(param.Type, param.Source)
	if err != nil {
		return result, err
	}
	if err := b.applyValidation(&result.Schema, ir.Field{Name: param.Name, Type: param.Type, Tag: param.Tag, Source: param.Source}); err != nil {
		return result, err
	}
	if param.Format != "" {
		editableSchema(&result.Schema).WithFormat(param.Format)
	}
	return result, nil
}

// applyValidation 只转换 Gin 实际读取的 binding 标签，不从数据库标签或描述猜测约束
func (b *Builder) applyValidation(value *openapi3.SchemaOrRef, field ir.Field) error {
	tag := reflect.StructTag(field.Tag)
	rules := tag.Get("binding")
	if rules == "" || strings.Contains(tag.Get("json"), ",string") {
		return nil
	}
	return b.applyValidationRules(value, field, strings.Split(rules, ","))
}

// applyValidationRules 按标签顺序转换约束，遇到 dive 时继续分析集合元素
func (b *Builder) applyValidationRules(value *openapi3.SchemaOrRef, field ir.Field, rules []string) error {
	schema := editableSchema(value)
	kind := b.schemaKind(*value, map[string]bool{})
	if kind == "" {
		typ := field.Type
		for typ.Element != nil && typ.Kind == ir.TypePointer {
			typ = *typ.Element
		}
		switch typ.Kind {
		case ir.TypeArray, ir.TypeSlice:
			kind = "array"
		case ir.TypeMap, ir.TypeStruct:
			kind = "object"
		case ir.TypeBasic:
			if typ.Name == "string" {
				kind = "string"
			} else {
				kind = "number"
			}
		}
	}
	for index, rule := range rules {
		pair := strings.SplitN(rule, "=", 2)
		name := pair[0]
		if name == "dive" {
			element := b.collectionElement(*value, map[string]bool{})
			if element != nil {
				copied := *element
				if copied.Schema != nil {
					definition := *copied.Schema
					copied.Schema = &definition
				}
				if err := b.applyValidationRules(&copied, field, rules[index+1:]); err != nil {
					return err
				}
				if kind == "array" {
					schema.WithItems(copied)
				} else {
					schema.WithAdditionalProperties(*(&openapi3.SchemaAdditionalProperties{}).WithSchemaOrRef(copied))
				}
			}
			return nil
		}
		if name == "email" || name == "uuid" {
			schema.WithFormat(name)
			continue
		}
		if len(pair) != 2 {
			continue
		}
		switch name {
		case "oneof":
			for _, item := range strings.Fields(pair[1]) {
				var entry any = item
				if kind == "number" || kind == "integer" {
					number, err := strconv.ParseFloat(item, 64)
					if err != nil {
						return fmt.Errorf("%s: invalid oneof value %q", field.Source, item)
					}
					entry = number
				}
				schema.Enum = append(schema.Enum, entry)
			}
		case "min", "max", "len", "gte", "gt", "lte", "lt":
			n, err := strconv.ParseFloat(pair[1], 64)
			if err != nil {
				return fmt.Errorf("%s: invalid validation %s", field.Source, rule)
			}
			lower := name == "min" || name == "gte" || name == "gt" || name == "len"
			upper := name == "max" || name == "lte" || name == "lt" || name == "len"
			length := int64(n)
			if name == "gt" {
				length++
			}
			if name == "lt" {
				length--
			}
			switch kind {
			case "string":
				if lower {
					schema.WithMinLength(length)
				}
				if upper {
					schema.WithMaxLength(length)
				}
			case "array":
				if lower {
					schema.WithMinItems(length)
				}
				if upper {
					schema.WithMaxItems(length)
				}
			case "object":
				if lower {
					schema.WithMinProperties(length)
				}
				if upper {
					schema.WithMaxProperties(length)
				}
			default:
				if lower {
					schema.WithMinimum(n)
				}
				if upper {
					schema.WithMaximum(n)
				}
				if name == "gt" {
					schema.WithExclusiveMinimum(true)
				}
				if name == "lt" {
					schema.WithExclusiveMaximum(true)
				}
			}
		}
	}
	return nil
}

// collectionElement 读取集合元素的 Schema，包括组件引用所表示的具名集合
func (b *Builder) collectionElement(value openapi3.SchemaOrRef, seen map[string]bool) *openapi3.SchemaOrRef {
	if value.Schema != nil {
		if value.Schema.Items != nil {
			return value.Schema.Items
		}
		if value.Schema.AdditionalProperties != nil {
			return value.Schema.AdditionalProperties.SchemaOrRef
		}
		for _, item := range value.Schema.AllOf {
			if element := b.collectionElement(item, seen); element != nil {
				return element
			}
		}
	}
	if value.SchemaReference != nil {
		ref := value.SchemaReference.Ref
		if !seen[ref] {
			seen[ref] = true
			return b.collectionElement(b.components[strings.TrimPrefix(ref, "#/components/schemas/")], seen)
		}
	}
	return nil
}

// schemaKind 通过 Schema 或组件引用确定约束作用于标量、数组还是对象
func (b *Builder) schemaKind(value openapi3.SchemaOrRef, seen map[string]bool) string {
	if value.Schema != nil {
		if value.Schema.Type != nil {
			return string(*value.Schema.Type)
		}
		for _, item := range value.Schema.AllOf {
			if kind := b.schemaKind(item, seen); kind != "" {
				return kind
			}
		}
	}
	if value.SchemaReference != nil {
		ref := value.SchemaReference.Ref
		if seen[ref] {
			return ""
		}
		seen[ref] = true
		return b.schemaKind(b.components[strings.TrimPrefix(ref, "#/components/schemas/")], seen)
	}
	return ""
}
