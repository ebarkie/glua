package glua

import (
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
