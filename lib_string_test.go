package glua

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// string.format's conversions, with the results C Lua 5.4.9 gives: the plain forms that skip
// fmt, the padded and flagged ones, and a float longer than its scratch buffer.
func TestStringFormat(t *testing.T) {
	cases := []struct{ spec, arg, want string }{
		{"%d", "0", "0"},
		{"%d", "-123456789", "-123456789"},
		{"%i", "math.mininteger", "-9223372036854775808"},
		{"%5d", "42", "   42"},
		{"%-5d|", "42", "42   |"},
		{"%05d", "-42", "-0042"},
		{"%+d", "7", "+7"},
		{"% d", "7", " 7"},
		{"%.0d", "0", ""},
		{"%5.3d", "7", "  007"},
		{"%-08x|", "255", "ff      |"},
		{"%#x", "0", "0"},
		{"%#X", "255", "0XFF"},
		{"%#o", "0", "0"},
		{"%#.3o", "8", "010"},
		{"%#5o", "8", "  010"},
		{"%u", "-1", "18446744073709551615"},
		{"%X", "math.mininteger", "8000000000000000"},
		{"%f", "1/3", "0.333333"},
		{"%.3f", "0.125", "0.125"},
		{"%.0f", "2.5", "2"},
		{"%e", "123456.789", "1.234568e+05"},
		{"%.2E", "-0.0", "-0.00E+00"},
		{"%.10f", "1e300", "1000000000000000052504760255204420248704468581108159154915854115511802457988908195786371375080447864043704443832883878176942523235360430575644792184786706982848387200926575803737830233794788090059368953234970799945081119038967640880074652742780142494579258788820056842838115669472196386865459400540160.0000000000"},
		{"%5.1f", "3.14159", "  3.1"},
		{"%-8s|", "'INFO'", "INFO    |"},
		{"%8s", "'INFO'", "    INFO"},
		{"%.2s", "'hello'", "he"},
		{"%-3c|", "65", "A  |"},
		{"%q", "'a\"b'", `"a\"b"`},
		{"%q", "math.mininteger", "0x8000000000000000"},
		{"%q", "0.1", "0x1.999999999999ap-4"},
	}
	var src strings.Builder
	src.WriteString("return ")
	for i, c := range cases {
		if i > 0 {
			src.WriteString(", ")
		}
		fmt.Fprintf(&src, "string.format(%q, %s)", c.spec, c.arg)
	}
	_, res := runWithOutput(t, src.String())
	if len(res) != len(cases) {
		t.Fatalf("got %d results, want %d", len(res), len(cases))
	}
	for i, c := range cases {
		if got := res[i].String(); got != c.want {
			t.Errorf("string.format(%q, %s) = %q, want %q", c.spec, c.arg, got, c.want)
		}
	}
}

// A scan for a match skips to the bytes its first item can take (pattern.go's firstSet),
// with the results C Lua gives: for the first items that take a byte, those that may take
// none or any, and the anchors, captures, balances, frontiers and back-references that are
// left to the matcher. Malformed patterns still raise, and the bytes passed over are
// charged as steps.
func TestPatternFirstSet(t *testing.T) {
	src := `
		local out = {}
		local function r(...)
			local t = table.pack(...)
			for i = 1, t.n do t[i] = tostring(t[i]) end
			out[#out + 1] = table.concat(t, ",", 1, t.n)
		end
		r(("hello world"):find("o")) -- one byte: memchr
		r(("hello world"):find("o", 6))
		r(("hello world"):find("o", 12))
		r(("abc"):find("d"))
		r((""):find("a"))
		r(("a1b22c333"):gsub("%d+", "#")) -- a class
		r(("x1y2"):gsub("%d", { ["1"] = "one" })) -- the run between matches is copied once
		r(("a\0b"):gsub("%z", "Z"))
		r(("abc"):find("[^a]")) -- a set
		r(("ab-c]d"):find("[%]-]"))
		r(("aaa"):gsub("a", "b", 2)) -- the limit
		r(("abc"):gsub("x*", "-")) -- may take no byte: every position
		r(("abc"):gsub("(x?)", "-"))
		r(("abc"):gsub("x-", "-"))
		r(("ab"):find("()b")) -- a position capture, then b
		r(("abab"):gsub("(ab)%1", "X")) -- a capture's first item
		r(("xaby"):find("((a)b)"))
		r(("foo(bar)"):find("%b()"))
		r(("THE (quick) fox"):find("%f[%a]%a+"))
		r(("a$b"):find("%$"))
		r(("ab"):find("$"))
		r(("ab"):gsub("$", "!"))
		r(("a$b"):find("$b"))
		r(("a^b"):find("^b"))
		r(("a^b"):find("b^"))
		r(("a.b"):find("%."))
		r(("abc"):find("."))
		local w = {}
		for n in ("1/100 101 2/200"):gmatch("%d+") do w[#w + 1] = n end
		r(table.concat(w, " "))
		local c = 0
		for _ in ("a^b^c"):gmatch("^.") do c = c + 1 end -- '^' is a literal in gmatch
		r(c)
		c = 0
		for _ in ("aaa"):gmatch("a*") do c = c + 1 end -- not the empty match right after
		r(c)
		r(pcall(string.find, "abc", "[a"))
		r(pcall(string.find, "abc", "a%"))
		r(pcall(string.gsub, "abc", "[", ""))
		local ok, err = pcall(function() for _ in ("abc"):gmatch("[a") do end end) -- raised by the iterator
		r(ok, err:match("malformed.*$"))
		for i = 0, 99 do -- past the cache's size, with and without a set
			local p = string.char(97 + i % 26) .. (i % 2 == 0 and "%d" or "")
			if ("q7"):find(p) ~= (i % 26 == 16 and 1 or nil) then error(p) end
		end
		r(#out)
		r((("a"):rep(400)):find(("a"):rep(300))) -- too long for the cache
		return table.concat(out, "|")`
	want := []string{
		"5,5", "8,8", "nil", "nil", "nil",
		"a#b#c#,3", "xoney2,2", "aZb,1",
		"2,2", "3,3",
		"bba,2",
		"-a-b-c-,4", "-a-b-c-,4", "-a-b-c-,4",
		"2,2,2", "X,1", "2,3,ab,a",
		"4,8", "1,3",
		"2,2", "3,2", "ab!,1", "2,3", "nil", "nil", "2,2", "1,1",
		"1 100 101 2 200", "2", "1",
		"false,malformed pattern (missing ']')",
		"false,malformed pattern (ends with '%')",
		"false,malformed pattern (missing ']')",
		"false,malformed pattern (missing ']')",
		"34", "1,300",
	}
	for _, checked := range []bool{true, false} {
		res, err := runInEnv(t, MustNew(withLoop(checked)), src)
		if err != nil || len(res) != 1 {
			t.Fatalf("checked=%v: %v %v", checked, res, err)
		}
		got := strings.Split(res[0].String(), "|")
		for i := range max(len(got), len(want)) {
			var g, w string
			if i < len(got) {
				g = got[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if g != w {
				t.Errorf("checked=%v: case %d: got %q, want %q", checked, i, g, w)
			}
		}
	}
	// the bytes skipped are charged, found or not: a megabyte scanned is 16K steps, over
	// the 7K left after the subject is made
	for _, src := range []string{
		`return (("x"):rep(1 << 20)):match("[yz]")`,
		`return (("x"):rep(1 << 20)):match("y")`,
		`return (("x"):rep(1 << 20) .. "y"):match("[yz]")`,
		`return (("x"):rep(1 << 20)):gsub("[yz]", "")`,
		`for _ in (("x"):rep(1 << 20)):gmatch("%d") do end`,
	} {
		_, err := runInEnv(t, MustNew(Limits{Steps: 40_000, AllocBytes: NoLimit}), src)
		if !errors.Is(err, ErrSteps) {
			t.Errorf("%s: %v, want the step limit", src, err)
		}
	}
	if _, err := runInEnv(t, MustNew(Limits{Steps: 40_000, AllocBytes: NoLimit}), `return #(("x"):rep(1 << 20))`); err != nil {
		t.Errorf("the subject alone: %v", err) // so the scans above were what ran out
	}
	// building a set is charged too, and a long set is left to the matcher
	_, err := runInEnv(t, MustNew(Limits{Steps: 2_000, AllocBytes: NoLimit}),
		`local p = "[" .. ("a"):rep(1 << 16) .. "]" for i = 1, 200 do ("x"):find(p) end`)
	if !errors.Is(err, ErrSteps) {
		t.Errorf("a long set: %v, want the step limit", err)
	}
	_, err = runInEnv(t, MustNew(Limits{Steps: 2_000, AllocBytes: NoLimit}),
		`for i = 1, 100 do ("x"):find("[" .. ("a"):rep(i % 30 + 1) .. "]") end`)
	if !errors.Is(err, ErrSteps) {
		t.Errorf("sets built each call: %v, want the step limit", err)
	}
	// the matcher's errors before the first item are not lost
	res, err := runInEnv(t, MustNew(), `return pcall(string.find, "xyz", ("("):rep(33) .. "a")`)
	if err != nil || len(res) != 2 || res[0].Truthy() || !strings.Contains(res[1].String(), "too many captures") {
		t.Errorf("33 captures: %v %v", res, err)
	}
	// an iterator the budget stopped mid-scan can be called again by a closer
	_, err = runInEnv(t, MustNew(Limits{Steps: 40_000, AllocBytes: NoLimit}), `
		local s = ("x"):rep(1 << 20) .. "y"
		local it = s:gmatch("yq")
		local t <close> = setmetatable({}, { __close = function() pcall(it) end })
		return it()`)
	if !errors.Is(err, ErrSteps) {
		t.Errorf("gmatch stopped by the budget: %v, want the step limit", err)
	}
}
