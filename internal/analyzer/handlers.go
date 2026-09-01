package analyzer

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/topiaco/doc-gen/internal/ir"
)

// HandlerResolution 保存单条路由的 Handler 声明或诊断
type HandlerResolution struct {
	Operation   ir.Operation
	Package     *packages.Package
	Declaration *ast.FuncDecl
	Function    *types.Func
	Signature   *types.Signature
	Comment     string
	Diagnostic  *ir.Diagnostic
}

// handlerDeclaration 保存函数声明索引项
type handlerDeclaration struct {
	pkg      *packages.Package
	decl     *ast.FuncDecl
	function *types.Func
}

// ResolveHandlers 按包路径和函数名定位每条路由的 Handler
func ResolveHandlers(program *Program, operations []ir.Operation) []HandlerResolution {
	index := indexFunctions(program)
	results := make([]HandlerResolution, 0, len(operations))
	for _, operation := range operations {
		result := HandlerResolution{Operation: operation}
		if operation.Handler.PackagePath == "" || operation.Handler.Name == "" {
			result.Diagnostic = handlerDiagnostic(operation, "handler.unsupported_expression", "route handler is a closure or dynamic expression")
			results = append(results, result)
			continue
		}

		key := operation.Handler.PackagePath + "." + operation.Handler.Name
		indexed, found := index[key]
		if !found {
			result.Diagnostic = handlerDiagnostic(operation, "handler.declaration_not_found", fmt.Sprintf("cannot locate handler declaration %s", key))
			results = append(results, result)
			continue
		}

		result.Operation.Handler.Source = program.SourceRef(indexed.decl.Pos())
		result.Package = indexed.pkg
		result.Declaration = indexed.decl
		result.Function = indexed.function
		result.Signature = indexed.function.Type().(*types.Signature)
		if indexed.decl.Doc != nil {
			result.Comment = strings.TrimSpace(indexed.decl.Doc.Text())
		}
		results = append(results, result)
	}
	return results
}

// indexFunctions 建立已加载包的包级函数索引
func indexFunctions(program *Program) map[string]handlerDeclaration {
	index := map[string]handlerDeclaration{}
	for _, pkg := range program.Packages {
		for _, file := range pkg.Syntax {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil {
					continue
				}
				object := pkg.TypesInfo.Defs[function.Name].(*types.Func)
				key := pkg.PkgPath + "." + function.Name.Name
				index[key] = handlerDeclaration{pkg: pkg, decl: function, function: object}
			}
		}
	}
	return index
}

// handlerDiagnostic 创建 Handler 定位诊断
func handlerDiagnostic(operation ir.Operation, code, message string) *ir.Diagnostic {
	return &ir.Diagnostic{
		Code:     code,
		Severity: ir.SeverityError,
		Message:  message,
		Source:   operation.Handler.Source,
	}
}
