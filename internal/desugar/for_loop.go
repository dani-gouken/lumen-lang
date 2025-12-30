package desugar

import (
	"github.com/dani-gouken/lumen/internal/ast"
	"github.com/dani-gouken/lumen/internal/token"
	"github.com/dani-gouken/lumen/internal/types"
)

// desugarForStmt transforms a for-in loop into a while loop
//
// Before:
//
//	for item in collection {
//	  body
//	}
//
// After (for arrays):
//
//	{
//	  let __iter_N = 0;
//	  let __len_N = collection.length;
//	  while __iter_N < __len_N {
//	    let item = collection[__iter_N];
//	    body
//	    __iter_N = __iter_N + 1;
//	  }
//	}
func (d *Desugarer) desugarForStmt(forStmt *ast.ForStmt) ast.Stmt {
	pos := forStmt.Position()

	// Validate that the iterable is actually iterable
	if !d.validateForLoopIterable(forStmt) {
		// Return the original statement on error - compilation will fail anyway
		return forStmt
	}

	// Desugar the iterable expression first
	iterableExpr := d.desugarExpr(forStmt.Iterable)

	// Generate unique variable names
	iterVar := d.generateIterVar()
	lenVar := d.generateLenVar()

	// Create iterator initialization: let __iter_N = 0;
	iterInit := &ast.LetStmt{
		BaseNode: ast.BaseNode{Pos: pos},
		Name:     iterVar,
		Type: &ast.Type{
			BaseNode: ast.BaseNode{Pos: pos},
			Kind:     ast.TypeBasic,
			Name:     "Int",
		},
		Value:   d.createIntLiteral("0", pos),
		IsConst: false,
	}

	// Register the type for the iterator variable
	iterIdent := d.createIdent(iterVar, pos)
	d.typeInfo.Types[iterIdent] = types.Int

	// Create length variable: let __len_N = collection.length;
	lengthExpr := d.createMemberExpr(iterableExpr, "length", pos)
	lenInit := &ast.LetStmt{
		BaseNode: ast.BaseNode{Pos: pos},
		Name:     lenVar,
		Type: &ast.Type{
			BaseNode: ast.BaseNode{Pos: pos},
			Kind:     ast.TypeBasic,
			Name:     "Int",
		},
		Value:   lengthExpr,
		IsConst: false,
	}

	// Register the type for the length variable
	lenIdent := d.createIdent(lenVar, pos)
	d.typeInfo.Types[lenIdent] = types.Int

	// Create while condition: __iter_N < __len_N
	iterVarExpr := d.createIdent(iterVar, pos)
	lenVarExpr := d.createIdent(lenVar, pos)
	d.typeInfo.Types[iterVarExpr] = types.Int
	d.typeInfo.Types[lenVarExpr] = types.Int

	whileCond := d.createBinaryExpr(iterVarExpr, "<", lenVarExpr, pos)
	d.typeInfo.Types[whileCond] = types.Bool

	// Create loop variable initialization: let item = collection[__iter_N];
	iterVarForIndex := d.createIdent(iterVar, pos)
	d.typeInfo.Types[iterVarForIndex] = types.Int

	indexExpr := d.createIndexExpr(iterableExpr, iterVarForIndex, pos)

	// Determine the type of the loop variable from the iterable type
	var loopVarType *ast.Type
	if iterableType, ok := d.typeInfo.Types[forStmt.Iterable]; ok {
		switch t := iterableType.(type) {
		case *types.ArrayType:
			// For arrays, the loop variable type is the element type
			loopVarType = d.typeToASTType(t.ElementType, pos)
			d.typeInfo.Types[indexExpr] = t.ElementType
		case *types.PrimitiveType:
			if t.Name == types.PrimitiveString {
				// For strings, the loop variable type is String
				loopVarType = &ast.Type{
					BaseNode: ast.BaseNode{Pos: pos},
					Kind:     ast.TypeBasic,
					Name:     "String",
				}
				d.typeInfo.Types[indexExpr] = types.String
			}
		}
	}

	if loopVarType == nil {
		// Default to any type if we can't determine it
		loopVarType = &ast.Type{
			BaseNode: ast.BaseNode{Pos: pos},
			Kind:     ast.TypeBasic,
			Name:     "Any",
		}
		d.typeInfo.Types[indexExpr] = types.Any
	}

	loopVarInit := &ast.LetStmt{
		BaseNode: ast.BaseNode{Pos: pos},
		Name:     forStmt.Var,
		Type:     loopVarType,
		Value:    indexExpr,
		IsConst:  false,
	}

	// Desugar the loop body
	desugaredBody := d.desugarStmt(forStmt.Body).(*ast.BlockStmt)

	// Create increment statement: __iter_N = __iter_N + 1;
	iterVarForIncr := d.createIdent(iterVar, pos)
	iterVarForAdd := d.createIdent(iterVar, pos)
	d.typeInfo.Types[iterVarForIncr] = types.Int
	d.typeInfo.Types[iterVarForAdd] = types.Int

	incrExpr := d.createBinaryExpr(
		iterVarForAdd,
		"+",
		d.createIntLiteral("1", pos),
		pos,
	)
	d.typeInfo.Types[incrExpr] = types.Int

	incrStmt := &ast.ExprStmt{
		BaseNode: ast.BaseNode{Pos: pos},
		Expr: &ast.BinaryExpr{
			BaseNode: ast.BaseNode{Pos: pos},
			Left:     iterVarForIncr,
			Op:       "=",
			Right:    incrExpr,
		},
	}

	// Build the while body: { let item = ...; <original body>; __iter_N = __iter_N + 1; }
	whileBodyStmts := make([]ast.Stmt, 0, len(desugaredBody.Stmts)+2)
	whileBodyStmts = append(whileBodyStmts, loopVarInit)
	whileBodyStmts = append(whileBodyStmts, desugaredBody.Stmts...)
	whileBodyStmts = append(whileBodyStmts, incrStmt)

	whileBody := &ast.BlockStmt{
		BaseNode: ast.BaseNode{Pos: pos},
		Stmts:    whileBodyStmts,
	}

	// Create the while statement
	whileStmt := &ast.WhileStmt{
		BaseNode: ast.BaseNode{Pos: pos},
		Cond:     whileCond,
		Body:     whileBody,
	}

	// Wrap everything in a block
	blockStmts := []ast.Stmt{iterInit, lenInit, whileStmt}
	block := d.wrapInBlock(blockStmts, pos)

	// Record the mapping
	d.info.Mapping[forStmt] = block

	return block
}

// typeToASTType converts a types.Type to an ast.Type
func (d *Desugarer) typeToASTType(t types.Type, pos token.Position) *ast.Type {
	switch t := t.(type) {
	case *types.PrimitiveType:
		return &ast.Type{
			BaseNode: ast.BaseNode{Pos: pos},
			Kind:     ast.TypeBasic,
			Name:     string(t.Name),
		}
	case *types.ArrayType:
		elemType := d.typeToASTType(t.ElementType, pos)
		return &ast.Type{
			BaseNode: ast.BaseNode{Pos: pos},
			Kind:     ast.TypeArray,
			ElemType: elemType,
		}
	case *types.StructType:
		return &ast.Type{
			BaseNode: ast.BaseNode{Pos: pos},
			Kind:     ast.TypeBasic,
			Name:     t.Name,
		}
	case *types.FunctionType:
		paramTypes := make([]*ast.Type, len(t.Params))
		for i, param := range t.Params {
			paramTypes[i] = d.typeToASTType(param, pos)
		}
		returnType := d.typeToASTType(t.ReturnType, pos)
		return &ast.Type{
			BaseNode:   ast.BaseNode{Pos: pos},
			Kind:       ast.TypeFunction,
			ParamTypes: paramTypes,
			ReturnType: returnType,
		}
	case *types.UnionType:
		unionTypes := make([]*ast.Type, len(t.Types))
		for i, ut := range t.Types {
			unionTypes[i] = d.typeToASTType(ut, pos)
		}
		return &ast.Type{
			BaseNode:   ast.BaseNode{Pos: pos},
			Kind:       ast.TypeUnion,
			UnionTypes: unionTypes,
		}
	default:
		// Default to Any type
		return &ast.Type{
			BaseNode: ast.BaseNode{Pos: pos},
			Kind:     ast.TypeBasic,
			Name:     "Any",
		}
	}
}
