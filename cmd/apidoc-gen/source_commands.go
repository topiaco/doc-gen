package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/topiaco/doc-gen/internal/output"
	"github.com/topiaco/doc-gen/internal/project"
)

// runInit 初始化项目，先检查工作流参数，再保存存量快照
func runInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("project-root", ".", "project directory (automatically locate its Go module)")
	existing := flags.Bool("existing", false, "snapshot existing endpoints; only later endpoints are generated")
	github := flags.Bool("github", false, "create a GitHub Actions workflow")
	title := flags.String("title", "", "API title (defaults to module name)")
	version := flags.String("api-version", "unversioned", "API version")
	generator := flags.String("generator-version", currentVersion(), "published generator version or commit for CI")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "init does not accept positional arguments")
		return 2
	}
	resolved, err := project.Locate(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var workflow output.File
	if *github {
		workflow, err = project.Workflow(resolved, *generator)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	state, created, diagnostics, err := project.Initialize(resolved, *existing, *title, *version)
	if err != nil {
		fmt.Fprintf(stderr, "init: %v\n", err)
		return 1
	}
	if *github {
		if err := output.WriteAtomic(workflow); err != nil {
			fmt.Fprintf(stderr, "init: %v\n", err)
			return 1
		}
	}
	verb := "initialized"
	if !created {
		verb = "already initialized; baseline preserved"
	}
	fmt.Fprintf(stdout, "%s\tmodule=%s\texisting=%d\n", verb, state.Module, len(state.Baseline))
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(stdout, "warning\t%s\t%s\t%s; unresolved routes are not included in the baseline\n", diagnostic.Code, diagnostic.Source, diagnostic.Message)
	}
	fmt.Fprintf(stdout, "Run apidoc-gen to generate documentation. Commit %s and the initialization files once.\n", project.StateFile)

	return 0
}

// runSourceGenerate 为本地和 CI 执行相同的分析流程，只读检查时不写入生成文件
func runSourceGenerate(args []string, stdout, stderr io.Writer, write bool) int {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("project-root", ".", "project directory (automatically locate its Go module)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "generate does not accept positional arguments")
		return 2
	}
	resolved, err := project.Locate(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	artifacts, err := project.Generate(resolved)
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}
	if write {
		if err := artifacts.Write(resolved); err != nil {
			fmt.Fprintf(stderr, "generate: %v\n", err)
			return 1
		}
	}
	summary := artifacts.Report.Summary
	fmt.Fprintf(stdout, "generated\toperations=%d\texcluded=%d\twarnings=%d\n", summary.Generated, summary.Excluded, summary.Warnings)
	if write {
		fmt.Fprintf(stdout, "OpenAPI: %s\nPreview: %s\n", filepath.Join(resolved, project.OutputDir, "openapi.json"), filepath.Join(resolved, project.OutputDir, "index.html"))
	}
	for _, issue := range artifacts.Report.Issues {
		fmt.Fprintf(stdout, "warning\t%s\t%s\t%s\t%s\n", issue.Code, issue.Source, strings.Join(issue.Operations, ", "), issue.Message)
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		for _, issue := range artifacts.Report.Issues {
			fmt.Fprintf(stdout, "::warning file=%s,line=%d::%s\n", workflowEscape(filepath.ToSlash(filepath.Join(relativeToGit(resolved), issue.Source.File))), issue.Source.Line, workflowEscape(issue.Message))
		}
		if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
			text := fmt.Sprintf("## API documentation\n\nGenerated **%d** operations; excluded %d existing/ignored operations; %d warnings.\n\nDownload the workflow artifact and open `index.html` for the offline reference.\n", summary.Generated, summary.Excluded, summary.Warnings)
			for _, issue := range artifacts.Report.Issues {
				text += fmt.Sprintf("\n- `%s` — %s (%s)\n", issue.Source, strings.ReplaceAll(issue.Message, "\n", " "), strings.Join(issue.Operations, ", "))
			}
			if file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644); err == nil {
				_, err = file.WriteString(text)
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					fmt.Fprintln(stderr, "warning: could not write GitHub summary")
				}
			} else {
				fmt.Fprintf(stderr, "warning: GitHub summary: %v\n", err)
			}
		}
	}
	return 0
}

// relativeToGit 计算业务 Module 相对于 Git 根目录的路径
func relativeToGit(root string) string {
	for dir := root; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			relative, _ := filepath.Rel(dir, root)
			return relative
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return "."
}

// workflowEscape 转义 GitHub Actions 注解中的特殊字符
func workflowEscape(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ",", "%2C", ":", "%3A").Replace(s)
}
