package analyzer

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/topiaco/doc-gen/internal/ir"
)

// call 先识别 Gin 调用与标准类型转换，其余调用在函数体可用时继续分析源码
func (f *sourceFrame) call(call *ast.CallExpr) []*sourceValue {
	info := f.fn.pkg.TypesInfo
	callee := f.expression(call.Fun)
	args := make([]*sourceValue, len(call.Args))
	for i, arg := range call.Args {
		args[i] = f.expression(arg)
	}
	var selector *ast.SelectorExpr
	if s, ok := call.Fun.(*ast.SelectorExpr); ok {
		selector = s
	}
	name := ""
	if obj := expressionObject(info, call.Fun); obj != nil {
		name = obj.Name()
	}
	if name == "" {
		if id, ok := call.Fun.(*ast.Ident); ok {
			name = id.Name
		}
	}
	result := f.engine.resultValues(f.resolveType(info.TypeOf(call)))
	for _, value := range result {
		value.source = f.engine.program.SourceRef(call.Pos())
	}
	if tv, ok := info.Types[call.Fun]; ok && tv.IsType() {
		if len(args) == 1 {
			if _, ok := underlying(result[0].typ).(*types.Interface); ok {
				return []*sourceValue{assignedValue(args[0])}
			}
			result[0].fields = args[0].fields
			result[0].items = args[0].items
			result[0].constant = args[0].constant
			result[0].origins = args[0].origins
			f.setOriginType(args[0], result[0].ref)
		}
		return result
	}
	if selector != nil {
		receiver := callee.receiver
		if receiver == nil {
			receiver = f.expression(selector.X)
		}
		if receiver.router != nil {
			if selector.Sel.Name == "Group" && len(args) > 0 {
				path, exact := stringValue(args[0])
				result[0].router = &groupPrefix{path: joinRoutePath(receiver.router.path, path), exact: receiver.router.exact && exact}
				return result
			}
			if _, ok := ginRouteMethods[selector.Sel.Name]; ok || selector.Sel.Name == "Handle" || selector.Sel.Name == "Any" {
				if f.engine.mode == "routes" {
					f.engine.events = append(f.engine.events, &sourceEvent{pkg: f.fn.pkg, call: call, name: selector.Sel.Name, receiver: receiver, args: args})
				}
				return result
			}
		}
		if isGinType(info.TypeOf(selector.X), "Context") {
			event := &sourceEvent{pkg: f.fn.pkg, call: call, name: selector.Sel.Name, receiver: receiver, args: args}
			if f.engine.mode == "handler" {
				// 响应结构取决于调用时的参数，因此复制当前值，防止后续赋值改变这次调用的分析结果
				seen := map[*sourceValue]*sourceValue{}
				for i, arg := range args {
					event.args[i] = cloneValue(arg, seen)
				}
				f.engine.events = append(f.engine.events, event)
				if _, ok := ginParameterReaders[event.name]; ok && len(result) > 0 {
					result[0].origins = []*sourceEvent{event}
				}
			}
			return result
		}
	}
	if name == "new" && len(call.Args) == 1 {
		result[0].pointee = f.value(info.TypeOf(call.Args[0]))
		return result
	}
	if name == "make" {
		if _, ok := underlying(result[0].typ).(*types.Map); ok {
			result[0].fields = map[string]*sourceValue{}
		}
		return result
	}
	if name == "append" && len(args) > 0 {
		v := assignedValue(args[0])
		v.items = append([]*sourceValue{}, args[0].items...)
		items := args[1:]
		if call.Ellipsis.IsValid() {
			items = args[1].items
			if items == nil {
				if slice, ok := underlying(args[1].typ).(*types.Slice); ok {
					items = []*sourceValue{f.value(slice.Elem())}
				}
			}
		}
		for _, item := range items {
			v.items = append(v.items, assignedValue(item))
		}
		return []*sourceValue{v}
	}
	if name == "len" && len(args) == 1 && args[0].items != nil {
		result[0].constant = constant.MakeInt64(int64(len(args[0].items)))
		return result
	}
	obj, _ := expressionObject(info, call.Fun).(*types.Func)
	if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "strconv" && len(args) > 0 {
		switch obj.Name() {
		case "Atoi", "ParseInt", "ParseUint", "ParseFloat", "ParseBool":
			if len(result) > 0 {
				f.setOriginType(args[0], result[0].ref)
			}
		}
	}
	if callee.function != nil {
		signature := callee.function.signature
		if signature.Variadic() && !call.Ellipsis.IsValid() {
			last := signature.Params().Len() - 1
			collection := f.engine.value(substituteType(signature.Params().At(last).Type(), callee.typeArgs))
			collection.items = append([]*sourceValue{}, args[last:]...)
			args = append(args[:last:last], collection)
		}
		values := f.engine.invoke(callee, args)
		if len(values) > 0 {
			values = append([]*sourceValue{}, values...)
			for i, value := range values {
				if i < len(result) && containsTypeParameter(value.ref) && !containsTypeParameter(result[i].ref) {
					value = cloneValue(value, map[*sourceValue]*sourceValue{})
					value.typ = result[i].typ
					value.ref = result[i].ref
					values[i] = value
				}
			}
			return values
		}
	}
	return result
}

// setOriginType 根据 strconv 等转换结果，更新原始参数读取调用所记录的类型
func (f *sourceFrame) setOriginType(value *sourceValue, typ ir.TypeRef) {
	for _, event := range value.origins {
		copied := typ
		event.parameterType = &copied
	}
}

// resultValues 将单值或多值返回类型转换为可继续分析的值
func (e *sourceEngine) resultValues(t types.Type) []*sourceValue {
	if tuple, ok := t.(*types.Tuple); ok {
		values := make([]*sourceValue, tuple.Len())
		for i := range values {
			values[i] = e.value(tuple.At(i).Type())
		}
		return values
	}
	if t == nil {
		return nil
	}
	return []*sourceValue{e.value(t)}
}

// invoke 将实参绑定到形参，汇总返回分支并复用可缓存的函数摘要
func (e *sourceEngine) invoke(callee *sourceValue, args []*sourceValue) []*sourceValue {
	fn := callee.function
	if fn == nil || fn.body == nil {
		return nil
	}
	frame := &sourceFrame{engine: e, fn: fn, env: sourceEnv{}, typeArgs: callee.typeArgs}
	defaults := make([]*sourceValue, fn.signature.Results().Len())
	for i := range defaults {
		defaults[i] = frame.value(fn.signature.Results().At(i).Type())
	}
	// 遇到正在分析的递归函数时，使用声明的返回类型继续分析，并记录无法进一步推断的原因
	if e.active[fn] {
		e.diagnostics = append(e.diagnostics, ir.Diagnostic{Code: "analysis.recursive_call", Severity: ir.SeverityWarning, Message: "recursive call uses its declared return type: " + fn.name, Source: e.program.SourceRef(fn.pos)})
		return defaults
	}
	keys := []string{e.mode, strconv.Itoa(int(fn.pos)), valueKey(callee.receiver), typeArgumentsKey(callee.typeArgs)}
	for _, arg := range args {
		keys = append(keys, valueKey(arg))
	}
	key := strings.Join(keys, "\x00")
	// 闭包引用外部变量，间接赋值可能修改实参对象，因此这些函数不按输入值复用缓存
	cacheable := len(callee.captures) == 0 && !e.mutatesArguments(fn, map[*sourceFunction]bool{})
	if cached, ok := e.summaries[key]; ok && cacheable {
		result := make([]*sourceValue, len(cached.values))
		for i, v := range cached.values {
			result[i] = cloneValue(v, map[*sourceValue]*sourceValue{})
		}
		return result
	}
	env := sourceEnv{}
	for obj, value := range callee.captures {
		env[obj] = value
	}
	if recv := fn.signature.Recv(); recv != nil {
		env[recv] = assignedValue(callee.receiver)
	}
	for i := 0; i < fn.signature.Params().Len(); i++ {
		param := fn.signature.Params().At(i)
		if i < len(args) {
			env[param] = assignedValue(args[i])
		} else {
			env[param] = frame.value(param.Type())
		}
	}
	for i := 0; i < fn.signature.Results().Len(); i++ {
		v := fn.signature.Results().At(i)
		env[v] = frame.value(v.Type())
	}
	frame.env = env
	start := len(e.events)
	e.active[fn] = true
	frame.block(fn.body)
	delete(e.active, fn)
	values := defaults
	if len(frame.returns) > 0 {
		values = make([]*sourceValue, len(defaults))
		for _, returned := range frame.returns {
			for i, v := range returned {
				if i < len(values) {
					values[i] = e.mergeValue(values[i], v)
				}
			}
		}
		for i, v := range values {
			if v == nil {
				values[i] = defaults[i]
			}
		}
	}
	if cacheable && len(e.events) == start {
		stored := make([]*sourceValue, len(values))
		seen := map[*sourceValue]*sourceValue{}
		for i, value := range values {
			if !independentReturnType(value.typ) {
				return values
			}
			stored[i] = cloneValue(value, seen)
		}
		e.summaries[key] = sourceSummary{values: stored}
	}
	return values
}

// independentReturnType 判断返回值是否不含可变引用，否则缓存副本会破坏返回值与实参之间的引用关系
func independentReturnType(t types.Type) bool {
	if _, pointer := types.Unalias(t).(*types.Pointer); pointer {
		return false
	}
	switch t := underlying(t).(type) {
	case *types.Basic:
		return true
	case *types.Array:
		return independentReturnType(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !independentReturnType(t.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}

// mutatesArguments 根据赋值和调用关系判断函数是否可能修改实参引用的数据
func (e *sourceEngine) mutatesArguments(fn *sourceFunction, seen map[*sourceFunction]bool) bool {
	if seen[fn] {
		return false
	}
	seen[fn] = true
	mutated := false
	ast.Inspect(fn.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			object := expressionObject(fn.pkg.TypesInfo, n.Fun)
			if called, ok := object.(*types.Func); ok {
				if target := e.functions[called.Origin()]; target != nil {
					mutated = e.mutatesArguments(target, seen)
				}
			} else if _, ok := object.(*types.Var); ok {
				mutated = true
			}
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				switch lhs.(type) {
				case *ast.StarExpr, *ast.SelectorExpr, *ast.IndexExpr:
					mutated = true
				}
			}
		}
		return !mutated
	})
	return mutated
}

// block 按源码顺序解释语句，返回值表示当前分支是否已通过 return 结束
func (f *sourceFrame) block(block *ast.BlockStmt) bool {
	if block == nil {
		return false
	}
	required := immediateRequiredCalls(block, f.fn.pkg.TypesInfo)
	for _, statement := range block.List {
		start := len(f.engine.events)
		stop := f.statement(statement)
		for _, event := range f.engine.events[start:] {
			if required[event.call] {
				event.required = true
			}
		}
		if stop {
			return true
		}
	}
	return false
}

// statement 分析赋值、调用和控制流语句，并返回当前分支是否结束
func (f *sourceFrame) statement(statement ast.Stmt) bool {
	switch s := statement.(type) {
	case *ast.BlockStmt:
		return f.block(s)
	case *ast.ExprStmt:
		f.expression(s.X)
	case *ast.AssignStmt:
		var values []*sourceValue
		if len(s.Rhs) == 1 {
			if call, ok := s.Rhs[0].(*ast.CallExpr); ok {
				values = f.call(call)
			} else {
				values = []*sourceValue{f.expression(s.Rhs[0])}
			}
		} else {
			for _, expr := range s.Rhs {
				values = append(values, f.expression(expr))
			}
		}
		for i, lhs := range s.Lhs {
			if i < len(values) {
				f.assign(lhs, values[i])
			}
		}
	case *ast.DeclStmt:
		if decl, ok := s.Decl.(*ast.GenDecl); ok {
			for _, spec := range decl.Specs {
				if spec, ok := spec.(*ast.ValueSpec); ok {
					var values []*sourceValue
					if len(spec.Values) == 1 {
						if call, ok := spec.Values[0].(*ast.CallExpr); ok {
							values = f.call(call)
						}
					}
					for i, name := range spec.Names {
						value := f.value(f.fn.pkg.TypesInfo.TypeOf(name))
						if i < len(values) {
							value = values[i]
						} else if i < len(spec.Values) {
							value = f.expression(spec.Values[i])
						}
						f.assign(name, value)
					}
				}
			}
		}
	case *ast.ReturnStmt:
		var values []*sourceValue
		if len(s.Results) == 1 {
			if call, ok := s.Results[0].(*ast.CallExpr); ok {
				values = f.call(call)
			}
		}
		if values == nil {
			for _, expr := range s.Results {
				values = append(values, f.expression(expr))
			}
		}
		if len(s.Results) == 0 {
			for i := 0; i < f.fn.signature.Results().Len(); i++ {
				values = append(values, f.env[f.fn.signature.Results().At(i)])
			}
		}
		f.returns = append(f.returns, values)
		return true
	case *ast.IfStmt:
		if s.Init != nil {
			f.statement(s.Init)
		}
		condition := f.expression(s.Cond)
		if condition.constant != nil && condition.constant.Kind() == constant.Bool {
			if constant.BoolVal(condition.constant) {
				return f.block(s.Body)
			}
			if s.Else != nil {
				return f.statement(s.Else)
			}
			return false
		}
		// 条件未知时分别分析两条路径，再合并仍能继续执行的分支环境
		before := copyEnv(f.env)
		yes := f.block(s.Body)
		yesEnv := f.env
		f.env = copyEnv(before)
		no := false
		if s.Else != nil {
			no = f.statement(s.Else)
		}
		noEnv := f.env
		switch {
		case yes && no:
			return true
		case yes:
			f.env = noEnv
		case no:
			f.env = yesEnv
		default:
			f.env = f.mergeEnvs(yesEnv, noEnv)
		}
	case *ast.RangeStmt:
		iterable := f.expression(s.X)
		items := iterable.items
		if items != nil {
			for i, value := range items {
				f.assign(s.Key, &sourceValue{ref: ir.TypeRef{Kind: ir.TypeBasic, Name: "int"}, constant: constant.MakeInt64(int64(i))})
				f.assign(s.Value, value)
				f.block(s.Body)
			}
		} else {
			before := copyEnv(f.env)
			if s.Value != nil {
				f.assign(s.Value, f.value(f.fn.pkg.TypesInfo.TypeOf(s.Value)))
			}
			f.block(s.Body)
			f.env = f.mergeEnvs(before, f.env)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			f.statement(s.Init)
		}
		if s.Cond != nil {
			f.expression(s.Cond)
		}
		before := copyEnv(f.env)
		f.block(s.Body)
		if s.Post != nil {
			f.statement(s.Post)
		}
		f.env = f.mergeEnvs(before, f.env)
	case *ast.SwitchStmt:
		if s.Init != nil {
			f.statement(s.Init)
		}
		if s.Tag != nil {
			f.expression(s.Tag)
		}
		return f.caseBranches(s.Body)
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			f.statement(s.Init)
		}
		if s.Assign != nil {
			f.statement(s.Assign)
		}
		return f.caseBranches(s.Body)
	case *ast.DeferStmt:
		f.call(s.Call)
	case *ast.GoStmt:
		f.call(s.Call)
	case *ast.LabeledStmt:
		return f.statement(s.Stmt)
	case *ast.IncDecStmt:
		f.assign(s.X, f.value(f.fn.pkg.TypesInfo.TypeOf(s.X)))
	}
	return false
}

// caseBranches 合并仍会继续执行的 switch 分支，有 default 时不保留已经被所有分支修改的初始值
func (f *sourceFrame) caseBranches(body *ast.BlockStmt) bool {
	before := copyEnv(f.env)
	merged := sourceEnv{}
	hasDefault, continues := false, false
	for _, statement := range body.List {
		clause := statement.(*ast.CaseClause)
		hasDefault = hasDefault || clause.List == nil
		f.env = copyEnv(before)
		if !f.block(&ast.BlockStmt{List: clause.Body}) {
			continues = true
			merged = f.mergeEnvs(merged, f.env)
		}
	}
	if !hasDefault {
		merged = f.mergeEnvs(merged, before)
		continues = true
	}
	f.env = merged
	return !continues
}

// assign 更新分析中记录的变量值，字段或下标赋值只修改对应成员
func (f *sourceFrame) assign(target ast.Expr, value *sourceValue) {
	if target == nil || value == nil {
		return
	}
	switch target := target.(type) {
	case *ast.Ident:
		if target.Name != "_" {
			object := f.fn.pkg.TypesInfo.ObjectOf(target)
			copied := assignedValue(value)
			if existing := f.env[object]; existing != nil {
				*existing = *copied
			} else {
				f.env[object] = copied
			}
		}
	case *ast.StarExpr:
		original := indirectValue(f.expression(target.X))
		*original = *assignedValue(value)
	case *ast.SelectorExpr:
		if selection := f.fn.pkg.TypesInfo.Selections[target]; selection != nil {
			if field := f.selectedField(f.expression(target.X), selection.Index()); field != nil {
				*field = *assignedValue(value)
			}
		}
	case *ast.IndexExpr:
		object := f.expression(target.X)
		index := f.expression(target.Index)
		if key, ok := stringValue(index); ok {
			if object.fields == nil {
				object.fields = map[string]*sourceValue{}
			}
			object.fields[key] = value
		} else {
			object.dynamic = true
		}
	}
}

// mergeEnvs 合并两个分支中各变量的可能值
func (f *sourceFrame) mergeEnvs(a, b sourceEnv) sourceEnv {
	result := sourceEnv{}
	for key, v := range a {
		result[key] = v
	}
	for key, v := range b {
		result[key] = f.engine.mergeValue(result[key], v)
	}
	return result
}

// warning 记录分析中无法确定的信息及其源码位置
func (e *sourceEngine) warning(code, message string, pos token.Pos) {
	e.diagnostics = append(e.diagnostics, ir.Diagnostic{Code: code, Severity: ir.SeverityWarning, Message: message, Source: e.program.SourceRef(pos)})
}
