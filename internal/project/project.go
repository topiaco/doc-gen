// Package project 根据存量快照选择接口，再执行源码分析、文档生成和文件写入
package project

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/topiaco/doc-gen/internal/analyzer"
	"github.com/topiaco/doc-gen/internal/ir"
	apiopenapi "github.com/topiaco/doc-gen/internal/openapi"
	"github.com/topiaco/doc-gen/internal/output"
	"github.com/topiaco/doc-gen/internal/preview"
	"github.com/topiaco/doc-gen/internal/schema"
	"golang.org/x/mod/modfile"
)

const StateFile = ".apidoc/state.json"
const OutputDir = ".apidoc/output"

// State 保存初始化时的项目信息，以及由 HTTP 方法和规范化路径标识的存量接口
type State struct {
	Version    int      `json:"version"`
	Module     string   `json:"module"`
	Title      string   `json:"title"`
	APIVersion string   `json:"apiVersion"`
	Baseline   []string `json:"baseline"`
}

type Summary struct {
	Discovered int `json:"discovered"`
	Generated  int `json:"generated"`
	Excluded   int `json:"excluded"`
	Warnings   int `json:"warnings"`
}

// Issue 将同一源码问题合并为一条记录，并列出所有受影响接口
type Issue struct {
	Code       string                `json:"code"`
	Severity   ir.DiagnosticSeverity `json:"severity"`
	Message    string                `json:"message"`
	Source     ir.SourceRef          `json:"source"`
	Operations []string              `json:"operations,omitempty"`
}

type Report struct {
	Summary    Summary        `json:"summary"`
	Operations []ir.Operation `json:"operations"`
	Issues     []Issue        `json:"issues"`
}

type Artifacts struct {
	OpenAPI    []byte
	HTML       []byte
	ReportJSON []byte
	Report     Report
}

// Locate 优先定位所在 Module，否则选择仓库内唯一的 Module
func Locate(start string) (string, error) {
	absolute, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for dir := absolute; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	var candidates []string
	err = filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != absolute && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "node_modules" || entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				candidates = append(candidates, path)
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no Go module found in %s; use --project-root DIR", absolute)
	}
	sort.Strings(candidates)
	return "", fmt.Errorf("multiple Go modules found; choose --project-root: %s", strings.Join(candidates, ", "))
}

// moduleName 从业务项目的 go.mod 读取 Module 名称
func moduleName(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	name := modfile.ModulePath(data)
	if name == "" {
		return "", fmt.Errorf("go.mod has no module declaration")
	}
	return name, nil
}

// ReadState 读取初始化记录，并确认记录属于当前 Module
func ReadState(root string) (State, error) {
	data, err := os.ReadFile(filepath.Join(root, StateFile))
	if os.IsNotExist(err) {
		return State{}, fmt.Errorf("project is not initialized; run apidoc-gen init --existing")
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("read %s: %w", StateFile, err)
	}
	if state.Version != 1 {
		return State{}, fmt.Errorf("unsupported state version %d", state.Version)
	}
	module, err := moduleName(root)
	if err != nil {
		return State{}, err
	}
	if state.Module != module {
		return State{}, fmt.Errorf("state belongs to module %s, but go.mod declares %s", state.Module, module)
	}
	return state, nil
}

// Initialize 首次执行时创建存量快照，再次执行时返回已有记录
func Initialize(root string, existing bool, title, version string) (State, bool, []ir.Diagnostic, error) {
	if _, err := os.Stat(filepath.Join(root, StateFile)); err == nil {
		state, err := ReadState(root)
		return state, false, nil, err
	} else if !os.IsNotExist(err) {
		return State{}, false, nil, err
	}
	module, err := moduleName(root)
	if err != nil {
		return State{}, false, nil, err
	}
	if title == "" {
		title = module
	}
	if version == "" {
		version = "unversioned"
	}
	state := State{Version: 1, Module: module, Title: title, APIVersion: version, Baseline: []string{}}
	var diagnostics []ir.Diagnostic
	if existing {
		program, err := analyzer.Load(context.Background(), root, []string{"./..."})
		if err != nil {
			return State{}, false, nil, err
		}
		source := analyzer.NewSourceAnalyzer(program)
		discovery := source.Discover()
		diagnostics = discovery.Diagnostics
		if len(program.AnnotationErrors) > 0 {
			d := program.AnnotationErrors[0]
			return State{}, false, nil, fmt.Errorf("%s: %s", d.Source, d.Message)
		}
		if err := checkRoutes(discovery.Operations); err != nil {
			return State{}, false, nil, err
		}
		for _, op := range discovery.Operations {
			state.Baseline = append(state.Baseline, op.Key().String())
		}
		sort.Strings(state.Baseline)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return State{}, false, nil, err
	}
	data = append(data, '\n')
	ignorePath := filepath.Join(root, ".gitignore")
	ignore, err := os.ReadFile(ignorePath)
	if err != nil && !os.IsNotExist(err) {
		return State{}, false, nil, err
	}
	files := []output.File{{Path: filepath.Join(root, StateFile), Data: data}}
	if !hasLine(string(ignore), "/.apidoc/output/") && !hasLine(string(ignore), ".apidoc/output/") {
		if len(ignore) > 0 && ignore[len(ignore)-1] != '\n' {
			ignore = append(ignore, '\n')
		}
		ignore = append(ignore, []byte("/.apidoc/output/\n")...)
		files = append(files, output.File{Path: ignorePath, Data: ignore})
	}
	if err := output.WriteAtomic(files...); err != nil {
		return State{}, false, nil, err
	}
	return state, true, diagnostics, nil
}

// hasLine 检查文本中是否已有指定行，用于避免重复添加 Git 忽略规则
func hasLine(text, line string) bool {
	for _, value := range strings.Split(text, "\n") {
		if strings.TrimSpace(value) == line {
			return true
		}
	}
	return false
}

// checkRoutes 检查是否有多个路由使用相同的 HTTP 方法和规范化路径
func checkRoutes(operations []ir.Operation) error {
	seen := map[string]ir.SourceRef{}
	for _, op := range operations {
		key := op.Key().String()
		if old, ok := seen[key]; ok {
			return fmt.Errorf("duplicate route %s at %s and %s", key, old, op.Source)
		}
		seen[key] = op.Source
	}
	return nil
}

// Generate 加载一次源码，确定文档中的接口，再分析结构和说明并生成文档与报告
func Generate(root string) (Artifacts, error) {
	state, err := ReadState(root)
	if err != nil {
		return Artifacts{}, err
	}
	program, err := analyzer.Load(context.Background(), root, []string{"./..."})
	if err != nil {
		return Artifacts{}, err
	}
	source := analyzer.NewSourceAnalyzer(program)
	discovery := source.Discover()
	if err := checkRoutes(discovery.Operations); err != nil {
		return Artifacts{}, err
	}
	// 存量快照不会随生成结果改变，因此新增接口在后续生成时仍会出现在文档中
	baseline := map[string]bool{}
	for _, key := range state.Baseline {
		baseline[key] = true
	}
	for i := range discovery.Operations {
		op := &discovery.Operations[i]
		op.Mode = ir.ModeManaged
		if baseline[op.Key().String()] {
			op.Mode = ir.ModeManual
			op.ModeReason = "existing endpoint at initialization"
		}
		if op.Selection == "include" {
			op.Mode = ir.ModeManaged
			op.ModeReason = "source annotation"
		}
		if op.Selection == "ignore" {
			op.Mode = ir.ModeIgnored
			op.ModeReason = "source annotation"
		}
	}
	operations, err := source.Analyze(discovery.Operations)
	if err != nil {
		return Artifacts{}, err
	}
	builder := schema.NewBuilder(program)
	document := Report{Operations: operations, Issues: []Issue{}}
	// 多个接口可能引用同一模型，因此按问题类型、源码位置和消息合并重复的问题记录
	groups := map[string]*Issue{}
	addIssue := func(d ir.Diagnostic, operation string) {
		key := d.Code + "\x00" + d.Source.String() + "\x00" + d.Message
		issue := groups[key]
		if issue == nil {
			issue = &Issue{Code: d.Code, Severity: d.Severity, Message: d.Message, Source: d.Source}
			groups[key] = issue
		}
		if operation != "" {
			for _, key := range issue.Operations {
				if key == operation {
					return
				}
			}
			issue.Operations = append(issue.Operations, operation)
		}
	}
	for _, d := range discovery.Diagnostics {
		addIssue(d, "")
	}
	for i := range operations {
		op := &operations[i]
		document.Summary.Discovered++
		if op.Mode != ir.ModeManaged {
			document.Summary.Excluded++
			continue
		}
		document.Summary.Generated++
		for _, param := range op.Parameters {
			result, err := builder.BuildParameter(param)
			if err != nil {
				return Artifacts{}, err
			}
			op.Diagnostics = append(op.Diagnostics, result.Diagnostics...)
		}
		if op.RequestBody != nil {
			result, err := builder.BuildRequest(op.RequestBody.Type, op.RequestBody.Source)
			if err != nil {
				return Artifacts{}, err
			}
			op.RequestBody.Confidence = result.Confidence
			op.Diagnostics = append(op.Diagnostics, result.Diagnostics...)
		}
		for j := range op.Responses {
			for k := range op.Responses[j].Content {
				content := &op.Responses[j].Content[k]
				if content.Type == nil {
					continue
				}
				result, err := builder.Build(*content.Type, op.Responses[j].Source)
				if err != nil {
					return Artifacts{}, err
				}
				content.Confidence = result.Confidence
				op.Diagnostics = append(op.Diagnostics, result.Diagnostics...)
			}
		}
		// 删除接口内的重复问题记录，使 HTML、报告和 OpenAPI 扩展使用相同的分析提示
		seen := map[string]bool{}
		unique := []ir.Diagnostic{}
		for _, d := range op.Diagnostics {
			key := d.Code + d.Source.String() + d.Message
			if !seen[key] {
				unique = append(unique, d)
				seen[key] = true
			}
			addIssue(d, op.Key().String())
		}
		op.Diagnostics = unique
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		issue := groups[key]
		sort.Strings(issue.Operations)
		document.Issues = append(document.Issues, *issue)
		if issue.Severity == ir.SeverityWarning {
			document.Summary.Warnings++
		}
	}
	api, err := apiopenapi.Assemble(apiopenapi.Info{Title: state.Title, Version: state.APIVersion}, operations, builder)
	if err != nil {
		return Artifacts{}, err
	}
	reportJSON, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return Artifacts{}, err
	}
	reportJSON = append(reportJSON, '\n')
	html, err := preview.Render(api.JSON, reportJSON)
	if err != nil {
		return Artifacts{}, err
	}
	return Artifacts{OpenAPI: api.JSON, HTML: html, ReportJSON: reportJSON, Report: document}, nil
}

// Write 写入分析和校验成功后生成的三个文件
func (a Artifacts) Write(root string) error {
	dir := filepath.Join(root, OutputDir)
	return output.WriteAtomic(output.File{Path: filepath.Join(dir, "openapi.json"), Data: a.OpenAPI}, output.File{Path: filepath.Join(dir, "index.html"), Data: a.HTML}, output.File{Path: filepath.Join(dir, "report.json"), Data: a.ReportJSON})
}
