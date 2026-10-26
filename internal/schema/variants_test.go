package schema

import (
	"reflect"
	"testing"

	"github.com/swaggest/openapi-go/openapi3"
)

// TestPrioritizeNonNullVariants 验证引用、nullable 对象和嵌套联合类型优先于 null，并保持同类分支稳定。
func TestPrioritizeNonNullVariants(t *testing.T) {
	null := openapi3.SchemaOrRef{Schema: (&openapi3.Schema{}).WithNullable(true).WithEnum(nil)}
	text := typedSchema(openapi3.SchemaTypeString)
	object := openapi3.SchemaOrRef{Schema: (&openapi3.Schema{}).WithType(openapi3.SchemaTypeObject).WithNullable(true)}
	builder := NewBuilder(nil)
	builder.components["Payload"] = object
	ref := componentReference("Payload")
	pointer := openapi3.SchemaOrRef{Schema: (&openapi3.Schema{AllOf: []openapi3.SchemaOrRef{ref}}).WithNullable(true)}
	union := openapi3.SchemaOrRef{Schema: &openapi3.Schema{AnyOf: []openapi3.SchemaOrRef{null, pointer}}}
	envelope := func(data openapi3.SchemaOrRef) openapi3.SchemaOrRef {
		return openapi3.SchemaOrRef{Schema: &openapi3.Schema{Properties: map[string]openapi3.SchemaOrRef{"data": data}}}
	}
	for _, test := range []struct {
		name        string
		input, want []openapi3.SchemaOrRef
	}{
		{"null and pointer", []openapi3.SchemaOrRef{null, pointer}, []openapi3.SchemaOrRef{pointer, null}},
		{"envelopes", []openapi3.SchemaOrRef{envelope(null), envelope(union)}, []openapi3.SchemaOrRef{envelope(union), envelope(null)}},
		{"stable ties", []openapi3.SchemaOrRef{null, text, object, ref}, []openapi3.SchemaOrRef{text, object, ref, null}},
		{"null only", []openapi3.SchemaOrRef{null}, []openapi3.SchemaOrRef{null}},
	} {
		t.Run(test.name, func(t *testing.T) {
			builder.PrioritizeNonNullVariants(test.input)
			if !reflect.DeepEqual(test.input, test.want) {
				t.Fatalf("unexpected variant ordering: %#v", test.input)
			}
			builder.PrioritizeNonNullVariants(test.input)
			if !reflect.DeepEqual(test.input, test.want) {
				t.Fatal("repeated ordering changed the result")
			}
		})
	}
}

// TestPrioritizeRecursiveVariants 验证引用环可终止，且不会修改组件或丢失任何分支。
func TestPrioritizeRecursiveVariants(t *testing.T) {
	builder := NewBuilder(nil)
	ref := componentReference("Node")
	builder.components["Node"] = openapi3.SchemaOrRef{Schema: &openapi3.Schema{
		Properties: map[string]openapi3.SchemaOrRef{"next": ref},
	}}
	null := openapi3.SchemaOrRef{Schema: (&openapi3.Schema{}).WithNullable(true).WithEnum(nil)}
	variants := []openapi3.SchemaOrRef{null, ref}
	builder.PrioritizeNonNullVariants(variants)
	if !reflect.DeepEqual(variants, []openapi3.SchemaOrRef{ref, null}) {
		t.Fatal("recursive response was not preferred over null")
	}
	if !reflect.DeepEqual(builder.components["Node"].Schema.Properties["next"], ref) {
		t.Fatal("component reference changed")
	}
}
