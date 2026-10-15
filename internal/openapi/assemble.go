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

	"github.com/topiaco/doc-gen/internal/ir"
	"github.com/topiaco/doc-gen/internal/schema"
)

// Document 保存组装后的 OpenAPI 文档
type Document struct {
	JSON []byte // 稳定 JSON 内容
}

// Info 保存文档标题和业务 API 版本
type Info struct {
	Title   string
	Version string
}

// Assemble 为选定接口生成 OpenAPI 3.0.3 文档，并校验文档结构
func Assemble(info Info, operations []ir.Operation, builder *schema.Builder) (Document, error) {
	paths := make(map[string]openapi3.PathItem)
	operationIDs := make(map[string]string)
	operationKeys := make(map[string]bool)
	operations = managedOperations(operations)

	for _, operation := range operations {
		if operationKeys[operation.Key().String()] {
			return Document{}, fmt.Errorf("duplicate route %s", operation.Key())
		}
		operationKeys[operation.Key().String()] = true
		converted, err := buildOperation(operation, builder)
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

	spec := openapi3.Spec{
		Openapi: "3.0.3",
		Info: openapi3.Info{
			Title:   info.Title,
			Version: info.Version,
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
	return Document{JSON: data}, nil
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

// managedOperations 选择需要生成文档的接口，再按 HTTP 方法和规范化路径排序
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
func buildOperation(operation ir.Operation, builder *schema.Builder) (openapi3.Operation, error) {
	operationID := OperationID(operation.Method, operation.Path)
	result := openapi3.Operation{}
	result.WithID(operationID)
	if operation.Summary != "" {
		result.WithSummary(operation.Summary)
	}
	if operation.Description != "" {
		result.WithDescription(operation.Description)
	}
	if len(operation.Diagnostics) > 0 {
		result.MapOfAnything = map[string]any{"x-apidoc-diagnostics": operation.Diagnostics}
	}

	parameters, formParameters, err := buildParameters(operation.Parameters, builder)
	if err != nil {
		return openapi3.Operation{}, err
	}
	result.Parameters = parameters
	requestBody, err := buildRequestBody(operation.RequestBody, formParameters, builder)
	if err != nil {
		return openapi3.Operation{}, err
	}
	if requestBody != nil {
		result.WithRequestBody(openapi3.RequestBodyOrRef{RequestBody: requestBody})
	}

	responses, err := buildResponses(operation.Responses, builder)
	if err != nil {
		return openapi3.Operation{}, err
	}
	result.WithResponses(responses)
	return result, nil
}

// buildParameters 按位置和名称排序参数，并单独返回用于构建请求体的表单字段
func buildParameters(parameters []ir.Parameter, builder *schema.Builder) ([]openapi3.ParameterOrRef, []ir.Parameter, error) {
	ordered := append([]ir.Parameter(nil), parameters...)
	sort.Slice(ordered, func(i, j int) bool {
		return string(ordered[i].Location)+"\x00"+ordered[i].Name < string(ordered[j].Location)+"\x00"+ordered[j].Name
	})
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
		converted, err := builder.BuildParameter(parameter)
		if err != nil {
			return nil, nil, fmt.Errorf("parameter %s: %w", parameter.Name, err)
		}
		value := openapi3.Parameter{Name: parameter.Name, In: location, Schema: &converted.Schema}
		value.WithRequired(parameter.Required || parameter.Location == ir.ParameterPath)
		if parameter.Description != "" {
			value.WithDescription(parameter.Description)
		}
		result = append(result, openapi3.ParameterOrRef{Parameter: &value})
	}
	return result, formParameters, nil
}

// buildRequestBody 根据请求绑定与校验规则生成 JSON 或表单请求体
func buildRequestBody(body *ir.RequestBody, formParameters []ir.Parameter, builder *schema.Builder) (*openapi3.RequestBody, error) {
	if body != nil && len(formParameters) > 0 {
		return nil, fmt.Errorf("JSON body and form parameters cannot be combined")
	}
	if body != nil {
		converted, err := builder.BuildRequest(body.Type, body.Source)
		if err != nil {
			return nil, fmt.Errorf("request body: %w", err)
		}
		return &openapi3.RequestBody{Content: map[string]openapi3.MediaType{body.ContentType: {Schema: &converted.Schema}}, Required: boolPointer(body.Required)}, nil
	}
	if len(formParameters) == 0 {
		return nil, nil
	}
	formSchema := openapi3.Schema{}
	formSchema.WithType(openapi3.SchemaTypeObject)
	contentType := "application/x-www-form-urlencoded"
	for _, parameter := range formParameters {
		converted, err := builder.BuildParameter(parameter)
		if err != nil {
			return nil, fmt.Errorf("form parameter %s: %w", parameter.Name, err)
		}
		// 表单参数由请求体属性表示，因此将字段说明写入对应属性
		if parameter.Description != "" {
			if converted.Schema.Schema == nil {
				converted.Schema = openapi3.SchemaOrRef{Schema: &openapi3.Schema{AllOf: []openapi3.SchemaOrRef{converted.Schema}}}
			}
			converted.Schema.Schema.WithDescription(parameter.Description)
		}
		formSchema.WithPropertiesItem(parameter.Name, converted.Schema)
		if parameter.Required {
			formSchema.Required = append(formSchema.Required, parameter.Name)
		}
		if parameter.Format == "binary" {
			contentType = "multipart/form-data"
		}
	}
	return &openapi3.RequestBody{Content: map[string]openapi3.MediaType{contentType: {Schema: &openapi3.SchemaOrRef{Schema: &formSchema}}}, Required: boolPointer(len(formSchema.Required) > 0)}, nil
}

// buildResponses 合并同一状态码下的媒体类型和响应变体
func buildResponses(responses []ir.Response, builder *schema.Builder) (openapi3.Responses, error) {
	if len(responses) == 0 {
		return openapi3.Responses{}, fmt.Errorf("operation has no statically known responses")
	}
	ordered := append([]ir.Response(nil), responses...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Status < ordered[j].Status })
	result := openapi3.Responses{MapOfResponseOrRefValues: make(map[string]openapi3.ResponseOrRef)}
	for _, response := range ordered {
		converted, err := buildResponse(response, builder)
		if err != nil {
			return openapi3.Responses{}, fmt.Errorf("response %d: %w", response.Status, err)
		}
		key := strconv.Itoa(response.Status)
		if response.Status == 0 {
			key = "default"
		}
		result.MapOfResponseOrRefValues[key] = openapi3.ResponseOrRef{Response: &converted}
	}
	return result, nil
}

// buildResponse 转换单个状态码下的全部 Content
func buildResponse(response ir.Response, builder *schema.Builder) (openapi3.Response, error) {
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
	mediaSchemas := make(map[string][]openapi3.SchemaOrRef)
	for _, content := range contents {
		converted, err := buildResponseContent(content, builder, response.Source)
		if err != nil {
			return openapi3.Response{}, err
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
		// 各分支的结构可能重叠，因此使用 anyOf 表示响应可以符合其中任意一种结构
		if len(variants) > 1 {
			combined = openapi3.SchemaOrRef{Schema: &openapi3.Schema{AnyOf: variants}}
		}
		result.Content[mediaType] = openapi3.MediaType{Schema: &combined}
	}
	return result, nil
}

// responseContentKey 返回响应内容变体的稳定排序键
func responseContentKey(content ir.ResponseContent) string {
	typeName := ""
	if content.Type != nil {
		typeName = content.Type.String()
	}
	return content.MediaType + "\x00" + typeName + "\x00" + strconv.FormatBool(content.Binary)
}

// buildResponseContent 将源码分析得到的响应类型或二进制标记转换为 Schema
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
	return data, nil
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

// boolPointer 为 OpenAPI 中需要区分未设置与 false 的字段创建指针
func boolPointer(value bool) *bool {
	return &value
}
