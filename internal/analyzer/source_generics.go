package analyzer

import (
	"go/ast"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// resolveType 使用当前调用的类型实参解释函数体内的类型，因此泛型请求与响应可以共用原始声明
func (f *sourceFrame) resolveType(t types.Type) types.Type {
	return substituteType(t, f.typeArgs)
}

// value 使用当前泛型实参创建表达式的初始分析值
func (f *sourceFrame) value(t types.Type) *sourceValue {
	return f.engine.value(f.resolveType(t))
}

// substituteType 递归替换类型参数，保留结构体字段位置和标签
func substituteType(t types.Type, args map[*types.TypeParam]types.Type) types.Type {
	if len(args) == 0 {
		return t
	}
	switch t := t.(type) {
	case *types.TypeParam:
		if actual := args[t]; actual != nil {
			return actual
		}
	case *types.Pointer:
		return types.NewPointer(substituteType(t.Elem(), args))
	case *types.Slice:
		return types.NewSlice(substituteType(t.Elem(), args))
	case *types.Array:
		return types.NewArray(substituteType(t.Elem(), args), t.Len())
	case *types.Map:
		return types.NewMap(substituteType(t.Key(), args), substituteType(t.Elem(), args))
	case *types.Named:
		if t.TypeArgs().Len() > 0 {
			actual := make([]types.Type, t.TypeArgs().Len())
			for i := range actual {
				actual[i] = substituteType(t.TypeArgs().At(i), args)
			}
			instance, err := types.Instantiate(nil, t.Origin(), actual, false)
			if err == nil {
				return instance
			}
		}
	case *types.Alias:
		if t.TypeArgs().Len() > 0 {
			actual := make([]types.Type, t.TypeArgs().Len())
			for i := range actual {
				actual[i] = substituteType(t.TypeArgs().At(i), args)
			}
			instance, err := types.Instantiate(nil, t.Origin(), actual, false)
			if err == nil {
				return instance
			}
		}
	case *types.Struct:
		fields := make([]*types.Var, t.NumFields())
		tags := make([]string, len(fields))
		for i := range fields {
			field := t.Field(i)
			fields[i] = types.NewField(field.Pos(), field.Pkg(), field.Name(), substituteType(field.Type(), args), field.Anonymous())
			tags[i] = t.Tag(i)
		}
		return types.NewStruct(fields, tags)
	case *types.Tuple:
		variables := make([]*types.Var, t.Len())
		for i := range variables {
			variable := t.At(i)
			variables[i] = types.NewVar(variable.Pos(), variable.Pkg(), variable.Name(), substituteType(variable.Type(), args))
		}
		return types.NewTuple(variables...)
	}
	return t
}

// functionTypeArgs 同时读取显式或推断的函数类型实参，以及泛型方法所属类型的实参
func (f *sourceFrame) functionTypeArgs(expr ast.Expr, value *sourceValue) {
	value.typeArgs = map[*types.TypeParam]types.Type{}
	if value.function == nil {
		return
	}
	var identifier *ast.Ident
	switch expr := expr.(type) {
	case *ast.Ident:
		identifier = expr
	case *ast.SelectorExpr:
		identifier = expr.Sel
	}
	if instance, ok := f.fn.pkg.TypesInfo.Instances[identifier]; ok {
		params := value.function.signature.TypeParams()
		for i := 0; i < params.Len(); i++ {
			value.typeArgs[params.At(i)] = f.resolveType(instance.TypeArgs.At(i))
		}
	}
	if value.receiver != nil {
		t := types.Unalias(value.receiver.typ)
		if pointer, ok := t.(*types.Pointer); ok {
			t = types.Unalias(pointer.Elem())
		}
		if named, ok := t.(*types.Named); ok {
			params := value.function.signature.RecvTypeParams()
			for i := 0; i < params.Len(); i++ {
				value.typeArgs[params.At(i)] = f.resolveType(named.TypeArgs().At(i))
			}
		}
	}
}

// typeArgumentsKey 为泛型实参生成稳定的缓存标识
func typeArgumentsKey(args map[*types.TypeParam]types.Type) string {
	keys := make([]string, 0, len(args))
	for param, actual := range args {
		keys = append(keys, strconv.Itoa(int(param.Obj().Pos()))+"="+types.TypeString(actual, nil))
	}
	sort.Strings(keys)
	return strings.Join(keys, ";")
}
