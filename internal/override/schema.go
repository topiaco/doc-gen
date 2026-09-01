// Package override validates and applies documentation overrides
package override

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/swaggest/openapi-go/openapi3"

	"github.com/topiaco/doc-gen/internal/config"
)

const componentRefPrefix = "#/components/schemas/"

type schemaIssue struct {
	code    string
	message string
}

// ApplySchemaPatch 将 Override 属性合并到 OpenAPI Schema
func ApplySchemaPatch(target *openapi3.SchemaOrRef, patch config.SchemaPatch) error {
	if patch.Ref != "" {
		*target = schemaReference(patch.Ref)
		if schemaPatchOnlyRef(patch) {
			return nil
		}
	}
	schema := editableSchema(target)
	if patch.Type != "" {
		schema.WithType(openapi3.SchemaType(patch.Type))
	}
	if patch.Format != "" {
		schema.WithFormat(patch.Format)
	}
	if patch.Description != "" {
		schema.WithDescription(patch.Description)
	}
	if patch.Nullable != nil {
		schema.WithNullable(*patch.Nullable)
	}
	if patch.Required != nil {
		schema.WithRequired(patch.Required...)
	}
	if patch.Minimum != nil {
		schema.WithMinimum(*patch.Minimum)
	}
	if patch.Maximum != nil {
		schema.WithMaximum(*patch.Maximum)
	}
	if patch.Default != nil {
		schema.WithDefault(patch.Default)
	}

	propertyNames := make([]string, 0, len(patch.Properties))
	for name := range patch.Properties {
		propertyNames = append(propertyNames, name)
	}
	sort.Strings(propertyNames)
	if len(propertyNames) > 0 && schema.Type == nil {
		schema.WithType(openapi3.SchemaTypeObject)
	}
	for _, name := range propertyNames {
		property := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
		if schema.Properties != nil {
			if existing, found := schema.Properties[name]; found {
				property = existing
			}
		}
		if err := ApplySchemaPatch(&property, patch.Properties[name]); err != nil {
			return fmt.Errorf("property %s: %w", name, err)
		}
		schema.WithPropertiesItem(name, property)
	}
	if patch.Items != nil {
		items := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
		if schema.Items != nil {
			items = *schema.Items
		}
		if err := ApplySchemaPatch(&items, *patch.Items); err != nil {
			return fmt.Errorf("items: %w", err)
		}
		schema.WithItems(items)
		if schema.Type == nil {
			schema.WithType(openapi3.SchemaTypeArray)
		}
	}
	if patch.AdditionalProperties != nil {
		additional := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
		if err := ApplySchemaPatch(&additional, *patch.AdditionalProperties); err != nil {
			return fmt.Errorf("additionalProperties: %w", err)
		}
		schema.WithAdditionalProperties(*(&openapi3.SchemaAdditionalProperties{}).WithSchemaOrRef(additional))
		if schema.Type == nil {
			schema.WithType(openapi3.SchemaTypeObject)
		}
	}
	if len(patch.Enum) > 0 {
		values := make([]any, len(patch.Enum))
		named := make([]map[string]any, len(patch.Enum))
		for i, item := range patch.Enum {
			values[i] = item.Value
			named[i] = map[string]any{"value": item.Value, "name": item.Name}
			if item.Description != "" {
				named[i]["description"] = item.Description
			}
		}
		schema.WithEnum(values...)
		if schema.MapOfAnything == nil {
			schema.MapOfAnything = make(map[string]any)
		}
		schema.MapOfAnything["x-apifox-enum"] = named
	}
	return nil
}

// cloneSchema 创建可安全修改的 Schema 副本
func cloneSchema(value openapi3.SchemaOrRef) (openapi3.SchemaOrRef, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return openapi3.SchemaOrRef{}, err
	}
	var clone openapi3.SchemaOrRef
	if err := json.Unmarshal(data, &clone); err != nil {
		return openapi3.SchemaOrRef{}, err
	}
	return clone, nil
}

// validateSchemaPatch 校验引用、字段路径和类型兼容性
func validateSchemaPatch(patch config.SchemaPatch, base openapi3.SchemaOrRef, components map[string]openapi3.SchemaOrRef, location string) []schemaIssue {
	var issues []schemaIssue
	resolvedBase := resolveSchema(base, components, map[string]bool{})
	baseType := openAPISchemaType(resolvedBase, components)
	if patch.Ref != "" {
		if _, err := referencedComponent(patch.Ref, components); err != nil {
			issues = append(issues, schemaIssue{code: "override.stale_ref", message: location + ": " + err.Error()})
		}
	}
	patchType, _ := schemaPatchType(patch, components)
	if baseType != "" && patchType != "" && baseType != patchType {
		issues = append(issues, schemaIssue{
			code:    "override.incompatible_type",
			message: fmt.Sprintf("%s changes source type %s to %s", location, baseType, patchType),
		})
	}

	baseProperties := schemaProperties(base, components, map[string]bool{})
	propertyNames := make([]string, 0, len(patch.Properties))
	for name := range patch.Properties {
		propertyNames = append(propertyNames, name)
	}
	sort.Strings(propertyNames)
	for _, name := range propertyNames {
		propertyBase := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
		if existing, found := baseProperties[name]; found {
			propertyBase = existing
		} else if len(baseProperties) > 0 {
			issues = append(issues, schemaIssue{
				code:    "override.orphan_property",
				message: fmt.Sprintf("%s property %q does not exist in source schema", location, name),
			})
		}
		issues = append(issues, validateSchemaPatch(patch.Properties[name], propertyBase, components, location+"."+name)...)
	}

	if patch.Items != nil {
		items := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
		if resolvedBase != nil && resolvedBase.Items != nil {
			items = *resolvedBase.Items
		}
		issues = append(issues, validateSchemaPatch(*patch.Items, items, components, location+".items")...)
	}
	if patch.AdditionalProperties != nil {
		additional := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
		if resolvedBase != nil && resolvedBase.AdditionalProperties != nil && resolvedBase.AdditionalProperties.SchemaOrRef != nil {
			additional = *resolvedBase.AdditionalProperties.SchemaOrRef
		}
		issues = append(issues, validateSchemaPatch(*patch.AdditionalProperties, additional, components, location+".additionalProperties")...)
	}

	knownProperties := make(map[string]struct{}, len(baseProperties)+len(patch.Properties))
	for name := range baseProperties {
		knownProperties[name] = struct{}{}
	}
	for name := range patch.Properties {
		knownProperties[name] = struct{}{}
	}
	if len(knownProperties) > 0 {
		for _, name := range patch.Required {
			if _, found := knownProperties[name]; !found {
				issues = append(issues, schemaIssue{
					code:    "override.orphan_property",
					message: fmt.Sprintf("%s required property %q is not defined", location, name),
				})
			}
		}
	}
	return issues
}

// schemaProperties 合并 Schema 与嵌入 allOf 的字段
func schemaProperties(value openapi3.SchemaOrRef, components map[string]openapi3.SchemaOrRef, visiting map[string]bool) map[string]openapi3.SchemaOrRef {
	if value.SchemaReference != nil {
		name := strings.TrimPrefix(value.SchemaReference.Ref, componentRefPrefix)
		if name == value.SchemaReference.Ref || visiting[name] {
			return nil
		}
		component, found := components[name]
		if !found {
			return nil
		}
		visiting[name] = true
		return schemaProperties(component, components, visiting)
	}
	if value.Schema == nil {
		return nil
	}
	properties := make(map[string]openapi3.SchemaOrRef)
	for _, embedded := range value.Schema.AllOf {
		for name, property := range schemaProperties(embedded, components, visiting) {
			properties[name] = property
		}
	}
	for name, property := range value.Schema.Properties {
		properties[name] = property
	}
	return properties
}

// schemaPatchType 返回 Override 明确表达的 Schema 类型
func schemaPatchType(patch config.SchemaPatch, components map[string]openapi3.SchemaOrRef) (string, error) {
	if patch.Type != "" {
		return patch.Type, nil
	}
	if patch.Ref != "" {
		name, err := referencedComponent(patch.Ref, components)
		if err != nil {
			return "", err
		}
		return openAPISchemaType(resolveSchema(components[name], components, map[string]bool{}), components), nil
	}
	if len(patch.Properties) > 0 || patch.AdditionalProperties != nil {
		return "object", nil
	}
	if patch.Items != nil {
		return "array", nil
	}
	return "", nil
}

// openAPISchemaType 读取 Schema 或其单一 allOf 引用的类型
func openAPISchemaType(value *openapi3.Schema, components map[string]openapi3.SchemaOrRef) string {
	if value == nil {
		return ""
	}
	if value.Type != nil {
		return string(*value.Type)
	}
	if len(value.AllOf) == 1 {
		return openAPISchemaType(resolveSchema(value.AllOf[0], components, map[string]bool{}), components)
	}
	return ""
}

// resolveSchema 解析本地 Component 引用
func resolveSchema(value openapi3.SchemaOrRef, components map[string]openapi3.SchemaOrRef, visiting map[string]bool) *openapi3.Schema {
	if value.Schema != nil {
		return value.Schema
	}
	if value.SchemaReference == nil {
		return nil
	}
	name := strings.TrimPrefix(value.SchemaReference.Ref, componentRefPrefix)
	if name == value.SchemaReference.Ref || visiting[name] {
		return nil
	}
	component, found := components[name]
	if !found {
		return nil
	}
	visiting[name] = true
	return resolveSchema(component, components, visiting)
}

// referencedComponent 校验并返回本地 Component 名称
func referencedComponent(ref string, components map[string]openapi3.SchemaOrRef) (string, error) {
	if !strings.HasPrefix(ref, componentRefPrefix) {
		return "", fmt.Errorf("reference %q must target components/schemas", ref)
	}
	name := strings.TrimPrefix(ref, componentRefPrefix)
	if _, found := components[name]; !found {
		return "", fmt.Errorf("reference %q does not exist", ref)
	}
	return name, nil
}

// editableSchema 包装引用以承载 Override 属性
func editableSchema(value *openapi3.SchemaOrRef) *openapi3.Schema {
	if value.Schema != nil {
		return value.Schema
	}
	if value.SchemaReference != nil {
		reference := *value
		value.SchemaReference = nil
		value.Schema = &openapi3.Schema{AllOf: []openapi3.SchemaOrRef{reference}}
		return value.Schema
	}
	value.Schema = &openapi3.Schema{}
	return value.Schema
}

// schemaReference 创建 Component Schema 引用
func schemaReference(ref string) openapi3.SchemaOrRef {
	reference := openapi3.SchemaReference{}
	reference.WithRef(ref)
	return openapi3.SchemaOrRef{SchemaReference: &reference}
}

// schemaPatchOnlyRef 判断 Patch 是否仅替换引用
func schemaPatchOnlyRef(patch config.SchemaPatch) bool {
	return patch.Type == "" && patch.Format == "" && patch.Description == "" && patch.Nullable == nil &&
		patch.Required == nil && len(patch.Properties) == 0 && patch.Items == nil && patch.AdditionalProperties == nil &&
		len(patch.Enum) == 0 && patch.Minimum == nil && patch.Maximum == nil && patch.Default == nil
}
