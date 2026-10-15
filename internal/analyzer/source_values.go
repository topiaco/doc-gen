package analyzer

import (
	"encoding/json"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/topiaco/doc-gen/internal/ir"
	"golang.org/x/tools/go/packages"
)

type sourceFunction struct {
	pkg       *packages.Package
	object    *types.Func
	signature *types.Signature
	body      *ast.BlockStmt
	pos       token.Pos
	name      string
}

// sourceValue 同时保存声明类型和已推断的值，因此 any、泛型和固定键对象可以依据实际赋值确定结构
type sourceValue struct {
	typ      types.Type
	ref      ir.TypeRef
	source   ir.SourceRef
	constant constant.Value
	fields   map[string]*sourceValue
	items    []*sourceValue
	router   *groupPrefix
	function *sourceFunction
	receiver *sourceValue
	pointee  *sourceValue
	captures sourceEnv
	typeArgs map[*types.TypeParam]types.Type
	origins  []*sourceEvent
	dynamic  bool
}

// sourceEnv 以 go/types 对象区分同名变量，闭包与函数调用都沿用这一绑定关系
type sourceEnv map[types.Object]*sourceValue

// sourceEvent 记录一次路由或 Context 调用，后续统一转换为接口模型
type sourceEvent struct {
	pkg           *packages.Package
	call          *ast.CallExpr
	name          string
	receiver      *sourceValue
	args          []*sourceValue
	parameterType *ir.TypeRef
	required      bool
}

type sourceFrame struct {
	engine   *sourceEngine
	fn       *sourceFunction
	env      sourceEnv
	returns  [][]*sourceValue
	typeArgs map[*types.TypeParam]types.Type
}

// sourceSummary 缓存没有调用副作用的函数返回值，供该函数在输入相同时复用
type sourceSummary struct {
	values []*sourceValue
}

// sourceEngine 通过静态分析推断局部值和函数调用结果，不执行业务代码
type sourceEngine struct {
	program      *Program
	functions    map[*types.Func]*sourceFunction
	globals      map[types.Object]sourceExpression
	summaries    map[string]sourceSummary
	active       map[*sourceFunction]bool
	globalActive map[types.Object]bool
	events       []*sourceEvent
	diagnostics  []ir.Diagnostic
	mode         string
}

type sourceExpression struct {
	pkg        *packages.Package
	expression ast.Expr
}

// newSourceEngine 索引函数声明与全局初始化表达式，泛型实例关联到原始声明
func newSourceEngine(program *Program, mode string) *sourceEngine {
	e := &sourceEngine{program: program, functions: map[*types.Func]*sourceFunction{}, globals: map[types.Object]sourceExpression{}, summaries: map[string]sourceSummary{}, active: map[*sourceFunction]bool{}, globalActive: map[types.Object]bool{}, mode: mode}
	for _, pkg := range program.Packages {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					obj, ok := pkg.TypesInfo.Defs[decl.Name].(*types.Func)
					if ok {
						e.functions[obj.Origin()] = &sourceFunction{pkg: pkg, object: obj, signature: obj.Type().(*types.Signature), body: decl.Body, pos: decl.Name.Pos(), name: decl.Name.Name}
					}
				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						if spec, ok := spec.(*ast.ValueSpec); ok && len(spec.Names) == len(spec.Values) {
							for i, name := range spec.Names {
								e.globals[pkg.TypesInfo.Defs[name]] = sourceExpression{pkg, spec.Values[i]}
							}
						}
					}
				}
			}
		}
	}
	return e
}

// value 根据声明类型创建分析值，并识别 Gin 路由对象
func (e *sourceEngine) value(t types.Type) *sourceValue {
	v := &sourceValue{typ: t, ref: TypeRefFromGoType(e.program, t, false)}
	if isGinType(t, "Engine") {
		v.router = &groupPrefix{exact: true}
	}
	if isGinType(t, "RouterGroup") || isGinType(t, "IRouter") || isGinType(t, "IRoutes") {
		v.router = &groupPrefix{}
	}
	return v
}

// expression 根据赋值、字段访问和函数调用推断表达式的值，无法确定具体值时保留声明类型
func (f *sourceFrame) expression(expr ast.Expr) *sourceValue {
	if expr == nil {
		return f.value(nil)
	}
	info := f.fn.pkg.TypesInfo
	value := f.value(info.TypeOf(expr))
	value.source = f.engine.program.SourceRef(expr.Pos())
	if tv := info.Types[expr]; tv.Value != nil {
		value.constant = tv.Value
		return value
	}
	switch expr := expr.(type) {
	case *ast.ParenExpr:
		return f.expression(expr.X)
	case *ast.Ident:
		if expr.Name == "nil" {
			value.ref = ir.TypeRef{Kind: ir.TypeNull}
			return value
		}
		object := info.ObjectOf(expr)
		if v, ok := f.env[object]; ok {
			return v
		}
		if object, ok := object.(*types.Func); ok {
			value.function = f.engine.functions[object.Origin()]
			f.functionTypeArgs(expr, value)
			return value
		}
		if init, ok := f.engine.globals[object]; ok && !f.engine.globalActive[object] {
			f.engine.globalActive[object] = true
			frame := &sourceFrame{engine: f.engine, fn: &sourceFunction{pkg: init.pkg}, env: sourceEnv{}}
			result := frame.expression(init.expression)
			delete(f.engine.globalActive, object)
			f.env[object] = result
			return result
		}
	case *ast.FuncLit:
		value.function = &sourceFunction{pkg: f.fn.pkg, signature: info.TypeOf(expr).(*types.Signature), body: expr.Body, pos: expr.Pos()}
		value.captures = sourceEnv{}
		for object, captured := range f.env {
			value.captures[object] = captured
		}
		value.typeArgs = f.typeArgs
	case *ast.UnaryExpr:
		inner := f.expression(expr.X)
		if expr.Op == token.AND {
			value.pointee = inner
			return value
		}
		if inner.constant != nil && (expr.Op == token.ADD || expr.Op == token.SUB || expr.Op == token.NOT || expr.Op == token.XOR) {
			value.constant = constant.UnaryOp(expr.Op, inner.constant, 0)
		}
	case *ast.StarExpr:
		return indirectValue(f.expression(expr.X))
	case *ast.SelectorExpr:
		if sel := info.Selections[expr]; sel != nil {
			receiver := f.expression(expr.X)
			if obj, ok := sel.Obj().(*types.Func); ok {
				value.function = f.engine.functions[obj.Origin()]
				if value.function != nil {
					_, wantsPointer := value.function.signature.Recv().Type().(*types.Pointer)
					_, hasPointer := types.Unalias(receiver.typ).(*types.Pointer)
					if wantsPointer && !hasPointer {
						pointer := f.engine.value(types.NewPointer(receiver.typ))
						pointer.pointee = receiver
						receiver = pointer
					} else if !wantsPointer && hasPointer {
						receiver = indirectValue(receiver)
					}
				}
				value.receiver = receiver
				f.functionTypeArgs(expr, value)
				return value
			}
			if field := f.selectedField(receiver, sel.Index()); field != nil {
				return field
			}
			return value
		}
		if object, ok := info.ObjectOf(expr.Sel).(*types.Func); ok {
			value.function = f.engine.functions[object.Origin()]
			f.functionTypeArgs(expr, value)
		}
		if init, ok := f.engine.globals[info.ObjectOf(expr.Sel)]; ok {
			frame := &sourceFrame{engine: f.engine, fn: &sourceFunction{pkg: init.pkg}, env: f.env}
			return frame.expression(init.expression)
		}
	case *ast.IndexExpr:
		// 泛型实例使用原始函数声明分析函数体，再根据调用处的类型实参确定返回类型
		if _, ok := info.TypeOf(expr).(*types.Signature); ok {
			return f.expression(expr.X)
		}
		target := f.expression(expr.X)
		index := f.expression(expr.Index)
		if key, ok := stringValue(index); ok && target.fields[key] != nil {
			return target.fields[key]
		}
		if index.constant != nil {
			if n, ok := constant.Int64Val(index.constant); ok && n >= 0 && n < int64(len(target.items)) {
				return target.items[n]
			}
		}
	case *ast.IndexListExpr:
		return f.expression(expr.X)
	case *ast.CompositeLit:
		return f.composite(expr)
	case *ast.CallExpr:
		values := f.call(expr)
		if len(values) > 0 {
			return values[0]
		}
	case *ast.BinaryExpr:
		left, right := f.expression(expr.X), f.expression(expr.Y)
		if left.constant != nil && right.constant != nil {
			switch expr.Op {
			case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
				value.constant = constant.MakeBool(constant.Compare(left.constant, expr.Op, right.constant))
			case token.ADD:
				value.constant = constant.BinaryOp(left.constant, expr.Op, right.constant)
			}
		}
	}
	return value
}

// composite 读取结构体、map 和集合字面量中的具体成员
func (f *sourceFrame) composite(expr *ast.CompositeLit) *sourceValue {
	v := f.value(f.fn.pkg.TypesInfo.TypeOf(expr))
	v.source = f.engine.program.SourceRef(expr.Pos())
	t := underlying(v.typ)
	switch t := t.(type) {
	case *types.Struct:
		v.fields = map[string]*sourceValue{}
		for i, element := range expr.Elts {
			name := ""
			expression := element
			if kv, ok := element.(*ast.KeyValueExpr); ok {
				if id, ok := kv.Key.(*ast.Ident); ok {
					name = id.Name
				}
				expression = kv.Value
			} else if i < t.NumFields() {
				name = t.Field(i).Name()
			}
			v.fields[name] = f.expression(expression)
		}
	case *types.Map:
		v.fields = map[string]*sourceValue{}
		for _, element := range expr.Elts {
			kv, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, known := stringValue(f.expression(kv.Key))
			field := f.expression(kv.Value)
			if known {
				v.fields[key] = field
			} else {
				v.dynamic = true
			}
		}
	case *types.Array, *types.Slice:
		for _, element := range expr.Elts {
			if kv, ok := element.(*ast.KeyValueExpr); ok {
				element = kv.Value
			}
			v.items = append(v.items, f.expression(element))
		}
	}
	return v
}

// underlying 解析类型别名和指针，返回用于结构分析的底层类型
func underlying(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		return underlying(p.Elem())
	}
	return t.Underlying()
}

// stringValue 返回能够静态确定的字符串常量
func stringValue(v *sourceValue) (string, bool) {
	if v != nil && v.constant != nil && v.constant.Kind() == constant.String {
		return constant.StringVal(v.constant), true
	}
	return "", false
}

// intValue 返回能够静态确定的整数常量
func intValue(v *sourceValue) (int, bool) {
	if v != nil && v.constant != nil && v.constant.Kind() == constant.Int {
		n, ok := constant.Int64Val(v.constant)
		return int(n), ok
	}
	return 0, false
}

// copyEnv 隔离分支环境，同时保留同一环境内的别名关系
func copyEnv(env sourceEnv) sourceEnv {
	cloned := sourceEnv{}
	seen := map[*sourceValue]*sourceValue{}
	for object, value := range env {
		cloned[object] = cloneValue(value, seen)
	}
	return cloned
}

// cloneValue 记录已复制的对象，因此循环引用和共享引用可以使用同一份副本
func cloneValue(v *sourceValue, seen map[*sourceValue]*sourceValue) *sourceValue {
	if v == nil {
		return nil
	}
	if result := seen[v]; result != nil {
		return result
	}
	result := *v
	seen[v] = &result
	if v.fields != nil {
		result.fields = map[string]*sourceValue{}
		for k, field := range v.fields {
			result.fields[k] = cloneValue(field, seen)
		}
	}
	result.items = nil
	for _, item := range v.items {
		result.items = append(result.items, cloneValue(item, seen))
	}
	result.pointee = cloneValue(v.pointee, seen)
	result.receiver = cloneValue(v.receiver, seen)
	if v.captures != nil {
		result.captures = sourceEnv{}
		for object, captured := range v.captures {
			result.captures[object] = cloneValue(captured, seen)
		}
	}
	return &result
}

// indirectValue 返回已知指针引用的实际值
func indirectValue(value *sourceValue) *sourceValue {
	for value.pointee != nil {
		value = value.pointee
	}
	return value
}

// assignedValue 遵循 Go 赋值语义复制结构体和数组，但是指针、切片和 map 仍引用原有数据
func assignedValue(value *sourceValue) *sourceValue {
	result := *value
	if _, pointer := types.Unalias(value.typ).(*types.Pointer); pointer {
		return &result
	}
	switch underlying(value.typ).(type) {
	case *types.Struct:
		if value.fields != nil {
			result.fields = map[string]*sourceValue{}
			for name, field := range value.fields {
				result.fields[name] = assignedValue(field)
			}
		}
	case *types.Array:
		result.items = make([]*sourceValue, len(value.items))
		for i, item := range value.items {
			result.items[i] = assignedValue(item)
		}
	}
	return &result
}

// concreteType 根据已推断的字段值和集合元素确定具体类型，同时保留模型说明和字段信息
func (e *sourceEngine) concreteType(v *sourceValue) ir.TypeRef {
	return e.concrete(v, map[*sourceValue]bool{})
}

// concrete 根据已知成员确定响应结构，并通过访问记录处理循环引用
func (e *sourceEngine) concrete(v *sourceValue, active map[*sourceValue]bool) ir.TypeRef {
	if v == nil {
		return ir.TypeRef{Kind: ir.TypeUnknown}
	}
	if active[v] {
		return v.ref
	}
	active[v] = true
	defer delete(active, v)
	ref := v.ref
	if ref.SchemaType != "" || ref.CustomEncoding {
		return ref
	}
	if v.pointee != nil {
		element := e.concrete(v.pointee, active)
		ref.Element = &element
		return ref
	}
	switch t := underlying(v.typ).(type) {
	case *types.Struct:
		if v.fields == nil {
			return ref
		}
		fields := structFields(e.program, t)
		refined := false
		for i := range fields {
			if value := e.knownField(v, fields[i], map[*sourceValue]bool{}); value != nil {
				actual := e.concrete(value, active)
				actual = refineType(fields[i].Type, actual)
				if !reflect.DeepEqual(fields[i].Type, actual) {
					fields[i].Type = actual
					refined = true
				}
			}
		}
		if refined {
			return refineType(ref, ir.TypeRef{Kind: ir.TypeStruct, Fields: fields, Description: ref.Description})
		}
	case *types.Map:
		if v.fields == nil {
			return ref
		}
		shaped := ir.TypeRef{Kind: ir.TypeStruct, Description: ref.Description}
		keys := make([]string, 0, len(v.fields))
		for k := range v.fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			shaped.Fields = append(shaped.Fields, ir.Field{Name: k, JSONName: k, Type: e.concrete(v.fields[k], active), Source: v.fields[k].source, Confidence: ir.ConfidenceExact})
		}
		if v.dynamic {
			shaped.Unresolved = "map also contains keys that cannot be determined"
		}
		return shaped
	case *types.Slice, *types.Array:
		if len(v.items) > 0 {
			var element *sourceValue
			for _, item := range v.items {
				element = e.mergeValue(element, item)
			}
			actual := e.concrete(element, active)
			if ref.Element != nil {
				actual = refineType(*ref.Element, actual)
				ref.Element = &actual
			}
		}
	}
	return ref
}

// containsDynamic 判断类型引用中是否仍有动态字段或集合元素
func containsDynamic(t ir.TypeRef) bool {
	if t.SchemaType != "" {
		return false
	}
	switch t.Kind {
	case ir.TypeUnknown, ir.TypeInterface, ir.TypeParameter:
		return true
	case ir.TypeMap:
		return true
	}
	for _, arg := range t.TypeArguments {
		if containsDynamic(arg) {
			return true
		}
	}
	if t.Element != nil && containsDynamic(*t.Element) {
		return true
	}
	for _, field := range t.Fields {
		if containsDynamic(field.Type) {
			return true
		}
	}
	return false
}

// containsTypeParameter 判断类型及其泛型实参中是否仍有未确定的类型参数
func containsTypeParameter(t ir.TypeRef) bool {
	if t.Kind == ir.TypeParameter {
		return true
	}
	for _, argument := range t.TypeArguments {
		if containsTypeParameter(argument) {
			return true
		}
	}
	return t.Element != nil && containsTypeParameter(*t.Element)
}

// mergeValue 合并各分支的可能值；如果无法确定唯一类型，则保留所有可能的类型
func (e *sourceEngine) mergeValue(a, b *sourceValue) *sourceValue {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if valueKey(a) == valueKey(b) {
		return a
	}
	result := &sourceValue{ref: mergeTypes(e.concreteType(a), e.concreteType(b)), origins: append(append([]*sourceEvent{}, a.origins...), b.origins...)}
	if a.router != nil || b.router != nil {
		result.router = &groupPrefix{}
	}
	return result
}

// mergeTypes 合并并去除重复类型，存在多个可能类型时生成联合类型
func mergeTypes(a, b ir.TypeRef) ir.TypeRef {
	values := []ir.TypeRef{}
	if a.Kind == ir.TypeUnion {
		values = append(values, a.Variants...)
	} else {
		values = append(values, a)
	}
	if b.Kind == ir.TypeUnion {
		values = append(values, b.Variants...)
	} else {
		values = append(values, b)
	}
	unique := map[string]ir.TypeRef{}
	for _, v := range values {
		data, _ := json.Marshal(v)
		unique[string(data)] = v
	}
	keys := make([]string, 0, len(unique))
	for k := range unique {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 1 {
		return unique[keys[0]]
	}
	result := ir.TypeRef{Kind: ir.TypeUnion}
	for _, k := range keys {
		result.Variants = append(result.Variants, unique[k])
	}
	return result
}

// valueKey 根据类型、常量、路由路径和字段值生成缓存键，因此不同路由路径不会误用同一份分析结果
func valueKey(v *sourceValue) string {
	var key strings.Builder
	active := map[*sourceValue]bool{}
	var visit func(*sourceValue)
	visit = func(value *sourceValue) {
		if value == nil {
			key.WriteString("nil;")
			return
		}
		data, _ := json.Marshal(value.ref)
		key.Write(data)
		if active[value] {
			key.WriteString("cycle;")
			return
		}
		active[value] = true
		defer delete(active, value)
		if value.constant != nil {
			key.WriteString(value.constant.ExactString())
		}
		if value.router != nil {
			key.WriteString("router:" + strconv.FormatBool(value.router.exact) + ":" + value.router.path)
		}
		if value.function != nil {
			key.WriteString("func:" + strconv.Itoa(int(value.function.pos)))
			key.WriteString(typeArgumentsKey(value.typeArgs))
		}
		if value.pointee != nil {
			key.WriteString("pointer:")
			visit(value.pointee)
		}
		keys := make([]string, 0, len(value.fields))
		for k := range value.fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			key.WriteString(strconv.Quote(k))
			visit(value.fields[k])
		}
		for _, item := range value.items {
			visit(item)
		}
		objects := make([]types.Object, 0, len(value.captures))
		for object := range value.captures {
			objects = append(objects, object)
		}
		sort.Slice(objects, func(i, j int) bool { return objects[i].Pos() < objects[j].Pos() })
		for _, object := range objects {
			key.WriteString(strconv.Itoa(int(object.Pos())))
			visit(value.captures[object])
		}
	}
	visit(v)
	return key.String()
}

// fieldTagName 返回绑定标签指定的参数名，未指定时使用 Go 字段名
func fieldTagName(field ir.Field, tag string) string {
	value := strings.Split(reflect.StructTag(field.Tag).Get(tag), ",")[0]
	if value == "" {
		return field.Name
	}
	return value
}
