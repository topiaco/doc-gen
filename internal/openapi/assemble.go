// Package openapi assembles analyzed operations into OpenAPI documents
package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"

	kinopenapi "github.com/getkin/kin-openapi/openapi3"
	"github.com/swaggest/openapi-go/openapi3"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
	"github.com/topiaco/doc-gen/internal/override"
	"github.com/topiaco/doc-gen/internal/schema"
)

// Document 保存组装后的 OpenAPI 文档
type Document struct {
	Spec openapi3.Spec // OpenAPI 结构
	JSON []byte        // 稳定 JSON 内容
}

// Assemble 组装 managed 接口并校验 OpenAPI 3.0 文档
func Assemble(project config.Project, applied override.Result, builder *schema.Builder) (Document, error) {
	paths := make(map[string]openapi3.PathItem)
	operationIDs := make(map[string]string)
	operations := managedOperations(applied.Operations)

	for _, operation := range operations {
		converted, err := buildOperation(operation, applied.OperationOverrides[operation.Key().String()], builder)
		if err != nil {
			return Document{}, fmt.Errorf("assemble %s: %w", operation.Key(), err)
		}
		operationID := *converted.ID
		if previous, found := operationIDs[operationID]; found {
			return Document{}, fmt.Errorf("duplicate operationId %q for %s and %s", operationID, previous, operation.Key())
		}
		operationIDs[operationID] = operation.Key().String()

		pathItem := paths[operation.Path]
		pathItem.WithMapOfOperationValuesItem(strings.ToLower(operation.Method), converted)
		paths[operation.Path] = pathItem
	}

	components := make(map[string]openapi3.SchemaOrRef)
	for name, component := range builder.Components() {
		components[name] = component
	}
	for name, component := range applied.Schemas {
		components[name] = component
	}

	spec := openapi3.Spec{
		Openapi: project.OpenAPIVersion,
		Info: openapi3.Info{
			Title:   project.Name,
			Version: project.Version,
		},
		Paths: openapi3.Paths{MapOfPathItemValues: paths},
	}
	if len(components) > 0 {
		spec.WithComponents(openapi3.Components{
			Schemas: &openapi3.ComponentsSchemas{MapOfSchemaOrRefValues: components},
		})
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return Document{}, fmt.Errorf("marshal OpenAPI: %w", err)
	}
	data = append(data, '\n')
	if err := Validate(data); err != nil {
		return Document{}, err
	}
	return Document{Spec: spec, JSON: data}, nil
}

// Validate 使用 kin-openapi 校验完整文档
func Validate(data []byte) error {
	loader := kinopenapi.NewLoader()
	loader.IsExternalRefsAllowed = false
	document, err := loader.LoadFromData(data)
	if err != nil {
		return fmt.Errorf("load OpenAPI: %w", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		return fmt.Errorf("validate OpenAPI: %w", err)
	}
	return nil
}

// OperationID 为接口生成稳定且可读的默认标识
func OperationID(method, path string) string {
	raw := strings.ToLower(method) + "_" + strings.Trim(path, "/")
	var result strings.Builder
	separator := false
	for _, value := range raw {
		if unicode.IsLetter(value) || unicode.IsDigit(value) {
			if separator && result.Len() > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(value)
			separator = false
			continue
		}
		separator = true
	}
	return strings.TrimSuffix(result.String(), "_")
}

// managedOperations 返回按精确接口键排序的纳管接口
func managedOperations(operations []ir.Operation) []ir.Operation {
	result := make([]ir.Operation, 0, len(operations))
	for _, operation := range operations {
		if operation.Mode == ir.ModeManaged {
			result = append(result, operation)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key().String() < result[j].Key().String() })
	return result
}

// buildOperation 转换单个接口的参数、请求体和响应
func buildOperation(operation ir.Operation, patch config.OperationOverride, builder *schema.Builder) (openapi3.Operation, error) {
	operationID := operation.OperationID
	if operationID == "" {
		operationID = OperationID(operation.Method, operation.Path)
	}
	result := openapi3.Operation{}
	result.WithID(operationID)
	if operation.Summary != "" {
		result.WithSummary(operation.Summary)
	}
	if operation.Description != "" {
		result.WithDescription(operation.Description)
	}
	result.Tags = sortedUnique(operation.Tags)

	parameters, formParameters, err := buildParameters(operation.Parameters, patch.Parameters, builder)
	if err != nil {
		return openapi3.Operation{}, err
	}
	result.Parameters = parameters
	requestBody, err := buildRequestBody(operation.RequestBody, formParameters, patch.Parameters, patch.Body, builder)
	if err != nil {
		return openapi3.Operation{}, err
	}
	if requestBody != nil {
		result.WithRequestBody(openapi3.RequestBodyOrRef{RequestBody: requestBody})
	}

	responses, err := buildResponses(operation.Responses, patch.Response, builder)
	if err != nil {
		return openapi3.Operation{}, err
	}
	result.WithResponses(responses)
	return result, nil
}

// buildParameters 转换 Path、Query、Header 参数并分离 Form 字段
func buildParameters(parameters []ir.Parameter, patches []config.ParameterOverride, builder *schema.Builder) ([]openapi3.ParameterOrRef, []ir.Parameter, error) {
	ordered := append([]ir.Parameter(nil), parameters...)
	sort.Slice(ordered, func(i, j int) bool {
		left := string(ordered[i].Location) + "\x00" + ordered[i].Name
		right := string(ordered[j].Location) + "\x00" + ordered[j].Name
		return left < right
	})
	patchByParameter := make(map[string]config.SchemaPatch, len(patches))
	for _, patch := range patches {
		patchByParameter[parameterKey(patch.Location, patch.Name)] = patch.Schema
	}

	result := make([]openapi3.ParameterOrRef, 0, len(ordered))
	var formParameters []ir.Parameter
	for _, parameter := range ordered {
		if parameter.Location == ir.ParameterForm {
			formParameters = append(formParameters, parameter)
			continue
		}
		location, err := parameterIn(parameter.Location)
		if err != nil {
			return nil, nil, err
		}
		converted, err := builder.Build(parameter.Type, parameter.Source)
		if err != nil {
			return nil, nil, fmt.Errorf("parameter %s: %w", parameter.Name, err)
		}
		schemaValue := converted.Schema
		basePatch := config.SchemaPatch{Format: parameter.Format, Minimum: parameter.Minimum}
		if err := override.ApplySchemaPatch(&schemaValue, basePatch); err != nil {
			return nil, nil, fmt.Errorf("parameter %s: %w", parameter.Name, err)
		}
		if patch, found := patchByParameter[parameterKey(string(parameter.Location), parameter.Name)]; found {
			if err := override.ApplySchemaPatch(&schemaValue, patch); err != nil {
				return nil, nil, fmt.Errorf("parameter %s override: %w", parameter.Name, err)
			}
		}

		value := openapi3.Parameter{Name: parameter.Name, In: location, Schema: &schemaValue}
		value.WithRequired(parameter.Required || parameter.Location == ir.ParameterPath)
		if parameter.Description != "" {
			value.WithDescription(parameter.Description)
		}
		if parameter.Collection == "multi" {
			value.WithStyle("form").WithExplode(true)
		}
		result = append(result, openapi3.ParameterOrRef{Parameter: &value})
	}
	return result, formParameters, nil
}

// buildRequestBody 转换 JSON Body 或表单字段
func buildRequestBody(body *ir.RequestBody, formParameters []ir.Parameter, parameterPatches []config.ParameterOverride, patch *config.SchemaPatch, builder *schema.Builder) (*openapi3.RequestBody, error) {
	if body != nil && len(formParameters) > 0 {
		return nil, fmt.Errorf("JSON body and form parameters cannot be combined")
	}
	if body != nil {
		converted, err := builder.Build(body.Type, body.Source)
		if err != nil {
			return nil, fmt.Errorf("request body: %w", err)
		}
		schemaValue := converted.Schema
		if patch != nil {
			if err := override.ApplySchemaPatch(&schemaValue, *patch); err != nil {
				return nil, fmt.Errorf("request body override: %w", err)
			}
		}
		contentType := body.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
		return &openapi3.RequestBody{
			Content:  map[string]openapi3.MediaType{contentType: {Schema: &schemaValue}},
			Required: boolPointer(body.Required),
		}, nil
	}
	if len(formParameters) == 0 {
		return nil, nil
	}

	formSchema := openapi3.Schema{}
	formSchema.WithType(openapi3.SchemaTypeObject)
	contentType := "application/x-www-form-urlencoded"
	patchByParameter := make(map[string]config.SchemaPatch, len(parameterPatches))
	for _, parameterPatch := range parameterPatches {
		patchByParameter[parameterKey(parameterPatch.Location, parameterPatch.Name)] = parameterPatch.Schema
	}
	for _, parameter := range formParameters {
		converted, err := builder.Build(parameter.Type, parameter.Source)
		if err != nil {
			return nil, fmt.Errorf("form parameter %s: %w", parameter.Name, err)
		}
		fieldSchema := converted.Schema
		basePatch := config.SchemaPatch{Format: parameter.Format, Description: parameter.Description, Minimum: parameter.Minimum}
		if err := override.ApplySchemaPatch(&fieldSchema, basePatch); err != nil {
			return nil, fmt.Errorf("form parameter %s: %w", parameter.Name, err)
		}
		if parameterPatch, found := patchByParameter[parameterKey(string(parameter.Location), parameter.Name)]; found {
			if err := override.ApplySchemaPatch(&fieldSchema, parameterPatch); err != nil {
				return nil, fmt.Errorf("form parameter %s override: %w", parameter.Name, err)
			}
		}
		formSchema.WithPropertiesItem(parameter.Name, fieldSchema)
		if parameter.Required {
			formSchema.Required = append(formSchema.Required, parameter.Name)
		}
		if parameter.Format == "binary" {
			contentType = "multipart/form-data"
		}
	}
	return &openapi3.RequestBody{
		Content:  map[string]openapi3.MediaType{contentType: {Schema: &openapi3.SchemaOrRef{Schema: &formSchema}}},
		Required: boolPointer(len(formSchema.Required) > 0),
	}, nil
}

// buildResponses 合并同一状态码下的媒体类型和响应变体
func buildResponses(responses []ir.Response, patch *config.ResponseOverride, builder *schema.Builder) (openapi3.Responses, error) {
	if len(responses) == 0 {
		return openapi3.Responses{}, fmt.Errorf("operation has no statically known responses")
	}
	ordered := append([]ir.Response(nil), responses...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Status < ordered[j].Status })
	result := openapi3.Responses{MapOfResponseOrRefValues: make(map[string]openapi3.ResponseOrRef)}
	for _, response := range ordered {
		converted, err := buildResponse(response, matchingResponsePatch(response.Status, patch), builder)
		if err != nil {
			return openapi3.Responses{}, fmt.Errorf("response %d: %w", response.Status, err)
		}
		result.MapOfResponseOrRefValues[strconv.Itoa(response.Status)] = openapi3.ResponseOrRef{Response: &converted}
	}
	return result, nil
}

// buildResponse 转换单个状态码下的全部 Content
func buildResponse(response ir.Response, patch *config.ResponseOverride, builder *schema.Builder) (openapi3.Response, error) {
	description := response.Description
	if description == "" {
		description = http.StatusText(response.Status)
	}
	if description == "" {
		description = "Response"
	}
	result := openapi3.Response{Description: description}
	if len(response.Content) == 0 {
		return result, nil
	}

	contents := append([]ir.ResponseContent(nil), response.Content...)
	sort.Slice(contents, func(i, j int) bool { return responseContentKey(contents[i]) < responseContentKey(contents[j]) })
	dataPatchTarget := responseDataPatchTarget(contents)
	mediaSchemas := make(map[string][]openapi3.SchemaOrRef)
	for index, content := range contents {
		converted, err := buildResponseContent(content, builder, response.Source)
		if err != nil {
			return openapi3.Response{}, err
		}
		if patch != nil && patch.Data != nil && index == dataPatchTarget {
			if err := applyDataPatch(&converted, content.Wrapper, *patch.Data); err != nil {
				return openapi3.Response{}, fmt.Errorf("data override: %w", err)
			}
		}
		mediaType := content.MediaType
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		mediaSchemas[mediaType] = append(mediaSchemas[mediaType], converted)
	}

	result.Content = make(map[string]openapi3.MediaType, len(mediaSchemas))
	for mediaType, variants := range mediaSchemas {
		combined := variants[0]
		if len(variants) > 1 {
			combined = openapi3.SchemaOrRef{Schema: &openapi3.Schema{OneOf: variants}}
		}
		if patch != nil {
			if contentPatch, found := patch.Content[mediaType]; found {
				if err := override.ApplySchemaPatch(&combined, contentPatch); err != nil {
					return openapi3.Response{}, fmt.Errorf("content %s override: %w", mediaType, err)
				}
			}
		}
		content := openapi3.MediaType{Schema: &combined}
		if patch != nil && patch.Example != nil && mediaType == "application/json" {
			content.WithExample(patch.Example)
		}
		result.Content[mediaType] = content
	}
	if patch != nil {
		for mediaType, contentPatch := range patch.Content {
			if _, found := result.Content[mediaType]; found {
				continue
			}
			contentSchema := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
			if err := override.ApplySchemaPatch(&contentSchema, contentPatch); err != nil {
				return openapi3.Response{}, fmt.Errorf("content %s override: %w", mediaType, err)
			}
			result.Content[mediaType] = openapi3.MediaType{Schema: &contentSchema}
		}
	}
	return result, nil
}

// responseContentKey 返回响应内容变体的稳定排序键
func responseContentKey(content ir.ResponseContent) string {
	typeName := ""
	if content.Type != nil {
		typeName = content.Type.String()
	}
	return content.MediaType + "\x00" + content.Wrapper + "\x00" + typeName + "\x00" + strconv.FormatBool(content.Binary)
}

// buildResponseContent 转换直接、二进制和统一包装响应
func buildResponseContent(content ir.ResponseContent, builder *schema.Builder, source ir.SourceRef) (openapi3.SchemaOrRef, error) {
	if content.Binary {
		value := openapi3.Schema{}
		value.WithType(openapi3.SchemaTypeString).WithFormat("binary")
		return openapi3.SchemaOrRef{Schema: &value}, nil
	}
	data := openapi3.SchemaOrRef{Schema: &openapi3.Schema{}}
	if content.Type != nil {
		converted, err := builder.Build(*content.Type, source)
		if err != nil {
			return openapi3.SchemaOrRef{}, err
		}
		data = converted.Schema
	} else {
		data.Schema.WithNullable(true)
	}
	if content.Wrapper == "" {
		return data, nil
	}
	return wrappedResponse(content.Wrapper, data), nil
}

// wrappedResponse 构造项目统一 JSON 响应包装
func wrappedResponse(wrapper string, data openapi3.SchemaOrRef) openapi3.SchemaOrRef {
	code := openapi3.Schema{}
	code.WithType(openapi3.SchemaTypeInteger)
	message := openapi3.Schema{}
	message.WithType(openapi3.SchemaTypeString)
	response := openapi3.Schema{}
	response.WithType(openapi3.SchemaTypeObject)
	response.WithPropertiesItem("code", openapi3.SchemaOrRef{Schema: &code})
	response.WithPropertiesItem("message", openapi3.SchemaOrRef{Schema: &message})
	response.WithPropertiesItem("data", data)
	response.WithRequired("code", "message", "data")

	switch wrapper {
	case "standardSuccess":
		code.WithEnum(0)
		message.WithDefault("success")
	case "standardError":
		code.WithEnum(-1)
	case "businessError":
		code.WithEnum(1)
	default:
		response.WithMapOfAnythingItem("x-response-wrapper", wrapper)
	}
	return openapi3.SchemaOrRef{Schema: &response}
}

// applyDataPatch 将响应 data 覆盖应用到包装或直接 Schema
func applyDataPatch(target *openapi3.SchemaOrRef, wrapper string, patch config.SchemaPatch) error {
	if wrapper == "" {
		return override.ApplySchemaPatch(target, patch)
	}
	if target.Schema == nil || target.Schema.Properties == nil {
		return fmt.Errorf("response wrapper has no data property")
	}
	data := target.Schema.Properties["data"]
	if err := override.ApplySchemaPatch(&data, patch); err != nil {
		return err
	}
	target.Schema.Properties["data"] = data
	return nil
}

// responseDataPatchTarget 选择成功包装或首个 JSON 响应作为 data 覆盖目标
func responseDataPatchTarget(contents []ir.ResponseContent) int {
	for index, content := range contents {
		if content.Wrapper == "standardSuccess" {
			return index
		}
	}
	for index, content := range contents {
		if !content.Binary {
			return index
		}
	}
	return -1
}

// matchingResponsePatch 返回当前状态码对应的覆盖
func matchingResponsePatch(status int, patch *config.ResponseOverride) *config.ResponseOverride {
	if patch != nil && patch.Status == status {
		return patch
	}
	return nil
}

// parameterIn 转换受支持的 OpenAPI 参数位置
func parameterIn(location ir.ParameterLocation) (openapi3.ParameterIn, error) {
	switch location {
	case ir.ParameterPath:
		return openapi3.ParameterInPath, nil
	case ir.ParameterQuery:
		return openapi3.ParameterInQuery, nil
	case ir.ParameterHeader:
		return openapi3.ParameterInHeader, nil
	default:
		return "", fmt.Errorf("unsupported parameter location %q", location)
	}
}

// sortedUnique 返回排序后的非空唯一字符串
func sortedUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

// parameterKey 返回参数覆盖的精确键
func parameterKey(location, name string) string {
	return location + "\x00" + name
}

// boolPointer 返回布尔值指针
func boolPointer(value bool) *bool {
	return &value
}
