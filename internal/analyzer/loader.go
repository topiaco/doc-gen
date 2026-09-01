// Package analyzer loads and inspects Go source without executing project code
package analyzer

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/topiaco/doc-gen/internal/ir"
)

// Program 保存一次静态加载得到的源码和类型信息
type Program struct {
	ProjectRoot   string
	FileSet       *token.FileSet
	Packages      []*packages.Package
	byPath        map[string]*packages.Package
	fieldComments map[token.Pos]string // 字段行尾注释
}

// Load 加载指定包的 AST、类型信息和源码位置
func Load(ctx context.Context, projectRoot string, patterns []string) (*Program, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}

	fileSet := token.NewFileSet()
	loaded, err := packages.Load(&packages.Config{
		Context: ctx,
		Dir:     root,
		Fset:    fileSet,
		Mode:    packages.LoadSyntax | packages.NeedModule,
		Tests:   false,
	}, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load Go packages: %w", err)
	}
	if err := packageErrors(loaded); err != nil {
		return nil, err
	}

	sort.Slice(loaded, func(i, j int) bool {
		return loaded[i].PkgPath < loaded[j].PkgPath
	})
	byPath := make(map[string]*packages.Package, len(loaded))
	for _, pkg := range loaded {
		indexPackage(byPath, pkg)
	}

	return &Program{
		ProjectRoot:   root,
		FileSet:       fileSet,
		Packages:      loaded,
		byPath:        byPath,
		fieldComments: collectFieldComments(loaded),
	}, nil
}

// Package 返回指定导入路径的已加载包
func (p *Program) Package(path string) *packages.Package {
	return p.byPath[path]
}

// FieldComment 返回字段的行尾注释
func (p *Program) FieldComment(pos token.Pos) string {
	return p.fieldComments[pos]
}

// SourceRef 将 token 位置转换为项目相对源码位置
func (p *Program) SourceRef(pos token.Pos) ir.SourceRef {
	position := p.FileSet.PositionFor(pos, true)
	if !position.IsValid() {
		return ir.SourceRef{}
	}

	filename := position.Filename
	if relative, err := filepath.Rel(p.ProjectRoot, filename); err == nil && filepath.IsLocal(relative) {
		filename = relative
	}
	return ir.SourceRef{
		File:   filepath.ToSlash(filename),
		Line:   position.Line,
		Column: position.Column,
	}
}

// packageErrors 汇总包元数据、语法和类型错误
func packageErrors(loaded []*packages.Package) error {
	var messages []string
	for _, pkg := range loaded {
		for _, problem := range pkg.Errors {
			messages = append(messages, problem.Error())
		}
	}
	if len(messages) == 0 {
		return nil
	}
	sort.Strings(messages)
	return fmt.Errorf("load Go packages:\n%s", strings.Join(messages, "\n"))
}

// indexPackage 索引加载包及其依赖的类型信息
func indexPackage(index map[string]*packages.Package, pkg *packages.Package) {
	if pkg == nil {
		return
	}
	if _, found := index[pkg.PkgPath]; found {
		return
	}
	index[pkg.PkgPath] = pkg
	for _, imported := range pkg.Imports {
		indexPackage(index, imported)
	}
}

// collectFieldComments 收集 Struct 字段的行尾注释
func collectFieldComments(loaded []*packages.Package) map[token.Pos]string {
	comments := make(map[token.Pos]string)
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				field, ok := node.(*ast.Field)
				if !ok || field.Comment == nil {
					return true
				}
				comment := strings.TrimSpace(field.Comment.Text())
				if len(field.Names) == 0 {
					comments[field.Pos()] = comment
				}
				for _, name := range field.Names {
					comments[name.Pos()] = comment
				}
				return true
			})
		}
	}
	return comments
}
