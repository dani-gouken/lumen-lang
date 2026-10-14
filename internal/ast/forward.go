package ast

import "strings"

// ForwardedTo returns the function a method of the standard library does
// nothing but call, with its receiver first and its parameters after it in
// their order: `pub fn trim() -> String { return trim(self); }`. Such a
// method is the function under another name, and is called as the
// function: nothing stands between the program and what fails, and there
// is one call where there were two.
func ForwardedTo(m *FnDecl) string {
	if !strings.HasPrefix(m.Module, "std.") || m.Body == nil || len(m.Body.Stmts) != 1 {
		return ""
	}
	var call *CallExpr
	switch stmt := m.Body.Stmts[0].(type) {
	case *ReturnStmt:
		call, _ = stmt.Value.(*CallExpr)
	case *ExprStmt:
		call, _ = stmt.Expr.(*CallExpr)
	}
	if call == nil || len(call.Args) != len(m.Params)+1 {
		return ""
	}
	callee, ok := call.Func.(*Ident)
	if !ok {
		return ""
	}
	for i, arg := range call.Args {
		name, ok := arg.(*Ident)
		switch {
		case !ok:
			return ""
		case i == 0 && name.Name != "self":
			return ""
		case i > 0 && (name.Name != m.Params[i-1].Name || m.Params[i-1].Type == nil || m.Params[i-1].Type.Kind == TypeVariadic):
			return ""
		}
	}
	return callee.Name
}
