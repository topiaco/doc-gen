// Package ir defines framework-independent API analysis results
package ir

import (
	"fmt"
	"strings"
)

// DocumentationMode 表示接口是否生成文档，以及因存量快照或显式指令排除的情况
type DocumentationMode string

const (
	ModeManaged DocumentationMode = "managed"
	ModeManual  DocumentationMode = "manual"
	ModeIgnored DocumentationMode = "ignored"
)

// Confidence 表示分析结论的可信来源
type Confidence string

const (
	ConfidenceExact    Confidence = "exact"
	ConfidenceOverride Confidence = "override"
	ConfidenceInferred Confidence = "inferred"
	ConfidenceUnknown  Confidence = "unknown"
)

// DiagnosticSeverity 表示诊断的严重级别
type DiagnosticSeverity string

const (
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityError   DiagnosticSeverity = "error"
)

// SourceRef 定位一段源码
type SourceRef struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

// String 返回适合诊断展示的源码位置
func (s SourceRef) String() string {
	if s.Line == 0 {
		return s.File
	}
	if s.Column == 0 {
		return fmt.Sprintf("%s:%d", s.File, s.Line)
	}
	return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Column)
}

// SymbolRef 标识一个 Go 符号
type SymbolRef struct {
	PackagePath string    `json:"packagePath"`
	Name        string    `json:"name"`
	Source      SourceRef `json:"source"`
}

// String 返回带包路径的 Go 符号名
func (s SymbolRef) String() string {
	if s.PackagePath == "" {
		return s.Name
	}
	return s.PackagePath + "." + s.Name
}

// TypeKind 表示 TypeRef 的结构类别
type TypeKind string

const (
	TypeUnknown   TypeKind = "unknown"
	TypeBasic     TypeKind = "basic"
	TypeNamed     TypeKind = "named"
	TypePointer   TypeKind = "pointer"
	TypeSlice     TypeKind = "slice"
	TypeArray     TypeKind = "array"
	TypeMap       TypeKind = "map"
	TypeStruct    TypeKind = "struct"
	TypeInterface TypeKind = "interface"
	TypeParameter TypeKind = "typeParameter"
	TypeNull      TypeKind = "null"
	TypeUnion     TypeKind = "union"
)

// TypeRef 递归表达静态 Go 类型
type TypeRef struct {
	Kind           TypeKind  `json:"kind"`
	PackagePath    string    `json:"packagePath,omitempty"`
	Name           string    `json:"name,omitempty"`
	Element        *TypeRef  `json:"element,omitempty"`
	Key            *TypeRef  `json:"key,omitempty"`
	Length         int64     `json:"length,omitempty"`
	TypeArguments  []TypeRef `json:"typeArguments,omitempty"`
	Fields         []Field   `json:"fields,omitempty"`
	Description    string    `json:"description,omitempty"`
	SchemaType     string    `json:"schemaType,omitempty"`
	Format         string    `json:"format,omitempty"`
	Unresolved     string    `json:"unresolved,omitempty"`
	CustomEncoding bool      `json:"customEncoding,omitempty"`
	Variants       []TypeRef `json:"variants,omitempty"`
}

// String 返回稳定的类型表达式
func (t TypeRef) String() string {
	switch t.Kind {
	case TypeBasic, TypeInterface, TypeParameter:
		return t.Name
	case TypeNamed:
		return t.namedString()
	case TypePointer:
		return "*" + typeString(t.Element)
	case TypeSlice:
		return "[]" + typeString(t.Element)
	case TypeArray:
		return fmt.Sprintf("[%d]%s", t.Length, typeString(t.Element))
	case TypeMap:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Element)
	case TypeStruct:
		return t.structString()
	case TypeUnion:
		parts := make([]string, len(t.Variants))
		for i, variant := range t.Variants {
			parts[i] = variant.String()
		}
		return "union[" + strings.Join(parts, ",") + "]"
	default:
		return string(TypeUnknown)
	}
}

// namedString 返回具名类型及其泛型实参
func (t TypeRef) namedString() string {
	name := t.Name
	if t.PackagePath != "" {
		name = t.PackagePath + "." + name
	}
	if len(t.TypeArguments) == 0 {
		return name
	}

	args := make([]string, len(t.TypeArguments))
	for i, arg := range t.TypeArguments {
		args[i] = arg.String()
	}
	return name + "[" + strings.Join(args, ",") + "]"
}

// structString 返回匿名结构体的稳定表达式
func (t TypeRef) structString() string {
	fields := make([]string, len(t.Fields))
	for i, field := range t.Fields {
		name := field.JSONName
		if name == "" {
			name = field.Name
		}
		fields[i] = name + " " + field.Type.String()
	}
	return "struct{" + strings.Join(fields, ";") + "}"
}

// typeString 返回可选子类型的稳定表达式
func typeString(t *TypeRef) string {
	if t == nil {
		return string(TypeUnknown)
	}
	return t.String()
}

// Field 表示结构体字段及其文档元数据
type Field struct {
	Name        string     `json:"name"`                  // Go 字段名
	JSONName    string     `json:"jsonName,omitempty"`    // JSON 字段名
	Type        TypeRef    `json:"type"`                  // 字段静态类型
	Tag         string     `json:"tag,omitempty"`         // 原始 Struct 标签
	Description string     `json:"description,omitempty"` // 字段说明
	Required    bool       `json:"required,omitempty"`    // 是否必填
	Confidence  Confidence `json:"confidence"`            // 元数据可信度
	Source      SourceRef  `json:"source"`                // 字段源码位置
}

// ParameterLocation 表示 OpenAPI 参数位置
type ParameterLocation string

const (
	ParameterPath   ParameterLocation = "path"
	ParameterQuery  ParameterLocation = "query"
	ParameterHeader ParameterLocation = "header"
	ParameterForm   ParameterLocation = "form"
)

// Parameter 表示接口参数
type Parameter struct {
	Name        string            `json:"name"`
	Location    ParameterLocation `json:"in"`
	Origin      string            `json:"origin"`
	Description string            `json:"description,omitempty"`
	Required    bool              `json:"required"`
	Type        TypeRef           `json:"type"`
	Format      string            `json:"format,omitempty"`
	Confidence  Confidence        `json:"confidence"`
	Source      SourceRef         `json:"source"`
	Tag         string            `json:"tag,omitempty"`
}

// RequestBody 表示接口请求体
type RequestBody struct {
	ContentType string     `json:"contentType"`
	Required    bool       `json:"required"`
	Type        TypeRef    `json:"type"`
	Confidence  Confidence `json:"confidence"`
	Source      SourceRef  `json:"source"`
}

// ResponseContent 表示一个响应媒体类型
type ResponseContent struct {
	MediaType  string     `json:"mediaType"`        // 响应媒体类型
	Type       *TypeRef   `json:"type,omitempty"`   // JSON 数据静态类型
	Binary     bool       `json:"binary,omitempty"` // 是否为二进制内容
	Confidence Confidence `json:"confidence"`       // 内容分析可信度
}

// Response 表示一个 HTTP 响应
type Response struct {
	Status             int               `json:"status"`                       // HTTP 状态码
	Description        string            `json:"description,omitempty"`        // 响应说明
	Content            []ResponseContent `json:"content,omitempty"`            // 响应内容变体
	RedirectLocation   string            `json:"redirectLocation,omitempty"`   // 重定向目标地址
	RedirectConfidence Confidence        `json:"redirectConfidence,omitempty"` // 重定向分析可信度
	Source             SourceRef         `json:"source"`                       // 响应源码位置
}

// Diagnostic 表示可追溯的分析问题
type Diagnostic struct {
	Code     string             `json:"code"`
	Severity DiagnosticSeverity `json:"severity"`
	Message  string             `json:"message"`
	Source   SourceRef          `json:"source"`
}

// Operation 表示一个与框架无关的 API 操作
type Operation struct {
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Handler     SymbolRef         `json:"handler"`
	Summary     string            `json:"summary,omitempty"`
	Description string            `json:"description,omitempty"`
	Parameters  []Parameter       `json:"parameters,omitempty"`
	RequestBody *RequestBody      `json:"requestBody,omitempty"`
	Responses   []Response        `json:"responses,omitempty"`
	Mode        DocumentationMode `json:"mode"`
	ModeReason  string            `json:"modeReason,omitempty"`
	Confidence  Confidence        `json:"confidence"`
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
	Source      SourceRef         `json:"source"`
	Selection   string            `json:"selection,omitempty"`
}

// Key 返回接口的精确匹配键
func (o Operation) Key() OperationKey {
	return OperationKey{Method: o.Method, Path: o.Path}
}
