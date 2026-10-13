package irgen

import (
	"fmt"
	"github.com/dani-gouken/lumen/internal/types"
	"sort"
	"strconv"
	"strings"

	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/ir"
	"github.com/dani-gouken/lumen/internal/resolve"
)

// compileExpr compiles an expression and returns the register containing the result
func (fc *FunctionCompiler) compileExpr(expr ast.Expr) *ir.Register {
	if expr == nil {
		return nil
	}
	fc.setPos(expr)

	switch e := expr.(type) {
	case *ast.Literal:
		return fc.compileLiteral(e)
	case *ast.Ident:
		return fc.compileIdent(e)
	case *ast.BinaryExpr:
		return fc.compileBinaryExpr(e)
	case *ast.UnaryExpr:
		return fc.compileUnaryExpr(e)
	case *ast.CallExpr:
		return fc.compileCallExpr(e)
	case *ast.IndexExpr:
		return fc.compileIndexExpr(e)
	case *ast.MemberExpr:
		return fc.compileMemberExpr(e)
	case *ast.ArrayLiteralExpr:
		return fc.compileArrayLiteral(e)
	case *ast.StructInitExpr:
		return fc.compileStructInit(e)
	case *ast.FnExpr:
		return fc.compileLambda(e)
	case *ast.CastExpr:
		return fc.compileCastExpr(e)
	case *ast.TypeTestExpr:
		return fc.compileTypeTest(e)
	case *ast.VariantExpr:
		return fc.compileVariant(e)
	case *ast.VariantTestExpr:
		return fc.compileVariantTest(e)
	case *ast.VariantFieldExpr:
		value := fc.compileExpr(e.Expr)
		fc.setPos(e)
		dest := fc.builder.NewRegister()
		fc.builder.Emit(ir.OpGetField, dest, value, ir.Constant{Val: variantField(e.Index), Type: "String"})
		return dest
	default:
		// Unsupported expression type
		fc.gen.errorAt(expr, ErrUnsupportedConstruct, "Unsupported expression type in IR generation", "")
		return fc.builder.NewRegister()
	}
}

// compileLiteral compiles a literal value
func (fc *FunctionCompiler) compileLiteral(lit *ast.Literal) *ir.Register {
	var val interface{}
	var typeRef string

	switch lit.Kind {
	case "int":
		intVal, _ := strconv.ParseInt(lit.Value, 10, 64)
		val = intVal
		typeRef = "Int"
	case "float":
		floatVal, _ := strconv.ParseFloat(lit.Value, 64)
		val = floatVal
		typeRef = "Float"
	case "string":
		val = lit.Value
		typeRef = "String"
	case "bool":
		val = lit.Value == "true"
		typeRef = "Bool"
	case "null":
		val = nil
		typeRef = "Null"
	default:
		val = lit.Value
		typeRef = "Unknown"
	}

	constant := ir.Constant{Val: val, Type: ir.TypeRef(typeRef)}
	return fc.builder.CreateMov(constant)
}

// compileIdent compiles an identifier
func (fc *FunctionCompiler) compileIdent(ident *ast.Ident) *ir.Register {
	// Check if it's a local variable
	if reg, ok := fc.lookup(ident.Name); ok {
		return reg
	}

	// Check if it's a captured variable (upvalue)
	if upvalIdx, ok := fc.upvalues[ident.Name]; ok {
		return fc.builder.CreateLoadUpvalue(upvalIdx)
	}

	// Must be a global function reference
	if _, ok := fc.gen.funcs[ident.Name]; !ok {
		fc.gen.errorAt(ident, ErrNotAValue,
			fmt.Sprintf("'%s' cannot be used as a value", ident.Name),
			"Only variables and functions can be used in expressions")
	}
	return fc.builder.CreateMov(ir.Constant{Val: ident.Name, Type: "FuncRef"})
}

// compileBinaryExpr compiles a binary expression
func (fc *FunctionCompiler) compileBinaryExpr(expr *ast.BinaryExpr) *ir.Register {
	switch expr.Op {
	case "=":
		return fc.compileAssignment(expr)
	case "&&", "||", "??":
		return fc.compileShortCircuit(expr)
	case ast.SafeAccessOp:
		return fc.compileSafeAccess(expr)
	}
	if base, ok := ast.CompoundOp(expr.Op); ok {
		return fc.compileCompoundAssignment(expr, base)
	}

	left := fc.compileExpr(expr.Left)
	right := fc.compileExpr(expr.Right)
	fc.setPos(expr)

	dest := fc.builder.NewRegister()

	switch expr.Op {
	case "+":
		fc.builder.Emit(ir.OpAdd, dest, left, right)
	case "-":
		fc.builder.Emit(ir.OpSub, dest, left, right)
	case "*":
		fc.builder.Emit(ir.OpMul, dest, left, right)
	case "/":
		fc.builder.Emit(ir.OpDiv, dest, left, right)
	case "%":
		fc.builder.Emit(ir.OpMod, dest, left, right)
	case "==":
		fc.builder.Emit(ir.OpEq, dest, left, right)
	case "!=":
		fc.builder.Emit(ir.OpNeq, dest, left, right)
	case "<":
		fc.builder.Emit(ir.OpLt, dest, left, right)
	case "<=":
		fc.builder.Emit(ir.OpLte, dest, left, right)
	case ">":
		fc.builder.Emit(ir.OpGt, dest, left, right)
	case ">=":
		fc.builder.Emit(ir.OpGte, dest, left, right)
	default:
		fc.gen.errorAt(expr, ErrUnsupportedConstruct, "Unsupported binary operator: "+expr.Op, "")
	}

	return dest
}

// compileShortCircuit compiles &&, || and ??. The right operand is only
// evaluated when the left one does not already decide the result.
//
//	result = left
//	br <needs right>, rhs, end
//	rhs: result = right; jmp end
//	end:
func (fc *FunctionCompiler) compileShortCircuit(expr *ast.BinaryExpr) *ir.Register {
	left := fc.compileExpr(expr.Left)
	fc.setPos(expr)

	// The result register is written on both paths
	result := fc.builder.CreateMov(left)
	fc.varRegs[result.ID] = true

	rhsBlock := fc.builder.NewBlock()
	endBlock := fc.builder.NewBlock()

	switch expr.Op {
	case "&&":
		fc.builder.CreateBr(result, rhsBlock, endBlock)
	case "||":
		fc.builder.CreateBr(result, endBlock, rhsBlock)
	case "??":
		null := fc.builder.CreateMov(ir.Constant{Val: nil, Type: "Null"})
		isNull := fc.builder.NewRegister()
		fc.builder.Emit(ir.OpEq, isNull, result, null)
		fc.builder.CreateBr(isNull, rhsBlock, endBlock)
	}

	fc.builder.SetBlock(rhsBlock)
	right := fc.compileExpr(expr.Right)
	fc.builder.Emit(ir.OpMov, result, right)
	fc.builder.CreateJmp(endBlock)

	fc.builder.SetBlock(endBlock)
	return result
}

// compileSafeAccess compiles `value?.member`: null if the value is null,
// otherwise the access, in which the value goes by a name of its own.
func (fc *FunctionCompiler) compileSafeAccess(expr *ast.BinaryExpr) *ir.Register {
	value := fc.compileExpr(expr.Left)
	fc.setPos(expr)

	result := fc.builder.CreateMov(ir.Constant{Val: nil, Type: "Null"})
	isNull := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpEq, isNull, value, result)

	accessBlock := fc.builder.NewBlock()
	endBlock := fc.builder.NewBlock()
	fc.builder.CreateBr(isNull, endBlock, accessBlock)

	fc.builder.SetBlock(accessBlock)
	// The value may be a variable that the access assigns: keep what it
	// was when it was tested
	held := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpMov, held, value)
	if holder := ast.SafeHolder(expr); holder != nil {
		fc.declare(holder.Name, held)
	}
	access := fc.compileExpr(expr.Right)
	fc.builder.Emit(ir.OpMov, result, access)
	fc.builder.CreateJmp(endBlock)

	fc.builder.SetBlock(endBlock)
	return result
}

// compileTry compiles `value?`: the value of a Result or of an optional,
// or a return from the function with what stands for "none". A Result
// that is a failure is returned as it is: a value of Result<T, E> without
// a T in it is a Result<U, E> as well.
func (fc *FunctionCompiler) compileTry(expr *ast.UnaryExpr) *ir.Register {
	value := fc.compileExpr(expr.Expr)
	fc.setPos(expr)

	leaveBlock := fc.builder.NewBlock()
	goOnBlock := fc.builder.NewBlock()
	failed := fc.builder.NewRegister()
	isResult := fc.gen.typeInfo.Tries[expr] == types.TryResult
	if isResult {
		fn := fc.builder.CreateMov(ir.Constant{Val: TypeTestIntrinsic, Type: "FuncRef"})
		desc := fc.builder.CreateMov(ir.Constant{Val: "{" + types.ResultEnum + ".Err}", Type: "String"})
		fc.builder.Emit(ir.OpMov, failed, fc.builder.CreateCall(fn, value, desc))
	} else {
		null := fc.builder.CreateMov(ir.Constant{Val: nil, Type: "Null"})
		fc.builder.Emit(ir.OpEq, failed, value, null)
	}
	fc.builder.CreateBr(failed, leaveBlock, goOnBlock)

	fc.builder.SetBlock(leaveBlock)
	fc.builder.CreateRet(value)

	fc.builder.SetBlock(goOnBlock)
	if !isResult {
		return value
	}
	dest := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpGetField, dest, value, ir.Constant{Val: variantField(0), Type: "String"})
	return dest
}

// compileAssignment compiles `target = value` for variables, array elements
// and struct fields.
func (fc *FunctionCompiler) compileAssignment(expr *ast.BinaryExpr) *ir.Register {
	switch target := expr.Left.(type) {
	case *ast.Ident:
		value := fc.compileExpr(expr.Right)
		fc.setPos(expr)
		if reg, ok := fc.lookup(target.Name); ok {
			fc.builder.Emit(ir.OpMov, reg, value)
		} else if upvalIdx, ok := fc.upvalues[target.Name]; ok {
			fc.builder.CreateStoreUpvalue(upvalIdx, value)
		} else {
			fc.gen.errorAt(target, ErrInvalidAssignment,
				fmt.Sprintf("cannot assign to '%s'", target.Name),
				"Only variables declared with 'let' can be assigned to")
		}
		return value

	case *ast.IndexExpr:
		arr := fc.compileExpr(target.Expr)
		index := fc.compileExpr(target.Index)
		value := fc.compileExpr(expr.Right)
		fc.setPos(expr)
		fc.builder.Emit(ir.OpSetIndex, nil, arr, index, value)
		return value

	case *ast.MemberExpr:
		obj := fc.compileExpr(target.Expr)
		value := fc.compileExpr(expr.Right)
		fc.setPos(expr)
		fc.builder.Emit(ir.OpSetField, nil, obj, ir.Constant{Val: target.Name, Type: "String"}, value)
		return value
	}

	fc.gen.errorAt(expr.Left, ErrInvalidAssignment, "invalid assignment target",
		"Only variables, fields and array elements can be assigned to")
	return fc.builder.NewRegister()
}

var arithmeticOps = map[string]ir.OpType{
	"+": ir.OpAdd,
	"-": ir.OpSub,
	"*": ir.OpMul,
	"/": ir.OpDiv,
	"%": ir.OpMod,
}

// compileCompoundAssignment compiles `target op= value`. The target is
// evaluated once: in `xs[next()] += 1`, next() is called a single time.
func (fc *FunctionCompiler) compileCompoundAssignment(expr *ast.BinaryExpr, op string) *ir.Register {
	irOp := arithmeticOps[op]

	switch target := expr.Left.(type) {
	case *ast.Ident:
		value := fc.compileExpr(expr.Right)
		fc.setPos(expr)
		if reg, ok := fc.lookup(target.Name); ok {
			// The variable's register is both operand and destination
			fc.builder.Emit(irOp, reg, reg, value)
			return reg
		}
		if upvalIdx, ok := fc.upvalues[target.Name]; ok {
			current := fc.builder.CreateLoadUpvalue(upvalIdx)
			result := fc.builder.NewRegister()
			fc.builder.Emit(irOp, result, current, value)
			fc.builder.CreateStoreUpvalue(upvalIdx, result)
			return result
		}
		fc.gen.errorAt(target, ErrInvalidAssignment,
			fmt.Sprintf("cannot assign to '%s'", target.Name),
			"Only variables declared with 'let' can be assigned to")

	case *ast.IndexExpr:
		arr := fc.compileExpr(target.Expr)
		index := fc.compileExpr(target.Index)
		fc.setPos(target)
		current := fc.builder.NewRegister()
		fc.builder.Emit(ir.OpGetIndex, current, arr, index)

		value := fc.compileExpr(expr.Right)
		fc.setPos(expr)
		result := fc.builder.NewRegister()
		fc.builder.Emit(irOp, result, current, value)
		fc.builder.Emit(ir.OpSetIndex, nil, arr, index, result)
		return result

	case *ast.MemberExpr:
		obj := fc.compileExpr(target.Expr)
		fc.setPos(target)
		current := fc.builder.CreateGetField(obj, target.Name)

		value := fc.compileExpr(expr.Right)
		fc.setPos(expr)
		result := fc.builder.NewRegister()
		fc.builder.Emit(irOp, result, current, value)
		fc.builder.Emit(ir.OpSetField, nil, obj, ir.Constant{Val: target.Name, Type: "String"}, result)
		return result

	default:
		fc.gen.errorAt(expr.Left, ErrInvalidAssignment, "invalid assignment target",
			"Only variables, fields and array elements can be assigned to")
	}
	return fc.builder.NewRegister()
}

// compileUnaryExpr compiles a unary expression
func (fc *FunctionCompiler) compileUnaryExpr(expr *ast.UnaryExpr) *ir.Register {
	// A negative number literal is one constant. This is also the only way
	// to write the smallest Int: its magnitude alone is not an Int.
	if lit, ok := expr.Expr.(*ast.Literal); ok && expr.Op == "-" && (lit.Kind == "int" || lit.Kind == "float") {
		negative := *lit
		negative.Value = "-" + lit.Value
		return fc.compileLiteral(&negative)
	}

	if expr.Op == ast.TryOp {
		return fc.compileTry(expr)
	}

	operand := fc.compileExpr(expr.Expr)
	fc.setPos(expr)
	dest := fc.builder.NewRegister()

	switch expr.Op {
	case "-":
		fc.builder.Emit(ir.OpNeg, dest, operand)
	case "!":
		fc.builder.Emit(ir.OpNot, dest, operand)
	default:
		fc.gen.errorAt(expr, ErrUnsupportedConstruct, "Unsupported unary operator: "+expr.Op, "")
	}

	return dest
}

// compileCastExpr compiles `value as Type`. The type checker has decided
// whether the cast can fail; only then is a runtime check emitted.
func (fc *FunctionCompiler) compileCastExpr(expr *ast.CastExpr) *ir.Register {
	value := fc.compileExpr(expr.Expr)

	descriptor, checked := fc.gen.typeInfo.Casts[expr]
	if !checked {
		return value
	}

	fc.setPos(expr)
	fn := fc.builder.CreateMov(ir.Constant{Val: CastIntrinsic, Type: "FuncRef"})
	desc := fc.builder.CreateMov(ir.Constant{Val: descriptor, Type: "String"})
	return fc.builder.CreateCall(fn, value, desc)
}

// compileTypeTest compiles `value is Type` to a call of the type test
// intrinsic with the description of the type computed by the type checker.
func (fc *FunctionCompiler) compileTypeTest(expr *ast.TypeTestExpr) *ir.Register {
	value := fc.compileExpr(expr.Expr)
	fc.setPos(expr)

	descriptor, ok := fc.gen.typeInfo.Tests[expr]
	if !ok {
		fc.gen.errorAt(expr, ErrUnsupportedConstruct, "this type test cannot be compiled", "")
		return fc.builder.NewRegister()
	}

	fn := fc.builder.CreateMov(ir.Constant{Val: TypeTestIntrinsic, Type: "FuncRef"})
	desc := fc.builder.CreateMov(ir.Constant{Val: descriptor, Type: "String"})
	return fc.builder.CreateCall(fn, value, desc)
}

// compileCallExpr compiles a function or method call
func (fc *FunctionCompiler) compileCallExpr(expr *ast.CallExpr) *ir.Register {
	if member, ok := expr.Func.(*ast.MemberExpr); ok {
		if how, structName := fc.gen.resolveMethod(member.Expr, member.Name); how != dispatchField {
			return fc.compileMethodCall(expr, member, how, structName)
		}
	}

	// The length of an array or a text is one instruction, not a call
	if ident, ok := expr.Func.(*ast.Ident); ok && len(expr.Args) == 1 && isLengthFunction(ident.Name) {
		if _, hidden := fc.lookup(ident.Name); !hidden {
			value := fc.compileExpr(expr.Args[0])
			fc.setPos(expr)
			dest := fc.builder.NewRegister()
			fc.builder.Emit(ir.OpLen, dest, value)
			return dest
		}
	}

	fn := fc.compileExpr(expr.Func)
	args := fc.compileArgs(expr)
	fc.setPos(expr)
	return fc.builder.CreateCall(fn, args...)
}

// isLengthFunction reports whether a function is one of the standard
// library's two length functions: strings.len, and arrays.len, which is
// generic and so has one name per type it is used with.
func isLengthFunction(name string) bool {
	return name == "std.strings.len" || name == "std.arrays.len" || strings.HasPrefix(name, "std.arrays.len_")
}

// compileMethodCall compiles `receiver.method(args)`. The receiver is passed
// as `self`. When the receiver's struct is known the method's function is
// called directly; when only a trait is known, the function is read from
// the value, where struct literals store the methods a trait can reach.
func (fc *FunctionCompiler) compileMethodCall(expr *ast.CallExpr, member *ast.MemberExpr, how dispatch, structName string) *ir.Register {
	receiver := fc.compileExpr(member.Expr)
	fc.setPos(member)

	var fn *ir.Register
	if how == dispatchStatic {
		fn = fc.builder.CreateMov(ir.Constant{Val: methodIRName(structName, member.Name), Type: "FuncRef"})
	} else {
		fn = fc.builder.CreateGetField(receiver, member.Name)
	}

	args := []ir.Value{receiver}
	for _, arg := range expr.Args {
		args = append(args, fc.compileExpr(arg))
	}
	fc.setPos(expr)
	return fc.builder.CreateCall(fn, args...)
}

// compileArgs compiles call arguments. When the callee is a known function
// with a variadic last parameter, the extra arguments are packed into an
// array, which is what the callee sees.
func (fc *FunctionCompiler) compileArgs(expr *ast.CallExpr) []ir.Value {
	decl := fc.calleeDecl(expr.Func)
	if decl == nil || !isVariadic(decl) {
		args := make([]ir.Value, len(expr.Args))
		for i, arg := range expr.Args {
			args[i] = fc.compileExpr(arg)
		}
		return args
	}

	fixed := len(decl.Params) - 1
	if fixed > len(expr.Args) {
		fixed = len(expr.Args)
	}

	args := make([]ir.Value, 0, fixed+1)
	for _, arg := range expr.Args[:fixed] {
		args = append(args, fc.compileExpr(arg))
	}
	return append(args, fc.compileArray(expr.Args[fixed:]))
}

// calleeDecl returns the declaration when the callee is a direct reference
// to a user-defined top-level function, and nil otherwise.
func (fc *FunctionCompiler) calleeDecl(callee ast.Expr) *ast.FnDecl {
	ident, ok := callee.(*ast.Ident)
	if !ok || resolve.IsBuiltin(ident.Name) {
		return nil // Builtins are variadic natively
	}
	if _, isLocal := fc.lookup(ident.Name); isLocal {
		return nil
	}
	if _, isUpvalue := fc.upvalues[ident.Name]; isUpvalue {
		return nil
	}
	return fc.gen.funcs[ident.Name]
}

func isVariadic(fn *ast.FnDecl) bool {
	if len(fn.Params) == 0 {
		return false
	}
	last := fn.Params[len(fn.Params)-1]
	return last.Type != nil && last.Type.Kind == ast.TypeVariadic
}

// compileIndexExpr compiles an index expression (array access)
func (fc *FunctionCompiler) compileIndexExpr(expr *ast.IndexExpr) *ir.Register {
	arr := fc.compileExpr(expr.Expr)
	index := fc.compileExpr(expr.Index)
	fc.setPos(expr)

	dest := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpGetIndex, dest, arr, index)
	return dest
}

// compileMemberExpr compiles a member access expression. `.length` is the
// length of an array or string; on a struct it reads the field of that name.
func (fc *FunctionCompiler) compileMemberExpr(expr *ast.MemberExpr) *ir.Register {
	obj := fc.compileExpr(expr.Expr)
	fc.setPos(expr)

	if expr.Name == "length" {
		dest := fc.builder.NewRegister()
		fc.builder.Emit(ir.OpLen, dest, obj)
		return dest
	}
	return fc.builder.CreateGetField(obj, expr.Name)
}

// compileArrayLiteral compiles an array literal
func (fc *FunctionCompiler) compileArrayLiteral(expr *ast.ArrayLiteralExpr) *ir.Register {
	return fc.compileArray(expr.Elements)
}

// compileArray builds an array holding the given expressions in order
func (fc *FunctionCompiler) compileArray(elements []ast.Expr) *ir.Register {
	// Compile each element
	values := make([]ir.Value, len(elements))
	for i, elem := range elements {
		values[i] = fc.compileExpr(elem)
	}

	// Create array with capacity, then append each element
	dest := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpNewArray, dest, ir.Constant{Val: len(elements), Type: "Int"})
	for _, value := range values {
		fc.builder.Emit(ir.OpPush, nil, dest, value)
	}

	return dest
}

// compileStructInit compiles a struct initialization. Fields are set by
// name, then the methods that need a runtime lookup are stored on the value.
func (fc *FunctionCompiler) compileStructInit(expr *ast.StructInitExpr) *ir.Register {
	// Map iteration order is random; sort for deterministic output
	names := make([]string, 0, len(expr.Fields))
	for name := range expr.Fields {
		names = append(names, name)
	}
	sort.Strings(names)

	values := make([]ir.Value, len(names))
	for i, name := range names {
		values[i] = fc.compileExpr(expr.Fields[name])
	}
	fc.setPos(expr)

	dest := fc.builder.CreateNewStruct(expr.Name)
	for i, name := range names {
		fc.builder.Emit(ir.OpSetField, nil, dest, ir.Constant{Val: name, Type: "String"}, values[i])
	}

	for _, m := range fc.gen.structMethods[expr.Name] {
		if !fc.gen.storedOnValue(m) {
			continue
		}
		fn := fc.builder.CreateMov(ir.Constant{Val: m.irName, Type: "FuncRef"})
		fc.builder.Emit(ir.OpSetField, nil, dest, ir.Constant{Val: m.name, Type: "String"}, fn)
	}

	return dest
}

// variantField is the name of the field holding the i-th value of an enum
// value.
func variantField(i int) string {
	return strconv.Itoa(i)
}

// compileVariant creates a value of an enum: an object carrying the name
// of the variant, with the values as fields "0", "1", ...
func (fc *FunctionCompiler) compileVariant(expr *ast.VariantExpr) *ir.Register {
	values := make([]ir.Value, len(expr.Args))
	for i, arg := range expr.Args {
		values[i] = fc.compileExpr(arg)
	}
	fc.setPos(expr)

	dest := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpNewVariant, dest, ir.Constant{Val: expr.Enum + "." + expr.Variant, Type: "String"})
	for i, value := range values {
		fc.builder.Emit(ir.OpSetField, nil, dest, ir.Constant{Val: variantField(i), Type: "String"}, value)
	}

	// Methods that are looked up on the value, as for a struct
	for _, m := range fc.gen.structMethods[expr.Enum] {
		if !fc.gen.storedOnValue(m) {
			continue
		}
		fn := fc.builder.CreateMov(ir.Constant{Val: m.irName, Type: "FuncRef"})
		fc.builder.Emit(ir.OpSetField, nil, dest, ir.Constant{Val: m.name, Type: "String"}, fn)
	}
	return dest
}

// compileVariantTest asks whether a value is a given variant, with the
// same intrinsic as `value is Type`: the variant's name is what the value
// carries.
func (fc *FunctionCompiler) compileVariantTest(expr *ast.VariantTestExpr) *ir.Register {
	value := fc.compileExpr(expr.Expr)
	fc.setPos(expr)

	fn := fc.builder.CreateMov(ir.Constant{Val: TypeTestIntrinsic, Type: "FuncRef"})
	desc := fc.builder.CreateMov(ir.Constant{Val: "{" + expr.Enum + "." + expr.Variant + "}", Type: "String"})
	return fc.builder.CreateCall(fn, value, desc)
}
