package schema

import (
	"sort"
	"strings"

	"github.com/swaggest/openapi-go/openapi3"
)

// PrioritizeNonNullVariants 将只能生成 null 的值较少的分支排在前面，供文档工具选择默认示例。
// 仅调整展示顺序，保留所有分支和 nullable 约束；权重相同时保持原有的稳定顺序。
func (b *Builder) PrioritizeNonNullVariants(variants []openapi3.SchemaOrRef) {
	sort.SliceStable(variants, func(i, j int) bool {
		return b.nullExampleWeight(variants[i], map[string]bool{}) < b.nullExampleWeight(variants[j], map[string]bool{})
	})
}

// nullExampleWeight 统计示例中的 null-only 值；nullable 的具体类型仍可展示非空示例。
func (b *Builder) nullExampleWeight(value openapi3.SchemaOrRef, seen map[string]bool) int {
	if value.SchemaReference != nil {
		ref := value.SchemaReference.Ref
		if seen[ref] {
			return 0
		}
		seen[ref] = true
		defer delete(seen, ref)
		return b.nullExampleWeight(b.components[strings.TrimPrefix(ref, "#/components/schemas/")], seen)
	}
	s := value.Schema
	if s == nil {
		return 0
	}
	if len(s.Enum) == 1 && s.Enum[0] == nil {
		return 1
	}
	weight := 0
	for _, field := range s.Properties {
		weight += b.nullExampleWeight(field, seen)
	}
	for _, part := range s.AllOf {
		weight += b.nullExampleWeight(part, seen)
	}
	for _, variants := range [][]openapi3.SchemaOrRef{s.AnyOf, s.OneOf} {
		if len(variants) == 0 {
			continue
		}
		best := b.nullExampleWeight(variants[0], seen)
		for _, variant := range variants[1:] {
			best = min(best, b.nullExampleWeight(variant, seen))
		}
		weight += best
	}
	return weight
}
