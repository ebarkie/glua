package glua

import (
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
