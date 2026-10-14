package glua

import (
	"math"
	"strconv"
	"strings"

	"github.com/folbricht/glua/internal/syntax"
)

// toNumber converts v to a number as Lua's arithmetic does: numbers as they are, strings
// that hold a numeral, nothing else.
func toNumber(v Value) (Value, bool) {
	if v.isNumber() {
		return v, true
	}
	if s, ok := v.Str(); ok {
		return strToNumber(s)
	}
	return Value{}, false
}

// strToNumber converts a string as C Lua's luaO_str2num does: optional surrounding
// whitespace, an optional sign, then a decimal or hexadecimal numeral.
func strToNumber(s string) (Value, bool) {
	s = strings.Trim(s, " \f\n\r\t\v")
	neg := false
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	if s == "" || strings.ContainsAny(s[:1], "+- \f\n\r\t\v") {
		return Value{}, false
	}
	i, f, isInt, ok := syntax.ParseNumber(s)
	if !ok {
		return Value{}, false
	}
	if neg && !isInt {
		// -9223372036854775808 is an integer, though its digits alone overflow
		if u, err := strconv.ParseUint(s, 10, 64); err == nil && u == 1<<63 {
			return Int(math.MinInt64), true
		}
	}
	if isInt {
		if neg {
			i = int64(-uint64(i))
		}
		return Int(i), true
	}
	if neg {
		f = -f
	}
	return Float(f), true
}

// strToIntBase converts s to an integer in base 2..36, as C Lua's tonumber(s, base) does:
// optional surrounding whitespace and a sign, digits and letters of either case, no fraction.
// The value wraps around on overflow.
func strToIntBase(s string, base int) (int64, bool) {
	s = strings.Trim(s, " \f\n\r\t\v")
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'z':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'Z':
			d = int(c-'A') + 10
		default:
			return 0, false
		}
		if d >= base {
			return 0, false
		}
		n = n*uint64(base) + uint64(d)
	}
	if neg {
		n = -n
	}
	return int64(n), true
}
