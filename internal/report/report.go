// Package report builds stable analysis reports and quality results
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
	apiopenapi "github.com/topiaco/doc-gen/internal/openapi"
)

// Document 保存完整分析报告
type Document struct {
	Summary     Summary         `json:"summary"`               // 汇总指标
	Quality     []QualityGate   `json:"quality"`               // 质量门禁结果
	Operations  []Operation     `json:"operations"`            // 全部路由分析
	Diagnostics []ir.Diagnostic `json:"diagnostics,omitempty"` // 全局诊断
}

// Summary 保存路由、Schema 和诊断统计
type Summary struct {
	DiscoveredRoutes        int     `json:"discoveredRoutes"`        // 已发现路由数
	Managed                 int     `json:"managed"`                 // 纳管路由数
	Manual                  int     `json:"manual"`                  // 手工路由数
	Ignored                 int     `json:"ignored"`                 // 忽略路由数
	GeneratedOperations     int     `json:"generatedOperations"`     // 已生成接口数
	RouteDiscoveryCoverage  float64 `json:"routeDiscoveryCoverage"`  // 路由发现覆盖率
	ManagedSchemaTargets    int     `json:"managedSchemaTargets"`    // 纳管 Schema 总数
	ManagedKnownSchemas     int     `json:"managedKnownSchemas"`     // 已知 Schema 数
	ManagedSchemaCoverage   float64 `json:"managedSchemaCoverage"`   // 纳管 Schema 覆盖率
	UnknownManagedResponses int     `json:"unknownManagedResponses"` // 未知纳管响应数
	WarningDiagnostics      int     `json:"warningDiagnostics"`      // warning 数量
	ErrorDiagnostics        int     `json:"errorDiagnostics"`        // error 数量
}

// Operation 保存单条路由的可追溯分析结果
type Operation struct {
	Method             string               `json:"method"`                // HTTP 方法
	Path               string               `json:"path"`                  // 规范化路径
	OperationID        string               `json:"operationId,omitempty"` // OpenAPI 接口标识
	Mode               ir.DocumentationMode `json:"mode"`                  // 文档管理状态
	ModeReason         string               `json:"modeReason,omitempty"`  // 状态原因
	ModeSource         string               `json:"modeSource"`            // 状态来源
	Handler            ir.SymbolRef         `json:"handler"`               // Handler 符号
	RouteConfidence    ir.Confidence        `json:"routeConfidence"`       // 路由可信度
	RequestConfidence  ir.Confidence        `json:"requestConfidence"`     // 请求可信度
	ResponseConfidence ir.Confidence        `json:"responseConfidence"`    // 响应可信度
	Parameters         []ir.Parameter       `json:"-"`                     // explain 使用的接口参数
	RequestBody        *ir.RequestBody      `json:"-"`                     // explain 使用的请求体
	Responses          []ir.Response        `json:"-"`                     // explain 使用的响应
	OverrideApplied    bool                 `json:"overrideApplied"`       // 是否应用覆盖
	Diagnostics        []ir.Diagnostic      `json:"diagnostics,omitempty"` // 接口诊断
	Source             ir.SourceRef         `json:"source"`                // 路由源码位置
}

// QualityGate 保存一项质量门禁结论
type QualityGate struct {
	Name     string `json:"name"`     // 门禁名称
	Passed   bool   `json:"passed"`   // 是否通过
	Actual   string `json:"actual"`   // 实际值
	Expected string `json:"expected"` // 期望值
	Message  string `json:"message"`  // 结果说明
}

// Issue 保存 list-unresolved 的一条结果
type Issue struct {
	Operation string                `json:"operation,omitempty"` // 接口精确键
	Area      string                `json:"area"`                // 未解析区域
	Code      string                `json:"code"`                // 诊断代码
	Severity  ir.DiagnosticSeverity `json:"severity"`            // 严重级别
	Message   string                `json:"message"`             // 问题说明
	Source    ir.SourceRef          `json:"source"`              // 问题位置
}

// Build 从分析结果构造稳定报告
func Build(operations []ir.Operation, selection config.Selection, selectionPath string, appliedOverrides map[string]config.OperationOverride, diagnostics []ir.Diagnostic, generatedOperations int) Document {
	ordered := append([]ir.Operation(nil), operations...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Key().String() < ordered[j].Key().String() })

	document := Document{Diagnostics: sortedDiagnostics(diagnostics)}
	selectionSource := filepath.Base(selectionPath)
	for _, operation := range ordered {
		key := operation.Key().String()
		modeSource := "defaultMode"
		if _, found := selection.Operations[key]; found {
			modeSource = selectionSource
		}
		operationID := operation.OperationID
		if operationID == "" {
			operationID = apiopenapi.OperationID(operation.Method, operation.Path)
		}
		entry := Operation{
			Method:             operation.Method,
			Path:               operation.Path,
			OperationID:        operationID,
			Mode:               operation.Mode,
			ModeReason:         operation.ModeReason,
			ModeSource:         modeSource,
			Handler:            operation.Handler,
			RouteConfidence:    operation.Confidence,
			RequestConfidence:  requestConfidence(operation),
			ResponseConfidence: responseConfidence(operation),
			Parameters:         operation.Parameters,
			RequestBody:        operation.RequestBody,
			Responses:          operation.Responses,
			OverrideApplied:    hasOverride(appliedOverrides, key),
			Diagnostics:        sortedDiagnostics(operation.Diagnostics),
			Source:             operation.Source,
		}
		document.Operations = append(document.Operations, entry)
	}
	document.Summary = summarize(document, generatedOperations)
	return document
}

// Evaluate 计算配置化质量门禁并返回失败项
func Evaluate(document *Document, quality config.Quality, openAPIValid bool) []QualityGate {
	duplicateIDs := duplicateOperationIDs(document.Operations)
	unresolvedPaths := unresolvedManagedPaths(document.Operations)
	orphanSelections := diagnosticCount(document.Diagnostics, "selection.orphan")
	orphanOverrides := diagnosticCount(document.Diagnostics, "override.orphan_")

	gates := []QualityGate{
		percentageGate("routeDiscoveryCoverage", document.Summary.RouteDiscoveryCoverage, float64(quality.MinimumRouteDiscoveryCoverage)),
		percentageGate("managedSchemaCoverage", document.Summary.ManagedSchemaCoverage, float64(quality.MinimumManagedExactSchemaCoverage)),
		countGate("duplicateOperationId", duplicateIDs, quality.FailOnDuplicateOperationID),
		booleanGate("validOpenAPI", openAPIValid, quality.FailOnInvalidOpenAPI),
		countGate("unresolvedManagedPath", unresolvedPaths, quality.FailOnUnresolvedManagedPath),
		countGate("unknownManagedResponse", document.Summary.UnknownManagedResponses, quality.FailOnUnknownManagedResponse),
		countGate("orphanSelection", orphanSelections, quality.FailOnOrphanSelection),
		countGate("orphanOverride", orphanOverrides, quality.FailOnOrphanOverride),
	}
	document.Quality = gates

	var failures []QualityGate
	for _, gate := range gates {
		if !gate.Passed {
			failures = append(failures, gate)
		}
	}
	return failures
}

// Marshal 输出两空格缩进且带结尾换行的稳定 JSON
func Marshal(document Document) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// FindOperation 按精确键查找报告接口
func FindOperation(document Document, key ir.OperationKey) (Operation, bool) {
	target := key.String()
	index := sort.Search(len(document.Operations), func(i int) bool {
		return document.Operations[i].Method+" "+document.Operations[i].Path >= target
	})
	if index < len(document.Operations) && document.Operations[index].Method+" "+document.Operations[index].Path == target {
		return document.Operations[index], true
	}
	return Operation{}, false
}

// Unresolved 返回全部显式诊断和未知请求响应
func Unresolved(document Document) []Issue {
	issues := make([]Issue, 0)
	for _, diagnostic := range document.Diagnostics {
		issues = append(issues, issueFromDiagnostic("", "global", diagnostic))
	}
	for _, operation := range document.Operations {
		key := operation.Method + " " + operation.Path
		for _, diagnostic := range operation.Diagnostics {
			issues = append(issues, issueFromDiagnostic(key, "operation", diagnostic))
		}
		if operation.RequestConfidence == ir.ConfidenceUnknown && !hasDiagnosticPrefix(operation.Diagnostics, "request_", "parameter.") {
			issues = append(issues, Issue{
				Operation: key,
				Area:      "request",
				Code:      "request.unknown",
				Severity:  ir.SeverityWarning,
				Message:   "request schema contains unresolved types",
				Source:    operation.Source,
			})
		}
		if operation.ResponseConfidence == ir.ConfidenceUnknown && !hasDiagnosticPrefix(operation.Diagnostics, "response.") {
			issues = append(issues, Issue{
				Operation: key,
				Area:      "response",
				Code:      "response.unknown",
				Severity:  ir.SeverityWarning,
				Message:   "response schema contains unresolved types",
				Source:    operation.Source,
			})
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		left := issues[i].Operation + "\x00" + issues[i].Area + "\x00" + issues[i].Code + "\x00" + issues[i].Source.String()
		right := issues[j].Operation + "\x00" + issues[j].Area + "\x00" + issues[j].Code + "\x00" + issues[j].Source.String()
		return left < right
	})
	return issues
}

// FailureMessage 汇总质量门禁失败原因
func FailureMessage(failures []QualityGate) string {
	messages := make([]string, len(failures))
	for i, failure := range failures {
		messages[i] = failure.Name + " " + failure.Actual + " expected " + failure.Expected
	}
	return strings.Join(messages, "; ")
}

// summarize 统计报告概览
func summarize(document Document, generatedOperations int) Summary {
	summary := Summary{DiscoveredRoutes: len(document.Operations), GeneratedOperations: generatedOperations}
	for _, operation := range document.Operations {
		switch operation.Mode {
		case ir.ModeManaged:
			summary.Managed++
		case ir.ModeManual:
			summary.Manual++
		case ir.ModeIgnored:
			summary.Ignored++
		}
		if operation.Mode == ir.ModeManaged {
			targets, known := operationSchemaStats(operation)
			summary.ManagedSchemaTargets += targets
			summary.ManagedKnownSchemas += known
			if operation.ResponseConfidence == ir.ConfidenceUnknown {
				summary.UnknownManagedResponses++
			}
		}
		countDiagnostics(&summary, operation.Diagnostics)
	}
	countDiagnostics(&summary, document.Diagnostics)
	missingRoutes := diagnosticCount(document.Diagnostics, "route.")
	summary.RouteDiscoveryCoverage = percentage(summary.DiscoveredRoutes, summary.DiscoveredRoutes+missingRoutes)
	summary.ManagedSchemaCoverage = percentage(summary.ManagedKnownSchemas, summary.ManagedSchemaTargets)
	return summary
}

// operationSchemaStats 统计纳管接口的请求和响应 Schema
func operationSchemaStats(operation Operation) (targets, known int) {
	for _, parameter := range operation.Parameters {
		targets++
		if parameter.Confidence != ir.ConfidenceUnknown {
			known++
		}
	}
	if operation.RequestBody != nil {
		targets++
		if operation.RequestBody.Confidence != ir.ConfidenceUnknown {
			known++
		}
	}
	for _, response := range operation.Responses {
		for _, content := range response.Content {
			targets++
			if content.Confidence != ir.ConfidenceUnknown {
				known++
			}
		}
	}
	return targets, known
}

// requestConfidence 合并请求参数和 Body 的最低可信度
func requestConfidence(operation ir.Operation) ir.Confidence {
	result := ir.ConfidenceExact
	for _, parameter := range operation.Parameters {
		result = lowerConfidence(result, parameter.Confidence)
	}
	if operation.RequestBody != nil {
		result = lowerConfidence(result, operation.RequestBody.Confidence)
	}
	return result
}

// responseConfidence 合并响应内容和重定向的最低可信度
func responseConfidence(operation ir.Operation) ir.Confidence {
	if len(operation.Responses) == 0 {
		return ir.ConfidenceUnknown
	}
	result := ir.ConfidenceExact
	for _, response := range operation.Responses {
		for _, content := range response.Content {
			result = lowerConfidence(result, content.Confidence)
		}
		if response.RedirectConfidence != "" {
			result = lowerConfidence(result, response.RedirectConfidence)
		}
	}
	return result
}

// lowerConfidence 返回可信度较低的一项
func lowerConfidence(left, right ir.Confidence) ir.Confidence {
	rank := map[ir.Confidence]int{
		ir.ConfidenceUnknown:    0,
		ir.ConfidenceInferred:   1,
		ir.ConfidenceConfigured: 2,
		ir.ConfidenceOverride:   3,
		ir.ConfidenceExact:      4,
	}
	if rank[right] < rank[left] {
		return right
	}
	return left
}

// percentage 返回保留两位小数的百分比
func percentage(value, total int) float64 {
	if total == 0 {
		return 100
	}
	return math.Round(float64(value)*10000/float64(total)) / 100
}

// percentageGate 创建最低百分比门禁
func percentageGate(name string, actual, expected float64) QualityGate {
	return QualityGate{
		Name:     name,
		Passed:   actual >= expected,
		Actual:   formatPercentage(actual),
		Expected: ">=" + formatPercentage(expected),
		Message:  "minimum coverage threshold",
	}
}

// countGate 创建可开关的零计数门禁
func countGate(name string, count int, enabled bool) QualityGate {
	gate := QualityGate{
		Name:     name,
		Passed:   !enabled || count == 0,
		Actual:   strconv.Itoa(count),
		Expected: "0",
		Message:  "no matching issues",
	}
	if !enabled {
		gate.Expected = "disabled"
		gate.Message = "quality gate is disabled"
	}
	return gate
}

// booleanGate 创建可开关的布尔门禁
func booleanGate(name string, actual, enabled bool) QualityGate {
	gate := QualityGate{
		Name:     name,
		Passed:   !enabled || actual,
		Actual:   strconv.FormatBool(actual),
		Expected: "true",
		Message:  "required validation result",
	}
	if !enabled {
		gate.Expected = "disabled"
		gate.Message = "quality gate is disabled"
	}
	return gate
}

// duplicateOperationIDs 统计纳管接口的重复 OperationID
func duplicateOperationIDs(operations []Operation) int {
	seen := make(map[string]bool)
	duplicates := 0
	for _, operation := range operations {
		if operation.Mode != ir.ModeManaged {
			continue
		}
		if seen[operation.OperationID] {
			duplicates++
		}
		seen[operation.OperationID] = true
	}
	return duplicates
}

// unresolvedManagedPaths 统计未从 Handler 读取的纳管 Path 参数
func unresolvedManagedPaths(operations []Operation) int {
	count := 0
	for _, operation := range operations {
		if operation.Mode != ir.ModeManaged {
			continue
		}
		for _, parameter := range operation.Parameters {
			if parameter.Location == ir.ParameterPath && parameter.Confidence == ir.ConfidenceUnknown {
				count++
			}
		}
	}
	return count
}

// diagnosticCount 统计指定代码前缀的全局诊断
func diagnosticCount(diagnostics []ir.Diagnostic, prefix string) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if strings.HasPrefix(diagnostic.Code, prefix) {
			count++
		}
	}
	return count
}

// hasDiagnosticPrefix 判断诊断是否已解释对应未知项
func hasDiagnosticPrefix(diagnostics []ir.Diagnostic, prefixes ...string) bool {
	for _, diagnostic := range diagnostics {
		for _, prefix := range prefixes {
			if strings.HasPrefix(diagnostic.Code, prefix) {
				return true
			}
		}
	}
	return false
}

// countDiagnostics 汇总 warning 和 error
func countDiagnostics(summary *Summary, diagnostics []ir.Diagnostic) {
	for _, diagnostic := range diagnostics {
		switch diagnostic.Severity {
		case ir.SeverityWarning:
			summary.WarningDiagnostics++
		case ir.SeverityError:
			summary.ErrorDiagnostics++
		}
	}
}

// sortedDiagnostics 返回稳定排序的诊断副本
func sortedDiagnostics(diagnostics []ir.Diagnostic) []ir.Diagnostic {
	result := append([]ir.Diagnostic(nil), diagnostics...)
	sort.Slice(result, func(i, j int) bool {
		left := result[i].Source.String() + "\x00" + string(result[i].Severity) + "\x00" + result[i].Code + "\x00" + result[i].Message
		right := result[j].Source.String() + "\x00" + string(result[j].Severity) + "\x00" + result[j].Code + "\x00" + result[j].Message
		return left < right
	})
	return result
}

// issueFromDiagnostic 转换统一诊断为未解析问题
func issueFromDiagnostic(operation, area string, diagnostic ir.Diagnostic) Issue {
	return Issue{
		Operation: operation,
		Area:      area,
		Code:      diagnostic.Code,
		Severity:  diagnostic.Severity,
		Message:   diagnostic.Message,
		Source:    diagnostic.Source,
	}
}

// hasOverride 判断接口是否命中有效覆盖
func hasOverride(overrides map[string]config.OperationOverride, key string) bool {
	_, found := overrides[key]
	return found
}

// formatPercentage 格式化报告中的百分比
func formatPercentage(value float64) string {
	return fmt.Sprintf("%.2f%%", value)
}
