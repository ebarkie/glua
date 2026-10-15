package glua

import (
	"math"
)

// Lua 5.4 arithmetic on numbers. Integer operations wrap around; / and ^ always give floats;
// // and % round towards minus infinity; bitwise operators need operands with an exact
// integer representation.

// arithError is why an arithmetic operation on two numbers failed.
type arithError uint8

const (
	arithOK      arithError = iota
	arithDivZero            // integer division or modulo by zero
	arithNoInt              // a bitwise operand has no integer representation
)

// numArith applies op (an opAdd..opShr opcode) to two numbers.
func numArith(op uint8, a, b Value) (Value, arithError) {
	switch op {
	case opBAnd, opBOr, opBXor, opShl, opShr:
		x, ok1 := toIntExact(a)
		y, ok2 := toIntExact(b)
		if !ok1 || !ok2 {
			return Value{}, arithNoInt
		}
		switch op {
		case opBAnd:
			return Int(x & y), arithOK
		case opBOr:
			return Int(x | y), arithOK
		case opBXor:
			return Int(x ^ y), arithOK
		case opShl:
			return Int(shiftLeft(x, y)), arithOK
		default:
			return Int(shiftLeft(x, -y)), arithOK
		}
	case opDiv, opPow:
		x, y := toFloat(&a), toFloat(&b)
		if op == opDiv {
			return Float(x / y), arithOK
		}
		return Float(luaPow(x, y)), arithOK
	}
	if a.p == tInt() && b.p == tInt() {
		x, y := int64(a.n), int64(b.n)
		switch op {
		case opAdd:
			return Int(int64(uint64(x) + uint64(y))), arithOK
		case opSub:
			return Int(int64(uint64(x) - uint64(y))), arithOK
		case opMul:
			return Int(int64(uint64(x) * uint64(y))), arithOK
		case opIDiv:
			if y == 0 {
				return Value{}, arithDivZero
			}
			return Int(intFloorDiv(x, y)), arithOK
		case opMod:
			if y == 0 {
				return Value{}, arithDivZero
			}
			return Int(intMod(x, y)), arithOK
		}
	}
	x, y := toFloat(&a), toFloat(&b)
	switch op {
	case opAdd:
		return Float(x + y), arithOK
	case opSub:
		return Float(x - y), arithOK
	case opMul:
		return Float(x * y), arithOK
	case opIDiv:
		return Float(math.Floor(x / y)), arithOK
	case opMod:
		return Float(floatMod(x, y)), arithOK
	}
	panic("glua: numArith: bad opcode")
}

// intFloorDiv is integer division rounding towards minus infinity. y is not zero.
func intFloorDiv(x, y int64) int64 {
	if y == -1 {
		return int64(-uint64(x)) // avoids the overflow trap of MinInt64 / -1
	}
	q := x / y
	if (x%y != 0) && ((x < 0) != (y < 0)) {
		q--
	}
	return q
}

// intMod is the integer modulo with the sign of the divisor. y is not zero.
func intMod(x, y int64) int64 {
	if y == -1 {
		return 0
	}
	m := x % y
	if m != 0 && (m^y) < 0 {
		m += y
	}
	return m
}

// floatMod follows C Lua's luai_nummod: fmod, then adjusted to the sign of the divisor.
// Where C's fmod gives NaN it computes (x*y)/(x*y), so the NaN is the one the hardware
// produces, sign included, as in C.
func floatMod(x, y float64) float64 {
	if y == 0 || math.IsInf(x, 0) || x != x || y != y {
		return (x * y) / (x * y)
	}
	m := math.Mod(x, y)
	if (m > 0 && y < 0) || (m < 0 && y != m && y > 0) {
		m += y
	}
	return m
}

// zero is a variable, so that zero/zero is computed by the hardware, not by the compiler.
var zero float64

// luaPow is C Lua's luai_numpow: x*x for an exponent of 2, else C's pow. A NaN operand
// passes through, and an invalid operation gives the hardware's NaN; the sign of a NaN from
// pow differs between C libraries anyway. Powers of two are computed exactly, as a correctly
// rounded pow does, and finite operands go through powPos, which is far closer to C's pow
// than math.Pow.
func luaPow(x, y float64) float64 {
	switch {
	case y == 2:
		return x * x
	case y == 0 || x == 1:
		return 1
	case x != x:
		return x
	case y != y:
		return y
	}
	if frac, exp := math.Frexp(x); frac == 0.5 && x > 0 {
		if e := float64(exp-1) * y; e == math.Trunc(e) && e >= -1074 && e <= 1023 {
			return math.Ldexp(1, int(e))
		}
	}
	if !math.IsInf(x, 0) && !math.IsInf(y, 0) && x != 0 {
		switch {
		case x > 0:
			return powPos(x, y)
		case y == math.Trunc(y): // a negative base: the sign comes from the exponent's parity
			r := powPos(-x, y)
			if math.Mod(y, 2) != 0 {
				r = -r
			}
			return r
		}
	}
	r := math.Pow(x, y)
	if r != r {
		return zero / zero
	}
	return r
}

// shiftLeft shifts x left by n bits, or right by -n bits, logically; 64 or more gives 0.
func shiftLeft(x, n int64) int64 {
	switch {
	case n <= -64 || n >= 64:
		return 0
	case n >= 0:
		return int64(uint64(x) << uint(n))
	default:
		return int64(uint64(x) >> uint(-n))
	}
}

// toIntExact converts a number to an integer if it has an exact integer value.
func toIntExact(v Value) (int64, bool) {
	switch v.p {
	case tInt():
		return int64(v.n), true
	case tFloat():
		return floatToInt(math.Float64frombits(v.n))
	}
	return 0, false
}

// foldArith folds an operation on two constants for the compiler. Like C Lua's constfolding,
// it refuses anything that would raise, and float results that are NaN or zero, so that the
// sign of a zero and the bits of a NaN are decided at run time.
func foldArith(op uint8, a, b Value) (Value, bool) {
	if !a.isNumber() || !b.isNumber() {
		return Value{}, false
	}
	switch op {
	case opDiv, opIDiv, opMod:
		if f, _ := b.Float(); f == 0 {
			return Value{}, false
		}
	}
	r, err := numArith(op, a, b)
	if err != arithOK {
		return Value{}, false
	}
	if r.p == tFloat() {
		f := math.Float64frombits(r.n)
		if f != f || f == 0 {
			return Value{}, false
		}
	}
	return r, true
}
