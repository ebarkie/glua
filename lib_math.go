package glua

import (
	"math"
	"math/bits"
	"math/rand/v2"
)

// The math library, after C Lua 5.4's lmathlib.c, with its xoshiro256** generator so that
// math.randomseed(n) gives C Lua's sequence.

func openMath(g *Table) {
	lib := newLib("math", map[string]GoFunc{
		"abs": mathAbs, "ceil": mathCeil, "floor": mathFloor, "fmod": mathFmod, "log": mathLog,
		"max": mathMax, "min": mathMin, "modf": mathModf, "random": mathRandom,
		"randomseed": mathRandomseed, "sqrt": mathFloat1(math.Sqrt, "sqrt"), "tointeger": mathToInteger,
		"type": mathType, "ult": mathUlt,
		"deg": mathFloat1(func(x float64) float64 { return x * (180.0 / math.Pi) }, "deg"),
		"rad": mathFloat1(func(x float64) float64 { return x * (math.Pi / 180.0) }, "rad"), "exp": mathFloat1(math.Exp, "exp"), "sin": mathFloat1(math.Sin, "sin"),
		"cos": mathFloat1(math.Cos, "cos"), "tan": mathFloat1(math.Tan, "tan"), "asin": mathFloat1(math.Asin, "asin"),
		"acos": mathFloat1(math.Acos, "acos"), "atan": mathAtan,
	})
	lib.Set(String("pi"), Float(math.Pi))
	lib.Set(String("huge"), Float(math.Inf(1)))
	lib.Set(String("maxinteger"), Int(math.MaxInt64))
	lib.Set(String("mininteger"), Int(math.MinInt64))
	g.Set(String("math"), tableValue(lib))
}

func mathFloat1(f func(float64) float64, name string) GoFunc {
	return func(c *Call) error {
		x, err := c.checkFloat(0, name)
		if err != nil {
			return err
		}
		return c.ReturnValues(Float(f(x)))
	}
}

func mathAbs(c *Call) error {
	n, err := c.checkNumber(0, "abs")
	if err != nil {
		return err
	}
	if n.p == tInt() {
		if i := int64(n.n); i < 0 {
			return c.ReturnValues(Int(int64(-uint64(i)))) // abs(mininteger) wraps, as in C Lua
		}
		return c.ReturnValues(n)
	}
	return c.ReturnValues(Float(math.Abs(math.Float64frombits(n.n))))
}

// floorCeil returns an integer when the result fits, else the float.
func floorCeil(c *Call, name string, f func(float64) float64) error {
	n, err := c.checkNumber(0, name)
	if err != nil {
		return err
	}
	if n.p == tInt() {
		return c.ReturnValues(n)
	}
	r := f(math.Float64frombits(n.n))
	if i, ok := floatToInt(r); ok {
		return c.ReturnValues(Int(i))
	}
	return c.ReturnValues(Float(r))
}

func mathFloor(c *Call) error { return floorCeil(c, "floor", math.Floor) }
func mathCeil(c *Call) error  { return floorCeil(c, "ceil", math.Ceil) }

func mathFmod(c *Call) error {
	a, err := c.checkNumber(0, "fmod")
	if err != nil {
		return err
	}
	b, err := c.checkNumber(1, "fmod")
	if err != nil {
		return err
	}
	if a.p == tInt() && b.p == tInt() {
		d := int64(b.n)
		switch {
		case d == 0:
			return c.argError(2, "fmod", "zero")
		case d == -1:
			return c.ReturnValues(Int(0)) // avoids the overflow of mininteger % -1
		}
		return c.ReturnValues(Int(int64(a.n) % d)) // C's %: the sign of the dividend
	}
	x, _ := a.Float()
	y, _ := b.Float()
	if y == 0 || math.IsInf(x, 0) || x != x || y != y {
		return c.ReturnValues(Float((x * y) / (x * y)))
	}
	return c.ReturnValues(Float(math.Mod(x, y)))
}

func mathModf(c *Call) error {
	n, err := c.checkNumber(0, "modf")
	if err != nil {
		return err
	}
	if n.p == tInt() {
		return c.ReturnValues(n, Float(0)) // an integer is its own integral part
	}
	x := math.Float64frombits(n.n)
	ip := math.Floor(x) // rounds towards zero
	if x < 0 {
		ip = math.Ceil(x)
	}
	frac := 0.0
	if x != ip {
		frac = x - ip
	}
	return c.ReturnValues(intOrFloat(ip), Float(frac))
}

func mathLog(c *Call) error {
	x, err := c.checkFloat(0, "log")
	if err != nil {
		return err
	}
	if c.Value(1).IsNil() {
		return c.ReturnValues(Float(math.Log(x)))
	}
	base, err := c.checkFloat(1, "log")
	if err != nil {
		return err
	}
	switch base {
	case 2:
		return c.ReturnValues(Float(math.Log2(x)))
	case 10:
		return c.ReturnValues(Float(math.Log10(x)))
	}
	return c.ReturnValues(Float(math.Log(x) / math.Log(base)))
}

func mathMinMax(c *Call, name string, max bool) error {
	n := c.Len()
	if n < 1 {
		return c.argError(1, name, "value expected")
	}
	best, err := c.checkNumber(0, name)
	if err != nil {
		return err
	}
	for i := 1; i < n; i++ {
		v, err := c.checkNumber(i, name)
		if err != nil {
			return err
		}
		if max && numLessAny(best, v) || !max && numLessAny(v, best) {
			best = v
		}
	}
	return c.ReturnValues(best)
}

// numLessAny is x < y for any two numbers.
func numLessAny(x, y Value) bool {
	if x.p == tInt() && y.p == tInt() {
		return int64(x.n) < int64(y.n)
	}
	return numLess(x, y)
}

func mathMax(c *Call) error { return mathMinMax(c, "max", true) }
func mathMin(c *Call) error { return mathMinMax(c, "min", false) }

func mathToInteger(c *Call) error {
	v := c.Value(0)
	if n, ok := toNumber(v); ok {
		if i, ok := toIntExact(n); ok {
			return c.ReturnValues(Int(i))
		}
	}
	if err := c.checkAny(0, "tointeger"); err != nil {
		return err
	}
	return c.ReturnValues(Value{})
}

func mathType(c *Call) error {
	if err := c.checkAny(0, "type"); err != nil {
		return err
	}
	switch c.Value(0).p {
	case tInt():
		return c.ReturnValues(String("integer"))
	case tFloat():
		return c.ReturnValues(String("float"))
	}
	return c.ReturnValues(Value{})
}

func mathUlt(c *Call) error {
	a, err := c.checkInt(0, "ult")
	if err != nil {
		return err
	}
	b, err := c.checkInt(1, "ult")
	if err != nil {
		return err
	}
	return c.ReturnValues(Bool(uint64(a) < uint64(b)))
}

func mathAtan(c *Call) error {
	y, err := c.checkFloat(0, "atan")
	if err != nil {
		return err
	}
	x := 1.0
	if !c.Value(1).IsNil() {
		if x, err = c.checkFloat(1, "atan"); err != nil {
			return err
		}
	}
	return c.ReturnValues(Float(math.Atan2(y, x)))
}

// randState is the generator of the current entry: seeded on first use from the RandSeed
// option or at random. math.randomseed affects only the current entry.
func (ex *executor) randState() *[4]uint64 {
	if !ex.randSeeded {
		ex.randSeeded = true
		seed := rand.Uint64()
		if ex.randSeed != nil {
			seed = *ex.randSeed
		}
		seedXoshiro(&ex.rand, seed, 0)
	}
	return &ex.rand
}

// xoshiroNext is C Lua 5.4's nextrand: xoshiro256**.
func xoshiroNext(s *[4]uint64) uint64 {
	s0, s1, s2, s3 := s[0], s[1], s[2], s[3]
	s2 ^= s0
	s3 ^= s1
	res := bits.RotateLeft64(s1*5, 7) * 9
	s1 ^= s2
	s0 ^= s3
	s2 ^= s[1] << 17
	s3 = bits.RotateLeft64(s3, 45)
	s[0], s[1], s[2], s[3] = s0, s1, s2, s3
	return res
}

// seedXoshiro is C Lua 5.4's setseed: two seed words, then 16 discarded values.
func seedXoshiro(s *[4]uint64, n1, n2 uint64) {
	s[0], s[1], s[2], s[3] = n1, 0xff, n2, 0
	for range 16 {
		xoshiroNext(s)
	}
}

// project is C Lua 5.4's project: a random integer in [0, n], uniformly, by rejection.
func project(ran, n uint64, s *[4]uint64) uint64 {
	if n&(n+1) == 0 { // n + 1 is a power of 2
		return ran & n
	}
	lim := n
	lim |= lim >> 1
	lim |= lim >> 2
	lim |= lim >> 4
	lim |= lim >> 8
	lim |= lim >> 16
	lim |= lim >> 32
	for ran &= lim; ran > n; ran = xoshiroNext(s) & lim {
	}
	return ran
}

func mathRandom(c *Call) error {
	s := c.ex.randState()
	rv := xoshiroNext(s)
	var low, up int64
	switch c.Len() {
	case 0:
		return c.ReturnValues(Float(float64(rv>>11) * 0x1p-53)) // a float in [0, 1)
	case 1:
		low = 1
		var err error
		if up, err = c.checkInt(0, "random"); err != nil {
			return err
		}
		if up == 0 { // a full random integer
			return c.ReturnValues(Int(int64(rv)))
		}
	case 2:
		var err error
		if low, err = c.checkInt(0, "random"); err != nil {
			return err
		}
		if up, err = c.checkInt(1, "random"); err != nil {
			return err
		}
	default:
		return c.libErrorf("wrong number of arguments")
	}
	if low > up {
		return c.argError(1, "random", "interval is empty")
	}
	return c.ReturnValues(Int(int64(project(rv, uint64(up)-uint64(low), s) + uint64(low))))
}

func mathRandomseed(c *Call) error {
	s := c.ex.randState()
	var n1, n2 uint64
	if c.Len() == 0 {
		n1, n2 = rand.Uint64(), rand.Uint64()
	} else {
		v, err := c.checkInt(0, "randomseed")
		if err != nil {
			return err
		}
		n1 = uint64(v)
		if !c.Value(1).IsNil() {
			v, err := c.checkInt(1, "randomseed")
			if err != nil {
				return err
			}
			n2 = uint64(v)
		}
	}
	seedXoshiro(s, n1, n2)
	return c.ReturnValues(Int(int64(n1)), Int(int64(n2)))
}
