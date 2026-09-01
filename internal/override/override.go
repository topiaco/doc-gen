package override

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/swaggest/openapi-go/openapi3"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
	"github.com/topiaco/doc-gen/internal/schema"
)

// ValidEntry 表示一个通过校验的 Override 定义
type ValidEntry struct {
	Kind   string       // 定义类别
	Key    string       // 定义键
	Source ir.SourceRef // Override 文件
}

// Result 保存 Override 合并和校验结果
type Result struct {
	Operations         []ir.Operation                      // 合并元数据后的接口
	OperationOverrides map[string]config.OperationOverride // 已生效接口覆盖
	Schemas            map[string]openapi3.SchemaOrRef     // 已验证组件覆盖
	Valid              []ValidEntry                        // 有效定义
	Diagnostics        []ir.Diagnostic                     // Override 诊断
}

type operationDefinition struct {
	value  config.OperationOverride
	source ir.SourceRef
}

type schemaDefinition struct {
	value  config.SchemaPatch
	source ir.SourceRef
}

type operationBase struct {
	parameters map[string]openapi3.SchemaOrRef
	body       *openapi3.SchemaOrRef
	responses  map[int]responseBase
}

type responseBase struct {
	data    openapi3.SchemaOrRef
	hasData bool
	content map[string]openapi3.SchemaOrRef
}

// Apply 合并 Override 并校验其源码目标和 Schema
func Apply(operations []ir.Operation, files []config.OverrideFile, builder *schema.Builder) Result {
	operationDefinitions, schemaDefinitions, diagnostics := collectDefinitions(files)
	result := Result{
		Operations:         append([]ir.Operation(nil), operations...),
		OperationOverrides: make(map[string]config.OperationOverride),
		Schemas:            make(map[string]openapi3.SchemaOrRef),
		Diagnostics:        diagnostics,
	}

	operationIndex := make(map[string]int, len(result.Operations))
	for i := range result.Operations {
		operationIndex[result.Operations[i].Key().String()] = i
	}
	bases, buildDiagnostics := buildOperationBases(result.Operations, builder)
	result.Diagnostics = append(result.Diagnostics, buildDiagnostics...)

	components := make(map[string]openapi3.SchemaOrRef)
	if builder != nil {
		for name, component := range builder.Components() {
			components[name] = component
		}
	}
	schemaNames := sortedSchemaDefinitionKeys(schemaDefinitions)
	for _, name := range schemaNames {
		definition := schemaDefinitions[name]
		target := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
		if generated, found := components[name]; found {
			clone, err := cloneSchema(generated)
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, overrideDiagnostic(
					definition.source, "override.schema_merge", ir.SeverityError, fmt.Sprintf("schema %s: %v", name, err),
				))
				continue
			}
			target = clone
		}
		if err := ApplySchemaPatch(&target, definition.value); err != nil {
			result.Diagnostics = append(result.Diagnostics, overrideDiagnostic(
				definition.source, "override.schema_merge", ir.SeverityError, fmt.Sprintf("schema %s: %v", name, err),
			))
			continue
		}
		components[name] = target
	}

	for _, name := range schemaNames {
		definition := schemaDefinitions[name]
		before := errorCount(result.Diagnostics)
		base := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
		if builder != nil {
			if generated, found := builder.Components()[name]; found {
				base = generated
			}
		}
		result.Diagnostics = appendSchemaIssues(result.Diagnostics, definition.source,
			validateSchemaPatch(definition.value, base, components, "schema "+name))
		if errorCount(result.Diagnostics) == before {
			result.Schemas[name] = components[name]
			result.Valid = append(result.Valid, ValidEntry{Kind: "schema", Key: name, Source: definition.source})
		}
	}

	operationKeys := sortedOperationDefinitionKeys(operationDefinitions)
	for _, key := range operationKeys {
		definition := operationDefinitions[key]
		index, found := operationIndex[key]
		if !found {
			result.Diagnostics = append(result.Diagnostics, overrideDiagnostic(
				definition.source, "override.orphan_operation", ir.SeverityError, "operation does not exist: "+key,
			))
			continue
		}
		operation := &result.Operations[index]
		if operation.Mode != ir.ModeManaged {
			result.Diagnostics = append(result.Diagnostics, overrideDiagnostic(
				definition.source,
				"override.inactive_operation",
				ir.SeverityWarning,
				fmt.Sprintf("operation %s is %s and override is not applied", key, operation.Mode),
			))
			continue
		}

		before := errorCount(result.Diagnostics)
		base := bases[key]
		result.Diagnostics = validateOperationOverride(result.Diagnostics, definition, key, base, components)
		if errorCount(result.Diagnostics) != before {
			continue
		}
		applyOperationOverride(operation, definition.value)
		result.OperationOverrides[key] = definition.value
		result.Valid = append(result.Valid, ValidEntry{Kind: "operation", Key: key, Source: definition.source})
	}

	sort.Slice(result.Valid, func(i, j int) bool {
		if result.Valid[i].Kind != result.Valid[j].Kind {
			return result.Valid[i].Kind < result.Valid[j].Kind
		}
		return result.Valid[i].Key < result.Valid[j].Key
	})
	sort.Slice(result.Diagnostics, func(i, j int) bool {
		left, right := result.Diagnostics[i], result.Diagnostics[j]
		if left.Source.File != right.Source.File {
			return left.Source.File < right.Source.File
		}
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Message < right.Message
	})
	return result
}

// HasErrors 判断 Override 结果是否包含错误
func (r Result) HasErrors() bool {
	return errorCount(r.Diagnostics) > 0
}

// Counts 返回有效项、warning 和 error 数量
func (r Result) Counts() (valid, warnings, errors int) {
	for _, diagnostic := range r.Diagnostics {
		switch diagnostic.Severity {
		case ir.SeverityWarning:
			warnings++
		case ir.SeverityError:
			errors++
		}
	}
	return len(r.Valid), warnings, errors
}

// collectDefinitions 按文件名收集定义并拒绝跨文件重复
func collectDefinitions(files []config.OverrideFile) (map[string]operationDefinition, map[string]schemaDefinition, []ir.Diagnostic) {
	sortedFiles := append([]config.OverrideFile(nil), files...)
	sort.Slice(sortedFiles, func(i, j int) bool { return sortedFiles[i].Path < sortedFiles[j].Path })
	operations := make(map[string]operationDefinition)
	schemas := make(map[string]schemaDefinition)
	duplicateOperations := make(map[string]bool)
	duplicateSchemas := make(map[string]bool)
	var diagnostics []ir.Diagnostic

	for _, file := range sortedFiles {
		source := ir.SourceRef{File: filepath.ToSlash(file.Path)}
		operationKeys := make([]string, 0, len(file.Document.Operations))
		for rawKey := range file.Document.Operations {
			operationKeys = append(operationKeys, rawKey)
		}
		sort.Strings(operationKeys)
		for _, rawKey := range operationKeys {
			key, err := ir.ParseOperationKey(rawKey)
			if err != nil {
				diagnostics = append(diagnostics, overrideDiagnostic(source, "override.invalid_operation_key", ir.SeverityError, err.Error()))
				continue
			}
			normalized := key.String()
			if duplicateOperations[normalized] {
				continue
			}
			if previous, found := operations[normalized]; found {
				diagnostics = append(diagnostics, overrideDiagnostic(
					source,
					"override.duplicate_operation",
					ir.SeverityError,
					fmt.Sprintf("operation %s is also defined in %s", normalized, previous.source.File),
				))
				delete(operations, normalized)
				duplicateOperations[normalized] = true
				continue
			}
			operations[normalized] = operationDefinition{value: file.Document.Operations[rawKey], source: source}
		}

		schemaNames := make([]string, 0, len(file.Document.Schemas))
		for name := range file.Document.Schemas {
			schemaNames = append(schemaNames, name)
		}
		sort.Strings(schemaNames)
		for _, name := range schemaNames {
			if duplicateSchemas[name] {
				continue
			}
			if previous, found := schemas[name]; found {
				diagnostics = append(diagnostics, overrideDiagnostic(
					source,
					"override.duplicate_schema",
					ir.SeverityError,
					fmt.Sprintf("schema %s is also defined in %s", name, previous.source.File),
				))
				delete(schemas, name)
				duplicateSchemas[name] = true
				continue
			}
			schemas[name] = schemaDefinition{value: file.Document.Schemas[name], source: source}
		}
	}
	return operations, schemas, diagnostics
}

// buildOperationBases 构建 Override 兼容性校验所需的源码 Schema
func buildOperationBases(operations []ir.Operation, builder *schema.Builder) (map[string]operationBase, []ir.Diagnostic) {
	bases := make(map[string]operationBase)
	if builder == nil {
		return bases, nil
	}
	var diagnostics []ir.Diagnostic
	for _, operation := range operations {
		if operation.Mode != ir.ModeManaged {
			continue
		}
		base := operationBase{
			parameters: make(map[string]openapi3.SchemaOrRef),
			responses:  make(map[int]responseBase),
		}
		for _, parameter := range operation.Parameters {
			converted, err := builder.Build(parameter.Type, parameter.Source)
			if err != nil {
				diagnostics = append(diagnostics, overrideDiagnostic(parameter.Source, "override.schema_build", ir.SeverityError, err.Error()))
				continue
			}
			base.parameters[parameterKey(string(parameter.Location), parameter.Name)] = converted.Schema
		}
		if operation.RequestBody != nil {
			converted, err := builder.Build(operation.RequestBody.Type, operation.RequestBody.Source)
			if err != nil {
				diagnostics = append(diagnostics, overrideDiagnostic(operation.RequestBody.Source, "override.schema_build", ir.SeverityError, err.Error()))
			} else {
				base.body = &converted.Schema
			}
		}
		for _, response := range operation.Responses {
			responseSchema := responseBase{content: make(map[string]openapi3.SchemaOrRef)}
			for _, content := range response.Content {
				contentSchema := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
				if content.Binary {
					contentSchema.Schema.WithType(openapi3.SchemaTypeString).WithFormat("binary")
				} else if content.Type != nil {
					converted, err := builder.Build(*content.Type, response.Source)
					if err != nil {
						diagnostics = append(diagnostics, overrideDiagnostic(response.Source, "override.schema_build", ir.SeverityError, err.Error()))
						continue
					}
					contentSchema = converted.Schema
					if !responseSchema.hasData || content.DataTarget {
						responseSchema.data = converted.Schema
						responseSchema.hasData = true
					}
				}
				if _, found := responseSchema.content[content.MediaType]; !found || content.Type != nil {
					responseSchema.content[content.MediaType] = contentSchema
				}
			}
			base.responses[response.Status] = responseSchema
		}
		bases[operation.Key().String()] = base
	}
	return bases, diagnostics
}

// validateOperationOverride 校验单条接口覆盖
func validateOperationOverride(diagnostics []ir.Diagnostic, definition operationDefinition, key string, base operationBase, components map[string]openapi3.SchemaOrRef) []ir.Diagnostic {
	value := definition.value
	parameterTargets := make(map[string]bool)
	for _, parameter := range value.Parameters {
		target := parameterKey(parameter.Location, parameter.Name)
		if parameterTargets[target] {
			diagnostics = append(diagnostics, overrideDiagnostic(definition.source, "override.duplicate_parameter", ir.SeverityError,
				fmt.Sprintf("operation %s repeats parameter %s %s", key, parameter.Location, parameter.Name)))
			continue
		}
		parameterTargets[target] = true
		parameterSchema, found := base.parameters[target]
		if !found {
			diagnostics = append(diagnostics, overrideDiagnostic(definition.source, "override.orphan_parameter", ir.SeverityError,
				fmt.Sprintf("operation %s has no %s parameter %q", key, parameter.Location, parameter.Name)))
			continue
		}
		if parameter.Location == string(ir.ParameterPath) && parameter.Required != nil && !*parameter.Required {
			diagnostics = append(diagnostics, overrideDiagnostic(definition.source, "override.path_required", ir.SeverityError,
				fmt.Sprintf("operation %s path parameter %q must remain required", key, parameter.Name)))
		}
		if semanticIntegerParameter(parameter.Schema, parameterSchema, components) {
			integerSchema := openapi3.Schema{}
			integerSchema.WithType(openapi3.SchemaTypeInteger)
			parameterSchema = openapi3.SchemaOrRef{Schema: &integerSchema}
		}
		diagnostics = appendSchemaIssues(diagnostics, definition.source,
			validateSchemaPatch(parameter.Schema, parameterSchema, components, fmt.Sprintf("operation %s parameter %s %s", key, parameter.Location, parameter.Name)))
	}

	if value.Body != nil {
		if base.body == nil {
			diagnostics = append(diagnostics, overrideDiagnostic(definition.source, "override.orphan_body", ir.SeverityError,
				"operation has no JSON body: "+key))
		} else {
			diagnostics = appendSchemaIssues(diagnostics, definition.source,
				validateSchemaPatch(*value.Body, *base.body, components, "operation "+key+" body"))
		}
	}
	if value.Response != nil {
		response, found := base.responses[value.Response.Status]
		if !found {
			diagnostics = append(diagnostics, overrideDiagnostic(definition.source, "override.orphan_response", ir.SeverityError,
				fmt.Sprintf("operation %s has no response status %d", key, value.Response.Status)))
			return diagnostics
		}
		if value.Response.Data != nil {
			data := response.data
			if !response.hasData {
				data = openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
			}
			diagnostics = appendSchemaIssues(diagnostics, definition.source,
				validateSchemaPatch(*value.Response.Data, data, components, fmt.Sprintf("operation %s response %d data", key, value.Response.Status)))
		}
		mediaTypes := make([]string, 0, len(value.Response.Content))
		for mediaType := range value.Response.Content {
			mediaTypes = append(mediaTypes, mediaType)
		}
		sort.Strings(mediaTypes)
		for _, mediaType := range mediaTypes {
			content := response.content[mediaType]
			if content.Schema == nil && content.SchemaReference == nil {
				content = openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
			}
			diagnostics = appendSchemaIssues(diagnostics, definition.source,
				validateSchemaPatch(value.Response.Content[mediaType], content, components,
					fmt.Sprintf("operation %s response %d content %s", key, value.Response.Status, mediaType)))
		}
	}
	return diagnostics
}

// applyOperationOverride 合并接口及参数元数据
func applyOperationOverride(operation *ir.Operation, value config.OperationOverride) {
	if value.OperationID != "" {
		operation.OperationID = value.OperationID
	}
	if value.Summary != "" {
		operation.Summary = value.Summary
	}
	if value.Description != "" {
		operation.Description = value.Description
	}
	if value.Tags != nil {
		operation.Tags = append([]string(nil), value.Tags...)
	}
	for _, override := range value.Parameters {
		for i := range operation.Parameters {
			parameter := &operation.Parameters[i]
			if parameter.Name != override.Name || string(parameter.Location) != override.Location {
				continue
			}
			if override.Description != "" {
				parameter.Description = override.Description
			}
			if override.Required != nil {
				parameter.Required = *override.Required
			}
			if override.Schema.Format != "" {
				parameter.Format = override.Schema.Format
			}
			if override.Schema.Type == "integer" {
				parameter.Type = ir.TypeRef{Kind: ir.TypeBasic, Name: "integer"}
			}
			if override.Schema.Minimum != nil {
				parameter.Minimum = override.Schema.Minimum
			}
			parameter.Confidence = ir.ConfidenceOverride
		}
	}
	if value.Body != nil && operation.RequestBody != nil {
		operation.RequestBody.Confidence = ir.ConfidenceOverride
	}
	if value.Response != nil {
		for i := range operation.Responses {
			response := &operation.Responses[i]
			if response.Status != value.Response.Status {
				continue
			}
			if value.Response.Description != "" {
				response.Description = value.Response.Description
			}
			for j := range response.Content {
				response.Content[j].Confidence = ir.ConfidenceOverride
			}
		}
		if value.Response.Data != nil {
			operation.Diagnostics = removeDiagnostic(operation.Diagnostics, "response.dynamic_data")
			operation.Diagnostics = removeDiagnostic(operation.Diagnostics, "response.map_keys_unknown")
		}
	}
}

// semanticIntegerParameter 判断字符串输入是否由 Override 声明为整数语义
func semanticIntegerParameter(patch config.SchemaPatch, base openapi3.SchemaOrRef, components map[string]openapi3.SchemaOrRef) bool {
	return patch.Type == "integer" && openAPISchemaType(resolveSchema(base, components, map[string]bool{}), components) == "string"
}

// removeDiagnostic 移除已由 Override 消解的诊断
func removeDiagnostic(diagnostics []ir.Diagnostic, code string) []ir.Diagnostic {
	result := make([]ir.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != code {
			result = append(result, diagnostic)
		}
	}
	return result
}

// appendSchemaIssues 将 Schema 校验问题转换为统一诊断
func appendSchemaIssues(diagnostics []ir.Diagnostic, source ir.SourceRef, issues []schemaIssue) []ir.Diagnostic {
	for _, issue := range issues {
		diagnostics = append(diagnostics, overrideDiagnostic(source, issue.code, ir.SeverityError, issue.message))
	}
	return diagnostics
}

// overrideDiagnostic 创建 Override 文件诊断
func overrideDiagnostic(source ir.SourceRef, code string, severity ir.DiagnosticSeverity, message string) ir.Diagnostic {
	return ir.Diagnostic{Code: code, Severity: severity, Message: message, Source: source}
}

// errorCount 返回错误诊断数量
func errorCount(diagnostics []ir.Diagnostic) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == ir.SeverityError {
			count++
		}
	}
	return count
}

// parameterKey 返回参数的精确匹配键
func parameterKey(location, name string) string {
	return location + "\x00" + name
}

// sortedOperationDefinitionKeys 返回稳定接口键列表
func sortedOperationDefinitionKeys(definitions map[string]operationDefinition) []string {
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedSchemaDefinitionKeys 返回稳定组件名列表
func sortedSchemaDefinitionKeys(definitions map[string]schemaDefinition) []string {
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// DiagnosticLine 返回 CLI 使用的稳定诊断文本
func DiagnosticLine(diagnostic ir.Diagnostic) string {
	parts := []string{string(diagnostic.Severity), diagnostic.Code}
	if source := diagnostic.Source.String(); source != "" {
		parts = append(parts, source)
	}
	parts = append(parts, diagnostic.Message)
	return strings.Join(parts, "\t")
}
