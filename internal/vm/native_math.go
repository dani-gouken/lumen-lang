package vm

import (
	"fmt"
	"math"
)

func init() {
	// float1 wraps a function of one Float
	float1 := func(name string, f func(float64) float64) native {
		return native{name, 1, func(vm *VM, args []Value) (Value, error) {
			x, err := argFloat(args, 0)
			if err != nil {
				return NewNull(), err
			}
			return NewNumber(f(x)), nil
		}}
	}
	// toInt wraps a function that rounds a Float to an Int
	toInt := func(name string, f func(float64) float64) native {
		return native{name, 1, func(vm *VM, args []Value) (Value, error) {
			x, err := argFloat(args, 0)
			if err != nil {
				return NewNull(), err
			}
			r := f(x)
			if math.IsNaN(r) || r < float64(MinInt) || r > float64(MaxInt) {
				return NewNull(), fmt.Errorf("%v does not fit in an Int", x)
			}
			return NewInt(int64(r)), nil
		}}
	}
	float2 := func(name string, f func(a, b float64) float64) native {
		return native{name, 2, func(vm *VM, args []Value) (Value, error) {
			a, err := argFloat(args, 0)
			if err != nil {
				return NewNull(), err
			}
			b, err := argFloat(args, 1)
			if err != nil {
				return NewNull(), err
			}
			return NewNumber(f(a, b)), nil
		}}
	}

	registerNatives("std.math",
		native{"toFloat", 1, func(vm *VM, args []Value) (Value, error) {
			n, err := argInt(args, 0)
			if err != nil {
				return NewNull(), err
			}
			return NewNumber(float64(n)), nil
		}},
		toInt("floor", math.Floor),
		toInt("ceil", math.Ceil),
		toInt("round", math.Round),
		toInt("trunc", math.Trunc),
		native{"sqrt", 1, func(vm *VM, args []Value) (Value, error) {
			x, err := argFloat(args, 0)
			if err != nil {
				return NewNull(), err
			}
			if x < 0 {
				return NewNull(), fmt.Errorf("the square root of %v is not a number", x)
			}
			return NewNumber(math.Sqrt(x)), nil
		}},
		float1("sin", math.Sin),
		float1("cos", math.Cos),
		float1("tan", math.Tan),
		float1("exp", math.Exp),
		native{"log", 1, func(vm *VM, args []Value) (Value, error) {
			x, err := argFloat(args, 0)
			if err != nil {
				return NewNull(), err
			}
			if x <= 0 {
				return NewNull(), fmt.Errorf("the logarithm of %v is not a number", x)
			}
			return NewNumber(math.Log(x)), nil
		}},
		float2("powf", math.Pow),
		float2("atan2", math.Atan2),
	)
}
