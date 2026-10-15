package analyzer

import (
	"go/types"
	"reflect"
	"sort"
	"strings"

	"github.com/topiaco/doc-gen/internal/ir"
)

// structFields 按 encoding/json 的嵌入提升、显式字段名和同层冲突规则选择输出字段
func structFields(program *Program, structure *types.Struct) []ir.Field {
	type candidate struct {
		field  ir.Field
		depth  int
		tagged bool
		order  int
	}
	candidates := map[string][]candidate{}
	active := map[*types.Struct]bool{}
	order := 0
	var walk func(*types.Struct, int)
	walk = func(current *types.Struct, depth int) {
		if active[current] {
			return
		}
		active[current] = true
		defer delete(active, current)
		for i := 0; i < current.NumFields(); i++ {
			field := current.Field(i)
			raw := current.Tag(i)
			tag := reflect.StructTag(raw).Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if field.Anonymous() && name == "" {
				if nested, ok := underlying(field.Type()).(*types.Struct); ok {
					walk(nested, depth+1)
					continue
				}
			}
			if !field.Exported() {
				continue
			}
			typ := TypeRefFromGoType(program, field.Type(), false)
			doc := program.Documentation(field.Pos())
			doc.Text = ""
			typ = applyTypeDocumentation(typ, doc)
			jsonName := jsonFieldName(field.Name(), tag)
			item := ir.Field{Name: field.Name(), JSONName: jsonName, Type: typ, Tag: raw, Description: fieldDescription(program, field, raw), Required: bindingRequired(reflect.StructTag(raw).Get("binding")), Source: program.SourceRef(field.Pos()), Confidence: ir.ConfidenceExact}
			candidates[jsonName] = append(candidates[jsonName], candidate{item, depth, name != "", order})
			order++
		}
	}
	walk(structure, 0)
	// 先选择嵌套层数最少的字段，再优先选择有显式 JSON 名的字段；如果仍有歧义，则按编码器规则省略
	var selected []candidate
	for _, options := range candidates {
		sort.SliceStable(options, func(i, j int) bool {
			if options[i].depth != options[j].depth {
				return options[i].depth < options[j].depth
			}
			return options[i].tagged && !options[j].tagged
		})
		if len(options) > 1 && options[0].depth == options[1].depth && options[0].tagged == options[1].tagged {
			continue
		}
		selected = append(selected, options[0])
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].order < selected[j].order })
	fields := make([]ir.Field, 0, len(selected))
	for _, option := range selected {
		fields = append(fields, option.field)
	}
	return fields
}
