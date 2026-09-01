package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
	apiopenapi "github.com/topiaco/doc-gen/internal/openapi"
	"github.com/topiaco/doc-gen/internal/output"
	"github.com/topiaco/doc-gen/internal/override"
	"github.com/topiaco/doc-gen/internal/report"
	"github.com/topiaco/doc-gen/internal/schema"
)

// projectOptions 保存通用项目参数
type projectOptions struct {
	ProjectRoot string // 项目根目录
	Config      string // 配置文件路径
}

// projectAnalysis 保存分析、覆盖和 Schema 构建状态
type projectAnalysis struct {
	Source  analysisResult  // 源码分析结果
	Applied override.Result // Override 结果
	Builder *schema.Builder // Schema 构建器
}

// generatedArtifacts 保存一次完整生成结果
type generatedArtifacts struct {
	OpenAPI    apiopenapi.Document // OpenAPI 文档
	Report     report.Document     // 分析报告
	ReportJSON []byte              // 报告 JSON
}

// runGenerate 生成并原子写入 OpenAPI 与报告
func runGenerate(args []string, stdout, stderr io.Writer) int {
	options, _, exitCode := parseProjectOptions("generate", args, stderr, writeGenerateUsage, 0)
	if exitCode != 0 {
		return exitCode
	}
	bundle, err := config.Load(options.ProjectRoot, options.Config)
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	artifacts, err := buildArtifacts(bundle)
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	if err := output.WriteAtomic(
		output.File{Path: bundle.Paths.Output, Data: artifacts.OpenAPI.JSON},
		output.File{Path: bundle.Paths.Report, Data: artifacts.ReportJSON},
	); err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "generated\toperations=%d\topenapi=%s\treport=%s\n",
		artifacts.Report.Summary.GeneratedOperations, bundle.Paths.Output, bundle.Paths.Report)
	return 0
}

// runValidate 执行完整分析和质量门禁但不写文件
func runValidate(args []string, stdout, stderr io.Writer) int {
	options, _, exitCode := parseProjectOptions("validate", args, stderr, writeValidateUsage, 0)
	if exitCode != 0 {
		return exitCode
	}
	bundle, err := config.Load(options.ProjectRoot, options.Config)
	if err != nil {
		fmt.Fprintf(stderr, "validate: %v\n", err)
		return 1
	}
	artifacts, err := buildArtifacts(bundle)
	if len(artifacts.Report.Quality) > 0 {
		writeQuality(stdout, artifacts.Report)
	}
	if err != nil {
		fmt.Fprintf(stderr, "validate: %v\n", err)
		return 1
	}
	return 0
}

// runExplain 展示一条接口的分析来源和生成结论
func runExplain(args []string, stdout, stderr io.Writer) int {
	options, positional, exitCode := parseProjectOptions("explain", args, stderr, writeExplainUsage, 1)
	if exitCode != 0 {
		return exitCode
	}
	key, err := ir.ParseOperationKey(positional[0])
	if err != nil {
		fmt.Fprintf(stderr, "explain: %v\n\n", err)
		writeExplainUsage(stderr)
		return 2
	}
	bundle, err := config.Load(options.ProjectRoot, options.Config)
	if err != nil {
		fmt.Fprintf(stderr, "explain: %v\n", err)
		return 1
	}
	document, err := buildAnalysisReport(bundle)
	if err != nil {
		fmt.Fprintf(stderr, "explain: %v\n", err)
		return 1
	}
	operation, found := report.FindOperation(document, key)
	if !found {
		fmt.Fprintf(stderr, "explain: operation not found: %s\n", key)
		return 1
	}
	writeExplanation(stdout, operation, document.Diagnostics)
	return 0
}

// runListUnresolved 列出显式诊断和未知请求响应
func runListUnresolved(args []string, stdout, stderr io.Writer) int {
	options, _, exitCode := parseProjectOptions("list-unresolved", args, stderr, writeListUnresolvedUsage, 0)
	if exitCode != 0 {
		return exitCode
	}
	bundle, err := config.Load(options.ProjectRoot, options.Config)
	if err != nil {
		fmt.Fprintf(stderr, "list-unresolved: %v\n", err)
		return 1
	}
	document, err := buildAnalysisReport(bundle)
	if err != nil {
		fmt.Fprintf(stderr, "list-unresolved: %v\n", err)
		return 1
	}
	issues := report.Unresolved(document)
	for _, issue := range issues {
		operation := issue.Operation
		if operation == "" {
			operation = "-"
		}
		source := issue.Source.String()
		if source == "" {
			source = "-"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%s\n",
			issue.Severity, issue.Code, operation, issue.Area, source, issue.Message)
	}
	fmt.Fprintf(stdout, "summary\tunresolved=%d\n", len(issues))
	return 0
}

// buildArtifacts 构建 OpenAPI、报告并执行质量门禁
func buildArtifacts(bundle *config.Bundle) (generatedArtifacts, error) {
	state, err := analyzeAndApply(bundle)
	if err != nil {
		return generatedArtifacts{}, err
	}
	reportDocument := reportFromAnalysis(bundle, state, countManaged(state.Applied.Operations))
	if state.Applied.HasErrors() {
		report.Evaluate(&reportDocument, bundle.Config.Quality, false)
		reportJSON, _ := report.Marshal(reportDocument)
		_, _, errorCount := state.Applied.Counts()
		return generatedArtifacts{Report: reportDocument, ReportJSON: reportJSON}, fmt.Errorf("override validation failed with %d errors", errorCount)
	}

	openAPIDocument, err := apiopenapi.Assemble(bundle.Config.Project, state.Applied, state.Builder)
	if err != nil {
		report.Evaluate(&reportDocument, bundle.Config.Quality, false)
		reportJSON, _ := report.Marshal(reportDocument)
		return generatedArtifacts{Report: reportDocument, ReportJSON: reportJSON}, err
	}
	reportDocument.Summary.GeneratedOperations = countGeneratedOperations(openAPIDocument)
	failures := report.Evaluate(&reportDocument, bundle.Config.Quality, true)
	reportJSON, err := report.Marshal(reportDocument)
	if err != nil {
		return generatedArtifacts{}, fmt.Errorf("marshal report: %w", err)
	}
	artifacts := generatedArtifacts{OpenAPI: openAPIDocument, Report: reportDocument, ReportJSON: reportJSON}
	if len(failures) > 0 {
		return artifacts, fmt.Errorf("quality gates failed: %s", report.FailureMessage(failures))
	}
	return artifacts, nil
}

// buildAnalysisReport 构建 explain 和 list-unresolved 使用的只读报告
func buildAnalysisReport(bundle *config.Bundle) (report.Document, error) {
	state, err := analyzeAndApply(bundle)
	if err != nil {
		return report.Document{}, err
	}
	return reportFromAnalysis(bundle, state, countManaged(state.Applied.Operations)), nil
}

// analyzeAndApply 执行源码分析和 Override 合并
func analyzeAndApply(bundle *config.Bundle) (projectAnalysis, error) {
	source, err := analyzeProject(bundle)
	if err != nil {
		return projectAnalysis{}, err
	}
	builder := schema.NewBuilder(source.Program, bundle.Config.SchemaMappings)
	applied := override.Apply(source.Operations, bundle.Overrides, builder)
	return projectAnalysis{Source: source, Applied: applied, Builder: builder}, nil
}

// reportFromAnalysis 合并路由发现与 Override 诊断
func reportFromAnalysis(bundle *config.Bundle, state projectAnalysis, generatedOperations int) report.Document {
	diagnostics := append([]ir.Diagnostic(nil), state.Source.Diagnostics...)
	diagnostics = append(diagnostics, state.Applied.Diagnostics...)
	return report.Build(
		state.Applied.Operations,
		bundle.Selection,
		bundle.Paths.Selection,
		state.Applied.OperationOverrides,
		diagnostics,
		generatedOperations,
	)
}

// parseProjectOptions 解析通用项目参数和固定数量位置参数
func parseProjectOptions(command string, args []string, stderr io.Writer, usage func(io.Writer), positionalCount int) (projectOptions, []string, int) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { usage(stderr) }
	projectRoot := flags.String("project-root", ".", "project root directory")
	configPath := flags.String("config", "api-doc/config.yaml", "configuration file relative to project root")
	if err := flags.Parse(args); err != nil {
		return projectOptions{}, nil, 2
	}
	if flags.NArg() != positionalCount {
		fmt.Fprintf(stderr, "%s expects %d positional arguments\n\n", command, positionalCount)
		usage(stderr)
		return projectOptions{}, nil, 2
	}
	return projectOptions{ProjectRoot: *projectRoot, Config: *configPath}, flags.Args(), 0
}

// writeQuality 输出质量门禁和核心统计
func writeQuality(writer io.Writer, document report.Document) {
	for _, gate := range document.Quality {
		status := "pass"
		if !gate.Passed {
			status = "fail"
		}
		fmt.Fprintf(writer, "%s\t%s\tactual=%s\texpected=%s\t%s\n", status, gate.Name, gate.Actual, gate.Expected, gate.Message)
	}
	summary := document.Summary
	fmt.Fprintf(writer, "summary\troutes=%d\tmanaged=%d\tmanual=%d\tignored=%d\tgenerated=%d\tunknownResponses=%d\n",
		summary.DiscoveredRoutes, summary.Managed, summary.Manual, summary.Ignored,
		summary.GeneratedOperations, summary.UnknownManagedResponses)
}

// writeExplanation 输出单条接口的分析详情
func writeExplanation(writer io.Writer, operation report.Operation, globalDiagnostics []ir.Diagnostic) {
	key := operation.Method + " " + operation.Path
	fmt.Fprintf(writer, "operation\t%s\n", key)
	fmt.Fprintf(writer, "operation-id\t%s\n", operation.OperationID)
	fmt.Fprintf(writer, "mode\t%s\n", operation.Mode)
	fmt.Fprintf(writer, "mode-source\t%s\n", operation.ModeSource)
	fmt.Fprintf(writer, "mode-reason\t%s\n", operation.ModeReason)
	fmt.Fprintf(writer, "route-source\t%s\n", operation.Source)
	fmt.Fprintf(writer, "route-confidence\t%s\n", operation.RouteConfidence)
	fmt.Fprintf(writer, "handler\t%s\t%s\n", operation.Handler, operation.Handler.Source)
	fmt.Fprintf(writer, "request-confidence\t%s\n", operation.RequestConfidence)
	fmt.Fprintf(writer, "response-confidence\t%s\n", operation.ResponseConfidence)
	fmt.Fprintf(writer, "override\t%t\n", operation.OverrideApplied)

	for _, parameter := range operation.Parameters {
		fmt.Fprintf(writer, "parameter\t%s\t%s\t%s\trequired=%t\tconfidence=%s\tsource=%s\n",
			parameter.Location, parameter.Name, parameter.Type.String(), parameter.Required, parameter.Confidence, parameter.Source)
	}
	if operation.RequestBody != nil {
		body := operation.RequestBody
		fmt.Fprintf(writer, "request-body\t%s\t%s\trequired=%t\tconfidence=%s\tsource=%s\n",
			body.ContentType, body.Type.String(), body.Required, body.Confidence, body.Source)
		writeTypeFields(writer, "request", body.Type)
	}
	for _, response := range operation.Responses {
		if len(response.Content) == 0 {
			fmt.Fprintf(writer, "response\t%d\t%s\tsource=%s\n", response.Status, response.Description, response.Source)
		}
		for _, content := range response.Content {
			typeName := "null"
			if content.Binary {
				typeName = "binary"
			} else if content.Type != nil {
				typeName = content.Type.String()
			}
			fmt.Fprintf(writer, "response\t%d\t%s\t%s\twrapper=%s\tconfidence=%s\tsource=%s\n",
				response.Status, content.MediaType, typeName, content.Wrapper, content.Confidence, response.Source)
			if content.Type != nil {
				writeTypeFields(writer, "response", *content.Type)
			}
		}
	}
	for _, diagnostic := range operation.Diagnostics {
		fmt.Fprintf(writer, "diagnostic\t%s\t%s\t%s\t%s\n", diagnostic.Severity, diagnostic.Code, diagnostic.Source, diagnostic.Message)
	}
	for _, diagnostic := range globalDiagnostics {
		if strings.Contains(diagnostic.Message, key) {
			fmt.Fprintf(writer, "global-diagnostic\t%s\t%s\t%s\t%s\n", diagnostic.Severity, diagnostic.Code, diagnostic.Source, diagnostic.Message)
		}
	}
}

// writeTypeFields 输出请求或响应类型携带的字段元数据
func writeTypeFields(writer io.Writer, prefix string, value ir.TypeRef) {
	for _, field := range value.Fields {
		name := field.JSONName
		if name == "" {
			name = field.Name
		}
		path := prefix + "." + name
		fmt.Fprintf(writer, "field\t%s\t%s\trequired=%t\tconfidence=%s\tsource=%s\tdescription=%s\n",
			path, field.Type.String(), field.Required, field.Confidence, field.Source, field.Description)
		writeTypeFields(writer, path, field.Type)
	}
	if value.Element != nil {
		elementPrefix := prefix
		if value.Kind == ir.TypeSlice || value.Kind == ir.TypeArray {
			elementPrefix += "[]"
		}
		writeTypeFields(writer, elementPrefix, *value.Element)
	}
}

// countManaged 统计纳管接口数量
func countManaged(operations []ir.Operation) int {
	count := 0
	for _, operation := range operations {
		if operation.Mode == ir.ModeManaged {
			count++
		}
	}
	return count
}

// countGeneratedOperations 统计 OpenAPI 中的接口数量
func countGeneratedOperations(document apiopenapi.Document) int {
	count := 0
	for _, path := range document.Spec.Paths.MapOfPathItemValues {
		count += len(path.MapOfOperationValues)
	}
	return count
}

// writeGenerateUsage 输出 generate 帮助
func writeGenerateUsage(writer io.Writer) {
	fmt.Fprint(writer, `Usage:
  apidoc-gen generate [--project-root DIR] [--config FILE]
`)
}

// writeValidateUsage 输出 validate 帮助
func writeValidateUsage(writer io.Writer) {
	fmt.Fprint(writer, `Usage:
  apidoc-gen validate [--project-root DIR] [--config FILE]
`)
}

// writeExplainUsage 输出 explain 帮助
func writeExplainUsage(writer io.Writer) {
	fmt.Fprint(writer, `Usage:
  apidoc-gen explain [--project-root DIR] [--config FILE] "METHOD /normalized/path"
`)
}

// writeListUnresolvedUsage 输出 list-unresolved 帮助
func writeListUnresolvedUsage(writer io.Writer) {
	fmt.Fprint(writer, `Usage:
  apidoc-gen list-unresolved [--project-root DIR] [--config FILE]
`)
}
