package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"text/template"

	"github.com/topiaco/doc-gen/internal/output"
)

// Workflow 创建使用固定生成器版本和业务项目 Go 版本的工作流，PR 与默认分支执行相同命令
func Workflow(root, version string) (output.File, error) {
	if !regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?|[a-fA-F0-9]{40})$`).MatchString(version) {
		return output.File{}, fmt.Errorf("GitHub setup needs a released generator version or full commit SHA; for a development build use --generator-version REF after publishing the implementation")
	}
	gitRoot := root
	for dir := root; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			gitRoot = dir
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	relative, err := filepath.Rel(gitRoot, root)
	if err != nil {
		return output.File{}, err
	}
	relative = filepath.ToSlash(relative)
	t, err := template.New("workflow").Delims("[[", "]]").Parse(githubWorkflow)
	if err != nil {
		return output.File{}, err
	}
	var buffer bytes.Buffer
	data := struct{ Project, Version, GoMod, GoSum, Artifacts string }{strconv.Quote(relative), strconv.Quote(version), strconv.Quote(filepath.ToSlash(filepath.Join(relative, "go.mod"))), strconv.Quote(filepath.ToSlash(filepath.Join(relative, "go.sum"))), strconv.Quote(filepath.ToSlash(filepath.Join(relative, OutputDir)))}
	if err := t.Execute(&buffer, data); err != nil {
		return output.File{}, err
	}
	path := filepath.Join(gitRoot, ".github", "workflows", "api-doc.yml")
	if existing, err := os.ReadFile(path); err == nil && !bytes.Equal(existing, buffer.Bytes()) {
		return output.File{}, fmt.Errorf("%s already exists with different content; keep the existing workflow until ready to replace it", path)
	} else if err != nil && !os.IsNotExist(err) {
		return output.File{}, err
	}
	return output.File{Path: path, Data: buffer.Bytes()}, nil
}

const githubWorkflow = `name: API Documentation

on:
  pull_request:
  push:
  workflow_dispatch:

permissions:
  contents: read

env:
  PROJECT_DIR: [[.Project]]
  APIDOC_VERSION: [[.Version]]

jobs:
  generate:
    if: github.event_name != 'push' || github.ref_name == github.event.repository.default_branch
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          persist-credentials: false
      - uses: actions/setup-go@v7
        with:
          go-version-file: [[.GoMod]]
          cache-dependency-path: [[.GoSum]]
      - name: Install documentation generator
        run: go install "github.com/topiaco/doc-gen/cmd/apidoc-gen@$APIDOC_VERSION"
      - name: Download project dependencies
        working-directory: ${{ env.PROJECT_DIR }}
        run: go mod download
      - name: Generate documentation
        working-directory: ${{ env.PROJECT_DIR }}
        run: apidoc-gen
      - name: Upload documentation
        uses: actions/upload-artifact@v7
        with:
          name: api-documentation-${{ github.sha }}
          path: [[.Artifacts]]
          include-hidden-files: true
          if-no-files-found: error
`
