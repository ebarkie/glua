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
		if y == 2 {
			return Float(x * x), arithOK
		}
		return Float(math.Pow(x, y)), arithOK
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
func floatMod(x, y float64) float64 {
	m := math.Mod(x, y)
	if m*y < 0 {
		m += y
	}
	return m
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
