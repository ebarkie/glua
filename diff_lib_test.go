package glua

import (
	"fmt"
	"strconv"
	"strings"
)

// libProgram writes a program that makes one library call and returns all it gives, or the
// error it raises: arguments come from generators that know what each function takes, with
// now and then a value of another type, so that the error paths are compared too.
func (g *progGen) libProgram() string {
	g.b.Reset()
	// d dumps fixed positions: # of a table with holes may be any border, and differ
	g.b.WriteString("local function d(t) local o = {} for i = -1, 9 do o[#o + 1] = tostring(t[i]) end return table.concat(o, ',') end\n")
	switch g.r.IntN(12) {
	case 0, 1:
		g.libFormat()
	case 2, 3:
		g.libPattern()
	case 4:
		g.libPack()
	case 5:
		g.libUTF8()
	case 6, 7:
		g.libTable()
	case 8:
		g.libDate()
	case 9:
		g.libTime()
	default:
		g.libMisc()
	}
	return g.b.String()
}

// call writes a protected call of fn with args, returning everything it gives.
func (g *progGen) call(fn string, args ...string) {
	fmt.Fprintf(&g.b, "return pcall(%s", fn)
	for _, a := range args {
		g.b.WriteString(", " + a)
	}
	g.b.WriteString(")")
}

// maybe returns s most of the time, and now and then a value of any type in its place.
func (g *progGen) maybe(s string) string {
	if g.r.IntN(12) == 0 {
		return g.pick("nil", "true", "{}", "'x'", "1.5", "-1", "0", "print")
	}
	return s
}

func (g *progGen) libStr() string {
	return g.pick("''", "'a'", "'abc'", "'hello world'", "'  x  '", "'a,b,,c'", "'%d'", "'héllo wörld'",
		"'\\0x\\0'", "'ABCdef123'", "'Mixed CASE 42'", "'aaa'", "'(nested (parens) here)'",
		"'x=1, y=22, z=333'", "'tab\\tnew\\nline'", "'\\255\\254\\128'", "'12'", "'0x1F'", "'-3.5e2'",
		"' 0x10 '", "'1e'", "'inf'", "'nan'", "'THE (quick) fox'", "'€uro'", "'a.b.c'", "'[x]'", "'10 20 30'")
}

func (g *progGen) libInt() string {
	return g.pick("0", "1", "2", "3", "5", "-1", "-2", "-5", "10", "17", "100", "255", "256", "1114111",
		"1114112", "2147483647", "-2147483648", "4294967296", "math.maxinteger", "math.mininteger", "65",
		"0x7f", "9", "-100", "31", "63", "64")
}

func (g *progGen) libFloat() string {
	return g.pick("0.5", "-0.0", "0.0", "1/0", "-1/0", "0/0", "3.14159", "1e15", "1e100", "2^53", "2^63",
		"-2^63", "123.456", "1e-310", "2.5", "-2.5", "1e308", "0.1", "100.0", "-7.25", "5e-324")
}

func (g *progGen) libNum() string {
	if g.r.IntN(2) == 0 {
		return g.libInt()
	}
	return g.libFloat()
}

// libFormat is string.format with generated conversions.
func (g *progGen) libFormat() {
	var f strings.Builder
	var args []string
	for range 1 + g.r.IntN(3) {
		f.WriteString(g.pick("", "", "x", " ", "<", "a-b ", "%%"))
		f.WriteByte('%')
		for range g.r.IntN(3) {
			f.WriteString(g.pick("-", "+", " ", "#", "0"))
		}
		if g.r.IntN(2) == 0 {
			f.WriteString(g.pick("1", "5", "10", "25", "99", "100", "0"))
		}
		if g.r.IntN(3) == 0 {
			f.WriteString(g.pick(".", ".0", ".1", ".3", ".10", ".20", ".99", ".100"))
		}
		conv := g.pick("d", "i", "u", "c", "x", "X", "o", "a", "A", "f", "F", "e", "E", "g", "G", "q", "s",
			"d", "s", "f", "g", "x", "z", "b", "l", "")
		f.WriteString(conv)
		var arg string
		switch conv {
		case "d", "i", "u", "c", "x", "X", "o":
			arg = g.libInt()
			if g.r.IntN(4) == 0 {
				arg = g.libNum()
			}
		case "a", "A", "f", "F", "e", "E", "g", "G":
			arg = g.libNum()
		case "q":
			arg = g.pick(g.libStr(), g.libNum(), "true", "nil", "{}")
		default:
			arg = g.pick(g.libStr(), g.libNum(), "true", "nil")
		}
		args = append(args, g.maybe(arg))
	}
	if g.r.IntN(10) == 0 {
		args = args[:len(args)-1] // one argument too few
	}
	g.call("string.format", append([]string{strconv.Quote(f.String())}, args...)...)
}

// libPattern is find, match, gmatch or gsub with a generated pattern.
func (g *progGen) libPattern() {
	var p strings.Builder
	if g.r.IntN(5) == 0 {
		p.WriteByte('^')
	}
	captures := 0
	for range 1 + g.r.IntN(4) {
		piece := g.pick("a", "l", "o", "x", " ", ",", "1", ".", "%a", "%d", "%s", "%w", "%p", "%l", "%u", "%x",
			"%c", "%g", "%A", "%D", "%S", "%W", "[a-c]", "[^%d]", "[%a_]", "[]]", "[^]]", "[a-]", "%.", "%(", "%%",
			"[%d.]", "é")
		switch g.r.IntN(8) {
		case 0:
			piece = "(" + piece + g.pick("", "*", "+") + ")"
			captures++
		case 1:
			piece = "()"
			captures++
		case 2:
			piece = g.pick("%b()", "%b[]", "%f[%w]", "%f[%W]", "%f[%a]")
		case 3:
			if captures > 0 {
				piece = "%" + strconv.Itoa(1+g.r.IntN(captures+1)) // sometimes one too many
			}
		}
		if g.r.IntN(3) == 0 {
			piece += g.pick("*", "+", "-", "?")
		}
		p.WriteString(piece)
	}
	if g.r.IntN(5) == 0 {
		p.WriteByte('$')
	}
	if g.r.IntN(20) == 0 {
		p.WriteString(g.pick("%", "[a", "(", ")", "%b", "%f", "%9")) // malformed
	}
	pat, subj := strconv.Quote(p.String()), g.libStr()
	switch g.r.IntN(5) {
	case 0:
		g.call("string.find", subj, pat, g.maybe(g.pick("1", "2", "-3", "0", "100", "nil")), g.pick("nil", "true", "false"))
	case 1:
		g.call("string.match", subj, pat, g.maybe(g.pick("1", "3", "-2", "nil")))
	case 2:
		fmt.Fprintf(&g.b, "return pcall(function() local o = {} for a, b in string.gmatch(%s, %s, %s) do o[#o + 1] = tostring(a) .. '/' .. tostring(b) end return table.concat(o, ' ') end)",
			subj, pat, g.pick("nil", "1", "3", "-2"))
	default:
		repl := g.pick("'<%0>'", "'%1'", "'[%1|%2]'", "'%%'", "''", "'x'", "'%9'", "'%'",
			"{hello = 'HI', a = 1, l = false}", "function(c) return c:upper() end",
			"function() return nil end", "function() return false end", "function(c) return {} end",
			"string.reverse")
		g.call("string.gsub", subj, pat, repl, g.maybe(g.pick("nil", "nil", "1", "2", "0", "-1", "2.0")))
	}
}

// libPack is string.pack with a generated format, then packsize and unpack of the result.
func (g *progGen) libPack() {
	var f strings.Builder
	var args []string
	for range 1 + g.r.IntN(4) {
		opt := g.pick("b", "B", "h", "H", "i", "I", "l", "L", "j", "J", "T", "f", "d", "n", "s", "z",
			"i1", "i2", "i3", "i8", "i16", "I3", "I16", "s1", "s2", "x", "Xi4", "<", ">", "=", "!", "!4", "!2",
			" ", "c3", "c0", "i17", "c", "X")
		f.WriteString(opt)
		switch opt[0] {
		case 'b', 'B', 'h', 'H', 'i', 'I', 'l', 'L', 'j', 'J', 'T':
			args = append(args, g.maybe(g.pick(g.libInt(), "-128", "127", "128", "-129", "65535", "65536", "-32768")))
		case 'f', 'd', 'n':
			args = append(args, g.maybe(g.libNum()))
		case 's', 'z', 'c':
			args = append(args, g.maybe(g.pick(g.libStr(), "'xyz'", "'ab'")))
		}
	}
	fs := strconv.Quote(f.String())
	fmt.Fprintf(&g.b, "local ok, s = pcall(string.pack, %s%s)\n", fs, strings.Join(append([]string{""}, args...), ", "))
	fmt.Fprintf(&g.b, "if not ok then return ok, s, pcall(string.packsize, %s) end\n", fs)
	fmt.Fprintf(&g.b, "return s, pcall(string.packsize, %s), pcall(string.unpack, %s, s, %s)", fs, fs, g.pick("nil", "1", "2", "-1", "100"))
}

// libUTF8 is a utf8 function on generated strings and code points.
func (g *progGen) libUTF8() {
	s := g.pick(g.libStr(), "'\\u{7FF}\\u{800}\\u{FFFF}\\u{10000}'", "'\\u{10FFFF}'", "'\\xF4\\x90\\x80\\x80'",
		"'\\xC0\\x80'", "'\\xED\\xA0\\x80'", "'a\\x80b'", "'\\xE2\\x82'", "'\\u{7FFFFFFF}'")
	lax := g.pick("nil", "nil", "true", "false")
	switch g.r.IntN(5) {
	case 0:
		var cps []string
		for range 1 + g.r.IntN(3) {
			cps = append(cps, g.maybe(g.pick("65", "0", "127", "128", "2047", "2048", "65535", "65536",
				"1114111", "1114112", "2147483647", "2147483648", "-1", "0xD800", "233")))
		}
		g.call("utf8.char", cps...)
	case 1:
		g.call("utf8.codepoint", s, g.maybe(g.pick("1", "2", "-1", "0", "100", "nil")), g.maybe(g.pick("nil", "-1", "3", "100")), lax)
	case 2:
		g.call("utf8.len", s, g.maybe(g.pick("1", "2", "-1", "0", "100", "nil")), g.maybe(g.pick("nil", "-1", "3", "100")), lax)
	case 3:
		g.call("utf8.offset", s, g.maybe(g.pick("1", "2", "0", "-1", "-3", "5", "100")), g.maybe(g.pick("nil", "1", "2", "-1", "100")))
	default:
		fmt.Fprintf(&g.b, "return pcall(function() local o = {} for p, c in utf8.codes(%s, %s) do o[#o + 1] = p .. ':' .. c end return table.concat(o, ' ') end)", s, lax)
	}
}

// libTable is a table function on a generated array.
func (g *progGen) libTable() {
	var elems []string
	strs := g.r.IntN(2) == 0
	for range g.r.IntN(7) {
		if strs {
			elems = append(elems, g.pick("'b'", "'a'", "'c'", "'aa'", "''", "'B'", "'10'", "'9'"))
		} else {
			elems = append(elems, g.pick("3", "1", "2", "-1", "2.5", "10", "0", "-0.0", "1e15", "7"))
		}
	}
	fmt.Fprintf(&g.b, "local t = {%s}\n", strings.Join(elems, ", "))
	pos := func() string { return g.maybe(g.pick("1", "2", "0", "-1", "#t", "#t + 1", "#t + 2", "3", "nil")) }
	switch g.r.IntN(7) {
	case 0:
		if g.r.IntN(2) == 0 {
			fmt.Fprintf(&g.b, "local ok, e = pcall(table.insert, t, %s)\n", g.maybe("'v'"))
		} else {
			fmt.Fprintf(&g.b, "local ok, e = pcall(table.insert, t, %s, 'v')\n", pos())
		}
		g.b.WriteString("return ok, e, #t, d(t)")
	case 1:
		fmt.Fprintf(&g.b, "local ok, e = pcall(table.remove, t%s)\nreturn ok, e, #t, d(t)", g.pick("", ", "+pos()))
	case 2:
		g.call("table.concat", "t", g.maybe(g.pick("nil", "','", "''", "' - '")), g.maybe(g.pick("nil", "1", "2", "0", "-1")), g.maybe(g.pick("nil", "#t", "2", "#t + 1", "0")))
	case 3:
		g.call("table.unpack", "t", g.maybe(g.pick("nil", "1", "2", "0", "-1", "#t")), g.maybe(g.pick("nil", "#t", "2", "#t + 2", "0", "-1")))
	case 4:
		fmt.Fprintf(&g.b, "local u = {}\nlocal ok, e = pcall(table.move, t, %s, %s, %s, %s)\nreturn ok, tostring(e) == tostring(%s), d(t), d(u)",
			g.maybe(g.pick("1", "2", "0", "-1")), g.maybe(g.pick("#t", "2", "1", "0")), g.maybe(g.pick("1", "2", "3", "0", "-1")),
			g.pick("nil", "u"), g.pick("t", "u"))
	case 5:
		cmp := g.pick("nil", "nil", "function(a, b) return a > b end", "function(a, b) return a < b end")
		fmt.Fprintf(&g.b, "local ok, e = pcall(table.sort, t, %s)\nreturn ok, e, d(t)", cmp)
	default:
		g.call("table.pack", elems...)
		g.b.Reset()
		g.b.WriteString("local function d(t) local o = {} for i = -1, 9 do o[#o + 1] = tostring(t[i]) end return table.concat(o, ',') end\n")
		fmt.Fprintf(&g.b, "local p = table.pack(%s)\nreturn p.n, d(p), select('#', table.unpack(p, 1, p.n))", strings.Join(elems, ", "))
	}
}

// libDate is os.date in UTC with generated formats and times.
func (g *progGen) libDate() {
	t := g.pick("0", "1", "86399", "951782400", "1791972000", "-1", "-86400", "2147483647", "2147483648",
		"4102444800", "-2208988800", "253402300799", "1e10", "1.5", "'x'", "{}", "true") // never the current time
	if g.r.IntN(4) == 0 {
		fmt.Fprintf(&g.b, "local ok, t = pcall(os.date, %s, %s)\nif not ok then return ok, t end\n", g.pick("'!*t'", "'*t'"), t)
		g.b.WriteString("return t.year, t.month, t.day, t.yday, t.wday, t.isdst ~= nil")
		return
	}
	var f strings.Builder
	f.WriteByte('!')
	for range 1 + g.r.IntN(4) {
		f.WriteString(g.pick("", " ", "-", "x"))
		f.WriteByte('%')
		f.WriteString(g.pick("a", "A", "b", "B", "C", "d", "D", "e", "F", "g", "G", "h", "H", "I", "j", "m", "M",
			"n", "p", "R", "S", "t", "T", "u", "U", "V", "w", "W", "y", "Y", "%", "Ey", "EY", "Od", "OH", "Q", "E", "Oy", "Ez", "q"))
	}
	g.call("os.date", strconv.Quote(f.String()), t)
}

// libTime is os.time normalising a generated date table. The difference to a fixed time and the
// normalised day and month do not depend on the time zone.
func (g *progGen) libTime() {
	field := func(name string, vals ...string) string {
		if g.r.IntN(8) == 0 {
			return ""
		}
		return name + " = " + g.maybe(g.pick(vals...)) + ", "
	}
	fmt.Fprintf(&g.b, "local tt = {%s%s%shour = 12}\n",
		field("year", "2000", "2024", "'2001'", "2000.0", "'2000.0'", "1970", "2030"),
		field("month", "1", "2", "12", "13", "0", "-1", "'3'", "2.0", "25"),
		field("day", "1", "15", "28", "29", "31", "32", "0", "-1", "'7'", "60"))
	g.b.WriteString("local ok, t = pcall(os.time, tt)\nif not ok then return ok, t end\n")
	g.b.WriteString("return (t - os.time({year = 2000, month = 1, day = 1, hour = 12})) // 86400, tt.year, tt.month, tt.day, tt.yday, tt.wday")
}

// libMisc is a conversion, math or string function on generated arguments.
func (g *progGen) libMisc() {
	switch g.r.IntN(14) {
	case 0:
		g.call("tonumber", g.pick(g.libStr(), "'ff'", "'FF'", "'zz'", "'777'", "'-10'", "' 11 '", "'1.5'", "'10'", "''", "'1e2'"),
			g.maybe(g.pick("nil", "2", "8", "10", "16", "36", "37", "1", "0")))
	case 1:
		g.call("tostring", g.maybe(g.pick(g.libNum(), g.libStr(), "true", "nil")))
	case 2:
		g.call("math.tointeger", g.maybe(g.pick(g.libNum(), "'8'", "'8.0'", "2^53", "3.0")))
	case 3:
		g.call(g.pick("math.fmod", "math.ult", "math.max", "math.min"), g.maybe(g.libNum()), g.maybe(g.libNum()))
	case 4:
		g.call(g.pick("math.modf", "math.floor", "math.ceil", "math.abs", "math.type", "math.sqrt", "math.exp", "math.log"), g.maybe(g.libNum()))
	case 5:
		g.call("math.log", g.libNum(), g.maybe(g.pick("2", "10", "8", "1", "0", "-1", "nil")))
	case 6:
		g.call(g.pick("math.max", "math.min"), g.libNum(), g.libNum(), g.libNum(), g.maybe(g.libNum()))
	case 7:
		g.call("string.rep", g.maybe(g.libStr()), g.maybe(g.pick("0", "1", "2", "3", "-1", "2.0", "'2'")), g.maybe(g.pick("nil", "','", "''")))
	case 8:
		g.call("string.byte", g.maybe(g.libStr()), g.maybe(g.pick("nil", "1", "2", "-1", "0", "100")), g.maybe(g.pick("nil", "-1", "3", "100")))
	case 9:
		var cs []string
		for range g.r.IntN(4) {
			cs = append(cs, g.maybe(g.pick("65", "0", "255", "256", "-1", "97.0", "'66'")))
		}
		g.call("string.char", cs...)
	case 10:
		g.call("string.sub", g.maybe(g.libStr()), g.maybe(g.pick("1", "2", "-3", "0", "100", "math.mininteger", "2.0")), g.maybe(g.pick("nil", "-1", "3", "100", "math.maxinteger", "0")))
	case 11:
		g.call(g.pick("string.upper", "string.lower", "string.len", "string.reverse"), g.maybe(g.libStr()))
	case 12:
		g.call(g.pick("select", "select"), g.maybe(g.pick("'#'", "1", "2", "-1", "-3", "0", "'#x'", "'2'")), "'a'", "'b'")
	default:
		g.call("string.format", "'%s %s'", g.maybe(g.libNum()), g.maybe(g.pick(g.libStr(), "math.pi", "-math.pi")))
	}
}
