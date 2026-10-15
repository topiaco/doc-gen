package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

const appName = "apidoc-gen"

// main 执行命令并将结果作为进程退出码
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run 解析命令，默认命令与 generate 都在生成文档的过程中完成检查
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runSourceGenerate(nil, stdout, stderr, true)
	}
	switch args[0] {
	case "help", "-h", "--help":
		writeUsage(stdout)
		return 0
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "%s %s\n", appName, currentVersion())
		return 0
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "generate":
		return runSourceGenerate(args[1:], stdout, stderr, true)
	case "validate":
		return runSourceGenerate(args[1:], stdout, stderr, false)
	default:
		if strings.HasPrefix(args[0], "-") {
			return runSourceGenerate(args, stdout, stderr, true)
		}
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		writeUsage(stderr)
		return 2
	}
}

// currentVersion 从 Go Build Info 读取安装时的 Module 标签
func currentVersion() string {
	return buildVersion(debug.ReadBuildInfo())
}

// buildVersion 返回安装版本，源码构建时返回 dev
func buildVersion(info *debug.BuildInfo, ok bool) string {
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return info.Main.Version
}

// writeUsage 展示初始化、文档生成和可选的只读检查命令
func writeUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  apidoc-gen [--project-root DIR]
  apidoc-gen init [--existing] [--github] [--project-root DIR]
  apidoc-gen generate [--project-root DIR]
  apidoc-gen validate [--project-root DIR]
  apidoc-gen version

Generate OpenAPI, offline HTML and a diagnostic report directly from Go/Gin source.
Initialize once; run apidoc-gen after code changes. No schema or selection configuration is needed.
`)
}
