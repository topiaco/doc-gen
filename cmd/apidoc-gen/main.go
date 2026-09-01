package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/topiaco/doc-gen/internal/analyzer"
	"github.com/topiaco/doc-gen/internal/config"
	"github.com/topiaco/doc-gen/internal/ir"
	"github.com/topiaco/doc-gen/internal/override"
	"github.com/topiaco/doc-gen/internal/schema"
	"github.com/topiaco/doc-gen/internal/selection"
)

const appName = "apidoc-gen"

var version = "dev"

// main 运行 API 文档生成命令
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 分发 CLI 命令并返回进程退出码
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stdout)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		writeUsage(stdout)
		return 0
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "%s %s\n", appName, version)
		return 0
	case "generate":
		return runGenerate(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "explain":
		return runExplain(args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout, stderr)
	case "list-unresolved":
		return runListUnresolved(args[1:], stdout, stderr)
	case "check-overrides":
		return runCheckOverrides(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		writeUsage(stderr)
		return 2
	}
}

// runCheckOverrides 分析源码并校验所有 Override
func runCheckOverrides(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("check-overrides", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { writeCheckOverridesUsage(stderr) }
	projectRoot := flags.String("project-root", ".", "project root directory")
	configPath := flags.String("config", "api-doc/config.yaml", "configuration file relative to project root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "check-overrides does not accept positional arguments\n\n")
		writeCheckOverridesUsage(stderr)
		return 2
	}

	bundle, err := config.Load(*projectRoot, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "check-overrides: %v\n", err)
		return 1
	}
	program, operations, err := analyzeOperations(bundle)
	if err != nil {
		fmt.Fprintf(stderr, "check-overrides: %v\n", err)
		return 1
	}
	result := override.Apply(operations, bundle.Overrides, schema.NewBuilder(program, bundle.Config.SchemaMappings))
	for _, entry := range result.Valid {
		fmt.Fprintf(stdout, "valid\t%s\t%s\t%s\n", entry.Kind, entry.Key, entry.Source)
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintln(stdout, override.DiagnosticLine(diagnostic))
	}
	valid, warnings, errors := result.Counts()
	fmt.Fprintf(stdout, "summary\tvalid=%d\twarnings=%d\terrors=%d\n", valid, warnings, errors)
	if result.HasErrors() {
		return 1
	}
	return 0
}

// analysisResult 保存完整源码分析结果
type analysisResult struct {
	Program     *analyzer.Program // 已加载源码
	Operations  []ir.Operation    // 已分析路由
	Diagnostics []ir.Diagnostic   // 路由发现诊断
}

// analyzeProject 运行完整静态分析链并保留发现诊断
func analyzeProject(bundle *config.Bundle) (analysisResult, error) {
	routeProgram, err := analyzer.Load(context.Background(), bundle.Paths.ProjectRoot, bundle.Config.Source.RoutePackages)
	if err != nil {
		return analysisResult{}, err
	}
	discovery := analyzer.DiscoverGinRoutes(routeProgram)
	operations, err := selection.Apply(discovery.Operations, bundle.Selection)
	if err != nil {
		return analysisResult{}, err
	}
	patterns := append([]string{}, bundle.Config.Source.RoutePackages...)
	patterns = append(patterns, bundle.Config.Source.AnalysisPackages...)
	program, err := analyzer.Load(context.Background(), bundle.Paths.ProjectRoot, patterns)
	if err != nil {
		return analysisResult{}, err
	}
	resolutions := analyzer.ResolveHandlers(program, operations)
	resolutions = analyzer.AnalyzeParameters(program, resolutions, bundle.Config.ParameterHelpers)
	resolutions = analyzer.AnalyzeJSONBodies(program, resolutions)
	resolutions = analyzer.AnalyzeJSONResponses(program, resolutions, bundle.Config.ResponseFunctions)
	resolutions = analyzer.AnalyzeDirectResponses(program, resolutions, bundle.Config.ResponseFunctions)
	analyzed := make([]ir.Operation, len(resolutions))
	for i := range resolutions {
		analyzed[i] = resolutions[i].Operation
		if resolutions[i].Diagnostic != nil {
			analyzed[i].Diagnostics = append(analyzed[i].Diagnostics, *resolutions[i].Diagnostic)
		}
	}
	return analysisResult{Program: program, Operations: analyzed, Diagnostics: discovery.Diagnostics}, nil
}

// analyzeOperations 保持需要完整路由发现的既有分析入口
func analyzeOperations(bundle *config.Bundle) (*analyzer.Program, []ir.Operation, error) {
	result, err := analyzeProject(bundle)
	if err != nil {
		return nil, nil, err
	}
	if len(result.Diagnostics) > 0 {
		diagnostic := result.Diagnostics[0]
		return nil, nil, fmt.Errorf("%s: %s: %s", diagnostic.Source, diagnostic.Code, diagnostic.Message)
	}
	return result.Program, result.Operations, nil
}

// runList 发现路由并按 Selection 状态输出
func runList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { writeListUsage(stderr) }
	projectRoot := flags.String("project-root", ".", "project root directory")
	configPath := flags.String("config", "api-doc/config.yaml", "configuration file relative to project root")
	mode := flags.String("mode", "", "filter by managed, manual, or ignored")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "list does not accept positional arguments\n\n")
		writeListUsage(stderr)
		return 2
	}

	selectedMode := ir.DocumentationMode(*mode)
	if selectedMode != "" && selectedMode != ir.ModeManaged && selectedMode != ir.ModeManual && selectedMode != ir.ModeIgnored {
		fmt.Fprintf(stderr, "invalid list mode %q\n\n", *mode)
		writeListUsage(stderr)
		return 2
	}

	bundle, err := config.Load(*projectRoot, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "list: %v\n", err)
		return 1
	}
	program, err := analyzer.Load(context.Background(), bundle.Paths.ProjectRoot, bundle.Config.Source.RoutePackages)
	if err != nil {
		fmt.Fprintf(stderr, "list: %v\n", err)
		return 1
	}
	discovery := analyzer.DiscoverGinRoutes(program)
	if len(discovery.Diagnostics) != 0 {
		for _, diagnostic := range discovery.Diagnostics {
			fmt.Fprintf(stderr, "%s: %s: %s\n", diagnostic.Source, diagnostic.Code, diagnostic.Message)
		}
		return 1
	}
	operations, err := selection.Apply(discovery.Operations, bundle.Selection)
	if err != nil {
		fmt.Fprintf(stderr, "list: %v\n", err)
		return 1
	}

	for _, operation := range selection.Filter(operations, selectedMode) {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", operation.Mode, operation.Key(), operation.ModeReason)
	}
	return 0
}

// writeUsage 输出命令帮助
func writeUsage(w io.Writer) {
	fmt.Fprint(w, `apidoc-gen generates OpenAPI documentation from Go source code.

Usage:
  apidoc-gen <command> [options]

Commands:
  generate         Generate OpenAPI and analysis report
  validate         Validate source analysis and generated documentation
  explain          Explain how one operation was analyzed
  list             List discovered operations by Selection mode
  list-unresolved  List unresolved analysis results
  check-overrides  Validate documentation overrides
  version          Print version information
  help             Print this help
`)
}

// writeListUsage 输出 list 命令帮助
func writeListUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  apidoc-gen list [--mode managed|manual|ignored] [--project-root DIR] [--config FILE]
`)
}

// writeCheckOverridesUsage 输出 check-overrides 帮助
func writeCheckOverridesUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  apidoc-gen check-overrides [--project-root DIR] [--config FILE]
`)
}
