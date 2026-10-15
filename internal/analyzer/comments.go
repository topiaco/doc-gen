package analyzer

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode"

	"github.com/topiaco/doc-gen/internal/ir"
)

// Directive 保存声明或路由注释中的 apidoc 指令及其源码位置
type Directive struct {
	Name  string
	Value string
	Pos   token.Pos
}

// Documentation 分别保存业务描述和生成指令，因此展示描述时不包含指令文字
type Documentation struct {
	Text       string
	Directives []Directive
}

// indexDocumentation 按声明位置建立注释索引，只将关联到声明的说明和指令作为文档信息
func (p *Program) indexDocumentation() {
	p.documents = map[token.Pos]Documentation{}
	for _, pkg := range p.Packages {
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.FuncDecl:
					p.documents[n.Name.Pos()] = p.parseDocumentation(n.Doc, n.Name.Name, "function")
				case *ast.GenDecl:
					for _, spec := range n.Specs {
						if spec, ok := spec.(*ast.TypeSpec); ok {
							doc := spec.Doc
							if doc == nil && len(n.Specs) == 1 {
								doc = n.Doc
							}
							p.documents[spec.Name.Pos()] = p.parseDocumentation(doc, spec.Name.Name, "type")
						}
					}
				case *ast.Field:
					// 字段前说明优先于行尾说明，但是两处的显式指令都需要保留
					doc := p.parseDocumentation(n.Doc, "", "field")
					tail := p.parseDocumentation(n.Comment, "", "field")
					doc.Directives = append(doc.Directives, tail.Directives...)
					if len(n.Names) == 0 {
						if name := embeddedFieldName(n.Type); name != nil {
							named := fieldDocumentation(doc, tail, name.Name)
							p.documents[n.Pos()] = named
							p.documents[name.Pos()] = named
						}
					}
					for _, name := range n.Names {
						p.documents[name.Pos()] = fieldDocumentation(doc, tail, name.Name)
					}
				}
				return true
			})
		}
	}
}

// fieldDocumentation 去除字段名前缀后选择说明，因此只有符号名的前注释不会覆盖行尾说明
func fieldDocumentation(leading, trailing Documentation, name string) Documentation {
	leading.Text = trimSymbol(leading.Text, name)
	if leading.Text == "" {
		leading.Text = trimSymbol(trailing.Text, name)
	}
	return leading
}

// embeddedFieldName 返回匿名字段的名称，包括指针和泛型嵌入字段
func embeddedFieldName(expr ast.Expr) *ast.Ident {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr
	case *ast.SelectorExpr:
		return expr.Sel
	case *ast.StarExpr:
		return embeddedFieldName(expr.X)
	case *ast.IndexExpr:
		return embeddedFieldName(expr.X)
	case *ast.IndexListExpr:
		return embeddedFieldName(expr.X)
	}
	return nil
}

// trimSymbol 只去掉与声明完全匹配且以空白分隔的符号前缀，避免截断正常业务文字
func trimSymbol(text, name string) string {
	if name != "" && strings.HasPrefix(text, name) {
		rest := strings.TrimPrefix(text, name)
		if rest == "" || len(rest) > 0 && unicode.IsSpace([]rune(rest)[0]) {
			return strings.TrimSpace(rest)
		}
	}
	return strings.TrimSpace(text)
}

// parseDocumentation 提取业务文字并检查指令位置，类型引用随后按声明所在作用域解析
func (p *Program) parseDocumentation(group *ast.CommentGroup, name, scope string) Documentation {
	var doc Documentation
	if group == nil {
		return doc
	}
	var lines []string
	for _, comment := range group.List {
		block := strings.HasPrefix(comment.Text, "/*")
		value := strings.TrimPrefix(comment.Text, "//")
		value = strings.TrimPrefix(value, "/*")
		value = strings.TrimSuffix(value, "*/")
		for _, line := range strings.Split(value, "\n") {
			line = strings.TrimSpace(line)
			if block && (line == "*" || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "*\t")) {
				line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
			}
			if !strings.HasPrefix(line, "apidoc:") {
				lines = append(lines, line)
				continue
			}
			name, argument := splitDirective(strings.TrimPrefix(line, "apidoc:"))
			d := Directive{Name: name, Value: argument, Pos: comment.Pos()}
			valid := false
			switch d.Name {
			case "request", "response":
				valid = scope == "function" && d.Value != ""
			case "type":
				valid = (scope == "type" || scope == "field") && (d.Value == "string" || d.Value == "integer" || d.Value == "number" || d.Value == "boolean" || d.Value == "object")
			case "format":
				valid = (scope == "type" || scope == "field") && d.Value != ""
			case "include", "ignore":
				valid = scope == "route" && d.Value == ""
			}
			if !valid {
				p.AnnotationErrors = append(p.AnnotationErrors, ir.Diagnostic{Code: "annotation.invalid", Severity: ir.SeverityError, Message: fmt.Sprintf("invalid apidoc:%s directive on %s", d.Name, scope), Source: p.SourceRef(d.Pos)})
			}
			doc.Directives = append(doc.Directives, d)
		}
	}
	doc.Text = trimSymbol(strings.Join(lines, "\n"), name)
	return doc
}

// splitDirective 按第一个空白分隔指令与参数，保留类型表达式内部的空白
func splitDirective(value string) (string, string) {
	if index := strings.IndexFunc(value, unicode.IsSpace); index >= 0 {
		return value[:index], strings.TrimSpace(value[index:])
	}
	return value, ""
}

// Documentation 返回指定声明位置的业务说明和指令
func (p *Program) Documentation(pos token.Pos) Documentation { return p.documents[pos] }

// applyTypeDocumentation 将业务说明及显式类型、格式指令应用于类型引用
func applyTypeDocumentation(value ir.TypeRef, doc Documentation) ir.TypeRef {
	if doc.Text != "" {
		value.Description = doc.Text
	}
	for _, d := range doc.Directives {
		switch d.Name {
		case "type":
			value.SchemaType = d.Value
		case "format":
			value.Format = d.Value
		}
	}
	return value
}

// typeDocumentation 读取具名类型或类型别名的声明注释
func (p *Program) typeDocumentation(value types.Type) Documentation {
	switch value := value.(type) {
	case *types.Named:
		return p.Documentation(value.Obj().Pos())
	case *types.Alias:
		return p.Documentation(value.Obj().Pos())
	}
	return Documentation{}
}

// splitSummary 将第一行作为标题，其余文字作为详细说明
func splitSummary(doc string) (string, string) {
	lines := strings.SplitN(strings.TrimSpace(doc), "\n", 2)
	if len(lines) == 1 {
		return lines[0], ""
	}
	return lines[0], strings.TrimSpace(lines[1])
}
