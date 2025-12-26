package irgen

import (
	"strconv"

	"github.com/yourname/lumen/internal/ast"
	"github.com/yourname/lumen/internal/ir"
)

// compileExpr compiles an expression and returns the register containing the result
func (fc *FunctionCompiler) compileExpr(expr ast.Expr) *ir.Register {
	if expr == nil {
		return nil
	}

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
	default:
		// Unsupported expression type
		fc.gen.errs.ReportErrorFromToken(
			ErrUnsupportedConstruct,
			"Unsupported expression type in IR generation",
			expr.Token(),
			fc.gen.sourceLines,
		)
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
	if reg, ok := fc.locals[ident.Name]; ok {
		return reg
	}

	// Check if it's a captured variable (upvalue)
	if upvalIdx, ok := fc.upvalues[ident.Name]; ok {
		return fc.builder.CreateLoadUpvalue(upvalIdx)
	}

	// Must be a global/function reference
	// Create a constant reference
	return fc.builder.CreateMov(ir.Constant{Val: ident.Name, Type: "FuncRef"})
}

// compileBinaryExpr compiles a binary expression
func (fc *FunctionCompiler) compileBinaryExpr(expr *ast.BinaryExpr) *ir.Register {
	left := fc.compileExpr(expr.Left)
	right := fc.compileExpr(expr.Right)

	dest := fc.builder.NewRegister()

	switch expr.Op {
	case "+":
		return fc.builder.CreateAdd(left, right)
	case "-":
		return fc.builder.CreateSub(left, right)
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
	case "&&":
		fc.builder.Emit(ir.OpAnd, dest, left, right)
	case "||":
		fc.builder.Emit(ir.OpOr, dest, left, right)
	default:
		fc.gen.errs.ReportErrorFromToken(
			ErrUnsupportedConstruct,
			"Unsupported binary operator: "+expr.Op,
			expr.Token(),
			fc.gen.sourceLines,
		)
	}

	return dest
}

// compileUnaryExpr compiles a unary expression
func (fc *FunctionCompiler) compileUnaryExpr(expr *ast.UnaryExpr) *ir.Register {
	operand := fc.compileExpr(expr.Expr)
	dest := fc.builder.NewRegister()

	switch expr.Op {
	case "-":
		fc.builder.Emit(ir.OpNeg, dest, operand)
	case "!":
		fc.builder.Emit(ir.OpNot, dest, operand)
	default:
		fc.gen.errs.ReportErrorFromToken(
			ErrUnsupportedConstruct,
			"Unsupported unary operator: "+expr.Op,
			expr.Token(),
			fc.gen.sourceLines,
		)
	}

	return dest
}

// compileCallExpr compiles a function call
func (fc *FunctionCompiler) compileCallExpr(expr *ast.CallExpr) *ir.Register {
	fn := fc.compileExpr(expr.Func)

	// Compile arguments
	args := make([]ir.Value, len(expr.Args))
	for i, arg := range expr.Args {
		args[i] = fc.compileExpr(arg)
	}

	return fc.builder.CreateCall(fn, args...)
}

// compileIndexExpr compiles an index expression (array access)
func (fc *FunctionCompiler) compileIndexExpr(expr *ast.IndexExpr) *ir.Register {
	arr := fc.compileExpr(expr.Expr)
	index := fc.compileExpr(expr.Index)

	dest := fc.builder.NewRegister()
	fc.builder.Emit(ir.OpGetIndex, dest, arr, index)
	return dest
}

// compileMemberExpr compiles a member access expression
func (fc *FunctionCompiler) compileMemberExpr(expr *ast.MemberExpr) *ir.Register {
	obj := fc.compileExpr(expr.Expr)
	return fc.builder.CreateGetField(obj, expr.Name)
}

// compileArrayLiteral compiles an array literal
func (fc *FunctionCompiler) compileArrayLiteral(expr *ast.ArrayLiteralExpr) *ir.Register {
	// Compile each element
	elements := make([]ir.Value, len(expr.Elements))
	for i, elem := range expr.Elements {
		elements[i] = fc.compileExpr(elem)
	}

	// Create array with size
	dest := fc.builder.NewRegister()
	size := ir.Constant{Val: len(expr.Elements), Type: "Int"}
	fc.builder.Emit(ir.OpNewArray, dest, size)

	// Initialize each element
	for i, elem := range elements {
		index := ir.Constant{Val: i, Type: "Int"}
		fc.builder.Emit(ir.OpSetIndex, nil, dest, index, elem)
	}

	return dest
}

// compileStructInit compiles a struct initialization
func (fc *FunctionCompiler) compileStructInit(expr *ast.StructInitExpr) *ir.Register {
	// Compile field values
	fields := make([]ir.Value, 0, len(expr.Fields))
	for _, fieldValue := range expr.Fields {
		fields = append(fields, fc.compileExpr(fieldValue))
	}

	return fc.builder.CreateNewStruct(expr.Name, fields...)
}
