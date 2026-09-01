package config

type Config struct {
	ConfigVersion     string                   `json:"configVersion" yaml:"configVersion"`
	Project           Project                  `json:"project" yaml:"project"`
	Source            Source                   `json:"source" yaml:"source"`
	Files             Files                    `json:"files" yaml:"files"`
	Analysis          Analysis                 `json:"analysis" yaml:"analysis"`
	ResponseFunctions []ResponseFunction       `json:"responseFunctions" yaml:"responseFunctions"`
	ParameterHelpers  []ParameterHelper        `json:"parameterHelpers" yaml:"parameterHelpers"`
	SchemaMappings    map[string]SchemaMapping `json:"schemaMappings" yaml:"schemaMappings"`
	Quality           Quality                  `json:"quality" yaml:"quality"`
}

type Project struct {
	Name           string `json:"name" yaml:"name"`
	Version        string `json:"version" yaml:"version"`
	Module         string `json:"module" yaml:"module"`
	Framework      string `json:"framework" yaml:"framework"`
	OpenAPIVersion string `json:"openapiVersion" yaml:"openapiVersion"`
}

type Source struct {
	RoutePackages      []string `json:"routePackages" yaml:"routePackages"`
	ControllerPackages []string `json:"controllerPackages" yaml:"controllerPackages"`
	ServicePackages    []string `json:"servicePackages" yaml:"servicePackages"`
	ModelPackages      []string `json:"modelPackages" yaml:"modelPackages"`
}

type Files struct {
	Selection string `json:"selection" yaml:"selection"`
	Overrides string `json:"overrides" yaml:"overrides"`
	Output    string `json:"output" yaml:"output"`
	Report    string `json:"report" yaml:"report"`
}

type Analysis struct {
	Conservative              bool     `json:"conservative" yaml:"conservative"`
	InferManualRequiredChecks bool     `json:"inferManualRequiredChecks" yaml:"inferManualRequiredChecks"`
	FieldDescriptionFallback  []string `json:"fieldDescriptionFallback" yaml:"fieldDescriptionFallback"`
}

type ResponseFunction struct {
	Symbol         string `json:"symbol" yaml:"symbol"`                                     // 响应函数完整符号
	Kind           string `json:"kind" yaml:"kind"`                                         // 响应函数类别
	Status         int    `json:"status,omitempty" yaml:"status,omitempty"`                 // 固定 HTTP 状态码
	StatusArgument *int   `json:"statusArgument,omitempty" yaml:"statusArgument,omitempty"` // 状态码实参下标
	DataArgument   *int   `json:"dataArgument,omitempty" yaml:"dataArgument,omitempty"`     // 响应数据实参下标
	Wrapper        string `json:"wrapper,omitempty" yaml:"wrapper,omitempty"`               // JSON 响应包装名称
	MediaType      string `json:"mediaType,omitempty" yaml:"mediaType,omitempty"`           // 二进制响应媒体类型
}

type ParameterHelper struct {
	Symbol           string   `json:"symbol" yaml:"symbol"`
	Location         string   `json:"location" yaml:"location"`
	NameArgument     int      `json:"nameArgument" yaml:"nameArgument"`
	Type             string   `json:"type" yaml:"type"`
	Format           string   `json:"format,omitempty" yaml:"format,omitempty"`
	CollectionFormat string   `json:"collectionFormat,omitempty" yaml:"collectionFormat,omitempty"`
	Minimum          *float64 `json:"minimum,omitempty" yaml:"minimum,omitempty"`
}

type SchemaMapping struct {
	Schema     map[string]any `json:"schema" yaml:"schema"`
	Diagnostic bool           `json:"diagnostic" yaml:"diagnostic"`
}

type Quality struct {
	MinimumRouteDiscoveryCoverage     int  `json:"minimumRouteDiscoveryCoverage" yaml:"minimumRouteDiscoveryCoverage"`
	MinimumManagedExactSchemaCoverage int  `json:"minimumManagedExactSchemaCoverage" yaml:"minimumManagedExactSchemaCoverage"`
	FailOnDuplicateOperationID        bool `json:"failOnDuplicateOperationID" yaml:"failOnDuplicateOperationID"`
	FailOnInvalidOpenAPI              bool `json:"failOnInvalidOpenAPI" yaml:"failOnInvalidOpenAPI"`
	FailOnUnresolvedManagedPath       bool `json:"failOnUnresolvedManagedPath" yaml:"failOnUnresolvedManagedPath"`
	FailOnUnknownManagedResponse      bool `json:"failOnUnknownManagedResponse" yaml:"failOnUnknownManagedResponse"`
	FailOnOrphanSelection             bool `json:"failOnOrphanSelection" yaml:"failOnOrphanSelection"`
	FailOnOrphanOverride              bool `json:"failOnOrphanOverride" yaml:"failOnOrphanOverride"`
}

type Selection struct {
	ConfigVersion string                        `json:"configVersion" yaml:"configVersion"`
	DefaultMode   string                        `json:"defaultMode" yaml:"defaultMode"`
	Operations    map[string]SelectionOperation `json:"operations" yaml:"operations"`
}

type SelectionOperation struct {
	Mode   string `json:"mode" yaml:"mode"`
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type OverrideDocument struct {
	ConfigVersion string                       `json:"configVersion" yaml:"configVersion"`
	Operations    map[string]OperationOverride `json:"operations" yaml:"operations"`
	Schemas       map[string]SchemaPatch       `json:"schemas" yaml:"schemas"`
}

type OperationOverride struct {
	OperationID string              `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Summary     string              `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string              `json:"description,omitempty" yaml:"description,omitempty"`
	Tags        []string            `json:"tags,omitempty" yaml:"tags,omitempty"`
	Parameters  []ParameterOverride `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Body        *SchemaPatch        `json:"body,omitempty" yaml:"body,omitempty"`
	Response    *ResponseOverride   `json:"response,omitempty" yaml:"response,omitempty"`
}

type ParameterOverride struct {
	Name        string      `json:"name" yaml:"name"`
	Location    string      `json:"in" yaml:"in"`
	Description string      `json:"description,omitempty" yaml:"description,omitempty"`
	Required    *bool       `json:"required,omitempty" yaml:"required,omitempty"`
	Schema      SchemaPatch `json:"schema" yaml:"schema"`
}

type ResponseOverride struct {
	Status      int                    `json:"status" yaml:"status"`                               // 响应状态码
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"` // 响应说明
	Data        *SchemaPatch           `json:"data,omitempty" yaml:"data,omitempty"`               // Data Schema 覆盖
	Content     map[string]SchemaPatch `json:"content,omitempty" yaml:"content,omitempty"`         // 媒体类型覆盖
	Example     any                    `json:"example,omitempty" yaml:"example,omitempty"`         // JSON 响应示例
}

type SchemaPatch struct {
	Ref                  string                 `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Type                 string                 `json:"type,omitempty" yaml:"type,omitempty"`
	Format               string                 `json:"format,omitempty" yaml:"format,omitempty"`
	Description          string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Nullable             *bool                  `json:"nullable,omitempty" yaml:"nullable,omitempty"`
	Required             []string               `json:"required,omitempty" yaml:"required,omitempty"`
	Properties           map[string]SchemaPatch `json:"properties,omitempty" yaml:"properties,omitempty"`
	Items                *SchemaPatch           `json:"items,omitempty" yaml:"items,omitempty"`
	AdditionalProperties *SchemaPatch           `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
	Enum                 []NamedEnumValue       `json:"enum,omitempty" yaml:"enum,omitempty"`
	Minimum              *float64               `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	Maximum              *float64               `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	Default              any                    `json:"default,omitempty" yaml:"default,omitempty"`
}

type NamedEnumValue struct {
	Value       any    `json:"value" yaml:"value"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type Paths struct {
	ProjectRoot string
	Config      string
	Selection   string
	Overrides   string
	Output      string
	Report      string
}

type OverrideFile struct {
	Path     string
	Document OverrideDocument
}

type Bundle struct {
	Config    Config
	Selection Selection
	Overrides []OverrideFile
	Paths     Paths
}
