package types

import (
	"fmt"

	"github.com/dani-gouken/lumen/internal/ast"
)

// What the operand of a `value?` is (Info.Tries).
const (
	TryResult   = "result"
	TryOptional = "optional"
)

// ResultEnum is the enum of the standard library that `value?` knows: a
// value, or the reason there is none.
const ResultEnum = "std.result.Result"

// resultType returns t as a Result<T, E>, or nil if it is not one.
func resultType(t Type) *StructType {
	st, ok := t.(*StructType)
	if !ok || st.BaseName != ResultEnum || len(st.TypeArgs) != 2 {
		return nil
	}
	return st
}

// nullable reports whether a value of the type may be null.
func nullable(t Type) bool {
	if t.Equals(Null) {
		return true
	}
	u, ok := t.(*UnionType)
	return ok && hasNull(u)
}

// checkSafeAccess checks `value?.member` and `value?.method(args)`: the
// access, made on the value when it is not null. The result is what the
// access gives, or null.
func (c *Checker) checkSafeAccess(e *ast.BinaryExpr) Type {
	left := c.checkExpr(e.Left)
	if left == nil || isUnknown(left) {
		// The name standing for the value has no type either: the access
		// is not looked at, and adds no error of its own
		return Unknown
	}

	if resultType(left) != nil {
		c.errorfWithSuggestion(e.Token(), ErrNeverNull,
			"To go on with its value and pass a failure on to the caller, write (value?).member; to look at both cases, use a match",
			"'?.' is for a value that may be null, and a %s never is", describe(left))
		return Unknown
	}
	if !nullable(left) && !left.Equals(Any) {
		c.errorfWithSuggestion(e.Token(), ErrNeverNull, "Use '.' here",
			"'?.' is for a value that may be null, and this %s never is", describe(left))
	}

	right := c.checkExpr(e.Right)
	if right == nil || isUnknown(right) {
		return Unknown
	}
	if right.Equals(Void) {
		// A method called for what it does: there is no value either way
		return Void
	}
	return optional(right)
}

// checkTry checks `value?`. On a Result it gives the value, and returns
// the failure from the enclosing function if there is none; on an
// optional it gives the value, and returns null if there is none. Either
// way the function has to be one that can return that; in a function
// that returns nothing, `value?` on an optional simply ends it.
func (c *Checker) checkTry(e *ast.UnaryExpr) Type {
	if c.inDefer {
		c.errorfWithSuggestion(e.Token(), ErrBadDefer,
			"A defer runs while its block is being left and cannot return; handle the failure there, with a match or orElse",
			"'?' inside a defer")
	}
	operand := c.checkExpr(e.Expr)
	if operand == nil || isUnknown(operand) {
		return Unknown
	}

	var returns Type = Void
	if c.currentFn != nil && c.currentFn.ReturnType != nil {
		returns = c.resolveType(c.currentFn.ReturnType)
	}

	if result := resultType(operand); result != nil {
		value, reason := result.TypeArgs[0], result.TypeArgs[1]
		outer := resultType(returns)
		switch {
		case isUnknown(returns):
		case outer == nil:
			c.errorfWithSuggestion(e.Token(), ErrBadTry,
				fmt.Sprintf("Make the function return Result<..., %s>, or handle the failure here with a match or orElse", describe(reason)),
				"'?' passes a failure on to the caller, and this function returns %s, not a Result", describe(returns))
			return value
		case !isUnknown(reason) && !isUnknown(outer.TypeArgs[1]) && !outer.TypeArgs[1].Equals(reason):
			c.errorfWithSuggestion(e.Token(), ErrBadTry,
				"Turn one into the other first: value.mapErr(convert)?",
				"'?' would pass on a failure of type %s, and this function returns failures of type %s", describe(reason), describe(outer.TypeArgs[1]))
			return value
		}
		c.info.Tries[e] = TryResult
		return value
	}

	if nullable(operand) {
		value := withoutNull(operand)
		if value == nil {
			value = Null
		}
		// A function that returns nothing just ends there
		if !isUnknown(returns) && !nullable(returns) && !returns.Equals(Any) && !returns.Equals(Void) {
			suggestion := "Give a fallback with '??', or test against null"
			if resultType(returns) == nil {
				suggestion = fmt.Sprintf("Make the function return %s?, give a fallback with '??', or test against null", describe(returns))
			}
			c.errorfWithSuggestion(e.Token(), ErrBadTry, suggestion,
				"'?' on a value that may be null returns null to the caller, and this function returns %s", describe(returns))
			return value
		}
		c.info.Tries[e] = TryOptional
		return value
	}

	c.errorfWithSuggestion(e.Token(), ErrBadTry, "Remove the '?'",
		"'?' is for a Result or a value that may be null, and this is a %s", describe(operand))
	return operand
}
